// crit-graph-view.js — the call-hierarchy viewer.
//
// The graph's two halves render in two different ways, and keeping them
// separate is what makes this tractable. The author's Mermaid is rendered
// verbatim (Mermaid owns layout, and crit has never parsed Mermaid); the
// metadata map is rendered by us, as decoration attached to the nodes Mermaid
// produced. The join is the node ID, which Mermaid conveniently exposes: it
// sets data-id to the node's own ID and builds the element id as
// "flowchart-<id>-<counter>", so nodeIdFromElement can recover it without
// depending on label text.
//
// Two decisions worth knowing before changing anything here:
//
//   * Status color is applied by appending `class` statements to the diagram
//     source, never by classDef. A `class` statement puts our class name on
//     the rendered <g>, so the actual colors live in style.css and follow the
//     theme; classDef would bake literal colors into inline styles and fight
//     light/dark.
//
//   * Badge markers are appended INSIDE each node's own <g>, not positioned
//     over the page. That way they inherit the node's SVG transform, so pan,
//     zoom, and the fullscreen overlay all keep them attached with no
//     repositioning code at all.

(function () {
  'use strict';

  // Status → CSS class. The vocabulary is closed on the Go side; anything
  // unrecognized falls back to "existing" rather than rendering unstyled,
  // because an uncolored node reads as a rendering bug rather than as data.
  var STATUS_CLASS = {
    existing: 'critNodeExisting',
    new: 'critNodeNew',
    modified: 'critNodeModified',
    external: 'critNodeExternal',
  };

  var STATUS_LABEL = {
    existing: 'already exists',
    new: 'net-new',
    modified: 'modified here',
    external: 'external',
  };

  function statusClass(status) {
    return STATUS_CLASS[status] || STATUS_CLASS.existing;
  }

  function statusLabel(status) {
    return STATUS_LABEL[status] || status || 'unknown';
  }

  // buildDiagramSource appends the `class` statements that color nodes by
  // status. The author's source is otherwise untouched — validation has
  // already rejected author styling, so there is nothing to reconcile.
  function buildDiagramSource(graph) {
    if (!graph || !graph.diagram) return '';
    var byClass = {};
    Object.keys(graph.nodes || {}).forEach(function (id) {
      var cls = statusClass((graph.nodes[id] || {}).status);
      (byClass[cls] = byClass[cls] || []).push(id);
    });
    var lines = [graph.diagram];
    Object.keys(byClass).sort().forEach(function (cls) {
      lines.push('  class ' + byClass[cls].join(',') + ' ' + cls);
    });
    return lines.join('\n');
  }

  // nodeIdFromElement recovers our node ID from a rendered Mermaid element.
  //
  // Mermaid namespaces every id in the SVG with the run's own id, so a node is
  // "mermaid-<runId>-flowchart-<nodeId>-<counter>" and a subgraph cluster is
  // "mermaid-<runId>-<subgraphId>" — no shared delimiter between them, and no
  // data-id on either in 11.x. Hence three strategies, cheapest first:
  //
  //   1. data-id, if a future version starts setting it on nodes.
  //   2. The "flowchart-<id>-<n>" infix, which only call nodes carry. Node IDs
  //      cannot contain a dash (validation rejects it), so anchoring on the
  //      trailing counter recovers the ID unambiguously.
  //   3. Stripping the owning SVG's id prefix, which is the only thing that
  //      identifies a cluster.
  //
  // Getting this wrong is silent — decoration simply does not attach — so the
  // e2e test asserts on decorated node count rather than trusting the parse.
  function nodeIdFromElement(el) {
    if (!el) return null;
    var dataId = el.getAttribute && el.getAttribute('data-id');
    if (dataId) return dataId;
    var domId = (el.getAttribute && el.getAttribute('id')) || '';
    if (!domId) return null;
    var m = /flowchart-(.+)-\d+$/.exec(domId);
    if (m) return m[1];
    var svg = el.ownerSVGElement;
    var prefix = svg && svg.id ? svg.id + '-' : '';
    if (prefix && domId.indexOf(prefix) === 0) {
      var rest = domId.slice(prefix.length);
      if (rest) return rest;
    }
    return null;
  }

  // copyTargetFor is what the copy button puts on the clipboard: the location
  // to paste into an editor or another agent. Line is omitted rather than
  // faked as :0 when the code does not exist yet.
  function copyTargetFor(node) {
    if (!node || !node.file) return '';
    return node.line > 0 ? node.file + ':' + node.line : node.file;
  }

  // badgesFor resolves a node's badge IDs against the legend. Unknown IDs
  // cannot reach a saved graph (validation rejects them), but a legend edited
  // by hand could produce one, so they degrade to a slate chip rather than
  // vanishing.
  function badgesFor(graph, node) {
    var legend = {};
    ((graph && graph.legend) || []).forEach(function (b) {
      legend[b.id] = b;
    });
    return ((node && node.badges) || []).map(function (id) {
      var def = legend[id];
      return {
        id: id,
        label: (def && def.label) || id,
        color: (def && def.color) || 'slate',
        group: (def && def.group) || '',
        description: (def && def.description) || '',
      };
    });
  }

  // legendGroups buckets the legend for display, preserving declaration order
  // within each group so an author controls how their vocabulary reads.
  function legendGroups(graph) {
    var order = [];
    var groups = {};
    ((graph && graph.legend) || []).forEach(function (b) {
      var key = b.group || 'other';
      if (!groups[key]) {
        groups[key] = [];
        order.push(key);
      }
      groups[key].push(b);
    });
    return order.map(function (key) {
      return { group: key, badges: groups[key] };
    });
  }

  // ===== DOM helpers =====

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text != null) node.textContent = text;
    return node;
  }

  function svgEl(tag) {
    return document.createElementNS('http://www.w3.org/2000/svg', tag);
  }

  // writeClipboard prefers the async Clipboard API but keeps the textarea
  // fallback: the API needs a secure context, and a crit review reached over
  // plain http through a reverse proxy (a documented setup) is not one.
  function writeClipboard(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.setAttribute('readonly', '');
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      var ok = false;
      try {
        ok = document.execCommand('copy');
      } catch (e) {
        ok = false;
      }
      document.body.removeChild(ta);
      ok ? resolve() : reject(new Error('copy failed'));
    });
  }

  function buildCopyButton(text, label) {
    var btn = el('button', 'crit-graph-copy');
    btn.type = 'button';
    btn.textContent = label || 'Copy path';
    btn.setAttribute('aria-label', 'Copy ' + text);
    btn.setAttribute('title', text);
    btn.addEventListener('click', function (e) {
      e.stopPropagation();
      writeClipboard(text).then(
        function () {
          var was = btn.textContent;
          btn.textContent = 'Copied';
          btn.classList.add('copied');
          setTimeout(function () {
            btn.textContent = was;
            btn.classList.remove('copied');
          }, 1200);
        },
        function () {
          btn.textContent = 'Copy failed';
        }
      );
    });
    return btn;
  }

  function buildBadgeChips(badges) {
    var row = el('div', 'crit-graph-badges');
    badges.forEach(function (b) {
      var chip = el('span', 'crit-graph-badge crit-badge-' + b.color, b.label);
      if (b.description) chip.setAttribute('title', b.description);
      row.appendChild(chip);
    });
    return row;
  }

  // buildNodeCard is both the hover card and the body of the detail panel.
  // One builder for both so the two can never drift into showing different
  // facts about the same node.
  function buildNodeCard(graph, id, node) {
    var card = el('div', 'crit-graph-card');

    var head = el('div', 'crit-graph-card-head');
    head.appendChild(el('span', 'crit-graph-card-name', (node && node.label) || id));
    head.appendChild(
      el('span', 'crit-graph-status crit-graph-status-' + ((node && node.status) || 'existing'),
        statusLabel(node && node.status))
    );
    card.appendChild(head);

    if (node && node.signature) {
      var sig = el('pre', 'crit-graph-signature');
      sig.appendChild(el('code', null, node.signature));
      card.appendChild(sig);
    }

    var target = copyTargetFor(node);
    if (target) {
      var loc = el('div', 'crit-graph-location');
      loc.appendChild(el('span', 'crit-graph-path', target));
      loc.appendChild(buildCopyButton(target, 'Copy'));
      card.appendChild(loc);
    }

    var badges = badgesFor(graph, node);
    if (badges.length) card.appendChild(buildBadgeChips(badges));

    if (node && node.note) card.appendChild(el('p', 'crit-graph-note', node.note));

    return card;
  }

  // buildCommentThread renders one comment and its replies. Replies are the
  // half that makes the loop visible: a reviewer asks a question on a node, the
  // agent answers with `crit comment --reply-to`, and without this the answer
  // existed in the review file but appeared nowhere in the graph view.
  function buildCommentThread(c) {
    var wrap = el('div', 'crit-graph-comment');
    if (c.resolved) wrap.classList.add('resolved');

    var head = el('div', 'crit-graph-comment-head');
    head.appendChild(el('span', 'crit-graph-comment-author', c.author || 'Reviewer'));
    if (c.resolved) head.appendChild(el('span', 'crit-graph-comment-resolved', 'resolved'));
    wrap.appendChild(head);
    wrap.appendChild(el('div', 'crit-graph-comment-body', c.body || ''));

    (c.replies || []).forEach(function (r) {
      var reply = el('div', 'crit-graph-reply');
      reply.appendChild(el('span', 'crit-graph-comment-author', r.author || 'Reply'));
      reply.appendChild(el('div', 'crit-graph-comment-body', r.body || ''));
      wrap.appendChild(reply);
    });
    return wrap;
  }

  // ===== Badge markers inside the SVG node =====

  var MARKER_SIZE = 7;
  var MARKER_GAP = 2;

  // attachBadgeMarkers draws one small square per badge inside the node's own
  // <g>, below its shape. Being inside the node's group is the whole trick:
  // the markers inherit its transform, so pan, zoom, and the detached
  // fullscreen clone all carry them with no repositioning.
  function attachBadgeMarkers(nodeEl, badges, box) {
    if (!badges.length || !box) return;
    if (nodeEl.querySelector('.crit-graph-marker-row')) return;
    var group = svgEl('g');
    group.setAttribute('class', 'crit-graph-marker-row');
    var totalWidth = badges.length * MARKER_SIZE + (badges.length - 1) * MARKER_GAP;
    var startX = box.x + box.width / 2 - totalWidth / 2;
    var y = box.y + box.height + MARKER_GAP;
    badges.forEach(function (b, i) {
      var rect = svgEl('rect');
      rect.setAttribute('class', 'crit-graph-marker crit-marker-' + b.color);
      rect.setAttribute('x', String(startX + i * (MARKER_SIZE + MARKER_GAP)));
      rect.setAttribute('y', String(y));
      rect.setAttribute('width', String(MARKER_SIZE));
      rect.setAttribute('height', String(MARKER_SIZE));
      rect.setAttribute('rx', '1.5');
      var title = svgEl('title');
      title.textContent = b.description ? b.label + ' — ' + b.description : b.label;
      rect.appendChild(title);
      group.appendChild(rect);
    });
    nodeEl.appendChild(group);
  }

  // shapeBox measures a node's own shape, not its <g>. Measuring the group
  // made decoration order load-bearing — every appended badge grew the box the
  // next one measured, so the comment badge's anchor drifted with the badge
  // count. The shape element never changes, so positioning off it is
  // order-independent by construction and safe to recompute at any time.
  var SHAPE_SELECTOR = '.label-container, rect, polygon, circle, ellipse, path';

  function shapeBox(nodeEl) {
    var shape = nodeEl.querySelector && nodeEl.querySelector(SHAPE_SELECTOR);
    var target = shape || nodeEl;
    if (!target.getBBox) return null;
    try {
      return target.getBBox();
    } catch (e) {
      return null;
    }
  }

  // ===== The view =====

  var state = {
    graph: null,
    comments: {},
    selected: null,
    hoverCard: null,
  };

  function commentsFor(id) {
    return (state.comments && state.comments[id]) || [];
  }

  function unresolvedCount(id) {
    return commentsFor(id).filter(function (c) {
      return !c.resolved;
    }).length;
  }

  // attachCommentBadge marks a node that already carries feedback, so a
  // reviewer returning to a graph can see where the conversation is without
  // opening every node.
  function attachCommentBadge(nodeEl, id, box) {
    var n = commentsFor(id).length;
    if (!n || !box) return;
    if (nodeEl.querySelector('.crit-graph-comment-badge')) return;
    var group = svgEl('g');
    group.setAttribute('class', 'crit-graph-comment-badge');
    if (unresolvedCount(id) > 0) group.setAttribute('data-unresolved', 'true');
    var circle = svgEl('circle');
    circle.setAttribute('cx', String(box.x + box.width));
    circle.setAttribute('cy', String(box.y));
    circle.setAttribute('r', '8');
    group.appendChild(circle);
    var text = svgEl('text');
    text.setAttribute('x', String(box.x + box.width));
    text.setAttribute('y', String(box.y));
    text.setAttribute('dy', '3.5');
    text.setAttribute('text-anchor', 'middle');
    text.textContent = String(n);
    group.appendChild(text);
    nodeEl.appendChild(group);
  }

  // HOVER_GRACE_MS bridges the gap between the node and its card. Without it
  // the card is unusable rather than merely awkward: it carries a copy button,
  // and reaching for the button leaves the node, which dismissed the card.
  var HOVER_GRACE_MS = 220;
  var hoverHideTimer = null;

  function cancelHoverHide() {
    if (hoverHideTimer) {
      clearTimeout(hoverHideTimer);
      hoverHideTimer = null;
    }
  }

  function scheduleHoverHide() {
    cancelHoverHide();
    hoverHideTimer = setTimeout(hideHoverCard, HOVER_GRACE_MS);
  }

  function showHoverCard(nodeEl, id) {
    cancelHoverHide();
    hideHoverCard();
    var node = (state.graph.nodes || {})[id];
    if (!node) return;
    var card = buildNodeCard(state.graph, id, node);
    card.classList.add('crit-graph-hovercard');
    card.addEventListener('mouseenter', cancelHoverHide);
    card.addEventListener('mouseleave', hideHoverCard);
    document.body.appendChild(card);
    var rect = nodeEl.getBoundingClientRect();
    var cardRect = card.getBoundingClientRect();
    // Flip above the node when there is no room below, so a node near the
    // bottom of a tall diagram still shows its card.
    var top = rect.bottom + 8;
    if (top + cardRect.height > window.innerHeight - 8) {
      top = Math.max(8, rect.top - cardRect.height - 8);
    }
    var left = Math.min(
      Math.max(8, rect.left + rect.width / 2 - cardRect.width / 2),
      window.innerWidth - cardRect.width - 8
    );
    card.style.top = top + 'px';
    card.style.left = left + 'px';
    state.hoverCard = card;
  }

  function hideHoverCard() {
    cancelHoverHide();
    if (state.hoverCard && state.hoverCard.parentNode) {
      state.hoverCard.parentNode.removeChild(state.hoverCard);
    }
    state.hoverCard = null;
  }

  // decorateNodes covers both call nodes (g.node) and module subgraphs
  // (g.cluster). Modules are part of what a reader hovers, so a subgraph with
  // a `nodes` entry gets the same card and comment thread a function does.
  function decorateNodes(container) {
    var nodes = container.querySelectorAll('g.node, g.cluster');
    Array.prototype.forEach.call(nodes, function (nodeEl) {
      var id = nodeIdFromElement(nodeEl);
      if (!id) return;
      var node = (state.graph.nodes || {})[id];
      if (!node) return;

      nodeEl.classList.add('crit-graph-node');
      nodeEl.setAttribute('tabindex', '0');
      nodeEl.setAttribute('role', 'button');
      nodeEl.setAttribute('aria-label', id + ' — ' + statusLabel(node.status));

      var box = shapeBox(nodeEl);
      attachBadgeMarkers(nodeEl, badgesFor(state.graph, node), box);
      attachCommentBadge(nodeEl, id, box);

      nodeEl.addEventListener('mouseenter', function () {
        showHoverCard(nodeEl, id);
      });
      nodeEl.addEventListener('mouseleave', scheduleHoverHide);
      nodeEl.addEventListener('focus', function () {
        showHoverCard(nodeEl, id);
      });
      nodeEl.addEventListener('blur', hideHoverCard);
      nodeEl.addEventListener('click', function (e) {
        e.stopPropagation();
        hideHoverCard();
        selectNode(id);
      });
      nodeEl.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          hideHoverCard();
          selectNode(id);
        }
      });
    });
  }

  function selectNode(id) {
    state.selected = id;
    renderPanel();
  }

  // refreshCommentBadge updates one node's badge in place. Re-rendering the
  // whole view to reflect a new comment would re-run Mermaid, rebuilding the
  // SVG and throwing away the reader's scroll position — an expensive way to
  // change one number.
  function refreshCommentBadge(nodeID) {
    var nodes = document.querySelectorAll('.crit-graph-diagram g.node, .crit-graph-diagram g.cluster');
    Array.prototype.forEach.call(nodes, function (nodeEl) {
      if (nodeIdFromElement(nodeEl) !== nodeID) return;
      var existing = nodeEl.querySelector('.crit-graph-comment-badge');
      if (existing) existing.parentNode.removeChild(existing);
      attachCommentBadge(nodeEl, nodeID, shapeBox(nodeEl));
    });
  }

  var hooks = {
    // postComment(nodeId, body) -> Promise. Injected by app.js so this module
    // stays free of transport and auth concerns.
    postComment: null,
    renderCommentCard: null,
  };

  function renderPanel() {
    var panel = document.getElementById('graphNodePanel');
    if (!panel) return;
    panel.innerHTML = '';
    var id = state.selected;
    if (!id || !state.graph) {
      panel.classList.remove('open');
      return;
    }
    var node = (state.graph.nodes || {})[id];
    if (!node) {
      panel.classList.remove('open');
      return;
    }
    panel.classList.add('open');

    var close = el('button', 'crit-graph-panel-close', '×');
    close.type = 'button';
    close.setAttribute('aria-label', 'Close node details');
    close.addEventListener('click', function () {
      state.selected = null;
      renderPanel();
    });
    panel.appendChild(close);

    panel.appendChild(buildNodeCard(state.graph, id, node));

    var thread = el('div', 'crit-graph-thread');
    var existing = commentsFor(id);
    if (existing.length) {
      thread.appendChild(el('h4', 'crit-graph-thread-title', 'Comments'));
      existing.forEach(function (c) {
        if (hooks.renderCommentCard) {
          var card = hooks.renderCommentCard(c);
          if (card) {
            thread.appendChild(card);
            return;
          }
        }
        thread.appendChild(buildCommentThread(c));
      });
    }
    panel.appendChild(thread);

    var form = el('form', 'crit-graph-comment-form');
    var ta = el('textarea');
    ta.placeholder = 'Comment on ' + id + '…';
    ta.rows = 3;
    form.appendChild(ta);
    var submit = el('button', 'btn btn-primary btn-sm', 'Comment');
    submit.type = 'submit';
    form.appendChild(submit);
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      var body = ta.value.trim();
      if (!body || !hooks.postComment) return;
      submit.disabled = true;
      hooks.postComment(id, body).then(
        function () {
          ta.value = '';
          submit.disabled = false;
        },
        function () {
          submit.disabled = false;
        }
      );
    });
    panel.appendChild(form);
  }

  // attachExpandButton reuses the review page's fullscreen pan/zoom overlay.
  // A call hierarchy is the diagram most likely to outgrow its container, and
  // the alternative to reuse is a second pan/zoom implementation.
  function attachExpandButton(wrap) {
    if (!window.crit || !window.crit.openMermaidOverlay) return;
    if (wrap.querySelector('.crit-graph-expand')) return;
    var svg = wrap.querySelector('.crit-graph-diagram svg');
    if (!svg) return;
    var btn = el('button', 'crit-graph-expand', '\u26F6 Expand');
    btn.type = 'button';
    btn.setAttribute('aria-label', 'Open the call hierarchy fullscreen');
    btn.setAttribute('title', 'Open fullscreen');
    btn.addEventListener('click', function (e) {
      e.stopPropagation();
      var current = wrap.querySelector('.crit-graph-diagram svg');
      if (current) window.crit.openMermaidOverlay(current, btn);
    });
    wrap.appendChild(btn);
  }

  function renderLegend(container) {
    var graph = state.graph;
    var wrap = el('div', 'crit-graph-legend');

    var statuses = el('div', 'crit-graph-legend-group');
    statuses.appendChild(el('span', 'crit-graph-legend-label', 'status'));
    ['existing', 'new', 'modified', 'external'].forEach(function (s) {
      var item = el('span', 'crit-graph-legend-item');
      item.appendChild(el('span', 'crit-graph-swatch crit-graph-status-' + s));
      item.appendChild(el('span', null, statusLabel(s)));
      statuses.appendChild(item);
    });
    wrap.appendChild(statuses);

    legendGroups(graph).forEach(function (g) {
      var row = el('div', 'crit-graph-legend-group');
      row.appendChild(el('span', 'crit-graph-legend-label', g.group));
      g.badges.forEach(function (b) {
        var item = el('span', 'crit-graph-legend-item');
        item.appendChild(el('span', 'crit-graph-swatch crit-marker-' + b.color));
        var text = el('span', null, b.label);
        if (b.description) text.setAttribute('title', b.description);
        item.appendChild(text);
        row.appendChild(item);
      });
      wrap.appendChild(row);
    });

    container.appendChild(wrap);
  }

  // renderNotices surfaces what the validator told the author: the warnings it
  // let through, and the drift between this graph and the one it replaced. A
  // reviewer needs both — a thin node and an unplanned function are exactly
  // the things worth a human look.
  function renderNotices(container) {
    var d = state.graph && state.graph.diagnostics;
    if (!d) return;

    if (d.drift) {
      var drift = el('div', 'crit-graph-notice crit-graph-drift');
      drift.appendChild(el('h4', null, 'Changed since the previous graph'));
      var list = el('ul');
      (d.drift.added || []).forEach(function (id) {
        list.appendChild(el('li', 'crit-drift-added', id + ' — appeared (was not planned)'));
      });
      (d.drift.removed || []).forEach(function (id) {
        list.appendChild(el('li', 'crit-drift-removed', id + ' — gone (was planned)'));
      });
      (d.drift.status_changed || []).forEach(function (s) {
        list.appendChild(el('li', 'crit-drift-changed', s));
      });
      drift.appendChild(list);
      container.appendChild(drift);
    }

    var warnings = (d.items || []).filter(function (it) {
      return it.severity === 'warning';
    });
    if (warnings.length) {
      var box = el('details', 'crit-graph-notice crit-graph-warnings');
      box.appendChild(el('summary', null, warnings.length + ' authoring warning(s)'));
      var ul = el('ul');
      warnings.forEach(function (it) {
        ul.appendChild(el('li', null, (it.node ? it.node + ': ' : '') + it.message));
      });
      box.appendChild(ul);
      container.appendChild(box);
    }
  }

  function renderHeader(container) {
    var g = state.graph;
    var head = el('div', 'crit-graph-header');
    head.appendChild(el('h2', 'crit-graph-title', g.title || 'Call hierarchy'));
    var meta = el('div', 'crit-graph-meta');
    // Stats normally come from the ingest's diagnostics; fall back to counting
    // the metadata map so a graph written without them does not read "0 nodes".
    var st = (g.diagnostics && g.diagnostics.stats) || {};
    var nodeCount = st.nodes || Object.keys(g.nodes || {}).length;
    meta.appendChild(el('span', 'crit-graph-phase', g.phase === 'review' ? 'after implementation' : 'plan'));
    meta.appendChild(el('span', null, nodeCount + (nodeCount === 1 ? ' node' : ' nodes')));
    if (st.new) meta.appendChild(el('span', null, st.new + ' net-new'));
    if (st.modified) meta.appendChild(el('span', null, st.modified + ' modified'));
    head.appendChild(meta);
    if (g.summary) head.appendChild(el('p', 'crit-graph-summary', g.summary));
    container.appendChild(head);
  }

  // render draws the whole view. Mermaid is initialized with htmlLabels so
  // author labels keep their formatting, and with startOnLoad off because we
  // drive the run ourselves after inserting the source.
  function render(container, graph, comments, opts) {
    if (!container) return;
    var keep = opts && opts.keepSelection ? state.selected : null;
    state.graph = graph;
    state.comments = comments || {};
    state.selected = null;
    hideHoverCard();
    container.innerHTML = '';
    if (!graph) {
      container.appendChild(el('p', 'crit-graph-empty', 'No call-hierarchy graph on this review.'));
      return;
    }

    renderHeader(container);
    renderNotices(container);
    renderLegend(container);

    var diagramWrap = el('div', 'crit-graph-diagram-wrap');
    // No `mermaid` class on purpose: this element is rendered by passing it to
    // mermaid.run({nodes}) explicitly, and the class would also enrol it in
    // every bare mermaid.run() the review page makes for markdown diagrams.
    var diagram = el('div', 'crit-graph-diagram');
    diagram.textContent = buildDiagramSource(graph);
    diagramWrap.appendChild(diagram);
    container.appendChild(diagramWrap);

    var panel = el('div', 'crit-graph-panel');
    panel.id = 'graphNodePanel';
    container.appendChild(panel);

    if (typeof mermaid === 'undefined') return;
    try {
      // useMaxWidth MUST stay false. The default scales the SVG down to the
      // container width, which for a left-to-right call hierarchy means an
      // 18-node graph rendered as unreadable slivers with no visible labels
      // (5400px of diagram squeezed into 840px). At natural size the wrapper
      // scrolls instead, and the expand button below gives the whole picture.
      mermaid.initialize({
        startOnLoad: false,
        theme: (window.crit && window.crit.mermaidTheme && window.crit.mermaidTheme()) || 'default',
        flowchart: { htmlLabels: true, useMaxWidth: false },
      });
      var finish = function () {
        decorateNodes(diagramWrap);
        attachExpandButton(diagramWrap);
        if (keep && (state.graph.nodes || {})[keep]) selectNode(keep);
      };
      var run = mermaid.run({ nodes: [diagram] });
      if (run && typeof run.then === 'function') {
        run.then(finish, function () {});
      } else {
        finish();
      }
    } catch (e) {
      diagramWrap.appendChild(el('p', 'crit-graph-error', 'Could not render the diagram.'));
    }
  }

  function setHooks(next) {
    hooks.postComment = (next && next.postComment) || null;
    hooks.renderCommentCard = (next && next.renderCommentCard) || null;
  }

  function setComments(comments) {
    state.comments = comments || {};
    if (state.selected) renderPanel();
  }

  var api = {
    STATUS_CLASS: STATUS_CLASS,
    STATUS_LABEL: STATUS_LABEL,
    statusClass: statusClass,
    statusLabel: statusLabel,
    buildDiagramSource: buildDiagramSource,
    nodeIdFromElement: nodeIdFromElement,
    copyTargetFor: copyTargetFor,
    badgesFor: badgesFor,
    legendGroups: legendGroups,
    buildNodeCard: buildNodeCard,
    render: render,
    renderPanel: renderPanel,
    selectNode: selectNode,
    refreshCommentBadge: refreshCommentBadge,
    setHooks: setHooks,
    setComments: setComments,
    hideHoverCard: hideHoverCard,
    attachExpandButton: attachExpandButton,
    _state: state,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.graphView = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
