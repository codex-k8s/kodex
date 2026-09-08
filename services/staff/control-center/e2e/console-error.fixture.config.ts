import { defineConfig } from "@playwright/test";
import base from "../playwright.synthetic.config";
export default defineConfig({
  ...base,
  testDir: ".",
  testMatch: "console-error.synthetic.spec.ts",
  webServer: undefined,
  workers: 3,
  timeout: 15000,
  globalTimeout: 90000,
  outputDir: "../test-results/console-error",
});
