import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  mkdtempSync,
  rmSync,
  readFileSync,
  writeFileSync,
  chmodSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import {
  readFixtureKey,
  setupSTTFixture,
  specificationFromCatalog,
} from "./stt-fixture-setup.mjs";
import {
  readAuthenticatedState,
  writeAuthenticatedState,
} from "./owner-session-storage.mjs";

const sentinel = "synthetic-stt-fixture-sentinel-only";
const origin = "https://control.disposable.invalid";
const configRef = "mcfg_fixture";
const revisionRef = "mrev_fixture";
const accountRef = "pacc_fixture";
const catalog = {
  version: "fixture-v1",
  observedAt: new Date().toISOString(),
  recommendedModel: "gpt-transcribe",
  recommendedMaximumAudioBytes: 10485760,
  recommendedMaximumAudioDurationMilliseconds: 120000,
  models: [
    { model: "gpt-transcribe", streamEnabled: false, chunkingStrategies: [""] },
  ],
};
const content = JSON.stringify({
  name: "stt-fixture SYSTEM_STT fixture",
  stt: specificationFromCatalog(catalog, accountRef),
});
const revisionDigest = createHash("sha256").update(content).digest("hex");
const json = (body, status = 200, headers = {}) =>
  new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
      ...headers,
    },
  });
function fixture(t, options = {}) {
  const directory = mkdtempSync(join(tmpdir(), "stt-setup-test-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const storagePath = join(directory, "owner.json");
  const state = {
    cookies: [false, true].map((csrf) => ({
      name: csrf ? "__Host-kodex-csrf" : "__Host-kodex-session",
      value: csrf ? "a".repeat(43) : `v1.${"a".repeat(64)}`,
      domain: "control.disposable.invalid",
      path: "/",
      expires: -1,
      secure: true,
      httpOnly: !csrf,
      sameSite: "Strict",
    })),
    origins: [],
  };
  writeAuthenticatedState(storagePath, state);
  const calls = [];
  const journals = [];
  let currentVersion = 1;
  let bound = false;
  let keyReads = 0;
  let sessionReads = 0;
  let cookieCharacter = "a";
  const result = (status) => ({
    configuration: {
      ref: configRef,
      version: currentVersion,
      managedBy: "UI",
      kind: "SYSTEM_STT",
    },
    revision: {
      ref: revisionRef,
      revision: 1,
      state: status,
      digest: revisionDigest,
      content,
      contentFormat: "JSON",
    },
  });
  const effective = {
    ...specificationFromCatalog(catalog, accountRef),
    configurationRef: configRef,
    revisionRef,
    revision: 1,
    digest: revisionDigest,
    ready: true,
    readinessBlockers: [],
    providerCredentialGeneration: 1,
  };
  const run = () =>
    setupSTTFixture({
      origin,
      storagePath,
      prefix: "stt-fixture",
      readKey: () => {
        keyReads++;
        return sentinel;
      },
      save: (journal) => {
        assert.ok(!JSON.stringify(journal).includes(sentinel));
        journals.push(journal);
        if (
          options.failSave === journal.phase &&
          journal.operations.at(-1)?.outcome === "UNKNOWN"
        )
          throw new Error(sentinel);
      },
      fetchAPI: async (url, init) => {
        const path = url.pathname;
        const method = init.method ?? "GET";
        const headers = new Headers(init.headers);
        calls.push({ path, method, body: init.body, headers });
        assert.equal(url.origin, origin);
        assert.equal(init.redirect, "error");
        assert.equal(headers.get("origin"), origin);
        assert.equal(headers.get("x-csrf-token"), cookieCharacter.repeat(43));
        if (path === "/api/v1/session") {
          const now = Date.now();
          if (method === "GET") sessionReads++;
          if (method === "PUT") cookieCharacter = "b";
          const response = json({
            generation: "11111111-1111-4111-8111-111111111111",
            version: cookieCharacter === "a" ? 1 : 2,
            sessionRevision: 1,
            renewalMode: "BACKEND_REFRESH",
            serverTime: new Date(now).toISOString(),
            accessExpiresAt: new Date(now + 300000).toISOString(),
            expiresAt: new Date(now + 1800000).toISOString(),
            absoluteExpiresAt: new Date(now + 3600000).toISOString(),
            renewAfter: new Date(
              now +
                (options.refresh && sessionReads === 2 && method === "GET"
                  ? -1000
                  : 180000),
            ).toISOString(),
          });
          if (method === "PUT")
            for (const cookie of state.cookies)
              response.headers.append(
                "Set-Cookie",
                `${cookie.name}=${cookie.httpOnly ? `v1.${"b".repeat(64)}` : "b".repeat(43)}; Path=/; Max-Age=1800;${cookie.httpOnly ? " HttpOnly;" : ""} Secure; SameSite=Strict`,
              );
          return response;
        }
        if (method === "POST") {
          const reserved = journals.at(-1).operations.at(-1);
          assert.equal(reserved.outcome, "UNKNOWN");
          assert.equal(headers.get("idempotency-key"), reserved.idempotencyKey);
          if (options.lostAck === reserved.phase) throw new Error(sentinel);
          if (options.conflict === reserved.phase)
            return json({ code: "VERSION_MISMATCH" }, 412);
        }
        if (path === "/api/v1/bootstrap")
          return json({
            initialized: true,
            platformRole: options.nonOwner ? "MEMBER" : "OWNER",
            currentUser: { ref: "usr_fixture" },
            speechTranscription: {
              available: !options.notReady,
              reason: options.notReady ? "STT_SERVICE_UNAVAILABLE" : "READY",
              validUntil: new Date(Date.now() + 100000).toISOString(),
            },
          });
        if (path === "/api/v1/system-stt-configuration")
          return bound || options.foreign
            ? json(
                options.foreign
                  ? { ...effective, configurationRef: "mcfg_foreign" }
                  : effective,
              )
            : json({}, 404);
        if (path === "/api/v1/managed-configurations")
          return json({
            items: options.gitOwned
              ? [{ ref: "mcfg_git", managedBy: "GIT" }]
              : [],
            total: options.gitOwned ? 1 : 0,
          });
        if (path === "/api/v1/system-stt/model-catalog") return json(catalog);
        if (path === "/api/v1/provider-accounts")
          return json(
            { ref: accountRef, state: "PENDING_AUTHORIZATION", enabled: false },
            201,
          );
        if (path === `/api/v1/provider-accounts/${accountRef}`)
          return json(
            {
              ref: accountRef,
              version: 7,
              nextActions: ["CONFIGURE_CREDENTIAL"],
              ...(bound
                ? {
                    enabled: true,
                    state: "AUTHORIZED",
                    authorization: { method: "API_KEY", state: "AUTHORIZED" },
                  }
                : {}),
            },
            200,
            { ETag: '"7"' },
          );
        if (path.endsWith("/api-key-authorization")) {
          assert.equal(headers.get("if-match"), '"7"');
          assert.deepEqual(JSON.parse(init.body), { apiKey: sentinel });
          return json({
            ref: accountRef,
            enabled: true,
            state: "AUTHORIZED",
            authorization: { method: "API_KEY", state: "AUTHORIZED" },
            externalAccountMasked: "fi***re",
          });
        }
        if (path.endsWith("/typed-drafts")) {
          assert.deepEqual(
            JSON.parse(init.body).specification,
            specificationFromCatalog(catalog, accountRef),
          );
          return json(result("DRAFT"), 201);
        }
        if (path.endsWith("/revisions"))
          return json({
            configuration: result("DRAFT").configuration,
            items: [],
            total: 0,
          });
        if (path.endsWith("/impact"))
          return json({
            configurationRef: configRef,
            targetRevisionRef: revisionRef,
            digest: "e".repeat(64),
            consumers: [],
            total: 0,
          });
        if (path.endsWith("/validation") || path.endsWith("/publication")) {
          assert.equal(headers.get("if-match"), `"${currentVersion}"`);
          currentVersion++;
          return json(
            result(path.endsWith("/validation") ? "VALID" : "PUBLISHED"),
          );
        }
        if (path.endsWith("/consumer-bindings")) {
          assert.equal(headers.get("if-match"), `"${currentVersion}"`);
          assert.deepEqual(JSON.parse(init.body), {
            impactDigest: "e".repeat(64),
            consumers: [
              {
                kind: "STT_SERVICE",
                ref: "stt-tts-service",
                expectedAbsent: true,
              },
            ],
          });
          bound = true;
          return json(result("PUBLISHED"));
        }
        throw new Error("Unexpected fixture endpoint");
      },
    });
  return { run, calls, journals, storagePath, keyReads: () => keyReads };
}

test("setup сохраняет account, typed revision и explicit first bind, без audio/browser/key evidence", async (t) => {
  const f = fixture(t);
  const result = await f.run();
  assert.equal(result.status, "PASS");
  assert.equal(result.accountRef, accountRef);
  assert.deepEqual(
    result.operations.map((op) => op.phase),
    [
      "CREATE_ACCOUNT",
      "AUTHORIZE_ACCOUNT",
      "CREATE_DRAFT",
      "VALIDATE",
      "PUBLISH",
      "INITIAL_BIND",
    ],
  );
  assert.ok(result.operations.every((op) => op.outcome === "CONFIRMED"));
  assert.equal(
    f.calls.filter((call) => call.body?.includes(sentinel)).length,
    1,
  );
  assert.ok(
    f.calls.every(
      (call) =>
        call.method !== "DELETE" &&
        !call.path.includes("transcriptions") &&
        !call.path.includes("agents"),
    ),
  );
  assert.ok(!readFileSync(f.storagePath, "utf8").includes(sentinel));
  assert.equal(readAuthenticatedState(f.storagePath).origins.length, 0);
});
test("authorization refresh сохраняет private auth state для дальнейших фаз и STT smoke", async (t) => {
  const f = fixture(t, { refresh: true });
  await f.run();
  assert.equal(
    f.calls.filter(
      (call) => call.path === "/api/v1/session" && call.method === "PUT",
    ).length,
    1,
  );
  const state = readAuthenticatedState(f.storagePath);
  assert.equal(
    state.cookies.find((cookie) => !cookie.httpOnly).value,
    "b".repeat(43),
  );
  assert.equal(f.calls.at(-1).headers.get("x-csrf-token"), "b".repeat(43));
  assert.ok(!JSON.stringify(state).includes(sentinel));
});
for (const phase of [
  "CREATE_ACCOUNT",
  "AUTHORIZE_ACCOUNT",
  "CREATE_DRAFT",
  "VALIDATE",
  "PUBLISH",
  "INITIAL_BIND",
]) {
  test(`lost ACK ${phase}: durable UNKNOWN, no retry, safe error`, async (t) => {
    const f = fixture(t, { lostAck: phase });
    await assert.rejects(
      f.run(),
      (error) => !error.message.includes(sentinel) && !error.cause,
    );
    assert.equal(f.journals.at(-1).status, "UNKNOWN");
    assert.equal(
      f.journals.at(-1).operations.filter((op) => op.phase === phase).length,
      1,
    );
  });
}
for (const variant of ["foreign", "gitOwned", "nonOwner"])
  test(`${variant}: отказ до key read и effects`, async (t) => {
    const f = fixture(t, { [variant]: true });
    await assert.rejects(f.run());
    assert.equal(f.keyReads(), 0);
    assert.equal(f.calls.filter((call) => call.method === "POST").length, 0);
  });
test("412 first bind не обновляет OCC и не повторяет mutation", async (t) => {
  const f = fixture(t, { conflict: "INITIAL_BIND" });
  await assert.rejects(f.run());
  assert.equal(
    f.calls.filter((call) => call.path.endsWith("/consumer-bindings")).length,
    1,
  );
  assert.equal(f.journals.at(-1).status, "UNKNOWN");
});
test("durable reservation failure запрещает authorization", async (t) => {
  const f = fixture(t, { failSave: "AUTHORIZE_ACCOUNT" });
  await assert.rejects(f.run());
  assert.ok(
    f.calls.every((call) => !call.path.endsWith("api-key-authorization")),
  );
});
test("readiness failure не становится PASS и не удаляет account", async (t) => {
  const f = fixture(t, { notReady: true });
  await assert.rejects(f.run());
  assert.equal(f.journals.at(-1).status, "FAIL");
  assert.ok(f.calls.every((call) => call.method !== "DELETE"));
});
test("key source: ровно один источник, private regular file, без вывода содержимого", (t) => {
  const directory = mkdtempSync(join(tmpdir(), "stt-key-test-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const path = join(directory, "sentinel");
  writeFileSync(path, `${sentinel}\n`, { mode: 0o600 });
  assert.equal(
    readFixtureKey({ KODEX_PROVIDER_E2E_API_KEY_FILE: path }),
    sentinel,
  );
  assert.throws(() =>
    readFixtureKey({
      OPENAI_API_KEY: sentinel,
      KODEX_PROVIDER_E2E_API_KEY_FILE: path,
    }),
  );
  chmodSync(path, 0o644);
  assert.throws(() =>
    readFixtureKey({ KODEX_PROVIDER_E2E_API_KEY_FILE: path }),
  );
});
