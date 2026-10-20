---
title: "Backup and Recovery"
type: concept
description: >-
  Protect PostgreSQL data, installation keys, and matching release
  executables.
---

The independent `acta-backup` service protects a PostgreSQL cluster with
pgBackRest backups and archived write-ahead logs (WAL). Its policy and job
state remain available when Acta or the source database is unavailable.
Browser controls require a human account with Manage backups; restoration and
traffic cutover remain operator actions.

## Choose a procedure

Use these pages for the relevant stage:

- [Backup Coverage](~acta/operations/backup-coverage)
- [Provisioning Backups](~acta/operations/backup-setup)
- [Backup Policy and Health](~acta/operations/backup-policy)
- [Restoring an Installation](~acta/operations/restoring)
- [Cutting Over to Restored Data](~acta/operations/recovery-cutover)
- [Checking Backup Recovery](~acta/operations/recovery-checks)
