---
title: "Server Configuration"
description: >-
  Database, origin, key, proxy, and process configuration read by the
  server.
---

The executable reads installation configuration at startup. Development run
targets supply convenience defaults; the executable retains its own
validation.

The principal server settings are:

| Setting | Purpose |
| --- | --- |
| `ACTA_DATABASE_URL` | Required PostgreSQL connection URL. Use a dedicated database and role. Configure TLS appropriately for remote PostgreSQL. |
| `ACTA_TRUSTED_PROXIES` | Optional comma-separated IPs/CIDRs trusted to supply `X-Forwarded-For`; empty trusts no forwarding headers. Invalid entries and all-address networks refuse startup. |
| `ACTA_PUBLIC_URL` | Exact external origin, default `http://localhost:8081`. Use `https://acta.peios.org` for that deployment. Non-loopback HTTP is rejected. |
| `ACTA_DEV_ORIGIN` | Optional extra loopback origin for Vite; allowed only with a local HTTP installation. Omit in production. |
| `ACTA_SECURITY_KEY_FILE` | Private, persistent encryption key file; default `.local/security.key` relative to the working directory. Provisioned on first start. All instances sharing a database use the same key. |
| `-listen` | HTTP bind address, default `:8081`. Behind a local proxy, use `127.0.0.1:8081`. |

## Process and storage boundaries

The database pool permits at most eight connections per server. Bounded
password hashing, request sizes, and HTTP timeouts limit individual request
work. The server serves the static SvelteKit bundle; deployment does not run
Node.

Fingerprint assets use immutable caching. The HTML shell and API do not. The
compiled bootstrap has a hash-based content security policy, and the server
adds frame restrictions and other response headers. JavaScript is required.

The security key is private persistent state. Its fingerprint is bound to the
database, and a mismatched key fails startup. Loss of the key makes encrypted
account security data unreadable. The backup and recovery procedures preserve
both the database and the matching key.

The independent workers use `ACTA_BACKUP_SOCKET`, `ACTA_BACKUP_TOKEN_FILE`,
`ACTA_UPDATE_SOCKET`, and `ACTA_UPDATE_TOKEN_FILE` when configured. The web
process receives bounded private APIs rather than worker filesystem or Docker
authority. `ACTA_RECOVERY_MODE` suppresses external activity for isolated
recovery verification; it is not a replacement for network isolation.
