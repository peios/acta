---
title: "Update Execution and Recovery"
type: concept
description: >-
  Follow durable update phases, the commit boundary, cold-copy recovery, and
  interrupted-job reconciliation.
---

An accepted update has one durable job. Its commit boundary determines whether
recovery restores the old installation or finishes the selected release.

1. Persist the accepted job before replying. Permit only one active update.
2. Verify the installed images match the journal, download every target image
  by digest, then complete a fresh full backup and real restore drill.
3. Write a durable maintenance marker. Caddy returns 503 with Retry-After;
  stop application, backup worker, and PostgreSQL. Existing connections close
  with the app. An ordinary app restart waits at this marker before accessing
  the database.
4. Copy the stopped database into a separate installation/job-labeled recovery
  volume. Reject non-owned or running database volumes and external
  tablespaces. An incomplete copy never becomes a restore source.
5. Start the candidate database and application in recovery mode. Apply
  migrations, check health and run the existing read-only recovery
  verification against the schema, security data, documents and embedded UI.
  External integrations remain disabled and the public maintenance gate
  remains closed.
6. Persist the **committing** phase before allowing normal execution. Bring up
  the final application, clear the gate, verify service health, and record
  success. A separate helper then reconciles the updater's own image,
  preserving its state.

Before committing, a failure or ambiguous candidate execution restores the
cold copy and previous images. The snapshot must come from a cleanly stopped
database. Recovery forks a new PostgreSQL timeline using only the snapshot's
local WAL; it fetches archive history files solely to avoid reusing a timeline
identifier. Candidate WAL is never replayed. This uses PostgreSQL's [archive
recovery and timeline
selection](https://www.postgresql.org/docs/17/runtime-config-wal.html#RUNTIME-CONFIG-WAL-RECOVERY-TARGET)
behavior. The temporary recovery settings are removed before new backups, and
an archive check must pass before reopening. Restored sessions are revoked and
pending notifications are discarded before enabling the old application. A
failed rollback leaves the installation in maintenance and records its error.
After committing, recovery only finishes the chosen release: it must never
restore a snapshot over newly accepted work. The same forward-only boundary
applies when reopening a rolled-back release.

The cold recovery volume closes the write gap after the independently verified
backup. It contains sensitive database data, protected like the installation's
normal database volume; it is not off-machine disaster recovery. The updater
retains the two most recent completed recovery copies by default; configure
`retain_recovery_copies` from 2 to 100. Before another update, it prunes only
older copies recorded in its terminal jobs and carrying matching ownership
labels. Incomplete copies from terminal failed jobs are also removed, after
stopping their owned copy helper. Copying requires the source size plus 10%
and 512 MiB free headroom. Monitor disk space: these copies are separate from
normal backup retention. Never delete the active job's volume or run broad
Docker prune commands during an update.

The journal records errors and resumable phases even when the app cannot
respond. Inspect `docker compose logs updater` for private command
diagnostics. The local operator interface provides `status`, `check`,
`install -release-id` (flags precede the command), and `retry`. Retry
reconciles the saved phase; it never creates a new job or assumes that a
timed-out command did not run.
