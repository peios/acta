---
title: "Deploying Acta"
type: how-to
description: >-
  Install the Caddy, Acta, and PostgreSQL services with private
  configuration and persistent volumes.
---

Acta's production Compose stack runs Caddy, the Go server with its embedded
web application, and a private PostgreSQL 17 cluster. Optional services
provide independent backups and signed updates. The development Compose file
is not a production configuration.

## Before you start

Use a Linux Docker host with Docker Compose 2.24.4 or newer. The runtime
limits are 1 GiB for Acta, 2 GiB for PostgreSQL, 256 MiB for Caddy and
optionally 2 GiB for recovery. Leave additional RAM/disk for the OS,
filesystem cache, image builds, WAL and restores. These are ceilings, not a
guarantee that every workload fits; monitor and size them for the
installation. Build off-host for larger deployments.

Choose a public hostname and certificate contact email. Point its DNS A/AAAA
records at the host; only publish working IPv6 records. Allow inbound TCP
80/443 and optionally UDP 443 for HTTP/3. PostgreSQL and Acta have no
published ports. Caddy's automatic HTTPS obtains/renews certificates and
redirects HTTP to HTTPS. Its `/data` and `/config` survive container
replacement. DNS, certificate issuance, and renewal must also be verified on
the real host before cutover.

Create private configuration outside the checkout. The helper refuses an
existing directory and never rotates an installation's keys or database
passwords:

```sh
python3 scripts/configure-deployment.py \
  --domain tasks.example.com --email operator@example.com \
  --directory /srv/acta-config

docker compose --env-file /srv/acta-config/.env \
  -f compose.production.yaml up -d --build
```

The configuration directory is mode 0700 and files are 0600. Preserve it
securely and separately from the host, particularly `security-key`. Docker
bind-backed secrets retain their host ownership, so entrypoints make private
runtime copies before dropping privileges. The application runs as UID/GID
10001 with no effective capabilities and a read-only root filesystem. It
connects as a dedicated database owner without superuser, role-creation or
database-creation powers. Initial database credentials are applied only to an
empty volume. Changing their files alone does not change PostgreSQL passwords.

Set `ACTA_PROJECT` once and retain it: it names the installation's volumes.
Set `ACTA_VERSION` to a unique local release label before producing release
images; `development` is only the initial local build default.
`ACTA_INSTALLATION_ID` is a stable generated identifier for recovery metadata.
Use the same `.env` and Compose files for every lifecycle command. Do not use
`down --volumes` on an installation.

Read the initial setup code through the protected operator console:

```sh
docker compose --env-file /srv/acta-config/.env \
  -f compose.production.yaml logs app
```

Open the HTTPS site and create the owner account. Keep Docker/operator logs
private; setup codes and infrastructure diagnostics belong there. Logs rotate
at 10 MiB, three files per service. Services restart unless deliberately
stopped. App startup waits for PostgreSQL, and Caddy waits for the app's
database-backed health check. Docker marks unhealthy services; a health check
alone does not restart a hung process. Monitor public availability and service
health independently.
