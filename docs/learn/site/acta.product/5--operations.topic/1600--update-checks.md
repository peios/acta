---
title: "Checking Update Recovery"
type: how-to
description: >-
  Qualify signed updates, process interruption, failed migration recovery,
  and candidate-to-release upgrades.
---

Update qualification exercises signed inputs, installation, interruption, and
recovery. Local fixtures use disposable keys and installations.

`scripts/test-update-timeline.py` repeatedly restores the same cold PostgreSQL
snapshot while later candidate WAL exists in the archive. It checks exact data
restoration and distinct timelines using the updater's recovery settings.

Unit tests inject failures and process interruption at every durable boundary,
cover concurrent service ownership, stale requests, signed payload tampering,
repository isolation and schema compatibility. Integration tests enforce human
superuser authorization.

`scripts/test-update-compose.py` installs two verified release image sets into
a unique loopback-only Compose project with generated secrets, a local TLS CA
and a real encrypted backup repository. It verifies data preservation through
update, controller restart and a failing migration, including whole-stack
interruption. Failure images and fixture signatures use a separate disposable
signing key and are never GitHub releases. `KEEP_UPDATE_TEST=1` retains the
test installation for inspection; otherwise only its own containers and
volumes are removed.

The release workflow accepts a `previous_version` to run this real recovery
test before publishing the target release. Every stable release requires a
compatible predecessor. The sole bootstrap exception is `v0.1.0-rc.1`, the
first layout-2 candidate: `--bootstrap` verifies its production signature,
then uses a disposable test signature and lower sequence with the same image
set as its rehearsal baseline. This tests a fresh installation, update
execution and both recovery paths; it does not claim compatibility with a
previous release. Stable `v0.1.0` must subsequently pass a real
candidate-to-stable upgrade. Existing tags and release versions cannot be
overwritten through the workflow.

References: [Docker volumes](https://docs.docker.com/engine/storage/volumes/),
[Compose up](https://docs.docker.com/reference/cli/docker/compose/up/),
[GitHub container
publishing](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images).
