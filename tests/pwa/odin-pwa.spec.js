const { test, expect } = require('@playwright/test');

test('Odin PWA shell is installable and renders live operator sections', async ({ page, baseURL }) => {
  await page.addInitScript(() => sessionStorage.setItem('odin.mobile.csrf', 'mobile-csrf'));
  await mockMobileAPI(page);
  await page.goto('/app/');

  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/app/manifest.webmanifest');
  await expect(page.getByRole('heading', { name: 'What needs me now?' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Action Required' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Browser Needs Help' })).toBeVisible();

  const manifest = await page.request.get(`${baseURL}/app/manifest.webmanifest`);
  expect(manifest.ok()).toBeTruthy();
  const manifestJSON = await manifest.json();
  expect(manifestJSON.start_url).toBe('/app/');
  expect(manifestJSON.display).toBe('standalone');
  expect(manifestJSON.icons.length).toBeGreaterThanOrEqual(2);

  const serviceWorker = await page.request.get(`${baseURL}/app/service-worker.js`);
  expect(serviceWorker.ok()).toBeTruthy();
  await expect(page.getByText('No action-required rows', { exact: true })).toBeVisible();

  await expect(page.getByRole('button', { name: 'Capture raw intake' })).toBeEnabled();
  await expect(page.locator('[data-capture-kind="note"]')).toBeChecked();
  await expect(page.getByRole('heading', { name: 'Failed Uploads' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Retry failed captures' })).toBeVisible();
});

test('Odin PWA approval cards submit authenticated decisions', async ({ page }) => {
  let postedDecision = null;
  let approvalStatus = 'pending';

  await page.addInitScript(() => sessionStorage.setItem('odin.mobile.csrf', 'mobile-csrf'));
  await mockMobileAPI(page, {
    onDecision: async (route) => {
      const request = route.request();
      postedDecision = {
        headers: request.headers(),
        body: JSON.parse(request.postData() || '{}')
      };
      approvalStatus = 'approved';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          approval_id: 99,
          task_id: 123,
          status: 'approved',
          action: 'approve',
          resolver_support: 'supported',
          resolved_at: '2026-05-13T00:05:00Z'
        })
      });
    },
    approval: () => approvalStatus === 'pending' ? approvalPayload() : null
  });

  await page.goto('/app/');

  await expect(page.getByRole('heading', { name: 'deploy-prod' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Approve approval 99' })).toBeVisible();
  await page.getByRole('button', { name: 'Approve approval 99' }).click();
  await page.getByPlaceholder('Required audit reason').fill('safe deployment window');
  await page.getByRole('button', { name: 'Confirm approval decision' }).click();

  await expect.poll(() => postedDecision?.body?.action).toBe('approve');
  expect(postedDecision.headers['x-odin-csrf']).toBe('mobile-csrf');
  expect(postedDecision.body.decision_by).toBe('odin-pwa');
  await expect(page.getByText('No pending approvals')).toBeVisible();
});

test('Odin PWA surfaces registration configuration failures', async ({ page }) => {
  await page.route('**/app/session', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ authenticated: false })
    });
  });
  await page.route('**/mobile/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (request.method() === 'POST' && path === '/mobile/devices/register') {
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          error: {
            code: 'admin_disabled',
            message: 'admin actions are disabled'
          }
        })
      });
      return;
    }
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({
        error: {
          code: 'admin_auth_required',
          message: 'admin authentication is required'
        }
      })
    });
  });
  page.on('dialog', async (dialog) => {
    await dialog.accept('attempted-token');
  });

  await page.goto('/app/');
  await expect(page.locator('#dashboard-error')).toBeHidden();
  await expect(page.locator('#capture-status')).toHaveText('Register this device to load Odin projections.');

  await page.getByRole('button', { name: 'Register this mobile device' }).click();

  await expect(page.locator('#capture-status')).toHaveText('Device registration failed: Odin admin token is not configured on this server.');
  await expect(page.locator('#dashboard-error')).toHaveText('Device registration failed: Odin admin token is not configured on this server.');
});

test('Odin PWA normalizes copied admin token when registering', async ({ page }) => {
  let authenticated = false;
  let authorization = '';
  let statusLoaded = false;

  await page.route('**/app/session', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ authenticated })
    });
  });
  await page.route('**/mobile/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (request.method() === 'POST' && path === '/mobile/devices/register') {
      authorization = request.headers().authorization || '';
      authenticated = true;
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          device_id: 'phone',
          session_id: 7,
          csrf_token: 'mobile-csrf',
          expires_at: '2026-06-13T00:00:00Z'
        })
      });
      return;
    }
    const responseByPath = {
      '/mobile/status': { generated_at: '2026-05-13T00:00:00Z', ready: true, health_status: 'healthy' },
      '/mobile/overview': overviewPayload(),
      '/mobile/review-queue': { generated_at: '2026-05-13T00:00:00Z', count: 0, items: [] },
      '/mobile/approvals': { generated_at: '2026-05-13T00:00:00Z', count: 0, items: [] },
      '/mobile/browser/status': { generated_at: '2026-05-13T00:00:00Z', session_count: 0, login_request_count: 0, runner_count: 0, sessions: [], login_requests: [], runners: [] },
      '/mobile/notifications/preferences': { status: 'not_configured', enabled: false, delivery_modes: ['web_push'], subscriptions: [] }
    };
    if (path === '/mobile/status') statusLoaded = true;
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(responseByPath[path] || {})
    });
  });
  page.on('dialog', async (dialog) => {
    await dialog.accept('ODIN_ADMIN_TOKEN=" secret-token "');
  });

  await page.goto('/app/');
  await expect(page.locator('#capture-status')).toHaveText('Register this device to load Odin projections.');

  await page.getByRole('button', { name: 'Register this mobile device' }).click();

  await expect.poll(() => authorization).toBe('Bearer secret-token');
  await expect.poll(() => statusLoaded).toBe(true);
  await expect(page.locator('#capture-status')).toHaveText('Device registered for this browser session.');
  await expect(page.locator('#dashboard-error')).toBeHidden();
});

test('Odin PWA explains invalid registration tokens', async ({ page }) => {
  await page.route('**/app/session', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ authenticated: false })
    });
  });
  await page.route('**/mobile/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (request.method() === 'POST' && path === '/mobile/devices/register') {
      await route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({
          error: {
            code: 'admin_auth_failed',
            message: 'admin authentication failed'
          }
        })
      });
      return;
    }
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({
        error: {
          code: 'admin_auth_required',
          message: 'admin authentication is required'
        }
      })
    });
  });
  page.on('dialog', async (dialog) => {
    await dialog.accept('stale-token');
  });

  await page.goto('/app/');
  await page.getByRole('button', { name: 'Register this mobile device' }).click();

  const message = 'Device registration failed: Admin token did not match this Odin server. Run odin mobile token on the server and paste that value.';
  await expect(page.locator('#capture-status')).toHaveText(message);
  await expect(page.locator('#dashboard-error')).toHaveText(message);
});

async function mockMobileAPI(page, options = {}) {
  await page.route('**/app/session', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ authenticated: true })
    });
  });
  await page.route('**/mobile/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (request.method() === 'POST' && path === '/mobile/approvals/99/decision') {
      await options.onDecision(route);
      return;
    }
    const approval = options.approval ? options.approval() : null;

    const responseByPath = {
      '/mobile/status': { generated_at: '2026-05-13T00:00:00Z', ready: true, health_status: 'healthy' },
      '/mobile/overview': overviewPayload(),
      '/mobile/review-queue': { generated_at: '2026-05-13T00:00:00Z', count: 0, items: [] },
      '/mobile/approvals': { generated_at: '2026-05-13T00:00:00Z', count: approval ? 1 : 0, items: approval ? [approval] : [] },
      '/mobile/browser/status': { generated_at: '2026-05-13T00:00:00Z', session_count: 0, login_request_count: 0, runner_count: 0, sessions: [], login_requests: [], runners: [] },
      '/mobile/notifications/preferences': { status: 'not_configured', enabled: false, delivery_modes: ['web_push'], subscriptions: [] }
    };
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(responseByPath[path] || {})
    });
  });
}

function overviewPayload() {
  return {
    generated_at: '2026-05-13T00:00:00Z',
    readiness: { status: 'ready', note: 'fixture ready' },
    actual_use: { action_required_count: 0, open_work_item_count: 0, active_run_count: 0 },
    review_queue: { total_count: 0 },
    observability: { blocked_work: [], recovery_guidance: [], active_runs: [] },
    notifications: { notifications_enabled: false, in_app_unread_count: 0, quiet_hours: 'none', batching: 'none' },
    intake_inbox: { raw_item_count: 0, status: 'empty', note: 'No raw intake.' },
    automation_triggers: { trigger_count: 0, enabled_count: 0 }
  };
}

function approvalPayload() {
  return {
    approval_id: 99,
    task_id: 123,
    task_key: 'deploy-prod',
    project_key: 'odin-core',
    status: 'pending',
    resolver_support: 'supported'
  };
}
