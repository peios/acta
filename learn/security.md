# Security implementation

The approved [Security design](security-design.md) is implemented at
`/user-settings/security`. Password changes, passkey enrollment/removal,
authenticator setup/replacement/disable, recovery-code replacement and the
extra-code preference use the same server-owned flow and modal components.
Login embeds the shared verification component. The password form lives in its
own modal; the page itself contains Password, Passkeys, Authenticator and Sessions.

## HTTP and flow state

All endpoints retain exact-Origin, JSON-only mutation checks, no-store responses
and existing cookie protections. The maximum request body is 64 KiB to allow
WebAuthn responses. No application session is issued while an additional factor
is pending.

| Endpoint | Behavior |
| --- | --- |
| `POST /api/login` | Username/password proof; returns a flow result, optionally requiring a code. |
| `GET /api/security` | Current account's safe passkey metadata, MFA settings, remaining code count and sessions. |
| `POST /api/security/flow` | Begin `login`, `password`, `passkey_add`, `passkey_remove`, `mfa_setup`, `mfa_disable`, `mfa_policy`, or `recovery`. Optional `target` and `enabled` are fixed when the flow begins. |
| `POST /api/security/advance` | Submit `id`, `action` and step-specific fields. The server validates the transition and current policy. |
| `POST /api/security/cancel` | Cancel the browser-bound flow by `id`. |
| `POST /api/security/revoke` | Revoke another session by public UUID `id`, or all others when empty. Always preserves the caller. |

A flow response includes its opaque `id`, `purpose` and authoritative `step`.
Only enrollment steps return a QR/manual secret, or newly prepared recovery
codes. A `done` login response sets the actual application cookie; its session
token is never included in JSON. Unknown/stale/consumed flows return 409
`flow_expired`; validation returns 422 with field errors. The UI leaves failed
ceremonies retryable and refreshes settings after a modal closes, including when
a response was lost after a successful commit.

Flows expire after ten minutes and bind to a random HttpOnly browser cookie.
Settings flows additionally bind to the current authenticated session. Fresh
proof expires after five minutes and carries method, credential, MFA evidence
and account security revision. Reusing a recent second factor during an explicit
current-password check preserves its original freshness deadline. Browsing
never extends authentication freshness. Security mutations invalidate previous
proof and flows. Session issuance and changes serialize on the same account.

Flow starts allow 40 attempts per resolved client IP (using the deployment proxy trust policy) per five minutes; advances
allow 30 per flow, password checks 10 per account, and authenticator/recovery
checks 10 per account in the same window. Limits are shared in PostgreSQL and
include successful attempts. Existing password-login limits also apply.

## Credentials and persistence

WebAuthn uses `go-webauthn/webauthn`. The RP ID is the configured public URL's
hostname, and allowed origins come from the validated server configuration.
Discoverable credentials and signed user verification are required. The server
checks challenge, origin, RP ID, credential ownership, signature, flags and
counter behavior. Each credential ID has a unique owner across the site; public
metadata uses separate random IDs. Names are required, with the existing
100-grapheme display-name validation. Accounts may hold up to 20 passkeys.
Changing the hostname changes the relying party; plan the permanent hostname
before relying on enrolled passkeys.

Authenticator codes use six-digit SHA-1 TOTP with a 30-second period and one
adjacent step of clock tolerance. Accepted steps cannot be reused. Ten recovery
codes each contain 80 random bits, displayed in grouped base32. Active recovery
codes are stored as SHA-256 digests; comparison normalizes case and hyphens.
A code is consumed in the same transaction as its successful flow transition.
Recovery-code preparation does not replace the active set until acknowledgement.

MFA secrets, passkey credential material and short-lived flow payloads are
AES-256-GCM encrypted with distinct account/flow context. Prepared recovery
plaintext exists only in its encrypted, expiring flow until completion or
cancellation. Public views and account events never contain these secrets.
`ACTA_SECURITY_KEY_FILE` is a private 32-byte key encoded as base64, provisioned
once with mode 0600. The database stores a fingerprint and refuses a different
key. Preserve this file across restarts, upgrades and restores; every instance
using the database requires the same key. Automatic key rotation and lost-key
recovery are not implemented.

`SecurityStore.WithSecurity` locks an active account before its flows/sessions.
Its callback and all writes commit together or roll back. Adapters must preserve
this ordering, security-revision rechecks, global credential ownership,
single-use challenges/codes and atomic required revocation. Expensive password
hashing occurs outside the account lock, then credential/revision checks repeat
inside it. PostgreSQL owns SQL and migrations; the authentication domain owns
policy, flow transitions and encryption. No generic workflow engine was added.

## Validation and remaining checks

`make check build` covers compilation, Svelte/TypeScript diagnostics, existing
frontend rule tests, Go unit tests, vet and formatting. The PostgreSQL race suite
uses isolated schemas and covers partial-login access denial, flow/browser/
session binding, recovery replay and double consumption, replacement atomicity,
competing security changes, and login issuance racing with revocation.

Signed protocol fixtures exercise WebAuthn registration, standalone login,
required user verification, wrong-origin rejection, challenge replay, passkey
removal and the extra-code policy. They do not prove hardware or browser behavior.
Browser checks on 8081 cover password changes/restoration, authenticator setup
and recovery acknowledgement, MFA password login with a recovery code, the
post-login passkey invitation, and desktop/mobile light/dark presentation.
Jack confirmed the native passkey prompt worked and cancelled it; the UI showed
a retry state. Jack subsequently confirmed all tested permutations, completing the real-device
validation of the Security slice.

Administrator-issued recovery and invitations are now implemented in
[User management](user-management.md). [Permissions](permissions.md) and
[Groups](groups.md) supply authorization policy. [CLI sessions](cli.md) share the
same session expiry and revocation rules. Dedicated API-key issuance and
sole-administrator total lockout recovery remain deferred.

### Reactive-state browser handoff

WebAuthn options are JSON from the API. The browser adapter in
`web/src/lib/passkeys.ts` copies their JSON representation before decoding the
challenge and credential/user IDs into binary buffers. This accepts Svelte's
reactive proxies without mutating flow state; native `structuredClone` rejects
those proxies. Regression tests exercise sign-in and registration with nested
proxies, assert that the credential API is reached with decoded options, and
verify the original flow is unchanged. These are browser-boundary unit tests,
not a claim of completed device authentication.

## Account requirements

[Permissions](permissions.md) adds Require MFA. Unenrolled accounts enter a
restricted version of the shared setup flow at `/mfa-required`; only identity,
enrolment and sign-out remain available. Acknowledging recovery codes completes
enrolment. Required accounts cannot disable MFA, and recovery that clears MFA
credentials retains the requirement. Superusers obey it. Verified passkeys and
the optional extra-code policy retain their existing sign-in behaviour.
