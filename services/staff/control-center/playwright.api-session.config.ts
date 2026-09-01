import { defineConfig, devices } from "@playwright/test";

import { loadE2EAPISessionEnvironment } from "./e2e/environment";

const environment = loadE2EAPISessionEnvironment();

export default defineConfig({
  testDir: "./e2e",
  testMatch: /api-session\.setup\.ts/,
  outputDir: "./test-results/api-session",
  forbidOnly: true,
  workers: 1,
  retries: 0,
  timeout: 120_000,
  reporter: [["list"]],
  use: {
    ...devices["Desktop Chrome"],
    baseURL: environment.baseURL,
    locale: "ru-RU",
    screenshot: "off",
    storageState: environment.inputStorageState,
    trace: "off",
    video: "off",
  },
});
