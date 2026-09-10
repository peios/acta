# Foundation quality review — 7 September 2026

Tracked in ACT-57. Jack authorized a general UI, UX and code quality pass after
the foundational task slice, including refactoring and restructuring. This pass
preserves the agreed feature set and design language.

## Baseline and review method

The source tree was saved before editing. The repository's implementation was
already untracked, so review used that saved source rather than treating every
untracked file as new work. Existing frontend checks and the full Go suite with
PostgreSQL and the race detector passed before changes.

The review concentrated on asynchronous request ownership, live task updates,
navigation and editor focus, shared popovers, task persistence queries and
frontend loading cost. Account, security, agent, group and workspace settings
received responsive browser review. The old Acta implementation was not used.

## Findings and changes

| Finding | Result |
| --- | --- |
| A failed workspace refresh or visit-recording request could tear down the current workspace UI. | Transient errors keep the loaded workspace and child UI. Access loss still removes it. Visit recording has its own failure handling and retry path. |
| Overlapping account/task reads could deliver stale results after a newer request or navigation. | A shared latest-request owner aborts superseded reads and ignores results after disposal. Direct task-link resolution is cancelled when its route changes. |
| The live task loop duplicated HTTP handling and could regress a newer focus-refresh revision. | One lifecycle-owned feed uses the shared API, accepts monotonic revisions, cancels outstanding work on teardown and retries temporary failures. The authenticated layout responds to the specific unauthenticated error code, without treating an incorrect current password as session expiry. |
| Refresh failures produced repeated connection errors across board columns. | The revision feed supplies shared feedback; identical child read errors are suppressed while distinct errors remain visible. Recovery triggers list refreshes. |
| Refreshing a paginated list discarded pages opened with Load more. | Refresh reads enough pages to replace the visible window atomically. Failures keep the old window, overlapping pages are deduplicated, and repeated cursors cannot loop indefinitely. |
| Generic navigation focus handling interfered with task dialogs; the panel initially focused its resize handle. | Query-only navigation preserves task focus. The close control receives initial task focus; presentation changes preserve an already focused task control. |
| Five popover implementations repeated viewport calculations with inconsistent scroll and edge handling. | One action handles anchoring, flipping, viewport bounds, scroll/resize events and changing content. Browser testing also caught and fixed a loading-height limit that prevented assignee search results from expanding. |
| Editor mode changes did not reliably focus the active input; an invalid stored mode could leave no editor. | Mode changes wait for rendering before focusing; unsupported stored preferences fall back to rich text. |
| Rich-text dependencies were part of the initial task route even when only viewing the list. | Description rendering/editing is loaded on demand, with loading and retry states. The task route chunk fell from approximately 582 kB to 98 kB minified, or 183 kB to 30 kB gzip. These are build chunk sizes, not a whole-page network benchmark. |
| Task assignment decoration repeated recursive queries and account projections for each row. | Assignment trees load once per page; per-person projections are reused within that transaction. Direct and descendant assignments remain distinct, with a test preventing source leakage between roots. No cross-request authorization cache was introduced. |
| The task workbench and PostgreSQL task file mixed unrelated responsibilities. | The task toolbar, revision feed and pagination window have separate owners. PostgreSQL task configuration and people/assignment projections are separated from core task reads/writes. |
| Frontend formatting relied on ad hoc tooling. | Pinned Prettier and its Svelte plugin, committed configuration and a formatting gate in make check. Existing formatting-only changes were separated from functional changes during review. |

The npm audit also identified a low-severity advisory in SvelteKit's transitive
cookie dependency. A narrow override pins its compatible patched 0.7.2 release;
the resulting audit reports zero known vulnerabilities. The relevant advisory
is [GHSA-pxg6-pf52-xh8x](https://github.com/advisories/GHSA-pxg6-pf52-xh8x).
No broad framework upgrade or authentication protocol change was made.

## Code boundaries

- `requests.js`: cancellation and ownership for replaceable reads and delays.
- `task-feed.js`: task revision polling, refresh arbitration and lifetime.
- `task-refresh-context.ts`: shared task-read error/recovery state for nested lists.
- `task-pages.js`: atomic refresh of the already visible cursor window.
- `popover-position.js` and `anchored-popover.ts`: pure placement geometry and browser lifecycle respectively.
- `TaskListToolbar.svelte`: task heading, responsive search and creation action.
- `TaskDescription.svelte`: deferred rich-text loading and retry feedback.
- `task_config.go`: task workflow settings and revision changes.
- `task_people.go`: people/access projections and assignment trees.
- `tasks.go`: core task lookup, listing and mutations.

## Validation

- `make check`: production frontend build, formatting, zero Svelte errors or
  warnings, 31 frontend tests, Go vet, unit tests and Go formatting.
- Full Go suite with `-race -count=1` and `ACTA_TEST_DATABASE_URL`: passed,
  including isolated-schema PostgreSQL tests. The application database was not
  wiped or repopulated.
- New regression cases cover request disposal, stale revision responses,
  reconnection and access loss, popup edges/visual-viewport offsets, pagination
  windows/failures and assignment-source isolation.
- Browser review on localhost:8081 at 390×844, 1000×800 and 1600×1000: mobile
  full-screen task, tablet modal, desktop panel, nested display menus, upward
  status popup, assignee search/results, editor focus, preset retention and
  account/settings dialogs. Both light and dark themes were inspected.
- A real backend stop/restart confirmed that loaded cards and an unsaved display
  change survive interruption and the live view recovers.

## Limits

This is a quality checkpoint, not a production-readiness or security
certification. The browser checks used desktop Chromium with emulated viewport
sizes; physical mobile keyboards, Safari and Firefox were not revalidated here.
Passkey/MFA enrollment and recovery were covered by the existing automated suite
rather than changing the local user's credentials. No large-company load test
or quantitative SQL latency benchmark was performed. Comments, activity UI,
projects and harness execution remain separate future product slices.
