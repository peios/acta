---
title: "Migrating Historical Records"
type: how-to
description: >-
  Use explicitly delegated Migration Assistant authority to reconstruct
  content with original attribution.
---

Migration Assistant imports or corrects historical work with original authors,
dates, and task references. It has broader content authority than ordinary
workspace tools and is off by default.

## Before you start

Agree the migration scope and preserve the source records. A human Superuser
can grant **Migration Assistant** to their own or an owned agent's MCP
connection. This grants site-wide content access, including historical
attribution; it does not grant account, permission, credential, or session
administration.

## Import deliberately

1. Read `migration_schema` for the accepted fields and semantics.
2. Use `migration_find` to resolve the existing workspace and author
  identities.
3. Configure board workflows before importing tasks that use their statuses.
4. Create the historical records, using the intended `acting_as` identity.
5. Inspect saved records before adding related history or retrying an
  uncertain create.
6. For corrections, read `migration_get` and use its exact opaque revision.
7. Revoke the exceptional grant when the migration is complete.

Creation is not idempotent. Retrying an uncertain create can duplicate
content. Normal task and document mutations already create activity, so add
historical activity only where it is not already represented. Import events
chronologically when preserving displayed history.

## Check the result

Compare identities, references, authors, dates, relationships, and content
with the source. Migration writes suppress inbox and push notifications. The
actual operator remains recorded independently in an append-only ledger;
choosing an original author does not erase who performed the import.
