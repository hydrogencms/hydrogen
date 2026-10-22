import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  // E2E_PROJECT picks one spec file for a project other than the default (examples/multisite): content ->
  // extensions.spec.ts and richtext-custom.spec.ts, starter -> blocks.spec.ts, canvas.spec.ts and share.spec.ts (preview host fixture), next -> real-next.spec.ts (the Next.js
  // starter), fullstack -> real-site.spec.ts and share.spec.ts (the site module). The default run skips all of them.
  ...(process.env.E2E_PROJECT
    ? { testMatch: { content: ['extensions.spec.ts', 'richtext-custom.spec.ts'], starter: ['blocks.spec.ts', 'canvas.spec.ts', 'share.spec.ts'], next: 'real-next.spec.ts', fullstack: ['real-site.spec.ts', 'share.spec.ts'] }[process.env.E2E_PROJECT] ?? 'none' }
    : { testIgnore: ['extensions.spec.ts', 'richtext-custom.spec.ts', 'blocks.spec.ts', 'canvas.spec.ts', 'real-next.spec.ts', 'real-site.spec.ts', 'share.spec.ts'] }),
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
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], launchOptions: { args: ['--disable-features=LocalNetworkAccessChecks'] } } }],
});
