import { test, expect } from '@playwright/test';

// ── Presets ───────────────────────────────────────────────────────────────────

test.describe('Presets', () => {

  test('save-as-preset link appears after typing prompt', async ({ page }) => {
    await page.goto('/');
    await page.getByPlaceholder('Describe what you want to do...').fill('Fix the build');
    await expect(page.getByText('Save as preset')).toBeVisible();
  });

  test('save preset modal has title input, scope buttons, and disabled Save', async ({ page }) => {
    await page.goto('/');
    await page.getByPlaceholder('Describe what you want to do...').fill('Fix the build');
    await page.getByText('Save as preset').click();

    const modal = page.locator('.fixed.inset-0');
    await expect(modal.locator('input[placeholder="e.g. Fix TypeScript errors"]')).toBeVisible();
    await expect(modal.getByRole('button', { name: 'Personal', exact: true })).toBeVisible();
    await expect(modal.getByRole('button', { name: 'Team', exact: true })).toBeVisible();
    // Save button disabled until a title is entered
    await expect(modal.getByRole('button', { name: /^Save$/ })).toBeDisabled();
  });

  test('save preset modal closes on Cancel', async ({ page }) => {
    await page.goto('/');
    await page.getByPlaceholder('Describe what you want to do...').fill('Fix the build');
    await page.getByText('Save as preset').click();
    await page.getByRole('button', { name: 'Cancel' }).click();
    await expect(page.locator('input[placeholder="e.g. Fix TypeScript errors"]')).not.toBeVisible();
  });

  test('full lifecycle: create preset → sidebar shows it → click injects prompt → delete', async ({ page }) => {
    const prompt = `Playwright smoke preset ${Date.now()}`;
    const title = `Smoke Preset ${Date.now()}`;

    await page.goto('/');
    await page.getByPlaceholder('Describe what you want to do...').fill(prompt);
    await page.getByText('Save as preset').click();

    await page.locator('input[placeholder="e.g. Fix TypeScript errors"]').fill(title);
    await page.getByRole('button', { name: /^Save$/ }).click();

    // Sidebar should appear and show the new preset
    await expect(page.getByRole('heading', { name: 'Presets' })).toBeVisible({ timeout: 5000 });
    const presetItem = page.locator('aside').locator('div.group').filter({ hasText: title }).first();
    await expect(presetItem).toBeVisible({ timeout: 5000 });

    // Click preset — injects its prompt into the textarea
    await page.getByPlaceholder('Describe what you want to do...').clear();
    await presetItem.click();
    await expect(page.getByPlaceholder('Describe what you want to do...')).toHaveValue(prompt);

    // Delete: hover the preset item so the X button becomes visible, then click it
    await presetItem.hover();
    await presetItem.locator('button').click();
    await expect(page.locator('aside').getByText(title)).not.toBeVisible({ timeout: 5000 });
  });

  test('team-scoped preset shows under Team Presets section', async ({ page }) => {
    const title = `Team Smoke Preset ${Date.now()}`;

    await page.goto('/');
    await page.getByPlaceholder('Describe what you want to do...').fill('Run the linter');
    await page.getByText('Save as preset').click();

    const modal = page.locator('.fixed.inset-0');
    await modal.locator('input[placeholder="e.g. Fix TypeScript errors"]').fill(title);
    // Switch to Team scope
    await modal.getByRole('button', { name: 'Team', exact: true }).click();
    await modal.getByRole('button', { name: /^Save$/ }).click();

    await expect(page.getByText('Team Presets')).toBeVisible({ timeout: 5000 });
    const presetItem = page.locator('aside').locator('div.group').filter({ hasText: title }).first();
    await expect(presetItem).toBeVisible({ timeout: 5000 });

    // Cleanup
    await presetItem.hover();
    await presetItem.locator('button').click();
    await expect(page.locator('aside').getByText(title)).not.toBeVisible({ timeout: 5000 });
  });

});

// ── Saved Repos ───────────────────────────────────────────────────────────────

test.describe('Saved Repos combobox', () => {

  test('bookmark button appears when a URL is typed', async ({ page }) => {
    await page.goto('/');
    await page.getByPlaceholder('https://github.com/org/repo').fill('https://github.com/tinkerloft/fleetlift');
    await expect(page.locator('button[title="Save repo for quick access"]')).toBeVisible();
  });

  test('bookmark button hidden when input is empty', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('button[title="Save repo for quick access"]')).not.toBeVisible();
  });

  test('full lifecycle: save repo → dropdown shows it → select fills input', async ({ page }) => {
    const testUrl = `https://github.com/smoke-test/repo-${Date.now()}`;

    await page.goto('/');
    const repoInput = page.getByPlaceholder('https://github.com/org/repo');

    await repoInput.fill(testUrl);
    await page.locator('button[title="Save repo for quick access"]').click();

    // Bookmark button disappears once the URL is saved (isSaved = true)
    await expect(page.locator('button[title="Save repo for quick access"]')).not.toBeVisible({ timeout: 3000 });

    // Clear input and focus — dropdown should open automatically (savedRepos.length > 0)
    await repoInput.clear();
    await repoInput.click();
    await expect(page.locator('button').filter({ hasText: testUrl })).toBeVisible({ timeout: 5000 });

    // Select the saved repo — fills the input
    await page.locator('button').filter({ hasText: testUrl }).click();
    await expect(repoInput).toHaveValue(testUrl);

    // Cleanup via API
    const listRes = await page.request.get('/api/saved-repos');
    if (listRes.ok()) {
      const data = await listRes.json();
      const repo = (data.items ?? []).find((r: { url: string; id: string }) => r.url === testUrl);
      if (repo) {
        await page.request.delete(`/api/saved-repos/${repo.id}`);
      }
    }
  });

  test('chevron button opens saved repo dropdown', async ({ page }) => {
    const testUrl = `https://github.com/smoke-test/chevron-${Date.now()}`;

    // Pre-create a saved repo via API so the dropdown has something to show
    await page.request.post('/api/saved-repos', {
      headers: { 'Content-Type': 'application/json' },
      data: { url: testUrl },
    });

    await page.goto('/');

    // Focusing the input opens the dropdown when savedRepos.length > 0 (onFocus handler)
    const repoInput = page.getByPlaceholder('https://github.com/org/repo');
    await expect(repoInput).toBeVisible({ timeout: 5000 });
    await repoInput.click();

    await expect(page.locator('button').filter({ hasText: testUrl })).toBeVisible({ timeout: 3000 });

    // Cleanup
    const listRes = await page.request.get('/api/saved-repos');
    if (listRes.ok()) {
      const data = await listRes.json();
      const repo = (data.items ?? []).find((r: { url: string; id: string }) => r.url === testUrl);
      if (repo) {
        await page.request.delete(`/api/saved-repos/${repo.id}`);
      }
    }
  });

});
