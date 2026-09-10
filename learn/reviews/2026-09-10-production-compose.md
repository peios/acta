# ACT-101: production Compose packaging verification

Scope: a local production package with Caddy, compiled app/frontend, PostgreSQL
and an opt-in package for the existing recovery worker. No repository push,
registry publication, VPS change, old-Acta modification, updater or proxy-trust
application change was performed.

## Evidence

- Built all three targets from source in Docker: `app`, `database`, `backup`.
  The first frontend build exposed a missing shared task-property catalogue in
  the build context; the Dockerfile now copies it before building Svelte.
- Pinned the tested Node 22, Go 1.26, Debian, PostgreSQL 17 and Caddy image digests.
- Docker Compose v5.4.0; packaged pgBackRest 2.59.1 and age 1.2.1.
- `python3 scripts/test-production-compose.py` passed the isolated stack test.
  `COMPOSE_TEST_BACKUPS=1` passed the same checks plus recovery integration.
- HTTPS served with a locally trusted test CA; HTTP redirected to the HTTPS origin.
  API, compiled HTML, service worker and OAuth issuer were served correctly.
- Acta ran as UID/GID 10001 with no effective capabilities; the application DB
  role was neither superuser nor allowed to create roles/databases. App and DB
  had no published ports and both had memory limits.
- Removing/recreating the test containers preserved a database probe, the
  encryption key fingerprint and Caddy CA identity through named volumes/secrets.
- Worker socket was reachable from the application UID using its own private
  token copy. PostgreSQL archived WAL to the isolated encrypted repository.
- The packaged worker completed an encrypted full backup and actual PostgreSQL
  restore drill using the archived compiled server executable.
- Worker status remained available after stopping both the app and source DB.
- The literal production Caddyfile validated with networking disabled; the smoke
  test used a local TLS issuer and never requested a public certificate.
- Test containers/networks/volumes were removed; development services were not
  restarted. Configuration secrets used for tests were newly generated.

## Operational boundaries

Public DNS/ACME issuance and renewal still require validation on the real host.
The mounted backup repository must be independently durable, keys and exact
release artifacts replicated, alert delivery configured and restore rehearsed
for the real installation. Docker marks unhealthy processes but does not restart
one merely because a health probe fails; independent monitoring is required.

Trusted-proxy client-IP handling remains a separate production gate. Today all
requests through Caddy share its IP-based rate-limit bucket. Accept forwarded
addresses only within a deliberately narrow proxy trust boundary.
