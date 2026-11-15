import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import { fingerprint, kubectlInvocation, planTarget, releaseCLI } from "./scoped-release.mjs";

const component = "staff-control-center";
const image = hex => `images.example.invalid/kodex/${component}@sha256:${hex.repeat(64)}`;
const id = "11111111-1111-4111-8111-111111111111";
const namespaceUID = "22222222-2222-4222-8222-222222222222";
const requested = { name: component, image: image("b"), build: { sourceSHA: "c".repeat(40), provenanceSHA256: "d".repeat(64) } };

function fixture() {
  return { apiVersion: "apps/v1", kind: "Deployment",
    metadata: { name: component, namespace: "kodex-system", uid: id, resourceVersion: "11", generation: 1,
      labels: { "app.kubernetes.io/part-of": "kodex", "kodex.dev/environment": "production", "kodex.dev/profile": "web-only" } },
    spec: { replicas: 2, strategy: { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } },
      template: { metadata: { annotations: { "kodex.dev/component-source-sha": "a".repeat(40),
        "kodex.dev/component-upgrade-sha256": "b".repeat(64), "kodex.dev/component-base-lock-sha256": "f".repeat(64), "kodex.dev/runtime-config-revision": "e".repeat(64) } },
      spec: { serviceAccountName: component, containers: [{ name: component, image: image("a"), ports: [{ name: "https", containerPort: 8443 }] }],
        volumes: [{ name: "runtime-config", configMap: { name: "synthetic-runtime" } }] } } },
    status: { observedGeneration: 1, replicas: 2, availableReplicas: 2, readyReplicas: 2, updatedReplicas: 2 } };
}

test("portable PWA requires explicit mode and preserves configuration, base lock and rollback provenance", () => {
  const current = fixture();
  current.spec.template.metadata.annotations["synthetic-private"] = "PRIVATE_ANNOTATION_SENTINEL";
  assert.throws(() => planTarget(current, requested, id), /STAGING_PROFILE_REQUIRED/);
  const before = structuredClone(current);
  const planned = planTarget(current, requested, id, undefined, "portable-pwa");
  assert.deepEqual(current, before);
  const expected = structuredClone(current.spec);
  expected.template.spec.containers[0].image = requested.image;
  Object.assign(expected.template.metadata.annotations, { "kodex.dev/component-source-sha": requested.build.sourceSHA,
    "kodex.dev/component-upgrade-sha256": requested.build.provenanceSHA256, "kodex.dev/application-release": id });
  assert.equal(planned.afterSpecSHA256, fingerprint(expected));
  assert.ok(!JSON.stringify(planned).includes("PRIVATE_ANNOTATION_SENTINEL"));
  current.spec = expected;
  const rollback = planTarget(current, planned.rollback, namespaceUID, undefined, "portable-pwa");
  assert.equal(rollback.image, image("a"));
  assert.equal(rollback.patch.find(row => row.path.endsWith("component-source-sha")).value, "a".repeat(40));
  assert.equal(rollback.patch.find(row => row.path.endsWith("component-upgrade-sha256")).value, "b".repeat(64));
  assert.ok(!rollback.patch.some(row => row.path.endsWith("component-base-lock-sha256")));
});

test("portable PWA cannot widen target, executable, mounts, registry or provenance", () => {
  for (const mutate of [
    d => { d.metadata.name = "control-plane"; },
    d => { delete d.metadata.labels["kodex.dev/profile"]; },
    d => { d.spec.template.spec.containers.push({ name: "extra", image: image("a") }); },
    d => { d.spec.template.spec.initContainers = [{ name: "extra", image: image("a") }]; },
    d => { d.spec.template.spec.containers[0].command = ["/bin/sh"]; },
    d => { d.spec.template.spec.containers[0].args = ["custom"]; },
    d => { d.spec.template.spec.volumes.push({ name: "host", hostPath: { path: "/synthetic" } }); },
    d => { d.spec.template.spec.hostNetwork = true; },
    d => { d.spec.template.spec.hostPID = true; },
    d => { delete d.spec.template.metadata.annotations["kodex.dev/component-source-sha"]; },
    d => { d.spec.template.metadata.annotations["kodex.dev/component-source-sha"] = "0".repeat(40); },
    d => { delete d.spec.template.metadata.annotations["kodex.dev/component-base-lock-sha256"]; },
    d => { d.spec.template.metadata.annotations["kodex.dev/component-image-profile-sha256"] = "a".repeat(64); },
  ]) {
    const current = fixture(); mutate(current);
    assert.throws(() => planTarget(current, requested, id, undefined, "portable-pwa"));
  }
  assert.throws(() => planTarget(fixture(), { ...requested, image: requested.image.replace("images.example.invalid", "other.example.invalid") }, id, undefined, "portable-pwa"));
  assert.throws(() => planTarget(fixture(), { ...requested, source: { path: "/synthetic", revision: "a".repeat(40) } }, id, undefined, "portable-pwa"));
  assert.throws(() => planTarget(fixture(), { ...requested, build: undefined }, id, undefined, "portable-pwa"));
});

test("exact kubeconfig is passed to every command and cannot mix with sudo or relative paths", () => {
  assert.deepEqual(kubectlInvocation("k3d-synthetic", ["get", "nodes"], false, "/synthetic/config").arguments,
    ["--kubeconfig", "/synthetic/config", "--context", "k3d-synthetic", "--request-timeout=30s", "get", "nodes"]);
  for (const path of ["relative", "/synthetic/../config", "/synthetic\nconfig", "/synthetic\0config"])
    assert.throws(() => kubectlInvocation("k3d-synthetic", [], false, path), /EXACT_KUBECONFIG_REQUIRED/);
  assert.throws(() => kubectlInvocation("k3d-synthetic", [], true, "/synthetic/config"), /EXACT_KUBECONFIG_REQUIRED/);
});

test("portable CLI binds namespace and config, keeps original journal after lost ACK and never repeats PATCH", async () => {
  const directory = await mkdtemp(join(tmpdir(), "kodex-pwa-contract-"));
  const previousExit = process.exitCode;
  try {
    const manifest = join(directory, "manifest.json"), plan = join(directory, "plan.json"), journal = join(directory, "journal.jsonl");
    await writeFile(manifest, JSON.stringify({ version: 1, targets: [requested] }), { mode: 0o600 });
    const deployment = fixture();
    let patches = 0, wrongNamespace = false;
    const source = () => ({ revision: "e".repeat(40) });
    const command = async (file, args) => {
      assert.equal(file, "kubectl");
      assert.deepEqual(args.slice(0, 4), ["--kubeconfig", "/synthetic/config", "--context", "k3d-synthetic"]);
      if (args.includes("namespace")) return { stdout: JSON.stringify({ metadata: { uid: args.includes("kube-system") ? id : wrongNamespace ? id : namespaceUID } }) };
      if (args.includes("patch")) { patches++; throw Error("synthetic lost acknowledgement"); }
      assert.ok(args.includes("get"));
      return { stdout: JSON.stringify(deployment) };
    };
    const flags = ["--profile", "portable-pwa", "--context", "k3d-synthetic", "--kubeconfig", "/synthetic/config"];
    await releaseCLI(["plan", ...flags, "--manifest", manifest, "--output", plan], command, source);
    const value = JSON.parse(await readFile(plan, "utf8"));
    assert.equal(value.namespaceUID, namespaceUID);
    assert.equal(value.kubeconfig, "/synthetic/config");
    const apply = ["apply", ...flags, "--plan", plan, "--evidence", journal, "--confirm", "APPLY-STAGING-APPLICATIONS"];
    wrongNamespace = true;
    await assert.rejects(releaseCLI(apply.slice(), command, source), /RELEASE_PLAN_IDENTITY_MISMATCH/);
    assert.equal(patches, 0);
    wrongNamespace = false;
    await assert.rejects(releaseCLI(apply.slice(), command, () => ({ revision: "f".repeat(40) })), /RELEASE_PLAN_IDENTITY_MISMATCH/);
    assert.equal(patches, 0);
    await releaseCLI(apply.slice(), command, source);
    assert.equal(patches, 1);
    const before = await readFile(journal, "utf8");
    assert.match(before, /PATCH_ATTEMPT/);
    assert.match(before, /UNKNOWN/);
    await assert.rejects(releaseCLI(apply.slice(), command, source), /EEXIST/);
    assert.equal(patches, 1);
    assert.equal(await readFile(journal, "utf8"), before);
  } finally { process.exitCode = previousExit; await rm(directory, { recursive: true, force: true }); }
});

test("portable CLI successful rollout uses real local kubectl patch and exact final spec", async () => {
  const directory = await mkdtemp(join(tmpdir(), "kodex-pwa-patch-"));
  const previousExit = process.exitCode;
  try {
    const manifest = join(directory, "manifest.json"), plan = join(directory, "plan.json"), journal = join(directory, "journal.jsonl");
    const localConfig = join(directory, "synthetic-config.json");
    await writeFile(localConfig, JSON.stringify({ apiVersion: "v1", kind: "Config", clusters: [{ name: "synthetic", cluster: { server: "https://kubernetes.example.invalid" } }],
      contexts: [{ name: "synthetic", context: { cluster: "synthetic" } }], "current-context": "synthetic", users: [] }), { mode: 0o600 });
    await writeFile(manifest, JSON.stringify({ version: 1, targets: [requested] }), { mode: 0o600 });
    let current = fixture(), patches = 0;
    const original = structuredClone(current.spec.template.spec.volumes);
    const command = async (file, args) => {
      assert.equal(file, "kubectl");
      assert.deepEqual(args.slice(0, 4), ["--kubeconfig", "/synthetic/config", "--context", "k3d-synthetic"]);
      if (args.includes("namespace")) return { stdout: JSON.stringify({ metadata: { uid: args.includes("kube-system") ? id : namespaceUID } }) };
      if (args.includes("patch")) {
        patches++;
        const input = join(directory, "deployment.json");
        await writeFile(input, JSON.stringify(current), { mode: 0o600 });
        // Только --local: настоящий kubectl проверяет JSON Patch на synthetic файле, без кластера.
        current = JSON.parse(execFileSync("kubectl", ["--kubeconfig", localConfig, "--context", "synthetic", "patch", "--local=true", "-f", input,
          "--type=json", "-p", args[args.indexOf("-p") + 1], "-o", "json"],
        { encoding: "utf8", env: { PATH: process.env.PATH, HOME: directory }, timeout: 10000 }));
        current.metadata.generation++;
        current.status.observedGeneration = current.metadata.generation;
        return { stdout: "deployment.apps/staff-control-center" };
      }
      if (args.includes("rollout")) return { stdout: "success" };
      assert.ok(args.includes("get"));
      return { stdout: JSON.stringify(current) };
    };
    const flags = ["--profile", "portable-pwa", "--context", "k3d-synthetic", "--kubeconfig", "/synthetic/config"];
    const source = () => ({ revision: "e".repeat(40) });
    await releaseCLI(["plan", ...flags, "--manifest", manifest, "--output", plan], command, source);
    await releaseCLI(["apply", ...flags, "--plan", plan, "--evidence", journal, "--confirm", "APPLY-STAGING-APPLICATIONS"], command, source);
    assert.equal(patches, 1);
    assert.equal(current.spec.template.spec.containers[0].image, requested.image);
    assert.deepEqual(current.spec.template.spec.volumes, original);
    const result = (await readFile(journal, "utf8")).trim().split("\n").map(JSON.parse).at(-1);
    assert.equal(result.status, "PASS");
    assert.equal(result.results[0].image, requested.image);
  } finally { process.exitCode = previousExit; await rm(directory, { recursive: true, force: true }); }
});
