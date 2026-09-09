import { defineConfig } from "@playwright/test";
import base from "./ui-readonly.fixture.config";
export default defineConfig({
  ...base,
  testMatch: "document-lifecycle.synthetic.spec.ts",
  outputDir: "../test-results/document-lifecycle",
  timeout: 20000,
  globalTimeout: 180000,
  workers: 3,
});
