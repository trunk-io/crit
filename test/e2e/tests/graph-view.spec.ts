import { test, expect, type APIRequestContext } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// A graph whose node IDs, statuses, and badges are all distinct, so an
// assertion can tell them apart in the rendered SVG.
const GRAPH = {
  title: 'Token minting path',
  phase: 'plan',
  summary: 'How a request reaches the signer.',
  diagram: [
    'flowchart LR',
    '  subgraph edgeMod["edge"]',
    '    handleReq["handleReq"]',
    '  end',
    '  subgraph coreMod["core"]',
    '    resolveOrg["resolveOrg"]',
    '    mintToken["mintToken"]',
    '  end',
    '  handleReq --> resolveOrg',
    '  resolveOrg --> mintToken',
    '  mintToken -.-> signKms',
  ].join('\n'),
  nodes: {
    edgeMod: { kind: 'module', status: 'existing', file: 'src/edge' },
    coreMod: { kind: 'module', status: 'new', file: 'src/core' },
    handleReq: {
      signature: 'export async function handleReq(r: Request): Promise<Response>',
      file: 'src/edge/handler.ts', line: 42, status: 'modified', badges: ['integration'],
    },
    resolveOrg: {
      signature: 'export async function resolveOrg(t: string): Promise<Org | null>',
      file: 'src/core/org.ts', line: 18, status: 'existing', badges: ['unit'],
    },
    mintToken: {
      signature: 'export async function mintToken(o: Org, r: Role): Promise<string>',
      file: 'src/core/token.ts', status: 'new', badges: ['unit', 'attention'],
      note: 'Signing key handling is worth a careful read.',
    },
    signKms: { kind: 'external', status: 'external' },
  },
  legend: [
    { id: 'integration', label: 'integration', color: 'teal', group: 'tests', description: 'msw + seeded db' },
    { id: 'unit', label: 'unit', color: 'blue', group: 'tests' },
    { id: 'attention', label: 'read closely', color: 'red', group: 'attention', style: 'outline' },
  ],
};

async function seedGraph(request: APIRequestContext, graph: unknown = GRAPH) {
  const res = await request.post('/api/graph', { data: { graph } });
  expect(res.status(), await res.text()).toBe(200);
}

async function clearGraph(request: APIRequestContext) {
  await request.delete('/api/graph');
}

test.describe('Call-hierarchy graph view', () => {
  test.beforeEach(async ({ request, page }) => {
    // Node comments live in the review-comment store and survive a graph
    // clear by design, so they leak between tests unless each starts clean.
    await clearAllComments(request);
    await seedGraph(request);
    await loadPage(page);
    await page.locator('#graphViewToggle .toggle-btn[data-graph-view="graph"]').click();
    await expect(page.locator('.crit-graph-diagram svg')).toBeVisible();
  });

  test.afterEach(async ({ request }) => {
    await clearGraph(request);
  });

  test('the toggle only appears when a graph is present', async ({ request, page }) => {
    await expect(page.locator('#graphViewToggle')).toBeVisible();
    await clearGraph(request);
    await loadPage(page);
    await expect(page.locator('#graphViewToggle')).toBeHidden();
  });

  // This is the assertion that protects the riskiest assumption in the whole
  // feature: that we can recover our node ID from Mermaid's rendered SVG.
  // Mermaid namespaces element ids with its run id and sets no data-id, so a
  // version bump that reshapes them would silently leave every node
  // undecorated — no error, just a diagram with no hover cards. Asserting on
  // the decorated count makes that a failing test instead.
  test('every node in the metadata map is decorated', async ({ page }) => {
    const nodes = page.locator('.crit-graph-diagram g.node');
    await expect(nodes).toHaveCount(4);
    await expect(page.locator('.crit-graph-diagram g.node.crit-graph-node')).toHaveCount(4);
  });

  test('node color follows status', async ({ page }) => {
    const diagram = page.locator('.crit-graph-diagram');
    await expect(diagram.locator('g.node.critNodeModified')).toHaveCount(1);
    await expect(diagram.locator('g.node.critNodeExisting')).toHaveCount(1);
    await expect(diagram.locator('g.node.critNodeNew')).toHaveCount(1);
    await expect(diagram.locator('g.node.critNodeExternal')).toHaveCount(1);
    // Module subgraphs are colored by their own status too.
    await expect(diagram.locator('g.cluster.critNodeNew')).toHaveCount(1);
  });

  // Badges must be readable on the node itself, not only in the hover card.
  // Coloured blocks needed a hover to decode; a labelled chip does not.
  test('badges render as labelled chips on the node', async ({ page }) => {
    const mint = page.locator('.crit-graph-diagram g.node', { hasText: 'mintToken' }).first();
    // mintToken carries `unit` (a chip) and `attention` (an outline), so one
    // chip and one ring — not two chips.
    await expect(mint.locator('.crit-graph-chip')).toHaveCount(1);
    await expect(mint.locator('.crit-graph-chip text')).toHaveText('unit');
    await expect(mint.locator('.crit-graph-outline')).toHaveCount(1);

    // An external node carries no badges, so neither channel appears.
    const kms = page.locator('.crit-graph-diagram g.node', { hasText: 'signKms' }).first();
    await expect(kms.locator('.crit-graph-chip')).toHaveCount(0);
    await expect(kms.locator('.crit-graph-outline')).toHaveCount(0);
  });

  // "Look at this" gets its own channel: a ring around the node rather than
  // one more chip competing with the chips beside it. It must not steal the
  // status colour, which the node's own border carries.
  test('an attention badge rings the node in its own colour', async ({ page }) => {
    const mint = page.locator('.crit-graph-diagram g.node', { hasText: 'mintToken' }).first();
    const colours = await mint.evaluate((el) => {
      const ring = el.querySelector('.crit-graph-outline') as SVGElement;
      const shape = el.querySelector('rect.label-container, rect:not(.crit-graph-outline)') as SVGElement;
      return {
        ring: getComputedStyle(ring).stroke,
        ringFill: getComputedStyle(ring).fill,
        shape: getComputedStyle(shape).stroke,
      };
    });
    expect(colours.ring).not.toBe(colours.shape);
    expect(colours.ringFill).toBe('none');
    // handleReq has no attention badge, so no ring.
    const handle = page.locator('.crit-graph-diagram g.node', { hasText: 'handleReq' }).first();
    await expect(handle.locator('.crit-graph-outline')).toHaveCount(0);
  });

  // A reader who guesses the direction wrong reads the call tree backwards,
  // so the direction is stated and the starting nodes are named.
  test('the read direction and entry points are stated', async ({ page }) => {
    const axis = page.locator('.crit-graph-axis');
    await expect(axis).toBeVisible();
    await expect(axis).toContainText('callers on the left');
    // handleReq is the only node nothing points at.
    const entries = axis.locator('.crit-graph-entry-link');
    await expect(entries).toHaveCount(1);
    await expect(entries.first()).toHaveText('handleReq');
    // And it is marked on the node itself.
    const handle = page.locator('.crit-graph-diagram g.node', { hasText: 'handleReq' }).first();
    await expect(handle.locator('.crit-graph-entry-marker')).toHaveCount(1);

    // Clicking the entry opens it, so the bar is a way in, not just a caption.
    await entries.first().click();
    await expect(page.locator('#graphNodePanel')).toHaveClass(/open/);
    await expect(page.locator('#graphNodePanel .crit-graph-card-name')).toHaveText('handleReq');
  });

  test('hovering a node shows its signature, path, and badges', async ({ page }) => {
    await page.locator('.crit-graph-diagram g.node', { hasText: 'mintToken' }).first().hover();
    const card = page.locator('.crit-graph-hovercard');
    await expect(card).toBeVisible();
    await expect(card.locator('.crit-graph-signature')).toContainText('mintToken(o: Org, r: Role)');
    await expect(card.locator('.crit-graph-path')).toHaveText('src/core/token.ts');
    await expect(card.locator('.crit-graph-badge')).toHaveCount(2);
    await expect(card.locator('.crit-graph-note')).toContainText('careful read');
  });

  // A planned function has no line yet, so the copy target must be the bare
  // path rather than a fabricated :0.
  test('the copy target includes the line only when one is known', async ({ page }) => {
    await page.locator('.crit-graph-diagram g.node', { hasText: 'handleReq' }).first().hover();
    await expect(page.locator('.crit-graph-hovercard .crit-graph-path')).toHaveText('src/edge/handler.ts:42');

    await page.locator('.crit-graph-diagram g.node', { hasText: 'mintToken' }).first().hover();
    await expect(page.locator('.crit-graph-hovercard .crit-graph-path')).toHaveText('src/core/token.ts');
  });

  test('the copy button puts the location on the clipboard', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    await page.locator('.crit-graph-diagram g.node', { hasText: 'handleReq' }).first().click();
    const panel = page.locator('#graphNodePanel');
    await expect(panel).toHaveClass(/open/);
    await panel.locator('.crit-graph-copy').first().click();
    await expect(panel.locator('.crit-graph-copy').first()).toHaveText('Copied');
    const clip = await page.evaluate(() => navigator.clipboard.readText());
    expect(clip).toBe('src/edge/handler.ts:42');
  });

  test('clicking a node opens its detail panel', async ({ page }) => {
    await page.locator('.crit-graph-diagram g.node', { hasText: 'resolveOrg' }).first().click();
    const panel = page.locator('#graphNodePanel');
    await expect(panel).toHaveClass(/open/);
    await expect(panel.locator('.crit-graph-card-name')).toHaveText('resolveOrg');
    await panel.locator('.crit-graph-panel-close').click();
    await expect(panel).not.toHaveClass(/open/);
  });

  test('a node is reachable and openable by keyboard', async ({ page }) => {
    const node = page.locator('.crit-graph-diagram g.node', { hasText: 'resolveOrg' }).first();
    await node.focus();
    await expect(page.locator('.crit-graph-hovercard')).toBeVisible();
    await node.press('Enter');
    await expect(page.locator('#graphNodePanel')).toHaveClass(/open/);
  });

  test('the legend lists statuses and every badge', async ({ page }) => {
    const legend = page.locator('.crit-graph-legend');
    await expect(legend).toBeVisible();
    await expect(legend).toContainText('already exists');
    await expect(legend).toContainText('net-new');
    await expect(legend).toContainText('integration');
    await expect(legend).toContainText('read closely');
  });

  test('a comment on a node persists and marks the node', async ({ page }) => {
    await page.locator('.crit-graph-diagram g.node', { hasText: 'mintToken' }).first().click();
    const panel = page.locator('#graphNodePanel');
    await panel.locator('textarea').fill('Where does the signing key come from?');
    await panel.locator('.crit-graph-comment-form button[type="submit"]').click();

    await expect(panel.locator('.crit-graph-comment')).toHaveCount(1);
    await expect(panel.locator('.crit-graph-comment-body')).toContainText('signing key');
    // The node itself gains a count badge, so a returning reviewer can see
    // where the conversation is without opening every node.
    const mint = page.locator('.crit-graph-diagram g.node', { hasText: 'mintToken' }).first();
    await expect(mint.locator('.crit-graph-comment-badge')).toHaveCount(1);

    // And it survives a reload — the comment went to the review file.
    await loadPage(page);
    await page.locator('#graphViewToggle .toggle-btn[data-graph-view="graph"]').click();
    await page.locator('.crit-graph-diagram g.node', { hasText: 'mintToken' }).first().click();
    await expect(page.locator('#graphNodePanel .crit-graph-comment-body')).toContainText('signing key');
  });

  // The copy button lives in the hover card too, and the card sits in a gap
  // away from the node — without a grace period, reaching for the button
  // dismissed the card, making it dead UI.
  test('the hover card survives the pointer moving onto it', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    await page.locator('.crit-graph-diagram g.node', { hasText: 'handleReq' }).first().hover();
    const copy = page.locator('.crit-graph-hovercard .crit-graph-copy');
    await expect(copy).toBeVisible();
    await copy.hover();
    await expect(page.locator('.crit-graph-hovercard')).toBeVisible();
    await copy.click();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('src/edge/handler.ts:42');
  });

  // The visible half of the round trip: a reviewer asks on a node, the agent
  // answers with `crit comment --reply-to`, and the answer has to appear here
  // or the loop is invisible from the browser.
  test('a reply to a node comment is shown in the panel', async ({ page }) => {
    await page.locator('.crit-graph-diagram g.node', { hasText: 'mintToken' }).first().click();
    const panel = page.locator('#graphNodePanel');
    await panel.locator('textarea').fill('Where does the signing key come from?');
    await panel.locator('.crit-graph-comment-form button[type="submit"]').click();
    await expect(panel.locator('.crit-graph-comment')).toHaveCount(1);
    await expect(panel.locator('.crit-graph-comment-body')).toHaveText('Where does the signing key come from?');

    const id = await page.evaluate(() => {
      const s = (window as any).crit.graphView._state;
      return s.comments.mintToken[s.comments.mintToken.length - 1].id;
    });
    const res = await page.request.post(`/api/review-comment/${id}/replies`, {
      data: { body: 'It comes from the KMS alias.', author: 'Claude Code' },
    });
    expect(res.ok(), await res.text()).toBeTruthy();

    await loadPage(page);
    await page.locator('#graphViewToggle .toggle-btn[data-graph-view="graph"]').click();
    await page.locator('.crit-graph-diagram g.node', { hasText: 'mintToken' }).first().click();
    const reply = page.locator('#graphNodePanel .crit-graph-reply');
    await expect(reply).toHaveCount(1);
    await expect(reply).toContainText('KMS alias');
  });

  test('drift and warnings from the ingest are shown to the reviewer', async ({ request, page }) => {
    // Re-ingest with a status flipped and a node dropped, which is what the
    // review-phase authoring looks like.
    const next = JSON.parse(JSON.stringify(GRAPH));
    next.phase = 'review';
    next.nodes.mintToken.status = 'existing';
    next.nodes.mintToken.line = 91;
    await seedGraph(request, next);

    await loadPage(page);
    await page.locator('#graphViewToggle .toggle-btn[data-graph-view="graph"]').click();
    const drift = page.locator('.crit-graph-drift');
    await expect(drift).toBeVisible();
    await expect(drift).toContainText('mintToken: new -> existing');
  });

  // A wide hierarchy must render at natural size and scroll. Letting Mermaid
  // scale it to the container turned an 18-node graph into unreadable slivers
  // with no visible labels.
  test('a wide graph renders full size and scrolls instead of shrinking', async ({ request, page }) => {
    const ids = Array.from({ length: 18 }, (_, i) => `aVeryLongFunctionName${i}`);
    const nodes: Record<string, unknown> = {};
    ids.forEach((id, i) => {
      nodes[id] = { signature: `${id}()`, file: 'src/a.ts', line: i + 1, status: 'new' };
    });
    await seedGraph(request, {
      title: 'wide',
      diagram: 'flowchart LR\n  ' + ids.slice(0, -1).map((id, i) => `${id} --> ${ids[i + 1]}`).join('\n  '),
      nodes,
    });
    await loadPage(page);
    await page.locator('#graphViewToggle .toggle-btn[data-graph-view="graph"]').click();
    await expect(page.locator('.crit-graph-diagram svg')).toBeVisible();

    const m = await page.evaluate(() => {
      const wrap = document.querySelector('.crit-graph-diagram-wrap') as HTMLElement;
      const rect = document.querySelector('.crit-graph-diagram g.node rect') as SVGGraphicsElement;
      return { scrollW: wrap.scrollWidth, clientW: wrap.clientWidth, nodeW: rect.getBoundingClientRect().width };
    });
    expect(m.scrollW).toBeGreaterThan(m.clientW + 2);
    // A squeezed graph rendered these ~39px wide with 16px text inside.
    expect(m.nodeW).toBeGreaterThan(120);
  });

  // A call hierarchy is the diagram most likely to outgrow its container, so
  // it reuses the review page's existing pan/zoom overlay.
  test('the diagram opens fullscreen with pan and zoom', async ({ page }) => {
    const expand = page.locator('.crit-graph-expand');
    await expect(expand).toBeVisible();
    await expand.click();
    await expect(page.locator('#mermaidOverlay')).toBeVisible();
    await expect(page.locator('#mermaidOverlayCanvas svg')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.locator('#mermaidOverlay')).toBeHidden();
  });

  // Mermaid bakes its theme into the emitted SVG and skips nodes it has
  // already processed, so a theme switch left the graph's edges, clusters and
  // arrowheads in the old theme while our own node fills followed along —
  // which is exactly what made it easy to miss.
  test('switching theme restyles the diagram and keeps the open node', async ({ page }) => {
    await page.locator('.crit-graph-diagram g.node', { hasText: 'mintToken' }).first().click();
    await expect(page.locator('#graphNodePanel')).toHaveClass(/open/);

    const before = await page.evaluate(
      () => document.querySelector('.crit-graph-diagram svg style')!.textContent
    );
    await page.evaluate(() => (window as any).applyTheme('dark'));
    await expect(page.locator('.crit-graph-diagram svg')).toBeVisible();
    await expect
      .poll(async () =>
        page.evaluate(() => document.querySelector('.crit-graph-diagram svg style')!.textContent)
      )
      .not.toBe(before);

    // Decoration and the open panel must survive the re-render.
    await expect(page.locator('.crit-graph-diagram g.node.crit-graph-node')).toHaveCount(4);
    await expect(page.locator('#graphNodePanel')).toHaveClass(/open/);
    await expect(page.locator('#graphNodePanel .crit-graph-card-name')).toHaveText('mintToken');
    await page.evaluate(() => (window as any).applyTheme('system'));
  });

  test('the toggle switches back to the file list', async ({ page }) => {
    await page.locator('#graphViewToggle .toggle-btn[data-graph-view="files"]').click();
    await expect(page.locator('#graphRoot')).toBeHidden();
    await expect(page.locator('#filesContainer')).toBeVisible();
  });
});

// The scanner's governing property is that nothing it accepts may fail to
// render in Mermaid — a graph that validates and then shows an error is
// indistinguishable from a crit bug. These are the constructs that actually
// bit: each one either broke the line-based scanner or was rejected outright
// while Mermaid accepted it. Rendering them end to end is the guard.
test.describe('Diagram syntax crit accepts must render', () => {
  const CASES: Record<string, string> = {
    'percent signs in a label': 'flowchart LR\n  handleReq["50%% done"] --> mintToken',
    'label spanning lines': 'flowchart LR\n  handleReq["two\nlines"] --> mintToken',
    'own-line comment': 'flowchart LR\n  %% a note\n  handleReq --> mintToken',
    'edge id': 'flowchart LR\n  handleReq e1@--> mintToken',
    'node metadata block': 'flowchart LR\n  handleReq@{ shape: rect } --> mintToken',
    'metadata block over lines': 'flowchart LR\n  handleReq@{\n    shape: rect\n  } --> mintToken',
    'quoted brackets in a label': 'flowchart LR\n  handleReq["fn(x[0])"] --> mintToken',
    'arrow inside a label': 'flowchart LR\n  handleReq["a --> b"] --> mintToken',
    'mixed shapes': 'flowchart LR\n  handleReq(("circle")) --> mintToken{{"hex"}}',
    'ampersand fan-out': 'flowchart LR\n  handleReq --> mintToken & resolveOrg',
  };

  for (const [name, diagram] of Object.entries(CASES)) {
    test(name, async ({ request, page }) => {
      // Only the IDs this diagram actually declares: the join check rejects
      // metadata for a node the diagram never mentions, and rightly so.
      const nodes: Record<string, unknown> = {};
      for (const id of ['handleReq', 'mintToken', 'resolveOrg']) {
        if (!diagram.includes(id)) continue;
        nodes[id] = { signature: `${id}()`, file: 'src/a.ts', line: 1, status: 'new' };
      }
      // crit accepted it (200), so Mermaid must render it.
      const res = await request.post('/api/graph', { data: { graph: { title: name, diagram, nodes } } });
      expect(res.status(), await res.text()).toBe(200);

      await loadPage(page);
      await page.locator('#graphViewToggle .toggle-btn[data-graph-view="graph"]').click();
      await expect(page.locator('.crit-graph-diagram svg')).toBeVisible();
      await expect(page.locator('.crit-graph-error')).toHaveCount(0);
      // Decoration proves the join survived: node IDs were recovered from the
      // SVG and matched back to the metadata map.
      await expect(page.locator('.crit-graph-diagram g.node.crit-graph-node').first()).toBeVisible();
      await request.delete('/api/graph');
    });
  }
});

test.describe('Graph ingest rejection', () => {
  // The API must refuse an invalid graph with the same actionable report the
  // CLI prints, since an agent may be posting straight to a running daemon.
  // Mermaid rejects a `%%` comment that follows content, so crit must reject
  // it too — stripping it let the graph validate and then fail to render.
  test('a trailing comment is refused rather than stripped', async ({ request }) => {
    const res = await request.post('/api/graph', {
      data: {
        graph: {
          title: 'trailing',
          diagram: 'flowchart LR\n  a --> b %% note',
          nodes: {
            a: { signature: 'a()', file: 'a.ts', line: 1, status: 'new' },
            b: { signature: 'b()', file: 'b.ts', line: 1, status: 'new' },
          },
        },
      },
    });
    expect(res.status()).toBe(422);
    const body = await res.json();
    expect(body.items.some((i: { code: string }) => i.code === 'diagram_trailing_comment')).toBeTruthy();
  });

  test('an invalid graph is rejected with per-finding fixes', async ({ request }) => {
    const res = await request.post('/api/graph', {
      data: {
        graph: {
          title: 'broken',
          diagram: 'flowchart LR\n  a --> b',
          nodes: { a: { signature: 'a()', file: 'a.ts', line: 1, status: 'new' } },
        },
      },
    });
    expect(res.status()).toBe(422);
    const body = await res.json();
    expect(body.ok).toBe(false);
    expect(body.errors).toBeGreaterThan(0);
    const missing = body.items.find((i: { code: string }) => i.code === 'node_missing_metadata');
    expect(missing).toBeTruthy();
    expect(missing.node).toBe('b');
    expect(missing.fix).toContain('b');
  });
});
