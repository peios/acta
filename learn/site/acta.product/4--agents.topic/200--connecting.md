---
title: "Connecting a Development Machine"
type: how-to
description: >-
  Start a harness, inspect provider discovery, and create a thread without
  sending an initial turn.
---

Connect a development machine to run provider conversations from Acta.
Provider programs, credentials, working directories, and execution remain on
that machine.

## Before you start

Install and authenticate the provider locally. Build or install the Acta
client, and authorize a profile as your human account. Agent sessions cannot
run a harness. The Acta server and client need compatible harness protocols.

## Connect and start a thread

1. Run `acta harness` using the intended active profile, or `acta --profile
  work harness` for an existing profile named `work`.
2. In Acta, open **User Settings → Harnesses** and inspect the connection.
3. Check the provider's installation and sign-in status.
4. In **My Agents**, select **New thread**, choose the connection and
  provider, and enter an absolute working directory on that machine.
5. Check the thread's connection and settings before sending a message.

Creation opens an empty thread. Only an explicit message submission starts its
first model turn. Discovery probes do not run a model turn either. A reported
sign-in state is the local provider's report; it does not verify entitlement,
network access, or remaining usage.

## Restart during development

`acta harness --separate-pipe` runs the generic process pipe in a separate
helper. Provider processes and raw capture can survive restarting the harness
supervisor. Do not run normal and separate-pipe harnesses against the same
local state at once.

Replacing the helper executable does not update an already running helper.
Stop its providers and terminate the helper before upgrading that process.
Resume stored sessions afterward. Keep the private local state directory
intact.

## Check the result

The connection appears only while it is live. A thread becomes reachable when
an authorized connection advertises it. If authentication fails, sign in again
and restart the harness; repeated network retries cannot repair a revoked
credential.
