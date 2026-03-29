import { test, expect, type Page } from '@playwright/test';

// Authenticate via the browser so the fl_token cookie lands in the browser's
// cookie jar (EventSource needs it). Then submit the run from browser context.
async function submitSandboxTest(page: Page, baseURL: string): Promise<{ runId: string }> {
  const { token, runId } = await page.evaluate(async (base) => {
    const loginRes = await fetch(`${base}/api/auth/dev-login`, { credentials: 'include' });
    if (!loginRes.ok) {
      throw new Error(`dev-login failed: ${loginRes.status} ${loginRes.statusText}`);
    }
    const { token: t } = await loginRes.json() as { token: string };

    const res = await fetch(`${base}/api/runs`, {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${t}`,
      },
      body: JSON.stringify({
        workflow_id: 'sandbox-test',
        parameters: { duration: 20, command2: 'echo sse-test-done' },
      }),
    });
    if (!res.ok) {
      throw new Error(`create run failed: ${res.status} ${res.statusText}`);
    }
    const body = await res.json() as { id: string };
    return { token: t, runId: body.id };
  }, baseURL);

  // Also store token in localStorage so the app's auth state works
  await page.evaluate((t) => localStorage.setItem('token', t), token);
  return { runId };
}

test.describe('SSE log streaming', () => {

  test('logs stream in real-time while step is running', async ({ page, baseURL }) => {
    test.setTimeout(90_000);

    // Navigate to app first so page.evaluate runs in the app's origin
    await page.goto('/');
    const { runId } = await submitSandboxTest(page, baseURL!);

    await page.goto(`/runs/${runId}`);
    await expect(page.getByText('Sandbox Test')).toBeVisible({ timeout: 10_000 });

    // Wait for the step to appear in the timeline, then click it to select it
    const runCommandStep = page.getByText('Run shell command').or(page.getByText('run_command')).first();
    await expect(runCommandStep).toBeVisible({ timeout: 30_000 });
    await runCommandStep.click();

    // Wait for the LogStream component to render (uses data-testid)
    const logContainer = page.getByTestId('log-stream');
    await expect(logContainer).toBeVisible({ timeout: 10_000 });

    // Poll for tick lines to appear (sandbox provisioning + command execution)
    let initialCount = 0;
    for (let i = 0; i < 15; i++) {
      const text = await logContainer.textContent() ?? '';
      const tickMatches = text.match(/\[tick \d+/g);
      if (tickMatches && tickMatches.length > 0) {
        initialCount = tickMatches.length;
        break;
      }
      await page.waitForTimeout(1_000);
    }
    expect(initialCount, 'Expected to see at least one [tick N/20] log line while step is running').toBeGreaterThan(0);

    // Wait and verify count increases (real-time streaming)
    await page.waitForTimeout(5_000);
    const text = await logContainer.textContent() ?? '';
    const finalCount = text.match(/\[tick \d+/g)?.length ?? 0;
    expect(finalCount, 'Expected log count to increase over 5 seconds (real-time streaming)').toBeGreaterThan(initialCount);
  });

});
