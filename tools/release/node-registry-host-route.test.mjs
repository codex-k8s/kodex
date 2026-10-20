import test from "node:test";
import assert from "node:assert/strict";
import { repairHosts } from "./node-registry-host-route.mjs";
test("exact registry host repair preserves other records and is idempotent", () => {
  const before = "127.0.0.1 localhost\n172.18.0.1 other.example\n";
  const after = repairHosts(before, "pull.example", "172.19.0.3");
  assert.equal(after, before + "172.19.0.3 pull.example\n");
  assert.equal(repairHosts(after, "pull.example", "172.19.0.3"), after);
  assert.equal(repairHosts(after, "pull.example", "172.19.0.4"), before + "172.19.0.4 pull.example\n");
  assert.throws(() => repairHosts(before + "172.19.0.3 pull.example another.example\n", "pull.example", "172.19.0.4"), /AMBIGUOUS/);
  for (const address of ["127.0.0.1", "0.0.0.0", "::1", "224.1.1.1", "not-ip"]) assert.throws(() => repairHosts(before, "pull.example", address));
  for (const host of ["user:password@pull.example", "pull.example/path", "pull.example\nother"]) assert.throws(() => repairHosts(before, host, "172.19.0.3"));
});
