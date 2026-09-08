import { defineConfig } from "@playwright/test";
import { loadE2ESessionRenewalEnvironment } from "./environment";
const environment = loadE2ESessionRenewalEnvironment();
const browserName = process.env.KODEX_E2E_BROWSER ?? "chromium";
if (!["chromium", "firefox", "webkit"].includes(browserName))
  throw new Error("Unsupported UI proof browser");
export default defineConfig({
  testDir: ".",
  testMatch: "ui-acceptance.live.spec.ts",
  outputDir: "../test-results/ui-acceptance",
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: environment.runTimeoutMs,
  reporter: [["list"]],
  use: {
    browserName: browserName as "chromium" | "firefox" | "webkit",
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
