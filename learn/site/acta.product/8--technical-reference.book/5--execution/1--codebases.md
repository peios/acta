---
title: "Codebases"
description: >-
  Host-owned root catalogs, bounded directory listings, request identities,
  and filesystem containment.
---

A codebase is a named collection of existing folders on one private Code host.
It can contain many repositories, but Acta does not inspect Git. The host owns
the catalog; Acta relays requests without persisting paths or listings.

## Use

The Code surface selects a private Code host and registers a named codebase
with absolute root paths on that host. Registration does not move files,
create directories, or initialize repositories.

**Files** shows each root separately and loads directories when expanded.
Directories sort first, followed by names within each group. Hidden entries
are included. Symbolic links are labeled and not expandable in the UI. File
selection only highlights its name; there is no file-content request or
viewer. The central editor remains sample content. Agents supports [Claude
Code and Codex threads and CAT
output](~acta/technical-reference/execution/code-threads/overview) in the
selected codebase.

**Refresh directory explorer** reloads the catalog and tree. There is no file
watcher. Errors, empty folders, and unavailable hosts are shown explicitly.
Each directory returns at most 2,000 entries with a truncation notice; larger
folders are not paginated. Host/codebase changes discard old tree state and
pending reads. Reconnects refresh the catalog and explorer.

## Persistence and boundaries

The catalog is versioned JSON beside the host identity at
`<Acta config>/code-host/<server-and-account-hash>/codebases.json`. It stores
codebase IDs, names, creation times, and root IDs with canonical absolute
paths. The host process lock excludes other writers. Additions are serialized
and saved with atomic replacement; a failed save retains the prior catalog.
Source directories receive no metadata files. Host and server restarts retain
IDs.

An add request supplies a fresh codebase UUID. Retrying it with the same name
and canonical paths returns the existing codebase/root IDs; different values
conflict. The dialog retains its request ID across retries. The relay never
automatically replays mutations after a disconnect. Closing and reopening the
dialog starts a new registration.

Registration accepts existing accessible root directories. Relative paths,
duplicate canonical roots, and invalid IDs are rejected. Limits are 16 roots
per codebase, 256 codebases per host, and 512 KiB of catalog JSON. Directory
requests identify a registered codebase/root and a relative path. Go's
`os.Root` contains traversal and symlinks beneath that root; listing opens a
directory and returns names/types only. Roots use their current filesystem
location, not a filesystem snapshot.

## Operations

The [private host channel](~acta/technical-reference/execution/code-host)
supports:

- `codebases.list` with `{}` → `{codebases: [...]}`.
- `codebases.add` with `{id, name, paths}` → the registered codebase.
- `directories.list` with `{codebase_id, root_id, path}` → `{entries: [{name,
  kind}], truncated}`.

The server accepts operations only when caller and native host authenticate as
the same human. Host ownership is never supplied by the caller, and site
superusers have no bypass. There are no Git operations, file-content
reads/writes, or agent execution methods.
