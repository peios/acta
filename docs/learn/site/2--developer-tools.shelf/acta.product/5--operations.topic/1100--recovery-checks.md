---
title: "Checking Backup Recovery"
type: how-to
description: >-
  Run isolated snapshot, point-in-time, integrity, and cutover-preparation
  checks.
---

Use the isolated recovery suite to exercise backups, point-in-time recovery,
verification, and preparation for cutover. Keep its fixtures separate from
live data.

`python3 scripts/test-backup-recovery.py` builds a fresh disposable PostgreSQL
17 container with no network, an encrypted repository, real migrations and
seeded account/task/document/session/notification data. It tests snapshot
recovery, PITR excluding a later transaction, matching executable/key
verification, revoking restored sessions and alerts, existing-target refusal,
deliberate corruption and retention protection. It never connects to the
development or production DB. `KEEP_BACKUP_TEST=1` retains the container and
private scratch files for diagnosis. Also run
`go test -race ./internal/backup`, normal backend integration tests and
`node web/tests/backups.browser.mjs` (from `web`, use
`node tests/backups.browser.mjs`).

References: [pgBackRest commands](https://pgbackrest.org/command.html),
[PostgreSQL continuous
recovery](https://www.postgresql.org/docs/17/continuous-archiving.html).
