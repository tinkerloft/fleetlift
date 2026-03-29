import { test, expect } from '@playwright/test';

test.describe('Credentials management', () => {

  test('create and delete a credential', async ({ page }) => {
    await page.goto('/settings');
    await expect(page.getByRole('heading', { name: /Team Credentials/i })).toBeVisible();

    // Click "+ Add" button
    await page.getByText('+ Add').click();

    // Fill in the name field (placeholder: CREDENTIAL_NAME)
    const nameInput = page.locator('input[placeholder="CREDENTIAL_NAME"]');
    await nameInput.fill('PLAYWRIGHT_SMOKE_CRED');

    // Fill in the value field (type=password, placeholder: Value)
    const valueInput = page.locator('input[placeholder="Value"]');
    await valueInput.fill('playwright-test-value');

    // Click Save
    await page.getByText('Save').click();

    // Verify it appears in the list
    await expect(page.getByText('PLAYWRIGHT_SMOKE_CRED')).toBeVisible({ timeout: 5000 });

    // Click Delete — the credential row is a flex div containing both the name and button
    const credRow = page.locator('div.flex.items-center.justify-between').filter({ hasText: 'PLAYWRIGHT_SMOKE_CRED' });
    await credRow.getByText('Delete').click();

    // Confirm the inline deletion
    await page.getByText('Confirm').click();

    // Verify it's gone
    await expect(page.getByText('PLAYWRIGHT_SMOKE_CRED')).not.toBeVisible({ timeout: 5000 });
  });

});
