---
title: "Deployment Updates"
type: concept
description: >-
  Understand the separate updater, signed releases, and durable recovery.
---

The independent updater verifies signed releases and records installation
progress in a durable journal. Human Superusers can check and install through
Site Settings. Automatic checks run every six hours and do not install a
release. A disconnected browser reconnects to inspect the saved job instead of
assuming the installation failed.

## Choose a procedure

Use these pages for the relevant stage:

- [Release Trust](~acta/updates/release-trust)
- [Provisioning the Updater](~acta/updates/updater-setup)
- [Update Execution and Recovery](~acta/updates/update-recovery)
- [Checking Update Recovery](~acta/updates/update-checks)

## Database retirement migration

Migration `043_retire_execution.sql` deletes stored execution conversations,
frames, commands, Code host registrations, and execution notifications. Task
records, agent accounts, MCP access, task notifications, and queued task push
deliveries remain. Startup applies this migration automatically.

Keep a verified backup with its matching executable before starting this
release against an older database. Recover retired execution history from
that backup in an isolated environment. An older executable cannot open the
upgraded schema.
