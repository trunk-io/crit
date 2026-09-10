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

// useMaxWidth:true scaled a wide hierarchy down to the container, rendering an
// 18-node graph as unreadable slivers. Both halves of the fix are load-bearing:
// the mermaid option AND the absence of a max-width in our own CSS, which
// re-squeezed it when they disagreed.
test('the diagram renders at natural size, not scaled to fit', () => {
  const src = fs.readFileSync(path.join(WEB, 'crit-graph-view.js'), 'utf8');
  assert.ok(src.includes('useMaxWidth: false'), 'mermaid must not scale the diagram to the container');

  const css = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');
  const rule = css.slice(css.indexOf('.crit-graph-diagram svg {'));
  const block = rule.slice(0, rule.indexOf('}'));
  assert.ok(!block.includes('max-width'), 'a max-width here undoes useMaxWidth:false');

  const wrap = css.slice(css.indexOf('.crit-graph-diagram-wrap {'));
  assert.ok(wrap.slice(0, wrap.indexOf('}')).includes('overflow: auto'), 'the wrapper must scroll');
});

// The container must NOT carry the `mermaid` class: it is rendered by explicit
// node, and the class would enrol it in every bare mermaid.run() the review
// page makes for markdown diagrams.
test('the diagram container is isolated from global mermaid runs', () => {
  const src = fs.readFileSync(path.join(WEB, 'crit-graph-view.js'), 'utf8');
  assert.ok(
    !/el\('div', 'mermaid crit-graph-diagram'\)/.test(src),
    'the container must not carry the mermaid class'
  );
  assert.ok(src.includes('mermaid.run({ nodes: [diagram] })'), 'render by explicit node');
});

// Mermaid bakes its theme into the emitted SVG and skips processed nodes, so
// only a re-render restyles the graph on a theme switch.
test('a theme change re-renders the graph and keeps the selection', () => {
  const app = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  const theme = app.slice(app.indexOf('window.applyTheme = function'));
  const body = theme.slice(0, theme.indexOf('\n  };'));
  assert.ok(body.includes('renderGraphView({ keepSelection: true })'), 'applyTheme must re-render the graph');
  assert.ok(app.includes('window.crit.openMermaidOverlay'), 'the pan/zoom overlay must be shared');
});

// Stats come from the ingest's diagnostics; a graph without them must not
// report "0 nodes". Asserted behaviourally rather than by grepping the
// implementation, which is what made this test brittle the first time.
test('the header falls back to counting nodes without diagnostics', () => {
  const src = fs.readFileSync(path.join(WEB, 'crit-graph-view.js'), 'utf8');
  assert.ok(src.includes('st.nodes || countCallNodes(g)'), 'need a node-count fallback');
  assert.strictEqual(view.countCallNodes({ nodes: { a: {}, b: {} } }), 2);
});

// The header's node count must mean the same thing as GraphStats.Nodes, which
// counts call nodes and tallies modules separately. Counting the whole map
// made the header disagree with what the CLI had just printed for the same
// graph — "30 nodes" against "25 nodes, 5 modules".
test('the node-count fallback excludes modules', () => {
  const g = {
    nodes: {
      mod: { kind: 'module', status: 'new' },
      a: { status: 'new' },
      b: { kind: 'function', status: 'new' },
    },
  };
  assert.strictEqual(view.countCallNodes(g), 2);
  assert.strictEqual(view.countCallNodes({}), 0);
  assert.strictEqual(view.countCallNodes(null), 0);
});

// A graph-only review has an empty file list, so it must default to the
// diagram rather than landing the reader on an empty pane. The decision has to
// happen after the file list loads — deciding in loadGraph would auto-show the
// graph in review mode too, because files load asynchronously.
test('a graph-only review defaults to the diagram', () => {
  const app = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  assert.ok(app.includes('function maybeAutoShowGraph'), 'need an auto-show decision');
  assert.ok(
    app.includes('applyStoryPresence();\n    // A graph-only review'),
    'auto-show must run after the session load, beside applyStoryPresence'
  );
  const fn = app.slice(app.indexOf('function maybeAutoShowGraph'));
  const body = fn.slice(0, fn.indexOf('\n  }'));
  assert.ok(body.includes('(files || []).length > 0'), 'must not fire when there is a diff');
  assert.ok(body.includes('graphVisible'), 'must not override a view the reader chose');
});

// Chips and outlines are two channels on purpose: a chip says WHAT, an
// outline says LOOK HERE. Rendering attention as one more chip made it compete
// with the chips beside it, which is the opposite of what it is for.
test('badges split into chip and outline channels', () => {
  const g = {
    legend: [
      { id: 'unit', label: 'unit', color: 'blue' },
      { id: 'attention', label: 'read closely', color: 'red', style: 'outline' },
    ],
  };
  const badges = view.badgesFor(g, { badges: ['unit', 'attention'] });
  assert.strictEqual(view.outlineBadge(badges).id, 'attention');
  assert.deepStrictEqual(view.chipBadges(badges).map((b) => b.id), ['unit']);
  // A badge with no style declared defaults to a chip.
  assert.strictEqual(view.badgesFor(g, { badges: ['unit'] })[0].style, 'chip');
  assert.strictEqual(view.outlineBadge(view.badgesFor(g, { badges: ['unit'] })), null);
});

// The ring is a direct-child <rect> of the node group, so the status rules
// (`.critNodeX > rect`) matched it and outranked a single outline class —
// every ring came out in the status colour instead of the badge's.
test('the attention ring is not captured by the status fill rules', () => {
  const css = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');
  for (const status of ['critNodeExisting', 'critNodeNew', 'critNodeModified', 'critNodeExternal']) {
    assert.ok(
      css.includes(`.${status} > rect:not(.crit-graph-outline)`),
      `${status} must exclude the outline rect`
    );
  }
  assert.ok(
    css.includes('.crit-graph-outline.crit-outline-red'),
    'the ring colour needs two classes to outrank the status rules'
  );
});

// Every palette colour needs a chip and an outline rule, or a valid badge
// renders colourless.
test('every palette colour has chip and outline CSS', () => {
  const css = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');
  for (const c of ['red', 'orange', 'amber', 'green', 'teal', 'blue', 'indigo', 'violet', 'pink', 'slate']) {
    // The rules are whitespace-aligned in the stylesheet, so match loosely.
    assert.ok(css.includes(`.crit-chip-${c}`), `missing chip rules for ${c}`);
    assert.ok(css.includes(`.crit-graph-chip-bg { fill: color-mix(in srgb, var(--crit-graph-${c})`),
      `missing chip bg for ${c}`);
    assert.ok(css.includes(`.crit-graph-chip-text { fill: var(--crit-graph-${c})`),
      `missing chip text for ${c}`);
    assert.ok(css.includes(`.crit-graph-outline.crit-outline-${c}`), `missing outline for ${c}`);
  }
});

// A reader who guesses the direction wrong reads the call tree backwards, so
// every direction Mermaid accepts must have a sentence.
test('every flowchart direction has a stated reading order', () => {
  const src = fs.readFileSync(path.join(WEB, 'crit-graph-view.js'), 'utf8');
  const table = src.slice(src.indexOf('var READ_DIRECTION'), src.indexOf('function renderAxis'));
  for (const dir of ['LR', 'RL', 'TD', 'TB', 'BT']) {
    assert.ok(table.includes(`${dir}: {`), `no reading order for ${dir}`);
  }
  assert.ok(src.includes('function renderAxis'), 'the direction must be rendered');
  assert.ok(src.includes('attachEntryMarker'), 'entry points must be marked on the nodes');
});
