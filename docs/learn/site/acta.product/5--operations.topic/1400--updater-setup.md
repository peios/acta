---
title: "Provisioning the Updater"
type: how-to
description: >-
  Configure the separate updater and bootstrap its journal from a verified
  release.
---

Provision the updater only after the production stack and recovery service are
configured. It needs a successful fresh backup and restore drill before
installing a release.

Provision the production deployment and the backup service first, as described
in [deployment](~acta/operations/deployment) and
[backups](~acta/operations/backups). An update requires a successful fresh
full backup and restore drill; it never silently proceeds with unconfigured
recovery. Backup schedules can remain disabled if operated manually.

The deployment bundle paths are absolute and mounted at identical paths inside
the updater. Compose bind mounts are resolved by Docker on the host, not
inside the updater. Keep the deployment bundle and operator configuration
private and trusted: the updater has Docker administration authority. The web
application has only a token-protected Unix API for status, checking,
installing a verified release ID and continuing interrupted recovery. It
cannot provide arbitrary paths, images, Compose files, or shell commands. No
Docker socket is mounted into the application.

For a **new installation**, download a signed release and verify it against
the independently obtained public key. Provision configuration, then bootstrap
the journal with that release before starting services:

```sh
python3 scripts/configure-updates.py --deployment /srv/acta-config \
  --bundle /srv/acta --state /srv/acta-update \
  --public-key deploy/update/release.pub --repository peios/acta --prereleases

# Run the trusted updater executable from your verified release/build.
acta-update -config /srv/acta-config/update/config.json \
  -input /srv/release/acta-release.json bootstrap

docker compose --env-file /srv/acta-config/.env \
  -f /srv/acta/compose.production.yaml -f /srv/acta/compose.backups.yaml \
  -f /srv/acta/compose.updates.yaml -f /srv/acta-update/active.json \
  --profile backups --profile updates up -d --no-build
```

Use all these files for subsequent operator commands. Run operator Compose
commands with root privileges (for example, `sudo docker compose`): the
updater keeps its journal and generated overlay private to root. `active.json`
is maintained by the updater and selects the exact installed image set. Do not
override images by hand. Bootstrap refuses an existing journal and does not
start containers or change data. Existing locally built installations need an
operator-controlled first switch to a signed image set; the updater refuses
image drift rather than pretending an unrecorded deployment is recoverable.

`/srv/acta-config/registry/config.json` holds the deployment's registry
sign-in. The public key and optional release-token file must be accessible
within the configured directory. State files remain mode 0600. The state
directory is traversable and only the non-sensitive maintenance marker is
readable by Caddy and the application. The production Compose file without the
updates overlay remains usable for installations managed entirely by their
operator.
