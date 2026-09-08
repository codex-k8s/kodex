import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: ".",
  testMatch: "ui-acceptance.synthetic.spec.ts",
  workers: 1,
  retries: 0,
  timeout: 30_000,
  reporter: "list",
  outputDir: "../test-results/ui-acceptance-fixture",
  use: {
    browserName: "chromium",
    baseURL: "https://kodex.test",
    screenshot: "off",
    trace: "off",
    video: "off",
    serviceWorkers: "block",
  },
});
