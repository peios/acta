---
title: "Failure Modes"
description: >-
  How conflicts, expired credentials, uncertain responses,
  and recovery failures appear.
---

Acta distinguishes rejected work from work whose outcome is uncertain. The
recovery path depends on which boundary recorded the operation durably.

## Record conflicts

Task field edits compare individual versions. Memories, documents, releases,
profiles, and permissions compare their corresponding revisions. A conflict
preserves the stored record and returns its current state. The browser retains
editable drafts where supported. Retrying against a newer revision without
reconciliation can overwrite a valid intervening change.

Task creation is not idempotent. An uncertain response can mean that a task
was created. Comment creation instead binds a request UUID to the author and
exact logical post; repeating that identity and content retrieves the same
outcome. Document and migration operations have their own identity and
revision rules.

## Authentication and access loss

Expired or revoked credentials stop authorizing requests. Re-enabling an
account does not revive its former sessions. An unmet required-authenticator
policy restricts the account to enrollment, identity inspection, and sign-out.
MCP tool grants do not replace workspace authority. A missing tool can reflect
a connection grant or a cached client catalog, while a forbidden operation can
reflect the account's live permissions.

OAuth refresh tokens rotate. Reuse revokes the connection, so competing
refreshes or a lost refresh response can require authorization again. Browser
revocation invalidates the durable MCP connection and its tokens together.

## Installation failures

A changed applied migration, unknown migration, or wrong installation key
fails startup. An older executable can reject a schema produced by a newer
release; image rollback alone does not establish recovery.

Backup health distinguishes integrity, restored evidence, and archived-log
age. A failed drill protects retained backup chains and can consume additional
space. Update failure before commit can restore the stopped cold copy. After
commit, recovery finishes the chosen release instead of overwriting accepted
work with an earlier snapshot. A failed rollback retains maintenance mode and
its error.

The [recovery procedures](~acta/operations/backups) describe operator
preparation, verification, and cutover. Recovery targets, journals, keys, and
operator logs contain sensitive material and are not public diagnostic
artifacts.
