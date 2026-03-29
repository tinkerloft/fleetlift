import { test, expect } from '@playwright/test';

test.describe('Home page', () => {

  test('home page loads with prompt zone', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: /Fleetlift/i })).toBeVisible();
    await expect(page.getByPlaceholder('Describe what you want to do...')).toBeVisible();
    await expect(page.getByPlaceholder('https://github.com/org/repo')).toBeVisible();
    await expect(page.getByRole('button', { name: /Run/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /Improve/i })).toBeVisible();
  });

  test('template grid shows workflows', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: /Workflows/i })).toBeVisible();
    // At least one workflow card should be visible
    await expect(page.getByText('Sandbox Test')).toBeVisible({ timeout: 5000 });
  });

  test('recent tasks section visible', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: /Recent Tasks/i })).toBeVisible();
  });

  test('model select dropdown is present', async ({ page }) => {
    await page.goto('/');
    // ModelSelect renders a native <select> element
    const modelSelect = page.locator('select').first();
    await expect(modelSelect).toBeVisible({ timeout: 3000 });
  });

  test('improve button disabled when prompt empty', async ({ page }) => {
    await page.goto('/');
    const improveBtn = page.getByRole('button', { name: /Improve/i });
    await expect(improveBtn).toBeDisabled();
  });

  test('improve button enabled when prompt has text', async ({ page }) => {
    await page.goto('/');
    const textarea = page.getByPlaceholder('Describe what you want to do...');
    await textarea.fill('Fix the authentication bug');
    const improveBtn = page.getByRole('button', { name: /Improve/i });
    await expect(improveBtn).toBeEnabled();
  });

  test('improve button opens modal', async ({ page }) => {
    await page.goto('/');
    const textarea = page.getByPlaceholder('Describe what you want to do...');
    await textarea.fill('Fix the authentication bug');

    await page.getByRole('button', { name: /Improve/i }).click();

    // Modal should appear — the fixed overlay container
    const modal = page.locator('.fixed.inset-0');
    await expect(modal).toBeVisible({ timeout: 3000 });

    // Should show "Improve Prompt" header or loading state
    const header = page.getByText('Improve Prompt');
    const loading = page.getByText('Analyzing and improving your prompt...');
    await expect(header.or(loading)).toBeVisible({ timeout: 3000 });
  });

  test('improve modal closes on escape', async ({ page }) => {
    await page.goto('/');
    const textarea = page.getByPlaceholder('Describe what you want to do...');
    await textarea.fill('Fix the bug');

    await page.getByRole('button', { name: /Improve/i }).click();
    const modal = page.locator('.fixed.inset-0');
    await expect(modal).toBeVisible({ timeout: 3000 });

    await page.keyboard.press('Escape');
    await expect(modal).not.toBeVisible({ timeout: 3000 });
  });

  test('run button disabled without repo URL', async ({ page }) => {
    await page.goto('/');
    const textarea = page.getByPlaceholder('Describe what you want to do...');
    await textarea.fill('Fix the bug');
    // Run button should still be disabled (no repo URL)
    const runBtn = page.getByRole('button', { name: /^Run$/i });
    await expect(runBtn).toBeDisabled();
  });

  test('view all link navigates to workflows', async ({ page }) => {
    await page.goto('/');
    await page.getByText('View all').click();
    await expect(page).toHaveURL(/\/workflows/);
  });

});
