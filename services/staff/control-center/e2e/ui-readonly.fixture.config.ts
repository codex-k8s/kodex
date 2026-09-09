import { defineConfig } from "@playwright/test";
import base from "./ui-acceptance.fixture.config";
export default defineConfig({
  ...base,
  projects: ["chromium", "firefox", "webkit"].map((browserName) => ({
    name: browserName,
    use: { browserName: browserName as "chromium" | "firefox" | "webkit" },
  })),
  testMatch: "ui-readonly-forms.synthetic.spec.ts",
  outputDir: "../test-results/ui-readonly-fixture",
});
