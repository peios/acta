---
title: "Security Flows"
description: >-
  Authentication proof, security-flow transitions, credential replacement,
  and session revocation.
---

Security operations use server-owned flows. The browser supplies proof and
requested inputs; the server determines the next step and commits only the
action bound to the flow.

## Authentication policy

Passkey sign-in requires signed user verification. Password sign-in and
passkey sign-in apply the account's authenticator policy as shown here:

| Account policy | Password proof | Verified passkey proof |
| --- | --- | --- |
| No authenticator | Completes authentication | Completes authentication |
| Authenticator with no extra passkey code | Also requires a code | Completes authentication |
| Authenticator with an extra passkey code | Also requires a code | Also requires a code |

A single-use recovery code can replace an authenticator code. It cannot
replace the password or passkey, turn off protection, or change the extra-code
policy.

Fresh proof lasts five minutes, is bound to the session and verified methods,
and is not extended by ordinary browsing. An incomplete sign-in with a pending
second factor grants no application session. Sensitive changes check the
policy in force before the change, including a change that would weaken that
policy.

## Passwords and passkeys

A password change checks the explicit current password and any additional
proof, updates the credential, and revokes every other session atomically. The
initiating browser remains signed in.

Passkey registration checks fresh proof, a supplied name, and the browser's
credential ceremony. Canceling before acceptance stores no usable Acta
credential. A device-side credential whose registration fails on the server
cannot authenticate. Adding a passkey retains sessions; removal revokes other
sessions after fresh proof. The account retains its password.

The optional passkey invitation follows a completed password sign-in. It does
not open a native credential prompt without selection. Dismissal or successful
creation is remembered for that account in that browser; canceling creation is
not success.

## Authenticators and recovery codes

Enrollment verifies a code from the proposed authenticator, presents ten
recovery codes, and activates protection only after acknowledgment.
Replacement keeps the previous authenticator and recovery codes active until
the replacement commits. Configuration and recovery-code replacement occur
together, with other sessions revoked in the same transaction.

Turning off the authenticator removes its secret and recovery codes, clears
the extra-passkey-code preference, and revokes other sessions.
Required-authenticator policy rejects this operation. Changing the extra-code
preference uses the existing policy's fresh-proof requirements and revokes
other sessions.

Recovery-code replacement prepares an encrypted pending set. Acknowledgment
replaces the active set; cancellation retains the old set. The UI shows how
many active codes remain without revealing their earlier plaintext.

## Sessions and restricted access

Session revocation checks the current account and session. Revoking other
sessions preserves the caller. Browser, CLI, and MCP sessions share account
revocation rules but use distinct credentials and authorization transports.

An unenrolled account subject to Require MFA has access only to identity
inspection, the required enrollment flow, and sign-out. Completing
recovery-code acknowledgment ends that restriction. Administrator recovery can
clear credentials without clearing the requirement; the next sign-in returns
to enrollment.
