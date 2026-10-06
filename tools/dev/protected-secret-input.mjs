import { createRequire } from "node:module";
import { createHash } from "node:crypto";
import { pathToFileURL } from "node:url";
import { createOwnerSessionClient } from "./owner-session-client.mjs";
import { selectedSessionCookies } from "./owner-session-storage.mjs";
import { readPrivateAgentSecrets } from "./private-agent-secret-reader.mjs";

const mappings = Object.freeze({
  context7: {
    key: "CONTEXT7_API_KEY",
    credentialKey: "api_key",
    adapter: "CONTEXT7",
  },
  github: {
    key: "CODEX_GITHUB_AGENT_INTEGRATION_TOKEN",
    credentialKey: "token",
    adapter: "GITHUB",
  },
});
const ownerKeys = ["KODEX_LOCAL_OWNER_USERNAME", "KODEX_LOCAL_OWNER_PASSWORD"];
const safeCodes = new Set([
  "CLI_INVALID",
  "UNSAFE_DEBUG_ENVIRONMENT",
  "SECRET_INPUT_INVALID",
  "SSO_FAILED",
  "READ_FAILED",
  "OWNER_INVALID",
  "TARGET_INVALID",
  "CATALOG_INVALID",
  "CREDENTIAL_REJECTED",
  "UNKNOWN_OUTCOME",
  "READBACK_FAILED",
]);
const maximumResponseBytes = 1 << 20;
const maximumSSOMilliseconds = 100_000;
const managedPinKeys = [
  "managedConfigurationRef",
  "managedConfigurationVersion",
  "managedRevisionRef",
  "managedRevisionDigest",
];
const sha256 = (value) =>
  createHash("sha256").update(value, "utf8").digest("hex");

function failure(code) {
  const error = new Error(code);
  error.code = code;
  return error;
}
function requireValue(condition, code) {
  if (!condition) throw failure(code);
}
function exactHTTPSOrigin(value) {
  try {
    const parsed = new URL(value);
    requireValue(
      parsed.protocol === "https:" &&
        parsed.pathname === "/" &&
        !parsed.username &&
        !parsed.password &&
        !parsed.search &&
        !parsed.hash &&
        !/prod(?:uction)?/i.test(parsed.hostname),
      "CLI_INVALID",
    );
    return parsed.origin;
  } catch {
    throw failure("CLI_INVALID");
  }
}
function opaqueRef(value) {
  return (
    typeof value === "string" && /^[A-Za-z][A-Za-z0-9_-]{7,127}$/.test(value)
  );
}
function version(value) {
  return Number.isSafeInteger(value) && value > 0;
}
function validCLI(value) {
  const pinCount = managedPinKeys.filter((key) =>
    Object.hasOwn(value, key),
  ).length;
  requireValue(
    pinCount === 0 ||
      (pinCount === managedPinKeys.length &&
        value.definitionKey === "github" &&
        opaqueRef(value.managedConfigurationRef) &&
        version(value.managedConfigurationVersion) &&
        opaqueRef(value.managedRevisionRef) &&
        /^[a-f0-9]{64}$/.test(value.managedRevisionDigest)),
    "CLI_INVALID",
  );
  requireValue(
    value.mode === "integration-credential" &&
      Object.hasOwn(mappings, value.definitionKey) &&
      value.secretKey === mappings[value.definitionKey].key &&
      opaqueRef(value.connectionRef) &&
      version(value.expectedVersion) &&
      typeof value.idempotencyKey === "string" &&
      /^[a-f0-9]{8}-[a-f0-9]{4}-[1-8][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/i.test(
        value.idempotencyKey,
      ) &&
      value.confirm === "CONFIGURE_EXACT_INTEGRATION_CREDENTIAL",
    "CLI_INVALID",
  );
  requireValue(
    exactHTTPSOrigin(value.origin) === value.origin &&
      exactHTTPSOrigin(value.identityOrigin) === value.identityOrigin,
    "CLI_INVALID",
  );
  return value;
}

export function parseCredentialCLI(args) {
  const names = new Map([
    ["--mode", "mode"],
    ["--origin", "origin"],
    ["--identity-origin", "identityOrigin"],
    ["--connection-ref", "connectionRef"],
    ["--expected-version", "expectedVersion"],
    ["--definition-key", "definitionKey"],
    ["--secret-key", "secretKey"],
    ["--idempotency-key", "idempotencyKey"],
    ["--confirm", "confirm"],
    ["--managed-configuration-ref", "managedConfigurationRef"],
    ["--managed-configuration-version", "managedConfigurationVersion"],
    ["--managed-revision-ref", "managedRevisionRef"],
    ["--managed-revision-digest", "managedRevisionDigest"],
  ]);
  const options = Object.create(null);
  requireValue(
    Array.isArray(args) &&
      [names.size - managedPinKeys.length, names.size]
        .map((size) => size * 2)
        .includes(args.length),
    "CLI_INVALID",
  );
  for (let index = 0; index < args.length; index += 2) {
    const name = names.get(args[index]);
    const value = args[index + 1];
    requireValue(
      name &&
        !Object.hasOwn(options, name) &&
        typeof value === "string" &&
        value.length <= 2048 &&
        !value.startsWith("--"),
      "CLI_INVALID",
    );
    options[name] = value;
  }
  requireValue(
    /^[1-9][0-9]{0,15}$/.test(options.expectedVersion),
    "CLI_INVALID",
  );
  options.expectedVersion = Number(options.expectedVersion);
  if (Object.hasOwn(options, "managedConfigurationVersion")) {
    requireValue(
      /^[1-9][0-9]{0,15}$/.test(options.managedConfigurationVersion),
      "CLI_INVALID",
    );
    options.managedConfigurationVersion = Number(
      options.managedConfigurationVersion,
    );
  }
  options.origin = exactHTTPSOrigin(options.origin);
  options.identityOrigin = exactHTTPSOrigin(options.identityOrigin);
  return validCLI(options);
}

export function browserEnvironment(environment = process.env) {
  requireValue(
    !["DEBUG", "PWDEBUG", "NODE_OPTIONS"].some((name) => environment[name]),
    "UNSAFE_DEBUG_ENVIRONMENT",
  );
  const allowed = [
    "PATH",
    "HOME",
    "LANG",
    "LC_ALL",
    "TZ",
    "DISPLAY",
    "WAYLAND_DISPLAY",
    "XDG_RUNTIME_DIR",
    "TMPDIR",
  ];
  return Object.fromEntries(
    allowed
      .filter((name) => typeof environment[name] === "string")
      .map((name) => [name, environment[name]]),
  );
}

export async function authenticateIsolatedOwner(
  options,
  credentials,
  dependencies = {},
) {
  let browser;
  let context;
  try {
    const environment = browserEnvironment(
      dependencies.environment ?? process.env,
    );
    const require = createRequire(
      new URL(
        "../../services/staff/control-center/package.json",
        import.meta.url,
      ),
    );
    const chromium = dependencies.chromium ?? require("playwright").chromium;
    browser = await chromium.launch({ headless: true, env: environment });
    context = await browser.newContext({
      locale: "ru-RU",
      serviceWorkers: "block",
      ignoreHTTPSErrors: false,
    });
    const origins = new Set([options.origin, options.identityOrigin]);
    await context.route("**/*", async (route) => {
      try {
        const url = new URL(route.request().url());
        if (
          url.protocol !== "https:" ||
          !origins.has(url.origin) ||
          url.username ||
          url.password
        )
          return await route.abort();
        await route.continue();
      } catch {
        await route.abort();
      }
    });
    const page = await context.newPage();
    page.setDefaultTimeout(15_000);
    await page.goto(options.origin, {
      waitUntil: "domcontentloaded",
      timeout: 30_000,
    });
    const now = dependencies.now ?? Date.now;
    const deadline = now() + maximumSSOMilliseconds;
    let submittedIdentity = false;
    let startedFrontend = false;
    while (now() < deadline) {
      const location = new URL(page.url());
      requireValue(origins.has(location.origin), "SSO_FAILED");
      if (
        location.origin === options.identityOrigin &&
        (await page.locator('input[name="password"]').isVisible())
      ) {
        requireValue(!submittedIdentity, "SSO_FAILED");
        submittedIdentity = true;
        await page
          .locator('input[name="username"]')
          .fill(credentials.KODEX_LOCAL_OWNER_USERNAME);
        await page
          .locator('input[name="password"]')
          .fill(credentials.KODEX_LOCAL_OWNER_PASSWORD);
        await page
          .locator('button[type="submit"], input[type="submit"]')
          .first()
          .click();
      } else if (
        location.origin === options.origin &&
        (await page.locator(".app-shell").isVisible())
      ) {
        requireValue(submittedIdentity, "SSO_FAILED");
        const cookies = selectedSessionCookies(
          { cookies: await context.cookies() },
          options.origin,
          now(),
        );
        return {
          storage: { cookies },
          close: async () => {
            try {
              await context.close();
            } finally {
              await browser.close();
            }
          },
        };
      } else if (
        location.origin === options.origin &&
        (await page
          .getByRole("button", { name: /^(Войти|Sign in)$/ })
          .isVisible())
      ) {
        requireValue(!startedFrontend, "SSO_FAILED");
        startedFrontend = true;
        await page.getByRole("button", { name: /^(Войти|Sign in)$/ }).click();
      }
      await page.waitForTimeout(200);
    }
    throw failure("SSO_FAILED");
  } catch {
    await context?.close().catch(() => {});
    await browser?.close().catch(() => {});
    throw failure("SSO_FAILED");
  }
}

async function safeJSON(response, status, code) {
  requireValue(
    response.status === status &&
      response.headers.get("content-type")?.split(";")[0].trim() ===
        "application/json" &&
      response.headers
        .get("cache-control")
        ?.split(",")
        .some((part) => part.trim() === "no-store"),
    code,
  );
  const reader = response.body?.getReader();
  requireValue(reader, code);
  const parts = [];
  let total = 0;
  try {
    for (;;) {
      const result = await reader.read();
      if (result.done) break;
      total += result.value.length;
      requireValue(total <= maximumResponseBytes, code);
      parts.push(Buffer.from(result.value));
    }
    return JSON.parse(Buffer.concat(parts).toString("utf8"));
  } catch {
    throw failure(code);
  } finally {
    await reader.cancel().catch(() => {});
  }
}

function requireConnection(
  connection,
  options,
  expectedVersion,
  code = "TARGET_INVALID",
) {
  requireValue(
    connection?.ref === options.connectionRef &&
      connection.definitionKey === options.definitionKey &&
      connection.version === expectedVersion &&
      version(connection.version) &&
      typeof connection.definitionVersion === "string" &&
      /^[1-9][0-9]*\.[0-9]+\.[0-9]+$/.test(connection.definitionVersion) &&
      /^[a-f0-9]{64}$/.test(connection.definitionDigest) &&
      ["NOT_CONNECTED", "TESTING", "CONNECTED", "DEGRADED"].includes(
        connection.state,
      ),
    code,
  );
}

async function readDefinition(client, options) {
  const seen = new Set();
  let cursor;
  for (let index = 0; index < 10; index++) {
    const query = new URLSearchParams({
      query: options.definitionKey,
      pageSize: "100",
    });
    if (cursor) query.set("pageToken", cursor);
    const page = await safeJSON(
      await client.request(`/api/v1/integration-definitions?${query}`),
      200,
      "CATALOG_INVALID",
    );
    requireValue(
      Array.isArray(page.items) && page.items.length <= 100,
      "CATALOG_INVALID",
    );
    const exact = page.items.filter(
      (item) => item?.key === options.definitionKey,
    );
    requireValue(exact.length <= 1, "CATALOG_INVALID");
    if (exact.length) return exact[0];
    cursor = page.nextPageToken;
    if (!cursor) break;
    requireValue(
      typeof cursor === "string" && cursor.length <= 2048 && !seen.has(cursor),
      "CATALOG_INVALID",
    );
    seen.add(cursor);
  }
  throw failure("CATALOG_INVALID");
}

// Исключение ограничено неизменённой UI-копией текущего SHIPPED GitHub.
// Authority и фактическую связь подтверждают owner history и impact, не CLI pins.
async function requireManagedCopy(client, options, connection, definition) {
  const path = `/api/v1/managed-configurations/${options.managedConfigurationRef}`;
  let cursor;
  const seen = new Set();
  let target;
  for (let index = 0; index < 10; index++) {
    const query = new URLSearchParams({ pageSize: "100" });
    if (cursor) query.set("pageToken", cursor);
    const page = await safeJSON(
      await client.request(`${path}/revisions?${query}`),
      200,
      "CATALOG_INVALID",
    );
    const config = page.configuration;
    const provenance = config?.copyProvenance;
    requireValue(
      config?.ref === options.managedConfigurationRef &&
        config.version === options.managedConfigurationVersion &&
        config.kind === "INTEGRATION_DEFINITION" &&
        config.managedBy === "UI" &&
        config.source === "control-center" &&
        !config.projectRef &&
        config.archived === false &&
        config.currentRevision?.ref === options.managedRevisionRef &&
        config.currentRevision.state === "PUBLISHED" &&
        config.currentRevision.digest === options.managedRevisionDigest &&
        provenance?.origin === "SHIPPED" &&
        provenance.sourceRef === definition.key &&
        provenance.sourceRevision === definition.definitionVersion &&
        // OCC каталога может вырасти без смены immutable package pins.
        version(provenance.sourceVersion) &&
        version(definition.version) &&
        provenance.sourceVersion <= definition.version &&
        provenance.sourceDigest === definition.digest &&
        Array.isArray(page.items) &&
        page.items.length <= 100,
      "CATALOG_INVALID",
    );
    for (const revision of page.items) {
      if (revision?.ref !== options.managedRevisionRef) continue;
      requireValue(!target, "CATALOG_INVALID");
      target = revision;
    }
    cursor = page.nextPageToken;
    if (!cursor) break;
    requireValue(
      typeof cursor === "string" &&
        cursor.length <= 2048 &&
        !seen.has(cursor) &&
        index < 9,
      "CATALOG_INVALID",
    );
    seen.add(cursor);
  }
  requireValue(
    target?.state === "PUBLISHED" &&
      target.contentFormat === "JSON" &&
      target.digest === options.managedRevisionDigest &&
      target.digest === connection.definitionDigest &&
      typeof target.content === "string" &&
      Buffer.byteLength(target.content, "utf8") <= 256 * 1024 &&
      sha256(target.content) === target.digest &&
      connection.definitionVersion === definition.definitionVersion,
    "CATALOG_INVALID",
  );
  const prefix = `{"apiVersion":"integrations.kodex.io/v1","kind":"IntegrationPackage","metadata":{"key":"github","version":"${definition.definitionVersion}","origin":`;
  requireValue(
    target.content.startsWith(`${prefix}"UI"},"spec":`) &&
      sha256(`${prefix}"SHIPPED"${target.content.slice(prefix.length + 4)}`) ===
        definition.digest,
    "CATALOG_INVALID",
  );
  requireValue(
    Array.isArray(definition.capabilities) &&
      definition.capabilities.length > 0 &&
      Array.isArray(connection.capabilities) &&
      connection.capabilities.length === definition.capabilities.length &&
      new Set(connection.capabilities.map((item) => item.key)).size ===
        connection.capabilities.length &&
      definition.capabilities.every((base) =>
        connection.capabilities.some(
          (item) =>
            item.key === base.key &&
            item.operation === base.operation &&
            item.resourceKind === base.resourceKind &&
            item.risk === base.risk &&
            typeof item.inputSchema === "string" &&
            /^[a-f0-9]{64}$/.test(item.inputSchemaSha256) &&
            sha256(item.inputSchema) === item.inputSchemaSha256 &&
            item.inputSchemaSha256 === base.inputSchemaSha256,
        ),
      ),
    "CATALOG_INVALID",
  );
  const query = new URLSearchParams({
    pageSize: "100",
    query: options.connectionRef,
  });
  const impact = await safeJSON(
    await client.request(
      `${path}/revisions/${options.managedRevisionRef}/impact?${query}`,
    ),
    200,
    "CATALOG_INVALID",
  );
  requireValue(
    impact.configurationRef === options.managedConfigurationRef &&
      impact.targetRevisionRef === options.managedRevisionRef &&
      /^[a-f0-9]{64}$/.test(impact.digest) &&
      !impact.nextPageToken &&
      impact.total === 1 &&
      Array.isArray(impact.consumers) &&
      impact.consumers.length === 1 &&
      impact.consumers[0].kind === "INTEGRATION_CONNECTION" &&
      impact.consumers[0].ref === connection.ref &&
      impact.consumers[0].revisionRef === options.managedRevisionRef &&
      version(impact.consumers[0].version),
    "CATALOG_INVALID",
  );
}

export async function configureExactIntegrationCredential(
  options,
  dependencies = {},
) {
  let owner;
  let credentials;
  let mutationAttempted = false;
  try {
    validCLI(options);
    browserEnvironment(dependencies.environment ?? process.env);
    const readSecrets = dependencies.readSecrets ?? readPrivateAgentSecrets;
    credentials = readSecrets(ownerKeys);
    owner = await (dependencies.authenticate ?? authenticateIsolatedOwner)(
      options,
      credentials,
    );
    credentials.KODEX_LOCAL_OWNER_USERNAME = "";
    credentials.KODEX_LOCAL_OWNER_PASSWORD = "";
    const client = createOwnerSessionClient({
      origin: options.origin,
      storage: owner.storage,
      ...(dependencies.fetchAPI ? { fetchAPI: dependencies.fetchAPI } : {}),
      ...(dependencies.now ? { now: dependencies.now } : {}),
    });
    const bootstrap = await safeJSON(
      await client.request("/api/v1/bootstrap"),
      200,
      "READ_FAILED",
    );
    requireValue(
      opaqueRef(bootstrap.organizationRef) &&
        opaqueRef(bootstrap.currentUser?.ref) &&
        ["OWNER", "ADMINISTRATOR"].includes(bootstrap.platformRole),
      "OWNER_INVALID",
    );
    const path = `/api/v1/integration-connections/${options.connectionRef}`;
    const connection = await safeJSON(
      await client.request(path),
      200,
      "READ_FAILED",
    );
    requireConnection(connection, options, options.expectedVersion);
    const definition = await readDefinition(client, options);
    const mapping = mappings[options.definitionKey];
    requireValue(
      definition.available === true &&
        definition.origin === "SHIPPED" &&
        definition.adapter === mapping.adapter &&
        definition.adapterOwner === "integration-gateway" &&
        definition.executionRoute === "MANAGED_MCP" &&
        definition.credentialSecretKey === mapping.credentialKey &&
        (options.managedConfigurationRef ||
          (definition.definitionVersion === connection.definitionVersion &&
            definition.digest === connection.definitionDigest)),
      "CATALOG_INVALID",
    );
    if (options.managedConfigurationRef)
      await requireManagedCopy(client, options, connection, definition);
    requireValue(
      connection.credentialsConfigured === false &&
        Array.isArray(connection.nextActions) &&
        connection.nextActions.includes("CONFIGURE_CREDENTIAL"),
      "TARGET_INVALID",
    );
    const selected = readSecrets([options.secretKey]);
    let response;
    try {
      mutationAttempted = true;
      response = await client.request(`${path}/credential`, {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
          "If-Match": `"${connection.version}"`,
          "Idempotency-Key": options.idempotencyKey,
        },
        body: JSON.stringify({ value: selected[options.secretKey] }),
      });
    } catch {
      throw failure("UNKNOWN_OUTCOME");
    } finally {
      selected[options.secretKey] = "";
    }
    if (response.status !== 200) {
      await response.body?.cancel().catch(() => {});
      throw failure(
        [400, 401, 403, 404, 409, 412, 422].includes(response.status)
          ? "CREDENTIAL_REJECTED"
          : "UNKNOWN_OUTCOME",
      );
    }
    const receipt = await safeJSON(response, 200, "READBACK_FAILED");
    requireConnection(receipt, options, receipt.version, "READBACK_FAILED");
    requireValue(
      receipt.version > connection.version &&
        receipt.credentialsConfigured === true &&
        receipt.definitionVersion === connection.definitionVersion &&
        receipt.definitionDigest === connection.definitionDigest,
      "READBACK_FAILED",
    );
    const fresh = await safeJSON(
      await client.request(path),
      200,
      "READBACK_FAILED",
    );
    requireConnection(fresh, options, receipt.version, "READBACK_FAILED");
    requireValue(
      fresh.credentialsConfigured === true &&
        fresh.definitionVersion === receipt.definitionVersion &&
        fresh.definitionDigest === receipt.definitionDigest,
      "READBACK_FAILED",
    );
    return { status: "PASS", connectionRef: fresh.ref, version: fresh.version };
  } catch (error) {
    const code = safeCodes.has(error?.code)
      ? error.code
      : error?.message === "SECRET_INPUT_INVALID"
        ? "SECRET_INPUT_INVALID"
        : mutationAttempted
          ? "READBACK_FAILED"
          : "READ_FAILED";
    return {
      status:
        code === "UNKNOWN_OUTCOME" ||
        (mutationAttempted && code === "READBACK_FAILED")
          ? "UNKNOWN"
          : "FAIL",
      code,
    };
  } finally {
    if (credentials) for (const key of ownerKeys) credentials[key] = "";
    await owner?.close().catch(() => {});
  }
}

export async function main(args, dependencies = {}) {
  let result;
  try {
    result = await configureExactIntegrationCredential(
      parseCredentialCLI(args),
      dependencies,
    );
  } catch {
    result = { status: "FAIL", code: "CLI_INVALID" };
  }
  (dependencies.output ?? ((value) => process.stdout.write(value)))(
    `${JSON.stringify(result)}\n`,
  );
  return result.status === "PASS" ? 0 : result.status === "UNKNOWN" ? 2 : 1;
}

if (
  process.argv[1] &&
  pathToFileURL(process.argv[1]).href === import.meta.url
) {
  process.exitCode = await main(process.argv.slice(2));
}
