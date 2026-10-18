---
title: "Connection and Authorization Guarantees"
description: >-
  Harness ownership, transport policy checks, liveness deadlines, and
  single-process routing.
---

This section describes connection and authorization guarantees in the Acta
implementation.

- The server holds a synchronized in-memory registry, scoped by human account.
  No database migration or durable registration is involved.
- `/api/harnesses/connect` requires CLI bearer authentication, rejects browser
  origins/cookie authority, and accepts the `acta-harness-v7` WebSocket
  subprotocol. The client advertises a bounded hostname before appearing in
  the list.
- `/api/harnesses` returns an owner-scoped snapshot; `/api/harnesses/live`
  publishes snapshots on changes. Browser streams require exact configured
  origins when an Origin header is present and use normal session cookies.
- Both live transports recheck current account/session policy every 15
  seconds. They send a WebSocket ping with a five-second response deadline.
  Lost transports and revoked/expired credentials are removed within
  approximately 20 seconds under normal server scheduling. Clean
  EOF/disconnect removes them immediately. Authorization is also rechecked
  before pushing list changes.
- A live connection counts as ongoing session activity under the existing CLI
  session policy. Absolute expiry, revocation, disablement, and required MFA
  remain enforced. Signing out of the CLI session ends harnesses using that
  session.
- Network failures retry automatically with jittered exponential backoff,
  capped at 30 seconds. A stable connection resets backoff. Terminal
  authentication, policy, or protocol failures stop the CLI; sign in again
  using the chosen profile where appropriate. Cancellation interrupts dialing,
  reading, and retry waits.
- Server shutdown cancels live connections through its base context.
  Connections and registry state are not persisted; clients advertise again
  after restart.
- These streams are exempt from the ordinary 15-second HTTP request context;
  handshake, authentication, reads/writes, and liveness have their own bounds.
- A single Acta server process owns the live registry. Multi-replica routing
  and shared presence are not implemented; do not load-balance these endpoints
  across unrelated server processes and expect one complete list.

Harness connections require WebSocket upgrades and long-lived proxy
connections. The development Vite proxy forwards WebSockets as well as
ordinary API requests.
