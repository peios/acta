---
title: "Authentication"
description: >-
  Server-owned authentication flows, credential encryption, freshness,
  expiry, and account restrictions.
---

Acta authenticates browser, command-line, and agent connections through shared
account policy. Security state is encrypted with an installation key kept
separately from the database.

## HTTP and flow state

All endpoints retain exact-Origin, JSON-only mutation checks, no-store
responses, and existing cookie protections. The maximum request body is 64 KiB
to allow WebAuthn responses. No application session is issued while an
additional factor is pending.

| Endpoint | Behavior |
| --- | --- |
| `POST /api/login` | Username/password proof; returns a flow result, optionally requiring a code. |
| `GET /api/security` | Current account's safe passkey metadata, MFA settings, remaining code count, and sessions. |
| `POST /api/security/flow` | Begin `login`, `password`, `passkey_add`, `passkey_remove`, `mfa_setup`, `mfa_disable`, `mfa_policy`, or `recovery`. Optional `target` and `enabled` are fixed when the flow begins. |
| `POST /api/security/advance` | Submit `id`, `action`, and step-specific fields. The server validates the transition and current policy. |
| `POST /api/security/cancel` | Cancel the browser-bound flow by `id`. |
| `POST /api/security/revoke` | Revoke another session by public UUID `id`, or all others when empty. Always preserves the caller. |

A flow response includes its opaque `id`, `purpose` and authoritative `step`.
Only enrollment steps return a QR/manual secret, or newly prepared recovery
codes. A `done` sign-in response sets the actual application cookie; its
session token is never included in JSON. Unknown/stale/consumed flows return
409 `flow_expired`; validation returns 422 with field errors. The UI leaves
failed ceremonies retryable and refreshes settings after a modal closes,
including when a response was lost after a successful commit.

Flows expire after ten minutes and bind to a random HttpOnly browser cookie.
Settings flows additionally bind to the current authenticated session. Fresh
proof expires after five minutes and carries method, credential, MFA evidence
and account security revision. Reusing a recent second factor during an
explicit current-password check preserves its original freshness deadline.
Browsing never extends authentication freshness. Security mutations invalidate
previous proof and flows. Session issuance and changes serialize on the same
account.

Flow starts allow 40 attempts per resolved client IP (using the deployment
proxy trust policy) per five minutes; advances allow 30 per flow, password
checks 10 per account, and authenticator/recovery checks 10 per account in the
same window. Limits are shared in PostgreSQL and include successful attempts.
Existing password-sign-in limits also apply.

## Credentials and persistence

WebAuthn uses `go-webauthn/webauthn`. The RP ID is the configured public URL's
hostname, and allowed origins come from the validated server configuration.
Discoverable credentials and signed user verification are required. The server
checks challenge, origin, RP ID, credential ownership, signature, flags, and
counter behavior. Each credential ID has a unique owner across the site;
public metadata uses separate random IDs. Names are required, with the
existing 100-grapheme display-name validation. Accounts can hold up to 20
passkeys. Changing the hostname changes the relying party; plan the permanent
hostname before relying on enrolled passkeys.

Authenticator codes use six-digit SHA-1 TOTP with a 30-second period and one
adjacent step of clock tolerance. Accepted steps cannot be reused. Ten
recovery codes each contain 80 random bits, displayed in grouped base32.
Active recovery codes are stored as SHA-256 digests; comparison normalizes
case and hyphens. A code is consumed in the same transaction as its successful
flow transition. Recovery-code preparation does not replace the active set
until acknowledgment.

MFA secrets, passkey credential material, and short-lived flow payloads are
AES-256-GCM encrypted with distinct account/flow context. Prepared recovery
plaintext exists only in its encrypted, expiring flow until completion or
cancellation. Public views and account events never contain these secrets.
`ACTA_SECURITY_KEY_FILE` is a private 32-byte key encoded as base64,
provisioned once with mode 0600. The database stores a fingerprint and refuses
a different key. Preserve this file across restarts, upgrades, and restores;
every instance using the database requires the same key. Automatic key
rotation and lost-key recovery are not implemented.

`SecurityStore.WithSecurity` locks an active account before its
flows/sessions. Its callback and all writes commit together or roll back.
Storage transactions preserve this ordering, security-revision rechecks,
global credential ownership, single-use challenges and codes, and atomic
revocation. Expensive password hashing occurs outside the account lock, then
credential/revision checks repeat inside it. PostgreSQL owns SQL and
migrations; the authentication domain owns policy, flow transitions, and
encryption. No generic workflow engine was added.

## Account requirements

[Permissions](~acta/technical-reference/accounts/permissions) adds Require
MFA. Unenrolled accounts enter a restricted version of the shared setup flow
at `/mfa-required`; only identity, enrollment, and sign-out remain available.
Acknowledging recovery codes completes enrollment. Required accounts cannot
disable MFA, and recovery that clears MFA credentials retains the requirement.
Superusers obey it. Verified passkeys and the optional extra-code policy
retain their existing sign-in behavior.
