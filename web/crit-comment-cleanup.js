// crit-comment-cleanup.js — review-toolbar action that turns "the agent left
// too many comments in this diff" into a single top-level review comment.
//
// The prompt text mirrors the comment-density PostToolUse hook: same keep/cut
// rules, same hard exemption for directive and suppression comments (rewording
// those changes behavior). The slider only widens the "remove" list — the keep
// rules are constant, so a higher level can never turn into "strip everything".

(function () {
  'use strict';

  // Kept verbatim at every level: removing or rewording a directive changes
  // behavior, and the WHY comments are the ones worth having in the first place.
  var ALWAYS_KEEP = [
    'Comments that explain a non-obvious WHY the code cannot show: a constraint, an ' +
      'invariant, a gotcha, or the reason a workaround exists.',
    'Directive and suppression comments — eslint-disable, nolint, noqa, ts-expect-error, ' +
      'golangci, shellcheck, clippy, type: ignore, @generated, "do not edit", SPDX headers. ' +
      'Removing or rewording these changes behavior.',
  ];

  var KEEP_PUBLIC_DOCS =
    "Doc comments on exported or public API where the language's tooling expects them.";

  // 1-based level; `remove` lists are additive — level N applies its own
  // bullets plus every lower level's.
  var LEVELS = [
    {
      id: 'light',
      label: 'Light',
      summary: 'Cut only the comments that say what the code already says.',
      remove: [
        'Comments that restate what the next line or two does in different words.',
      ],
    },
    {
      id: 'moderate',
      label: 'Moderate',
      summary: 'Also cut decorative and label-only comments.',
      remove: [
        'Decorative separators and section banners that carry no information.',
        'Trailing end-of-line comments that just name the value being assigned.',
      ],
    },
    {
      id: 'balanced',
      label: 'Balanced',
      summary: 'Also cut step-by-step narration and untracked notes.',
      remove: [
        'Narration that walks through a sequence ("first we…", "then…") the code already reads as.',
        "TODO and FIXME notes added in this change that aren't actionable or tracked anywhere.",
      ],
    },
    {
      id: 'aggressive',
      label: 'Aggressive',
      summary: 'Also cut redundancy with names, types, and tests.',
      remove: [
        'Multi-line explanations of straightforward code — collapse to one short line, or cut.',
        'Comments that repeat information already carried by a nearby name, type, or test.',
      ],
    },
    {
      id: 'maximum',
      label: 'Maximum',
      summary: 'Keep only WHY comments and directives. When in doubt, cut.',
      dropPublicDocs: true,
      remove: [
        'Anything a reader fluent in the language would not need. When in doubt at this level, cut.',
      ],
    },
  ];

  var DEFAULT_LEVEL = 3; // 1-based → "Balanced"

  function clampLevel(level) {
    var n = Math.round(Number(level));
    if (!isFinite(n)) return DEFAULT_LEVEL;
    if (n < 1) return 1;
    if (n > LEVELS.length) return LEVELS.length;
    return n;
  }

  function levelAt(level) {
    return LEVELS[clampLevel(level) - 1];
  }

  function bullets(items) {
    return items
      .map(function (item) {
        return '- ' + item;
      })
      .join('\n');
  }

  function buildCleanupPrompt(level) {
    var n = clampLevel(level);
    var spec = LEVELS[n - 1];

    var keep = ALWAYS_KEEP.slice();
    if (!spec.dropPublicDocs) keep.push(KEEP_PUBLIC_DOCS);

    var remove = [];
    for (var i = 0; i < n; i++) {
      remove = remove.concat(LEVELS[i].remove);
    }

    return [
      '**Clean up the code comments in this change.**',
      '',
      'This is a comments-only pass: do not change behavior, and do not reformat or rename anything.',
      'Only touch comments this change added or modified — leave pre-existing comments in these files alone.',
      '',
      'Level ' + n + '/' + LEVELS.length + ' — ' + spec.label + '. ' + spec.summary,
      '',
      'Keep:',
      bullets(keep),
      '',
      'Remove:',
      bullets(remove),
      '',
      'Where a comment still earns its place but runs long, collapse it to a single short line.',
      'When you are done, say how many comment lines you removed and name any you deliberately kept.',
    ].join('\n');
  }

  var host = null; // injected { addReviewComment, finishReview, showToast }
  var lastFocused = null;
  var SETTING_KEY = 'commentCleanupLevel';

  function shared() {
    return typeof window !== 'undefined' && window.crit ? window.crit.shared : null;
  }

  function savedLevel() {
    var sh = shared();
    return clampLevel(sh && sh.getSetting ? sh.getSetting(SETTING_KEY, DEFAULT_LEVEL) : DEFAULT_LEVEL);
  }

  function el(id) {
    return typeof document === 'undefined' ? null : document.getElementById(id);
  }

  function syncPreview() {
    var slider = el('cleanupLevel');
    if (!slider) return;
    var n = clampLevel(slider.value);
    var spec = LEVELS[n - 1];

    var label = el('cleanupLevelLabel');
    if (label) label.textContent = spec.label;

    var summary = el('cleanupLevelSummary');
    if (summary) summary.textContent = spec.summary;

    var preview = el('cleanupPromptPreview');
    if (preview) preview.textContent = buildCleanupPrompt(n);

    slider.setAttribute('aria-valuetext', 'Level ' + n + ' of ' + LEVELS.length + ': ' + spec.label);
  }

  function persistLevel() {
    var slider = el('cleanupLevel');
    var sh = shared();
    if (!slider || !sh || !sh.setSetting) return;
    try { sh.setSetting(SETTING_KEY, clampLevel(slider.value)); } catch (_) {}
  }

  function isOpen() {
    var overlay = el('cleanupOverlay');
    return !!overlay && overlay.classList.contains('active');
  }

  function open() {
    var overlay = el('cleanupOverlay');
    if (!overlay || isOpen()) return;
    lastFocused = document.activeElement;
    overlay.classList.add('active');
    var slider0 = el('cleanupLevel');
    if (slider0) slider0.value = String(savedLevel());
    syncPreview();
    var slider = el('cleanupLevel');
    if (slider) slider.focus();
  }

  function close() {
    var overlay = el('cleanupOverlay');
    if (!overlay || !isOpen()) return;
    overlay.classList.remove('active');
    setBusy(false);
    if (lastFocused && typeof lastFocused.focus === 'function') lastFocused.focus();
    lastFocused = null;
  }

  function setBusy(busy) {
    var ids = ['cleanupAddBtn', 'cleanupAddSubmitBtn', 'cleanupCancelBtn', 'cleanupLevel'];
    for (var i = 0; i < ids.length; i++) {
      var node = el(ids[i]);
      if (node) node.disabled = !!busy;
    }
  }

  var submitting = false;
  async function apply(alsoSubmit) {
    if (submitting || !host) return;
    var slider = el('cleanupLevel');
    var prompt = buildCleanupPrompt(slider ? slider.value : DEFAULT_LEVEL);

    submitting = true;
    setBusy(true);
    var added = null;
    try {
      added = await host.addReviewComment(prompt);
    } catch (err) {
      console.error('Comment cleanup: failed to add review comment', err);
    }
    submitting = false;

    if (!added) {
      setBusy(false);
      if (host.showToast) host.showToast('Failed to add cleanup comment');
      return;
    }

    close();
    if (alsoSubmit) {
      await host.finishReview();
    } else if (host.showToast) {
      host.showToast('Cleanup comment added');
    }
  }

  function init(deps) {
    if (typeof document === 'undefined') return;
    host = deps || null;

    var btn = el('cleanupCommentsBtn');
    if (btn) btn.addEventListener('click', open);

    var slider = el('cleanupLevel');
    if (slider) {
      slider.value = String(savedLevel());
      slider.addEventListener('input', syncPreview);
      slider.addEventListener('change', persistLevel);
    }

    var cancel = el('cleanupCancelBtn');
    if (cancel) cancel.addEventListener('click', close);

    var closeX = el('cleanupCloseBtn');
    if (closeX) closeX.addEventListener('click', close);

    var add = el('cleanupAddBtn');
    if (add) {
      add.addEventListener('click', function () {
        apply(false);
      });
    }

    var addSubmit = el('cleanupAddSubmitBtn');
    if (addSubmit) {
      addSubmit.addEventListener('click', function () {
        apply(true);
      });
    }

    var overlay = el('cleanupOverlay');
    if (overlay) {
      overlay.addEventListener('click', function (e) {
        if (e.target === overlay) close();
      });
    }

    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && isOpen()) {
        e.stopPropagation();
        close();
      }
    });

    syncPreview();
  }

  var api = {
    LEVELS: LEVELS,
    DEFAULT_LEVEL: DEFAULT_LEVEL,
    buildCleanupPrompt: buildCleanupPrompt,
    clampLevel: clampLevel,
    levelAt: levelAt,
    init: init,
    open: open,
    close: close,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.commentCleanup = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
