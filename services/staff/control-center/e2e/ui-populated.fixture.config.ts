import { defineConfig } from "@playwright/test";
import base from "../playwright.synthetic.config";
export default defineConfig({
  ...base,
  testDir: ".",
  testMatch: [
    "ui-populated.synthetic.spec.ts",
    "ui-document-navigation.synthetic.spec.ts",
  ],
  workers: 3,
  timeout: 30000,
  globalTimeout: 300000,
  outputDir: "../test-results/ui-populated",
});
