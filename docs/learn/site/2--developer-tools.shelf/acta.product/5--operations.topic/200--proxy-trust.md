---
title: "Proxy Trust and Deployment Boundaries"
type: concept
description: >-
  Configure client-address trust, fixed proxy addresses, and deployment
  checks without trusting arbitrary forwarding headers.
---

Acta uses an explicit proxy trust policy to resolve client addresses for rate
limiting. Proxy headers do not choose the public URL, allowed origins, or
cookie security.

This package supplies HTTPS termination, persistence, private networking,
service limits, and restart/log policies. The optional
[updater](~acta/operations/updates) handles signed release installation and
recovery; data migration remains an operator-controlled cutover.

Compose assigns Caddy `172.30.50.2` on the private `172.30.50.0/29` edge
network, assigns Acta `172.30.50.3`, and sets `ACTA_TRUSTED_PROXIES` to
Caddy's exact IP. Both are fixed so startup order cannot allocate Caddy's
address to Acta. Other container addresses, the bridge gateway, and direct
callers are not trusted. If the subnet conflicts with existing Docker/VPN/host
routes, choose a free subnet with `ACTA_PROXY_SUBNET` and set `ACTA_PROXY_IP`
and `ACTA_APP_PROXY_IP` to distinct usable hosts within it; change all three
in the deployment `.env` before recreating the stack. Do not attach untrusted
containers to the installation's networks. Containers keep the proxy identity
across restarts; DNS discovery is not used to grant trust.

Outside Compose, set `ACTA_TRUSTED_PROXIES` to comma-separated explicit IPs or
CIDRs belonging only to your reverse proxies. The default trusts none. Invalid
entries, hostnames, and all-address networks refuse startup. Acta reads
`X-Forwarded-For` only from a trusted transport peer, walks the chain right to
left, and stops at the first untrusted address. Malformed chains, over 32 hops
or 4 KiB, or chains without an untrusted client fall back to the direct peer.
IPv4-mapped addresses are normalized to avoid separate rate-limit identities.
`Forwarded` and `X-Real-IP` are ignored. Proxy headers never change the
configured public URL, allowed origins or secure-cookie policy.

The shipped Caddy configuration is the Internet-facing proxy and uses its
default sanitization of incoming forwarding headers. Adding a CDN or another
proxy in front requires explicitly configuring Caddy's own upstream trust
policy too; otherwise it correctly treats that intermediary as the client.
This is operator configuration, not an editable Site Settings page.

Before cutover, verify public DNS/TLS, OAuth/MCP URLs, passkeys, push and
CLI reconnection on the real hostname; prove backup restoration and
preserve the matching security key; rehearse migration and rollback. A schema
migration can prevent an older binary from starting: image rollback alone is
not recovery.
