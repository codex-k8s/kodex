import { defineConfig, devices } from "@playwright/test";

import {
  discoveryChromiumLaunchOptions,
  loadE2EAuthEnvironment,
} from "./e2e/environment";

const environment = loadE2EAuthEnvironment();
const launchOptions = discoveryChromiumLaunchOptions(environment.baseURL);

export default defineConfig({
  testDir: "./e2e",
  testMatch: /local-avatar-lifecycle\.ts/,
  outputDir:
    process.env.KODEX_E2E_PRIVATE_OUTPUT_DIR ||
    "./test-results/local-avatar-lifecycle",
  forbidOnly: true,
  workers: 1,
  retries: 0,
  timeout: 180_000,
  reporter: [["list"]],
  use: {
    ...devices["Desktop Chrome"],
    baseURL: environment.baseURL,
    locale: "ru-RU",
    screenshot: "only-on-failure",
    trace: "off",
    video: "off",
    launchOptions,
  },
});
