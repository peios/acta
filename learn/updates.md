# Deployment updates (ACT-106)

Acta's updater is a separate service. It has its own persistent journal, Unix
socket and credential, and continues when the application or its database is
unavailable. Only human superusers can use Site Settings → Updates. Check and
install are separate operations; automatic checks run every six hours and never
install a release. A disconnected browser must reconnect to inspect the durable
job, not infer failure and submit another installation.

## Release contract and trust

`acta-release.json` is an Ed25519-signed envelope. Its base64 payload contains the
version, monotonically increasing release sequence, repository, updater protocol,
deployment layout, PostgreSQL major version, supported starting schema, target
schema, notes, and exact image digests for app, db, backup, caddy and updater.
The verifier pins the operator-installed public key and configured repository.
Neither a GitHub response nor the application can replace that trust root.
Replayed/older releases, unsupported layouts/protocols, wrong namespaces and
unsupported schema transitions are rejected. PostgreSQL major upgrades are not
performed by this updater. Layout or updater-protocol changes require an explicit
operator deployment upgrade; normal compatible updater image changes are handled
by a detached replacement helper after application cutover is durably complete.

The initial release channel is **peios/acta2 only**, with separate
`ghcr.io/peios/acta2-{app,db,backup,updater}` packages. Publishing scripts and the
workflow explicitly refuse old Acta's repository. Workflow dispatch publishes
prereleases only. Repository selection is operator configuration; switching to
peios/acta requires an explicit cutover, release workflow/namespace changes, and
trust-root review, not just renaming a browser setting.

The repository workflow uses `ACTA_RELEASE_SIGNING_SEED` to sign the manifest.
The matching public key is `deploy/update/release.pub`. The private seed is never
shipped in an image. Maintain a secure independent backup of the signing seed;
rotate verification keys through a separately authenticated operator procedure.
The workflow builds Linux amd64 images initially. Do not install these on ARM.

GitHub packages for a private repository require registry authentication in the
operator's Docker config. Reading private release assets also requires a GitHub
credential with access to the repository; configure `github_token_file` in the
updater config. Keep both outside the checkout. Release credentials are sent only
to GitHub's API and stripped from redirected asset downloads.

## Provisioning

Provision the production deployment and the backup service first, as described
in [deployment](deployment.md) and [backups](backups.md). An update requires a
successful fresh full backup and restore drill; it never silently proceeds with
unconfigured recovery. Backup schedules may remain disabled if operated manually.

The deployment bundle paths are absolute and mounted at identical paths inside
the updater. Compose bind mounts are resolved by Docker on the host, not inside
the updater. Keep the deployment bundle and operator configuration private and
trusted: the updater has Docker administration authority. The web application has
only a token-protected Unix API for status, checking, installing a verified release
ID and continuing interrupted recovery. It cannot provide arbitrary paths, images,
Compose files or shell commands. No Docker socket is mounted into the application.

For a **new installation**, download a signed release and verify it against the
independently obtained public key. Provision configuration, then bootstrap the
journal with that release before starting services:

```sh
python3 scripts/configure-updates.py --deployment /srv/acta2-config \
  --bundle /srv/acta2 --state /srv/acta2-update \
  --public-key deploy/update/release.pub --repository peios/acta2 --prereleases

# Run the trusted updater executable from your verified release/build.
acta2-update -config /srv/acta2-config/update/config.json \
  -input /srv/release/acta-release.json bootstrap

docker compose --env-file /srv/acta2-config/.env \
  -f /srv/acta2/compose.production.yaml -f /srv/acta2/compose.backups.yaml \
  -f /srv/acta2/compose.updates.yaml -f /srv/acta2-update/active.json \
  --profile backups --profile updates up -d --no-build
```

Use all these files for subsequent operator commands. `active.json` is maintained
by the updater and selects the exact installed image set. Do not override images
by hand. Bootstrap refuses an existing journal and does not start containers or
change data. Existing locally built installations need an operator-controlled
first switch to a signed image set; the updater refuses image drift rather than
pretending an unrecorded deployment is recoverable.

`/srv/acta2-config/registry/config.json` holds the deployment's registry login.
The public key and optional release-token file must be accessible within the
configured directory. State files remain mode 0600. The state directory is
traversable and only the non-sensitive maintenance marker is readable by Caddy
and the application. The production Compose file without the updates overlay
remains usable for installations managed entirely by their operator.

## Durable installation and recovery

1. Persist the accepted job before replying. Permit only one active update.
2. Verify the installed images match the journal, download every target image by
   digest, then complete a fresh full backup and real restore drill.
3. Write a durable maintenance marker. Caddy returns 503 with Retry-After; stop
   application, backup worker and PostgreSQL. Existing connections close with the
   app. An ordinary app restart waits at this marker before accessing the database.
4. Copy the stopped database into a separate installation/job-labelled recovery
   volume. Reject non-owned or running database volumes and external tablespaces.
   An incomplete copy never becomes a restore source.
5. Start the candidate database and application in recovery mode. Apply migrations,
   check health and run the existing read-only recovery verification against the
   schema, security data, documents and embedded UI. External integrations remain
   disabled and the public maintenance gate remains closed.
6. Persist the **committing** phase before allowing normal execution. Bring up the
   final application, clear the gate, verify service health, and record success.
   A separate helper then reconciles the updater's own image, preserving its state.

Before committing, a failure or ambiguous candidate execution restores the cold
copy and previous images. Restored sessions are revoked and pending notifications
are discarded before enabling the old application. A failed rollback leaves the
installation in maintenance and records its error. After committing, recovery only
finishes the chosen release: it must never restore a snapshot over newly accepted
work. The same forward-only boundary applies when reopening a rolled-back release.

The cold recovery volume closes the write gap after the independently verified
backup. It contains sensitive database data, protected like the installation's
normal database volume; it is not off-machine disaster recovery. The updater retains the two most recent completed recovery copies by default;
configure `retain_recovery_copies` from 2 to 100. Before another update, it prunes
only older copies recorded in its terminal jobs and carrying matching ownership
labels. Copying requires the source size plus 10% and 512 MiB free headroom.
Monitor disk space: these copies are separate from normal backup retention.
Never delete the active job's volume or run broad Docker prune commands during an update.

The journal records errors and resumable phases even when the app cannot respond.
Inspect `docker compose logs updater` for private command diagnostics. The local
operator interface provides `status`, `check`, `install -release-id` (flags precede
the command), and `retry`. Retry reconciles the saved phase; it never creates a new
job or assumes that a timed-out command did not run.

## Verification

Unit tests inject failures and process interruption at every durable boundary,
cover concurrent service ownership, stale requests, signed payload tampering,
repository isolation and schema compatibility. Integration tests enforce human
superuser authorization.

`scripts/test-update-compose.py` installs two verified release image sets into a
unique loopback-only Compose project with generated secrets, a local TLS CA and a
real encrypted backup repository. It verifies data preservation through update,
controller restart and a failing migration, including whole-stack interruption.
Failure images and fixture signatures use a separate disposable signing key and
are never GitHub releases. `KEEP_UPDATE_TEST=1` retains the test installation for
inspection; otherwise only its own containers and volumes are removed.

The release workflow accepts a `previous_version` to run this real recovery test
before publishing the target release. A first bootstrap prerelease has no previous
release; every subsequent release should specify its predecessor.

References: [Docker volumes](https://docs.docker.com/engine/storage/volumes/),
[Compose up](https://docs.docker.com/reference/cli/docker/compose/up/),
[GitHub container publishing](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images).
