import { test, expect } from '@playwright/test';

test.describe('Page navigation', () => {

  test('workflow library loads', async ({ page }) => {
    await page.goto('/workflows');
    await expect(page.getByRole('heading', { name: /Workflow Library/i })).toBeVisible();
    await expect(page.getByText('Sandbox Test')).toBeVisible();
    await expect(page.getByText('PR Review')).toBeVisible();
    await expect(page.getByText('Bug Fix')).toBeVisible();
  });

  test('runs page loads', async ({ page }) => {
    await page.goto('/runs');
    await expect(page.getByRole('heading', { name: /Runs/i })).toBeVisible();
  });

  test('inbox page loads', async ({ page }) => {
    await page.goto('/inbox');
    await expect(page.getByRole('heading', { name: /Inbox/i })).toBeVisible();
  });

  test('reports page loads', async ({ page }) => {
    await page.goto('/reports');
    await expect(page.getByRole('heading', { name: /Reports/i })).toBeVisible();
  });

  test('knowledge page loads', async ({ page }) => {
    await page.goto('/knowledge');
    await expect(page.getByRole('heading', { name: /Knowledge/i })).toBeVisible();
  });

  test('settings page loads', async ({ page }) => {
    await page.goto('/settings');
    await expect(page.getByText(/Credentials/i)).toBeVisible();
  });

  test('system health page loads', async ({ page }) => {
    await page.goto('/system');
    await expect(page.getByText(/System/i)).toBeVisible();
  });

});
