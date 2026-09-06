import assert from "node:assert/strict";
import {
  readFileSync,
  mkdtempSync,
  writeFileSync,
  chmodSync,
  symlinkSync,
  linkSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import {
  exactOrigin,
  fixtureDigest,
  matchesRussianFixture,
  readAuthenticatedState,
  runSTTHTTPAcceptance,
  sessionHeaders,
} from "./stt-http-acceptance.mjs";
import { boundedResponseBody } from "./runtime-workspace-acceptance.mjs";

const origin = "https://control.disposable.invalid";
const audio = readFileSync(
  new URL(
    "../../services/internal/stt-tts-service/testdata/1-2-3-4-5.mp3",
    import.meta.url,
  ),
);
const storage = () => ({
  cookies: [
    {
      name: "__Host-kodex-session",
      value: `v1.${"s".repeat(64)}`,
      domain: "control.disposable.invalid",
      path: "/",
      secure: true,
      httpOnly: true,
      sameSite: "Strict",
      expires: -1,
    },
    {
      name: "__Host-kodex-csrf",
      value: "c".repeat(43),
      domain: "control.disposable.invalid",
      path: "/",
      secure: true,
      httpOnly: false,
      sameSite: "Strict",
      expires: -1,
    },
  ],
  origins: [],
});
const configuration = () => ({
  ready: true,
  enabled: true,
  permissionKey: "platform.stt.use",
  revision: 7,
  digest: "a".repeat(64),
  model: "gpt-transcribe",
  providerCredentialGeneration: 3,
  maximumAudioBytes: 10485760,
  readinessBlockers: [],
});
const transcript = () => ({
  text: "Раз два три четыре пять.",
  receipt: {
    completedStage: "PROVIDER_COMPLETED",
    configRevision: 7,
    model: "gpt-transcribe",
    authoritySourceRevision: 11,
    requestId: "11111111-1111-4111-8111-111111111111",
    correlationId: "22222222-2222-4222-8222-222222222222",
    language: "",
  },
});
const json = (value, headers = {}) =>
  new Response(JSON.stringify(value), {
    headers: {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
      ...headers,
    },
  });

function fixture(change = () => {}) {
  const calls = [];
  let reads = 0;
  const fetchAPI = async (url, options) => {
    calls.push({ url, options });
    assert.equal(url.origin, origin);
    assert.equal(options.redirect, "error");
    assert.equal(options.headers.Origin, origin);
    assert.ok(options.signal instanceof AbortSignal);
    let value;
    if (url.pathname === "/api/v1/session") {
      const time = Date.now();
      value = {
        generation: "33333333-3333-4333-8333-333333333333",
        version: 1,
        sessionRevision: 2,
        renewalMode: "BACKEND_REFRESH",
        serverTime: new Date(time).toISOString(),
        accessExpiresAt: new Date(time + 600000).toISOString(),
        expiresAt: new Date(time + 1800000).toISOString(),
        absoluteExpiresAt: new Date(time + 3600000).toISOString(),
        renewAfter: new Date(time + 480000).toISOString(),
      };
    } else if (url.pathname === "/api/v1/system-stt-configuration") {
      reads += 1;
      value = configuration();
    } else if (url.pathname === "/api/v1/bootstrap")
      value = {
        speechTranscription: {
          available: true,
          reason: "READY",
          validUntil: new Date(Date.now() + 60000).toISOString(),
        },
      };
    else {
      assert.equal(options.method, "POST");
      assert.equal(
        new Headers(options.headers).get("X-Audio-Size"),
        String(audio.length),
      );
      assert.equal(options.headers["Idempotency-Key"], undefined);
      assert.ok(options.body instanceof FormData);
      assert.equal(options.body.get("audio").size, audio.length);
      value = transcript();
    }
    const response = change({ value, url, options, reads });
    return response ?? json(value);
  };
  return { calls, fetchAPI };
}

test("protected org and project paths preserve receipt pins without transcript evidence", async () => {
  for (const projectRef of ["", "prj_fixture01"]) {
    const f = fixture();
    let reservations = 0;
    const result = await runSTTHTTPAcceptance({
      origin,
      storage: storage(),
      audio,
      projectRef,
      fetchAPI: f.fetchAPI,
      beforePost: () => reservations++,
    });
    assert.equal(reservations, 1);
    assert.equal(result.status, "PASS");
    assert.equal(result.fixtureDigest, fixtureDigest);
    assert.equal(result.match, true);
    assert.equal(result.configurationRevision, 7);
    assert.equal(result.authoritySourceRevision, 11);
    assert.equal(f.calls.length, 5);
    assert.equal(
      f.calls[3].url.pathname,
      projectRef
        ? `/api/v1/projects/${projectRef}/speech/transcriptions`
        : "/api/v1/speech/transcriptions",
    );
    assert.equal(Object.hasOwn(result, "text"), false);
    assert.equal(Object.hasOwn(result, "model"), false);
    assert.equal(Object.hasOwn(result, "credentialGeneration"), false);
  }
});

test("session renewal precedes the only billable upload and uses the renewed cookie pair", async () => {
  for (const failedPost of [false, true]) {
    const f = fixture(({ value, url, options }) => {
      if (url.pathname === "/api/v1/session") {
        if (options.method === "GET") value.renewAfter = value.serverTime;
        else {
          assert.equal(options.method, "PUT");
          assert.equal(options.body, undefined);
          assert.equal(new Headers(options.headers).get("if-match"), null);
          const headers = new Headers({
            "Content-Type": "application/json",
            "Cache-Control": "no-store",
          });
          for (const cookie of storage().cookies) {
            headers.append(
              "Set-Cookie",
              `${cookie.name}=${cookie.httpOnly ? `v1.${"n".repeat(64)}` : "n".repeat(43)}; Path=/; Secure; SameSite=Strict; Max-Age=1800${cookie.httpOnly ? "; HttpOnly" : ""}`,
            );
          }
          value.version = 2;
          return new Response(JSON.stringify(value), { headers });
        }
      } else {
        assert.equal(
          new Headers(options.headers).get("x-csrf-token"),
          "n".repeat(43),
        );
        if (options.method === "POST" && failedPost)
          return new Response(null, { status: 429 });
      }
    });
    const run = runSTTHTTPAcceptance({
      origin,
      storage: storage(),
      audio,
      fetchAPI: f.fetchAPI,
    });
    if (failedPost) await assert.rejects(run);
    else assert.equal((await run).status, "PASS");
    assert.equal(
      f.calls.filter((call) => call.options.method === "PUT").length,
      1,
    );
    assert.equal(
      f.calls.filter((call) => call.options.method === "POST").length,
      1,
    );
  }
});

test("only case, whitespace and terminal punctuation are normalized", () => {
  for (const value of [
    "раз два три четыре пять",
    " РАЗ\nдва  три\tчетыре пять?! ",
    "раз два три четыре пять…",
    "\u0085раз\u0085два\u2003три четыре пять ! ? . \n",
  ])
    assert.equal(matchesRussianFixture(value), true);
  for (const value of [
    undefined,
    "раз, два три четыре пять",
    "р@аз два три четыре пять",
    "раз два три четыре пять🙂",
    "раз два четыре три пять",
    "раз два три четыре",
    "1 2 3 4 5",
    "раз два три четыре пять ещё",
    "\ufeffраз два три четыре пять",
  ])
    assert.equal(matchesRussianFixture(value), false);
});

test("origin and exact host cookies reject ambiguous or foreign transport", () => {
  for (const invalid of [
    "http://control.invalid",
    "https://prod.invalid",
    "https://control.invalid/path",
    "https://control.invalid/?x=1",
    "https://user:password@control.invalid",
    "https://control.invalid/#x",
  ])
    assert.throws(() => exactOrigin(invalid));
  for (const mutation of [
    (s) => s.cookies.push(s.cookies[0]),
    (s) => (s.cookies[0].domain = ".disposable.invalid"),
    (s) => (s.cookies[0].domain = "foreign.invalid"),
    (s) => (s.cookies[0].path = "/api"),
    (s) => (s.cookies[0].secure = false),
    (s) => (s.cookies[0].sameSite = "None"),
    (s) => (s.cookies[0].httpOnly = false),
    (s) => (s.cookies[0].expires = 1),
    (s) => (s.cookies[0].value += ";other=1"),
    (s) => (s.cookies[0].partitionKey = "foreign"),
    (s) => (s.cookies[1].httpOnly = true),
  ]) {
    const value = storage();
    mutation(value);
    assert.throws(() => sessionHeaders(value, origin));
  }
});

test("invalid fixture and stale eligibility never submit audio", async () => {
  const cases = [
    (c) => {
      c.ready = false;
    },
    (c) => {
      c.enabled = false;
    },
    (c) => {
      c.providerCredentialGeneration = 0;
    },
    (c) => {
      c.maximumAudioBytes = 1;
    },
    (c) => {
      c.readinessBlockers = ["STT_CREDENTIAL_UNAVAILABLE"];
    },
    (c) => {
      c.digest = "invalid";
    },
  ];
  for (const mutation of cases) {
    const f = fixture(({ value, url }) => {
      if (url.pathname === "/api/v1/system-stt-configuration") mutation(value);
    });
    await assert.rejects(
      runSTTHTTPAcceptance({
        origin,
        storage: storage(),
        audio,
        fetchAPI: f.fetchAPI,
      }),
    );
    assert.equal(
      f.calls.some((call) => call.options.method === "POST"),
      false,
    );
  }
  for (const mutation of [
    (a) => {
      a.available = false;
    },
    (a) => {
      a.reason = "STT_PERMISSION_DENIED";
    },
    (a) => {
      a.validUntil = "invalid";
    },
    (a) => {
      a.validUntil = new Date(1).toISOString();
    },
  ]) {
    const f = fixture(({ value, url }) => {
      if (url.pathname === "/api/v1/bootstrap")
        mutation(value.speechTranscription);
    });
    await assert.rejects(
      runSTTHTTPAcceptance({
        origin,
        storage: storage(),
        audio,
        fetchAPI: f.fetchAPI,
      }),
    );
    assert.equal(f.calls.length, 3);
  }
  const f = fixture();
  await assert.rejects(
    runSTTHTTPAcceptance({
      origin,
      storage: storage(),
      audio: Buffer.alloc(audio.length),
      fetchAPI: f.fetchAPI,
    }),
  );
  assert.equal(f.calls.length, 0);
});

test("stale receipt, false match and changed serving configuration reject success", async () => {
  for (const mutation of [
    (v) => {
      v.receipt.configRevision++;
    },
    (v) => {
      v.receipt.authoritySourceRevision = 0;
    },
    (v) => {
      v.receipt.completedStage = "STARTED";
    },
    (v) => {
      v.receipt.requestId = "invalid";
    },
    (v) => {
      v.receipt.model = "different";
    },
    (v) => {
      v.text = "раз два три";
    },
  ]) {
    const f = fixture(({ value, options }) => {
      if (options.method === "POST") mutation(value);
    });
    await assert.rejects(
      runSTTHTTPAcceptance({
        origin,
        storage: storage(),
        audio,
        fetchAPI: f.fetchAPI,
      }),
    );
    assert.equal(
      f.calls.filter((call) => call.options.method === "POST").length,
      1,
    );
  }
  for (const field of ["revision", "digest", "providerCredentialGeneration"]) {
    const f = fixture(({ value, reads, url }) => {
      if (url.pathname === "/api/v1/system-stt-configuration" && reads === 2)
        value[field] = field === "digest" ? "b".repeat(64) : value[field] + 1;
    });
    await assert.rejects(
      runSTTHTTPAcceptance({
        origin,
        storage: storage(),
        audio,
        fetchAPI: f.fetchAPI,
      }),
    );
  }
});

test("transport, provider failure and lost response never retry a billable POST", async () => {
  for (const outcome of [
    "network",
    "429",
    "503",
    "redirect",
    "oversize",
    "html",
  ]) {
    const f = fixture(({ options }) => {
      if (options.method !== "POST") return;
      if (outcome === "network")
        throw new Error("synthetic private provider response");
      if (outcome === "oversize")
        return json(transcript(), { "Content-Length": "65537" });
      if (outcome === "html")
        return json(transcript(), { "Content-Type": "text/html" });
      return new Response("synthetic private provider response", {
        status: outcome === "redirect" ? 302 : Number(outcome),
      });
    });
    await assert.rejects(
      runSTTHTTPAcceptance({
        origin,
        storage: storage(),
        audio,
        fetchAPI: f.fetchAPI,
      }),
    );
    assert.equal(
      f.calls.filter((call) => call.options.method === "POST").length,
      1,
    );
  }
});

test("authenticated state uses a private regular descriptor and rejects aliases", () => {
  const directory = mkdtempSync(join(tmpdir(), "kodex-stt-http-"));
  try {
    chmodSync(directory, 0o700);
    const path = join(directory, "state.json");
    writeFileSync(path, JSON.stringify(storage()), { mode: 0o600 });
    assert.equal(readAuthenticatedState(path).cookies.length, 2);
    chmodSync(path, 0o644);
    assert.throws(() => readAuthenticatedState(path));
    chmodSync(path, 0o600);
    symlinkSync(path, join(directory, "symlink"));
    assert.throws(() => readAuthenticatedState(join(directory, "symlink")));
    linkSync(path, join(directory, "hardlink"));
    assert.throws(() => readAuthenticatedState(path));
    rmSync(join(directory, "hardlink"));
    chmodSync(directory, 0o755);
    assert.throws(() => readAuthenticatedState(path));
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("oversize response cancels the stream before buffering provider data", async () => {
  for (const declared of ["65537", "invalid"]) {
    let cancelled = false;
    const body = new ReadableStream({
      cancel() {
        cancelled = true;
      },
    });
    const response = new Response(body, {
      headers: { "Content-Length": declared },
    });
    await assert.rejects(boundedResponseBody(response, 65536));
    assert.equal(cancelled, true);
  }
});

test("failed durable reservation prevents the provider request", async () => {
  const f = fixture();
  await assert.rejects(
    runSTTHTTPAcceptance({
      origin,
      storage: storage(),
      audio,
      fetchAPI: f.fetchAPI,
      beforePost() {
        throw new Error("Synthetic reservation failure");
      },
    }),
  );
  assert.equal(
    f.calls.some((call) => call.options.method === "POST"),
    false,
  );
});
