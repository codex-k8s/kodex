import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { test } from "node:test";
import { authorizeProviderAPIKeyFixture } from "./provider-api-key-acceptance.mjs";

const sentinel = "synthetic-provider-sentinel-not-a-real-key";
const origin = "https://control.disposable.invalid";
const accountRef = "pacc_fixture1";
const account = {
  ref: accountRef,
  version: 17,
  nextActions: ["CONFIGURE_CREDENTIAL"],
};
const authorized = {
  ...account,
  version: 18,
  enabled: true,
  state: "AUTHORIZED",
  authorization: { method: "API_KEY", state: "AUTHORIZED" },
  externalAccountMasked: "fi***re",
};
const storage = () => ({
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
});
function json(value, headers = {}, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
      ...headers,
    },
  });
}
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
function fixture(overrides = {}) {
  const calls = [];
  const browserCalls = [];
  const snapshot = storage();
  const options = {
    origin,
    storage: snapshot,
    accountRef,
    apiKey: sentinel,
    onSessionCookies: async (cookies) => {
      browserCalls.push({ addCookies: cookies });
    },
    fetchAPI: async (url, init) => {
      const headers = new Headers(init.headers);
      calls.push({
        path: url.pathname,
        method: init.method ?? "GET",
        headers,
        body: init.body,
      });
      assert.equal(url.origin, origin);
      assert.equal(init.redirect, "error");
      assert.equal(headers.get("origin"), origin);
      assert.equal(headers.get("x-csrf-token"), "a".repeat(43));
      assert.match(headers.get("cookie"), /^__Host-kodex-session=v1\./);
      assert.equal(headers.get("authorization"), null);
      if (url.pathname === "/api/v1/session")
        return overrides.session?.() ?? json(metadata());
      if (init.method !== "POST")
        return overrides.read?.() ?? json(account, { ETag: '"17"' });
      assert.equal(headers.get("if-match"), '"17"');
      assert.match(headers.get("idempotency-key"), /^[a-f0-9-]{36}$/);
      assert.equal(headers.get("content-type"), "application/json");
      assert.deepEqual(JSON.parse(init.body), { apiKey: sentinel });
      return overrides.post?.() ?? json(authorized);
    },
  };
  return { options, calls, browserCalls, snapshot };
}
async function rejectsSafely(options) {
  await assert.rejects(authorizeProviderAPIKeyFixture(options), (error) => {
    assert.equal(
      error.message,
      "Provider fixture authorization failed; no automatic retry was performed",
    );
    assert.equal(error.cause, undefined);
    assert.ok(!error.stack.includes(sentinel));
    assert.ok(!JSON.stringify(error).includes(sentinel));
    return true;
  });
}

test("sentinel находится только в одном Node POST; browser cookies/storage/outcome чисты", async () => {
  const f = fixture();
  const before = JSON.stringify(f.snapshot);
  assert.equal(await authorizeProviderAPIKeyFixture(f.options), undefined);
  assert.deepEqual(
    f.calls.map(({ path, method }) => [path, method]),
    [
      ["/api/v1/session", "GET"],
      [`/api/v1/provider-accounts/${accountRef}`, "GET"],
      [`/api/v1/provider-accounts/${accountRef}/api-key-authorization`, "POST"],
    ],
  );
  assert.ok(!JSON.stringify(f.browserCalls).includes(sentinel));
  assert.equal(JSON.stringify(f.snapshot), before);
  assert.ok(!before.includes(sentinel));
  f.browserCalls[0].addCookies[0].value = "changed-synthetic-copy";
  assert.equal(JSON.stringify(f.snapshot), before);
});

test("lost ACK, 412 и отражённый credential не повторяют POST и не раскрывают error/body", async () => {
  for (const post of [
    () => {
      throw new Error(sentinel);
    },
    () => json({ secret: sentinel }, {}, 412),
    () => json({ ...authorized, credential: sentinel }),
    () => json(authorized, { "Set-Cookie": sentinel }),
  ]) {
    const f = fixture({ post });
    await rejectsSafely(f.options);
    assert.equal(f.calls.filter((call) => call.method === "POST").length, 1);
    assert.ok(!JSON.stringify(f.browserCalls).includes(sentinel));
  }
});

test("renewed cookie snapshot доступен browser cleanup после lost ACK без credential", async () => {
  const before = storage();
  const browserCalls = [];
  let posts = 0;
  await rejectsSafely({
    origin,
    storage: before,
    accountRef,
    apiKey: sentinel,
    onSessionCookies: async (cookies) => {
      browserCalls.push(cookies);
    },
    fetchAPI: async (url, init) => {
      if (url.pathname === "/api/v1/session") {
        const response = json(metadata());
        for (const cookie of storage().cookies) {
          const value = cookie.httpOnly
            ? `v1.${"b".repeat(64)}`
            : "b".repeat(43);
          response.headers.append(
            "Set-Cookie",
            `${cookie.name}=${value}; Path=/; Max-Age=1800;${cookie.httpOnly ? " HttpOnly;" : ""} Secure; SameSite=Strict`,
          );
        }
        return response;
      }
      assert.equal(
        new Headers(init.headers).get("x-csrf-token"),
        "b".repeat(43),
      );
      if (init.method !== "POST") return json(account, { ETag: '\"17\"' });
      posts++;
      throw new Error(sentinel);
    },
  });
  assert.equal(posts, 1);
  assert.equal(browserCalls.length, 1);
  assert.equal(browserCalls[0][1].value, "b".repeat(43));
  assert.ok(!JSON.stringify(browserCalls).includes(sentinel));
  assert.equal(before.cookies[1].value, "a".repeat(43));
});

test("неверная cookie boundary, stale descriptor, ETag или permission закрывают POST", async () => {
  for (const modify of [
    (f) => {
      f.options.storage.cookies[0].domain = ".disposable.invalid";
    },
    (f) => {
      f.options.storage.cookies[1].secure = false;
    },
    (f) => {
      f.options.storage.cookies.push(f.options.storage.cookies[0]);
    },
    (f) => {
      f.options.origin = "http://control.disposable.invalid";
    },
  ]) {
    const f = fixture();
    modify(f);
    await rejectsSafely(f.options);
    assert.equal(f.calls.length, 0);
  }
  for (const response of [
    json(account),
    json(account, { ETag: 'W/"17"' }),
    json({ ...account, version: 16 }, { ETag: '"17"' }),
    json({ ...account, nextActions: [] }, { ETag: '"17"' }),
  ]) {
    const f = fixture({ read: () => response });
    await rejectsSafely(f.options);
    assert.equal(f.calls.filter((call) => call.method === "POST").length, 0);
  }
});

test("E2E source не передаёт apiKey в DOM/evaluate/storage/browser network; trace off", () => {
  const require = createRequire(
    new URL(
      "../../services/staff/control-center/package.json",
      import.meta.url,
    ),
  );
  const ts = require("typescript");
  const source = readFileSync(
    new URL(
      "../../services/staff/control-center/e2e/integration-path.spec.ts",
      import.meta.url,
    ),
    "utf8",
  );
  const tree = ts.createSourceFile(
    "integration-path.spec.ts",
    source,
    ts.ScriptTarget.Latest,
    true,
  );
  const uses = [];
  function visit(node) {
    if (ts.isIdentifier(node) && node.text === "apiKey") {
      const parent = node.parent;
      if (ts.isVariableDeclaration(parent) && parent.name === node) {
        assert.match(
          parent.initializer.getText(tree),
          /readProviderAPIKey\(\)/,
        );
      } else if (ts.isBinaryExpression(parent) && parent.left === node) {
        assert.equal(parent.right.getText(tree), "undefined");
      } else {
        assert.ok(ts.isShorthandPropertyAssignment(parent));
        assert.ok(ts.isObjectLiteralExpression(parent.parent));
        const call = parent.parent.parent;
        assert.ok(ts.isCallExpression(call));
        assert.equal(
          call.expression.getText(tree),
          "authorizeProviderAPIKeyFixture",
        );
        uses.push(call);
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(tree);
  assert.equal(uses.length, 1);
  const helper = readFileSync(
    new URL("./provider-api-key-acceptance.mjs", import.meta.url),
    "utf8",
  );
  const helperTree = ts.createSourceFile(
    "helper.mjs",
    helper,
    ts.ScriptTarget.Latest,
    true,
  );
  function noBrowser(node) {
    if (ts.isIdentifier(node))
      assert.ok(
        ![
          "page",
          "document",
          "window",
          "localStorage",
          "sessionStorage",
          "APIRequestContext",
        ].includes(node.text),
      );
    ts.forEachChild(node, noBrowser);
  }
  noBrowser(helperTree);
  const config = readFileSync(
    new URL(
      "../../services/staff/control-center/playwright.integration.config.ts",
      import.meta.url,
    ),
    "utf8",
  );
  for (const option of ["trace", "video", "screenshot"])
    assert.match(config, new RegExp(`${option}: "off"`));
  assert.match(config, /retries: 0/);
});
