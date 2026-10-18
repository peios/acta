---
title: "Inviting and Recovering Users"
type: how-to
description: >-
  Create an account, issue its invitation, and recover or suspend access.
---

User administration preserves account identity while controlling credentials
and access. Invitation and recovery links grant sensitive operations to
whoever possesses them.

## Before you start

Use a human account with the relevant user-administration permissions. Only a
Superuser can issue credential links for an account that can manage
permissions or holds Superuser. Keep links out of public comments and logs.

## Invite a person

1. In **Site Settings → Users**, create a user with a username and optional
  display name.
2. Configure the account's permissions and group membership.
3. Generate a fresh invitation after changing access.
4. Send the invitation through the intended private channel.
5. Check that the account becomes active after the recipient redeems it.

Permission changes invalidate outstanding invitation and recovery links. A new
link also replaces the previous one. Invitations expire after 24 hours and are
single-use. Closing the invitation dialog does not delete the pending account.

New accounts join the built-in Default group. Creating them requires authority
over the capabilities that group grants as well as **Create users**.

## Recover credentials

Open the account and select **Reset password**. Choose whether recovery also
clears the authenticator or passkeys; both choices start off. Issuing the link
changes no credentials. Redemption applies the selected changes and revokes
existing sessions together.

Clearing authenticator credentials does not clear a requirement to use
multi-factor authentication. The person enrolls again after signing in.

## Suspend access

Disable the account from its management page. Acta revokes its sessions,
outstanding credential links, and owned-agent sessions while retaining work
and history. The last active Superuser is protected from removal.

Re-enabling the account does not revive sessions or links. A pending account
still needs a fresh invitation; an active account signs in again.
