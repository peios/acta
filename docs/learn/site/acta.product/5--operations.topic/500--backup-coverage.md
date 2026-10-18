---
title: "Backup Coverage"
type: concept
description: >-
  Identify the database, keys, release executable, and external files needed
  for recovery.
---

A recoverable installation needs the database, its installation key, recovery
credentials, and the exact executable recorded with its backup.

Backups cover the complete PostgreSQL cluster, including tasks, comments,
memberships, memories, MCP grants, and document bytes and versions. Physical
backups cover every database in the cluster: use a dedicated
Acta cluster or explicitly account for other databases when planning recovery.

Each completed backup carries an age-encrypted recovery record: installation
security key, database identity, PostgreSQL major version/replay settings,
migration checksums, installation ID, public URL and exact Acta executable
digest. The matching executable must also be preserved in an operator-managed
release archive, separately replicated off the source machine. This executable
embeds the web bundle. Files stored outside Acta are not covered.

For the integrated Docker package, see [production
deployment](~acta/operations/recovery-service).
