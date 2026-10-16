import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import test from "node:test";

function resources() {
  return execFileSync("kubectl", ["kustomize", new URL(".", import.meta.url).pathname], { encoding: "utf8" });
}
test("two identity pods use jdbc discovery and encrypted cache while DB stays single", () => {
  const rendered = resources();
  const documents = rendered.split(/^---\s*$/m);
  const deployment = documents.find(d => /kind: Deployment/.test(d) && /name: sso\n/.test(d));
  const database = documents.find(d => /kind: StatefulSet/.test(d));
  assert.ok(deployment); assert.match(deployment, /replicas: 2/);
  assert.match(deployment, /name: KC_CACHE\n\s+value: ispn/);
  assert.match(deployment, /name: KC_CACHE_STACK\n\s+value: jdbc-ping/);
  assert.match(deployment, /name: KC_CACHE_EMBEDDED_MTLS_ENABLED\n\s+value: "true"/);
  assert.match(database, /replicas: 1/);
  assert.ok(documents.some(d => /kind: PodDisruptionBudget/.test(d) && /minAvailable: 1/.test(d)));
});

test("peer cache traffic has exact same-namespace pod selectors in both directions", () => {
  const policy = readFileSync(new URL("networkpolicy.yaml", import.meta.url), "utf8").split(/^---\s*$/m)
    .find(d => d.includes("name: sso-exact-paths"));
  for (const section of [policy.split("  ingress:\n")[1].split("  egress:\n")[0], policy.split("  egress:\n")[1]]) {
    assert.match(section, /app.kubernetes.io\/name: sso/);
    assert.match(section, /app.kubernetes.io\/component: identity-provider/);
    assert.match(section, /port: 7800/); assert.match(section, /port: 57800/);
    assert.equal(section.includes("0.0.0.0/0"), false);
  }
});
