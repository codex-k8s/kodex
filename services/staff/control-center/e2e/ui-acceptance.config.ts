import { defineConfig } from "@playwright/test";
import { selectedVariants } from "./ui-acceptance-proof";
import { loadFixtureManifest } from "./ui-fixture-manifest";
import { loadE2ESessionRenewalEnvironment } from "./environment";
const environment = loadE2ESessionRenewalEnvironment();
const createMode = process.env.KODEX_E2E_UI_CREATE_PROJECTS ?? "0";
selectedVariants(process.env.KODEX_E2E_UI_VARIANTS, createMode);
const fixture = loadFixtureManifest(process.env.KODEX_E2E_UI_FIXTURE_MANIFEST);
if (fixture.manifest && createMode !== "0")
  throw new Error("Pinned fixture profile is readonly");
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
