# ACT-100 self-hosted recovery validation — 2026-09-10

Implemented the agreed independent pgBackRest service, Site Settings control and
operator-only restore/preparation workflow. Managed PostgreSQL integrations remain
explicitly deferred pending provider selection. Live deployment on port 8081 shows
the unconfigured state; no production repository or schedule was selected.

## Executed recovery tests

`python3 scripts/test-backup-recovery.py` passed from a new network-disabled Docker
container using PostgreSQL 17, pgBackRest 2.59.1 and age. The container was seeded
from every current migration with an encrypted account-security record, a task,
versioned document bytes, browser/CLI/MCP sessions, an OAuth token, a notification
and a pending push delivery. The test invoked the exact archived Acta executable's
read-only verifier, not merely `SELECT 1`.

- Catalog and full restore succeeded while source PostgreSQL was stopped and
  source-only credential/key/release paths were replaced with nonexistent paths.
- The snapshot restored the task and verified document size/content hash plus
  decrypted account security state using the recovered installation key.
- PITR retained probe transactions 1 and 2 and excluded transaction 3, committed
  after the requested recovery instant.
- Recovery refused a missing age identity before creating a target and refused
  an existing verified target.
- Production preparation revoked three session kinds and discarded the pending
  alert. Database assertions confirmed zero sessions, OAuth tokens, push
  subscriptions/deliveries or unresolved old notifications, with the task intact.
  A second preparation run made no additional revocations.
- Deliberate corruption of one encrypted repository file was detected and invalidated
  integrity/restore evidence; its original bytes were restored after the test.
- Retention protected the last restored full chain despite two newer full backups.
  After the newest full passed a drill, expiration retained exactly two full sets.
  The remaining newest set still restored successfully after expiration.

The corruption test exposed pgBackRest's exit-status behavior: `verify` may exit
zero while reporting invalid files. Acta now requires its verbose, explicitly valid
stanza/backup/WAL report. Unrecognized report shapes fail closed. Real recovery
also caught required PostgreSQL replay settings and recovery-target timestamp
format differences; both are exercised by the repeatable test.

## Other verification

- Backup unit tests with the race detector: schedule coalescing, revision conflicts,
  bounded retention policy, upload/integrity failures preventing expiration,
  durable queue/restart identity, one-worker locking, corrupt state rejection,
  failed state writes not acknowledging mutations, secret-free status, Unix API
  authentication and socket ownership.
- Streamed a synthetic catalog over 16 MiB with 1,000 recovery records, retaining
  the sealed payload only for the requested restore. Compact status retained full
  and restore evidence outside its most recent 500 records.
- PostgreSQL integration test: backup-management permission, MFA-required denial,
  unconfigured behavior and exclusion from the agent permission catalogue.
- Full Go test suite, Go vet, Svelte check and 31 frontend unit tests passed.
- Actual settings component browser fixture passed: unsaved edits survive refresh,
  revision conflicts preserve the draft, actions are disabled during queued jobs,
  operator retention bounds and desktop/mobile/unconfigured layouts.
- Live authenticated browser navigation confirmed Backups in Site Settings on 8081.

Global formatting checks also reported existing unrelated formatting in
`internal/integration/conversations_test.go` and `web/tests/documents.browser.mjs`;
these were not changed in this task. Changed backup files are formatted.

## Deployment boundaries

The test proves the self-hosted local encrypted-repository path, not a managed
provider or a particular production object-storage account. Before production,
provision and drill the actual offsite destination, preserve recovery keys and
matching release artifacts independently, configure/test external alert delivery
and service uptime monitoring, and apply the deployment-specific recovery egress
boundary. No production traffic switch is performed by the backup settings API.
See [the operational runbook](../backups.md).
