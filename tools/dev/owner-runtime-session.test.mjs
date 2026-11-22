import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, rmSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { test } from "node:test";
import {
  bootstrapOwnerRuntimeSession,
  createOwnerRuntimeSessionClient,
  createOwnerSessionClient,
} from "./owner-session-client.mjs";
import {
  exactOrigin,
  ownerLocalRuntimeOriginProfile,
  sessionHeaders,
} from "./owner-session-storage.mjs";
const origin = "https://production.control.disposable.invalid";
const state = {
  runtimeOrigin: origin,
  cookies: ["__Host-kodex-session", "__Host-kodex-csrf"].map((name, index) => ({
    name,
    value: index ? "1".repeat(43) : "v1." + "1".repeat(64),
    domain: "production.control.disposable.invalid",
    path: "/",
    secure: true,
    httpOnly: index === 0,
    sameSite: "Strict",
    expires: -1,
  })),
  origins: [],
};
function metadata() {
  const now = Date.now();
  return {
    generation: "11111111-1111-4111-8111-111111111111",
    version: 1,
    sessionRevision: 1,
    renewalMode: "BACKEND_REFRESH",
    serverTime: new Date(now).toISOString(),
    accessExpiresAt: new Date(now + 300000).toISOString(),
    expiresAt: new Date(now + 1800000).toISOString(),
    absoluteExpiresAt: new Date(now + 3600000).toISOString(),
    renewAfter: new Date(now + 180000).toISOString(),
  };
}
const cookies = () =>
  state.cookies.map(
    (c) =>
      `${c.name}=${c.value}; Path=/; Max-Age=1800;${c.httpOnly ? " HttpOnly;" : ""} Secure; SameSite=Strict`,
  );
function bootstrapResponse() {
  const headers = new Headers();
  for (const c of cookies()) headers.append("Set-Cookie", c);
  return new Response(null, { status: 204, headers });
}

test("runtime profile preserves exact HTTPS/host cookie boundary and acceptance defaults", async () => {
  assert.throws(() => createOwnerSessionClient({ origin, storage: state }));
  assert.throws(() => exactOrigin(origin));
  assert.throws(() => sessionHeaders(state, origin));
  assert.equal(exactOrigin(origin, ownerLocalRuntimeOriginProfile), origin);
  for (const url of [
    "http://control.invalid",
    "https://u:p@control.invalid",
    "https://control.invalid/path",
    "https://control.invalid/?q=1",
    "https://control.invalid/#f",
  ])
    assert.throws(() => exactOrigin(url, ownerLocalRuntimeOriginProfile));
  assert.throws(() => exactOrigin(origin, "ARBITRARY"));
  const calls = [];
  const client = createOwnerRuntimeSessionClient({
    origin,
    storage: state,
    fetchAPI: async (url, options) => {
      calls.push(url);
      assert.equal(new Headers(options.headers).get("origin"), origin);
      return new Response(JSON.stringify(metadata()), {
        headers: {
          "Content-Type": "application/json",
          "Cache-Control": "no-store",
        },
      });
    },
  });
  await client.observe("/api/v1/session");
  assert.ok(calls.length >= 1);
  assert.throws(() =>
    createOwnerRuntimeSessionClient({
      origin: "https://other.disposable.invalid",
      storage: state,
    }),
  );
});

test("private token-file bootstrap makes one existing session POST and retains no bearer in persisted state", async (t) => {
  const dir = mkdtempSync(join(tmpdir(), "kodex-owner-runtime-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const tokenFile = join(dir, "input.json"),
    storagePath = join(dir, "session.json"),
    marker = "synthetic-owner-access-token-".repeat(3);
  writeFileSync(tokenFile, JSON.stringify({ origin, accessToken: marker }), {
    mode: 0o600,
  });
  let calls = 0;
  await bootstrapOwnerRuntimeSession({
    origin,
    tokenFile,
    storagePath,
    fetchAPI: async (url, options) => {
      if (options.method !== "POST")
        return new Response(JSON.stringify(metadata()), {
          headers: {
            "Content-Type": "application/json",
            "Cache-Control": "no-store",
          },
        });
      calls++;
      assert.equal(url.href, origin + "/api/v1/session");
      assert.equal(options.method, "POST");
      assert.equal(options.redirect, "error");
      assert.equal(
        new Headers(options.headers).get("authorization"),
        "Bearer " + marker,
      );
      return bootstrapResponse();
    },
  });
  assert.equal(calls, 1);
  assert.ok(!readFileSync(storagePath, "utf8").includes(marker));
  assert.equal(JSON.parse(readFileSync(storagePath, "utf8")).cookies.length, 2);
  await assert.rejects(
    bootstrapOwnerRuntimeSession({
      origin,
      tokenFile,
      storagePath,
      fetchAPI: async () => {
        calls++;
        return bootstrapResponse();
      },
    }),
  );
  assert.equal(calls, 1);
});

test("lost bootstrap reply is not retried or converted into authenticated state", async (t) => {
  const dir = mkdtempSync(join(tmpdir(), "kodex-owner-runtime-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const tokenFile = join(dir, "input.json"),
    storagePath = join(dir, "session.json");
  writeFileSync(
    tokenFile,
    JSON.stringify({
      origin,
      accessToken: "synthetic-owner-access-token-".repeat(3),
    }),
    { mode: 0o600 },
  );
  let calls = 0;
  await assert.rejects(
    bootstrapOwnerRuntimeSession({
      origin,
      tokenFile,
      storagePath,
      fetchAPI: async () => {
        calls++;
        throw Error("private diagnostic");
      },
    }),
    (e) => !e.message.includes("private diagnostic"),
  );
  assert.equal(calls, 1);
  assert.throws(() => readFileSync(storagePath));
});

test("bootstrap cannot overwrite a concurrently installed owner state", async (t) => {
  const dir = mkdtempSync(join(tmpdir(), "kodex-owner-runtime-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const tokenFile = join(dir, "input.json"),
    storagePath = join(dir, "session.json");
  writeFileSync(
    tokenFile,
    JSON.stringify({
      origin,
      accessToken: "synthetic-owner-access-token-".repeat(3),
    }),
    { mode: 0o600 },
  );
  const original = JSON.stringify({ protected: "other-session" });
  await assert.rejects(
    bootstrapOwnerRuntimeSession({
      origin,
      tokenFile,
      storagePath,
      fetchAPI: async (_url, options) => {
        if (options.method !== "POST")
          return new Response(JSON.stringify(metadata()), {
            headers: {
              "Content-Type": "application/json",
              "Cache-Control": "no-store",
            },
          });
        writeFileSync(storagePath, original, { mode: 0o600 });
        return bootstrapResponse();
      },
    }),
  );
  assert.equal(readFileSync(storagePath, "utf8"), original);
});

test("private token origin mismatch refuses network before session creation", async (t) => {
  const dir = mkdtempSync(join(tmpdir(), "kodex-owner-runtime-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const tokenFile = join(dir, "input.json"),
    storagePath = join(dir, "session.json");
  writeFileSync(
    tokenFile,
    JSON.stringify({
      origin: "https://foreign.disposable.invalid",
      accessToken: "synthetic-owner-access-token-".repeat(3),
    }),
    { mode: 0o600 },
  );
  let calls = 0;
  await assert.rejects(
    bootstrapOwnerRuntimeSession({
      origin,
      tokenFile,
      storagePath,
      fetchAPI: async () => {
        calls++;
        return bootstrapResponse();
      },
    }),
  );
  assert.equal(calls, 0);
  assert.throws(() => readFileSync(storagePath));
});

test("unverified or lost authenticated readback cannot persist state or repeat the session POST", async (t) => {
  for (const status of [401, 200, "LOST"]) {
    const dir = mkdtempSync(join(tmpdir(), "kodex-owner-runtime-"));
    t.after(() => rmSync(dir, { recursive: true, force: true }));
    const tokenFile = join(dir, "input.json"),
      storagePath = join(dir, "session.json");
    writeFileSync(
      tokenFile,
      JSON.stringify({
        origin,
        accessToken: "synthetic-owner-access-token-".repeat(3),
      }),
      { mode: 0o600 },
    );
    let posts = 0,
      reads = 0;
    await assert.rejects(
      bootstrapOwnerRuntimeSession({
        origin,
        tokenFile,
        storagePath,
        fetchAPI: async (_url, options) => {
          if (options.method === "POST") {
            posts++;
            return bootstrapResponse();
          }
          reads++;
          if (status === "LOST") throw Error("private upstream diagnostics");
          return new Response("{}", {
            status,
            headers: {
              "Content-Type":
                status === 401
                  ? "application/problem+json"
                  : "application/json",
              "Cache-Control": "no-store",
            },
          });
        },
      }),
      (error) => !error.message.includes("private upstream"),
    );
    assert.equal(posts, 1);
    assert.equal(reads, 1);
    assert.throws(() => readFileSync(storagePath));
  }
});

test("runtime session state binds exact port before the first network request", async () => {
  let calls = 0;
  const fetchAPI = async () => {
    calls++;
    throw Error("unexpected network");
  };
  assert.throws(() =>
    createOwnerRuntimeSessionClient({
      origin: origin + ":8443",
      storage: state,
      fetchAPI,
    }),
  );
  const legacy = structuredClone(state);
  delete legacy.runtimeOrigin;
  assert.throws(() =>
    createOwnerRuntimeSessionClient({ origin, storage: legacy, fetchAPI }),
  );
  const forged = structuredClone(state);
  forged.runtimeOrigin = "https://foreign.disposable.invalid";
  assert.throws(() =>
    createOwnerRuntimeSessionClient({ origin, storage: forged, fetchAPI }),
  );
  assert.equal(calls, 0);
});
