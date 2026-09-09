#!/usr/bin/env node

import { createHash, randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import { closeSync, constants, existsSync, fstatSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, unlinkSync, writeSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createOwnerSessionClient } from "./owner-session-client.mjs";
import { exactOrigin } from "./owner-session-storage.mjs";
import { boundedResponseBody } from "./runtime-workspace-acceptance.mjs";

const root = fileURLToPath(new URL("../../", import.meta.url));
const confirmation = "APPLY-STAGING-ROLE-IMAGE-FIXTURE";
const recoveryConfirmation = "RECOVER-SAME-STAGING-PROJECT-INTENT";
const sha = (value) => createHash("sha256").update(typeof value === "string" || Buffer.isBuffer(value) ? value : JSON.stringify(value)).digest("hex");
const requireValue = (ok, code) => { if (!ok) throw new Error(code); };
const ref = (value) => { requireValue(typeof value === "string" && /^[a-zA-Z0-9_-]{1,160}$/.test(value), "REFERENCE_INVALID"); return value; };
const version = (value) => { requireValue(Number.isSafeInteger(value) && value > 0, "VERSION_INVALID"); return value; };
const digest = (value) => { requireValue(typeof value === "string" && /^[a-f0-9]{64}$/.test(value), "DIGEST_INVALID"); return value; };
const encode = encodeURIComponent;
const projectBody = (prefix) => ({ name: `${prefix} RoleImage`, purpose: "Приёмка новой базы образа без provider запуска", language: "ru" });
export const bindingPin = (item) => ({ ref: ref(item?.ref), version: version(item.version), agentRef: ref(item.agentRef), environmentRef: ref(item.environmentRef), versionRef: ref(item.versionRef), digest: digest(item.digest) });
const safeState = (value) => ["DRAFT", "VALID", "INVALID", "PUBLISHED", "SUPERSEDED", "DISCARDED", "QUEUED", "MATERIALIZATION", "CONTEXT_VALIDATION", "BASE_PULL", "SOLVING", "INSTALLATION", "TRUSTED_RUNTIME_FINALIZATION", "STAGING_PUSH", "PROVENANCE", "COMPLETED", "FAILED", "CANCELLED", "EXPIRED", "DEAD_LETTER"].includes(value) ? value : "UNKNOWN";

export function privateJournal(path, header, { readOnly = false, createOnly = false } = {}) {
  path = resolve(path);
  requireValue(realpathSync(dirname(path)) === dirname(path) && (lstatSync(dirname(path)).mode & 0o077) === 0, "PRIVATE_DIRECTORY_REQUIRED");
  let lock;
  if (!readOnly) {
    lock = openSync(`${path}.lock`, "wx", 0o600);
    writeSync(lock, `${process.pid}\n`); fsyncSync(lock);
  }
  let fd;
  try {
    const fresh = !existsSync(path);
    requireValue(!createOnly || fresh, "JOURNAL_ALREADY_EXISTS");
    requireValue(!fresh || !readOnly, "JOURNAL_MISSING");
    fd = openSync(path, (readOnly ? constants.O_RDONLY : constants.O_RDWR | constants.O_APPEND) | constants.O_NOFOLLOW | (fresh ? constants.O_CREAT | constants.O_EXCL : 0), 0o600);
    const info = fstatSync(fd);
    requireValue(info.isFile() && info.nlink === 1 && (info.mode & 0o077) === 0 && info.size <= (8 << 20), "JOURNAL_INVALID");
    const raw = readFileSync(fd, "utf8");
    requireValue(!raw || raw.endsWith("\n"), "JOURNAL_TRUNCATED");
    const events = raw ? raw.trimEnd().split("\n").map((line) => JSON.parse(line)) : [];
    const append = (event) => {
      requireValue(!readOnly, "JOURNAL_READ_ONLY");
      const item = { ...event, at: new Date().toISOString() };
      const buffer = Buffer.from(`${JSON.stringify(item)}\n`);
      for (let offset = 0; offset < buffer.length;) offset += writeSync(fd, buffer, offset, buffer.length - offset);
      fsyncSync(fd); events.push(item);
    };
    if (fresh) {
      append({ type: "HEADER", ...header });
      const directory = openSync(dirname(path), constants.O_RDONLY); try { fsyncSync(directory); } finally { closeSync(directory); }
    }
    requireValue(events[0]?.type === "HEADER" && Object.entries(header).every(([key, value]) => JSON.stringify(events[0][key]) === JSON.stringify(value)), "JOURNAL_SCOPE_MISMATCH");
    return { events, append, bytesSHA256: sha(raw), close() { closeSync(fd); if (lock !== undefined) { closeSync(lock); unlinkSync(`${path}.lock`); } } };
  } catch (error) {
    if (fd !== undefined) closeSync(fd);
    if (lock !== undefined) { closeSync(lock); unlinkSync(`${path}.lock`); }
    throw error;
  }
}

// Исключение только для первой owner-idempotent Project create. Не применять
// к build/provider UNKNOWN: отсутствие ресурса не доказывает отсутствие эффекта.
export function projectPredecessor(path, expectedSHA256, scope) {
  const previous = privateJournal(path, { version: 1, ...scope }, { readOnly: true });
  try {
    requireValue(previous.bytesSHA256 === digest(expectedSHA256), "PREDECESSOR_DIGEST_MISMATCH");
    const [header, intent, stop] = previous.events;
    requireValue(previous.events.length === 3 && /^[a-f0-9]{40}$/.test(header.sourceSHA ?? "") && intent?.type === "INTENT" && intent.step === "project" && intent.method === "POST" && intent.path === "/api/v1/projects" && /^[a-f0-9-]{36}$/.test(intent.key ?? "") && intent.bodySHA256 === sha(projectBody(scope.prefix)) && intent.version === undefined && stop?.type === "STOP" && stop.step === "project" && stop.key === intent.key && stop.outcome === "UNKNOWN" && stop.status === undefined, "PREDECESSOR_NOT_FIRST_PROJECT");
    return { previousJournalSHA256: expectedSHA256, previousSourceSHA: header.sourceSHA, key: intent.key, bodySHA256: intent.bodySHA256 };
  } finally { previous.close(); }
}

// Журнал сохраняет intent до HTTP, но никогда не сохраняет исходник/ответ целиком.
// Незавершённый intent запрещает повторную mutation даже после restart.
export function mutationDriver(journal, request) {
  return async (step, method, path, body, expectedStatus, expectedVersion, project) => {
    const prior = journal.events.findLast((item) => item.step === step && item.type === "ACK");
    if (prior) {
      const intent = journal.events.find((item) => item.type === "INTENT" && item.key === prior.key);
      requireValue(intent?.method === method && intent.path === path && intent.bodySHA256 === sha(body ?? null), "COMMAND_CHANGED");
      return prior.result;
    }
    requireValue(!journal.events.some((item) => item.type === "INTENT" && !journal.events.some((later) => later.type === "ACK" && later.key === item.key)), "UNRESOLVED_INTENT_READBACK_REQUIRED");
    const recovery = step === "project" ? journal.events.find((item) => item.type === "PROJECT_DELIVERY_RECOVERY") : undefined;
    if (recovery) requireValue(method === "POST" && path === "/api/v1/projects" && recovery.bodySHA256 === sha(body), "RECOVERY_COMMAND_CHANGED");
    const key = recovery?.key ?? randomUUID();
    journal.append({ type: "INTENT", step, key, method, path, bodySHA256: sha(body ?? null), ...(expectedVersion === undefined ? {} : { version: version(expectedVersion) }) });
    let status;
    try {
      const headers = { Accept: "application/json", "Idempotency-Key": key };
      if (body !== undefined) headers["Content-Type"] = "application/json";
      if (expectedVersion !== undefined) headers["If-Match"] = `"${expectedVersion}"`;
      const response = await request(path, { method, headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
      status = response.status;
      const payload = JSON.parse((await boundedResponseBody(response, 2 << 20)).toString("utf8"));
      requireValue(status === expectedStatus, "HTTP_STATUS");
      const result = project(payload);
      journal.append({ type: "ACK", step, key, status, result });
      return result;
    } catch {
      journal.append({ type: "STOP", step, key, outcome: [400, 401, 403, 404, 409, 412, 422].includes(status) ? "REJECTED" : "UNKNOWN", ...(status ? { status } : {}) });
      throw new Error("MUTATION_STOPPED_READBACK_REQUIRED");
    }
  };
}

export function managed(value, expectedState) {
  requireValue(value?.configuration?.kind === "ROLE_IMAGE" && value.configuration.managedBy === "UI" && value.revision?.state === expectedState, "MANAGED_STATE_INVALID");
  return { configurationRef: ref(value.configuration.ref), version: version(value.configuration.version), revisionRef: ref(value.revision.ref), revision: version(value.revision.revision), contentSHA256: digest(value.revision.digest), ...(value.revision.parentRevisionRef ? { parentRevisionRef: ref(value.revision.parentRevisionRef) } : {}), ...(value.configuration.currentRevision ? { publishedRevisionRef: ref(value.configuration.currentRevision.ref) } : {}) };
}

export function admittedArtifact(detail, recipeRef, revisionRef, promoted = false) {
  requireValue(detail?.recipe?.ref === recipeRef && detail.recipe.managedLineage?.managedBy === "UI" && detail.recipe.managedLineage.revisionRef === revisionRef, "RECIPE_LINEAGE_INVALID");
  const artifact = promoted ? detail.activeArtifact : detail.promotionCandidate;
  requireValue(artifact?.recipeRef === recipeRef && artifact.recipeGeneration === detail.recipe.generation && artifact.admissionVerdict === "ACCEPTED", "ARTIFACT_NOT_ADMITTED");
  requireValue(/^sha256:[a-f0-9]{64}$/.test(artifact.manifestDigest ?? ""), "IMAGE_DIGEST_INVALID");
  const build = detail.builds?.find((item) => item.configurationRevisionRef === revisionRef && item.recipeGeneration === artifact.recipeGeneration && item.stage === "COMPLETED");
  requireValue(build && build.recipeRef === recipeRef, "EXACT_BUILD_MISSING");
  const result = { recipeRef, recipeVersion: version(detail.recipe.version), generation: version(artifact.recipeGeneration), revisionRef, buildRef: ref(build.ref), artifactRef: ref(artifact.ref), manifestDigest: artifact.manifestDigest, provenanceSHA256: digest(artifact.provenanceSha256), sbomSHA256: digest(artifact.sbomSha256), vulnerabilityEvidenceSHA256: digest(artifact.vulnerabilityEvidenceSha256) };
  if (promoted) {
    requireValue(detail.recipe.promotedImageReady === true && detail.recipe.activeImageArtifactRef === artifact.ref && /^[a-z0-9][a-z0-9./:_-]*\/roles@sha256:[a-f0-9]{64}$/.test(artifact.promotedReference ?? "") && artifact.promotedReference.endsWith(`@${artifact.manifestDigest}`), "PROMOTED_REFERENCE_INVALID");
    result.promotedReference = artifact.promotedReference;
    result.promotionReceiptSHA256 = digest(artifact.promotionReceiptSha256);
    requireValue(Number.isFinite(Date.parse(artifact.promotedAt)), "PROMOTED_TIMESTAMP_INVALID");
  }
  return result;
}

export async function runAcceptance({ phase, journal, request, preflight, predecessor, prefix, runnerDigest, timeoutMs = 1200000, sleep = (ms) => new Promise((done) => setTimeout(done, ms)), now = Date.now }) {
  const deadline = now() + timeoutMs;
  const mutate = mutationDriver(journal, request);
  const get = async (path) => {
    requireValue(now() < deadline, "READBACK_DEADLINE");
    const response = await request(path, { headers: { Accept: "application/json" } });
    requireValue(response.status === 200, `READBACK_HTTP_${response.status}`);
    return JSON.parse((await boundedResponseBody(response, 2 << 20)).toString("utf8"));
  };
  const saved = (step) => journal.events.findLast((event) => event.step === step && ["ACK", "CHECKPOINT"].includes(event.type))?.result;
  const checkpoint = (step, result) => { journal.append({ type: "CHECKPOINT", step, result }); return result; };
  const pages = async (path) => {
    const items = []; const cursors = new Set(); let cursor;
    do {
      const page = await get(`${path}${path.includes("?") ? "&" : "?"}pageSize=50${cursor ? `&pageToken=${encode(cursor)}` : ""}`);
      requireValue(Array.isArray(page.items) && items.length + page.items.length <= 1000, "PAGE_INVALID"); items.push(...page.items);
      cursor = page.nextPageToken; requireValue(!cursor || !cursors.has(cursor), "PAGE_CURSOR_REPEATED"); if (cursor) cursors.add(cursor);
    } while (cursor);
    return items;
  };
  if (phase === "inspect") {
    const project = saved("project");
    const output = { pendingSteps: journal.events.filter((event) => event.type === "INTENT" && !journal.events.some((ack) => ack.type === "ACK" && ack.key === event.key)).map(({ step, method, path }) => ({ step, method, path })) };
    if (project) {
      output.recipes = (await pages(`/api/v1/projects/${project.ref}/role-image-recipes`)).filter((item) => item.name === `${prefix} Образ`).map((item) => ({ ref: ref(item.ref), version: version(item.version), generation: version(item.generation), promotedImageReady: item.promotedImageReady === true, ...(item.managedLineage?.revisionRef ? { revisionRef: ref(item.managedLineage.revisionRef) } : {}) }));
      output.configurations = (await pages(`/api/v1/managed-configurations?kind=ROLE_IMAGE&projectRef=${project.ref}`)).filter((item) => item.name === `${prefix} Образ`).map((item) => ({ ref: ref(item.ref), version: version(item.version), ...(item.currentRevision ? { revisionRef: ref(item.currentRevision.ref), state: safeState(item.currentRevision.state) } : {}) }));
      for (const recipe of output.recipes) {
        const detail = await get(`/api/v1/projects/${project.ref}/role-image-recipes/${recipe.ref}`);
        recipe.builds = (detail.builds ?? []).map((build) => ({ ref: ref(build.ref), stage: safeState(build.stage), ...(build.configurationRevisionRef ? { revisionRef: ref(build.configurationRevisionRef) } : {}) }));
        const artifact = detail.activeArtifact ?? detail.promotionCandidate;
        if (artifact) recipe.artifact = { ref: ref(artifact.ref), verdict: ["ACCEPTED", "REJECTED"].includes(artifact.admissionVerdict) ? artifact.admissionVerdict : "UNKNOWN", promoted: typeof artifact.promotedReference === "string" && Boolean(artifact.promotionReceiptSha256) };
      }
      for (const configuration of output.configurations) configuration.revisions = (await pages(`/api/v1/managed-configurations/${configuration.ref}/revisions`)).map((item) => ({ ref: ref(item.ref), revision: version(item.revision), state: safeState(item.state), digest: digest(item.digest) }));
    } else output.projects = (await pages(`/api/v1/projects?query=${encode(prefix)}`)).filter((item) => item.name === `${prefix} RoleImage`).map((item) => ({ ref: ref(item.ref), version: version(item.version) }));
    return output;
  }
  requireValue(["prepare", "advance", "restore", "recover-project"].includes(phase), "PHASE_INVALID");
  requireValue(typeof preflight === "function", "SESSION_PREFLIGHT_REQUIRED");
  await preflight();
  if (phase === "recover-project") {
    requireValue(predecessor && journal.events.length === 1 && journal.events[0].previousJournalSHA256 === predecessor.previousJournalSHA256, "FRESH_RECOVERY_JOURNAL_REQUIRED");
    const matching = (await pages(`/api/v1/projects?query=${encode(prefix)}`)).filter((item) => item.name === projectBody(prefix).name);
    requireValue(matching.length === 0, "RECOVERY_PROJECT_ALREADY_EXISTS");
    journal.append({ type: "PROJECT_DELIVERY_RECOVERY", ...predecessor, authoritativeAbsence: true });
    phase = "prepare";
  }
  requireValue(!journal.events.some((event) => event.type === "INTENT" && !journal.events.some((ack) => ack.type === "ACK" && ack.key === event.key)), "UNRESOLVED_INTENT_READBACK_REQUIRED");
  if (saved(`${phase}-complete`)) return saved(`${phase}-complete`);
  if (phase !== "prepare") requireValue(saved("prepare-complete") && (phase !== "restore" || saved("advance-complete")), "PREVIOUS_PHASE_REQUIRED");
  const project = saved("project") ?? await mutate("project", "POST", "/api/v1/projects", projectBody(prefix), 201, undefined, (item) => ({ ref: ref(item.ref) }));
  const agent = saved("agent") ?? await mutate("agent", "POST", `/api/v1/projects/${project.ref}/agents`, { name: `${prefix} Исполнитель`, purpose: "Приёмка привязки образа", roleDescription: "Проверочный исполнитель", initialInstructions: "Выполняй только явно поставленные задачи." }, 201, undefined, (item) => ({ ref: ref(item.ref), roleDefinitionRef: ref(item.roleDefinitionRef), version: version(item.version) }));
  const catalog = await get("/api/v1/role-environments");
  const standard = catalog.items?.find((item) => item.key === "standard" && item.available === true);
  const template = standard?.dockerfileTemplate;
  requireValue(typeof template === "string" && /^FROM [a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}\n$/.test(template) && template.endsWith(`@${runnerDigest}\n`), "TRUSTED_BASE_TEMPLATE_MISMATCH");
  const templateSHA256 = sha(template);
  if (saved("template")) requireValue(saved("template").sha256 === templateSHA256, "CATALOG_CHANGED");
  else checkpoint("template", { sha256: templateSHA256 });
  const dockerfile = phase === "advance" ? `${template}\n# CFG-01 forward revision\n` : template;
  const content = JSON.stringify({ name: `${prefix} Образ`, roleImage: { roleDefinitionRef: agent.roleDefinitionRef, environment: { environmentKey: "standard", packageKeys: [], toolKeys: [], dockerfile } } });
  const configurationRef = saved("prepare-draft")?.configurationRef;
  let current;
  if (phase !== "prepare") {
    const history = await get(`/api/v1/managed-configurations/${configurationRef}/revisions?pageSize=50`);
    current = history.configuration;
    requireValue(current?.ref === configurationRef && current.managedBy === "UI" && current.projectRef === project.ref, "CONFIGURATION_SCOPE_INVALID");
    if (!saved(`${phase}-before`)) checkpoint(`${phase}-before`, { publishedRevisionRef: ref(current.currentRevision?.ref), revision: version(current.currentRevision?.revision), binding: bindingPin((await get(`/api/v1/agents/${agent.ref}/runtime-configuration`)).environmentBinding) });
    if (phase === "restore") {
      const first = (await pages(`/api/v1/managed-configurations/${configurationRef}/revisions`)).find((item) => item.ref === saved("prepare-draft").revisionRef);
      requireValue(first?.sourceAvailable === true && first.content === content && first.digest === saved("prepare-draft").contentSHA256, "HISTORICAL_SOURCE_MISMATCH");
    }
  }
  const draft = await mutate(`${phase}-draft`, "POST", "/api/v1/role-image-configurations/drafts", { ...(configurationRef && phase !== "prepare" ? { configurationRef } : {}), projectRef: project.ref, name: `${prefix} Образ`, contentFormat: "JSON", content }, 201, phase === "prepare" ? undefined : current.version, (item) => managed(item, "DRAFT"));
  requireValue(draft.contentSHA256 === sha(content), "DRAFT_DIGEST_MISMATCH");
  if (phase !== "prepare") requireValue(draft.revision > saved(`${phase}-before`).revision && draft.revisionRef !== saved("prepare-draft").revisionRef && draft.parentRevisionRef === saved(`${phase}-before`).publishedRevisionRef && draft.publishedRevisionRef === saved(`${phase}-before`).publishedRevisionRef, "FORWARD_REVISION_REQUIRED");
  const revisionPath = `/api/v1/role-image-configurations/${draft.configurationRef}/revisions/${draft.revisionRef}`;
  const validated = await mutate(`${phase}-validate`, "POST", `${revisionPath}/validation`, undefined, 200, draft.version, (item) => managed(item, "VALID"));
  if (phase !== "prepare") requireValue(validated.publishedRevisionRef === saved(`${phase}-before`).publishedRevisionRef, "DRAFT_MOVED_PUBLISHED_POINTER");
  const published = await mutate(`${phase}-publish`, "POST", `${revisionPath}/publication`, undefined, 200, validated.version, (item) => managed(item, "PUBLISHED"));
  const recipes = await pages(`/api/v1/projects/${project.ref}/role-image-recipes`);
  const matching = recipes.filter((item) => item.managedLineage?.configurationRef === draft.configurationRef && item.managedLineage.revisionRef === draft.revisionRef);
  requireValue(matching.length === 1 && matching[0].roleDefinitionRef === agent.roleDefinitionRef, "EXACT_RECIPE_MISSING");
  const recipeRef = ref(matching[0].ref);
  const recipePath = `/api/v1/projects/${project.ref}/role-image-recipes/${recipeRef}`;
  const waitArtifact = async (promoted) => {
    for (;;) {
      const detail = await get(recipePath);
      requireValue(!detail.builds?.some((item) => item.configurationRevisionRef === draft.revisionRef && ["FAILED", "CANCELLED", "EXPIRED", "DEAD_LETTER"].includes(item.stage)), "BUILD_TERMINAL_FAILURE");
      const artifact = promoted ? detail.activeArtifact : detail.promotionCandidate;
      if (artifact?.recipeGeneration === detail.recipe?.generation && artifact?.admissionVerdict === "REJECTED") throw new Error("ADMISSION_REJECTED");
      if (artifact?.recipeGeneration === detail.recipe?.generation && artifact?.admissionVerdict === "ACCEPTED" && (!promoted || detail.recipe?.promotedImageReady === true)) return admittedArtifact(detail, recipeRef, draft.revisionRef, promoted);
      await sleep(2000);
    }
  };
  const candidate = saved(`${phase}-candidate`) ?? checkpoint(`${phase}-candidate`, await waitArtifact(false));
  await mutate(`${phase}-promote`, "POST", `${recipePath}/promotions`, { imageArtifactRef: candidate.artifactRef, expectedProvenanceSha256: candidate.provenanceSHA256 }, 202, candidate.recipeVersion, (item) => {
    requireValue(item.recipeRef === recipeRef && item.imageArtifactRef === candidate.artifactRef && ["QUEUED", "PROMOTING", "PROMOTED"].includes(item.state), "PROMOTION_RECEIPT_INVALID");
    return { ref: ref(item.ref), artifactRef: candidate.artifactRef };
  });
  const artifact = checkpoint(`${phase}-artifact`, await waitArtifact(true));
  let environment;
  if (phase === "prepare") {
    environment = await mutate("environment", "POST", `/api/v1/projects/${project.ref}/runtime-environments`, { name: `${prefix} Окружение`, description: "Приёмка exact admitted image", imageArtifactRef: artifact.artifactRef, tools: [], values: [], secretBindings: [], policy: { resources: { cpuRequestMilli: 100, cpuLimitMilli: 500, memoryRequestMib: 128, memoryLimitMib: 512, ephemeralStorageRequestMib: 256, ephemeralStorageLimitMib: 1024 }, volumes: [], networkDestinations: ["DNS", "RUNTIME_CALLBACK", "PROVIDER_PROXY"], kubernetesAccess: "NONE" } }, 201, undefined, (item) => ({ ref: ref(item.ref) }));
    await mutate("bind-agent", "PUT", `/api/v1/agents/${agent.ref}/runtime-environment-binding`, { environmentRef: environment.ref }, 200, agent.version, (item) => { requireValue(item.environment?.currentVersion?.image?.reference === artifact.promotedReference, "BINDING_IMAGE_MISMATCH"); return { environmentRef: environment.ref }; });
  } else {
    environment = saved("environment");
    requireValue(sha(bindingPin((await get(`/api/v1/agents/${agent.ref}/runtime-configuration`)).environmentBinding)) === sha(saved(`${phase}-before`).binding), "PUBLICATION_CHANGED_OLD_BINDING");
    const plan = await mutate(`${phase}-impact`, "POST", `${revisionPath}/impact-plans`, undefined, 201, published.version, (item) => { requireValue(item.configurationRef === draft.configurationRef && item.revisionRef === draft.revisionRef && item.artifactRef === artifact.artifactRef && item.total >= 2, "NONEMPTY_IMPACT_REQUIRED"); return { ref: ref(item.ref), digest: digest(item.digest) }; });
    const items = await pages(`/api/v1/role-image-impact-plans/${plan.ref}`);
    const selected = items.filter((item) => item.projectRef === project.ref && item.environmentRef === environment.ref);
    requireValue(selected.length >= 2 && selected.length === items.length, "IMPACT_FIXTURE_SCOPE_MISMATCH");
    await mutate(`${phase}-rebind`, "POST", `${revisionPath}/consumer-bindings`, { planRef: plan.ref, impactDigest: plan.digest, selectedItemRefs: selected.map((item) => ref(item.ref)) }, 200, published.version, (item) => { requireValue(item.plan?.ref === plan.ref && item.plan.state === "APPLIED", "REBIND_NOT_APPLIED"); return { planRef: plan.ref }; });
    const outcomes = await pages(`/api/v1/role-image-impact-plans/${plan.ref}`);
    requireValue(outcomes.length === selected.length && outcomes.every((item) => item.outcome === "APPLIED"), "REBIND_OUTCOME_INVALID");
  }
  const bound = await get(`/api/v1/agents/${agent.ref}/runtime-configuration`);
  requireValue(bound.environment?.ref === environment.ref && bound.environment?.currentVersion?.image?.reference === artifact.promotedReference && bound.environment.currentVersion.image.artifactRef === artifact.artifactRef && bound.environmentBinding?.agentRef === agent.ref && bound.environmentBinding.environmentRef === environment.ref && bound.environmentBinding.versionRef === bound.environment.currentVersion.ref, "FINAL_BINDING_IMAGE_MISMATCH");
  return checkpoint(`${phase}-complete`, { status: "PASS", projectRef: project.ref, agentRef: agent.ref, environmentRef: environment.ref, configurationRef: draft.configurationRef, ...artifact, runtimeJob: "NOT_RUN", browserUI: "NOT_RUN" });
}

async function main() {
  const args = process.argv.slice(2); const phase = args.shift(); const options = {};
  while (args.length) { const key = args.shift(); requireValue(/^--[a-z][a-z0-9-]*$/.test(key ?? "") && args.length && !(key in options), "ARGUMENT_INVALID"); options[key] = args.shift(); }
  requireValue(Object.keys(options).every((key) => ["--origin", "--storage-state", "--state", "--prefix", "--runner-digest", "--serving-manifest", "--timeout-ms", "--confirm", "--previous-state", "--previous-sha256"].includes(key)), "ARGUMENT_UNKNOWN");
  requireValue(["prepare", "advance", "restore", "inspect", "recover-project"].includes(phase), "PHASE_INVALID");
  const origin = exactOrigin(options["--origin"] ?? "");
  requireValue(new URL(origin).protocol === "https:" && !/prod(?:uction)?/i.test(new URL(origin).hostname), "STAGING_ORIGIN_REQUIRED");
  requireValue(phase === "inspect" || options["--confirm"] === (phase === "recover-project" ? recoveryConfirmation : confirmation), "CONFIRMATION_REQUIRED");
  const prefix = options["--prefix"]; const runnerDigest = options["--runner-digest"];
  requireValue(/^[a-z][a-z0-9-]{5,35}$/.test(prefix ?? "") && /^sha256:[a-f0-9]{64}$/.test(runnerDigest ?? ""), "FIXTURE_ARGUMENT_INVALID");
  requireValue(options["--storage-state"] && options["--state"] && options["--serving-manifest"], "PRIVATE_INPUT_REQUIRED");
  const manifestPath = resolve(options["--serving-manifest"]);
  const manifestInfo = lstatSync(manifestPath); requireValue(manifestInfo.isFile() && !manifestInfo.isSymbolicLink() && (manifestInfo.mode & 0o077) === 0 && manifestInfo.size > 0 && manifestInfo.size <= (1 << 20), "MANIFEST_INVALID");
  const servingManifestSHA256 = sha(readFileSync(manifestPath));
  const timeoutMs = Number(options["--timeout-ms"] ?? 1200000); requireValue(Number.isSafeInteger(timeoutMs) && timeoutMs >= 60000 && timeoutMs <= 1800000, "TIMEOUT_INVALID");
  const sourceSHA = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
  requireValue(execFileSync("git", ["status", "--porcelain"], { cwd: root, encoding: "utf8" }).trim() === "", "CLEAN_CHECKOUT_REQUIRED");
  const scope = { origin, prefix, runnerDigest, servingManifestSHA256 };
  let predecessor;
  if (phase === "recover-project") {
    requireValue(options["--previous-state"] && options["--previous-sha256"] && !existsSync(resolve(options["--state"])), "RECOVERY_INPUT_INVALID");
    predecessor = projectPredecessor(options["--previous-state"], options["--previous-sha256"], scope);
    execFileSync("git", ["merge-base", "--is-ancestor", predecessor.previousSourceSHA, sourceSHA], { cwd: root, stdio: "pipe" });
  } else requireValue(!options["--previous-state"] && !options["--previous-sha256"], "RECOVERY_ARGUMENTS_FORBIDDEN");
  const journal = privateJournal(options["--state"], { version: 1, ...scope, sourceSHA, ...(predecessor ? { previousJournalSHA256: predecessor.previousJournalSHA256, previousSourceSHA: predecessor.previousSourceSHA } : {}) }, { readOnly: phase === "inspect" });
  try {
    const client = createOwnerSessionClient({ origin, storagePath: resolve(options["--storage-state"]) });
    const deadline = Date.now() + timeoutMs;
    const signal = () => AbortSignal.timeout(Math.max(1, Math.min(30000, deadline - Date.now())));
    const result = await runAcceptance({ phase, journal, prefix, runnerDigest, timeoutMs, predecessor, preflight: async () => { const response = await client.observe("/api/v1/session", { signal: signal() }); requireValue(response.status === 200, "SESSION_PREFLIGHT_FAILED"); await response.body?.cancel(); }, request: (path, options) => client.request(path, { ...options, signal: signal() }) });
    process.stdout.write(`${JSON.stringify(result)}\n`);
  } finally { journal.close(); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main().catch((error) => {
  const code = /^[A-Z][A-Z0-9_]{1,80}$/.test(error?.message ?? "") ? error.message : "ROLE_IMAGE_ACCEPTANCE_FAILED";
  process.stderr.write(`${JSON.stringify({ status: "FAIL", code })}\n`); process.exitCode = 1;
});
