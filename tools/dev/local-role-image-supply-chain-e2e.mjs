#!/usr/bin/env node

import { randomUUID } from "node:crypto";
import {
  chmodSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import { dirname, resolve } from "node:path";

function fail(message) {
  throw new Error(`Kodex local RoleImage E2E failed: ${message}`);
}

const phase = process.argv[2] ?? "";
if (!new Set(["prepare", "launch"]).has(phase)) {
  fail("phase must be prepare or launch");
}

const baseURL = new URL(process.env.KODEX_ROLE_IMAGE_E2E_BASE_URL ?? "");
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

privateRegularFile(storageStatePath, 1 << 20);
const storageState = JSON.parse(readFileSync(storageStatePath, "utf8"));
if (!Array.isArray(storageState.cookies))
  fail("browser storage cookies are invalid");
const matchingCookies = storageState.cookies.filter((cookie) => {
  if (
    !cookie ||
    typeof cookie !== "object" ||
    typeof cookie.name !== "string" ||
    typeof cookie.value !== "string"
  )
    return false;
  const domain = String(cookie.domain ?? "").replace(/^\./, "");
  return (
    cookie.secure === true &&
    (baseURL.hostname === domain || baseURL.hostname.endsWith(`.${domain}`))
  );
});
const csrf = matchingCookies.find(
  (cookie) => cookie.name === "__Host-kodex-csrf",
)?.value;
const session = matchingCookies.find(
  (cookie) => cookie.name === "__Host-kodex-session",
)?.value;
if (
  typeof csrf !== "string" ||
  csrf.length < 43 ||
  csrf.length > 256 ||
  typeof session !== "string" ||
  session.length < 32
) {
  fail("authenticated owner storage state is incomplete");
}
const cookieHeader = matchingCookies
  .map((cookie) => `${cookie.name}=${cookie.value}`)
  .join("; ");

async function request(method, path, { body, version, expectedStatus } = {}) {
  const headers = {
    Accept: "application/json",
    Cookie: cookieHeader,
    Origin: baseURL.origin,
  };
  const idempotencyKey = body === undefined ? "" : randomUUID();
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    headers["Idempotency-Key"] = idempotencyKey;
    headers["X-CSRF-Token"] = csrf;
  }
  if (version !== undefined) headers["If-Match"] = `"${String(version)}"`;
  const requestBody = body === undefined ? undefined : JSON.stringify(body);
  for (let attempt = 1; attempt <= 3; attempt += 1) {
    let response;
    try {
      response = await fetch(new URL(path, baseURL), {
        method,
        headers,
        body: requestBody,
        redirect: "error",
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
    const text = await response.text();
    if (Buffer.byteLength(text) > 2 << 20)
      fail(`oversized API response: ${path}`);
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
    const code = typeof value.code === "string" ? value.code : "UNKNOWN";
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
  const receipt = await request(
    "POST",
    `/api/v1/projects/${encodeURIComponent(projectRef)}/role-image-recipes/${encodeURIComponent(recipeRef)}/commands`,
    {
      body: { action: "REQUEST_BUILD" },
      version: recipe.version,
      expectedStatus: 200,
    },
  );
  const buildRef = boundedString(receipt.imageBuild?.ref, "image build ref");

  const deadline = Date.now() + timeoutMilliseconds;
  let detail;
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
    if (failedBuild) {
      fail(
        `RoleImage build terminated at ${String(failedBuild.stage)} (${String(failedBuild.diagnosticCode ?? failedBuild.safeErrorCode ?? "UNKNOWN")})`,
      );
    }
    if (
      detail.activeArtifact?.admissionVerdict === "ACCEPTED" &&
      detail.recipe?.promotedImageReady === true
    )
      break;
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 2000));
  }
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
  const workspace = await request("POST", "/api/v1/runs", {
    body: {
      projectRef,
      targetRef: agentRef,
      targetType: "AGENT",
      title: `${prefix} promoted runtime readback`,
      task: "Ответь одним коротким предложением, что локальный RoleImage runtime запущен.",
    },
    expectedStatus: 201,
  });
  const runRef = boundedString(workspace.run?.ref, "run ref");
  writeState({
    ...state,
    status: "launched",
    runRef,
    launchedAt: new Date().toISOString(),
  });
}

try {
  if (phase === "prepare") await prepare();
  else await launch();
} catch (error) {
  console.error(
    error instanceof Error ? error.message : "Kodex local RoleImage E2E failed",
  );
  process.exitCode = 1;
}
