import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: 'tests/e2e',
  timeout: 30_000,
  use: {
    baseURL: 'http://127.0.0.1:8765',
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'go run ./cmd/gork',
    url: 'http://127.0.0.1:8765/health',
    reuseExistingServer: true,
    timeout: 120_000,
    env: {
      HOST: '127.0.0.1',
      PORT: '8765',
      DATA_DIR: '.tmp/playwright-data',
      LOG_FILE_ENABLED: 'false',
      GROK_APP_WEBUI_ENABLED: 'true',
      GROK_APP_WEBUI_KEY: '',
    },
  },
});
