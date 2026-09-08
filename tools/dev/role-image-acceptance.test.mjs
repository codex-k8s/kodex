import test from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { admittedArtifact, mutationDriver, privateJournal, projectPredecessor, runAcceptance } from "./role-image-acceptance.mjs";

const h = (value) => createHash("sha256").update(value).digest("hex");
const digest = "a".repeat(64);
const fixture = (t) => { const directory = mkdtempSync(join(tmpdir(), "riqa-")); chmodSync(directory, 0o700); t.after(() => rmSync(directory, { recursive: true, force: true })); return join(directory, "state.jsonl"); };
const response = (value, status = 200) => new Response(JSON.stringify(value), { status });

test("durable intent survives lost response and prevents another effect after restart", async (t) => {
  const path = fixture(t); const header = { version: 1, origin: "https://fixture.invalid" };
  let journal = privateJournal(path, header); let calls = 0;
  const request = async () => { calls++; assert.match(readFileSync(path, "utf8"), /"type":"INTENT"/); throw new Error("secret reflected by provider"); };
  await assert.rejects(mutationDriver(journal, request)("publish", "POST", "/api/v1/test", {}, 200, 1, (item) => item), /MUTATION_STOPPED/);
  journal.close(); journal = privateJournal(path, header);
  await assert.rejects(mutationDriver(journal, request)("publish", "POST", "/api/v1/test", {}, 200, 1, (item) => item), /UNRESOLVED_INTENT/);
  assert.equal(calls, 1); assert.equal(readFileSync(path, "utf8").includes("secret reflected"), false); journal.close();
});

test("successful command resume reads exact ACK and rejects changed body", async (t) => {
  const path = fixture(t); const journal = privateJournal(path, { version: 1 }); let calls = 0;
  const mutate = mutationDriver(journal, async () => { calls++; return response({ ref: "fixture", secret: "hidden" }); });
  assert.deepEqual(await mutate("create", "POST", "/api/v1/test", { name: "safe" }, 200, undefined, ({ ref }) => ({ ref })), { ref: "fixture" });
  assert.deepEqual(await mutate("create", "POST", "/api/v1/test", { name: "safe" }, 200, undefined, ({ ref }) => ({ ref })), { ref: "fixture" });
  await assert.rejects(mutate("create", "POST", "/api/v1/test", { name: "other" }, 200, undefined, ({ ref }) => ({ ref })), /COMMAND_CHANGED/);
  assert.equal(calls, 1); assert.equal(readFileSync(path, "utf8").includes("hidden"), false); journal.close();
});

for (const status of [412, 503]) test(`HTTP ${status} stops without retry and keeps a classified intent`, async (t) => {
  const journal = privateJournal(fixture(t), { version: 1 }); let calls = 0;
  await assert.rejects(mutationDriver(journal, async () => { calls++; return response({ code: "SECRET_CONTENT" }, status); })("build", "POST", "/api/v1/test", {}, 201, 1, (item) => item));
  assert.equal(calls, 1); assert.equal(journal.events.at(-1).outcome, status === 412 ? "REJECTED" : "UNKNOWN"); journal.close();
});

test("journal rejects concurrent writer, symlink, scope drift and truncated tail", (t) => {
  const path = fixture(t); const journal = privateJournal(path, { version: 1 });
  assert.throws(() => privateJournal(path, { version: 1 })); journal.close();
  assert.throws(() => privateJournal(path, { version: 2 }), /SCOPE_MISMATCH/);
  const link = `${path}.link`; symlinkSync(path, link); assert.throws(() => privateJournal(link, { version: 1 }));
  writeFileSync(path, '{"type":"HEADER"'); assert.throws(() => privateJournal(path, { version: 1 }), /TRUNCATED/);
  assert.equal(existsSync(`${path}.lock`), false);
});

test("inspect after UNKNOWN only reads owner state and never changes intent", async (t) => {
  const path = fixture(t); let journal = privateJournal(path, { version: 1 });
  journal.append({ type: "INTENT", step: "project", key: "key", method: "POST", path: "/api/v1/projects", bodySHA256: digest }); journal.close();
  const before = readFileSync(path, "utf8"); journal = privateJournal(path, { version: 1 }, { readOnly: true });
  const output = await runAcceptance({ phase: "inspect", journal, prefix: "fixture-test", request: async (_path, options) => { assert.equal(options.method, undefined); return response({ items: [{ ref: "project", version: 1, name: "fixture-test RoleImage", secret: "hidden" }] }); } });
  assert.equal(output.pendingSteps.length, 1); assert.deepEqual(output.projects, [{ ref: "project", version: 1 }]); assert.equal(readFileSync(path, "utf8"), before); journal.close();
});

function recoveryFixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "riqa-recovery-")); chmodSync(directory, 0o700); const path = join(directory, "state.jsonl"); const prefix = "fixture-test";
  const scope = { origin: "https://fixture.invalid", prefix, runnerDigest: `sha256:${digest}`, servingManifestSHA256: digest };
  const key = "11111111-1111-4111-8111-111111111111";
  const body = { name: `${prefix} RoleImage`, purpose: "Приёмка новой базы образа без provider запуска", language: "ru" };
  const previous = privateJournal(path, { version: 1, ...scope, sourceSHA: "b".repeat(40) });
  previous.append({ type: "INTENT", step: "project", key, method: "POST", path: "/api/v1/projects", bodySHA256: h(JSON.stringify(body)) });
  previous.append({ type: "STOP", step: "project", key, outcome: "UNKNOWN" }); previous.close();
  const oldBytes = readFileSync(path, "utf8"); const previousSHA = h(oldBytes);
  const predecessor = projectPredecessor(path, previousSHA, scope);
  const journal = privateJournal(`${path}.new`, { version: 1, ...scope, previousJournalSHA256: previousSHA });
  t.after(() => { journal.close(); rmSync(directory, { recursive: true, force: true }); });
  return { path, prefix, scope, key, body, oldBytes, previousSHA, predecessor, journal };
}

test("preflight failure creates no business intent or request", async (t) => {
  const journal = privateJournal(fixture(t), { version: 1 }); let requests = 0;
  await assert.rejects(runAcceptance({ phase: "prepare", journal, prefix: "fixture-test", preflight: async () => { throw new Error("expired"); }, request: async () => { requests++; } }), /expired/);
  assert.equal(requests, 0); assert.equal(journal.events.length, 1); journal.close();
});

test("recovery binds old bytes, command body and exact origin/prefix/manifest", (t) => {
  const f = recoveryFixture(t);
  assert.equal(f.predecessor.key, f.key);
  assert.throws(() => projectPredecessor(f.path, "c".repeat(64), f.scope), /DIGEST_MISMATCH/);
  for (const field of ["prefix", "origin", "servingManifestSHA256", "runnerDigest"]) assert.throws(() => projectPredecessor(f.path, f.previousSHA, { ...f.scope, [field]: "changed" }), /SCOPE_MISMATCH/);
  const changed = JSON.parse(f.oldBytes.split("\n")[1]); changed.bodySHA256 = "c".repeat(64);
  const lines = f.oldBytes.trimEnd().split("\n"); lines[1] = JSON.stringify(changed); const raw = `${lines.join("\n")}\n`; writeFileSync(f.path, raw);
  assert.throws(() => projectPredecessor(f.path, h(raw), f.scope), /NOT_FIRST_PROJECT/);
});

for (const scenario of ["existing", "expired", "conflict", "lost-ack", "accepted"]) test(`first-project recovery ${scenario} remains bounded and uses the original key`, async (t) => {
  const f = recoveryFixture(t); const calls = [];
  const options = { phase: "recover-project", journal: f.journal, predecessor: f.predecessor, prefix: f.prefix, preflight: async () => { if (scenario === "expired") throw new Error("expired"); }, request: async (path, request = {}) => {
    calls.push({ method: request.method ?? "GET", path, key: request.headers?.["Idempotency-Key"] });
    if (!request.method) return response({ items: scenario === "existing" ? [{ name: f.body.name, ref: "existing" }] : [] });
    if (path === "/api/v1/projects") {
      assert.equal(request.headers["Idempotency-Key"], f.key); assert.deepEqual(JSON.parse(request.body), f.body);
      if (scenario === "lost-ack") throw new Error("lost response");
      return response({ ref: "project" }, scenario === "accepted" ? 201 : 409);
    }
    return response({}, 409);
  } };
  await assert.rejects(runAcceptance(options));
  const projectPosts = calls.filter((item) => item.method === "POST" && item.path === "/api/v1/projects");
  assert.equal(projectPosts.length, ["existing", "expired"].includes(scenario) ? 0 : 1);
  assert.equal(readFileSync(f.path, "utf8"), f.oldBytes);
  if (["conflict", "lost-ack", "accepted"].includes(scenario)) {
    const previousCalls = calls.length;
    await assert.rejects(runAcceptance({ ...options, phase: "prepare" }), /UNRESOLVED_INTENT/);
    assert.equal(calls.length, previousCalls);
    assert.equal(f.journal.events.find((item) => item.type === "INTENT").key, f.key);
    assert.equal(f.journal.events.filter((item) => item.type === "PROJECT_DELIVERY_RECOVERY").length, 1);
  }
});

function artifactDetail(generation = 1) {
  const artifact = { ref: `artifact${generation}`, recipeRef: "recipe", recipeGeneration: generation, manifestDigest: `sha256:${digest}`, provenanceSha256: digest, sbomSha256: digest, vulnerabilityEvidenceSha256: digest, admissionVerdict: "ACCEPTED", promotedReference: `pull.fixture.invalid/kodex/roles@sha256:${digest}`, promotionReceiptSha256: digest, promotedAt: new Date().toISOString() };
  return { recipe: { ref: "recipe", version: generation, generation, managedLineage: { managedBy: "UI", configurationRef: "configuration", revisionRef: `revision${generation}` }, promotedImageReady: true, activeImageArtifactRef: artifact.ref }, builds: [{ ref: `build${generation}`, recipeRef: "recipe", recipeGeneration: generation, configurationRevisionRef: `revision${generation}`, stage: "COMPLETED" }], promotionCandidate: artifact, activeArtifact: artifact };
}

test("artifact requires exact completed build, lineage, scan, SBOM and promotion receipt", () => {
  assert.equal(admittedArtifact(artifactDetail(), "recipe", "revision1", true).buildRef, "build1");
  for (const change of [
    (value) => { value.activeArtifact.sbomSha256 = undefined; },
    (value) => { value.activeArtifact.vulnerabilityEvidenceSha256 = undefined; },
    (value) => { value.activeArtifact.promotionReceiptSha256 = undefined; },
    (value) => { value.builds[0].configurationRevisionRef = "other"; },
    (value) => { value.activeArtifact.promotedReference = `registry.fixture.invalid/agent-runner@sha256:${digest}`; },
    (value) => { value.recipe.managedLineage.managedBy = "SHIPPED"; },
  ]) { const value = artifactDetail(); change(value); assert.throws(() => admittedArtifact(value, "recipe", "revision1", true)); }
});

test("managed prepare, forward publication and exact historical restore use isolated bindings", async (t) => {
  const journal = privateJournal(fixture(t), { version: 1 }); const revisions = []; let generation = 0; let configurationVersion = 1; let bound = false; let activeReference; let activeArtifactRef; let publishedRevision; let mutations = 0;
  const template = `FROM registry.fixture.invalid/kodex/agent-runner@sha256:${digest}\n`;
  const configuration = () => ({ ref: "configuration", version: configurationVersion, kind: "ROLE_IMAGE", managedBy: "UI", projectRef: "project", ...(publishedRevision ? { currentRevision: publishedRevision } : {}) });
  const request = async (path, options = {}) => {
    const method = options.method ?? "GET"; const url = new URL(path, "https://fixture.invalid"); const body = options.body ? JSON.parse(options.body) : undefined;
    if (method !== "GET") mutations++;
    if (method === "GET" && url.pathname === "/api/v1/role-environments") return response({ items: [{ key: "standard", available: true, dockerfileTemplate: template }] });
    if (method === "POST" && path === "/api/v1/projects") return response({ ref: "project" }, 201);
    if (method === "POST" && path.endsWith("/agents")) return response({ ref: "agent", roleDefinitionRef: "role", version: 1 }, 201);
    if (method === "POST" && path.endsWith("/drafts")) {
      generation++; configurationVersion++;
      const revision = { ref: `revision${generation}`, revision: generation, state: "DRAFT", content: body.content, digest: h(body.content), sourceAvailable: true, ...(publishedRevision ? { parentRevisionRef: publishedRevision.ref } : {}) }; revisions.push(revision);
      return response({ configuration: configuration(), revision }, 201);
    }
    if (method === "POST" && /\/(validation|publication)$/.test(path)) {
      const revision = revisions.at(-1); revision.state = path.endsWith("validation") ? "VALID" : "PUBLISHED"; configurationVersion++; if (path.endsWith("publication")) publishedRevision = revision;
      return response({ configuration: configuration(), revision });
    }
    if (method === "GET" && url.pathname.endsWith("/role-image-recipes")) return response({ items: [{ ...artifactDetail(generation).recipe, roleDefinitionRef: "role" }] });
    if (method === "GET" && url.pathname.endsWith("/role-image-recipes/recipe")) return response(artifactDetail(generation));
    if (method === "POST" && path.endsWith("/promotions")) return response({ ref: `promotion${generation}`, recipeRef: "recipe", imageArtifactRef: `artifact${generation}`, state: "QUEUED" }, 202);
    if (method === "POST" && path.endsWith("/runtime-environments")) return response({ ref: "environment" }, 201);
    if (method === "PUT" && path.endsWith("/runtime-environment-binding")) { bound = true; activeReference = artifactDetail(generation).activeArtifact.promotedReference; activeArtifactRef = `artifact${generation}`; return response({ environment: { currentVersion: { image: { reference: activeReference } } } }); }
    if (method === "GET" && url.pathname.endsWith("/revisions")) return response({ configuration: configuration(), items: revisions });
    const impact = { ref: `plan${generation}`, digest, configurationRef: "configuration", revisionRef: `revision${generation}`, artifactRef: `artifact${generation}`, total: 2 };
    if (method === "POST" && path.endsWith("/impact-plans")) return response(impact, 201);
    if (method === "GET" && url.pathname.includes("/role-image-impact-plans/")) return response({ items: ["environment-item", "agent-item"].map((ref) => ({ ref, environmentRef: "environment", projectRef: "project", outcome: "APPLIED" })) });
    if (method === "POST" && path.endsWith("/consumer-bindings")) { assert.equal(body.selectedItemRefs.length, 2); activeArtifactRef = `artifact${generation}`; return response({ plan: { ...impact, state: "APPLIED" } }); }
    if (method === "GET" && path.endsWith("/runtime-configuration")) { assert.equal(bound, true); return response({ environment: { ref: "environment", currentVersion: { ref: "envversion", image: { reference: activeReference, artifactRef: activeArtifactRef } } }, environmentBinding: { ref: "binding", version: 1, digest, agentRef: "agent", environmentRef: "environment", versionRef: "envversion" } }); }
    throw new Error(`unhandled test route ${method} ${path}`);
  };
  for (const phase of ["prepare", "advance", "restore"]) {
    const result = await runAcceptance({ phase, journal, request, preflight: async () => {}, prefix: "fixture-test", runnerDigest: `sha256:${digest}` });
    assert.equal(result.status, "PASS"); assert.equal(result.runtimeJob, "NOT_RUN"); assert.equal(result.revisionRef, `revision${generation}`);
    const previous = mutations; await runAcceptance({ phase, journal, request, preflight: async () => {}, prefix: "fixture-test", runnerDigest: `sha256:${digest}` }); assert.equal(mutations, previous);
  }
  assert.equal(revisions.length, 3); assert.equal(revisions[2].content, revisions[0].content); assert.notEqual(revisions[1].content, revisions[0].content);
  assert.equal(journal.events.filter((item) => item.type === "INTENT" && item.path.endsWith("/publication")).length, 3);
  assert.equal(journal.events.some((item) => item.path?.endsWith("/runs")), false); journal.close();
});
