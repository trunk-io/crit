const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

const form = require('../crit-comment-form.js');
const icons = require('../crit-icons.js');

const read = (f) => fs.readFileSync(path.join(__dirname, '..', f), 'utf8');

test('createForm exposes onReserve and a reserve label', () => {
  const src = read('crit-comment-form.js');
  assert.match(src, /opts\.onReserve/);
  assert.match(src, /reserveLabel/);
  assert.match(src, /btn-reserve/);
  assert.equal(typeof form.createForm, 'function');
});

// The reserve path must validate the body exactly like the primary submit;
// an empty reserved comment would post a blank paragraph into the PR body.
test('doReserve mirrors doSubmit body validation', () => {
  const src = read('crit-comment-form.js');
  const doReserve = src.slice(src.indexOf('function doReserve'));
  assert.match(doReserve, /textarea\.value\.trim\(\)/);
  assert.match(doReserve, /if \(!body\) \{ textarea\.focus\(\); return; \}/);
});

test('GitHub mark icon is exported', () => {
  assert.equal(typeof icons.ICON_GITHUB_MARK, 'string');
  assert.match(icons.ICON_GITHUB_MARK, /^<svg/);
  assert.match(icons.ICON_GITHUB_MARK, /viewBox="0 0 16 16"/);
});

test('comment card badges reserved comments but not once they reach GitHub', () => {
  const src = read('crit-comment-card.js');
  assert.match(src, /comment\.reserved_for_github && !comment\.github_id/);
  assert.match(src, /reserved-badge/);
});

test('app.js sends reserved_for_github on both comment scopes', () => {
  const src = read('app.js');
  assert.match(src, /payload\.reserved_for_github = true/);
  assert.match(src, /addReviewComment\(textarea\.value, \{ reserved: true \}\)/);
  assert.match(src, /submitComment\(textarea\.value, formObj, \{ reserved: true \}\)/);
});

// The client's Approve/Finish label must agree with the server's `approved`
// decision, which excludes reserved comments from UnresolvedCommentCount.
test('approve gate counts only agent-facing comments', () => {
  const src = read('app.js');
  const fn = src.slice(src.indexOf('function updateCommentCount'), src.indexOf('function updateReservedCount'));
  assert.match(fn, /agentUnresolved/);
  assert.match(fn, /if \(!c\.reserved_for_github\) agentUnresolved\+\+/);
  assert.match(fn, /const openTotal = agentUnresolved \+ hiddenUnresolved/);
  assert.doesNotMatch(fn, /const openTotal = unresolved \+ hiddenUnresolved/);
});

test('reserved chip hides at zero and is wired to the toolbar', () => {
  const src = read('app.js');
  const fn = src.slice(src.indexOf('function updateReservedCount'));
  assert.match(fn, /chip\.style\.display = n === 0 \? 'none' : ''/);
  assert.match(fn, /!c\.resolved && c\.reserved_for_github/);

  const html = read('index.html');
  assert.match(html, /id="reservedCount"/);
  assert.match(html, /id="reservedCountNum"/);
  const toolbar = html.slice(html.indexOf('id="reviewToolbar"'));
  assert.ok(toolbar.indexOf('id="reservedCount"') < toolbar.indexOf('</div>\n</div>'),
    'the reserved chip must live inside #reviewToolbar');
});

test('reserve button is git-mode only and hidden while editing', () => {
  const src = read('app.js');
  assert.match(src, /session\.mode === 'git' && !opts\.editingId && !formObj\.editingId/);
});

test('reserve styles exist for button, badge and chip', () => {
  const css = read('style.css');
  for (const sel of ['.btn-reserve', '.reserved-badge', '.review-toolbar-chip']) {
    assert.ok(css.includes(sel + ' {') || css.includes(sel + ','), `missing style for ${sel}`);
  }
});
