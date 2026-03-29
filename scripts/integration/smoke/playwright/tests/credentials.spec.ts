import { test, expect } from '@playwright/test';

test.describe('Credentials management', () => {

  test('create and delete a credential', async ({ page }) => {
    await page.goto('/settings');
    await expect(page.getByText(/Credentials/i)).toBeVisible();

    const addButton = page.getByRole('button', { name: /Add|Create|New/i });
    await expect(addButton).toBeVisible();
    await addButton.click();

    const nameInput = page.locator('input[name="name"], input[placeholder*="name" i]');
    await nameInput.fill('PLAYWRIGHT_SMOKE_CRED');

    const valueInput = page.locator('input[name="value"], input[placeholder*="value" i], input[type="password"]');
    await valueInput.fill('playwright-test-value');

    const saveButton = page.getByRole('button', { name: /Save|Create|Add/i }).last();
    await saveButton.click();

    await expect(page.getByText('PLAYWRIGHT_SMOKE_CRED')).toBeVisible({ timeout: 5000 });

    const row = page.locator('tr, [data-credential]').filter({ hasText: 'PLAYWRIGHT_SMOKE_CRED' });
    const deleteButton = row.getByRole('button', { name: /Delete|Remove/i });
    await deleteButton.click();

    const confirmButton = page.getByRole('button', { name: /Confirm|Yes|Delete/i });
    if (await confirmButton.isVisible({ timeout: 2000 }).catch(() => false)) {
      await confirmButton.click();
    }

    await expect(page.getByText('PLAYWRIGHT_SMOKE_CRED')).not.toBeVisible({ timeout: 5000 });
  });

});
