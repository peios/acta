---
title: "Securing Your Account"
type: how-to
description: >-
  Configure passkeys, authenticator codes, recovery codes, and session
  revocation.
---

Use **User Settings → Security** to control how your account signs in and
which connections retain access. Sensitive changes ask for fresh proof of your
identity.

## Add a passkey

1. In **Passkeys**, select **New passkey**.
2. Complete the requested authentication and enter a name for the passkey.
3. Complete the browser's credential prompt.
4. Check that the passkey appears in the list.

Canceling a browser prompt does not add an Acta credential. A passkey accepted
by your device but rejected by the server cannot sign in to Acta.

Passkey sign-in requires the device's user verification, such as its PIN or
biometric check. Keep another usable sign-in method before removing a passkey.

## Configure an authenticator

1. In **Authenticator**, start setup and complete authentication.
2. Add the displayed QR code or manual key to your authenticator app.
3. Enter a code from that app.
4. Save the recovery codes in a secure location and acknowledge that you saved
  them.

Acta activates the authenticator only after the last step. During replacement,
the previous authenticator and recovery codes remain valid until the
replacement is confirmed. Password sign-in then also requires an authenticator
or recovery code. A verified passkey needs an extra code only if you select
that preference.

A recovery code replaces an authenticator code once. It does not replace your
password or passkey. Generating a replacement set invalidates the previous set
when you acknowledge the replacement.

## Revoke a connection

1. In **Sessions**, locate the browser, command-line, or MCP connection.
2. Revoke that session, or choose the action that signs out all other
  sessions.
3. Check that the connection disappears from the active list.

Connections acting as an owned agent appear on that agent's detail page.
Revocation removes server-side access; keeping a copy of the old credential
does not restore it. Changing a password retains the initiating browser and
revokes other sessions.

If you have lost all usable credentials, contact an administrator who can
issue a recovery link. Acta has no self-service bypass for a sole
administrator who has lost every credential.
