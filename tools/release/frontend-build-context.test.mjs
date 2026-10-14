import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const dockerfile = readFileSync(new URL("../../services/staff/control-center/Dockerfile", import.meta.url), "utf8");

test("production build retains full typecheck and materializes fixture config dependencies", () => {
  assert.ok(dockerfile.includes("RUN npm run build"));
  for (const pattern of ["tsconfig*.json", "playwright*.config.ts", "vite*.config.ts"])
    assert.ok(dockerfile.includes(`services/staff/control-center/${pattern}`));
  for (const file of ["provider-api-key-acceptance.mjs", "provider-api-key-acceptance.d.mts",
    "owner-session-storage.mjs", "owner-session-storage.d.mts", "owner-session-client.mjs", "runtime-workspace-acceptance.mjs"])
    assert.ok(dockerfile.includes(`tools/dev/${file}`));
  const runtime = dockerfile.slice(dockerfile.lastIndexOf("\nFROM "));
  assert.equal(runtime.includes("tools/dev/"), false);
  assert.equal(runtime.includes("e2e/"), false);
  assert.equal(dockerfile.includes("COPY . "), false);
});
