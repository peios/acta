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

- [Backup Coverage](~acta/backups-and-recovery/backup-coverage)
- [Provisioning Backups](~acta/backups-and-recovery/backup-setup)
- [Backup Policy and Health](~acta/backups-and-recovery/backup-policy)
- [Restoring an Installation](~acta/backups-and-recovery/restoring)
- [Cutting Over to Restored Data](~acta/backups-and-recovery/recovery-cutover)
- [Checking Backup Recovery](~acta/backups-and-recovery/recovery-checks)
