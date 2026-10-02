import assert from "node:assert/strict";
import test from "node:test";
import { classifyPresence } from "./reconcile-local-proxy-session-store.mjs";

test("классифицирует только пустую, полную и частичную установку", () => {
  assert.equal(classifyPresence(0, 11), "absent");
  assert.equal(classifyPresence(11, 11), "complete");
  assert.equal(classifyPresence(3, 11), "partial");
});
