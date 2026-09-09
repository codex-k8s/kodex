import { defineConfig } from "@playwright/test";
import base from "./ui-readonly.fixture.config";

export default defineConfig({
  ...base,
  testMatch: "auth-flow.synthetic.spec.ts",
  outputDir: "../test-results/auth-flow-fixture",
  timeout: 15000,
  globalTimeout: 180000,
  workers: 3,
});
