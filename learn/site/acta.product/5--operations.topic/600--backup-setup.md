---
title: "Provisioning Backups"
type: how-to
description: >-
  Provision the recovery worker, encrypted repository, release archive,
  keys, and PostgreSQL log archiving.
---

Provision the independent backup worker and its recovery material before
turning on schedules. A backup stored only on the source machine does not
protect against losing that machine.

## Before you start

The recovery suite exercises PostgreSQL 17, pgBackRest 2.59.1 and age.
Validate upgrades using that suite before changing deployed versions. The
verification-report parser deliberately rejects unexpected formats.

## Provision the worker

1. Run `./bazel run //:export`. Install `acta-backup` independently
  of the web executable. Provision pgBackRest and age on the database/recovery
  host.
2. Adapt `deploy/backup/backup.example.json`, `pgbackrest.example.conf`, and
  `acta-backup.service`. The service must have access to the PostgreSQL data
  directory for physical backups, local backup/restore directories, a database
  account able to inspect cluster settings and all Acta tables, and the
  repository. The example runs under PostgreSQL's OS account. Keep all
  deployment paths and credentials operator-owned; none are editable through
  the web API.
3. Provision an encrypted **off-machine** repository. A local path in the
  example is suitable only when that mount is independently durable. Configure
  transport and credentials using pgBackRest, including repository encryption.
  Disable pgBackRest auto-expiration (`expire-auto=n`), external expiration
  timers and unrelated backup schedulers for this stanza: Acta owns retention
  decisions.
4. Create an age key using `age-keygen -o recovery.age`, record its public
  recipient with `age-keygen -y recovery.age`, and keep an independently
  recoverable private copy. Keep the repository cipher password and access
  credentials outside the failed machine too. A backup without its recovery
  keys is not recoverable. `recovery_identity_file` is optional for
  backup-only workers; it is required for automated drills. This worker
  possesses decryption authority when enabled.
5. Provision private mode-0600 `database_url_file`, `security_key_file` (the
  actual Acta key, never a newly generated replacement) and `token_file` (at
  least 32 random characters). `state_dir` and `restore_root` must be private
  durable directories. Set up the socket parent with a dedicated shared group:
  the worker owns it; the web process can traverse it and access the 0660
  socket. If running under different users, each process gets its own 0600
  copy of the same API token.
6. Archive the deployed `acta-server` under
  `release_dir/<sha256>/acta-server`, immutable and executable. `release_file`
  is JSON with `id`, `sha256`, `public_url`, and a stable `installation`
  identifier. Replicate the release archive and recovery configuration
  independently, retaining artifacts for at least as long as any backup refers
  to them. Deployment must update the release record together with the server.
  A backup spanning a schema, key, or release change fails consistency
  verification; take a new full backup.
7. Configure PostgreSQL `archive_mode=on` and an `archive_command` using the
  provisioned pgBackRest stanza, for example `pgbackrest
  --config=/etc/acta-backup/pgbackrest.conf --stanza=acta archive-push %p`.
  `archive_mode` needs a PostgreSQL restart. Set `archive_timeout` within the
  chosen archive-age limit for continuous recovery. Run `stanza-create` and
  `check` with the same operator configuration before starting the service.
  Both snapshot and continuous modes need the WAL necessary to make physical
  backups consistent; continuous mode additionally monitors ongoing archive
  freshness.
8. Start the independent service. Set `ACTA_BACKUP_SOCKET` and
  `ACTA_BACKUP_TOKEN_FILE` on the web process, then restart the web process.
  Automatic backups start **disabled**. Select policy explicitly in Site
  Settings, take a full backup and complete a restore drill before relying on
  the deployment.

The systemd example must be adapted to actual mount/socket paths. PostgreSQL
and pgBackRest need access under the configured service account. Recovery
PostgreSQL uses a private Unix socket and no TCP listener, with preload
libraries and logical replication workers disabled. These controls do not
replace an OS/container egress boundary: allow access only to the backup
repository during drills, with no access to provider harnesses, mail, webhook
or push endpoints. Repository credentials and operator logs are sensitive; do
not expose them through Acta.

## Check the result

In Site Settings, inspect backup health, run a full backup, and complete a
restore drill. Check that the matching release and recovery keys are available
independently of the source machine.
