import { test } from "node:test";
import assert from "node:assert/strict";
import { fingerprint } from "./scoped-release.mjs";
import { policyBase, policyDigest, preparePolicy, requireIdle, planDeployment, planBinding, planGatewayMaintenance } from "./runner-policy-model.mjs";
import { samePlan } from "./runner-policy-transition.mjs";
import { mkdtempSync, writeFileSync, readFileSync, mkdirSync, rmSync } from "node:fs";
import { execFileSync, spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const oldDigest = `sha256:${"1".repeat(64)}`, newDigest = `sha256:${"2".repeat(64)}`;
const readerImage = `registry.local.kodex/kodex/image-admission@sha256:${"3".repeat(64)}`;
function metadata(name) { return { name, namespace: "kodex-system", uid: "11111111-1111-4111-8111-111111111111", resourceVersion: "19", generation: 2,
  labels: { "app.kubernetes.io/part-of": "kodex", "kodex.dev/local-profile": "hot-reload", "kodex.dev/owner-intent": "true" } }; }
function fixtures() {
  const data = { policyRevision: "4", trustedRoleBaseDigest: oldDigest, trustedRoleBaseRepository: "registry/kodex/agent-runner",
    orchestrationRevision: "a".repeat(40), nodeReadbackImage: `pull/kodex/agent-runner@${oldDigest}`, roleRuntimeContractRevision: "2", roleRuntimeContractSHA256: "e".repeat(64) };
  data.policySHA256 = policyDigest(data);
  const policy = { apiVersion: "v1", kind: "ConfigMap", metadata: metadata(policyBase), immutable: true, data };
  const parameters = { apiVersion: "supplychain.kodex.dev/v1alpha1", kind: "ImageAdmissionPolicyParameters", metadata: metadata(policyBase), spec: structuredClone(data) };
  const catalog = { apiVersion: "v1", kind: "ConfigMap", metadata: metadata("kodex-role-environments"), data: { "catalog.json": JSON.stringify({ schemaVersion: 1,
    context: { contextRef: "unchanged-exact-context" }, environments: [
      { key: "standard", available: true, baseImageReference: data.trustedRoleBaseRepository, baseImageDigest: oldDigest },
      { key: "documents", available: false, baseImageDigest: `sha256:${"0".repeat(64)}` },
    ] }) } };
  return { policy, parameters, catalog };
}
function prepared() { const f = fixtures(); return preparePolicy(f.policy, f.parameters, f.catalog, newDigest); }
function deployment(name) {
  const f = fixtures();
  const env = name === "control-plane" ? [
    { name: "CONTROL_PLANE_IMAGE_POLICY_REVISION", value: "4" }, { name: "CONTROL_PLANE_IMAGE_POLICY_SHA256", value: f.policy.data.policySHA256 },
    { name: "CONTROL_PLANE_TRUSTED_ROLE_BASE_DIGEST", value: oldDigest }, { name: "CONTROL_PLANE_DEFAULT_ROLE_IMAGE_REFERENCE", value: f.policy.data.nodeReadbackImage },
  ] : name === "image-admission-controller" ? [{ name: "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP", value: policyBase }] :
    name === "role-image-builder" ? ["REPOSITORY", "DIGEST"].map((suffix) => ({ name: `ROLE_IMAGE_BUILDER_TRUSTED_ROLE_BASE_${suffix}`,
      valueFrom: { fieldRef: { fieldPath: `metadata.annotations['kodex.dev/trusted-role-base-${suffix.toLowerCase()}']` } } })) : [];
  return { apiVersion: "apps/v1", kind: "Deployment", metadata: metadata(name), spec: { replicas: 1, strategy: { type: "Recreate" }, template: {
    metadata: { annotations: { "kodex.dev/trusted-role-base-repository": f.policy.data.trustedRoleBaseRepository, "kodex.dev/trusted-role-base-digest": oldDigest } },
    spec: { containers: [{ name, image: "old-exact-image", env }], initContainers: [{ name: "native-grant-agent", image: "old-security-image" }],
      volumes: [{ name: "role-environments", configMap: { name: "kodex-role-environments" } }, { name: "private", secret: { secretName: "unchanged" } }] },
  } }, status: { observedGeneration: 2, replicas: 1, updatedReplicas: 1, readyReplicas: 1, availableReplicas: 1 } };
}

test("new immutable policy preserves helper, contract, other catalog entries and predecessor", () => {
  const f = fixtures(), before = structuredClone(f), bundle = preparePolicy(f.policy, f.parameters, f.catalog, newDigest);
  assert.deepEqual(f, before);
  assert.equal(bundle.resources[0].data.policyRevision, "5");
  assert.equal(bundle.resources[0].metadata.name, `${policyBase}-${bundle.resources[0].data.policySHA256.slice(0, 32)}`);
  assert.equal(policyDigest(bundle.resources[0].data), bundle.resources[0].data.policySHA256);
  assert.deepEqual(bundle.resources[1].spec, bundle.resources[0].data);
  for (const key of ["nodeReadbackImage", "roleRuntimeContractRevision", "roleRuntimeContractSHA256", "orchestrationRevision"]) assert.equal(bundle.resources[0].data[key], f.policy.data[key]);
  const catalog = JSON.parse(bundle.resources[2].data["catalog.json"]), old = JSON.parse(f.catalog.data["catalog.json"]);
  assert.deepEqual(catalog.context, old.context); assert.deepEqual(catalog.environments[1], old.environments[1]);
  assert.equal(catalog.environments[0].baseImageDigest, newDigest); assert.equal(bundle.resources[2].immutable, true);
});

test("policy corruption, unexpected owner, same base and parameter drift fail closed", () => {
  for (const mutate of [
    (f) => { f.policy.immutable = false; }, (f) => { f.policy.metadata.namespace = "foreign"; },
    (f) => { f.policy.metadata.labels["kodex.dev/owner-intent"] = "false"; },
    (f) => { f.parameters.spec.policyRevision = "7"; }, (f) => { f.policy.data.trustedRoleBaseDigest = newDigest; },
    (f) => { f.catalog.data["extra"] = "unexpected"; },
  ]) { const f = fixtures(); mutate(f); assert.throws(() => preparePolicy(f.policy, f.parameters, f.catalog, newDigest)); }
  const f = fixtures(); assert.throws(() => preparePolicy(f.policy, f.parameters, f.catalog, oldDigest));
  assert.throws(() => policyDigest({ foreign: "non-ascii: Я" }));
});

test("consumer updates preserve old default, sidecars, references and exact CAS", () => {
  const bundle = prepared();
  for (const name of ["control-plane", "role-image-builder"]) {
    const d = deployment(name), original = structuredClone(d), change = planDeployment(d, bundle, name, readerImage);
    assert.deepEqual(d, original); assert.deepEqual(change.next.template.spec.initContainers, d.spec.template.spec.initContainers);
    assert.deepEqual(change.next.template.spec.volumes[1], d.spec.template.spec.volumes[1]);
    assert.equal(change.next.template.spec.containers[0].image, d.spec.template.spec.containers[0].image);
    assert.deepEqual(change.patch.slice(0, 2).map((entry) => entry.path), ["/metadata/uid", "/metadata/resourceVersion"]);
    if (name === "control-plane") assert.equal(change.next.template.spec.containers[0].env.find((entry) => entry.name === "CONTROL_PLANE_DEFAULT_ROLE_IMAGE_REFERENCE").value, fixtures().policy.data.nodeReadbackImage);
  }
  const bad = deployment("control-plane"); bad.status.readyReplicas = 0;
  assert.throws(() => planDeployment(bad, bundle, "control-plane", readerImage), /HEALTHY/);
});

test("reader pause and policy switch cannot silently resume old binary or old configuration", () => {
  const bundle = prepared(), d = deployment("image-admission-controller");
  assert.throws(() => planDeployment(d, bundle, "controller", readerImage));
  d.spec = planDeployment(d, bundle, "reader", readerImage).next;
  assert.throws(() => planDeployment(d, bundle, "reader", readerImage));
  assert.throws(() => planDeployment(d, bundle, "resume", readerImage));
  d.spec = planDeployment(d, bundle, "controller", readerImage).next;
  d.spec = planDeployment(d, bundle, "resume", readerImage).next;
  assert.equal(d.spec.template.spec.containers[0].env.find((entry) => entry.name === "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS").value, "false");
});

test("binding preserves Deny and adds only exact revision read names, including partial readback", () => {
  const bundle = prepared();
  const binding = { kind: "ValidatingAdmissionPolicyBinding", metadata: metadata("kodex-image-admission-controller-jobs"), spec: {
    policyName: "kodex-image-admission-controller-jobs", paramRef: { name: policyBase, namespace: "kodex-system", parameterNotFoundAction: "Deny" }, validationActions: ["Deny"] } };
  const role = { kind: "Role", metadata: metadata("image-admission-controller"), rules: ["configmaps", "imageadmissionpolicyparameters"].map((resource) => ({ resources: [resource], verbs: ["get"], resourceNames: [policyBase] })) };
  const result = planBinding(binding, role, bundle);
  assert.equal(result.spec.paramRef.parameterNotFoundAction, "Deny");
  assert.deepEqual(result.rules[0].resourceNames, [policyBase, bundle.resources[0].metadata.name]);
  role.rules = result.rules;
  assert.deepEqual(planBinding(binding, role, bundle), result);
  binding.spec.paramRef.parameterNotFoundAction = "Allow";
  assert.throws(() => planBinding(binding, role, bundle));
});

test("fresh idle preflight rejects pending work, stale snapshot and absent counts", () => {
  const at = "2026-09-08T10:00:00Z", now = Date.parse(at);
  const state = { at, openBuilds: 0, pendingAdmissions: 0, pendingPromotions: 0, activeRuntimeRuns: 0, claimedRuntimeLeases: 0, promotedArtifactCount: 4, promotedPinsSHA256: "a".repeat(64) };
  requireIdle(state, now);
  for (const key of ["openBuilds", "pendingAdmissions", "pendingPromotions", "activeRuntimeRuns", "claimedRuntimeLeases"]) {
    assert.throws(() => requireIdle({ ...state, [key]: 1 }, now)); assert.throws(() => requireIdle({ ...state, [key]: undefined }, now));
  }
  assert.throws(() => requireIdle(state, now + 30_001));
});

test("maintenance records exact replica count, preserves template and rejects another operation", () => {
  const d = deployment("control-api-gateway"), sha = fingerprint(prepared());
  d.spec.replicas = 2; d.status.availableReplicas = 2;
  const stopped = planGatewayMaintenance(d, sha);
  assert.equal(stopped.spec.replicas, 0); assert.deepEqual(stopped.spec.template, d.spec.template);
  d.spec = stopped.spec; d.metadata.annotations = stopped.annotations; d.status.replicas = 0;
  assert.throws(() => planGatewayMaintenance(d, sha)); assert.throws(() => planGatewayMaintenance(d, "f".repeat(64), true));
  const resumed = planGatewayMaintenance(d, sha, true); assert.equal(resumed.spec.replicas, 2); assert.deepEqual(resumed.annotations, {});
});

test("saved plan cannot survive another resourceVersion, operation, cluster or pinned history", () => {
  const plan = { version: 1, context: "default", clusterUID: "cluster", namespaceUID: "namespace", bundleSHA256: "a".repeat(64), phase: "resources", readerImage,
    operations: [{ type: "patch", resourceVersion: "7" }], guards: { promotedPinsSHA256: "b".repeat(64) } };
  samePlan(plan, structuredClone(plan));
  for (const mutate of [(p) => { p.clusterUID = "other"; }, (p) => { p.operations[0].resourceVersion = "8"; }, (p) => { p.guards.promotedPinsSHA256 = "c".repeat(64); }]) {
    const changed = structuredClone(plan); mutate(changed); assert.throws(() => samePlan(plan, changed));
  }
});

test("public CLI completes ordered maintenance and preserves predecessors; lost patch ACK is UNKNOWN without retry", () => {
  const directory = mkdtempSync(join(tmpdir(), "kodex-policy-cli-"));
  try {
    const bin = join(directory, "bin"); mkdirSync(bin);
    const statePath = join(directory, "state.json"), callsPath = join(directory, "calls.jsonl");
    const f = fixtures(), objects = [f.policy, f.parameters, f.catalog,
      ...["control-plane", "role-image-builder", "image-admission-controller", "control-api-gateway"].map(deployment),
      ...["kube-system", "kodex-system"].map((name) => ({ kind: "Namespace", metadata: metadata(name) })),
      { kind: "ValidatingAdmissionPolicy", metadata: metadata("kodex-image-admission-controller-jobs"), spec: { failurePolicy: "Fail" } },
      { kind: "ValidatingAdmissionPolicyBinding", metadata: metadata("kodex-image-admission-controller-jobs"), spec: {
        policyName: "kodex-image-admission-controller-jobs", paramRef: { name: policyBase, namespace: "kodex-system", parameterNotFoundAction: "Deny" }, validationActions: ["Deny"] } },
      { kind: "Role", metadata: metadata("image-admission-controller"), rules: ["configmaps", "imageadmissionpolicyparameters"].map((resource) => ({ resources: [resource], verbs: ["get"], resourceNames: [policyBase] })) },
    ];
    writeFileSync(statePath, JSON.stringify(objects));
    writeFileSync(join(bin, "kubectl"), `#!/usr/bin/env node
const fs=require('node:fs');
const args=process.argv.slice(2); args.splice(0,4);
const state=JSON.parse(fs.readFileSync(process.env.FIXTURE_STATE));
const key=(x)=>x.toLowerCase();
const find=(kind,name)=>state.find(x=>key(x.kind)===key(kind)&&x.metadata.name===name);
const output=x=>process.stdout.write(JSON.stringify(x));
if(args[0]==='get') {
  if(['jobs','persistentvolumeclaims'].includes(args[1])) {
    if(args[args.indexOf('-l')+1]!=='kodex.dev/image-admission-orchestrated=true') process.exit(12);
    output({items:[]});
  } else { const value=find(args[1],args[2]); if(value)output(value); else if(!args.includes('--ignore-not-found'))process.exit(3); }
} else if(args[0]==='exec') {
  fs.readFileSync(0); output({at:new Date().toISOString(),openBuilds:0,pendingAdmissions:0,pendingPromotions:0,activeRuntimeRuns:0,claimedRuntimeLeases:0,promotedArtifactCount:4,promotedPinsSHA256:'a'.repeat(64)});
} else if(args[0]==='create') {
  const value=JSON.parse(fs.readFileSync(0)); if(find(value.kind,value.metadata.name))process.exit(4);
  value.metadata.uid='22222222-2222-4222-8222-222222222222';value.metadata.resourceVersion='1';state.push(value);
  fs.writeFileSync(process.env.FIXTURE_STATE,JSON.stringify(state));
} else if(args[0]==='patch') {
  const value=find(args[1],args[2]); if(!value)process.exit(5);
  for(const op of JSON.parse(args[args.indexOf('-p')+1])) {
    const parts=op.path.split('/').slice(1), last=parts.pop();let target=value;
    for(const p of parts)target=target[p];
    if(op.op==='test'){if(JSON.stringify(target[last])!==JSON.stringify(op.value))process.exit(6);}else target[last]=op.value;
  }
  value.metadata.resourceVersion=String(Number(value.metadata.resourceVersion)+1);
  if(value.kind==='Deployment'){value.metadata.generation++;value.status={observedGeneration:value.metadata.generation,replicas:value.spec.replicas,updatedReplicas:value.spec.replicas,readyReplicas:value.spec.replicas,availableReplicas:value.spec.replicas};}
  fs.writeFileSync(process.env.FIXTURE_STATE,JSON.stringify(state));
  fs.appendFileSync(process.env.FIXTURE_CALLS,JSON.stringify({kind:value.kind,name:value.metadata.name})+'\\n');
  if(process.env.FIXTURE_LOST_ACK==='true')process.exit(7);
} else if(args[0]!=='rollout')process.exit(8);
`, { mode: 0o700 });
    const cli = fileURLToPath(new URL("./runner-policy-transition.mjs", import.meta.url));
    const environment = { ...process.env, PATH: `${bin}:${process.env.PATH}`, FIXTURE_STATE: statePath, FIXTURE_CALLS: callsPath };
    const run = (args) => execFileSync(process.execPath, [cli, ...args], { env: environment, stdio: "pipe" });
    const bundle = join(directory, "bundle.json");
    run(["prepare", "--context", "default", "--runner-digest", newDigest, "--output", bundle]);
    const common = ["--context", "default", "--bundle", bundle, "--reader-image", readerImage];
    const phases = ["maintenance", "reader", "resources", "binding", "control-plane", "role-image-builder", "controller", "resume", "open"];
    for (const phase of phases) {
      const plan = join(directory, `${phase}.json`), evidence = join(directory, `${phase}.jsonl`);
      run(["plan", ...common, "--phase", phase, "--output", plan]);
      run(["apply", ...common, "--plan", plan, "--evidence", evidence, "--confirm", "APPLY-STAGING-RUNNER-POLICY"]);
      assert.equal(JSON.parse(readFileSync(evidence, "utf8").trim().split("\n").at(-1)).status, "PASS");
    }
    const final = JSON.parse(readFileSync(statePath));
    assert.deepEqual(final.find((item) => item.kind === "ConfigMap" && item.metadata.name === policyBase), f.policy);
    assert.equal(final.find((item) => item.kind === "Deployment" && item.metadata.name === "control-api-gateway").spec.replicas, 1);
    assert.deepEqual(final.find((item) => item.kind === "Role").rules[0].resourceNames, [JSON.parse(readFileSync(bundle)).resources[0].metadata.name]);
    // Новый независимый fixture моделирует принятую сервером запись с потерянным ACK.
    writeFileSync(statePath, JSON.stringify(objects));
    const plan = join(directory, "lost.json"), evidence = join(directory, "lost.jsonl");
    run(["plan", ...common, "--phase", "maintenance", "--output", plan]);
    const before = readFileSync(callsPath, "utf8").trim().split("\n").length;
    const result = spawnSync(process.execPath, [cli, "apply", ...common, "--plan", plan, "--evidence", evidence, "--confirm", "APPLY-STAGING-RUNNER-POLICY"], { env: { ...environment, FIXTURE_LOST_ACK: "true" }, encoding: "utf8" });
    assert.equal(result.status, 1);
    assert.equal(JSON.parse(readFileSync(evidence, "utf8").trim().split("\n").at(-1)).status, "UNKNOWN");
    assert.equal(readFileSync(callsPath, "utf8").trim().split("\n").length, before + 1);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
