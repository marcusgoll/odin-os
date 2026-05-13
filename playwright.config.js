const { defineConfig, devices } = require('@playwright/test');

module.exports = defineConfig({
  testDir: './tests/pwa',
  timeout: 30_000,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: process.env.ODIN_PWA_BASE_URL || 'http://127.0.0.1:9443',
    trace: 'retain-on-failure'
  },
  projects: [
    {
      name: 'mobile-chrome',
      use: {
        ...devices['Pixel 5'],
        channel: 'chrome'
      }
    }
  ]
});
