# Code preview

The Code scope opens `/code` using the existing Acta sidebar, theme tokens and
navigation. The editor is a rough UI mockup; host discovery, codebases, directory
browsing and [Claude Code and Codex thread output](code-threads.md) are live on port 8080.

The Code sidebar header contains a host picker in the same position and style as
the Workspaces scope picker. It lists your registered hosts with live presence;
see [Code host registration](code-host.md).

The codebase presents agent thread output on the left and a sample editor on
the right, side by side
when the content area exceeds 850px. Narrower views use Editor / Conversation
buttons. The Code sidebar uses Agents / Files tabs: Agents lists real host-owned
threads with a start button; Files shows the [directory explorer](codebases.md).
Selecting a file only highlights it; selecting a thread opens its CAT output.
Mobile thread selection closes the navigation drawer. The editor's example diff
and Terminal toggle remain illustrative.

Sample source can be edited in the page. Sample files and the selected sidebar
tab are held in the authenticated app layout and reset on reload
or logout. A live codebase selector sits directly above Agents / Files inside a shared
bordered control, with an Add codebase button. The old sample conversation and
draft composer have been replaced by a live CAT thread view with basic message sending. The explorer
reads directory names/types, not file contents. The editor never writes source
files or executes its sample terminal commands.

Implementation reuses Acta chrome and styling but no existing agent runtime or
thread runtime. Provider switching is not implemented. Durable buffers, language services and
desktop packaging remain future iterations.

## Live host presence

The host picker now lists the signed-in human user’s real registered hosts, with
online/offline presence. See [Code host registration](code-host.md). Codebase
registration, directory browsing and Claude/Codex thread output are live; the editor
remains sample content.
