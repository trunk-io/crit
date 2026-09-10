const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const view = require('../crit-graph-view.js');

const WEB = path.join(__dirname, '..');

function graph() {
  return {
    title: 'Mint org tokens',
    phase: 'plan',
    diagram: 'flowchart LR\n  handle --> mint\n  mint --> signKms',
    nodes: {
      handle: { signature: 'handle(r: Req): Res', file: 'src/handle.ts', line: 10, status: 'modified', badges: ['integration'] },
      mint: { signature: 'mint(o: Org): string', file: 'src/mint.ts', status: 'new', badges: ['unit', 'attention'] },
      signKms: { status: 'external', kind: 'external' },
    },
    legend: [
      { id: 'integration', label: 'integration', color: 'teal', group: 'tests', description: 'msw + seeded db' },
      { id: 'unit', label: 'unit', color: 'blue', group: 'tests' },
      { id: 'attention', label: 'read closely', color: 'red', group: 'attention' },
    ],
  };
}

test('buildDiagramSource appends class statements and never classDef', () => {
  const src = view.buildDiagramSource(graph());
  assert.ok(src.startsWith('flowchart LR'), 'author source must lead');
  assert.ok(src.includes('  class mint critNodeNew'), src);
  assert.ok(src.includes('  class handle critNodeModified'), src);
  assert.ok(src.includes('  class signKms critNodeExternal'), src);
  // classDef would bake literal colors into the SVG and defeat theming.
  assert.ok(!/classDef/.test(src), 'must not emit classDef');
});

test('buildDiagramSource groups nodes sharing a status into one statement', () => {
  const g = graph();
  g.nodes.signKms = { signature: 's()', file: 's.ts', line: 1, status: 'new' };
  const src = view.buildDiagramSource(g);
  const newLine = src.split('\n').find((l) => l.includes('critNodeNew'));
  assert.match(newLine, /class (mint,signKms|signKms,mint) critNodeNew/);
});

test('buildDiagramSource leaves the author source untouched', () => {
  const g = graph();
  const src = view.buildDiagramSource(g);
  assert.ok(src.includes(g.diagram), 'the author diagram must appear verbatim');
});

test('buildDiagramSource tolerates an empty graph', () => {
  assert.strictEqual(view.buildDiagramSource(null), '');
  assert.strictEqual(view.buildDiagramSource({ diagram: '' }), '');
});

test('nodeIdFromElement prefers data-id, falls back to the flowchart id pattern', () => {
  assert.strictEqual(view.nodeIdFromElement({ getAttribute: (k) => (k === 'data-id' ? 'mintToken' : null) }), 'mintToken');
  assert.strictEqual(
    view.nodeIdFromElement({ getAttribute: (k) => (k === 'id' ? 'flowchart-mintToken-7' : null) }),
    'mintToken'
  );
  // An underscore-bearing id must survive the pattern.
  assert.strictEqual(
    view.nodeIdFromElement({ getAttribute: (k) => (k === 'id' ? 'flowchart-mint_token-12' : null) }),
    'mint_token'
  );
  assert.strictEqual(view.nodeIdFromElement({ getAttribute: () => null }), null);
  assert.strictEqual(view.nodeIdFromElement(null), null);
});

test('copyTargetFor omits the line rather than faking :0 for unwritten code', () => {
  assert.strictEqual(view.copyTargetFor({ file: 'src/a.ts', line: 42 }), 'src/a.ts:42');
  assert.strictEqual(view.copyTargetFor({ file: 'src/a.ts' }), 'src/a.ts');
  assert.strictEqual(view.copyTargetFor({ file: 'src/a.ts', line: 0 }), 'src/a.ts');
  assert.strictEqual(view.copyTargetFor({}), '');
  assert.strictEqual(view.copyTargetFor(null), '');
});

test('badgesFor resolves ids against the legend', () => {
  const g = graph();
  const badges = view.badgesFor(g, g.nodes.handle);
  assert.strictEqual(badges.length, 1);
  assert.strictEqual(badges[0].label, 'integration');
  assert.strictEqual(badges[0].color, 'teal');
  assert.strictEqual(badges[0].description, 'msw + seeded db');
});

test('badgesFor degrades an unknown badge to slate rather than dropping it', () => {
  const g = graph();
  const badges = view.badgesFor(g, { badges: ['nope'] });
  assert.strictEqual(badges.length, 1);
  assert.strictEqual(badges[0].color, 'slate');
  assert.strictEqual(badges[0].label, 'nope');
});

test('legendGroups preserves declaration order within a group', () => {
  const groups = view.legendGroups(graph());
  assert.deepStrictEqual(groups.map((g) => g.group), ['tests', 'attention']);
  assert.deepStrictEqual(groups[0].badges.map((b) => b.id), ['integration', 'unit']);
});

test('statusClass falls back rather than leaving a node unstyled', () => {
  assert.strictEqual(view.statusClass('new'), 'critNodeNew');
  // An uncolored node reads as a rendering bug rather than as data.
  assert.strictEqual(view.statusClass('nonsense'), 'critNodeExisting');
  assert.strictEqual(view.statusClass(undefined), 'critNodeExisting');
});

// Every palette color the Go side accepts needs both a marker fill (drawn in
// the SVG) and a badge chip rule (drawn in HTML), or a valid graph renders a
// colorless badge.
test('every palette color has marker and badge CSS', () => {
  const css = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');
  const palette = ['red', 'orange', 'amber', 'green', 'teal', 'blue', 'indigo', 'violet', 'pink', 'slate'];
  for (const color of palette) {
    assert.ok(css.includes('--crit-graph-' + color + ':'), `missing --crit-graph-${color}`);
    assert.ok(css.includes('.crit-marker-' + color), `missing .crit-marker-${color}`);
    assert.ok(css.includes('.crit-badge-' + color), `missing .crit-badge-${color}`);
  }
});

// The Go validator's status vocabulary and the CSS that colors it must agree,
// or a valid status renders unstyled.
test('every node status has CSS and a label', () => {
  const css = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');
  for (const status of ['existing', 'new', 'modified', 'external']) {
    assert.ok(view.STATUS_CLASS[status], `no class for status ${status}`);
    assert.ok(view.STATUS_LABEL[status], `no label for status ${status}`);
    assert.ok(css.includes('.' + view.STATUS_CLASS[status] + ' >'), `no CSS for ${status}`);
    assert.ok(css.includes('.crit-graph-status-' + status), `no swatch CSS for ${status}`);
  }
});

test('index.html provides the ids and script the view needs', () => {
  const html = fs.readFileSync(path.join(WEB, 'index.html'), 'utf8');
  for (const id of ['graphRoot', 'graphViewToggle']) {
    assert.ok(html.includes('id="' + id + '"'), `index.html is missing #${id}`);
  }
  assert.ok(html.includes('crit-graph-view.js'), 'index.html does not load crit-graph-view.js');
});

// app.js owns transport; the module owns rendering. Pin the seam so a rename
// on either side fails here instead of at runtime.
test('app.js wires the graph transport to the view module', () => {
  const app = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  assert.ok(app.includes("fetch('/api/graph')"), 'app.js does not fetch the graph');
  assert.ok(app.includes("'/api/graph-comment'"), 'app.js does not post node comments');
  assert.ok(app.includes("'graph-updated'"), 'app.js does not handle the graph-updated SSE event');
  assert.ok(app.includes('window.crit.graphView.render'), 'app.js does not call the view renderer');
  assert.ok(app.includes('window.crit.mermaidTheme'), 'app.js does not expose the mermaid theme resolver');
});

test('the module exposes the api app.js calls', () => {
  for (const fn of ['render', 'setHooks', 'setComments', 'selectNode', 'hideHoverCard', 'refreshCommentBadge']) {
    assert.strictEqual(typeof view[fn], 'function', `missing export ${fn}`);
  }
});

// Posting a comment must not re-run Mermaid: that rebuilds the SVG and throws
// away the reader's scroll position to change one number.
test('app.js updates one badge instead of re-rendering after a comment', () => {
  const app = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  const post = app.slice(app.indexOf('async function postGraphComment'));
  const body = post.slice(0, post.indexOf('\n  }'));
  assert.ok(body.includes('refreshCommentBadge'), 'postGraphComment should refresh the badge');
  assert.ok(!body.includes('renderGraphView()'), 'postGraphComment must not re-render the whole view');
});

// The hover card carries a copy button, so it cannot be pointer-events:none —
// the button would be unclickable and the card would hide as you reached it.
test('the hover card is interactive', () => {
  const css = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');
  const rule = css.slice(css.indexOf('.crit-graph-hovercard {'));
  const block = rule.slice(0, rule.indexOf('}'));
  assert.ok(block.includes('pointer-events: auto'), 'hovercard must accept pointer events');
  const src = fs.readFileSync(path.join(WEB, 'crit-graph-view.js'), 'utf8');
  assert.ok(src.includes('HOVER_GRACE_MS'), 'a grace period must bridge the node/card gap');
});

// `external` is the one unfilled status chip, so it needs its own text colour;
// the shared white-on-fill rule made it invisible in light mode.
test('the external status chip has its own colour', () => {
  const css = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');
  assert.ok(
    css.includes('.crit-graph-status.crit-graph-status-external'),
    'external needs an explicit colour, not the filled-chip default'
  );
});
