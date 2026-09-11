# Mobile workflow audit — 11 September 2026

Tracked by ACT-4. This extends the sidebar, board drag/snap and mobile form work
in ACT-2 and ACT-3. Scope is local development; no release or production deployment.

## Reproduced defects and fixes

1. Search used the layout viewport, extending below a simulated software keyboard.
   At a 380px visual viewport its 467px popup extended past the available area.
   It now uses the shared visual viewport height and offset on phones.
2. Filter checkbox rows measured 37px high. Coarse-pointer rows now have a 44px
   minimum touch target.
3. The effort slider had a 32px touch region. Its input and container now use 44px
   on coarse pointers; the visual track remains compact and centred.
4. Closing a description editor on pointer-down moved the destination before
   pointer-up. A tap on Write a comment closed the editor but did not open the
   composer. Dismissal now happens after the click, using the event's composed
   path to recognise clicks inside the editor. The single-tap transition passes.
5. The light-theme sign-in footer had a 3.91:1 contrast ratio for 12px text,
   below the 4.5:1 threshold. Its colour now uses the darker muted-text value;
   the served login page passes the automated contrast check in all three engines.

## Executed browser coverage

Tests mount the actual Svelte components with mocked API responses. This exercises
browser layout, events and component state, not production persistence or provider
protocols. No real provider sessions or model quota were used.

| Area | Checks | Engines |
| --- | --- | --- |
| Task editing | Title and description autosave; description-to-comment tap; comment draft recovery on reload; rich text round-trip | Chromium, Firefox, WebKit |
| Network failure | Aborted comment request retains draft; retry reuses its request ID and succeeds | Chromium, Firefox, WebKit |
| Search | Results, highlights, keyboard selection, pagination, workspace filtering, empty/error/retry states; keyboard viewport bounds | Chromium, Firefox, WebKit |
| Documents | Upload, replacement, Markdown preview, version history, deletion, keyboard viewport bounds | Chromium, Firefox, WebKit |
| Filters and assignment | Checkbox rows, assignee search/selection, reachable Done action with keyboard | Chromium, Firefox, WebKit |
| Agent controls | Model catalogue, effort target size, approval and question popups, free-text answer, disabled disconnected approval and reconnect | Chromium, Firefox, WebKit |
| Layout | Model popup bounds and document overflow at 320/390/720px portrait and 844×390 landscape; long URL, code and table content | Chromium, Firefox, WebKit |
| Navigation | Ten repeated sidebar open/close gestures; eight browser back/forward cycles preserving both board scroll axes | Chromium, Firefox, WebKit |
| Board | Snap styling, cancelled holds, multi-touch/resize cancellation, permissions, move menu, save conflict and desktop drag | Chromium, Firefox, WebKit; native touch pan/drag additionally in Chromium |
| Login | Real served build; narrow/landscape/reduced-height form reachability; locally aborted login request recovery | Chromium, Firefox, WebKit |
| Conversation scroll | Existing wheel, idle polling, growth/resize anchoring and reverse-pagination regressions; native touch drag while rows arrive | Chromium |
| PWA | Real service worker, manifest/icons, push without an open app tab, duplicate/revocation filtering, offline fallback and logout cleanup | Chromium |

The controls, search and documents suites passed on all three engines. The conversation
scroll suite passed on Chromium, including CDP touch input during incoming rows.
The compact approval screenshot was also visually inspected with the simulated
keyboard viewport: its actions remained visible and content wrapped correctly.

An additional axe-core 4.13.0 audit of the tested surfaces found no remaining
serious or critical WCAG A/AA violations. This includes open filter, model,
approval and question popups, the board, task details, document view and login.
It is an automated check, not a screen-reader or comprehensive accessibility audit.

WebKit 26.5 ran locally through Playwright 1.62.1 with dependencies unpacked in
`/tmp`; no OS packages were installed. Its desktop Select All keyboard shortcuts
did not create a selection in mobile emulation. The formatting test therefore
establishes a DOM selection before tapping the real toolbar. Sidebar/hold
lifecycle events are synthetic outside Chromium; native mobile selection handles
and physical gesture arbitration remain device checks.

Final validation: Svelte check reported zero errors/warnings; all 31 unit tests
passed; frontend build, format check, Go server build and whitespace check passed.
The rebuilt local server serves the browser login route on port 8081.

## Reproduction

Run from `web/`, with Playwright installed and its browser executables available:

```sh
node tests/mobile-controls.browser.mjs
node tests/mobile-board.browser.mjs
node tests/mobile-details.browser.mjs
node tests/mobile-login.browser.mjs # requires the local build served on 8081
ACTA_MOBILE_AUDIT=1 node tests/task-search.browser.mjs
ACTA_MOBILE_AUDIT=1 node tests/documents.browser.mjs
node tests/thread-scroll.browser.mjs
node tests/pwa.browser.mjs
```

Set `ACTA_MOBILE_BROWSER=firefox` or `webkit` for mobile suites/documents, or
`ACTA_SEARCH_BROWSER` for search. `ACTA_MOBILE_EXECUTABLE` can select the installed
executable. Scroll supports `ACTA_SCROLL_EXECUTABLE`. Set `ACTA_AXE_SCRIPT` to a
local axe-core script to enable the optional accessibility assertions. Test-only
dependencies are not added to the application.

## Remaining device validation

No physical iOS or Android device was available. Linux WebKit was tested;
the actual Safari application and iOS browser integration were not.
Keyboard tests resize the visual viewport; they do not exercise an actual OS
keyboard, focus zoom, predictive input or keyboard animation. Rotation is viewport
resizing. Connection failure is a browser-request abort, and reconnect is a
component availability change. These do not establish mobile OS background/resume,
process eviction, push delivery or native file-picker behaviour.

The next step agreed with Jack is deployment followed by phone testing. Use a real
iPhone and Android phone, where available, for sidebar and board gestures,
dragging between columns, keyboard-open editing/approvals, browser back navigation,
rotation, app switching and reconnection. Browser-emulated passes are evidence for
the covered workflows, not a claim of complete mobile compatibility.

## v0.2.1 follow-up: iOS edge navigation

Jack reported native swipe-back competing with the sidebar in the installed iOS
home-screen app. The patch reserves the existing 28px opening strip using a
non-passive `touchstart` listener and applies root horizontal overscroll suppression.
Controls remain tappable, and browser history is retained. Native vertical pan
starting in the reserved strip is suppressed as an unavoidable consequence of
cancelling touchstart before gesture direction is known.

The updated board suite passed in Chromium, Firefox and Linux WebKit. Assertions
cover cancellation at touchstart without a `pageX` property, outside-edge input,
non-cancelable input, links/buttons/labels/fields, multi-touch, repeated sidebar
gestures and existing history restoration. Chromium's native touch tests also
passed sidebar opening, vertical scrolling, snapping and cross-column dragging.
`make check` passed, including frontend diagnostics, formatting, unit tests,
production frontend build, Go vet and Go tests. These tests do not reproduce
iOS's native history gesture recognizer; suppression on Jack's phone remains
unverified until the patch is installed there.

### Follow-up after phone feedback

Jack confirmed v0.2.1 blocks swipe-back when opening the sidebar, but repeating
the swipe with it already open still navigated back. Edge cancellation now
precedes overlay gating and applies with the drawer, dialogs and popovers open.
A rightward swipe leaves the open drawer alone; leftward closing is unchanged.
The six-dot card drag handle and immediate-pickup path were also removed at
Jack's request. Long-press pickup retains the 350ms delay and the three-dot move
menu remains available.

The updated board suite passed in Chromium, Firefox and Linux WebKit, including
repeated edge swipes while open, dialog/popover protection, control exemptions,
and absence of drag handles. Native Chromium cross-column movement and save
conflict recovery now use whole-card long press. Native iOS suppression with
overlays still requires a phone check.
