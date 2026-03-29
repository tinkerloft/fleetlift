import { test, expect } from '@playwright/test';

test.describe('Workflow execution from UI', () => {

  test('launch sandbox-test from workflow detail page', async ({ page }) => {
    await page.goto('/workflows/sandbox-test');
    await expect(page.getByText('Sandbox Test')).toBeVisible();

    const durationInput = page.locator('input[name="duration"], [data-param="duration"] input');
    if (await durationInput.isVisible({ timeout: 3000 }).catch(() => false)) {
      await durationInput.fill('3');
    }

    const runButton = page.getByRole('button', { name: /Run|Start|Execute/i });
    await expect(runButton).toBeVisible();
    await runButton.click();

    await expect(page).toHaveURL(/\/runs\/[0-9a-f-]+/, { timeout: 10000 });
    await expect(page.getByText('Sandbox Test')).toBeVisible({ timeout: 5000 });
  });

  test('runs list shows recent runs', async ({ page }) => {
    await page.goto('/runs');
    await expect(page.getByRole('heading', { name: /Runs/i })).toBeVisible();
    await page.waitForTimeout(2000);
    const rows = page.locator('table tbody tr, [data-run-id]');
    await expect(rows.first()).toBeVisible({ timeout: 10000 });
  });

});
