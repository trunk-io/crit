import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// ============================================================
// Review Toolbar — Comment Cleanup dialog (git mode)
// ============================================================
test.describe('Comment Cleanup', () => {
  const overlay = '#cleanupOverlay';

  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('toolbar button opens the dialog and focuses the slider', async ({ page }) => {
    await expect(page.locator('#reviewToolbar')).toBeVisible();
    await page.click('#cleanupCommentsBtn');

    await expect(page.locator(overlay)).toHaveClass(/active/);
    await expect(page.locator('#cleanupLevel')).toBeFocused();
    await expect(page.locator('#cleanupLevelLabel')).toHaveText('Balanced');
  });

  test('Escape closes the dialog and returns focus to the toolbar button', async ({ page }) => {
    await page.click('#cleanupCommentsBtn');
    await expect(page.locator(overlay)).toHaveClass(/active/);

    await page.keyboard.press('Escape');
    await expect(page.locator(overlay)).not.toHaveClass(/active/);
    await expect(page.locator('#cleanupCommentsBtn')).toBeFocused();
  });

  test('backdrop click closes the dialog', async ({ page }) => {
    await page.click('#cleanupCommentsBtn');
    await expect(page.locator(overlay)).toHaveClass(/active/);

    await page.locator(overlay).click({ position: { x: 8, y: 8 } });
    await expect(page.locator(overlay)).not.toHaveClass(/active/);
  });

  test('slider updates the level name and the prompt preview', async ({ page }) => {
    await page.click('#cleanupCommentsBtn');
    const preview = page.locator('#cleanupPromptPreview');
    await expect(preview).toContainText('Level 3/5 — Balanced');

    await page.locator('#cleanupLevel').fill('5');
    await expect(page.locator('#cleanupLevelLabel')).toHaveText('Maximum');
    await expect(preview).toContainText('Level 5/5 — Maximum');

    await page.locator('#cleanupLevel').fill('1');
    await expect(page.locator('#cleanupLevelLabel')).toHaveText('Light');
    await expect(preview).toContainText('Level 1/5 — Light');
  });

  test('every level keeps the directive exemption in the prompt', async ({ page }) => {
    await page.click('#cleanupCommentsBtn');
    const preview = page.locator('#cleanupPromptPreview');
    for (const level of ['1', '2', '3', '4', '5']) {
      await page.locator('#cleanupLevel').fill(level);
      await expect(preview).toContainText('eslint-disable');
      await expect(preview).toContainText('SPDX');
    }
  });

  test('Add comment posts the previewed prompt as a review-level comment', async ({ page, request }) => {
    await page.click('#cleanupCommentsBtn');
    await page.locator('#cleanupLevel').fill('2');
    const preview = await page.locator('#cleanupPromptPreview').textContent();

    await page.click('#cleanupAddBtn');
    await expect(page.locator(overlay)).not.toHaveClass(/active/);

    await expect(async () => {
      const comments = await request.get('/api/comments').then(r => r.json());
      expect(comments).toHaveLength(1);
      // The dialog is a "what you see is what is sent" surface — the posted
      // body must match the preview byte for byte, not just contain it.
      expect(comments[0].body).toBe(preview);
      expect(comments[0].body).toContain('Level 2/5 — Moderate');
    }).toPass();

    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
  });

  test('the level choice survives a reload', async ({ page }) => {
    await page.click('#cleanupCommentsBtn');
    await page.locator('#cleanupLevel').fill('4');
    await page.locator('#cleanupLevel').dispatchEvent('change');
    await page.click('#cleanupCancelBtn');

    await loadPage(page);
    await page.click('#cleanupCommentsBtn');
    await expect(page.locator('#cleanupLevel')).toHaveValue('4');
    await expect(page.locator('#cleanupLevelLabel')).toHaveText('Aggressive');
  });

  test('Add & submit review posts the comment and finishes the round', async ({ page, request }) => {
    await page.click('#cleanupCommentsBtn');
    await page.click('#cleanupAddSubmitBtn');

    await expect(page.locator(overlay)).not.toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

    await expect(async () => {
      const comments = await request.get('/api/comments').then(r => r.json());
      expect(comments).toHaveLength(1);
      expect(comments[0].body).toContain('Clean up the code comments');
    }).toPass();

    // Hand the shared git-mode server back in the reviewing state — specs in
    // this project run sequentially against it.
    await page.click('#backToEditing');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);
  });

  test('the toolbar sits inside the header so --header-height covers it', async ({ page }) => {
    await expect(async () => {
      const fits = await page.evaluate(() => {
        const header = document.querySelector('.header')!.getBoundingClientRect();
        const toolbar = document.getElementById('reviewToolbar')!.getBoundingClientRect();
        const cssVar = getComputedStyle(document.documentElement)
          .getPropertyValue('--header-height')
          .trim();
        return {
          inside: toolbar.top >= header.top && toolbar.bottom <= header.bottom + 1,
          varMatchesHeader: Math.abs(parseFloat(cssVar) - header.height) < 1.5,
        };
      });
      expect(fits.inside).toBe(true);
      expect(fits.varMatchesHeader).toBe(true);
    }).toPass();
  });
});
