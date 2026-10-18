# ACT-102: deployment-owned proxy trust

Acta now accepts an explicit `ACTA_TRUSTED_PROXIES` IP/CIDR list. Empty defaults
trust nothing; invalid entries and all-address networks reject startup. Every
existing client-IP authentication/security call uses the same resolver, including
setup, login, security flows, account links, device authorization and OAuth.

Only a trusted direct peer permits `X-Forwarded-For` resolution, from right to
left up to the first untrusted hop. Invalid, missing, excessive or entirely trusted
chains retain the direct peer. IPv4-mapped addresses normalize to IPv4. Other
forwarding headers cannot select the client IP, public origin or cookie policy.
There is no editable Site Settings surface.

Compose fixes Caddy at 172.30.50.2 and Acta at 172.30.50.3 on a configurable /29,
and trusts only Caddy's exact address. The initial isolated test caught dynamic
allocation taking the reserved proxy address; explicit addresses for both services
fix that startup-order collision. Deployment instructions document subnet conflicts
and the separate trust configuration needed if another proxy/CDN precedes Caddy.

Validation passed:

- Race-enabled config and HTTP unit tests: default/untrusted spoofing, trusted
  chains, attacker-controlled prefixes, repeated header lines, malformed data,
  size/hop bounds, IPv6 and mapped IPv4, invalid configuration.
- PostgreSQL integration: two clients behind one proxy have independent limits;
  changing forged prefixes or headers from an untrusted peer cannot evade limits.
- Go vet for config, HTTP API and server; changed Go files formatted.
- Fresh production image build and actual Caddy HTTPS Compose smoke test. Eleven
  requests with changing forged X-Forwarded-For values share one real-client
  bucket and reach the limit. Database inspection confirms neither the Caddy IP
  nor any forged address owns that bucket. A direct loopback request is separate.
- The stack also passes HTTPS/redirect/UI/OAuth origin, privileges, limits, private
  ports and persistent data/key/certificate checks across container recreation.
- Disposable containers and volumes removed. No publishing, VPS changes or
  restarts of existing development services.
