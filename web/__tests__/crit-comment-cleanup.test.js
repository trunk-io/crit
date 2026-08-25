// A level that dropped a keep rule — especially the directive exemption — would
// tell the agent to strip eslint-disable / nolint / SPDX lines and change
// behavior, so the keep rules are asserted at every level, not just the default.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

const cleanup = require('../crit-comment-cleanup.js');

const { LEVELS, DEFAULT_LEVEL, buildCleanupPrompt, clampLevel, levelAt } = cleanup;

function sectionOf(prompt, heading) {
  const lines = prompt.split('\n');
  const start = lines.indexOf(heading);
  assert.notStrictEqual(start, -1, `expected a "${heading}" section in the prompt`);
  const out = [];
  for (let i = start + 1; i < lines.length; i++) {
    if (!lines[i].startsWith('- ')) break;
    out.push(lines[i].slice(2));
  }
  return out;
}

test('exports five ordered levels with a Balanced default', () => {
  assert.equal(LEVELS.length, 5);
  assert.deepEqual(
    LEVELS.map((l) => l.label),
    ['Light', 'Moderate', 'Balanced', 'Aggressive', 'Maximum']
  );
  assert.equal(DEFAULT_LEVEL, 3);
  assert.equal(levelAt(DEFAULT_LEVEL).id, 'balanced');
});

test('clampLevel bounds the slider range and falls back on garbage', () => {
  assert.equal(clampLevel(0), 1);
  assert.equal(clampLevel(-4), 1);
  assert.equal(clampLevel(6), 5);
  assert.equal(clampLevel(99), 5);
  assert.equal(clampLevel('4'), 4); // range inputs hand back strings
  assert.equal(clampLevel(3.4), 3);
  assert.equal(clampLevel(undefined), DEFAULT_LEVEL);
  assert.equal(clampLevel('nope'), DEFAULT_LEVEL);
});

test('every level keeps the WHY and directive exemptions', () => {
  for (let n = 1; n <= LEVELS.length; n++) {
    const keep = sectionOf(buildCleanupPrompt(n), 'Keep:');
    assert.ok(
      keep.some((k) => /non-obvious WHY/.test(k)),
      `level ${n} must keep WHY comments`
    );
    assert.ok(
      keep.some((k) => /eslint-disable/.test(k) && /nolint/.test(k) && /SPDX/.test(k)),
      `level ${n} must keep directive and suppression comments`
    );
  }
});

test('the remove list is additive across levels', () => {
  let prev = [];
  for (let n = 1; n <= LEVELS.length; n++) {
    const remove = sectionOf(buildCleanupPrompt(n), 'Remove:');
    for (const earlier of prev) {
      assert.ok(remove.includes(earlier), `level ${n} dropped an earlier rule: ${earlier}`);
    }
    assert.ok(remove.length > prev.length, `level ${n} should add at least one rule`);
    prev = remove;
  }
});

test('only the maximum level relaxes the public-doc keep rule', () => {
  for (let n = 1; n < LEVELS.length; n++) {
    const keep = sectionOf(buildCleanupPrompt(n), 'Keep:');
    assert.ok(
      keep.some((k) => /exported or public API/.test(k)),
      `level ${n} should still keep public API docs`
    );
  }
  const maxKeep = sectionOf(buildCleanupPrompt(LEVELS.length), 'Keep:');
  assert.ok(!maxKeep.some((k) => /exported or public API/.test(k)));
});

test('prompt scopes the pass to this change and forbids behavior edits', () => {
  const prompt = buildCleanupPrompt(DEFAULT_LEVEL);
  assert.match(prompt, /comments-only pass/);
  assert.match(prompt, /do not change behavior/);
  assert.match(prompt, /leave pre-existing comments in these files alone/);
  assert.match(prompt, /Level 3\/5 — Balanced/);
});

test('out-of-range slider values still produce a valid prompt', () => {
  assert.equal(buildCleanupPrompt(0), buildCleanupPrompt(1));
  assert.equal(buildCleanupPrompt(42), buildCleanupPrompt(5));
  assert.equal(buildCleanupPrompt('nope'), buildCleanupPrompt(DEFAULT_LEVEL));
});

test('init is a no-op without a DOM', () => {
  assert.equal(typeof document, 'undefined');
  assert.doesNotThrow(() => cleanup.init({}));
  assert.doesNotThrow(() => cleanup.open());
  assert.doesNotThrow(() => cleanup.close());
});

// A renamed id in index.html would silently disable the dialog, not fail a test.
test('index.html provides every id the module wires up', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const src = fs.readFileSync(path.join(__dirname, '..', 'crit-comment-cleanup.js'), 'utf8');

  const ids = [...src.matchAll(/el\('([A-Za-z]+)'\)/g)].map((m) => m[1]);
  const wired = [...new Set(ids.concat(['cleanupAddBtn', 'cleanupAddSubmitBtn']))];
  assert.ok(wired.length >= 8, 'expected the module to reference several dialog ids');

  for (const id of wired) {
    assert.match(html, new RegExp(`id="${id}"`), `index.html is missing id="${id}"`);
  }
});

test('the toolbar lives inside .header so --header-height covers it', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const header = html.match(/<div class="header">([\s\S]*?)\n<\/div>\n/);
  assert.ok(header, 'expected a top-level .header block');
  assert.match(header[1], /id="reviewToolbar"/);
  assert.match(header[1], /class="header-bar"/);

  const css = fs.readFileSync(path.join(__dirname, '..', 'style.css'), 'utf8');
  assert.match(css, /\.header-bar \{[^}]*padding: 12px 24px;/);

  const live = fs.readFileSync(path.join(__dirname, '..', 'style-live.css'), 'utf8');
  assert.match(live, /#reviewToolbar \{ display: none !important; \}/);
});
