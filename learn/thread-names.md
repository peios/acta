# Thread names

Choose **Rename thread** from the thread header's three-dot menu. Enter a name
and press Enter or Rename; Cancel and Escape leave it unchanged. The header and
sidebar use this name. Threads without a name retain the working-directory
basename as their display title. Names are trimmed, single-line, and limited to
120 Unicode characters.

Renaming requires the thread's harness to be connected. The provider itself can
be stopped. Acta sends a durable `rename` hypercontrol command containing the
thread UUID, command UUID and `name`; the harness saves the name in its local
thread descriptor alongside the command result, then publishes it by discovery.
The UI shows the confirmed descriptor rather than assuming a successful write.

The command uses the existing owner-scoped command journal and replay rules.
Reusing an ID with a different name is rejected. Replaying a completed rename
does not undo later names. Descriptor revisions reject stale discovery updates.
Pending accepted work is delivered on reconnect; a lost connection does not
prove that a rename failed. Local persistence failure stops the controller from
advertising unpersisted state.

The name cascades into the native provider title: Codex uses `thread/name/set`
with its provider thread ID, and Claude uses the `rename_session` control request.
Running providers acknowledge the update before it is reported as synchronized.
Stopped providers retain `name_sync_pending`; their native title is applied when
resumed, without starting a session just to rename it. The thread shows this
pending state. Renaming never sends a model prompt or changes the working directory.

A provider rejection or uncertain acknowledgement still saves the requested Acta
name and shows the native synchronization error. Retry by renaming again, or
resume the provider. On resume, title restoration failure leaves the healthy
provider running and the synchronization pending, rather than killing the session.
The native rename requests have stable IDs within the existing pipe journal;
recovery can inspect the original acknowledgement without blindly resending.
Names survive harness restarts and provider resumption. Native title notifications
and rename acknowledgements are local debug frames, not conversation messages.
Native title changes made outside Acta are not imported into the Acta name.

The installed Codex protocol schema and Claude control protocol were verified
with isolated native sessions. Codex readback returned the requested title;
Claude's stored custom title was checked after process exit. Automated tests cover
both providers' running and deferred rename paths, restart and stale replay,
command ownership, stale discovery, and frontend command receipts.
