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

- [Release Trust](~acta/operations/release-trust)
- [Provisioning the Updater](~acta/operations/updater-setup)
- [Update Execution and Recovery](~acta/operations/update-recovery)
- [Checking Update Recovery](~acta/operations/update-checks)
