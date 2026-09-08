import { defineConfig } from "@playwright/test";
import base from "../playwright.synthetic.config";
export default defineConfig({
  ...base,
  testDir: ".",
  testMatch: "dev-reload.synthetic.spec.ts",
  webServer: undefined,
  workers: 3,
  timeout: 15000,
  globalTimeout: 90000,
  outputDir: "../test-results/dev-reload",
});
