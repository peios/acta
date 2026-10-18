---
title: "Checking a Deployment"
type: how-to
description: >-
  Exercise an isolated Compose installation and distinguish local checks
  from real-host qualification.
---

The production Compose check exercises an isolated local installation. It does
not establish that a public hostname, external proxy, or real-device
authentication works.

Build the images and run `python3 scripts/test-production-compose.py`. It
creates a uniquely named stack with ephemeral loopback ports and a private
local Caddy CA. It checks HTTPS/redirects, the embedded UI, OAuth origin,
process/database privilege, private ports, limits, and
persistence/key/certificate continuity after removing and recreating
containers. It removes only its own volumes afterward. `KEEP_COMPOSE_TEST=1`
retains the isolated stack and prints its config directory. It never requests
public certificates or contacts an existing Acta database.

Set `COMPOSE_TEST_BACKUPS=1` to also exercise the optional profile with
generated test keys, a disposable encrypted repository, WAL archiving, a full
backup and a real restore drill. It also checks that the worker remains
reachable when the source app and database are stopped. This local repository
is only a test fixture.
