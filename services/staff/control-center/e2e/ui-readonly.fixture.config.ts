import { defineConfig } from "@playwright/test";
import base from "./ui-acceptance.fixture.config";
export default defineConfig({
  ...base,
  testMatch: "ui-readonly-forms.synthetic.spec.ts",
  outputDir: "../test-results/ui-readonly-fixture",
});
