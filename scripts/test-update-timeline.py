#!/usr/bin/env python3
"""Verify repeated cold-snapshot recovery forks timelines without replaying candidate WAL."""

import subprocess
import time
import uuid
from pathlib import Path

image = "postgres:17@sha256:e38411452a464af89e5adadb8d223bf53b898d47d6ef918b2d58c08707350449"
source = (Path(__file__).resolve().parents[1] / "internal/update/postgres.go").read_text()
recovery = source.split("const prepareRecovery = `", 1)[1].split("`", 1)[0]
recovery = recovery.replace(
    "pgbackrest --config=/var/lib/acta-backup/config/pgbackrest.conf --stanza=acta archive-get",
    "cp /archive/",
).replace('cp /archive/ "%f" "%p"', 'cp "/archive/%f" "%p"')
container = "acta-timeline-" + uuid.uuid4().hex[:8]
data_volume = container + "-data"
snapshot_volume = container + "-snapshot"
archive_volume = container + "-archive"


def run(*args: str) -> str:
    return subprocess.check_output(["docker", *args], text=True).strip()


def sql(query: str) -> str:
    return run("exec", "-u", "postgres", container, "psql", "-h", "127.0.0.1", "-At", "-c", query)


def helper(script: str) -> str:
    return run(
        "run",
        "--rm",
        "--network",
        "none",
        "-v",
        data_volume + ":/data",
        "-v",
        snapshot_volume + ":/snapshot",
        "-v",
        archive_volume + ":/archive",
        "--entrypoint",
        "sh",
        image,
        "-ec",
        script,
    )


def ready() -> None:
    for _ in range(60):
        try:
            if sql("SELECT NOT pg_is_in_recovery()") == "t":
                return
        except subprocess.CalledProcessError:
            # PostgreSQL rejects connections while starting; retry within the startup budget.
            pass
        time.sleep(0.2)
    raise RuntimeError(run("logs", container))


try:
    for volume in (data_volume, snapshot_volume, archive_volume):
        run("volume", "create", volume)
    helper("chmod 777 /archive")
    run(
        "run",
        "-d",
        "--name",
        container,
        "--network",
        "none",
        "-e",
        "POSTGRES_HOST_AUTH_METHOD=trust",
        "-v",
        data_volume + ":/var/lib/postgresql/data",
        "-v",
        archive_volume + ":/archive",
        image,
        "postgres",
        "-c",
        "archive_mode=on",
        "-c",
        "archive_command=test ! -f /archive/%f && cp %p /archive/%f || cmp %p /archive/%f",
    )
    ready()
    sql("CREATE TABLE probe(value text); INSERT INTO probe VALUES('safe')")
    run("stop", container)
    helper("cp -a /data/. /snapshot/")
    run("start", container)
    ready()
    sql("INSERT INTO probe VALUES('must disappear'); SELECT pg_switch_wal()")
    time.sleep(1)
    run("stop", container)
    for i in range(3):
        helper("find /data -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +; cp -a /snapshot/. /data/")
        helper(recovery)
        run("start", container)
        ready()
        assert sql("SELECT string_agg(value,',') FROM probe") == "safe"
        timeline = sql("SELECT timeline_id FROM pg_control_checkpoint()")
        print("Recovered timeline", timeline, flush=True)
        assert int(timeline) == i + 2
        sql("INSERT INTO probe VALUES('must also disappear'); SELECT pg_switch_wal()")
        time.sleep(1)
        run("stop", container)
    print("PASS: exact cold snapshot, repeated promotion, preserved archive namespace")
except BaseException:
    subprocess.run(["docker", "logs", container], check=False)
    raise
finally:
    subprocess.run(["docker", "rm", "-f", container], stdout=subprocess.DEVNULL)
    for volume in (data_volume, snapshot_volume, archive_volume):
        subprocess.run(["docker", "volume", "rm", volume], stdout=subprocess.DEVNULL)
