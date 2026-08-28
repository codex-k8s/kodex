import { defineConfig, devices } from "@playwright/test";

import { loadE2EEnvironment } from "./e2e/environment";

const environment = loadE2EEnvironment();

export default defineConfig({
  testDir: "./e2e",
  testMatch: /integration-path\.spec\.ts/,
  outputDir:
    process.env.KODEX_E2E_PRIVATE_OUTPUT_DIR ??
    "./test-results/integration-deployed",
  fullyParallel: false,
  forbidOnly: true,
  workers: 1,
  retries: 0,
  timeout: environment.runTimeoutMs,
  expect: { timeout: 30_000 },
  reporter: [["list"]],
  use: {
    ...devices["Desktop Chrome"],
    baseURL: environment.baseURL,
    storageState: environment.storageState,
    locale: "ru-RU",
    timezoneId: "Europe/Saratov",
    actionTimeout: 30_000,
    navigationTimeout: 30_000,
    screenshot: "off",
    trace: "off",
    video: "off",
  },
});
