---
title: "Adding the Recovery Service"
type: how-to
description: >-
  Add the independent backup worker to the production Compose deployment and
  verify its repository access.
---

The Compose recovery service keeps backup policy, jobs, release archives, and
restore targets outside the application process. Provision its repository
independently before relying on it.

The `backups` profile runs the existing independent recovery worker. Its
policy, release archive, socket, and restore targets use durable named
volumes. It stays alive during app/DB outages. The supplied package uses a
**mounted repository**; provision an independently durable/off-machine mount
before enabling it. App and Caddy have no access to the repository or restored
databases. The backup container has only the internal database network and no
Internet route. A direct S3/SFTP repository needs an explicitly restricted
egress setup before using it here.

Follow [the recovery runbook](~acta/backups-and-recovery/backups) for keys, retention,
verification and cutover semantics. Before enabling the profile:

1. Create `/srv/acta-config/backup` mode 0700. Copy
  `deploy/production/backup.example.json` as `backup.json` and
  `deploy/production/pgbackrest.example.conf` as `pgbackrest.conf`, both mode
  0600.
2. Supply a random repository cipher password in `pgbackrest.conf`. Provision
  `recovery.age` privately in that directory and its public recipient in
  `backup.json`; retain independent recovery copies. The worker's UID 999
  needs read/write access to the repository mount; give only that account
  access.
3. Write a private `backup/database-url` containing
  `postgres://postgres@/acta?host=/var/run/postgresql`. This uses the private
  shared PostgreSQL socket and local authentication, not a published
  administrator port. Only the database and recovery worker mount that socket.
4. Set `ACTA_BACKUP_REPOSITORY` in `.env` to the absolute provisioned mount
  path. The deployment refuses to create a missing mount directory. The worker
  copies operator files privately into its durable configuration area and
  archives the exact server executable on each start. Keep app and worker on
  the same image build/release; include both when updating, and take a new
  full backup afterward.
5. Start the worker first to prepare its config, then enable DB archiving
  using the additional Compose file. The worker starts with automatic
  schedules disabled. Its image copies `acta-backup` from `bin/`, which
  `tools/export` writes:

```sh
docker compose --env-file /srv/acta-config/.env -f compose.production.yaml \
  --profile backups up -d --build backup

docker compose --env-file /srv/acta-config/.env -f compose.production.yaml \
-f compose.backups.yaml --profile backups up -d --build

docker compose --env-file /srv/acta-config/.env -f compose.production.yaml \
-f compose.backups.yaml --profile backups exec -u 999:10001 backup \
pgbackrest --config=/var/lib/acta-backup/config/pgbackrest.conf --stanza=acta
stanza-create

docker compose --env-file /srv/acta-config/.env -f compose.production.yaml \
  -f compose.backups.yaml --profile backups exec -u 999:10001 backup \
  pgbackrest --config=/var/lib/acta-backup/config/pgbackrest.conf --stanza=acta check
```

Retain both Compose files and the profile for subsequent starts. Monitor WAL
disk usage immediately: failed archiving retains WAL on the database disk.
Configure policy in Site Settings, take a full backup and pass a restore drill
before relying on it. Configure a reachable independent alert mechanism and
external uptime monitor. To run offline recovery, use
`docker compose run --rm --no-deps backup` with the worker's flags preceding
`restore`; restore never starts app workers. Replicate the release archive and
recovery credentials independently along with repository data. A local named
volume alone is not disaster recovery.
