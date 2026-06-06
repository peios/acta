// Behavioural test for board-prefs.js — run with: node board-prefs.test.mjs
//
// board-prefs has no browser harness in CI, and its replay-vs-reset decision is
// subtle (a deliberate reset to the default view must NOT be auto-restored), so
// this evaluates the real file against mocked globals and asserts the outcomes.
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const here = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(here, 'board-prefs.js'), 'utf8');

// Run board-prefs.js in a sandbox. The IIFE reads bare globals (location,
// localStorage, …); we pass them as Function params so they resolve to our mocks.
function run({ search, saved, nav }) {
  const ls = new Map();
  if (saved != null) ls.set('acta:board:general', saved);
  const ss = new Map();
  if (nav) ss.set('acta:nav:general', '1');
  let redirectedTo = null;
  const location = { pathname: '/general', search, replace: (u) => { redirectedTo = u; } };
  const localStorage = { getItem: (k) => (ls.has(k) ? ls.get(k) : null), setItem: (k, v) => ls.set(k, v) };
  const sessionStorage = {
    getItem: (k) => (ss.has(k) ? ss.get(k) : null),
    setItem: (k, v) => ss.set(k, v),
    removeItem: (k) => ss.delete(k),
  };
  const document = { readyState: 'complete', querySelector: () => null, addEventListener: () => {} };
  // eslint-disable-next-line no-new-func
  new Function('location', 'localStorage', 'sessionStorage', 'document', 'window', src)(
    location, localStorage, sessionStorage, document, {});
  return {
    redirectedTo,
    savedAfter: ls.has('acta:board:general') ? ls.get('acta:board:general') : null,
    navAfter: ss.get('acta:nav:general') ?? null,
  };
}

let fail = 0;
const check = (name, cond) => {
  if (!cond) { console.log('FAIL:', name); fail++; } else console.log('ok  :', name);
};

// Fresh, unspecified arrival restores the saved view.
let r = run({ search: '', saved: 'subtasks=1', nav: false });
check('fresh bare replays saved', r.redirectedTo === '/general?subtasks=1');

// THE BUG: a deliberate reset (bare URL + explicit-nav marker) must not replay,
// and must clear the saved view so it stays reset.
r = run({ search: '', saved: 'subtasks=1', nav: true });
check('explicit reset does not replay', r.redirectedTo === null);
check('explicit reset clears saved', r.savedAfter === '');
check('explicit reset consumes the marker', r.navAfter === null);

// A URL that specifies a view is authoritative — remembered, never replayed.
r = run({ search: '?subtasks=1', saved: '', nav: false });
check('hasView does not replay', r.redirectedTo === null);
check('hasView saves canon', r.savedAfter === 'subtasks=1');

// Fresh arrival with nothing saved: nothing to restore, saves the empty default.
r = run({ search: '', saved: null, nav: false });
check('fresh bare empty: no replay, saves ""', r.redirectedTo === null && r.savedAfter === '');

// A non-default grouping is remembered.
r = run({ search: '?mode=priority', saved: '', nav: false });
check('grouping remembered', r.savedAfter === 'mode=priority');

// Resetting one axis while another stays non-default is a non-bare URL: the
// remaining axis persists.
r = run({ search: '?subtasks=1', saved: 'mode=priority&subtasks=1', nav: true });
check('partial reset keeps remaining axes', r.savedAfter === 'subtasks=1' && r.redirectedTo === null);

console.log(fail ? `\n${fail} FAILED` : '\nALL PASSED');
process.exit(fail ? 1 : 0);
