import assert from "node:assert/strict";
import {
  chmodSync,
  linkSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { createOwnerSessionClient } from "./owner-session-client.mjs";
import {
  readAuthenticatedState,
  sessionCookieNames,
  sessionHeaders,
  writeAuthenticatedState,
} from "./owner-session-storage.mjs";

const origin = "https://control.disposable.invalid";
const initialTime = Date.parse("2026-09-06T10:00:00Z");
const storage = (version = 1) => ({
  cookies: sessionCookieNames.map((name, index) => ({
    name,
    value: index
      ? String(version).repeat(43)
      : `v1.${String(version).repeat(64)}`,
    domain: "control.disposable.invalid",
    path: "/",
    secure: true,
    httpOnly: index === 0,
    sameSite: "Strict",
    expires: -1,
  })),
  origins: [],
});
const metadata = (time = initialTime, version = 1) => ({
  generation: "11111111-1111-4111-8111-111111111111",
  version,
  sessionRevision: 7,
  renewalMode: "BACKEND_REFRESH",
  serverTime: new Date(time).toISOString(),
  accessExpiresAt: new Date(time + 300000).toISOString(),
  expiresAt: new Date(time + 1800000).toISOString(),
  absoluteExpiresAt: new Date(initialTime + 3600000).toISOString(),
  renewAfter: new Date(time + 180000).toISOString(),
});
const cookieLines = (version = 2) =>
  storage(version).cookies.map(
    (cookie) =>
      `${cookie.name}=${cookie.value}; Path=/; Max-Age=1800;${cookie.httpOnly ? " HttpOnly;" : ""} Secure; SameSite=Strict`,
  );
function json(value, cookies = []) {
  const headers = new Headers({
    "Content-Type": "application/json",
    "Cache-Control": "no-store",
  });
  for (const cookie of cookies) headers.append("Set-Cookie", cookie);
  return new Response(JSON.stringify(value), { headers });
}
function fixture(options = {}) {
  let time = initialTime;
  let version = 1;
  const calls = [];
  const fetchAPI = async (url, init) => {
    assert.equal(url.origin, origin);
    assert.equal(init.redirect, "error");
    assert.equal(new Headers(init.headers).get("authorization"), null);
    assert.equal(new Headers(init.headers).get("origin"), origin);
    assert.ok(init.signal instanceof AbortSignal);
    calls.push({
      path: url.pathname,
      method: init.method ?? "GET",
      headers: new Headers(init.headers),
    });
    if (url.pathname === "/api/v1/session") {
      if (init.method === "PUT") version++;
      return (
        options.session?.({ init, time, version }) ??
        json(
          metadata(time, version),
          init.method === "PUT" ? cookieLines(version) : [],
        )
      );
    }
    return options.business?.({ init, time, version }) ?? json({ ok: true });
  };
  return {
    calls,
    fetchAPI,
    now: () => time,
    advance: (amount) => {
      time += amount;
    },
  };
}
const close = async (response) => {
  await response.body?.cancel();
};

test("cookie snapshot после renewal изолирован от внутреннего state и origins", async () => {
  const f = fixture();
  const client = createOwnerSessionClient({ origin, storage: storage(), ...f });
  assert.throws(() => client.authenticatedCookies(), /snapshot is unavailable/);
  await close(await client.request("/api/v1/runs/run_first"));
  f.advance(181000);
  await close(await client.request("/api/v1/runs/run_next"));
  const snapshot = client.authenticatedCookies();
  assert.equal(snapshot.length, 2);
  assert.equal(snapshot[1].value, "2".repeat(43));
  snapshot[1].value = "changed-synthetic-copy";
  assert.equal(client.authenticatedCookies()[1].value, "2".repeat(43));
});

test("long polling renews once before concurrent effects and adopts fresh CSRF", async () => {
  const f = fixture();
  const client = createOwnerSessionClient({ origin, storage: storage(), ...f });
  await close(await client.request("/api/v1/runs/run_first"));
  f.advance(181000);
  await Promise.all(
    [
      client.request("/api/v1/runs/run_next"),
      client.request("/api/v1/files/file_result/content"),
    ].map(async (request) => close(await request)),
  );
  assert.deepEqual(
    f.calls.map((call) => [call.path, call.method]),
    [
      ["/api/v1/session", "GET"],
      ["/api/v1/runs/run_first", "GET"],
      ["/api/v1/session", "PUT"],
      ["/api/v1/runs/run_next", "GET"],
      ["/api/v1/files/file_result/content", "GET"],
    ],
  );
  const renewal = f.calls[2];
  assert.equal(renewal.headers.get("if-match"), null);
  assert.equal(renewal.headers.get("x-csrf-token"), "1".repeat(43));
  for (const call of f.calls.slice(3)) {
    assert.equal(call.headers.get("x-csrf-token"), "2".repeat(43));
    assert.equal(
      call.headers.get("cookie"),
      sessionHeaders(storage(2), origin, f.now()).Cookie,
    );
  }
});

test("separate phases persist private cookies and recover expired access with a single PUT", async () => {
  const directory = mkdtempSync(join(tmpdir(), "kodex-owner-session-"));
  const path = join(directory, "authenticated.json");
  try {
    writeAuthenticatedState(path, storage());
    const f = fixture();
    const first = createOwnerSessionClient({ origin, storagePath: path, ...f });
    await close(await first.request("/api/v1/runs/run_first"));
    f.advance(301000);
    const next = fixture({
      session: ({ init, time, version }) =>
        init.method === "GET"
          ? new Response(null, { status: 401 })
          : json(metadata(time, version), cookieLines(version)),
    });
    next.advance(301000);
    const second = createOwnerSessionClient({
      origin,
      storagePath: path,
      ...next,
    });
    await close(await second.request("/api/v1/runs/run_second"));
    assert.deepEqual(
      next.calls.map((call) => call.method),
      ["GET", "PUT", "GET"],
    );
    assert.equal(readAuthenticatedState(path).cookies[1].value, "2".repeat(43));
    assert.equal(statSync(path).mode & 0o777, 0o600);
    assert.equal(statSync(`${path}.session.json`).mode & 0o777, 0o600);
    const cached = readFileSync(`${path}.session.json`, "utf8");
    assert.equal(cached.includes("2".repeat(43)), false);
    assert.equal(cached.includes("refreshToken"), false);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("unobserved unauthorized session never attempts refresh or an effect", async () => {
  const f = fixture({ session: () => new Response(null, { status: 401 }) });
  const client = createOwnerSessionClient({ origin, storage: storage(), ...f });
  await assert.rejects(client.request("/api/v1/runs", { method: "POST" }));
  await assert.rejects(client.request("/api/v1/runs", { method: "POST" }));
  assert.deepEqual(
    f.calls.map((call) => call.method),
    ["GET"],
  );
});

test("lost refresh ACK, 503, 401 and invalid metadata stop all queued effects without retry", async () => {
  for (const outcome of ["lost", "503", "401", "metadata"]) {
    const f = fixture({
      session: ({ init }) => {
        if (init.method !== "PUT") return;
        if (outcome === "lost") throw new Error("Synthetic private response");
        if (outcome === "metadata")
          return json({ ...metadata(), renewalMode: "UNKNOWN" }, cookieLines());
        return new Response(null, { status: Number(outcome) });
      },
    });
    const client = createOwnerSessionClient({
      origin,
      storage: storage(),
      ...f,
    });
    await close(await client.request("/api/v1/bootstrap"));
    f.advance(181000);
    const results = await Promise.allSettled([
      client.request("/api/v1/runs", { method: "POST" }),
      client.request("/api/v1/runs", { method: "POST" }),
    ]);
    assert.ok(results.every((result) => result.status === "rejected"));
    assert.equal(f.calls.filter((call) => call.method === "PUT").length, 1);
    assert.equal(f.calls.filter((call) => call.method === "POST").length, 0);
  }
});

test("partial, ambiguous, foreign and insecure Set-Cookie pairs fail before the effect", async () => {
  for (const mutate of [
    (lines) => lines.slice(0, 1),
    (lines) => [lines[0], lines[0]],
    (lines) => [lines[0] + "; Domain=disposable.invalid", lines[1]],
    (lines) => [lines[0].replace("Path=/", "Path=/api"), lines[1]],
    (lines) => [lines[0].replace(" Secure;", ""), lines[1]],
    (lines) => [lines[0], lines[1] + "; HttpOnly"],
    (lines) => [lines[0].replace("Max-Age=1800", "Max-Age=0"), lines[1]],
    (lines) => [lines[0] + "; Max-Age=1800", lines[1]],
    (lines) => [lines[0].replace("SameSite=Strict", "SameSite=None"), lines[1]],
  ]) {
    const f = fixture({
      session: ({ init, time, version }) =>
        init.method === "PUT"
          ? json(metadata(time, version), mutate(cookieLines(version)))
          : undefined,
    });
    const client = createOwnerSessionClient({
      origin,
      storage: storage(),
      ...f,
    });
    await close(await client.request("/api/v1/bootstrap"));
    f.advance(181000);
    await assert.rejects(client.request("/api/v1/runs", { method: "POST" }));
    assert.equal(f.calls.filter((call) => call.method === "POST").length, 0);
  }
});

test("idle expiry, absolute expiry, backwards clock and reauthentication mode do not extend authority", async () => {
  for (const scenario of ["idle", "absolute", "clock", "reauthentication"]) {
    const f = fixture({
      session: ({ time, version }) => {
        const value = metadata(time, version);
        if (scenario === "reauthentication")
          value.renewalMode = "REAUTHENTICATION";
        if (scenario === "absolute") {
          value.absoluteExpiresAt = value.expiresAt = new Date(
            time + 181000,
          ).toISOString();
        }
        return json(value);
      },
    });
    const client = createOwnerSessionClient({
      origin,
      storage: storage(),
      ...f,
    });
    await close(await client.request("/api/v1/bootstrap"));
    f.advance(
      scenario === "clock" ? -1000 : scenario === "idle" ? 1800001 : 181001,
    );
    await assert.rejects(client.request("/api/v1/runs", { method: "POST" }));
    assert.equal(
      f.calls.filter((call) => call.method === "PUT" || call.method === "POST")
        .length,
      0,
    );
  }
});

test("business 401 is returned once and never causes a hidden renewal or resend", async () => {
  const f = fixture({ business: () => new Response(null, { status: 401 }) });
  const client = createOwnerSessionClient({ origin, storage: storage(), ...f });
  assert.equal(
    (await client.request("/api/v1/speech/transcriptions", { method: "POST" }))
      .status,
    401,
  );
  await assert.rejects(
    client.request("/api/v1/speech/transcriptions", { method: "POST" }),
  );
  assert.deepEqual(
    f.calls.map((call) => call.method),
    ["GET", "POST"],
  );
});

test("an expected resource permission denial does not invalidate a live session", async () => {
  let denied = false;
  const f = fixture({
    business: () => {
      if (denied) return;
      denied = true;
      return new Response(null, { status: 403 });
    },
  });
  const client = createOwnerSessionClient({ origin, storage: storage(), ...f });
  assert.equal(
    (await client.request("/api/v1/files/file_foreign")).status,
    403,
  );
  await close(await client.request("/api/v1/bootstrap"));
  assert.deepEqual(
    f.calls.map((call) => call.path),
    ["/api/v1/session", "/api/v1/files/file_foreign", "/api/v1/bootstrap"],
  );
});

test("business cookie rotation adopts metadata before the next command", async () => {
  let rotated = false;
  const f = fixture({
    session: ({ time }) => json(metadata(time, rotated ? 2 : 1)),
    business: () => {
      if (rotated) return;
      rotated = true;
      return json({ ok: true }, cookieLines(2));
    },
  });
  const client = createOwnerSessionClient({ origin, storage: storage(), ...f });
  await close(
    await client.request("/api/v1/secrets/secret_fixture/reveal", {
      method: "POST",
    }),
  );
  await close(await client.request("/api/v1/bootstrap"));
  assert.deepEqual(
    f.calls.map((call) => call.method),
    ["GET", "POST", "GET", "GET"],
  );
  assert.equal(f.calls[2].headers.get("x-csrf-token"), "2".repeat(43));
  assert.equal(f.calls[3].headers.get("x-csrf-token"), "2".repeat(43));
});

test("authenticated state rejects aliases and stale concurrent writers", async () => {
  const directory = mkdtempSync(join(tmpdir(), "kodex-owner-session-files-"));
  const path = join(directory, "authenticated.json");
  try {
    writeAuthenticatedState(path, storage());
    const f = fixture();
    const client = createOwnerSessionClient({
      origin,
      storagePath: path,
      ...f,
    });
    await close(await client.request("/api/v1/bootstrap"));
    writeAuthenticatedState(path, storage(3));
    f.advance(181000);
    await assert.rejects(client.request("/api/v1/runs", { method: "POST" }));
    assert.equal(readAuthenticatedState(path).cookies[1].value, "3".repeat(43));
    symlinkSync(path, join(directory, "alias"));
    assert.throws(() =>
      writeAuthenticatedState(join(directory, "alias"), storage()),
    );
    linkSync(path, join(directory, "hardlink"));
    assert.throws(() => writeAuthenticatedState(path, storage()));
    rmSync(join(directory, "hardlink"));
    chmodSync(path, 0o644);
    assert.throws(() => writeAuthenticatedState(path, storage()));
    chmodSync(path, 0o600);
    writeFileSync(path, "synthetic-private-invalid-json");
    assert.throws(() => readAuthenticatedState(path), {
      message: "Owner session acceptance storage JSON is invalid",
    });
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("foreign paths and supplied bearer or cookie headers fail before network", async () => {
  for (const input of [
    { path: "https://foreign.invalid/api/v1/runs" },
    { path: "/api/v1/session" },
    { path: "/api/v1/runs", headers: { Authorization: "synthetic" } },
    { path: "/api/v1/runs", headers: { Cookie: "synthetic" } },
  ]) {
    const f = fixture();
    const client = createOwnerSessionClient({
      origin,
      storage: storage(),
      ...f,
    });
    await assert.rejects(
      client.request(input.path, { headers: input.headers }),
    );
    assert.equal(f.calls.length, 0);
  }
});

test("invalid metadata and response policy stop before any business request", async () => {
  for (const mutation of [
    (value) => {
      value.version = 0;
    },
    (value) => {
      value.sessionRevision = 1.1;
    },
    (value) => {
      value.generation = [value.generation];
    },
    (value) => {
      value.renewalMode = "UNKNOWN";
    },
    (value) => {
      value.accessExpiresAt = value.serverTime;
    },
    (value) => {
      value.expiresAt = value.serverTime;
    },
    (value) => {
      value.absoluteExpiresAt = value.serverTime;
    },
    (value) => {
      value.extra = "synthetic-private-data";
    },
  ]) {
    const f = fixture({
      session: () => {
        const value = metadata();
        mutation(value);
        return json(value);
      },
    });
    const client = createOwnerSessionClient({
      origin,
      storage: storage(),
      ...f,
    });
    await assert.rejects(client.request("/api/v1/runs", { method: "POST" }));
    assert.equal(f.calls.length, 1);
  }
  for (const headers of [
    { "Content-Type": "text/html", "Cache-Control": "no-store" },
    { "Content-Type": "application/json", "Cache-Control": "public" },
    {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
      "Content-Length": "16385",
    },
  ]) {
    const f = fixture({
      session: () => new Response(JSON.stringify(metadata()), { headers }),
    });
    const client = createOwnerSessionClient({
      origin,
      storage: storage(),
      ...f,
    });
    await assert.rejects(client.request("/api/v1/runs", { method: "POST" }));
    assert.equal(f.calls.length, 1);
  }
});

test("cache cookie mismatch cannot authorize GET401 refresh and expired state cannot be reused", async () => {
  const directory = mkdtempSync(join(tmpdir(), "kodex-owner-session-cache-"));
  const path = join(directory, "authenticated.json");
  try {
    writeAuthenticatedState(path, storage());
    const f = fixture();
    const first = createOwnerSessionClient({ origin, storagePath: path, ...f });
    await close(await first.request("/api/v1/bootstrap"));
    writeAuthenticatedState(path, storage(3));
    const next = fixture({
      session: () => new Response(null, { status: 401 }),
    });
    const second = createOwnerSessionClient({
      origin,
      storagePath: path,
      ...next,
    });
    await assert.rejects(second.request("/api/v1/runs", { method: "POST" }));
    assert.deepEqual(
      next.calls.map((call) => call.method),
      ["GET"],
    );
    const expired = storage();
    expired.cookies[0].expires = initialTime / 1000;
    writeAuthenticatedState(path, expired);
    assert.throws(() =>
      createOwnerSessionClient({ origin, storagePath: path, ...f }),
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
