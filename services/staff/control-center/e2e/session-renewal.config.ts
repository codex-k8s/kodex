import { defineConfig } from "@playwright/test";
import { loadE2ESessionRenewalEnvironment } from "./environment";

const environment = loadE2ESessionRenewalEnvironment();
const browserName = process.env.KODEX_E2E_BROWSER ?? "chromium";
if (!["chromium", "firefox", "webkit"].includes(browserName))
  throw new Error("Unsupported session proof browser");
export default defineConfig({
  testDir: ".",
  testMatch: "session-renewal.live.spec.ts",
  outputDir: "../test-results/session-renewal",
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
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
    screenshot: "off",
    trace: "off",
    video: "off",
  },
});
