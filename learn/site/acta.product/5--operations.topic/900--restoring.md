---
title: "Restoring an Installation"
type: how-to
description: >-
  Recover a backup or point in time into a private target and verify it
  without starting application workers.
---

Restore into a separate private target when the source service or database is
unavailable. Verification does not start application workers or switch
production traffic.

## Before you start

Run commands under the recovery PostgreSQL OS account. All flags precede the
command. Restore is offline with respect to the source database and the Acta
web service; only the repository, keys, matching release archive, PostgreSQL
binaries, and adapted operator configuration are required. Keep target paths
short and use letters, digits, `/`, `.`, `_`, `-` only.

## Restore a recovery point

The commands list available points and restore the selected backup:

```sh
acta-backup --config /etc/acta-backup/backup.json --destination offsite list
acta-backup --config /etc/acta-backup/backup.json --destination offsite \
  --set "<backup-label>" --identity /secure/recovery.age \
  --target /var/lib/acta-restores/review restore
```

Replace `<backup-label>` with a label from the catalog.

For point-in-time recovery add `--time 2026-09-10T12:30:00Z` before `restore`.
Choose an appropriate backup before that time and preserve all required WAL. A
missing WAL segment or an unreachable target must fail recovery; never
interpret it as permission to recover to a different time. Snapshot restore
stops at the selected backup's earliest consistent point. Both paths refuse an
existing target, even an empty directory. Failed targets remain for diagnosis;
do not overwrite them.

## Check the result

Successful recovery leaves stopped PostgreSQL data, a private recovered
`security.key`, encrypted-source metadata decrypted in `acta-recovery.json`,
and `verified.json` evidence. Inspect row counts, document verification,
release identity, and requested time. Protect the entire target as secret
data. Verification runs no application workers, network listeners, or
migrations. For a separate UI review on an isolated host, start the recorded
executable with `ACTA_RECOVERY_MODE=true`, the recovered database and key;
that disables push, harness/MCP/OAuth entry points, backup controls, and
thread mutations. This is not a replacement for network isolation and is not a
production setting.
