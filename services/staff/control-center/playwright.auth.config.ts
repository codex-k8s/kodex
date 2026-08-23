import { defineConfig, devices } from "@playwright/test";

import { loadE2EAuthEnvironment } from "./e2e/environment";

const environment = loadE2EAuthEnvironment();

export default defineConfig({
  testDir: "./e2e",
  testMatch: /auth\.setup\.ts/,
  outputDir: "./test-results/auth",
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
    trace: "off",
    video: "off",
  },
});
