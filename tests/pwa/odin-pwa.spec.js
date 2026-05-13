const { test, expect } = require('@playwright/test');

test('Odin PWA shell is installable and navigates mobile screens', async ({ page, baseURL }) => {
  await page.goto('/app/');

  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/app/manifest.webmanifest');
  await expect(page.locator('.bottom-nav')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();

  const manifest = await page.request.get(`${baseURL}/app/manifest.webmanifest`);
  expect(manifest.ok()).toBeTruthy();
  const manifestJSON = await manifest.json();
  expect(manifestJSON.start_url).toBe('/app/');
  expect(manifestJSON.display).toBe('standalone');
  expect(manifestJSON.icons.length).toBeGreaterThanOrEqual(2);

  const serviceWorker = await page.request.get(`${baseURL}/app/service-worker.js`);
  expect(serviceWorker.ok()).toBeTruthy();
  await expect.poll(async () => await page.locator('.metric').count()).toBeGreaterThanOrEqual(6);

  for (const screen of ['Approvals', 'Review', 'Work', 'Inbox', 'Settings']) {
    await page.getByRole('button', { name: screen }).click();
    await expect(page.getByRole('heading', { name: screen === 'Work' ? 'Work & Runs' : screen === 'Inbox' ? 'Inbox Capture' : screen === 'Review' ? 'Review Queue' : screen })).toBeVisible();
  }

  await page.getByRole('button', { name: 'Inbox' }).click();
  await expect(page.getByPlaceholder('Capture is read-only until an Odin write endpoint is designed.')).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Capture disabled' })).toBeDisabled();
});

test('Odin PWA approval cards require confirmation and submit decisions', async ({ page }) => {
  let postedDecision = null;
  let approvalStatus = 'pending';

  await page.addInitScript(() => localStorage.setItem('odin_admin_token', 'mobile-token'));
  await page.route('**/mobile/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (request.method() === 'POST' && path === '/mobile/approvals/99/decision') {
      postedDecision = {
        headers: request.headers(),
        body: JSON.parse(request.postData() || '{}')
      };
      approvalStatus = 'approved';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          result: 'approved',
          summary: 'approval 99 approved',
          approval: approvalPayload(approvalStatus)
        })
      });
      return;
    }

    const responseByPath = {
      '/mobile/summary': { generated_at: '2026-05-13T00:00:00Z', readiness: { ready: true, health_status: 'healthy' }, runtime: { status: 'ready' }, counts: { approvals: 1, review_queue: 0, work_items: 0, run_attempts: 0, automation_triggers: 0, intake_items: 0 }, offline: { mode: 'shell-only', policy_statement: 'No offline approvals.' } },
      '/mobile/approvals': { generated_at: '2026-05-13T00:00:00Z', count: 1, approvals: [approvalPayload(approvalStatus)], items: [approvalPayload(approvalStatus)] },
      '/mobile/review': { generated_at: '2026-05-13T00:00:00Z', count: 0, items: [] },
      '/mobile/work': { generated_at: '2026-05-13T00:00:00Z', work_items: [], runs: [] },
      '/mobile/inbox': { generated_at: '2026-05-13T00:00:00Z', raw_items: [], linked_items: [], capture: { policy_statement: 'Capture disabled.' } },
      '/mobile/settings': { generated_at: '2026-05-13T00:00:00Z', runtime_source: 'odin-api', admin_actions: { enabled: true, policy_statement: 'Admin token required.' }, offline: { mode: 'shell-only', policy_statement: 'No offline approvals.' }, endpoints: ['/mobile/approvals'] }
    };
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(responseByPath[path] || {})
    });
  });

  await page.goto('/app/');
  await page.getByRole('button', { name: 'Approvals' }).click();

  await expect(page.getByRole('heading', { name: 'Critical deploy' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Approve' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Deny' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Clarify' })).toBeVisible();

  await page.getByPlaceholder('Decision reason').fill('safe deployment window');
  await page.getByRole('button', { name: 'Approve' }).click();
  await expect(page.locator('#approvals-status')).toContainText('requires confirmation text: APPROVE 99');
  expect(postedDecision).toBeNull();

  await page.getByPlaceholder('APPROVE 99').fill('APPROVE 99');
  await page.getByRole('button', { name: 'Approve' }).click();
  await expect.poll(() => postedDecision?.body?.action).toBe('approve');
  expect(postedDecision.headers['x-odin-admin-token']).toBe('mobile-token');
  expect(postedDecision.body.confirmation_text).toBe('APPROVE 99');
  expect(postedDecision.body.expected_policy_snapshot_hash).toBe('policy-99');
  expect(postedDecision.body.expected_runtime_snapshot_hash).toBe('runtime-99');
  await expect(page.locator('[data-approval-id="99"]')).not.toContainText('Approve');
});

function approvalPayload(status) {
  const pending = status === 'pending';
  return {
    id: 99,
    title: 'Critical deploy',
    status,
    risk_level: 'critical',
    source_object: 'odin-core/deploy-prod',
    requested_action: 'deploy production',
    required_reason: 'approval_required',
    evidence_context: ['task=deploy-prod', 'project=odin-core'],
    consequences: ['approval may allow external or irreversible side effects'],
    expires_at: '2026-05-13T01:00:00Z',
    policy_snapshot_hash: 'policy-99',
    runtime_snapshot_hash: 'runtime-99',
    audit_trail_preview: ['approval requested by operator'],
    actions: pending ? ['approve', 'deny', 'clarify'] : [],
    confirmation_prompt: pending ? 'APPROVE 99' : '',
    task_id: 123,
    task_key: 'deploy-prod',
    project_key: 'odin-core',
    resolver_support: 'supported'
  };
}
