import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { validateStableReceipt } from "./stable-image-publication.mjs";

const rows = execFileSync("yq", ["eval-all", "-N", "-o=json", "-I=0", ".",
  fileURLToPath(new URL("../../deploy/k8s/base/image-supply-chain/image-admission-controller.yaml", import.meta.url))],
{ encoding: "utf8" }).trim().split("\n").map(JSON.parse);

test("admission replica profile binds Pod UID and preserves one available instance", () => {
  const deployment = rows.find(r => r.kind === "Deployment");
  assert.equal(deployment.spec.replicas, 2);
  assert.deepEqual(deployment.spec.strategy, { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } });
  assert.equal(deployment.spec.template.spec.automountServiceAccountToken, false);
  const env = deployment.spec.template.spec.containers.find(c => c.name === deployment.metadata.name).env;
  assert.deepEqual(env.find(e => e.name === "POD_UID"), { name: "POD_UID", valueFrom: { fieldRef: { fieldPath: "metadata.uid" } } });
  const pdb = rows.find(r => r.kind === "PodDisruptionBudget");
  assert.equal(pdb.spec.minAvailable, 1);
  assert.deepEqual(pdb.spec.selector.matchLabels, deployment.spec.selector.matchLabels);
});

test("runtime can only get/update the precreated exact leader Lease", () => {
  const lease = rows.find(r => r.kind === "Lease");
  assert.equal(lease.apiVersion, "coordination.k8s.io/v1");
  assert.equal(lease.metadata.name, "image-admission-controller-leader");
  assert.equal(lease.metadata.labels["app.kubernetes.io/name"], "image-admission-controller");
  assert.deepEqual(lease.spec, {});
  const grants = rows.find(r => r.kind === "Role").rules.filter(r => r.apiGroups.includes("coordination.k8s.io"));
  assert.deepEqual(grants, [{ apiGroups: ["coordination.k8s.io"], resources: ["leases"],
    resourceNames: [lease.metadata.name], verbs: ["get", "update"] }]);
});

test("compiled orchestrator has its own bounded recipe without scanner or writer binaries", () => {
  const root = new URL("../../", import.meta.url);
  const recipe = JSON.parse(readFileSync(new URL("tools/release/images.json", root), "utf8")).images
    .find(r => r.component === "image-admission-controller");
  assert.equal(recipe.target, "admission-controller-runtime");
  const stage = readFileSync(new URL(recipe.dockerfile, root), "utf8").split(" AS admission-controller-runtime\n")[1];
  assert.ok(stage);
  assert.equal(stage.includes("FROM "), false);
  assert.equal(stage.match(/^COPY /gm)?.length, 2);
  assert.match(stage, /apk add --no-cache bash=[0-9.]+-r[0-9]+ jq=[0-9.]+-r[0-9]+/);
  assert.match(stage, /USER 10001:10001/);
  assert.match(stage, /ENTRYPOINT \["\/usr\/local\/bin\/image-admission-controller"\]/);
  assert.equal(validateStableReceipt({ schema: "kodex.stable-local-build/v1", component: recipe.component,
    sourceSHA: "a".repeat(40), contextSHA256: "b".repeat(64), archiveSHA256: "c".repeat(64), imageDigest: "sha256:" + "d".repeat(64),
    admission: "NOT_RUN", publication: "NOT_RUN", nodePull: "NOT_RUN" }).component, recipe.component);
});
