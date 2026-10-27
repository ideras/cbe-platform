import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/ui',
  fullyParallel: false,
  workers: 1,
  reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:4178', headless: true },
  webServer: { command: 'npm run dev -- --mode ui-test --port 4178 --strictPort', url: 'http://127.0.0.1:4178', reuseExistingServer: false },
});
