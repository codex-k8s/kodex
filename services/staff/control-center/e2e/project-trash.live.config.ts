import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.KODEX_E2E_BASE_URL;
if (baseURL !== "https://control.127.0.0.1.nip.io") {
  throw new Error("Local project trash test requires the exact local Control Center origin");
}

export default defineConfig({
  testDir: ".",
  testMatch: "project-trash.live.spec.ts",
  workers: 1,
  retries: 0,
  timeout: 180_000,
  expect: { timeout: 20_000 },
  reporter: "list",
  outputDir: "../test-results/project-trash-live",
  use: {
    ...devices["Desktop Chrome"],
    baseURL,
    locale: "ru-RU",
    timezoneId: "Europe/Moscow",
    viewport: { width: 1440, height: 900 },
    screenshot: "off",
    trace: "off",
    video: "off",
  },
});
