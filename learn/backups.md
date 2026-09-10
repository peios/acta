# Installation backup and recovery

Acta's self-hosted recovery service is `acta2-backup`. It uses pgBackRest for
PostgreSQL physical backups and archived transaction logs (WAL). It runs
independently of Acta and stores policy/jobs on its own durable filesystem.
Site Settings → Backups manages a bounded policy and shows recovery evidence.
Only human accounts with `site.backups.manage` can use it; this permission is
not delegable to agents. Restore and production cutover are operator CLI actions.
Managed PostgreSQL provider integration is deferred until a provider is chosen.

## What is protected

The complete PostgreSQL cluster, including tasks, comments, memberships, memories,
MCP grants, document bytes and versions, and Acta's stored thread history. Physical
backups cover every database in the cluster: use a dedicated Acta cluster or
explicitly account for other databases when planning recovery.

Each completed backup carries an age-encrypted recovery record: installation
security key, database identity, PostgreSQL major version/replay settings,
migration checksums, installation ID, public URL and exact Acta executable digest.
The matching executable must also be preserved in an operator-managed release
archive, separately replicated off the source machine. This executable embeds the
web bundle. Provider sessions, local source code and files on development machines
are not stored by the Acta server and are not covered.

For the integrated Docker package, see [production deployment](deployment.md#optional-recovery-service-in-the-same-deployment).

## Provisioning (Linux, PostgreSQL 17)

The real recovery suite currently exercises PostgreSQL 17, pgBackRest 2.59.1 and
age. Validate upgrades using that suite before changing deployed versions. The
verification-report parser deliberately rejects unexpected formats.

1. Build the web bundle and `make build`. Install `acta2-backup` independently of
   the web executable. Provision pgBackRest and age on the database/recovery host.
2. Adapt `deploy/backup/backup.example.json`, `pgbackrest.example.conf` and
   `acta2-backup.service`. The service must have access to the PostgreSQL data
   directory for physical backups, local backup/restore directories, a database
   account able to inspect cluster settings and all Acta tables, and the repository.
   The example runs under PostgreSQL's OS account. Keep all deployment paths and
   credentials operator-owned; none are editable through the web API.
3. Provision an encrypted **off-machine** repository. A local path in the example
   is suitable only when that mount is independently durable. Configure transport
   and credentials using pgBackRest, including repository encryption. Disable
   pgBackRest auto-expiration (`expire-auto=n`), external expiration timers and
   unrelated backup schedulers for this stanza: Acta owns retention decisions.
4. Create an age key using `age-keygen -o recovery.age`, record its public recipient
   with `age-keygen -y recovery.age`, and keep an independently recoverable private
   copy. Keep the repository cipher password and access credentials outside the
   failed machine too. A backup without its recovery keys is not recoverable.
   `recovery_identity_file` is optional for backup-only workers; it is required for
   automated drills. This worker possesses decryption authority when enabled.
5. Provision private mode-0600 `database_url_file`, `security_key_file` (the actual
   Acta key, never a newly generated replacement) and `token_file` (at least 32
   random characters). `state_dir` and `restore_root` must be private durable
   directories. Set up the socket parent with a dedicated shared group: the worker
   owns it; the web process can traverse it and access the 0660 socket. If running
   under different users, each process gets its own 0600 copy of the same API token.
6. Archive the deployed `acta2-server` under
   `release_dir/<sha256>/acta2-server`, immutable and executable. `release_file` is
   JSON with `id`, `sha256`, `public_url`, and a stable `installation` identifier.
   Replicate the release archive and recovery configuration independently, retaining
   artifacts for at least as long as any backup refers to them. Deployment must
   update the release record together with the server. A backup spanning a schema,
   key or release change fails consistency verification; take a new full backup.
7. Configure PostgreSQL `archive_mode=on` and an `archive_command` using the
   provisioned pgBackRest stanza, e.g.
   `pgbackrest --config=/etc/acta-backup/pgbackrest.conf --stanza=acta2 archive-push %p`.
   `archive_mode` needs a PostgreSQL restart. Set `archive_timeout` within the chosen
   archive-age limit for continuous recovery. Run `stanza-create` and `check` with
   the same operator configuration before starting the service. Both snapshot and
   continuous modes need the WAL necessary to make physical backups consistent;
   continuous mode additionally monitors ongoing archive freshness.
8. Start the independent service. Set `ACTA_BACKUP_SOCKET` and
   `ACTA_BACKUP_TOKEN_FILE` on the web process, then restart the web process.
   Automatic backups start **disabled**. Select policy explicitly in Site Settings,
   take a full backup and complete a restore drill before relying on the deployment.

The systemd example must be adapted to actual mount/socket paths. PostgreSQL and
pgBackRest need access under the configured service account. Recovery PostgreSQL
uses a private Unix socket and no TCP listener, with preload libraries and logical
replication workers disabled. These controls do not replace an OS/container egress
boundary: allow access only to the backup repository during drills, with no access
to provider harnesses, mail, webhook or push endpoints. Repository credentials and
operator logs are sensitive; do not expose them through Acta.

## Policy and health

Schedules are fixed intervals anchored to an explicit UTC timestamp, rather than
local wall-clock cron. Set the anchor to the desired first slot. Daylight-saving
changes do not shift that UTC cadence. Backup/full/drill intervals and freshness
alerts are configurable in minutes; missed slots coalesce into one job. Full
backups take precedence, then incrementals, then drills. Failures back off five
minutes. A saved job captures its policy and keeps the same UUID through crash
reconciliation. Jobs never overlap and policy changes are refused while busy.

Retention counts full chains, bounded by operator minimum/maximum (minimum two).
No expiration occurs before a full backup passes a real restore. The most recent
restored full chain stays protected until a newer full passes; unverified newer
fulls prevent cleanup. Extra retention therefore consumes space deliberately when
drills fail. WAL retention follows retained full chains. Monitor repository free
space independently. Switching destination changes future jobs, leaving previous
repositories untouched; the operator remains responsible for their retention.

A recovery point is complete only after the metadata consistency check; integrity
also requires a valid pgBackRest report, including every checked data/WAL file.
Upload success and exit status alone do not establish integrity. A drill starts
PostgreSQL from backup, validates cluster identity/schema/key and document hashes,
then invokes the **recorded release's** read-only verifier, including account
security-data decryption. The drill tests the newest integrity-checked full backup.
Standalone restores can target any set, including incrementals and explicit times.

Health distinguishes last integrity-checked backup, last successful restore, and
last archived-WAL evidence. It does not invent a guaranteed latest recoverable
instant from one archive timestamp. Repository errors, stale evidence, disabled
schedules, failed jobs and overdue drills are visible. Jobs keep the latest 100
records. The settings page retains the latest 500 recovery points plus older
full/restore evidence; the standalone `list` command provides the complete catalog.
Catalog inspection streams individual records, discarding unneeded encrypted
metadata rather than accumulating it in memory. Protected pgBackRest/operator logs provide lower-level diagnostics.

`alert_command` may name an operator-owned executable. On health changes it
receives JSON on stdin (`kind: acta.backup.health`, `warnings`, `at`); failed delivery
is visible and retried after five minutes. This path works without the Acta DB.
Configure it before production and test its delivery. Also use an external uptime
monitor for the backup service/host itself: no process can report its own death.
Health inspection runs between bounded jobs, so size job timeouts and the external
monitor's deadline for the installation. State write errors fail closed and cause
service restart; policy is never acknowledged before its atomic durable write.

## Recovery when Acta and the source database are gone

Run commands under the recovery PostgreSQL OS account. All flags precede the
command. Restore is offline with respect to the source database and the Acta web
service; only the repository, keys, matching release archive, PostgreSQL binaries
and adapted operator configuration are required. Keep target paths short and use
letters, digits, `/`, `.`, `_`, `-` only.

```sh
acta2-backup --config /etc/acta-backup/backup.json --destination offsite list
acta2-backup --config /etc/acta-backup/backup.json --destination offsite \
  --set EXACT_BACKUP_LABEL --identity /secure/recovery.age \
  --target /var/lib/acta-restores/review restore
```

For point-in-time recovery add `--time 2026-09-10T12:30:00Z` before `restore`. Choose
an appropriate backup before that time and preserve all required WAL. A missing
WAL segment or an unreachable target must fail recovery; never interpret it as
permission to recover to a different time. Snapshot restore stops at the selected
backup's earliest consistent point. Both paths refuse an existing target, even an
empty directory. Failed targets remain for diagnosis; do not overwrite them.

Successful recovery leaves stopped PostgreSQL data, a private recovered
`security.key`, encrypted-source metadata now decrypted in `acta-recovery.json`,
and `verified.json` evidence. Inspect row counts, document verification, release
identity and requested time. Protect the entire target as secret data. Verification
runs no application workers, network listeners or migrations. For a separate UI
review on an isolated host, start the recorded executable with
`ACTA_RECOVERY_MODE=true`, the recovered database and key; that disables push,
harness/MCP/OAuth entry points, backup controls and thread mutations. This is not a
replacement for network isolation and is not a production setting.

## Prepare and cut over

Stop all writers to the original installation and isolate the restored target.
First keep an independently verified backup of any surviving current production
state. Cutover deliberately requires an operator action outside the browser.

```sh
acta2-backup --config /etc/acta-backup/backup.json \
  --target /var/lib/acta-restores/review prepare-cutover
```

Preparation requires a verified, stopped target. It rechecks the restored data,
then transactionally revokes **all** restored browser, CLI and MCP sessions,
clears in-flight authentication/setup grants, and discards pending push deliveries.
Old notifications are marked read/resolved, retaining their historical records.
Users and harnesses must authenticate again. Passwords and MFA enrollment remain;
credentials legitimately changed after the backup have rolled back, so apply any
known post-backup account disables/password changes before reopening access.
Preparation is repeatable and records `prepared.json`; it does not switch traffic.

Configure production PostgreSQL explicitly (including WAL archiving, authentication,
network bindings and paths), preserving the recovered data and matching key/release.
Do not reuse the verification configuration as a production configuration. Start
Acta against that target with recovery mode disabled, validate locally, then switch
the service/proxy. Re-provision push subscriptions after login. Keep the former
installation stopped to avoid two writable copies. Take and drill a new full
backup after cutover before resuming ordinary retention.

## Repeatable verification

`python3 scripts/test-backup-recovery.py` builds a fresh disposable PostgreSQL 17
container with no network, an encrypted repository, real migrations and seeded
account/task/document/session/notification data. It tests snapshot recovery,
PITR excluding a later transaction, matching executable/key verification, revoking
restored sessions and alerts, existing-target refusal, deliberate corruption and
retention protection. It never connects to the development or production DB.
`KEEP_BACKUP_TEST=1` retains the container and private scratch files for diagnosis.
Also run `go test -race ./internal/backup`, normal backend integration tests and
`node web/tests/backups.browser.mjs` (from `web`, use `node tests/backups.browser.mjs`).

References: [pgBackRest commands](https://pgbackrest.org/command.html),
[PostgreSQL continuous recovery](https://www.postgresql.org/docs/17/continuous-archiving.html).
