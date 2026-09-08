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
  proxyCookieName,
  replaceAuthenticatedCookies,
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

test("observation cannot perform effects, foreign requests or arbitrary same-origin reads", async () => {
  let calls = 0;
  const client = createOwnerSessionClient({ origin, storage: storage(), now: () => initialTime,
    fetchAPI: async () => { calls++; throw new Error("Unexpected network"); } });
  for (const [path, options] of [["/api/v1/session", { method: "PUT" }], ["/api/v1/bootstrap", {}],
    ["/?q=private", {}], ["https://foreign.invalid/", {}], ["/", { headers: { cookie: "synthetic" } }]]) {
    await assert.rejects(client.observe(path, options));
  }
  assert.equal(calls, 0);
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

const proxyCookies = (count = 2, version = 1, time = initialTime) =>
  Array.from({ length: count }, (_, index) => ({
    name: count === 1 ? proxyCookieName : `${proxyCookieName}_${index}`,
    value: `synthetic-proxy-${version}-${index}|signed=`,
    domain: "control.disposable.invalid",
    path: "/",
    secure: true,
    httpOnly: true,
    sameSite: "Lax",
    expires: time / 1000 + 28800,
  }));
const proxyLines = (count = 2, version = 2) =>
  proxyCookies(count, version).map(
    (cookie) =>
      `${cookie.name}=${cookie.value}; Path=/; Max-Age=28800; HttpOnly; Secure; SameSite=Lax`,
  );
const withProxy = (count = 2) => ({
  ...storage(),
  cookies: [...storage().cookies, ...proxyCookies(count)],
});

test("оба слоя отправляются в session GET и business, IdP и login CSRF не передаются", async () => {
  for (const count of [1, 2, 4]) {
    const state = withProxy(count);
    state.cookies.push({
      ...proxyCookies(1)[0],
      name: "KEYCLOAK_SESSION",
      domain: "idp.disposable.invalid",
      value: "synthetic-idp-sentinel",
    });
    state.cookies.push({
      ...proxyCookies(1)[0],
      name: `${proxyCookieName}_csrf_nonce`,
      value: "synthetic-login-sentinel",
    });
    state.cookies.push({
      ...proxyCookies(1)[0],
      name: `${proxyCookieName}_nonce_csrf`,
      value: "synthetic-login-sentinel",
    });
    const f = fixture();
    const client = createOwnerSessionClient({ origin, storage: state, ...f });
    await close(await client.request("/api/v1/bootstrap"));
    for (const call of f.calls) {
      for (const cookie of proxyCookies(count))
        assert.ok(
          call.headers.get("cookie").includes(`${cookie.name}=${cookie.value}`),
        );
      assert.equal(call.headers.get("cookie").includes("sentinel"), false);
      assert.equal(call.headers.get("x-csrf-token"), "1".repeat(43));
    }
    assert.equal(client.authenticatedCookies().length, 2 + count);
  }
});

test("foreign, expired, ambiguous и malformed proxy cookies закрывают network без раскрытия значений", () => {
  for (const mutate of [
    (cookies) => {
      cookies[0].domain = ".disposable.invalid";
    },
    (cookies) => {
      cookies[0].domain = "foreign.invalid";
    },
    (cookies) => {
      cookies[0].path = "/api";
    },
    (cookies) => {
      cookies[0].secure = false;
    },
    (cookies) => {
      cookies[0].httpOnly = false;
    },
    (cookies) => {
      cookies[0].sameSite = "None";
    },
    (cookies) => {
      cookies[0].partitionKey = origin;
    },
    (cookies) => {
      cookies[0].expires = initialTime / 1000;
    },
    (cookies) => {
      cookies[0].expires = initialTime / 1000 + 28801;
    },
    (cookies) => {
      cookies[0].expires = -1;
    },
    (cookies) => {
      cookies[0].value = "synthetic-secret;injection";
    },
    (cookies) => {
      cookies[0].value = "s".repeat(4097);
    },
    (cookies) => {
      cookies[0].name = `${proxyCookieName}_00`;
    },
    (cookies) => {
      cookies[1].name = `${proxyCookieName}_3`;
    },
    (cookies) => {
      cookies.pop();
    },
    (cookies) => {
      cookies.push(cookies[0]);
    },
    (cookies) => {
      cookies.push(...proxyCookies(1));
    },
    (cookies) => {
      cookies.push(...proxyCookies(4));
    },
  ]) {
    const proxy = proxyCookies();
    mutate(proxy);
    const f = fixture();
    assert.throws(
      () =>
        createOwnerSessionClient({
          origin,
          storage: { cookies: [...storage().cookies, ...proxy] },
          ...f,
        }),
      (error) =>
        error.message.startsWith("Owner session acceptance proxy cookie") &&
        !error.message.includes("synthetic-secret"),
    );
    assert.equal(f.calls.length, 0);
  }
  assert.throws(
    () =>
      sessionHeaders(
        withProxy(),
        "http://control.disposable.invalid",
        initialTime,
      ),
    /origin is invalid/,
  );
});

test("proxy rotation меняет весь набор chunks, BFF renewal сохраняет оба слоя", async () => {
  for (const [before, after] of [
    [1, 2],
    [2, 1],
    [4, 2],
    [2, 4],
  ]) {
    let rotated = false;
    const f = fixture({
      business: () => {
        if (rotated) return;
        rotated = true;
        return json({ ok: true }, proxyLines(after));
      },
    });
    const client = createOwnerSessionClient({
      origin,
      storage: withProxy(before),
      ...f,
    });
    await close(await client.request("/api/v1/bootstrap"));
    assert.deepEqual(
      client
        .authenticatedCookies()
        .slice(2)
        .map((cookie) => cookie.name),
      proxyCookies(after).map((cookie) => cookie.name),
    );
    f.advance(181000);
    await close(await client.request("/api/v1/runs", { method: "POST" }));
    const last = f.calls.at(-1);
    assert.equal(last.headers.get("x-csrf-token"), "2".repeat(43));
    for (const cookie of proxyCookies(after, 2))
      assert.ok(
        last.headers.get("cookie").includes(`${cookie.name}=${cookie.value}`),
      );
    assert.equal(f.calls.filter((call) => call.method === "POST").length, 1);
  }
});

test("полная совместная rotation и удаление старых proxy chunks принимаются атомарно", async () => {
  const f = fixture({
    session: ({ time }) =>
      json(metadata(time), [
        ...cookieLines(2),
        ...proxyLines(1),
        ...proxyCookies().map(
          (cookie) =>
            `${cookie.name}=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax`,
        ),
      ]),
  });
  const client = createOwnerSessionClient({
    origin,
    storage: withProxy(),
    ...f,
  });
  await close(await client.request("/api/v1/bootstrap"));
  assert.equal(client.authenticatedCookies().length, 3);
  assert.equal(f.calls.at(-1).headers.get("x-csrf-token"), "2".repeat(43));
});

test("неполная, foreign и небезопасная rotation запрещает effect и повтор", async () => {
  for (const lines of [
    proxyLines().slice(0, 1),
    [proxyLines()[0], proxyLines()[0]],
    [...proxyLines(), proxyLines(1)[0]],
    [proxyLines(1)[0] + "; Domain=control.disposable.invalid"],
    [proxyLines(1)[0].replace("Max-Age=28800", "Max-Age=28801")],
    [proxyLines(1)[0].replace("HttpOnly; ", "")],
    [proxyLines(1)[0].replace("SameSite=Lax", "SameSite=Strict")],
    [`${proxyCookieName}=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax`],
    [
      "KEYCLOAK_SESSION=synthetic-secret; Path=/; Max-Age=28800; HttpOnly; Secure; SameSite=Lax",
    ],
    [cookieLines()[0], ...proxyLines()],
  ]) {
    const f = fixture({ session: () => json(metadata(), lines) });
    const client = createOwnerSessionClient({
      origin,
      storage: withProxy(),
      ...f,
    });
    await assert.rejects(
      client.request("/api/v1/runs", { method: "POST" }),
      /preflight failed/,
    );
    await assert.rejects(
      client.request("/api/v1/runs", { method: "POST" }),
      /preflight failed/,
    );
    assert.equal(f.calls.length, 1);
    assert.throws(
      () => client.authenticatedCookies(),
      /snapshot is unavailable/,
    );
  }
});

test("proxy expiry после prepare закрывает последующий запрос", async () => {
  const state = withProxy();
  for (const cookie of state.cookies.slice(2))
    cookie.expires = initialTime / 1000 + 1;
  const f = fixture();
  const client = createOwnerSessionClient({ origin, storage: state, ...f });
  await close(await client.request("/api/v1/bootstrap"));
  f.advance(1000);
  await assert.rejects(
    client.request("/api/v1/runs", { method: "POST" }),
    /proxy cookie boundary/,
  );
  assert.equal(f.calls.length, 2);
});

test("private persistence учитывает proxy rotation, ingress 401 не разрешает BFF refresh", async () => {
  const directory = mkdtempSync(join(tmpdir(), "kodex-owner-proxy-"));
  const path = join(directory, "authenticated.json");
  try {
    writeAuthenticatedState(path, withProxy());
    const first = fixture({
      session: ({ time }) => json(metadata(time), proxyLines(1)),
    });
    await close(
      await createOwnerSessionClient({
        origin,
        storagePath: path,
        ...first,
      }).request("/api/v1/bootstrap"),
    );
    assert.equal(readAuthenticatedState(path).cookies.length, 3);
    assert.equal(readAuthenticatedState(path).cookies[2].name, proxyCookieName);
    assert.equal(
      readFileSync(`${path}.session.json`, "utf8").includes("synthetic-proxy"),
      false,
    );
    const second = fixture({
      session: () =>
        new Response("Unauthorized", {
          status: 401,
          headers: { "Content-Type": "text/plain" },
        }),
    });
    const client = createOwnerSessionClient({
      origin,
      storagePath: path,
      ...second,
    });
    await assert.rejects(
      client.request("/api/v1/bootstrap"),
      /preflight failed/,
    );
    assert.deepEqual(
      second.calls.map((call) => call.method),
      ["GET"],
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("network exception не раскрывает cookies и не повторяет UNKNOWN mutation", async () => {
  const f = fixture({
    business: () => {
      throw new Error("synthetic-proxy-private-sentinel");
    },
  });
  const client = createOwnerSessionClient({
    origin,
    storage: withProxy(),
    ...f,
  });
  await assert.rejects(client.request("/api/v1/runs", { method: "POST" }), {
    message:
      "Owner session acceptance request failed; no automatic retry was performed",
  });
  assert.equal(f.calls.filter((call) => call.method === "POST").length, 1);
});

test("browser handoff удаляет старые chunks и сохраняет IdP; concurrent change закрывает запись", async () => {
  for (const changed of [false, true]) {
    const previous = storage();
    previous.cookies.push(...proxyCookies(2, 1, Date.now()));
    const next = [...storage(2).cookies, ...proxyCookies(1, 2, Date.now())];
    let current = structuredClone(previous.cookies);
    current.push({
      ...proxyCookies(1, 1, Date.now())[0],
      name: "KEYCLOAK_SESSION",
      domain: "idp.disposable.invalid",
    });
    if (changed) current[2].value = "synthetic-concurrent-generation";
    let writes = 0;
    const context = {
      cookies: async () => structuredClone(current),
      clearCookies: async (filter) => {
        writes++;
        current = current.filter(
          (cookie) =>
            !(
              cookie.name === filter.name &&
              cookie.domain === filter.domain &&
              cookie.path === filter.path
            ),
        );
      },
      addCookies: async (cookies) => {
        writes++;
        current = [
          ...current.filter(
            (cookie) =>
              !cookies.some(
                (item) =>
                  item.name === cookie.name &&
                  item.domain === cookie.domain &&
                  item.path === cookie.path,
              ),
          ),
          ...cookies,
        ];
      },
    };
    if (changed) {
      await assert.rejects(
        replaceAuthenticatedCookies(context, origin, previous, next),
        /browser handoff failed/,
      );
      assert.equal(writes, 0);
    } else {
      await replaceAuthenticatedCookies(context, origin, previous, next);
      assert.equal(current.length, 4);
      assert.ok(current.some((cookie) => cookie.name === "KEYCLOAK_SESSION"));
      assert.ok(
        !current.some(
          (cookie) => cookie.name.endsWith("_0") || cookie.name.endsWith("_1"),
        ),
      );
    }
  }
});

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
