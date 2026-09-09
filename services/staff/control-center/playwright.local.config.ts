import { defineConfig, devices } from "@playwright/test";

import { loadE2EAuthEnvironment } from "./e2e/environment";

const environment = loadE2EAuthEnvironment();

export default defineConfig({
  testDir: "./e2e",
  testMatch: /local-smoke\.ts/,
  outputDir:
    process.env.KODEX_E2E_PRIVATE_OUTPUT_DIR || "./test-results/local-smoke",
  forbidOnly: true,
  workers: 1,
  retries: 0,
  timeout: 120_000,
  reporter: [["list"]],
  use: {
    ...devices["Desktop Chrome"],
    baseURL: environment.baseURL,
    locale: "ru-RU",
    screenshot: "only-on-failure",
    trace: "off",
    video: "off",
  },
});
