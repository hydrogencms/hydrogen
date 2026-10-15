import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  // E2E_PROJECT=content runs only the extensions spec (against examples/content); the default run skips it.
  ...(process.env.E2E_PROJECT === 'content' ? { testMatch: 'extensions.spec.ts' } : { testIgnore: 'extensions.spec.ts' }),
  outputDir: '../../.work/e2e',
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: process.env.BASE_URL ?? 'http://127.0.0.1:8080',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
