---
title: "Managing Permissions and Groups"
type: how-to
description: >-
  Review additive grants, shared requirements, and the authority needed to
  change access.
---

Acta combines direct and group grants. Removing one source does not remove
access still supplied by another source. Multi-factor authentication (MFA)
requirements combine separately from capabilities.

## Review effective access

1. Open the account's permission editor or its group memberships.
2. Inspect each grant's direct and inherited sources.
3. Check **Require MFA** separately from the granted capabilities.
4. Save the intended change and review the effective result.

Changing a grant requires both **Manage permissions** and that capability.
Only Superusers can change Superuser grants. An existing grant outside your
authority can remain unchanged. Acta rejects changes that would remove its
last active, non-pending Superuser.

## Use groups

Create and edit groups in **Site Settings → Groups**. Add a person through
their **Groups** section, and inspect membership from either page. Changing
group membership requires authority over the group's capabilities, even when
another source grants the person the same access.

The built-in Default group's name and grants can change, but its automatic
assignment role remains. New human accounts join it. Existing members can be
removed individually.

## Check consequences

Permission changes take effect on subsequent server operations. The browser
refreshes its access display on focus and relevant saves, so a visible control
can lag behind the server's decision.

Access changes invalidate outstanding credential links. Losing owner authority
also prunes corresponding explicit agent grants. Restoring owner access does
not restore those explicit delegations. Issue replacement links or delegate
again only after reviewing the intended access.
