# Production Compose deployment

`compose.production.yaml` packages Caddy, the compiled Acta server/web UI and a
private PostgreSQL 17 cluster. `compose.yaml` remains development-only. The
production project has separate networks, volumes and local image names; it does
not publish anything or alter old Acta's release channel. The optional [deployment updater](updates.md) adds signed releases and in-app updates.
Base images are pinned by digest. Review and refresh those pins for security
updates; OS packages are resolved during the build, so retain the resulting exact
release images rather than assuming a later rebuild is byte-identical.
The new release uses `acta-server`, `acta-db`, `acta-backup` and `acta-updater`
container packages. Reuse of `acta-server` is approved; the old installation's
auto-updater is disabled.
Repository handover and release publication require separate approval.

## First installation

Use a Linux Docker host with Docker Compose 2.24.4 or newer. The runtime limits are
1 GiB for Acta, 2 GiB for PostgreSQL, 256 MiB for Caddy and optionally 2 GiB for
recovery. Leave additional RAM/disk for the OS, filesystem cache, image builds,
WAL and restores. These are ceilings, not a guarantee that every workload fits;
monitor and size them for the installation. Build off-host for larger deployments.

Choose a public hostname and certificate contact email. Point its DNS A/AAAA
records at the host; only publish working IPv6 records. Allow inbound TCP 80/443
and optionally UDP 443 for HTTP/3. PostgreSQL and Acta have no published ports.
Caddy's automatic HTTPS obtains/renews certificates and redirects HTTP to HTTPS.
Its `/data` and `/config` survive container replacement. DNS, certificate issuance
and renewal must also be verified on the real host before cutover.

Create private configuration outside the checkout. The helper refuses an existing
directory and never rotates an installation's keys or database passwords:

```sh
python3 scripts/configure-deployment.py \
  --domain tasks.example.com --email operator@example.com \
  --directory /srv/acta-config

docker compose --env-file /srv/acta-config/.env \
  -f compose.production.yaml up -d --build
```

The configuration directory is mode 0700 and files are 0600. Preserve it securely
and separately from the host, particularly `security-key`. Docker bind-backed
secrets retain their host ownership, so entrypoints make private runtime copies
before dropping privileges. The application runs as UID/GID 10001 with no effective
capabilities and a read-only root filesystem. It connects as a dedicated database
owner without superuser, role-creation or database-creation powers. Initial database
credentials are applied only to an empty volume. Changing their files alone does
not change PostgreSQL passwords.

Set `ACTA_PROJECT` once and retain it: it names the installation's volumes. Set
`ACTA_VERSION` to a unique local release label before producing release images;
`development` is only the initial local build default. `ACTA_INSTALLATION_ID` is a
stable generated identifier for recovery metadata. Use the same `.env` and Compose
files for every lifecycle command. Do not use `down --volumes` on an installation.

Read the initial setup code through the protected operator console:

```sh
docker compose --env-file /srv/acta-config/.env \
  -f compose.production.yaml logs app
```

Open the HTTPS site and create the owner account. Keep Docker/operator logs private;
setup codes and infrastructure diagnostics belong there. Logs rotate at 10 MiB,
three files per service. Services restart unless deliberately stopped. App startup
waits for PostgreSQL, and Caddy waits for the app's database-backed health check.
Docker marks unhealthy services; a health check alone does not restart a hung
process. Monitor public availability and service health independently.

## Optional recovery service in the same deployment

The `backups` profile runs the existing independent recovery worker. Its policy,
release archive, socket and restore targets use durable named volumes. It stays
alive during app/DB outages. The supplied package uses a **mounted repository**;
provision an independently durable/off-machine mount before enabling it. App and
Caddy have no access to the repository or restored databases. The backup container
has only the internal database network and no Internet route. A direct S3/SFTP
repository needs an explicitly restricted egress setup before using it here.

Follow [the recovery runbook](backups.md) for keys, retention, verification and
cutover semantics. Before enabling the profile:

1. Create `/srv/acta-config/backup` mode 0700. Copy
   `deploy/production/backup.example.json` as `backup.json` and
   `deploy/production/pgbackrest.example.conf` as `pgbackrest.conf`, both mode 0600.
2. Supply a random repository cipher password in `pgbackrest.conf`. Provision
   `recovery.age` privately in that directory and its public recipient in
   `backup.json`; retain independent recovery copies. The worker's UID 999 needs
   read/write access to the repository mount; give only that account access.
3. Write a private `backup/database-url` containing
   `postgres://postgres@/acta?host=/var/run/postgresql`. This uses the private shared
   PostgreSQL socket and local authentication, not a published administrator port.
   Only the database and recovery worker mount that socket.
4. Set `ACTA_BACKUP_REPOSITORY` in `.env` to the absolute provisioned mount path.
   The deployment refuses to create a missing mount directory. The worker copies
   operator files privately into its durable configuration area and archives the
   exact server executable on each start. Keep app and worker on the same image
   build/release; include both when updating, and take a new full backup afterward.
5. Start the worker first to prepare its config, then enable DB archiving using
   the additional Compose file. The worker starts with automatic schedules disabled:

```sh
docker compose --env-file /srv/acta-config/.env -f compose.production.yaml \
  --profile backups up -d --build backup

docker compose --env-file /srv/acta-config/.env -f compose.production.yaml \
  -f compose.backups.yaml --profile backups up -d --build

docker compose --env-file /srv/acta-config/.env -f compose.production.yaml \
  -f compose.backups.yaml --profile backups exec -u 999:10001 backup \
  pgbackrest --config=/var/lib/acta-backup/config/pgbackrest.conf --stanza=acta stanza-create

docker compose --env-file /srv/acta-config/.env -f compose.production.yaml \
  -f compose.backups.yaml --profile backups exec -u 999:10001 backup \
  pgbackrest --config=/var/lib/acta-backup/config/pgbackrest.conf --stanza=acta check
```

Retain both Compose files and the profile for subsequent starts. Monitor WAL disk
usage immediately: failed archiving retains WAL on the database disk. Configure
policy in Site Settings, take a full backup and pass a restore drill before relying
on it. Configure a reachable independent alert mechanism and external uptime
monitor. To run offline recovery, use `docker compose run --rm --no-deps backup`
with the worker's flags preceding `restore`; restore never starts app workers.
Replicate the release archive and recovery credentials independently along with
repository data. A local named volume alone is not disaster recovery.

## Remaining production gates

This package supplies HTTPS termination, persistence, private networking, service
limits and restart/log policies. The optional [updater](updates.md) handles signed
release installation and recovery; old-Acta migration remains a separate cutover.

Compose assigns Caddy `172.30.50.2` on the private `172.30.50.0/29` edge
network, assigns Acta `172.30.50.3`, and sets `ACTA_TRUSTED_PROXIES` to Caddy's exact IP. Both are fixed so startup order cannot allocate Caddy's address to Acta. Other container addresses,
the bridge gateway and direct callers are not trusted. If the subnet conflicts
with existing Docker/VPN/host routes, choose a free subnet with
`ACTA_PROXY_SUBNET` and set `ACTA_PROXY_IP` and `ACTA_APP_PROXY_IP` to distinct
usable hosts within it; change all three
in the deployment `.env` before recreating the stack. Do not attach untrusted
containers to the installation's networks. Containers keep the proxy identity
across restarts; DNS discovery is not used to grant trust.

Outside Compose, set `ACTA_TRUSTED_PROXIES` to comma-separated explicit IPs or
CIDRs belonging only to your reverse proxies. The default trusts none. Invalid
entries, hostnames and all-address networks refuse startup. Acta reads
`X-Forwarded-For` only from a trusted transport peer, walks the chain right to
left, and stops at the first untrusted address. Malformed chains, over 32 hops or
4 KiB, or chains without an untrusted client fall back to the direct peer.
IPv4-mapped addresses are normalized to avoid separate rate-limit identities.
`Forwarded` and `X-Real-IP` are ignored. Proxy headers never change the configured
public URL, allowed origins or secure-cookie policy.

The shipped Caddy configuration is the Internet-facing proxy and uses its default
sanitization of incoming forwarding headers. Adding a CDN or another proxy in
front requires explicitly configuring Caddy's own upstream trust policy too;
otherwise it correctly treats that intermediary as the client. This is operator
configuration, not an editable Site Settings page.

Before cutover, verify public DNS/TLS, OAuth/MCP URLs, passkeys, push and harness
reconnection on the real hostname; prove backup restoration and preserve the
matching security key; rehearse migration and rollback. A schema migration may
prevent an older binary from starting: image rollback alone is not recovery.

## Verification

Build the images and run `python3 scripts/test-production-compose.py`. It creates
a uniquely named stack with ephemeral loopback ports and a private local Caddy CA.
It checks HTTPS/redirects, the embedded UI, OAuth origin, process/database privilege,
private ports, limits, and persistence/key/certificate continuity after removing
and recreating containers. It removes only its own volumes afterward.
`KEEP_COMPOSE_TEST=1` retains the isolated stack and prints its config directory.
It never requests public certificates or contacts an existing Acta database.

Set `COMPOSE_TEST_BACKUPS=1` to also exercise the optional profile with generated
test keys, a disposable encrypted repository, WAL archiving, a full backup and a
real restore drill. It also checks that the worker remains reachable when the
source app and database are stopped. This local repository is only a test fixture.
