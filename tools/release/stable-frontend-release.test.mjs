import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { releaseFrontend } from "./stable-frontend-release.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";

const base = fileURLToPath(new URL("../../deploy/k8s/base/staff-control-center/", import.meta.url));
const yaml = path => JSON.parse(execFileSync("yq", ["-o=json", "-I=0", path], { encoding: "utf8" }));
const selected = (path, name) => JSON.parse(execFileSync("yq", ["eval-all", "-o=json", "-I=0", `select(.metadata.name == "${name}")`, path], { encoding: "utf8" }));
const release = { component: "staff-control-center", sourceSHA: "a".repeat(40), provenanceSHA256: "b".repeat(64), image: "registry.invalid/kodex/staff-control-center@sha256:" + "c".repeat(64) };

function fixture() {
  const directory = mkdtempSync(join(tmpdir(), "kodex-frontend-release-fixture-"));
  execFileSync("openssl", ["req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:prime256v1", "-nodes", "-subj", "/CN=fixture",
    "-days", "1", "-addext", "subjectAltName=DNS:control-api.kodex.local,DNS:staff-control-center.kodex-system.svc.cluster.local",
    "-keyout", join(directory, "fixture.key"), "-out", join(directory, "fixture.crt")], { stdio: ["ignore", "ignore", "ignore"] });
  const certificate = readFileSync(join(directory, "fixture.crt")).toString("base64");
  const resources = new Map(), trace = [], flags = {};
  const put = value => { const r = structuredClone(value); r.metadata.namespace = "kodex-system"; r.metadata.uid ??= r.kind + "-" + r.metadata.name;
    r.metadata.resourceVersion ??= "1"; resources.set(`${r.kind}/${r.metadata.name}`, r); return r; };
  const deployment = put(yaml(base + "deployment.yaml")); deployment.metadata.generation = 1; deployment.spec.replicas = 1;
  deployment.spec.template.metadata.annotations.untouched = "PRIVATE_ANNOTATION_SENTINEL";
  const pod = deployment.spec.template.spec, app = pod.containers[0];
  app.image = "source-image"; app.command = ["/workspace/tools/dev/run-frontend.sh"]; app.args = []; app.workingDir = "/workspace/services/staff/control-center";
  app.env = [{ name: "KODEX_DEV_NODE_IMAGE", value: app.image }, { name: "KODEX_DEV_CACHE_DIR", value: "/tmp/vite-cache" },
    { name: "KODEX_DEV_PUBLIC_HOST", value: "platform.example.test" }, { name: "KODEX_DEV_API_TARGET", value: "https://control-api-gateway.kodex-system.svc:8443" }];
  const service = put(yaml(base + "service.yaml")); delete service.metadata.annotations;
  service.spec.ports = [{ name: "http", port: 8080, targetPort: "http", protocol: "TCP" }];
  for (const [file, api] of [["ingress.yaml", false], ["api-ingress.yaml", true], ["public-assets-ingress.yaml", false]]) {
    const ingress = put(yaml(base + file)); ingress.spec.rules[0].host = "platform.example.test"; ingress.spec.tls[0].hosts = ["platform.example.test"];
    for (const path of ingress.spec.rules[0].http.paths) path.backend.service = api ? { name: "control-api-gateway", port: { name: "https" } } : { name: "staff-control-center", port: { name: "http" } };
  }
  const policy = put(selected(base + "networkpolicy.yaml", "staff-control-center-ingress"));
  policy.spec.ingress[0] = { from: [{ namespaceSelector: { matchLabels: { "kubernetes.io/metadata.name": "kube-system" } }, podSelector: { matchLabels: { "app.kubernetes.io/name": "traefik" } } }], ports: [{ port: 8080, protocol: "TCP" }] };
  put(selected(base + "networkpolicy.yaml", "staff-control-center-exact-egress"));
  put({ kind: "NetworkPolicy", metadata: { name: "control-api-gateway-exact-runtime-paths" }, spec: { fixture: true } });
  const runtimeName = pod.volumes.find(v => v.name === "runtime-config").configMap.name;
  put({ kind: "ConfigMap", metadata: { name: runtimeName }, data: {
    "runtime-config.json": JSON.stringify({ revision: "d".repeat(64), environment: "development", apiBaseUrl: "/", realtimeUrl: "/api/v1", requestTimeoutMs: 15000,
      oidc: { authority: "https://sso.example.test/realms/kodex", clientId: "kodex-control-center", redirectUri: "/auth/callback", postLogoutRedirectUri: "/", scope: "openid kodex.owner" } }),
    "security-headers.conf": readFileSync(base + "security-headers.conf", "utf8").replaceAll("__KODEX_OIDC_ORIGIN__", "https://sso.example.test"),
    "public-assets.conf": readFileSync(base + "public-assets.conf", "utf8"), "readiness.json": "{}" } });
  const types = { deployment: "Deployment", service: "Service", ingress: "Ingress", networkpolicy: "NetworkPolicy", configmap: "ConfigMap", serverstransport: "ServersTransport" };
  const port = async (program, argv, input) => {
    trace.push({ program, argv });
    if (program === "git") return argv[0] === "rev-parse" ? "f".repeat(40) : "";
    if (program === "yq") return execFileSync("yq", argv, { encoding: "utf8" });
    if (program === "/bin/sh") argv = argv.slice(3);
    const get = argv.indexOf("get"), patch = argv.indexOf("patch");
    if (get >= 0) {
      const kind = argv[get + 1], name = argv[get + 2];
      if (kind === "namespace") return JSON.stringify({ metadata: { uid: name } });
      if (kind === "secret") return `secret-${name}|1|${certificate}|${certificate}`;
      if (kind === "replicasets,pods") {
        const d = resources.get("Deployment/staff-control-center"), count = d.spec.replicas;
        return JSON.stringify({ items: [{ kind: "ReplicaSet", metadata: { uid: "rs", ownerReferences: [{ kind: "Deployment", uid: d.metadata.uid, controller: true }] } },
          ...Array.from({ length: count }, (_, index) => ({ kind: "Pod", metadata: { uid: `pod-${index}`, ownerReferences: [{ kind: "ReplicaSet", uid: "rs", controller: true }] },
            spec: d.spec.template.spec, status: { conditions: [{ type: "Ready", status: "True" }], containerStatuses: [{ name: "staff-control-center", ready: true, imageID: d.spec.template.spec.containers[0].image }] } }))] });
      }
      const r = resources.get(`${types[kind]}/${name}`); if (!r && !argv.includes("--ignore-not-found")) throw Error("FIXTURE_NOT_FOUND");
      return r ? JSON.stringify(r) : "";
    }
    if (patch >= 0) {
      const key = `${types[argv[patch + 1]]}/${argv[patch + 2]}`, current = resources.get(key), result = structuredClone(current), body = JSON.parse(input);
      for (const op of body.filter(o => o.op === "test")) {
        const actual = op.path === "/spec" ? current.spec : current.metadata[op.path.split("/").at(-1)];
        assert.equal(fingerprint(actual), fingerprint(op.value));
      }
      result.spec = applyReverseSpec(result.spec, body.filter(o => o.path.startsWith("/spec") && o.op !== "test"));
      for (const op of body.filter(o => o.path.startsWith("/metadata") && o.op !== "test")) {
        if (op.path === "/metadata/annotations") result.metadata.annotations = op.value;
        else { const field = op.path.split("/").at(-1).replaceAll("~1", "/").replaceAll("~0", "~");
          if (op.op === "remove") delete result.metadata.annotations[field]; else result.metadata.annotations[field] = op.value; }
      }
      if (!argv.includes("--dry-run=server")) {
        result.metadata.resourceVersion = String(Number(result.metadata.resourceVersion) + 1); result.metadata.generation = (result.metadata.generation ?? 0) + 1;
        if (result.kind === "Deployment") result.status = { observedGeneration: result.metadata.generation, replicas: result.spec.replicas,
          readyReplicas: result.spec.replicas, availableReplicas: result.spec.replicas, updatedReplicas: result.spec.replicas };
        resources.set(key, result);
        if (flags.lostCompiled && result.kind === "Deployment" && !result.spec.template.spec.containers[0].command) throw Error("FIXTURE_LOST_RESPONSE");
      }
      return JSON.stringify(result);
    }
    if (argv.includes("create")) {
      const value = put(JSON.parse(input));
      if (flags.lostCreate) throw Error("FIXTURE_LOST_RESPONSE");
      return JSON.stringify(value);
    }
    if (argv.includes("rollout")) return "complete";
    if (argv.includes("delete")) {
      const url = argv.find(v => v.startsWith("--raw=")), part = url.split("/"), resourceName = part.at(-1), type = part.at(-2) === "configmaps" ? "ConfigMap" : "ServersTransport";
      const key = `${type}/${resourceName}`, current = resources.get(key), request = JSON.parse(input);
      assert.equal(current.metadata.uid, request.preconditions.uid); assert.equal(current.metadata.resourceVersion, request.preconditions.resourceVersion);
      resources.delete(key); return "deleted";
    }
    throw Error("FIXTURE_UNEXPECTED_OPERATION");
  };
  const releasePath = join(directory, "release.json"); writeFileSync(releasePath, JSON.stringify(release), { mode: 0o600 });
  return { directory, resources, trace, flags, port, releasePath, cleanup: () => rmSync(directory, { recursive: true, force: true }) };
}
async function planned(f) {
  const path = join(f.directory, "plan.json"); await releaseFrontend(["plan", "--context", "k3d-fixture", "--release", f.releasePath, "--output", path], f.port);
  assert.equal(readFileSync(path, "utf8").includes("PRIVATE_ANNOTATION_SENTINEL"), false);
  return { path, hash: fingerprint(JSON.parse(readFileSync(path, "utf8"))) };
}
const args = (f, p, mode, file) => [mode, "--context", "k3d-fixture", "--plan", p.path, "--output", join(f.directory, file),
  ...(mode === "apply" || mode === "rollback" ? ["--approved-plan-sha", p.hash] : []),
  ...(mode === "readback" || mode === "rollback" ? ["--journal", join(f.directory, "apply.jsonl")] : [])];

test("frontend orchestrator switches all protected routes and restores source without secret snapshot", async () => {
  const f = fixture(); try {
    const p = await planned(f); await releaseFrontend(args(f, p, "apply", "apply.jsonl"), f.port);
    await releaseFrontend(args(f, p, "readback", "readback.json"), f.port);
    await releaseFrontend(args(f, p, "rollback", "rollback.jsonl"), f.port);
    assert.equal(f.resources.get("Deployment/staff-control-center").spec.replicas, 1);
    assert.equal(f.resources.has("ServersTransport/staff-control-center"), false);
  } finally { f.cleanup(); }
});
test("unknown create is not retried and rollback resolves only exact owned intent", async () => {
  const f = fixture(); try {
    const p = await planned(f); f.flags.lostCreate = true;
    await assert.rejects(releaseFrontend(args(f, p, "apply", "apply.jsonl"), f.port));
    assert.equal(f.trace.filter(t => t.argv.includes("create")).length, 1);
    f.flags.lostCreate = false; await releaseFrontend(args(f, p, "rollback", "rollback.jsonl"), f.port);
    assert.equal(f.resources.get("Deployment/staff-control-center").spec.replicas, 1);
    assert.equal(f.resources.has("ServersTransport/staff-control-center"), false);
  } finally { f.cleanup(); }
});
test("unknown compiled patch and replaced created UID cannot silently overwrite foreign state", async () => {
  const f = fixture(); try {
    const p = await planned(f); f.flags.lostCompiled = true;
    await assert.rejects(releaseFrontend(args(f, p, "apply", "apply.jsonl"), f.port));
    const transport = f.resources.get("ServersTransport/staff-control-center"); transport.metadata.uid = "foreign-replacement";
    const patches = f.trace.filter(t => t.argv.includes("patch") && !t.argv.includes("--dry-run=server")).length;
    f.flags.lostCompiled = false; await assert.rejects(releaseFrontend(args(f, p, "rollback", "rollback.jsonl"), f.port), /CREATED_RESOURCE_DRIFT/);
    assert.equal(f.trace.filter(t => t.argv.includes("patch") && !t.argv.includes("--dry-run=server")).length, patches);
    assert.equal(f.resources.get("Deployment/staff-control-center").spec.replicas, 2);
  } finally { f.cleanup(); }
});
