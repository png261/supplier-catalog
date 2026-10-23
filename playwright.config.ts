import { defineConfig, devices } from '@playwright/test';

const PORT = process.env.PLAYWRIGHT_PORT || '18080';
const BASE_URL = `http://127.0.0.1:${PORT}`;

export default defineConfig({
  testDir: './tests/playwright',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  reporter: [['list']],
  timeout: 60000,
  expect: {
    timeout: 15000,
  },
  use: {
    baseURL: BASE_URL,
    trace: 'retain-on-failure',
    headless: true,
    viewport: { width: 1440, height: 900 },
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: `rm -rf /tmp/pw-catalog-e2e* && ./bin/server -port ${PORT} -db /tmp/pw-catalog-e2e/catalog.db`,
    url: `${BASE_URL}/swagger/doc.json`,
    reuseExistingServer: false,
    timeout: 30000,
  },
});
