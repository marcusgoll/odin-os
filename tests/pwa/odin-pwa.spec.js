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
