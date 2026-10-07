import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import {
  authenticateIsolatedOwner,
  browserEnvironment,
  configureExactIntegrationCredential,
  main,
  parseCredentialCLI,
} from "./protected-secret-input.mjs";

const origin = "https://control.disposable.invalid";
const identityOrigin = "https://identity.disposable.invalid";
const time = Date.parse("2026-10-04T10:00:00Z");
const token = "SYNTHETIC_PROVIDER_SECRET_SENTINEL";
const password = "SYNTHETIC_OWNER_PASSWORD_SENTINEL";
const options = () => ({
  mode: "integration-credential",
  origin,
  identityOrigin,
  connectionRef: "int_synthetic01",
  expectedVersion: 7,
  definitionKey: "context7",
  secretKey: "CONTEXT7_API_KEY",
  idempotencyKey: "11111111-1111-4111-8111-111111111111",
  confirm: "CONFIGURE_EXACT_INTEGRATION_CREDENTIAL",
});
const argumentsFor = (value = options()) =>
  Object.entries(value).flatMap(([key, item]) => [
    `--${key.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`)}`,
    String(item),
  ]);
const cookies = () =>
  ["__Host-kodex-session", "__Host-kodex-csrf"].map((name, index) => ({
    name,
    value: index ? "c".repeat(43) : `v1.${"s".repeat(64)}`,
    path: "/",
    secure: true,
    httpOnly: index === 0,
    sameSite: "Strict",
    domain: "control.disposable.invalid",
    expires: -1,
  }));
const connection = (configured = false) => ({
  ref: options().connectionRef,
  version: configured ? 8 : 7,
  definitionKey: "context7",
  definitionVersion: "1.0.0",
  definitionDigest: "a".repeat(64),
  credentialsConfigured: configured,
  state: "NOT_CONNECTED",
  nextActions: ["CONFIGURE_CREDENTIAL"],
});
const definition = () => ({
  key: "context7",
  available: true,
  origin: "SHIPPED",
  adapter: "CONTEXT7",
  adapterOwner: "integration-gateway",
  executionRoute: "MANAGED_MCP",
  credentialSecretKey: "api_key",
  definitionVersion: "1.0.0",
  digest: "a".repeat(64),
});
function json(value, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
    },
  });
}

function fixture(change = {}, selectedOptions = options()) {
  const calls = [];
  const reads = [];
  let configured = false;
  let closed = 0;
  const currentConnection = (configured) => ({
    ...connection(configured),
    definitionKey: selectedOptions.definitionKey,
    definitionVersion:
      selectedOptions.definitionKey === "github" ? "2.3.1" : "1.0.0",
  });
  const currentDefinition = () => ({
    ...definition(),
    key: selectedOptions.definitionKey,
    adapter: selectedOptions.definitionKey === "github" ? "GITHUB" : "CONTEXT7",
    credentialSecretKey:
      selectedOptions.definitionKey === "github" ? "token" : "api_key",
    definitionVersion:
      selectedOptions.definitionKey === "github" ? "2.3.1" : "1.0.0",
  });
  const deps = {
    environment: {},
    now: () => time,
    readSecrets(keys) {
      reads.push([...keys]);
      return Object.fromEntries(
        keys.map((key) => [
          key,
          key === "KODEX_LOCAL_OWNER_PASSWORD"
            ? password
            : key === "KODEX_LOCAL_OWNER_USERNAME"
              ? "synthetic-owner"
              : token,
        ]),
      );
    },
    authenticate: async () => ({
      storage: { cookies: cookies() },
      close: async () => {
        closed++;
      },
    }),
    fetchAPI: async (url, init) => {
      const headers = new Headers(init.headers);
      assert.equal(url.origin, origin);
      assert.equal(init.redirect, "error");
      assert.equal(headers.get("origin"), origin);
      assert.equal(headers.get("x-csrf-token"), "c".repeat(43));
      assert.equal(headers.get("authorization"), null);
      calls.push({
        path: url.pathname,
        method: init.method ?? "GET",
        headers,
        body: init.body,
      });
      if (url.pathname === "/api/v1/session")
        return json({
          generation: "11111111-1111-4111-8111-111111111111",
          version: 1,
          sessionRevision: 1,
          renewalMode: "BACKEND_REFRESH",
          serverTime: new Date(time).toISOString(),
          accessExpiresAt: new Date(time + 300000).toISOString(),
          expiresAt: new Date(time + 1800000).toISOString(),
          absoluteExpiresAt: new Date(time + 3600000).toISOString(),
          renewAfter: new Date(time + 180000).toISOString(),
        });
      if (url.pathname === "/api/v1/bootstrap")
        return json({
          organizationRef: "org_synthetic",
          platformRole: "OWNER",
          currentUser: { ref: "usr_synthetic" },
          ...(change.bootstrap ?? {}),
        });
      if (url.pathname.startsWith("/api/v1/managed-configurations/")) {
        assert.equal(init.method ?? "GET", "GET");
        if (change.managed) return json(change.managed(url));
        assert.fail("Unexpected managed read");
      }
      if (url.pathname === "/api/v1/integration-definitions") {
        if (change.catalog) return change.catalog(url);
        return json({
          items: [{ ...currentDefinition(), ...(change.definition ?? {}) }],
          nextPageToken: "",
        });
      }
      if (url.pathname.endsWith("/credential")) {
        assert.equal(init.method, "PUT");
        assert.equal(headers.get("if-match"), '"7"');
        assert.equal(headers.get("idempotency-key"), options().idempotencyKey);
        assert.deepEqual(JSON.parse(init.body), { value: token });
        if (change.effect) return change.effect();
        configured = true;
        return json(currentConnection(true));
      }
      if (
        url.pathname ===
        `/api/v1/integration-connections/${options().connectionRef}`
      )
        return json({
          ...currentConnection(configured),
          ...(configured ? change.readback : change.connection),
        });
      throw new Error(`Unexpected request ${token}`);
    },
  };
  return { deps, calls, reads, closed: () => closed };
}

test("CLI только закрытая credential операция, без runtime mode/owner key/URL с credential", () => {
  assert.deepEqual({ ...parseCredentialCLI(argumentsFor()) }, options());
  for (const value of [
    { mode: "runtime-secret" },
    { secretKey: "CODEX_GITHUB_AGENT_GIT_TOKEN" },
    { secretKey: "GIT_OWNER_PAT" },
    { definitionKey: "email" },
    { expectedVersion: 0 },
    { expectedVersion: "7junk" },
    { expectedVersion: "9007199254740992" },
    { confirm: "yes" },
    { idempotencyKey: "unsafe" },
    { origin: "http://control.disposable.invalid" },
    { origin: "https://user:password@control.disposable.invalid" },
    { identityOrigin: "https://identity.disposable.invalid/path" },
  ])
    assert.throws(
      () => parseCredentialCLI(argumentsFor({ ...options(), ...value })),
      /^Error: CLI_INVALID$/,
    );
  assert.throws(
    () => parseCredentialCLI([...argumentsFor(), "--secret-value", token]),
    /^Error: CLI_INVALID$/,
  );
  assert.throws(
    () =>
      parseCredentialCLI([...argumentsFor().slice(0, -2), "--origin", origin]),
    /^Error: CLI_INVALID$/,
  );
});

test("Chromium получает только несекретное окружение без inherited tokens/debug", () => {
  const safe = browserEnvironment({
    PATH: "/synthetic/bin",
    HOME: "/synthetic/user",
    CONTEXT7_API_KEY: token,
    KODEX_LOCAL_OWNER_PASSWORD: password,
    GIT_OWNER_PAT: token,
    NODE_EXTRA_CA_CERTS: "/private/ignored",
  });
  assert.deepEqual(safe, { PATH: "/synthetic/bin", HOME: "/synthetic/user" });
  for (const name of ["DEBUG", "PWDEBUG", "NODE_OPTIONS"])
    assert.throws(
      () => browserEnvironment({ [name]: "enabled" }),
      /^Error: UNSAFE_DEBUG_ENVIRONMENT$/,
    );
});

test("actual owner session client + synthetic server: ровно один PUT, exact pins/CSRF и fresh readback", async () => {
  const f = fixture();
  const result = await configureExactIntegrationCredential(options(), f.deps);
  assert.deepEqual(result, {
    status: "PASS",
    connectionRef: options().connectionRef,
    version: 8,
  });
  assert.equal(f.closed(), 1);
  assert.deepEqual(f.reads, [
    ["KODEX_LOCAL_OWNER_USERNAME", "KODEX_LOCAL_OWNER_PASSWORD"],
    ["CONTEXT7_API_KEY"],
  ]);
  assert.equal(f.calls.filter((call) => call.method === "PUT").length, 1);
  assert.equal(
    f.calls.filter((call) => call.path.endsWith("int_synthetic01")).length,
    2,
  );
  assert.ok(!JSON.stringify(result).includes(token));
});

test("GitHub использует только отдельный agent integration token, не Git/runtime/owner token", async () => {
  const selected = {
    ...options(),
    definitionKey: "github",
    secretKey: "CODEX_GITHUB_AGENT_INTEGRATION_TOKEN",
  };
  const f = fixture({}, selected);
  assert.equal(
    (await configureExactIntegrationCredential(selected, f.deps)).status,
    "PASS",
  );
  assert.deepEqual(f.reads.at(-1), ["CODEX_GITHUB_AGENT_INTEGRATION_TOKEN"]);
  assert.equal(f.calls.filter((call) => call.method === "PUT").length, 1);
});

function managedFixture(change = {}) {
  const hash = (raw) => createHash("sha256").update(raw).digest("hex");
  const packageVersion = change.packageVersion ?? "2.4.0";
  const schema =
    '{"additionalProperties":false,"properties":{"draft":{"type":"boolean"}},"type":"object"}';
  const capability = {
    key: "github.pull_request.create",
    operation: "CREATE",
    risk: "WRITE",
    resourceKind: "GITHUB_REPOSITORY",
    inputSchema: schema,
    inputSchemaSha256: hash(schema),
  };
  const raw = `{"apiVersion":"integrations.kodex.io/v1","kind":"IntegrationPackage","metadata":{"key":"github","version":"${packageVersion}","origin":"UI"},"spec":{"credential":{"secretKey":"token","kind":"TOKEN"}}}`;
  const content = change.content ?? raw;
  const digest = hash(content);
  const shippedDigest = hash(
    raw.replace('"origin":"UI"', '"origin":"SHIPPED"'),
  );
  const selected = {
    ...options(),
    definitionKey: "github",
    secretKey: "CODEX_GITHUB_AGENT_INTEGRATION_TOKEN",
    managedConfigurationRef: "mcfg_synthetic01",
    managedConfigurationVersion: 3,
    managedRevisionRef: "mrev_synthetic01",
    managedRevisionDigest: digest,
  };
  const revision = {
    ref: selected.managedRevisionRef,
    revision: 1,
    state: "PUBLISHED",
    contentFormat: "JSON",
    content,
    digest,
  };
  const config = {
    ref: selected.managedConfigurationRef,
    version: 3,
    kind: "INTEGRATION_DEFINITION",
    managedBy: "UI",
    source: "control-center",
    archived: false,
    currentRevision: revision,
    copyProvenance: {
      origin: "SHIPPED",
      sourceRef: "github",
      sourceRevision: packageVersion,
      sourceVersion: 300,
      sourceDigest: shippedDigest,
    },
  };
  const impact = {
    configurationRef: config.ref,
    targetRevisionRef: revision.ref,
    digest: "c".repeat(64),
    total: 1,
    consumers: [
      {
        kind: "INTEGRATION_CONNECTION",
        ref: selected.connectionRef,
        revisionRef: revision.ref,
        version: 1,
      },
    ],
  };
  const f = fixture(
    {
      ...change,
      connection: {
        definitionVersion: packageVersion,
        definitionDigest: digest,
        capabilities: [capability],
        ...change.connection,
      },
      readback: {
        definitionVersion: packageVersion,
        definitionDigest: digest,
        ...change.readback,
      },
      definition: {
        definitionVersion: packageVersion,
        version: 300,
        digest: shippedDigest,
        capabilities: [capability],
        ...change.definition,
      },
      managed: (url) =>
        url.pathname.endsWith("/impact")
          ? { ...impact, ...change.impact }
          : {
              configuration: { ...config, ...change.config },
              items: [
                { ...revision, ...change.revision },
                ...(change.additionalRevisions ?? []),
              ],
              ...change.history,
            },
    },
    selected,
  );
  // Fixture receipt должен сохранять bound pins, как настоящий owner.
  const fetchAPI = f.deps.fetchAPI;
  f.deps.fetchAPI = async (url, init) => {
    const response = await fetchAPI(url, init);
    if (
      url.pathname.endsWith("/credential") &&
      response.status === 200 &&
      !change.effect
    ) {
      const value = await response.json();
      return json({
        ...value,
        definitionVersion: packageVersion,
        definitionDigest: digest,
      });
    }
    return response;
  };
  return { ...f, selected, revision, config, impact, capability };
}

function forwardManagedFixture(change = {}) {
  const copied = managedFixture();
  const current = managedFixture({ packageVersion: "2.5.0" });
  const original = {
    ...copied.revision,
    ref: "mrev_original01",
    state: "SUPERSEDED",
    ...change.original,
  };
  return managedFixture({
    ...change,
    packageVersion: "2.5.0",
    config: {
      copyProvenance: copied.config.copyProvenance,
      currentRevision: { ...current.revision, revision: 3 },
      ...change.config,
    },
    revision: { revision: 3, ...change.revision },
    additionalRevisions: [original],
    ...change.fixture,
  });
}

test("exact SHIPPED UI-copy: owner history/current/content/schema + actual impact binding до одного PUT", async () => {
  const f = managedFixture();
  assert.deepEqual(
    { ...parseCredentialCLI(argumentsFor(f.selected)) },
    f.selected,
  );
  assert.equal(
    (await configureExactIntegrationCredential(f.selected, f.deps)).status,
    "PASS",
  );
  assert.equal(f.calls.filter((call) => call.method === "PUT").length, 1);
  assert.equal(
    f.calls.filter((call) =>
      call.path.startsWith("/api/v1/managed-configurations/"),
    ).length,
    2,
  );
  assert.equal(f.reads.length, 2);
});

test("copy generation300/current catalog301 допускаются только при прежнем exact immutable package2.4", async () => {
  const f = managedFixture({ definition: { version: 301 } });
  assert.equal(f.config.copyProvenance.sourceVersion, 300);
  assert.equal(f.config.copyProvenance.sourceRevision, "2.4.0");
  assert.equal(
    (await configureExactIntegrationCredential(f.selected, f.deps)).status,
    "PASS",
  );
  assert.equal(f.reads.length, 2);
  assert.equal(f.calls.filter((call) => call.method === "PUT").length, 1);
});

test("новая owner PUBLISHED revision существующей SHIPPED UI-копии проверяет исходный provenance отдельно от current package", async () => {
  const f = forwardManagedFixture({ definition: { version: 301 } });
  const result = await configureExactIntegrationCredential(f.selected, f.deps);
  assert.deepEqual(result, {
    status: "PASS",
    connectionRef: f.selected.connectionRef,
    version: 8,
  });
  assert.deepEqual(f.reads.at(-1), ["CODEX_GITHUB_AGENT_INTEGRATION_TOKEN"]);
  assert.equal(f.calls.filter((call) => call.method === "PUT").length, 1);
});

test("forward original revision читается до EOF owner history, а drift provenance между страницами запрещён", async () => {
  for (const drift of [false, true]) {
    const f = forwardManagedFixture();
    const fetchAPI = f.deps.fetchAPI;
    f.deps.fetchAPI = async (url, init) => {
      const response = await fetchAPI(url, init);
      if (!url.pathname.endsWith("/revisions")) return response;
      const history = await response.json();
      if (!url.searchParams.has("pageToken"))
        return json({
          ...history,
          items: [history.items[0]],
          nextPageToken: "older",
        });
      assert.equal(url.searchParams.get("pageToken"), "older");
      if (drift)
        history.configuration.copyProvenance.sourceDigest = "b".repeat(64);
      return json({ ...history, items: [history.items[1]], nextPageToken: "" });
    };
    assert.equal(
      (await configureExactIntegrationCredential(f.selected, f.deps)).status,
      drift ? "FAIL" : "PASS",
    );
    assert.equal(f.reads.length, drift ? 1 : 2);
    assert.equal(
      f.calls.filter((call) => call.method === "PUT").length,
      drift ? 0 : 1,
    );
  }
});

test("forward UI publication: forged origin/history, rollback, arbitrary package, catalog schema и actual bindings закрыты до key/PUT", async () => {
  const initial = managedFixture();
  const current = managedFixture({ packageVersion: "2.5.0" });
  const original = {
    ...initial.revision,
    ref: "mrev_original01",
    state: "SUPERSEDED",
  };
  const cases = [
    { original: { state: "DRAFT" } },
    { original: { ref: current.revision.ref } },
    { original: { revision: 2 } },
    { original: { contentFormat: "YAML" } },
    { original: { digest: "b".repeat(64) } },
    { original: { content: `${original.content} ` } },
    { fixture: { additionalRevisions: [] } },
    { fixture: { additionalRevisions: [original, original] } },
    { revision: { revision: 1 } },
    { revision: { revision: "3" } },
    { revision: { state: "VALID" } },
    { config: { version: 4 } },
    { config: { projectRef: "prj_foreign001" } },
    { config: { currentRevision: { ...current.revision, revision: 2 } } },
    {
      config: {
        currentRevision: { ...current.revision, ref: "mrev_foreign01" },
      },
    },
    ...["2.6.0", "3.0.0", "2.5.0", "bad", ""].map((sourceRevision) => ({
      config: {
        copyProvenance: { ...initial.config.copyProvenance, sourceRevision },
      },
    })),
    ...[
      { origin: "UI" },
      { sourceRef: "foreign" },
      { sourceVersion: 302 },
      { sourceDigest: "b".repeat(64) },
    ].map((patch) => ({
      definition: { version: 301 },
      config: {
        copyProvenance: { ...initial.config.copyProvenance, ...patch },
      },
    })),
    {
      content: current.revision.content.replace(
        '"secretKey":"token"',
        '"secretKey":"foreign"',
      ),
    },
    { definition: { digest: "b".repeat(64) } },
    { connection: { definitionVersion: "2.4.0" } },
    { connection: { definitionDigest: initial.revision.digest } },
    {
      definition: {
        capabilities: [{ ...current.capability, inputSchema: "{}" }],
      },
    },
    {
      definition: {
        capabilities: [current.capability, current.capability],
      },
    },
    {
      connection: {
        capabilities: [{ ...current.capability, inputSchema: "{}" }],
      },
    },
    {
      impact: {
        consumers: [
          { ...current.impact.consumers[0], revisionRef: original.ref },
        ],
      },
    },
    { impact: { consumers: [], total: 0 } },
  ];
  for (const change of cases) {
    const f = forwardManagedFixture(change);
    assert.deepEqual(
      await configureExactIntegrationCredential(f.selected, f.deps),
      { status: "FAIL", code: "CATALOG_INVALID", phase: "CATALOG" },
    );
    assert.equal(f.reads.length, 1);
    assert.equal(f.calls.filter((call) => call.method === "PUT").length, 0);
    assert.equal(f.closed(), 1);
  }
});

test("catalog generation не подменяет immutable pins: future/unsafe/changed package закрыты до key и PUT", async () => {
  const baseline = managedFixture();
  const cases = [
    ...[0, -1, 300.5, "301", Number.MAX_SAFE_INTEGER + 1].map((version) => ({
      definition: { version },
    })),
    ...[0, -1, 300.5, "300", Number.MAX_SAFE_INTEGER + 1, 302].map(
      (sourceVersion) => ({
        definition: { version: 301 },
        config: {
          copyProvenance: { ...baseline.config.copyProvenance, sourceVersion },
        },
      }),
    ),
    { definition: { version: 301, definitionVersion: "2.5.0" } },
    { definition: { version: 301, digest: "b".repeat(64) } },
    {
      definition: { version: 301 },
      config: {
        copyProvenance: {
          ...baseline.config.copyProvenance,
          sourceRef: "foreign",
        },
      },
    },
    {
      definition: { version: 301 },
      content: baseline.revision.content.replace(
        '"secretKey":"token"',
        '"secretKey":"foreign"',
      ),
    },
    {
      definition: { version: 301 },
      connection: { definitionVersion: "2.3.1" },
    },
    {
      definition: { version: 301 },
      connection: { definitionDigest: "b".repeat(64) },
    },
    {
      definition: { version: 301 },
      connection: {
        capabilities: [{ ...baseline.capability, inputSchema: "{}" }],
      },
    },
  ];
  for (const change of cases) {
    const f = managedFixture(change);
    assert.equal(
      (await configureExactIntegrationCredential(f.selected, f.deps)).status,
      "FAIL",
    );
    assert.equal(f.reads.length, 1);
    assert.equal(f.calls.filter((call) => call.method === "PUT").length, 0);
  }
});

test("managed pins all-or-none, exact GitHub only; неизвестные CLI aliases запрещены", () => {
  const f = managedFixture();
  for (const key of [
    "managedConfigurationRef",
    "managedConfigurationVersion",
    "managedRevisionRef",
    "managedRevisionDigest",
  ]) {
    const value = { ...f.selected };
    delete value[key];
    assert.throws(
      () => parseCredentialCLI(argumentsFor(value)),
      /^Error: CLI_INVALID$/,
    );
  }
  for (const patch of [
    { managedConfigurationVersion: 0 },
    { managedConfigurationVersion: "9007199254740992" },
    { managedRevisionDigest: "b" },
    { definitionKey: "context7", secretKey: "CONTEXT7_API_KEY" },
  ])
    assert.throws(
      () => parseCredentialCLI(argumentsFor({ ...f.selected, ...patch })),
      /^Error: CLI_INVALID$/,
    );
});

test("managed mismatch/unpublished/custom/source/schema/foreign/no binding закрыты до provider key и PUT", async () => {
  const baseline = managedFixture();
  for (const change of [
    { config: { ref: "mcfg_foreign001" } },
    { config: { version: 4 } },
    { config: { managedBy: "GIT" } },
    { config: { projectRef: "prj_foreign001" } },
    { config: { archived: true } },
    { config: { source: "foreign" } },
    {
      config: {
        currentRevision: { ...baseline.revision, ref: "mrev_foreign001" },
      },
    },
    {
      config: {
        copyProvenance: {
          ...baseline.config.copyProvenance,
          sourceDigest: "b".repeat(64),
        },
      },
    },
    { revision: { state: "VALID" } },
    { revision: { contentFormat: "YAML" } },
    { revision: { digest: "b".repeat(64) } },
    { revision: { content: `${baseline.revision.content} ` } },
    {
      content: baseline.revision.content.replace(
        '"secretKey":"token"',
        '"secretKey":"foreign"',
      ),
    },
    {
      content: baseline.revision.content.replace(
        '"origin":"UI"',
        '"origin":"GIT"',
      ),
    },
    { definition: { digest: "b".repeat(64) } },
    {
      connection: {
        capabilities: [{ ...baseline.capability, inputSchema: "{}" }],
      },
    },
    {
      connection: { capabilities: [baseline.capability, baseline.capability] },
    },
    { impact: { configurationRef: "mcfg_foreign001" } },
    { impact: { targetRevisionRef: "mrev_foreign001" } },
    {
      impact: {
        consumers: [{ ...baseline.impact.consumers[0], ref: "int_foreign001" }],
      },
    },
    {
      impact: {
        consumers: [
          { ...baseline.impact.consumers[0], revisionRef: "mrev_foreign001" },
        ],
      },
    },
    { impact: { consumers: [], total: 0 } },
    { impact: { nextPageToken: "more" } },
    { history: { nextPageToken: "repeat" } },
    { history: { items: [baseline.revision, baseline.revision] } },
  ]) {
    const f = managedFixture(change);
    assert.equal(
      (await configureExactIntegrationCredential(f.selected, f.deps)).status,
      "FAIL",
    );
    assert.equal(f.reads.length, 1);
    assert.equal(f.calls.filter((call) => call.method === "PUT").length, 0);
    assert.equal(f.closed(), 1);
  }
});

test("managed unknown outcome не повторяет PUT; stdout не содержит provider/package content", async () => {
  const f = managedFixture({
    effect: () => {
      throw new Error(token);
    },
  });
  let output = "";
  assert.equal(
    await main(argumentsFor(f.selected), {
      ...f.deps,
      output: (value) => {
        output += value;
      },
    }),
    2,
  );
  assert.deepEqual(JSON.parse(output), {
    status: "UNKNOWN",
    code: "UNKNOWN_OUTCOME",
    phase: "CREDENTIAL",
  });
  assert.equal(f.calls.filter((call) => call.method === "PUT").length, 1);
  assert.ok(
    !output.includes(token) &&
      !output.includes(password) &&
      !output.includes("IntegrationPackage"),
  );
});

test("owner/connection/catalog mismatch отклоняется до чтения provider key и PUT", async () => {
  for (const change of [
    { bootstrap: { platformRole: "MEMBER" } },
    { bootstrap: { organizationRef: "" } },
    { connection: { ref: "int_foreign01" } },
    { connection: { version: 8 } },
    { connection: { state: "DELETED" } },
    { connection: { credentialsConfigured: true } },
    { connection: { nextActions: [] } },
    { definition: { digest: "b".repeat(64) } },
    { definition: { definitionVersion: "2.0.0" } },
    { definition: { credentialSecretKey: "token" } },
    { definition: { origin: "USER" } },
    { definition: { available: false } },
    { definition: { adapter: "GITHUB" } },
    { definition: { executionRoute: "ARBITRARY" } },
  ]) {
    const f = fixture(change);
    assert.equal(
      (await configureExactIntegrationCredential(options(), f.deps)).status,
      "FAIL",
    );
    assert.equal(f.calls.filter((call) => call.method === "PUT").length, 0);
    assert.equal(f.reads.length, 1);
    assert.equal(f.closed(), 1);
  }
});

test("catalog bounded cursor: repeated и duplicate exact definition закрыто отклоняются", async () => {
  for (const catalog of [
    () => json({ items: [], nextPageToken: "repeated" }),
    () => json({ items: [definition(), definition()], nextPageToken: "" }),
  ]) {
    const f = fixture({ catalog });
    assert.equal(
      (await configureExactIntegrationCredential(options(), f.deps)).status,
      "FAIL",
    );
    assert.ok(
      f.calls.filter((call) => call.path === "/api/v1/integration-definitions")
        .length <= 2,
    );
    assert.equal(f.reads.length, 1);
  }
});

test("rejected/unknown mutation не повторяется и не продолжает readback", async () => {
  for (const [effect, expected] of [
    [
      () => json({ detail: token }, 403),
      { status: "FAIL", code: "CREDENTIAL_REJECTED", phase: "CREDENTIAL" },
    ],
    [
      () => json({ detail: token }, 409),
      { status: "FAIL", code: "CREDENTIAL_REJECTED", phase: "CREDENTIAL" },
    ],
    [
      () => json({ detail: token }, 503),
      { status: "UNKNOWN", code: "UNKNOWN_OUTCOME", phase: "CREDENTIAL" },
    ],
    [
      () => {
        throw new Error(`Lost ACK ${token}`);
      },
      { status: "UNKNOWN", code: "UNKNOWN_OUTCOME", phase: "CREDENTIAL" },
    ],
  ]) {
    const f = fixture({ effect });
    assert.deepEqual(
      await configureExactIntegrationCredential(options(), f.deps),
      expected,
    );
    assert.equal(f.calls.filter((call) => call.method === "PUT").length, 1);
    assert.ok(f.calls.at(-1).path.endsWith("/credential"));
    assert.equal(f.closed(), 1);
  }
});

test("после известного effect повреждённый readback не называется PASS", async () => {
  for (const readback of [
    { ref: "int_foreign01" },
    { version: 9 },
    { credentialsConfigured: false },
    { definitionDigest: "b".repeat(64) },
  ]) {
    const f = fixture({ readback });
    const result = await configureExactIntegrationCredential(options(), f.deps);
    assert.equal(result.status, "UNKNOWN");
    assert.equal(f.calls.filter((call) => call.method === "PUT").length, 1);
  }
});

test("main stdout только закрытые коды, без exception/stack/password/token", async () => {
  let output = "";
  const f = fixture({
    effect: () => {
      throw new Error(`${token} ${password}`);
    },
  });
  const exit = await main(argumentsFor(), {
    ...f.deps,
    output: (value) => {
      output += value;
    },
  });
  assert.equal(exit, 2);
  assert.deepEqual(JSON.parse(output), {
    status: "UNKNOWN",
    code: "UNKNOWN_OUTCOME",
    phase: "CREDENTIAL",
  });
  assert.ok(
    !output.includes(token) &&
      !output.includes(password) &&
      !output.includes("stack"),
  );
  output = "";
  assert.equal(
    await main(["--unknown", token], {
      output: (value) => {
        output += value;
      },
    }),
    1,
  );
  assert.equal(
    output,
    '{"status":"FAIL","code":"CLI_INVALID","phase":"PREFLIGHT"}\n',
  );
});

test("закрытый этап различает SSO, session bootstrap, connection, catalog, credential и readback", async () => {
  const cases = [
    {
      phase: "SSO",
      code: "READ_FAILED",
      inject: (f) => {
        f.deps.authenticate = async () => {
          throw Object.assign(new Error(token), {
            phase: password,
            code: token,
          });
        };
      },
    },
    { phase: "BOOTSTRAP", code: "READ_FAILED", path: "/api/v1/session" },
    { phase: "BOOTSTRAP", code: "READ_FAILED", path: "/api/v1/bootstrap" },
    {
      phase: "BOOTSTRAP",
      code: "OWNER_INVALID",
      change: { bootstrap: { platformRole: "MEMBER" } },
    },
    {
      phase: "CONNECTION",
      code: "READ_FAILED",
      path: "/api/v1/integration-connections/int_synthetic01",
    },
    {
      phase: "CONNECTION",
      code: "TARGET_INVALID",
      change: { connection: { version: 9 } },
    },
    {
      phase: "CATALOG",
      code: "CATALOG_INVALID",
      change: { definition: { available: false } },
    },
    {
      phase: "CATALOG",
      code: "READ_FAILED",
      path: "/api/v1/integration-definitions",
    },
    {
      phase: "CREDENTIAL",
      code: "SECRET_INPUT_INVALID",
      inject: (f) => {
        const read = f.deps.readSecrets;
        f.deps.readSecrets = (keys) => {
          if (keys.length === 1) throw new Error("SECRET_INPUT_INVALID");
          return read(keys);
        };
      },
    },
    {
      phase: "READBACK",
      code: "READBACK_FAILED",
      change: { readback: { credentialsConfigured: false } },
      status: "UNKNOWN",
    },
  ];
  for (const item of cases) {
    const f = fixture(item.change);
    if (item.path) {
      const fetch = f.deps.fetchAPI;
      f.deps.fetchAPI = async (url, init) => {
        if (url.pathname === item.path)
          throw Object.assign(new Error(`${token} ${password}`), {
            phase: token,
          });
        return fetch(url, init);
      };
    }
    item.inject?.(f);
    let output = "";
    const exit = await main(argumentsFor(), {
      ...f.deps,
      output: (value) => {
        output += value;
      },
    });
    assert.equal(exit, item.status === "UNKNOWN" ? 2 : 1);
    assert.deepEqual(JSON.parse(output), {
      status: item.status ?? "FAIL",
      code: item.code,
      phase: item.phase,
    });
    assert.equal(
      f.calls.filter((call) => call.method === "PUT").length,
      item.status === "UNKNOWN" ? 1 : 0,
    );
    assert.ok(
      !output.includes(token) &&
        !output.includes(password) &&
        !output.includes("stack"),
    );
  }
});

function browserFixture({
  foreign = false,
  repeatedIdentity = false,
  rawFailure = false,
} = {}) {
  let stage = 0;
  let closeCount = 0;
  let routeHandler;
  let launch;
  let contextOptions;
  const fills = [];
  const page = {
    setDefaultTimeout() {},
    goto: async () => {},
    url: () =>
      foreign
        ? "https://foreign.invalid"
        : repeatedIdentity
          ? identityOrigin
          : stage
            ? origin
            : identityOrigin,
    locator(selector) {
      return {
        isVisible: async () =>
          selector === ".app-shell"
            ? stage === 1 && !repeatedIdentity
            : selector.includes('name="password"')
              ? stage === 0 || repeatedIdentity
              : false,
        fill: async (value) => {
          if (rawFailure) throw new Error(value);
          fills.push({ selector, value });
        },
        first() {
          return this;
        },
        click: async () => {
          stage = 1;
        },
      };
    },
    getByRole: () => ({ isVisible: async () => false }),
    waitForTimeout: async () => {},
  };
  const context = {
    route: async (_pattern, handler) => {
      routeHandler = handler;
    },
    newPage: async () => page,
    cookies: async () => cookies(),
    close: async () => {
      closeCount++;
    },
  };
  const chromium = {
    launch: async (value) => {
      launch = value;
      return {
        newContext: async (value) => {
          contextOptions = value;
          return context;
        },
        close: async () => {
          closeCount++;
        },
      };
    },
  };
  return {
    deps: {
      chromium,
      environment: { PATH: "/synthetic", CONTEXT7_API_KEY: token },
      now: () => time,
    },
    fills,
    closed: () => closeCount,
    launch: () => launch,
    contextOptions: () => contextOptions,
    route: () => routeHandler,
  };
}

test("synthetic isolated SSO: exact IdP, once submission, no artifacts/child secret env", async () => {
  const f = browserFixture();
  const owner = await authenticateIsolatedOwner(
    options(),
    {
      KODEX_LOCAL_OWNER_USERNAME: "synthetic-owner",
      KODEX_LOCAL_OWNER_PASSWORD: password,
    },
    f.deps,
  );
  assert.equal(f.fills.length, 2);
  assert.deepEqual(f.launch(), { headless: true, env: { PATH: "/synthetic" } });
  assert.deepEqual(f.contextOptions(), {
    locale: "ru-RU",
    serviceWorkers: "block",
    ignoreHTTPSErrors: false,
  });
  let aborted = false;
  await f.route()({
    request: () => ({ url: () => "https://foreign.invalid/path" }),
    abort: async () => {
      aborted = true;
    },
    continue: async () => assert.fail("foreign request"),
  });
  assert.equal(aborted, true);
  await owner.close();
  assert.equal(f.closed(), 2);
});

test("SSO foreign redirect, повтор login или raw fill error не раскрывают password и закрывают свой браузер", async () => {
  for (const options of [
    { foreign: true },
    { rawFailure: true },
    { repeatedIdentity: true },
  ]) {
    const f = browserFixture(options);
    await assert.rejects(
      authenticateIsolatedOwner(
        { origin, identityOrigin },
        {
          KODEX_LOCAL_OWNER_USERNAME: "synthetic-owner",
          KODEX_LOCAL_OWNER_PASSWORD: password,
        },
        f.deps,
      ),
      /^Error: SSO_FAILED$/,
    );
    assert.equal(f.closed(), 2);
  }
});
