import { defineConfig, devices } from "@playwright/test";

import { loadE2EEnvironment } from "./e2e/environment";

const environment = loadE2EEnvironment();

export default defineConfig({
  testDir: "./e2e",
  outputDir: "./test-results/discovery",
  fullyParallel: false,
  forbidOnly: true,
  workers: 1,
  retries: 0,
  timeout: environment.runTimeoutMs,
  expect: { timeout: 30_000 },
  reporter: [["list"], ["./e2e/discovery-reporter.ts"]],
  use: {
    baseURL: environment.baseURL,
    storageState: environment.storageState,
    locale: "ru-RU",
    timezoneId: "Europe/Moscow",
    actionTimeout: 30_000,
    navigationTimeout: 30_000,
    screenshot: "only-on-failure",
    trace: "off",
    video: "off",
  },
  projects: [
    {
      name: "web-only-discovery-desktop-chromium",
      testMatch: /web-only\.spec\.ts/,
      use: { ...devices["Desktop Chrome"] },
    },
    {
      name: "web-only-discovery-mobile-chromium",
      testMatch: /responsive\.spec\.ts/,
      use: { ...devices["Pixel 7"] },
    },
  ],
});
