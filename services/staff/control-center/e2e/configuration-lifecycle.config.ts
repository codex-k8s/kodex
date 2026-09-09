import { defineConfig } from "@playwright/test";

import { loadE2ESessionRenewalEnvironment } from "./environment";
import { loadLifecycleConfiguration } from "./configuration-lifecycle-proof";
import { versionsFromEnvironment } from "./ui-acceptance-proof";

const environment = loadE2ESessionRenewalEnvironment();
const configuration = await loadLifecycleConfiguration(
  process.env,
  versionsFromEnvironment(process.env),
);

export default defineConfig({
  testDir: ".",
  testMatch: "configuration-lifecycle.live.spec.ts",
  outputDir: "../test-results/configuration-lifecycle",
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: configuration.runTimeoutMs,
  reporter: [["list"]],
  use: {
    browserName: configuration.browser,
    baseURL: environment.baseURL,
    storageState: environment.storageState,
    locale: "ru-RU",
    actionTimeout: 10_000,
    navigationTimeout: 20_000,
    serviceWorkers: "block",
    screenshot: "off",
    trace: "off",
    video: "off",
  },
});
