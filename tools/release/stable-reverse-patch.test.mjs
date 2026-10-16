import assert from "node:assert/strict";
import test from "node:test";
import { applyReverseSpec, reverseSpecPatch } from "./stable-reverse-patch.mjs";

test("inverse keeps unchanged private env out of evidence across named array shifts", () => {
  const before = { template: { spec: { containers: [{ name: "app", env: [
    { name: "GOCACHE", value: "/go" }, { name: "PRIVATE", value: "PRIVATE_VALUE_SENTINEL" }, { name: "GOTOOLCHAIN", value: "local" },
  ] }], volumes: [{ name: "source", hostPath: { path: "/fixture/source" } }, { name: "secret", secret: { secretName: "reference-only" } }] } } };
  const after = structuredClone(before);
  after.template.spec.containers[0].env = [after.template.spec.containers[0].env[1]];
  after.template.spec.volumes.shift();
  const patch = reverseSpecPatch(before, after);
  assert.equal(JSON.stringify(patch).includes("PRIVATE_VALUE_SENTINEL"), false);
  assert.equal(JSON.stringify(patch).includes("reference-only"), false);
  assert.deepEqual(applyReverseSpec(after, patch), before);
});
test("inverse roundtrips nested objects named ordering and repeated positional arguments", () => {
  const before = { args: ["module", "binary", "service", "controller", "controller"],
    ports: [{ name: "http", port: 8080 }], annotations: { "kodex.dev/a~b": "source" },
    env: [{ name: "a", value: "1" }, { name: "b", value: "2" }] };
  const after = { args: ["controller", "controller"], ports: [{ name: "https", port: 8443 }],
    annotations: { "new/key": "stable" }, env: [{ name: "b", value: "new" }, { name: "a", value: "1" }] };
  assert.deepEqual(applyReverseSpec(after, reverseSpecPatch(before, after)), before);
  assert.deepEqual(reverseSpecPatch(before, before), []);
});
test("inverse cannot mutate metadata status or prototype through unowned pointer paths", () => {
  for (const path of ["/metadata/uid", "/status", "/spec/foreign/value", "/spec/~invalid", "/spec/array/01"])
    assert.throws(() => applyReverseSpec({ array: [] }, [{ op: "add", path, value: "bad" }]), /REVERSE_PATCH/);
  const spec = JSON.parse('{"annotations":{"__proto__":"own-data"}}');
  assert.deepEqual(applyReverseSpec({}, reverseSpecPatch(spec, {})), spec);
  assert.equal({}.polluted, undefined);
});
