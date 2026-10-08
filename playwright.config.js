const { defineConfig } = require('@playwright/test');
const { mkdtempSync } = require('node:fs');
const { tmpdir } = require('node:os');
const { join } = require('node:path');

const port = process.env.PASTE_E2E_PORT || '8098';
const baseURL = `http://127.0.0.1:${port}`;

module.exports = defineConfig({
  testDir: './e2e',
  workers: 1,
  use: {
    baseURL,
    screenshot: 'only-on-failure',
    launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE },
  },
  webServer: {
    command: 'go run ./cmd/pastebin',
    url: `${baseURL}/healthz`,
    env: {
      BASE_URL: baseURL,
      LISTEN_ADDR: `127.0.0.1:${port}`,
      DATA_DIR: mkdtempSync(join(tmpdir(), 'paste-e2e-')),
      DATABASE_PATH: '',
      SESSION_SECRET: 'local-browser-test-secret',
    },
    timeout: 120000,
  },
});
