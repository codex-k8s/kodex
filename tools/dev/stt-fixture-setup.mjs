#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import { isDeepStrictEqual } from "node:util";
import {
  closeSync,
  constants,
  fstatSync,
  fsyncSync,
  openSync,
  readFileSync,
  realpathSync,
} from "node:fs";
import { dirname, isAbsolute, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createOwnerSessionClient } from "./owner-session-client.mjs";
import {
  exactOrigin,
  readAuthenticatedState,
  writeAuthenticatedState,
} from "./owner-session-storage.mjs";
import { authorizeProviderAPIKeyFixture } from "./provider-api-key-acceptance.mjs";
import { boundedResponseBody } from "./runtime-workspace-acceptance.mjs";

const failure =
  "STT fixture setup stopped; inspect private phase journal before any further action";
const requireValue = (value) => {
  if (!value) throw new Error(failure);
};
const ref = (value) =>
  typeof value === "string" && /^[A-Za-z][A-Za-z0-9_-]{1,127}$/.test(value);
const version = (value) => Number.isSafeInteger(value) && value > 0;
const digest = (value) =>
  typeof value === "string" && /^[a-f0-9]{64}$/.test(value);
const root = fileURLToPath(new URL("../../", import.meta.url));

// Только Node; credential читается лениво и никогда не включается в journal.
export function readFixtureKey(env = process.env) {
  const fromEnv = env.OPENAI_API_KEY;
  const path = env.KODEX_PROVIDER_E2E_API_KEY_FILE;
  requireValue(Boolean(fromEnv) !== Boolean(path));
  let value = fromEnv;
  if (path) {
    requireValue(isAbsolute(path) && realpathSync(path) === path);
    const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const stat = fstatSync(fd);
      requireValue(
        stat.isFile() &&
          stat.nlink === 1 &&
          stat.uid === process.getuid() &&
          (stat.mode & 0o077) === 0 &&
          stat.size >= 8 &&
          stat.size <= 16385,
      );
      value = readFileSync(fd, "utf8").replace(/\n$/, "");
    } finally {
      closeSync(fd);
    }
  }
  requireValue(
    typeof value === "string" &&
      value.length >= 8 &&
      value.length <= 16384 &&
      value.trim() === value &&
      !/[\r\n\0]/.test(value),
  );
  return value;
}

export function specificationFromCatalog(catalog, accountRef) {
  requireValue(
    typeof catalog.version === "string" &&
      catalog.version.length > 0 &&
      Number.isFinite(Date.parse(catalog.observedAt)),
  );
  const model = catalog.models?.find(
    (item) => item.model === catalog.recommendedModel,
  );
  requireValue(
    model &&
      model.streamEnabled === false &&
      model.chunkingStrategies?.includes(""),
  );
  requireValue(
    Number.isInteger(catalog.recommendedMaximumAudioBytes) &&
      catalog.recommendedMaximumAudioBytes >= 1024 &&
      catalog.recommendedMaximumAudioBytes <= 26214400,
  );
  requireValue(
    Number.isInteger(catalog.recommendedMaximumAudioDurationMilliseconds) &&
      catalog.recommendedMaximumAudioDurationMilliseconds >= 1000 &&
      catalog.recommendedMaximumAudioDurationMilliseconds <= 1800000,
  );
  return {
    enabled: true,
    providerAccountRef: accountRef,
    model: model.model,
    language: "ru",
    permissionKey: "platform.stt.use",
    parameters: {
      languages: [],
      keywords: [],
      prompt: "",
      temperature: 0,
      chunkingStrategy: "",
      stream: false,
    },
    maximumAudioBytes: catalog.recommendedMaximumAudioBytes,
    maximumAudioDurationMilliseconds:
      catalog.recommendedMaximumAudioDurationMilliseconds,
    providerTimeoutMilliseconds: 15000,
  };
}

// reserve синхронно фиксирует UNKNOWN до каждого потенциального эффекта.
// Повтор/восстановление после UNKNOWN не входит в этот entrypoint.
export async function setupSTTFixture({
  origin,
  storagePath,
  prefix,
  readKey = readFixtureKey,
  fetchAPI = fetch,
  save,
  signal = AbortSignal.timeout(120000),
}) {
  origin = exactOrigin(origin);
  requireValue(/^[a-z0-9][a-z0-9-]{2,38}[a-z0-9]$/.test(prefix));
  let apiKey;
  const journal = {
    version: 1,
    status: "NOT RUN",
    phase: "PREFLIGHT",
    operations: [],
    previousBinding: null,
  };
  let client = createOwnerSessionClient({ origin, storagePath, fetchAPI });
  const persist = () => save(structuredClone(journal));
  const reserve = (phase) => {
    const operation = {
      phase,
      idempotencyKey: randomUUID(),
      outcome: "UNKNOWN",
    };
    journal.phase = phase;
    journal.status = "UNKNOWN";
    journal.operations.push(operation);
    persist();
    return operation;
  };
  async function request(
    path,
    { body, expectedVersion, operation, missing = false, status = 200 } = {},
  ) {
    const response = await client.request(path, {
      signal,
      method: body === undefined ? "GET" : "POST",
      headers:
        body === undefined
          ? {}
          : {
              "Content-Type": "application/json",
              "Idempotency-Key": operation.idempotencyKey,
              ...(expectedVersion
                ? { "If-Match": `"${expectedVersion}"` }
                : {}),
            },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
    if (missing && response.status === 404) {
      await response.body?.cancel();
      return null;
    }
    if (
      response.status !== status ||
      response.headers.get("content-type")?.split(";")[0] !== "application/json"
    ) {
      await response.body?.cancel().catch(() => {});
      throw new Error(failure);
    }
    const value = JSON.parse(
      (await boundedResponseBody(response, 1048576)).toString("utf8"),
    );
    return value;
  }
  async function absence() {
    const current = await request("/api/v1/system-stt-configuration", {
      missing: true,
    });
    if (current) {
      requireValue(
        ref(current.configurationRef) &&
          ref(current.revisionRef) &&
          digest(current.digest),
      );
      journal.previousBinding = {
        configurationRef: current.configurationRef,
        revisionRef: current.revisionRef,
        digest: current.digest,
      };
      persist();
      throw new Error(failure);
    }
  }
  function configuration(result, expectedState) {
    const config = result.configuration;
    const revision = result.revision;
    requireValue(
      config?.kind === "SYSTEM_STT" &&
        config.managedBy === "UI" &&
        ref(config.ref) &&
        version(config.version) &&
        ref(revision?.ref) &&
        digest(revision.digest) &&
        version(revision.revision) &&
        revision.state === expectedState,
    );
    if (journal.configurationRef)
      requireValue(
        config.ref === journal.configurationRef &&
          revision.ref === journal.revisionRef &&
          revision.digest === journal.digest,
      );
    journal.configurationRef = config.ref;
    journal.revisionRef = revision.ref;
    journal.digest = revision.digest;
    journal.revision = revision.revision;
    return config.version;
  }
  async function currentVersion() {
    const history = await request(
      `/api/v1/managed-configurations/${journal.configurationRef}/revisions`,
    );
    requireValue(
      history.configuration?.ref === journal.configurationRef &&
        history.configuration.managedBy === "UI" &&
        version(history.configuration.version),
    );
    return history.configuration.version;
  }
  try {
    persist();
    const bootstrap = await request("/api/v1/bootstrap");
    requireValue(
      bootstrap.initialized === true &&
        bootstrap.platformRole === "OWNER" &&
        ref(bootstrap.currentUser?.ref),
    );
    journal.ownerRef = bootstrap.currentUser.ref;
    persist();
    await absence();
    // Любой уже существующий SYSTEM_STT требует явного решения владельца.
    const existing = await request(
      "/api/v1/managed-configurations?kind=SYSTEM_STT&pageSize=100",
    );
    requireValue(
      Array.isArray(existing.items) &&
        existing.items.length === 0 &&
        existing.total === 0 &&
        !existing.nextPageToken,
    );
    const catalog = await request("/api/v1/system-stt/model-catalog");
    specificationFromCatalog(catalog, "pacc_preflight");
    journal.catalogVersion = catalog.version;
    journal.catalogObservedAt = catalog.observedAt;
    journal.model = catalog.recommendedModel;
    apiKey = readKey();
    let operation = reserve("CREATE_ACCOUNT");
    const account = await request("/api/v1/provider-accounts", {
      body: { definitionKey: "openai-codex", name: `${prefix} STT fixture` },
      operation,
      status: 201,
    });
    requireValue(
      /^pacc_[A-Za-z0-9_-]{1,96}$/.test(account.ref) &&
        account.state === "PENDING_AUTHORIZATION" &&
        account.enabled === false,
    );
    journal.accountRef = account.ref;
    operation.outcome = "CONFIRMED";
    persist();
    operation = reserve("AUTHORIZE_ACCOUNT");
    await authorizeProviderAPIKeyFixture({
      origin,
      storagePath,
      accountRef: account.ref,
      apiKey,
      fetchAPI,
      idempotencyKey: operation.idempotencyKey,
    });
    apiKey = undefined;
    // Helper мог обновить сессию; следующий client читает сохранённое состояние.
    client = createOwnerSessionClient({ origin, storagePath, fetchAPI });
    operation.outcome = "CONFIRMED";
    persist();
    const specification = specificationFromCatalog(catalog, account.ref);
    operation = reserve("CREATE_DRAFT");
    const draft = await request(
      "/api/v1/system-stt-configurations/typed-drafts",
      {
        body: { name: `${prefix} SYSTEM_STT fixture`, specification },
        operation,
        status: 201,
      },
    );
    configuration(draft, "DRAFT");
    requireValue(
      draft.revision.contentFormat === "JSON" &&
        typeof draft.revision.content === "string" &&
        createHash("sha256").update(draft.revision.content).digest("hex") ===
          journal.digest &&
        isDeepStrictEqual(JSON.parse(draft.revision.content), {
          name: `${prefix} SYSTEM_STT fixture`,
          stt: specification,
        }),
    );
    operation.outcome = "CONFIRMED";
    persist();
    const path = `/api/v1/system-stt-configurations/${journal.configurationRef}/revisions/${journal.revisionRef}`;
    for (const [phase, suffix, state] of [
      ["VALIDATE", "validation", "VALID"],
      ["PUBLISH", "publication", "PUBLISHED"],
    ]) {
      const expectedVersion = await currentVersion();
      operation = reserve(phase);
      configuration(
        await request(`${path}/${suffix}`, {
          body: {},
          expectedVersion,
          operation,
        }),
        state,
      );
      operation.outcome = "CONFIRMED";
      persist();
    }
    const impact = await request(
      `/api/v1/managed-configurations/${journal.configurationRef}/revisions/${journal.revisionRef}/impact`,
    );
    requireValue(
      impact.configurationRef === journal.configurationRef &&
        impact.targetRevisionRef === journal.revisionRef &&
        digest(impact.digest) &&
        impact.total === 0 &&
        Array.isArray(impact.consumers) &&
        impact.consumers.length === 0 &&
        !impact.nextPageToken,
    );
    await absence();
    const expectedVersion = await currentVersion();
    operation = reserve("INITIAL_BIND");
    await request(`${path}/consumer-bindings`, {
      expectedVersion,
      operation,
      body: {
        impactDigest: impact.digest,
        consumers: [
          { kind: "STT_SERVICE", ref: "stt-tts-service", expectedAbsent: true },
        ],
      },
    });
    operation.outcome = "CONFIRMED";
    persist();
    const effective = await request("/api/v1/system-stt-configuration");
    requireValue(
      effective.configurationRef === journal.configurationRef &&
        effective.revisionRef === journal.revisionRef &&
        effective.digest === journal.digest &&
        effective.revision === journal.revision &&
        effective.providerAccountRef === account.ref &&
        effective.model === specification.model &&
        effective.ready === true &&
        effective.enabled === true &&
        Array.isArray(effective.readinessBlockers) &&
        effective.readinessBlockers.length === 0 &&
        version(effective.providerCredentialGeneration),
    );
    requireValue(
      Object.entries(specification).every(([key, value]) =>
        isDeepStrictEqual(effective[key], value),
      ),
    );
    const descriptor = await request(
      `/api/v1/provider-accounts/${account.ref}`,
    );
    requireValue(
      descriptor.ref === account.ref &&
        descriptor.enabled === true &&
        descriptor.state === "AUTHORIZED" &&
        descriptor.authorization?.method === "API_KEY" &&
        descriptor.authorization.state === "AUTHORIZED",
    );
    const availability = (await request("/api/v1/bootstrap"))
      .speechTranscription;
    requireValue(
      availability?.available === true &&
        availability.reason === "READY" &&
        Date.parse(availability.validUntil) > Date.now(),
    );
    journal.providerCredentialGeneration =
      effective.providerCredentialGeneration;
    journal.phase = "READBACK";
    journal.status = "PASS";
    persist();
    return structuredClone(journal);
  } catch {
    if (!journal.operations.some((item) => item.outcome === "UNKNOWN"))
      journal.status = "FAIL";
    try {
      persist();
    } catch {
      /* Первичный durable UNKNOWN остаётся авторитетным. */
    }
    throw new Error(failure);
  } finally {
    apiKey = undefined;
  }
}

async function main() {
  const args = process.argv.slice(2);
  requireValue(
    args.length === 2 &&
      args[0] === "--expected-sha" &&
      /^[a-f0-9]{40}$/.test(args[1]),
  );
  requireValue(
    process.env.KODEX_E2E_CONFIRM_DISPOSABLE ===
      "I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION" &&
      process.env.KODEX_PROVIDER_E2E_API_KEY === "1",
  );
  const git = (argv) =>
    execFileSync("git", argv, {
      cwd: root,
      encoding: "utf8",
      timeout: 5000,
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
  requireValue(
    git(["rev-parse", "HEAD"]) === args[1] &&
      git(["status", "--porcelain", "--untracked-files=all"]) === "",
  );
  const origin = exactOrigin(process.env.KODEX_E2E_BASE_URL);
  const storagePath = process.env.KODEX_E2E_STORAGE_STATE ?? "";
  const directory = process.env.KODEX_E2E_STATE_DIRECTORY ?? "";
  const prefix = process.env.KODEX_E2E_RESOURCE_PREFIX ?? "";
  requireValue(
    isAbsolute(directory) &&
      realpathSync(directory) === directory &&
      dirname(storagePath) === directory &&
      /^[a-z0-9][a-z0-9-]{2,38}[a-z0-9]$/.test(prefix),
  );
  readAuthenticatedState(storagePath);
  const journalPath = resolve(directory, `${prefix}-stt-setup.json`);
  try {
    readAuthenticatedState(journalPath);
    throw new Error(failure);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  // Reservation не удаляется автоматически даже после PASS: повтор требует
  // отдельного read-only разбора, а не создания второго платного аккаунта.
  const lock = openSync(
    `${journalPath}.reserved`,
    constants.O_WRONLY |
      constants.O_CREAT |
      constants.O_EXCL |
      constants.O_NOFOLLOW,
    0o600,
  );
  try {
    fsyncSync(lock);
    const dir = openSync(
      directory,
      constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW,
    );
    try {
      fsyncSync(dir);
    } finally {
      closeSync(dir);
    }
    await setupSTTFixture({
      origin,
      storagePath,
      prefix,
      save: (journal) =>
        writeAuthenticatedState(journalPath, {
          ...journal,
          sourceSHA: args[1],
          origin,
          prefix,
        }),
    });
    process.stdout.write(
      "STT fixture setup PASS; persistent resources retained; audio NOT RUN\n",
    );
  } finally {
    closeSync(lock);
  }
}
if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  main().catch(() => {
    process.stderr.write(`${failure}\n`);
    process.exitCode = 1;
  });
}
