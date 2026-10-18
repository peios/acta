# Codebases and directory explorer

A codebase is a named collection of existing folders on one private Code host.
It can contain many repositories, but this slice does not inspect Git. The host
owns the catalogue; Acta relays requests without persisting paths or listings.

## Use

Start `acta-code-host` with your [Acta profile](cli.md), select it in Code, then
use **+** beside the codebase selector. Enter a name and absolute folder paths on
that host, one per line. Registration does not move files, create directories or
initialise repositories. For example, `peios` can use `/home/jack/projects/peios`
as one root containing all its projects.

**Files** shows each root separately and loads directories when expanded.
Directories sort first, followed by names within each group. Hidden entries are
included. Symbolic links are labelled and not expandable in the UI. File
selection only highlights its name; there is no file-content request or viewer.
The central editor remains sample content. Agents now supports
[Claude Code and Codex threads and CAT output](code-threads.md) in the selected codebase.

**Refresh directory explorer** reloads the catalogue and tree. There is no file
watcher yet. Errors, empty folders and unavailable hosts are shown explicitly.
Each directory returns at most 2,000 entries with a truncation notice; larger
folders are not paginated yet. Host/codebase changes discard old tree state and
pending reads. Reconnects refresh the catalogue and explorer.

## Persistence and boundaries

The catalogue is versioned JSON beside the host identity at
`<Acta config>/code-host/<server-and-account-hash>/codebases.json`. It stores
codebase IDs, names, creation times, and root IDs with canonical absolute paths.
The host process lock excludes other writers. Additions are serialized and saved
with atomic replacement; a failed save retains the prior catalogue. Source
directories receive no metadata files. Host and server restarts retain IDs.

An add request supplies a fresh codebase UUID. Retrying it with the same name and
canonical paths returns the existing codebase/root IDs; different values conflict.
The dialog retains its request ID across retries. The relay never automatically
replays mutations after a disconnect. Closing and reopening the dialog starts a
new registration.

Roots must be existing accessible directories. Relative paths, duplicate
canonical roots and invalid IDs are rejected. Limits are 16 roots per codebase,
256 codebases per host, and 512 KiB of catalogue JSON. Directory requests identify
a registered codebase/root and a relative path. Go's `os.Root` contains traversal
and symlinks beneath that root; listing opens a directory and returns names/types
only. Roots use their current filesystem location, not a filesystem snapshot.

## Operations

The [private host channel](code-host.md) supports:

- `codebases.list` with `{}` → `{codebases: [...]}`.
- `codebases.add` with `{id, name, paths}` → the registered codebase.
- `directories.list` with `{codebase_id, root_id, path}` →
  `{entries: [{name, kind}], truncated}`.

Both the caller and native host must be the same authenticated human. Host
ownership is never supplied by the caller, and site superusers have no bypass.
There are no Git operations, file-content reads/writes or agent execution methods.
