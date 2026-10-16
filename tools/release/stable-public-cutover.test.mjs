import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import test from "node:test";
import { fingerprint } from "./stable-inventory.mjs";
import { publicCanonicalRoutes, publicCutoverContent } from "./stable-public-cutover-model.mjs";
import { publicCutover } from "./stable-public-cutover.mjs";

async function fixture(t) {
  const dir = await mkdtemp(tmpdir() + "/kodex-public-cutover-test-"); t.after(() => rm(dir, { recursive: true, force: true }));
  const profile = JSON.parse(await readFile(new URL("../../infra/public-ingress/identity-profile.json", import.meta.url), "utf8")), operation = "a".repeat(32), source = "f".repeat(40),
    endpoints = { before: { publicOrigin: "https://control.example.test", oidcIssuer: "https://sso.example.test/realms/kodex" },
      after: { publicOrigin: "https://platform.kodex.works", oidcIssuer: "https://sso.kodex.works/realms/kodex" } },
    ca = { kind: "ConfigMap", metadata: { uid: "ca-uid" }, data: { "ca.pem": "FIXTURE_PUBLIC_CA" } }, objects = new Map(), trace = [], flags = {};
  const key = r => [r.namespace ?? r.metadata?.namespace, r.kind, r.name ?? r.metadata?.name].join("/");
  const rows = publicCanonicalRoutes(endpoints, profile).map(resource => {
    resource.metadata.annotations = { ...resource.metadata.annotations, "kodex.dev/public-cutover-operation": operation };
    return { kind: resource.kind, namespace: resource.metadata.namespace, name: resource.metadata.name, uid: null, before: null,
      resource, after: fingerprint(publicCutoverContent(resource)) };
  });
  const owner = () => ({ at: new Date().toISOString(), openBuilds: 0, pendingAdmissions: 0, pendingPromotions: 0, activeRuntimeRuns: 0,
    claimedRuntimeLeases: 0, promotedArtifactCount: 85, promotedPinsSHA256: (flags.pins ? "b" : "c").repeat(64) });
  const plan = { schema: "kodex.public-cutover-plan/v1", source, context: "k3d-kodex", stage: "canonical", operation,
    boundary: { clusterUID: "cluster-uid", caUID: ca.metadata.uid, caSHA256: fingerprint(ca.data), owner: { count: 85, pinsSHA256: "c".repeat(64) } },
    baseline: { endpoints, serving: [["identity", "sso"], ...["staff-control-center", "control-plane", "control-api-gateway"].map(n => ["kodex-system", n])]
      .map(([namespace, name]) => ({ namespace, name, proof: [{ kind: "Deployment", name, uid: name + "-uid" }] })),
    certificates: [["identity", profile.oidcCertificate], ["kodex-system", profile.platformCertificate]]
      .map(([namespace, name]) => ({ kind: "Certificate", metadata: { namespace, name } })) }, rows, serving: [], apply: "NOT_RUN" };
  const hash = fingerprint(plan), path = dir + "/plan.json"; await writeFile(path, JSON.stringify({ plan, planSHA256: hash }), { mode: 0o600 });
  const run = async (program, argv, input) => {
    if (program === "git") return argv[0] === "rev-parse" ? source : "";
    if (program === "/bin/sh") argv = argv.slice(argv.indexOf("--context"));
    if (argv.includes("exec")) return JSON.stringify(owner());
    if (argv.includes("get")) {
      const at = argv.indexOf("get"), kind = argv[at + 1], name = argv[at + 2], ns = argv[argv.indexOf("-n") + 1];
      if (kind === "namespace") return JSON.stringify({ metadata: { uid: "cluster-uid" } });
      if (name === "kodex-oidc-ca") return JSON.stringify(ca);
      if (kind === "deployments,ingresses") return JSON.stringify({ items: [...objects.values()].filter(r => r.metadata.namespace === ns && r.kind === "Ingress") });
      return objects.has([ns, kind, name].join("/")) ? JSON.stringify(objects.get([ns, kind, name].join("/"))) : "";
    }
    if (argv.includes("create")) {
      const r = JSON.parse(input); assert.equal(objects.has(key(r)), false); trace.push({ verb: "create", key: key(r) });
      r.metadata.uid = r.metadata.name + "-uid"; r.metadata.resourceVersion = "1"; objects.set(key(r), r);
      if (flags.lost) { flags.lost = false; throw Error("FIXTURE_ACK_LOST"); }
      return JSON.stringify(r);
    }
    if (argv.includes("delete")) {
      const path = argv[argv.indexOf("--raw") + 1], name = path.split("/").at(-1), kind = path.split("/").at(-2) === "ingresses" ? "Ingress" : "Middleware",
        row = [...objects.values()].find(r => r.metadata.name === name && r.kind === kind), expected = JSON.parse(input).preconditions;
      assert.equal(row.metadata.uid, expected.uid); assert.equal(row.metadata.resourceVersion, expected.resourceVersion);
      trace.push({ verb: "delete", key: key(row) }); objects.delete(key(row)); return "{}";
    }
    throw Error("FIXTURE_COMMAND_INVALID");
  };
  const invoke = (mode, file) => publicCutover([mode, "--context", "k3d-kodex", "--plan", path, "--output", dir + "/" + file,
    ...(mode === "apply" ? [] : ["--journal", dir + "/apply.jsonl"]),
    ...(["apply", "continue", "rollback"].includes(mode) ? ["--approved-plan-sha", hash] : [])], run);
  return { invoke, objects, trace, flags, dir };
}

test("synthetic canonical apply/readback/rollback deletes only exact own resources and does not replay", async t => {
  const f = await fixture(t); await f.invoke("apply", "apply.jsonl"); assert.equal(f.objects.size, 4);
  await f.invoke("readback", "readback.json"); await f.invoke("rollback", "rollback.jsonl"); assert.equal(f.objects.size, 0);
  assert.equal(f.trace.length, 8); await assert.rejects(f.invoke("rollback", "rollback-again.jsonl"), /EEXIST/); assert.equal(f.trace.length, 8);
});

test("lost create ACK is inspected without replay; guarded rollback can resolve its exact operation", async t => {
  const f = await fixture(t); f.flags.lost = true; await assert.rejects(f.invoke("apply", "apply.jsonl"), /FIXTURE_ACK_LOST/);
  assert.equal(f.trace.length, 1); await f.invoke("inspect", "inspect.json"); assert.equal(f.trace.length, 1);
  await f.invoke("rollback", "rollback.jsonl"); assert.equal(f.objects.size, 0); assert.equal(f.trace.length, 2);
});

test("explicit continuation does not recreate observed resources and finishes only untouched rows", async t => {
  const f = await fixture(t); f.flags.lost = true; await assert.rejects(f.invoke("apply", "apply.jsonl"), /FIXTURE_ACK_LOST/);
  await f.invoke("continue", "continue.jsonl"); assert.equal(f.objects.size, 4); assert.equal(f.trace.length, 4);
  const records = (await readFile(f.dir + "/continue.jsonl", "utf8")).trim().split("\n").map(JSON.parse);
  assert.equal(records[0].mode, "apply"); assert.equal(records.filter(r => r.status === "INTENT").length, 4);
  assert.equal(records.at(-1).status, "COMPLETE");
});

test("foreign UID, unrelated object drift and changed owner pins prevent all recovery mutations", async t => {
  for (const type of ["uid", "spec", "pins"]) {
    const f = await fixture(t); await f.invoke("apply", "apply.jsonl"); const r = [...f.objects.values()][0];
    if (type === "uid") r.metadata.uid = "foreign";
    else if (type === "spec") r.spec.redirectRegex.replacement = "https://foreign.example.test/";
    else f.flags.pins = true;
    const count = f.trace.length; await assert.rejects(f.invoke("rollback", "rollback.jsonl"), /PUBLIC_CUTOVER_/); assert.equal(f.trace.length, count);
  }
});
