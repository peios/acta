#!/usr/bin/env python3
"""Verify repeated cold-snapshot recovery forks timelines without replaying candidate WAL."""
import subprocess,uuid,time
from pathlib import Path
image="postgres:17@sha256:e38411452a464af89e5adadb8d223bf53b898d47d6ef918b2d58c08707350449"
source=(Path(__file__).resolve().parents[1]/"internal/update/postgres.go").read_text()
recovery=source.split("const prepareRecovery = `",1)[1].split("`",1)[0]
recovery=recovery.replace("pgbackrest --config=/var/lib/acta-backup/config/pgbackrest.conf --stanza=acta archive-get", "cp /archive/").replace('cp /archive/ "%f" "%p"', 'cp "/archive/%f" "%p"')
n='acta-timeline-'+uuid.uuid4().hex[:8];v=n+'-data';s=n+'-snapshot';a=n+'-archive'
def run(*args):return subprocess.check_output(['docker',*args],text=True).strip()
def sql(q):return run('exec','-u','postgres',n,'psql','-h','127.0.0.1','-At','-c',q)
def helper(script):return run('run','--rm','--network','none','-v',v+':/data','-v',s+':/snapshot','-v',a+':/archive','--entrypoint','sh',image,'-ec',script)
def ready():
 for _ in range(60):
  try:
   if sql('SELECT NOT pg_is_in_recovery()')=='t':return
  except subprocess.CalledProcessError:pass
  time.sleep(.2)
 raise RuntimeError(run('logs',n))
try:
 for vol in (v,s,a):run('volume','create',vol)
 helper('chmod 777 /archive')
 run('run','-d','--name',n,'--network','none','-e','POSTGRES_HOST_AUTH_METHOD=trust','-v',v+':/var/lib/postgresql/data','-v',a+':/archive',image,'postgres','-c','archive_mode=on','-c','archive_command=test ! -f /archive/%f && cp %p /archive/%f || cmp %p /archive/%f')
 ready();sql("CREATE TABLE probe(value text); INSERT INTO probe VALUES('safe')")
 run('stop',n);helper('cp -a /data/. /snapshot/')
 run('start',n);ready();sql("INSERT INTO probe VALUES('must disappear'); SELECT pg_switch_wal()")
 time.sleep(1);run('stop',n)
 for i in range(3):
  helper('find /data -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +; cp -a /snapshot/. /data/')
  helper(recovery)
  run('start',n);ready();assert sql('SELECT string_agg(value,\',\') FROM probe')=='safe'
  timeline=sql('SELECT timeline_id FROM pg_control_checkpoint()');print('Recovered timeline',timeline,flush=True)
  assert int(timeline)==i+2
  sql("INSERT INTO probe VALUES('must also disappear'); SELECT pg_switch_wal()")
  time.sleep(1);run('stop',n)
 print('PASS: exact cold snapshot, repeated promotion, preserved archive namespace')
except BaseException:
 subprocess.run(['docker','logs',n],check=False)
 raise
finally:
 subprocess.run(['docker','rm','-f',n],stdout=subprocess.DEVNULL)
 for vol in (v,s,a):subprocess.run(['docker','volume','rm',vol],stdout=subprocess.DEVNULL)
