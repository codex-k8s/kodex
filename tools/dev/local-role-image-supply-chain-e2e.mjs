#!/usr/bin/env node

import { createHash, randomBytes, randomUUID } from "node:crypto";
import {
  boundedResponseBody,
  verifyWorkspaceAcceptance,
  verifyWorkspaceQuota,
  workspaceAcceptanceTask,
  workspaceQuotaTask,
} from "./runtime-workspace-acceptance.mjs";
import {
  chmodSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import { dirname, resolve } from "node:path";
import { createOwnerSessionClient } from "./owner-session-client.mjs";
import { exactOrigin } from "./owner-session-storage.mjs";

function fail(message) {
  throw new Error(`Kodex local RoleImage E2E failed: ${message}`);
}

const phase = process.argv[2] ?? "";
if (
  !new Set([
    "prepare",
    "launch",
    "capture-runtime",
    "verify-workspace",
    "launch-quota",
    "verify-quota",
  ]).has(phase)
) {
  fail("phase is unsupported");
}

const baseURL = new URL(
  exactOrigin(process.env.KODEX_ROLE_IMAGE_E2E_BASE_URL ?? ""),
);
if (baseURL.protocol !== "https:" || baseURL.pathname !== "/") {
  fail("base URL must be an exact HTTPS origin");
}
if (/prod(?:uction)?/i.test(baseURL.hostname))
  fail("production origin is forbidden");

const storageStatePath = resolve(
  process.env.KODEX_ROLE_IMAGE_E2E_STORAGE_STATE ?? "",
);
const statePath = resolve(process.env.KODEX_ROLE_IMAGE_E2E_STATE ?? "");
const prefix = process.env.KODEX_ROLE_IMAGE_E2E_PREFIX ?? "";
const timeoutMilliseconds = Number(
  process.env.KODEX_ROLE_IMAGE_E2E_TIMEOUT_MS ?? "1200000",
);
if (!/^[a-z0-9](?:[a-z0-9-]{2,38}[a-z0-9])$/.test(prefix))
  fail("resource prefix is invalid");
if (
  !Number.isSafeInteger(timeoutMilliseconds) ||
  timeoutMilliseconds < 60_000 ||
  timeoutMilliseconds > 1_800_000
) {
  fail("timeout is invalid");
}
const phaseDeadline = Date.now() + timeoutMilliseconds;

function requestSignal() {
  const remaining = phaseDeadline - Date.now();
  if (remaining <= 0) fail("phase deadline exceeded");
  return AbortSignal.timeout(Math.min(30_000, remaining));
}

function privateRegularFile(path, maximumBytes) {
  const info = lstatSync(path);
  if (
    !info.isFile() ||
    info.isSymbolicLink() ||
    (info.mode & 0o077) !== 0 ||
    info.size < 1 ||
    info.size > maximumBytes
  ) {
    fail(`private input file is invalid: ${path}`);
  }
}

const ownerSession = createOwnerSessionClient({
  origin: baseURL.origin,
  storagePath: storageStatePath,
});

async function request(method, path, { body, version, expectedStatus } = {}) {
  const headers = {
    Accept: "application/json",
  };
  const mutation = ["POST", "PUT", "PATCH", "DELETE"].includes(method);
  const idempotencyKey = mutation ? randomUUID() : "";
  if (mutation) {
    headers["Idempotency-Key"] = idempotencyKey;
  }
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  if (version !== undefined) headers["If-Match"] = `"${String(version)}"`;
  const requestBody = body === undefined ? undefined : JSON.stringify(body);
  for (let attempt = 1; attempt <= 3; attempt += 1) {
    let response;
    try {
      response = await ownerSession.request(path, {
        method,
        headers,
        body: requestBody,
        redirect: "error",
        signal: requestSignal(),
      });
    } catch (error) {
      if (attempt < 3) {
        await retryDelay(attempt);
        continue;
      }
      fail(
        `${method} ${path} failed before a response (${error instanceof Error ? error.name : "UNKNOWN"})`,
      );
    }
    const text = (await boundedResponseBody(response, 2 << 20)).toString(
      "utf8",
    );
    let value = {};
    if (text) {
      try {
        value = JSON.parse(text);
      } catch {
        fail(
          `non-JSON API response with status ${String(response.status)}: ${path}`,
        );
      }
    }
    if (expectedStatus === undefined || response.status === expectedStatus) {
      return value;
    }
    if ([429, 502, 503, 504].includes(response.status) && attempt < 3) {
      await retryDelay(attempt);
      continue;
    }
    const code =
      typeof value.code === "string" && /^[A-Z0-9_]{1,80}$/.test(value.code)
        ? value.code
        : "UNKNOWN";
    fail(`${method} ${path} returned ${String(response.status)} (${code})`);
  }
  fail(`${method} ${path} exhausted bounded retries`);
}

async function retryDelay(attempt) {
  await new Promise((resolve) => setTimeout(resolve, attempt * 250));
}

function boundedString(value, field) {
  if (typeof value !== "string" || value.length < 1 || value.length > 2048)
    fail(`${field} is invalid`);
  return value;
}

function exactDigest(value, field) {
  if (typeof value !== "string" || !/^sha256:[a-f0-9]{64}$/.test(value))
    fail(`${field} is invalid`);
  return value;
}

function exactSHA256(value, field) {
  if (typeof value !== "string" || !/^[a-f0-9]{64}$/.test(value))
    fail(`${field} is invalid`);
  return value;
}

function exactImage(value, field) {
  if (
    typeof value !== "string" ||
    !/^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(value)
  )
    fail(`${field} is invalid`);
  return value;
}

function writeState(value) {
  mkdirSync(dirname(statePath), { recursive: true, mode: 0o700 });
  const temporary = `${statePath}.${String(process.pid)}.tmp`;
  writeFileSync(temporary, `${JSON.stringify(value)}\n`, {
    encoding: "utf8",
    mode: 0o600,
    flag: "wx",
  });
  chmodSync(temporary, 0o600);
  renameSync(temporary, statePath);
}

async function prepare() {
  const project = await request("POST", "/api/v1/projects", {
    body: {
      name: `${prefix} RoleImage E2E`,
      purpose: "Локальная проверка полного supply-chain RoleImage",
      language: "ru",
    },
    expectedStatus: 201,
  });
  const projectRef = boundedString(project.ref, "project ref");
  const agent = await request(
    "POST",
    `/api/v1/projects/${encodeURIComponent(projectRef)}/agents`,
    {
      body: {
        name: `${prefix} Исполнитель`,
        purpose: "Проверить promoted RoleImage в runtime Pod",
        roleDescription: "Локальный E2E исполнитель RoleImage",
        initialInstructions:
          "Выполни пользовательскую задачу и кратко сообщи результат.",
      },
      expectedStatus: 201,
    },
  );
  const agentRef = boundedString(agent.ref, "agent ref");
  const roleDefinitionRef = boundedString(
    agent.roleDefinitionRef,
    "role definition ref",
  );
  if (!Number.isSafeInteger(agent.version) || agent.version < 1)
    fail("agent version is invalid");

  const catalog = await request("GET", "/api/v1/role-environments", {
    expectedStatus: 200,
  });
  const environment = Array.isArray(catalog.items)
    ? catalog.items.find(
        (item) => item?.key === "standard" && item.available === true,
      )
    : undefined;
  const dockerfile = boundedString(
    environment?.dockerfileTemplate,
    "standard Dockerfile template",
  );
  const recipe = await request(
    "POST",
    `/api/v1/projects/${encodeURIComponent(projectRef)}/role-image-recipes`,
    {
      body: {
        roleDefinitionRef,
        name: `${prefix} Standard image`,
        environment: {
          environmentKey: "standard",
          packageKeys: [],
          toolKeys: [],
          dockerfile,
        },
      },
      expectedStatus: 201,
    },
  );
  const recipeRef = boundedString(recipe.ref, "recipe ref");
  if (!Number.isSafeInteger(recipe.version) || recipe.version < 1)
    fail("recipe version is invalid");

  const deadline = Date.now() + timeoutMilliseconds;
  let detail;
  let buildRef = "";
  let promotionRequested = false;
  while (Date.now() < deadline) {
    detail = await request(
      "GET",
      `/api/v1/projects/${encodeURIComponent(projectRef)}/role-image-recipes/${encodeURIComponent(recipeRef)}`,
      { expectedStatus: 200 },
    );
    const failedBuild = Array.isArray(detail.builds)
      ? detail.builds.find((build) =>
          ["FAILED", "CANCELLED", "EXPIRED", "DEAD_LETTER"].includes(
            build?.stage,
          ),
        )
      : undefined;
    if (!buildRef && Array.isArray(detail.builds) && detail.builds.length > 0) {
      buildRef = boundedString(detail.builds[0]?.ref, "image build ref");
    }
    if (failedBuild) {
      fail(
        `RoleImage build terminated at ${String(failedBuild.stage)} (${String(failedBuild.diagnosticCode ?? failedBuild.safeErrorCode ?? "UNKNOWN")})`,
      );
    }
    if (
      !promotionRequested &&
      detail.promotionCandidate?.admissionVerdict === "ACCEPTED" &&
      detail.recipe?.promotedImageReady !== true
    ) {
      const artifactRef = boundedString(
        detail.promotionCandidate.ref,
        "admitted artifact ref",
      );
      const provenanceSHA256 = exactSHA256(
        detail.promotionCandidate.provenanceSha256,
        "admitted artifact provenance",
      );
      const recipeVersion = Number(detail.recipe?.version);
      if (!Number.isSafeInteger(recipeVersion) || recipeVersion < 1)
        fail("recipe version for promotion is invalid");
      await request(
        "POST",
        `/api/v1/projects/${encodeURIComponent(projectRef)}/role-image-recipes/${encodeURIComponent(recipeRef)}/promotions`,
        {
          body: {
            imageArtifactRef: artifactRef,
            expectedProvenanceSha256: provenanceSHA256,
          },
          version: recipeVersion,
          expectedStatus: 202,
        },
      );
      promotionRequested = true;
    }
    if (
      detail.activeArtifact?.admissionVerdict === "ACCEPTED" &&
      detail.recipe?.promotedImageReady === true
    )
      break;
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 2000));
  }
  if (!buildRef) fail("image build created with the recipe was not observed");
  if (detail?.activeArtifact?.admissionVerdict !== "ACCEPTED")
    fail("accepted promoted RoleImage was not produced before timeout");
  const artifactRef = boundedString(detail.activeArtifact.ref, "artifact ref");
  const promotedReference = exactImage(
    detail.activeArtifact.promotedReference,
    "promoted reference",
  );
  const manifestDigest = exactDigest(
    detail.activeArtifact.manifestDigest,
    "manifest digest",
  );
  if (!promotedReference.endsWith(`@${manifestDigest}`))
    fail("promoted reference and artifact digest differ");

  const runtimeEnvironment = await request(
    "POST",
    `/api/v1/projects/${encodeURIComponent(projectRef)}/runtime-environments`,
    {
      body: {
        name: `${prefix} Runtime`,
        description: "Локальная exact-digest проверка promoted RoleImage",
        imageArtifactRef: artifactRef,
        tools: [],
        values: [],
        secretBindings: [],
        policy: {
          resources: {
            cpuRequestMilli: 100,
            cpuLimitMilli: 500,
            memoryRequestMib: 128,
            memoryLimitMib: 512,
            ephemeralStorageRequestMib: 256,
            ephemeralStorageLimitMib: 1024,
          },
          volumes: [],
          networkDestinations: ["DNS", "RUNTIME_CALLBACK", "PROVIDER_PROXY"],
          kubernetesAccess: "NONE",
        },
      },
      expectedStatus: 201,
    },
  );
  const environmentRef = boundedString(
    runtimeEnvironment.ref,
    "runtime environment ref",
  );
  const bound = await request(
    "PUT",
    `/api/v1/agents/${encodeURIComponent(agentRef)}/runtime-environment-binding`,
    {
      body: { environmentRef },
      version: agent.version,
      expectedStatus: 200,
    },
  );
  if (
    bound.environment?.currentVersion?.image?.reference !== promotedReference
  ) {
    fail("agent runtime binding does not read back the promoted exact image");
  }

  writeState({
    version: 1,
    status: "prepared",
    projectRef,
    agentRef,
    recipeRef,
    buildRef,
    artifactRef,
    environmentRef,
    promotedReference,
    manifestDigest,
    preparedAt: new Date().toISOString(),
  });
}

async function launch() {
  privateRegularFile(statePath, 1 << 20);
  const state = JSON.parse(readFileSync(statePath, "utf8"));
  if (state.version !== 1 || state.status !== "prepared")
    fail("prepared E2E state is invalid");
  const projectRef = boundedString(state.projectRef, "project ref");
  const agentRef = boundedString(state.agentRef, "agent ref");
  exactImage(state.promotedReference, "promoted reference");
  exactDigest(state.manifestDigest, "manifest digest");
  const nonce = randomBytes(16).toString("hex");
  const currentAgent = await request(
    "GET",
    `/api/v1/agents/${encodeURIComponent(agentRef)}`,
    { expectedStatus: 200 },
  );
  if (currentAgent.ref !== agentRef || currentAgent.projectRef !== projectRef)
    fail("workspace agent binding is invalid");
  if (
    !currentAgent.capabilities?.some(
      (capability) => capability.key === "platform.artifact.manage",
    )
  ) {
    await request(
      "POST",
      `/api/v1/agents/${encodeURIComponent(agentRef)}/commands`,
      {
        body: {
          action: "GRANT_CAPABILITY",
          capabilityKey: "platform.artifact.manage",
        },
        version: currentAgent.version,
        expectedStatus: 200,
      },
    );
  }
  const runtime = await request(
    "GET",
    `/api/v1/agents/${encodeURIComponent(agentRef)}/runtime-configuration`,
    { expectedStatus: 200 },
  );
  const modelQuery = new URLSearchParams({
    query: runtime.configuration.model,
    providerDefinitionKey: "openai-codex",
    pageSize: "100",
  });
  const models = await request(
    "GET",
    `/api/v1/model-capabilities?${modelQuery}`,
    { expectedStatus: 200 },
  );
  const accountRef = models.items?.find(
    (model) => model.id === runtime.configuration.model && model.available,
  )?.eligibleProviderAccountRefs?.[0];
  if (
    typeof accountRef !== "string" ||
    !/^pacc_[A-Za-z0-9_-]+$/.test(accountRef)
  )
    fail("configured runtime model has no eligible provider account");
  modelQuery.set("providerAccountRef", accountRef);
  const accountModels = await request(
    "GET",
    `/api/v1/model-capabilities?${modelQuery}`,
    { expectedStatus: 200 },
  );
  const selectedModel = accountModels.items?.find(
    (model) =>
      model.id === runtime.configuration.model &&
      model.available &&
      model.eligibleProviderAccountRefs?.includes(accountRef),
  );
  if (
    !selectedModel ||
    !/^mcat_[a-f0-9]{64}$/.test(accountModels.catalogRevision) ||
    !/^[a-f0-9]{64}$/.test(accountModels.catalogDigest)
  )
    fail("exact account model catalog is unavailable");
  await request(
    "PUT",
    `/api/v1/agents/${encodeURIComponent(agentRef)}/runtime-configuration`,
    {
      body: {
        runtimeProfileRef: runtime.configuration.runtimeProfileRef,
        model: runtime.configuration.model,
        providerPolicyMode: "FIXED",
        providerAccounts: [
          {
            accountRef,
            weight: 1,
            catalogRevision: accountModels.catalogRevision,
            catalogDigest: accountModels.catalogDigest,
            providerDefinitionKey: selectedModel.providerDefinitionKey,
          },
        ],
      },
      version: runtime.agentVersion,
      expectedStatus: 200,
    },
  );
  const workspace = await request("POST", "/api/v1/runs", {
    body: {
      projectRef,
      targetRef: agentRef,
      targetType: "AGENT",
      title: `${prefix} promoted runtime readback`,
      task: workspaceAcceptanceTask(nonce),
    },
    expectedStatus: 201,
  });
  const runRef = boundedString(workspace.run?.ref, "run ref");
  writeState({
    ...state,
    status: "launched",
    runRef,
    activeRunRef: runRef,
    workspaceNonce: nonce,
    launchedAt: new Date().toISOString(),
  });
}

async function captureRuntime() {
  privateRegularFile(statePath, 1 << 20);
  const state = JSON.parse(readFileSync(statePath, "utf8"));
  if (
    state.version !== 1 ||
    !["launched", "quota-launched"].includes(state.status)
  )
    fail("launched runtime state is absent");
  const runRef = boundedString(state.activeRunRef, "active run ref");
  for (;;) {
    const run = await request(
      "GET",
      `/api/v1/runs/${encodeURIComponent(runRef)}`,
      { expectedStatus: 200 },
    );
    if (
      run.ref !== runRef ||
      run.projectRef !== state.projectRef ||
      run.target?.ref !== state.agentRef
    )
      fail("active runtime owner differs");
    if (run.state !== "QUEUED") {
      if (!["RUNNING", "SUCCEEDED", "FAILED"].includes(run.state))
        fail("active runtime state is unexpected");
      const revision = (
        await request(
          "GET",
          `/api/v1/runs/${encodeURIComponent(runRef)}/runtime-revision-diff`,
          { expectedStatus: 200 },
        )
      ).current;
      if (
        !revision ||
        revision.runRef !== runRef ||
        revision.sessionRef !== run.sessionRef ||
        revision.attempt !== run.attempt ||
        !Number.isSafeInteger(run.attempt) ||
        run.attempt < 1
      )
        fail("active runtime revision differs");
      const shortHash = (value) =>
        createHash("sha256")
          .update(boundedString(value, "runtime binding ref"))
          .digest("hex")
          .slice(0, 16);
      writeState({
        ...state,
        runtimeBinding: {
          runRef,
          revisionDigest: exactSHA256(
            revision.revisionDigest,
            "runtime revision digest",
          ),
          projectHash: shortHash(run.projectRef),
          sessionHash: shortHash(run.sessionRef),
          attempt: run.attempt,
        },
      });
      return;
    }
    if (Date.now() >= phaseDeadline) fail("runtime binding deadline exceeded");
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
}

async function launchQuota() {
  privateRegularFile(statePath, 1 << 20);
  const state = JSON.parse(readFileSync(statePath, "utf8"));
  if (
    state.version !== 1 ||
    state.status !== "workspace-passed" ||
    state.workspaceEvidence?.checks?.nativeAgentShell !== "PASS"
  )
    fail("positive workspace control is absent");
  const run = await request("POST", "/api/v1/runs", {
    expectedStatus: 201,
    body: {
      projectRef: state.projectRef,
      targetRef: state.agentRef,
      targetType: "AGENT",
      title: `${prefix} workspace quota negative`,
      task: workspaceQuotaTask(randomBytes(16).toString("hex")),
    },
  });
  const quotaRunRef = boundedString(run.run?.ref, "quota run ref");
  if (quotaRunRef === state.runRef) fail("quota run reused positive control");
  const { runtimeBinding: _binding, ...previous } = state;
  writeState({
    ...previous,
    status: "quota-launched",
    quotaRunRef,
    activeRunRef: quotaRunRef,
  });
}

async function verifyWorkspace() {
  privateRegularFile(statePath, 1 << 20);
  const state = JSON.parse(readFileSync(statePath, "utf8"));
  if (
    state.version !== 1 ||
    state.status !== "runtime-observed" ||
    !state.runtimePod?.uid
  )
    fail("runtime image readback is absent");
  const runRef = boundedString(state.runRef, "run ref");
  for (;;) {
    const run = await request(
      "GET",
      `/api/v1/runs/${encodeURIComponent(runRef)}`,
      { expectedStatus: 200 },
    );
    if (run.state === "SUCCEEDED") break;
    if (!["QUEUED", "RUNNING"].includes(run.state))
      fail("workspace run did not succeed");
    if (Date.now() >= phaseDeadline) fail("workspace run deadline exceeded");
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  const verification = {
    getJSON: (path) => request("GET", path, { expectedStatus: 200 }),
    getContent: async (path, maximumBytes) => {
      const response = await ownerSession.request(path, {
        redirect: "error",
        signal: requestSignal(),
      });
      if (response.status !== 200) fail("workspace artifact download failed");
      return boundedResponseBody(response, maximumBytes);
    },
    runRef,
    projectRef: state.projectRef,
    agentRef: state.agentRef,
    nonce: state.workspaceNonce,
  };
  let evidence;
  for (;;) {
    try {
      evidence = await verifyWorkspaceAcceptance(verification);
      break;
    } catch (error) {
      if (
        error?.code !== "ARTIFACT_SCAN_PENDING" ||
        Date.now() >= phaseDeadline
      )
        throw error;
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  }
  const { workspaceNonce: _nonce, ...safeState } = state;
  writeState({
    ...safeState,
    status: "workspace-passed",
    workspaceEvidence: evidence,
    workspaceFinishedAt: new Date().toISOString(),
  });
}

async function verifyQuota() {
  privateRegularFile(statePath, 1 << 20);
  const state = JSON.parse(readFileSync(statePath, "utf8"));
  if (state.version !== 1 || state.status !== "quota-observed")
    fail("quota runtime observation is absent");
  const runRef = boundedString(state.quotaRunRef, "quota run ref");
  for (;;) {
    const run = await request(
      "GET",
      `/api/v1/runs/${encodeURIComponent(runRef)}`,
      { expectedStatus: 200 },
    );
    if (run.state === "FAILED") break;
    if (!["QUEUED", "RUNNING"].includes(run.state))
      fail("quota run did not fail");
    if (Date.now() >= phaseDeadline) fail("quota run deadline exceeded");
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  const quotaEvidence = await verifyWorkspaceQuota({
    getJSON: (path) => request("GET", path, { expectedStatus: 200 }),
    runRef,
    projectRef: state.projectRef,
    agentRef: state.agentRef,
    observation: state.quotaObservation,
  });
  writeState({
    ...state,
    status: "passed",
    quotaEvidence,
    workspaceEvidence: { ...state.workspaceEvidence, quota: "PASS" },
    finishedAt: new Date().toISOString(),
  });
}

try {
  if (phase === "prepare") await prepare();
  else if (phase === "launch") await launch();
  else if (phase === "capture-runtime") await captureRuntime();
  else if (phase === "launch-quota") await launchQuota();
  else if (phase === "verify-quota") await verifyQuota();
  else await verifyWorkspace();
} catch (error) {
  console.error(
    error instanceof Error ? error.message : "Kodex local RoleImage E2E failed",
  );
  process.exitCode = 1;
}
