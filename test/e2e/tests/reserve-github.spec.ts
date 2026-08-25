import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection } from './helpers';

// ============================================================
// Reserve for GitHub (git mode)
// ============================================================
test.describe('Reserve for GitHub', () => {
  const chip = '#reservedCount';

  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  // Code files render as diffs in git mode: hover an addition line, click its +.
  async function openInlineForm(page: import('@playwright/test').Page) {
    const section = goSection(page);
    await expect(section).toBeVisible();
    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.scrollIntoViewIfNeeded();
    await additionSide.hover();
    await additionSide.locator('.diff-comment-btn').click();
    await expect(page.locator('.comment-form textarea').first()).toBeVisible();
  }

  test('the reserved chip is hidden until something is reserved', async ({ page }) => {
    await expect(page.locator('#reviewToolbar')).toBeVisible();
    await expect(page.locator(chip)).toBeHidden();
  });

  test('reserving an inline comment stores the flag and shows the chip', async ({ page, request }) => {
    await openInlineForm(page);
    await page.locator('.comment-form textarea').first().fill('This should go on the PR, not to the agent.');
    await page.locator('.comment-form .btn-reserve').first().click();

    await expect(page.locator(chip)).toBeVisible();
    await expect(page.locator('#reservedCountNum')).toHaveText('1');

    await expect(async () => {
      const comments = await request
        .get('/api/file/comments?path=' + encodeURIComponent('server.go'))
        .then(r => r.json());
      const reserved = comments.filter((c: { reserved_for_github?: boolean }) => c.reserved_for_github);
      expect(reserved).toHaveLength(1);
    }).toPass();

    await expect(page.locator('.reserved-badge').first()).toBeVisible();
  });

  // The point of reserving: the reviewer can still approve while holding PR feedback.
  test('a reserved comment alone leaves the button on Approve', async ({ page }) => {
    await openInlineForm(page);
    await page.locator('.comment-form textarea').first().fill('PR-only note.');
    await page.locator('.comment-form .btn-reserve').first().click();

    await expect(page.locator(chip)).toBeVisible();
    await expect(page.locator('#finishBtn')).toHaveText('Approve');
  });

  test('an ordinary comment still flips the button to Finish Review', async ({ page }) => {
    await openInlineForm(page);
    await page.locator('.comment-form textarea').first().fill('Agent, please fix this.');
    await page.locator('.comment-form .btn-primary').first().click();

    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
    await expect(page.locator(chip)).toBeHidden();
  });

  test('reserving does not send the comment to the agent listing', async ({ page, request }) => {
    await openInlineForm(page);
    await page.locator('.comment-form textarea').first().fill('PR-only note.');
    await page.locator('.comment-form .btn-reserve').first().click();
    await expect(page.locator(chip)).toBeVisible();

    // Driven through the API, not the UI: this asserts the server's own gate,
    // which is what actually decides whether the agent gets work.
    const resp = await request.post('/api/finish');
    expect(resp.ok()).toBeTruthy();
    const body = await resp.json();
    expect(body.approved).toBe(true);
  });

  test('the chip counts inline and review-level reservations together', async ({ page, request }) => {
    await request.post('/api/file/comments?path=' + encodeURIComponent('server.go'), {
      data: { start_line: 1, end_line: 1, body: 'inline pr note', reserved_for_github: true },
    });
    await request.post('/api/comments', {
      data: { body: 'review-level pr note', reserved_for_github: true },
    });
    await loadPage(page);

    await expect(page.locator(chip)).toBeVisible();
    await expect(page.locator('#reservedCountNum')).toHaveText('2');
    await expect(page.locator('#finishBtn')).toHaveText('Approve');
  });

  test('the reserve button is absent while editing an existing comment', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'an existing note' } });
    await loadPage(page);

    await page.locator('#treeConversationSection .tree-conversation-row').first().click();
    const card = page.locator('#reviewConversation .comment-card').first();
    await expect(card).toBeVisible();
    await card.locator('.comment-actions button[title*="Edit" i], .comment-actions button[aria-label*="Edit" i]').first().click();

    const editForm = page.locator('#reviewConversation .comment-form').first();
    await expect(editForm.locator('textarea')).toBeVisible();
    await expect(editForm.locator('.btn-reserve')).toHaveCount(0);
  });
});

