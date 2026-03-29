import { test, expect } from '@playwright/test';

async function submitSandboxTest(baseURL: string): Promise<{ runId: string }> {
  const loginRes = await fetch(`${baseURL}/api/auth/dev-login`);
  const { token } = await loginRes.json() as { token: string };

  const res = await fetch(`${baseURL}/api/runs`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    },
    body: JSON.stringify({
      workflow_id: 'sandbox-test',
      parameters: { duration: 20, command2: 'echo sse-test-done' },
    }),
  });
  const body = await res.json() as { id: string };
  return { runId: body.id };
}

test.describe('SSE log streaming', () => {

  test('logs stream in real-time while step is running', async ({ page, baseURL }) => {
    test.setTimeout(90_000);

    const { runId } = await submitSandboxTest(baseURL!);

    await page.goto(`/runs/${runId}`);
    await expect(page.getByText('Sandbox Test')).toBeVisible({ timeout: 10_000 });

    const runCommandStep = page.locator('text=Run shell command, text=run_command').first();
    await expect(runCommandStep).toBeVisible({ timeout: 30_000 });
    await runCommandStep.click();

    const cookies = await page.context().cookies();
    const flTokenCookie = cookies.find(c => c.name === 'fl_token');
    const localStorageToken = await page.evaluate(() => localStorage.getItem('token'));

    console.log('--- SSE Diagnostic ---');
    console.log(`fl_token cookie: ${flTokenCookie ? 'present' : 'MISSING'}`);
    console.log(`localStorage.token: ${localStorageToken ? 'present' : 'MISSING'}`);

    const sseRequests: { url: string; status: number; headers: Record<string, string> }[] = [];
    page.on('response', response => {
      if (response.url().includes('/api/runs/steps/') && response.url().includes('/logs')) {
        sseRequests.push({
          url: response.url(),
          status: response.status(),
          headers: response.headers(),
        });
        console.log(`SSE response: ${response.status()} ${response.url()}`);
      }
    });

    const consoleErrors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') {
        consoleErrors.push(msg.text());
      }
    });

    await page.waitForTimeout(2_000);
    await page.screenshot({ path: 'test-results/sse-01-initial.png', fullPage: true });

    const logContainer = page.locator('.font-mono.text-green-400');
    await expect(logContainer).toBeVisible({ timeout: 5_000 });

    let sawTick = false;
    let initialCount = 0;
    let finalCount = 0;

    for (let i = 0; i < 15; i++) {
      const text = await logContainer.textContent() ?? '';
      const tickMatches = text.match(/\[tick \d+/g);
      if (tickMatches && tickMatches.length > 0) {
        sawTick = true;
        initialCount = tickMatches.length;
        console.log(`Found ${initialCount} tick lines after ${i + 1}s`);
        break;
      }
      await page.waitForTimeout(1_000);
    }

    await page.screenshot({ path: 'test-results/sse-02-after-wait.png', fullPage: true });

    if (sawTick) {
      await page.waitForTimeout(5_000);
      const text = await logContainer.textContent() ?? '';
      const tickMatches = text.match(/\[tick \d+/g);
      finalCount = tickMatches?.length ?? 0;
      console.log(`After 5s more: ${finalCount} tick lines (was ${initialCount})`);
    }

    await page.screenshot({ path: 'test-results/sse-03-final.png', fullPage: true });

    console.log('--- SSE Network ---');
    for (const req of sseRequests) {
      console.log(`  ${req.status} ${req.url}`);
      console.log(`  content-type: ${req.headers['content-type'] ?? 'none'}`);
    }
    if (sseRequests.length === 0) {
      console.log('  NO SSE requests captured — EventSource may not have connected');
    }

    console.log('--- Console Errors ---');
    for (const err of consoleErrors) {
      console.log(`  ${err}`);
    }

    expect(sawTick, 'Expected to see at least one [tick N/20] log line while step is running').toBe(true);
    expect(finalCount, 'Expected log count to increase over 5 seconds (real-time streaming)').toBeGreaterThan(initialCount);
  });

});
