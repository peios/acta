# Application chrome

The signed-in root opens the most recently visited accessible workspace. The
shared shell hosts functional User Settings, Site Settings and workspace pages.

## Structure

The bottom of the sidebar selects a scope; the upper section belongs to that
scope. Workspaces has a permanent scope button. Site Settings appears when the
account can access Users or Groups. The main header names the current page.
Within Workspaces, the top scope label becomes a searchable workspace switcher.
The workspace work area contains the [task list and responsive task view](tasks.md).
Workspace settings include Details (including task prefix and statuses) and Members, with actions gated by effective
workspace permissions. See [Workspaces](workspaces.md).

A small Acta wordmark and version stamp sit just above the scope hairline, at
the bottom of the contextual navigation area. The displayed version comes from
the frontend package manifest (currently v0.0.0), not a separate hardcoded label.
A hairline above the scope selector separates it from the contextual navigation.
The compact profile footer sits below the scope selector without another divider. It shows the account's display
name on one line, falling back to username when no display name is set. An initial provides
a simple avatar placeholder. Long names truncate within the available space;
the account button retains its full accessible name.

The profile button opens a menu upward with **User Settings** and **Log out**.
User Settings switches the upper sidebar and main content into its own scope,
but has no permanent entry in the bottom scope selector. The theme toggle is an
independent button alongside the profile and retains its sun/moon animation.
Login and setup keep their top-right theme toggle.

User Settings contains Profile, Security and Agents. Site Settings contains Users
and Groups. Navigation uses real routes under `/user-settings`, `/site-settings`
and `/workspaces/{slug}`, including direct links, reloads and browser history.
A shared signed-in layout owns the current account and chrome across these pages.
Existing authentication and session-revoking logout are retained.

## Interaction

On desktop the sidebar starts at 264 pixels and can be resized between 220 and
400 pixels by dragging its right edge. Dragging below 160 pixels collapses it
to a 68-pixel icon rail; dragging back out to 190 pixels expands it. These
separate thresholds prevent flicker around the collapse boundary. A collapse
gesture preserves the expanded width from before that gesture, which the
expand button restores. Width and collapsed state are saved in browser local
storage; blocked storage does not prevent resizing.

The expand control remains at the top, with compact branding, Site Settings, the
account avatar at the bottom. The theme toggle and workspace picker are hidden while collapsed. Scope and account buttons retain
accessible names and reveal visible labels on hover or keyboard focus. The
profile menu works in both widths. The width follows the pointer directly while
dragging; button transitions respect reduced motion.

Double-clicking the edge resets the expanded width to 264 pixels. The edge is a
focusable separator: Left/Right adjust width by 16 pixels (32 with Shift), Left
at minimum width collapses, Right from collapsed restores the remembered width,
Enter toggles collapse, Home collapses, and End expands to maximum width.
Escape, pointer cancellation, or a window resize cancels an active drag and
restores its starting state. Keyboard semantics follow the
[ARIA window splitter pattern](https://www.w3.org/WAI/ARIA/apg/patterns/windowsplitter/).

The account menu supports keyboard focus, arrow keys, Home/End, Escape and
outside-click dismissal. A small entrance animation respects reduced motion.
Selecting a scope closes the menu and focuses the new page heading.

Below 721 CSS pixels the sidebar becomes a modal navigation drawer, opened from
the main header. Its account menu remains a nested popup; Escape dismisses that
menu before the drawer. Selecting a scope closes the drawer. Native dialog
behavior manages focus containment and restoration.

The current bottom scope selector and contextual upper sidebar follow the
interaction Jack liked in old Acta. Their styling follows the newly agreed Acta
visual direction; the old implementation has not been inspected or reused.

The dark palette uses neutral charcoal surfaces and warm gray text, with a muted
blue accent. Visual iterations are served on port 8081 so Jack can review them
as they happen.

## Security navigation and scrolling

User Settings contains Profile and Security. The main panel owns its scrolling;
the sidebar and page header remain anchored to the viewport. The grid row and
main flex child have explicit zero minimum sizes so a long session list cannot
expand the shell. Scroll chaining from the main panel is contained.
