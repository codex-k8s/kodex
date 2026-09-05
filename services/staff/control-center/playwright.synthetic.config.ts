import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  testMatch: /synthetic\.spec\.ts/,
  outputDir: "./test-results/synthetic",
  workers: 1,
  retries: 0,
  timeout: 45_000,
  forbidOnly: true,
  reporter: "list",
  use: {
    baseURL: "https://kodex.test",
    locale: "ru-RU",
    serviceWorkers: "block",
    trace: "off",
    launchOptions: {
      args: [
        "--use-fake-device-for-media-stream",
        "--use-fake-ui-for-media-stream",
      ],
    },
  },
  webServer: {
    command:
      "npm run preview -- --host 127.0.0.1 --port 43122 --strictPort --outDir dist-synthetic",
    url: "http://127.0.0.1:43122",
    reuseExistingServer: false,
    timeout: 30_000,
  },
});
