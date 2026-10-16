import assert from "node:assert/strict";
import test from "node:test";
import { retainedPublicationImage, retainedServingEvidence } from "./stable-retained-artifact.mjs";

function fixture(component = "image-admission") {
  const name = component === "image-admission" ? "image-admission-controller" : "kodex-image-registry-promotion";
  const metadata = (name, uid) => ({ name, uid, namespace: "kodex-system", labels: { "app.kubernetes.io/part-of": "kodex" } });
  const digest = "sha256:" + "a".repeat(64), image = "registry.local.kodex/kodex/" + component + "@" + digest;
  const deployment = { kind: "Deployment", metadata: metadata(name, "deployment"),
    spec: { template: { spec: { containers: [{ name: "consumer", image, env: [{ name: "PRIVATE", value: "SENTINEL_PRIVATE_SPEC" }] }] } } } };
  const set = { kind: "ReplicaSet", metadata: { ...metadata("replica-set", "set"), ownerReferences: [{ kind: "Deployment", uid: "deployment", controller: true }] } };
  const pod = { kind: "Pod", metadata: { ...metadata("pod", "pod"), ownerReferences: [{ kind: "ReplicaSet", uid: "set", controller: true }] },
    spec: deployment.spec.template.spec, status: { conditions: [{ type: "Ready", status: "True" }],
      containerStatuses: [{ name: "consumer", ready: true, imageID: image }] } };
  return { objects: structuredClone([deployment, set, pod]), digest };
}

test("retained proof binds exact controller lineage and serving digest without raw spec or build claims", () => {
  for (const component of ["image-admission", "image-admission-tools"]) {
    const { objects, digest } = fixture(component), proof = retainedServingEvidence(component, objects);
    assert.equal(proof.digest, digest); assert.equal(proof.serving.length, 1);
    assert.equal(JSON.stringify(proof).includes("SENTINEL_PRIVATE_SPEC"), false);
    assert.deepEqual(retainedPublicationImage(proof), { component, digest, tag: "retained-" + "a".repeat(64) });
    const next = structuredClone(objects); next[2].metadata.uid = "replacement-pod";
    assert.deepEqual(retainedServingEvidence(component, next).deployments, proof.deployments);
  }
});

test("retained proof rejects foreign namespace owner forged imageID and mixed target versions", () => {
  for (const mutate of [
    rows => { rows[0].metadata.name = "user-controlled-deployment"; },
    rows => { rows[0].metadata.namespace = "foreign"; },
    rows => { rows[2].metadata.namespace = "foreign"; },
    rows => { rows[1].metadata.ownerReferences[0].uid = "foreign-owner"; },
    rows => { rows[2].metadata.ownerReferences[0].controller = false; },
    rows => { rows[2].status.containerStatuses[0].imageID = "sha256:" + "b".repeat(64); },
    rows => { rows[2].metadata.deletionTimestamp = "2026-01-01T00:00:00Z"; },
    rows => { rows[0].spec.template.spec.containers[0].image = "registry.local.kodex/kodex/image-admission@sha256:" + "b".repeat(64); },
  ]) {
    const { objects } = fixture(); mutate(objects);
    assert.throws(() => retainedServingEvidence("image-admission", objects));
  }
  const { objects } = fixture(), extra = structuredClone(objects[0]);
  extra.metadata.uid = "second-deployment"; extra.metadata.name = "kodex-image-registry-evidence";
  extra.spec.template.spec.containers[0].image = "registry.local.kodex/kodex/image-admission@sha256:" + "c".repeat(64);
  assert.throws(() => retainedServingEvidence("image-admission", [...objects, extra]), /DIGEST_AMBIGUOUS/);
  assert.throws(() => retainedServingEvidence("control-plane", objects), /COMPONENT_INVALID/);
  assert.throws(() => retainedPublicationImage({ component: "control-plane", digest: "sha256:" + "a".repeat(64), deployments: [1], serving: [1] }));
});
