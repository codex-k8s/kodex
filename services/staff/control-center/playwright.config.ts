import { defineConfig, devices } from "@playwright/test";

import { loadE2EEnvironment } from "./e2e/environment";

const environment = loadE2EEnvironment();
const projects =
  environment.profile === "mattermost"
    ? [
        {
          name: "mattermost-desktop-chromium",
          testMatch: /mattermost\.spec\.ts/,
          use: { ...devices["Desktop Chrome"] },
        },
      ]
    : [
        {
          name: "web-only-desktop-chromium",
          testMatch: /web-only\.spec\.ts/,
          use: { ...devices["Desktop Chrome"] },
        },
        {
          name: "web-only-mobile-chromium",
          testMatch: /responsive\.spec\.ts/,
          dependencies: ["web-only-desktop-chromium"],
          use: { ...devices["Pixel 7"] },
        },
      ];

export default defineConfig({
  testDir: "./e2e",
  outputDir: "./test-results",
  fullyParallel: false,
  forbidOnly: true,
  workers: 1,
  retries: 0,
  timeout: environment.runTimeoutMs,
  expect: { timeout: 30_000 },
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: environment.baseURL,
    storageState: environment.storageState,
    locale: "ru-RU",
    timezoneId: "Europe/Moscow",
    actionTimeout: 30_000,
    navigationTimeout: 30_000,
    screenshot: "only-on-failure",
    // Authenticated traces and video may capture cookies, bearer redirects,
    // user input or integration metadata. The supported suite never records them.
    trace: "off",
    video: "off",
  },
  projects,
});
