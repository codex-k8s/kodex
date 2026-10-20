import assert from "node:assert/strict";
import test from "node:test";
import { refreshAPIRules } from "./reconcile-local-api-egress.mjs";
function fixture() { return { metadata: { name: "control-plane-exact-runtime-paths", namespace: "kodex-system" }, spec: {
  podSelector: { matchLabels: { "app.kubernetes.io/name": "control-plane" } }, policyTypes: ["Ingress", "Egress"],
  ingress: [{ ports: [{ port: 9090, protocol: "TCP" }] }], egress: [
    { to: [{ namespaceSelector: { matchLabels: { "kubernetes.io/metadata.name": "kube-system" } }, podSelector: { matchLabels: { "k8s-app": "kube-dns" } } }], ports: [{ port: 53, protocol: "UDP" }] },
    { to: [{ ipBlock: { cidr: "172.19.0.2/32" } }], ports: [{ port: 6443, protocol: "TCP" }] }] } }; }
test("only API destination changes, obsolete address disappears and stable reconciliation is idempotent", () => {
  const before = fixture(), snapshot = structuredClone(before), after = refreshAPIRules(before, ["172.19.0.4"], [6443]);
  assert.deepEqual(before, snapshot); assert.deepEqual(after.ingress, before.spec.ingress); assert.deepEqual(after.egress[0], before.spec.egress[0]);
  assert.deepEqual(after.egress[1].to, [{ ipBlock: { cidr: "172.19.0.4/32" } }]);
  assert.deepEqual(refreshAPIRules({ ...before, spec: after }, ["172.19.0.4"], [6443]), after);
});
test("unknown workload, port, selectors, except ranges and incomplete discovery fail closed", () => {
  for (const mutate of [p => p.metadata.name = "foreign", p => p.metadata.namespace = "foreign",
    p => p.spec.podSelector.matchLabels["app.kubernetes.io/name"] = "agent-runner",
    p => p.spec.egress[1].to[0].ipBlock.except = ["172.19.0.3/32"],
    p => p.spec.egress[1].to.push({ podSelector: {} }), p => p.spec.egress[1].ports.push({ port: 443, protocol: "TCP" }),
    p => p.spec.egress.push(structuredClone(p.spec.egress[1]))]) {
    const policy = fixture(); mutate(policy); assert.throws(() => refreshAPIRules(policy, ["172.19.0.4"], [6443]));
  }
  for (const [ips, ports] of [[[], [6443]], [["invalid"], [6443]], [["172.19.0.4"], [443]], [["172.19.0.4", "172.19.0.4"], [6443]]])
    assert.throws(() => refreshAPIRules(fixture(), ips, ports));
});
