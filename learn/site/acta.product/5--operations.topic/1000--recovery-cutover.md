---
title: "Cutting Over to Restored Data"
type: how-to
description: >-
  Prepare restored security state, stop competing writers, switch production
  traffic, and verify a fresh backup.
---

Cutover makes restored data writable production state. Stop the former
installation and prepare the restored security state before reopening access.

## Before you start

Stop all writers to the original installation and isolate the restored target.
First keep an independently verified backup of any surviving current
production state. Cutover deliberately requires an operator action outside the
browser.

## Prepare the target

Run preparation against the verified target:

```sh
acta-backup --config /etc/acta-backup/backup.json \
  --target /var/lib/acta-restores/review prepare-cutover
```

Preparation requires a verified, stopped target. It rechecks the restored
data, then transactionally revokes **all** restored browser, CLI and MCP
sessions, clears in-flight authentication/setup grants, and discards pending
push deliveries. Old notifications are marked read/resolved, retaining their
historical records. Users and harnesses must authenticate again. Passwords and
MFA enrollment remain; credentials legitimately changed after the backup have
rolled back, so apply any known post-backup account disables/password changes
before reopening access. Preparation is repeatable and records
`prepared.json`; it does not switch traffic.

## Switch the installation

Configure production PostgreSQL explicitly (including WAL archiving,
authentication, network bindings, and paths), preserving the recovered data
and matching key/release. Do not reuse the verification configuration as a
production configuration. Start Acta against that target with recovery mode
disabled, validate locally, then switch the service/proxy. Re-provision push
subscriptions after sign-in. Keep the former installation stopped to avoid two
writable copies. Take and drill a new full backup after cutover before
resuming ordinary retention.

## Check the result

Confirm that the former installation remains stopped, users authenticate
against the restored installation, and a new full backup passes its restore
drill.
