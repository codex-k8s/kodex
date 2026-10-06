import test from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { Readable } from "node:stream";
import vm from "node:vm";
import {
  assertOwnerPins,
  secretInputIntent,
  secretInputKey,
} from "./project-secret-owner-ui-input.mjs";
import { assertPeerUID } from "./protected-owner-ui-input.mjs";
import {
  assertBrokerRequest,
  brokerBody,
  brokerHost,
  brokerOrigin,
  brokerPath,
  browserInputScript,
  createBrokerHandler,
  main,
  parseBrokerCLI,
  profileDigest,
  brokerLifetime,
} from "./project-secret-owner-ui-broker.mjs";

const port = 23456;
const sentinel = "synthetic-private-Git-token-never-log";
const profile = {
  cdpEndpoint: "http://127.0.0.1:9222",
  chromePID: 1234,
  chromeStartTicks: "4321",
  pageURL: `${brokerOrigin}/projects/prj_synthetic/secrets`,
  organizationRef: "org_synthetic",
  ownerRef: "usr_synthetic",
  projectRef: "prj_synthetic",
  environmentRef: "renv_synthetic",
  environmentVersion: 2,
  versionRef: "renvv_synthetic",
  environmentDigest: "a".repeat(64),
  agentRef: "agt_synthetic",
  agentVersion: 5,
  bindingRef: "aenv_synthetic",
  bindingVersion: 3,
  bindingDigest: "b".repeat(64),
};
function proof() {
  return {
    ownerRef: profile.ownerRef,
    organizationRef: profile.organizationRef,
    platformRole: "OWNER",
    environment: {
      ref: profile.environmentRef,
      organizationRef: profile.organizationRef,
      projectRef: profile.projectRef,
      scopeKind: "PROJECT",
      name: "selfdev-write",
      state: "ACTIVE",
      ready: true,
      version: profile.environmentVersion,
      revisionRef: profile.versionRef,
      digest: profile.environmentDigest,
    },
    agentVersion: profile.agentVersion,
    binding: {
      ref: profile.bindingRef,
      version: profile.bindingVersion,
      agentRef: profile.agentRef,
      environmentRef: profile.environmentRef,
      versionRef: profile.versionRef,
      digest: profile.bindingDigest,
    },
    assignedAgents: {
      items: [
        {
          ref: profile.agentRef,
          version: profile.agentVersion,
          projectRef: profile.projectRef,
          system: false,
        },
      ],
      nextPageToken: "",
    },
    impact: {
      environmentRef: profile.environmentRef,
      environmentVersion: profile.environmentVersion,
      targetVersionRef: profile.versionRef,
      targetDigest: profile.environmentDigest,
      total: 0,
      consumers: [],
      nextPageToken: "",
    },
    secretAbsent: true,
  };
}
function request(patch = {}, body = Buffer.from(brokerBody(profile))) {
  const r = Readable.from([body]);
  Object.assign(
    r,
    {
      socket: {
        servername: brokerHost,
        getProtocol: () => "TLSv1.3",
        localAddress: "127.0.0.2",
        localPort: port,
        remoteAddress: "127.0.0.1",
        remotePort: 45678,
        destroyed: false,
      },
      method: "POST",
      url: brokerPath,
      headers: {
        host: `${brokerHost}:${port}`,
        origin: brokerOrigin,
        "content-type": "application/json",
        "content-length": String(body.length),
      },
    },
    patch,
  );
  r.rawHeaders = Object.entries(r.headers).flat();
  return r;
}
function response() {
  const r = new EventEmitter();
  r.headers = {};
  r.setHeader = (name, value) => (r.headers[name] = value);
  r.writeHead = (status) => (r.status = status);
  r.end = (body) => {
    r.body = body;
    r.emit("finish");
  };
  return r;
}
function fixture(patch = {}) {
  const calls = [],
    states = [];
  const selected = { [secretInputKey]: sentinel };
  const handler = createBrokerHandler({
    port,
    profile,
    deadline: 60000,
    now: () => 1000,
    readPeer: async () => calls.push("peer"),
    readSecrets: (keys) => {
      assert.deepEqual(keys, [secretInputKey]);
      calls.push("secret");
      return selected;
    },
    close: async () => calls.push("close"),
    output: (value) => states.push(value),
    ...patch,
  });
  return { handler, calls, states, selected };
}

test("CLI/profile digest только public intent+exact private pins, без arbitrary key", async () => {
  assert.equal(
    parseBrokerCLI([
      "--profile",
      "/private/profile.json",
      "--confirm",
      secretInputIntent,
    ]),
    "/private/profile.json",
  );
  assert.match(profileDigest(profile), /^[a-f0-9]{64}$/);
  assert.notEqual(
    profileDigest({ ...profile, agentVersion: 6 }),
    profileDigest(profile),
  );
  assert.equal(brokerLifetime, 60000);
  for (const args of [
    [],
    ["--profile", "/private/profile.json", "--confirm", "OTHER"],
    [
      "--profile",
      "/private/profile.json",
      "--confirm",
      secretInputIntent,
      "--key",
      "OTHER",
    ],
  ])
    assert.throws(() => parseBrokerCLI(args));
  let tls = false;
  const states = [];
  assert.equal(
    await main(
      ["--profile", "/private/profile.json", "--confirm", secretInputIntent],
      {
        environment: { DEBUG: "enabled" },
        argumentsList: [],
        readTLS: () => {
          tls = true;
        },
        output: (value) => states.push(value),
      },
    ),
    1,
  );
  assert.equal(tls, false);
  assert.deepEqual(states, [{ state: "FAILED" }]);
});

test("Request exact app Origin/Host/SNI/TLS1.3/loopback; foreign frames не читают Git token", async () => {
  assert.equal(assertBrokerRequest(request(), port), "INPUT");
  const mutations = [
    (r) => (r.socket.servername = "foreign"),
    (r) => (r.socket.getProtocol = () => "TLSv1.2"),
    (r) => (r.socket.localAddress = "0.0.0.0"),
    (r) => (r.socket.remoteAddress = "10.0.0.2"),
    (r) => (r.url += "?other=1"),
    (r) => (r.method = "GET"),
    (r) => (r.headers.origin = "https://foreign.invalid"),
    (r) => (r.headers.host = "foreign:23456"),
    (r) => (r.headers.authorization = "synthetic"),
    (r) => (r.headers.cookie = "synthetic"),
    (r) => (r.headers["content-type"] = "text/plain"),
    (r) => (r.headers["transfer-encoding"] = "chunked"),
    (r) => (r.headers["content-length"] = "300"),
    (r) => (r.headers["content-length"] = "01"),
  ];
  for (const change of mutations) {
    const r = request();
    change(r);
    r.rawHeaders = Object.entries(r.headers).flat();
    const f = fixture(),
      out = response();
    await f.handler(r, out);
    assert.equal(out.status, 403);
    assert(!f.calls.includes("secret"));
    assert(!out.body);
  }
  const duplicate = request();
  duplicate.rawHeaders.push("Origin", brokerOrigin);
  assert.throws(() => assertBrokerRequest(duplicate, port));
});

test("Malformed preflight/body framing и private profile errors остаются закрытыми без TLS/secret read", async () => {
  for (const patch of [
    { "access-control-request-method": "GET" },
    { "access-control-request-headers": "content-type,authorization" },
    { "access-control-request-private-network": "false" },
  ]) {
    const r = request({
      method: "OPTIONS",
      headers: {
        host: `${brokerHost}:${port}`,
        origin: brokerOrigin,
        "access-control-request-method": "POST",
        "access-control-request-headers": "content-type",
        ...patch,
      },
    });
    const f = fixture(),
      out = response();
    await f.handler(r, out);
    assert.equal(out.status, 403);
    assert(!f.calls.includes("secret"));
  }
  const r = request();
  r.headers["content-length"] = String(
    Buffer.byteLength(brokerBody(profile)) - 1,
  );
  r.rawHeaders = Object.entries(r.headers).flat();
  const f = fixture(),
    out = response();
  await f.handler(r, out);
  assert.equal(out.status, 403);
  assert(!f.calls.includes("secret"));
  const states = [];
  let tls = false;
  assert.equal(
    await main(
      ["--profile", "/private/profile.json", "--confirm", secretInputIntent],
      {
        environment: {},
        argumentsList: [],
        readProfile: () => {
          throw new Error(sentinel);
        },
        readTLS: () => {
          tls = true;
        },
        output: (value) => states.push(value),
      },
    ),
    1,
  );
  assert.equal(tls, false);
  assert.deepEqual(states, [{ state: "FAILED" }]);
  assert(!JSON.stringify(states).includes(sentinel));
});

test("Exact preflight + sameUID reverse established peer, без credential response", async () => {
  const f = fixture(),
    out = response();
  const r = request({
    method: "OPTIONS",
    headers: {
      host: `${brokerHost}:${port}`,
      origin: brokerOrigin,
      "access-control-request-method": "POST",
      "access-control-request-headers": "content-type",
      "access-control-request-private-network": "true",
    },
  });
  await f.handler(r, out);
  assert.equal(out.status, 204);
  assert.equal(out.headers["Access-Control-Allow-Private-Network"], "true");
  assert.deepEqual(f.calls, ["peer"]);
  assert(!out.body);
  const source =
    "header\n0: 0100007F:B26E 0200007F:5BA0 01 0:0 00:0 0 1000 0 123";
  assert.doesNotThrow(() => assertPeerUID(source, request().socket, 1000));
  assert.throws(() => assertPeerUID(source, request().socket, 1001));
  assert.throws(() =>
    assertPeerUID(
      source + "\n" + source.split("\n")[1],
      request().socket,
      1000,
    ),
  );
});

test("Body digest/expiry/peer deny до secret; concurrent once+closed stdout", async () => {
  for (const payload of [
    Buffer.from(brokerBody({ ...profile, agentVersion: 6 })),
    Buffer.from("{}"),
    Buffer.from(" " + brokerBody(profile)),
    Buffer.from(
      '{"intent":"' + secretInputIntent + '","profileDigest":"bad","extra":1}',
    ),
    Buffer.alloc(257),
  ]) {
    const f = fixture(),
      out = response();
    await f.handler(request({}, payload), out);
    assert.equal(out.status, 403);
    assert(!f.calls.includes("secret"));
  }
  for (const patch of [
    { now: () => 60000 },
    {
      readPeer: async () => {
        throw new Error(sentinel);
      },
    },
  ]) {
    const f = fixture(patch),
      out = response();
    await f.handler(request(), out);
    assert.equal(out.status, 403);
    assert(!f.calls.includes("secret"));
    assert(!JSON.stringify(f.states).includes(sentinel));
  }
  const f = fixture(),
    out = response();
  await Promise.all([
    f.handler(request(), out),
    f.handler(request(), response()),
  ]);
  assert.equal(f.calls.filter((x) => x === "secret").length, 1);
  assert.equal(out.headers["Cache-Control"], "no-store");
  assert.equal(JSON.parse(out.body).value, sentinel);
  assert.equal(f.selected[secretInputKey], "");
  assert.deepEqual(f.states, [{ state: "CONSUMED" }]);
  assert(!JSON.stringify(f.states).includes(sentinel));
  const replay = response();
  await f.handler(request(), replay);
  assert.equal(replay.status, 403);
});

test("После consume peer исчезновение/raw reader error/invalid token не раскрывают values и не повторяются", async () => {
  let count = 0;
  for (const patch of [
    {
      readPeer: async () => {
        if (++count === 2) throw new Error(sentinel);
      },
    },
    {
      readSecrets: () => {
        throw new Error(sentinel);
      },
    },
    { readSecrets: () => ({ [secretInputKey]: "bad\nvalue" }) },
  ]) {
    const f = fixture(patch),
      out = response();
    await f.handler(request(), out);
    assert.equal(out.status, 403);
    assert(!out.body);
    assert(!JSON.stringify(f.states).includes(sentinel));
    const replay = response();
    await f.handler(request(), replay);
    assert.equal(replay.status, 403);
    assert(f.calls.includes("close"));
  }
});

test("Истечение между peerchecks не читает key, malformed selected values не передаются", async () => {
  let clock = 1000;
  const f = fixture({
      now: () => clock,
      readPeer: async () => {
        clock += 30000;
      },
    }),
    out = response();
  await f.handler(request(), out);
  assert.equal(out.status, 403);
  assert(!f.calls.includes("secret"));
  for (const value of [undefined, "", "bad\u0000value", "a".repeat(8193)]) {
    const selected = { [secretInputKey]: value };
    const g = fixture({ readSecrets: () => selected }),
      r = response();
    await g.handler(request(), r);
    assert.equal(r.status, 403);
    assert(!r.body);
    assert.equal(selected[secretInputKey], "");
  }
});

function browserFixture(options = {}) {
  const calls = [],
    events = [];
  let round = 0;
  const nodeProof = proof();
  class TextArea {
    constructor() {
      this.value = "";
      this.disabled = false;
      this.readOnly = false;
      this.classList = { contains: () => options.masked !== false };
    }
    closest() {
      return dialog;
    }
    getClientRects() {
      return [{}];
    }
    dispatchEvent(event) {
      events.push(event.type);
      options.onEvent?.(this, event);
    }
  }
  const field = new TextArea(),
    name = { value: "GH_TOKEN", disabled: false },
    type = { value: "STRING", disabled: false };
  const dialog = {
    querySelectorAll: (selector) =>
      selector.startsWith("input") ? [name] : [type],
  };
  const document = {
    contains: (node) => node === field && !options.detached,
    querySelectorAll: () => (options.duplicate ? [field, field] : [field]),
  };
  const location = new URL(profile.pageURL);
  let posts = 0;
  const context = {
    document,
    location,
    HTMLTextAreaElement: TextArea,
    getComputedStyle: () => ({
      webkitTextSecurity: options.masked === false ? "none" : "disc",
    }),
    Event: class {
      constructor(type) {
        this.type = type;
      }
    },
    AbortSignal,
    TextDecoder,
    Uint8Array,
    fetch: async (path, parameters) => {
      calls.push({ path, method: parameters.method });
      assert.equal(parameters.redirect, "error");
      assert.equal(parameters.cache, "no-store");
      if (parameters.method === "POST") {
        posts++;
        assert.equal(path, `https://${brokerHost}:${port}${brokerPath}`);
        assert.equal(parameters.credentials, "omit");
        assert.equal(parameters.body, brokerBody(profile));
        assert(!parameters.body.includes(sentinel));
        options.afterPost?.({ location, field, document });
        const bytes = new TextEncoder().encode(
          JSON.stringify(options.secretResponse ?? { value: sentinel }),
        );
        return {
          status: options.postStatus ?? 200,
          url: path,
          arrayBuffer: async () => bytes.buffer,
        };
      }
      assert.equal(parameters.credentials, "same-origin");
      assert.equal(
        parameters.headers["X-Kodex-Project-ID"],
        profile.projectRef,
      );
      assert(!Object.hasOwn(parameters, "body"));
      const index = calls.filter((x) => x.method === "GET").length - 1;
      if (index % 6 === 0) {
        round++;
        options.onRound?.(round, nodeProof);
      }
      const bodies = [
        {
          currentUser: { ref: nodeProof.ownerRef },
          organizationRef: nodeProof.organizationRef,
          platformRole: nodeProof.platformRole,
        },
        {
          ...nodeProof.environment,
          currentVersion: {
            ref: nodeProof.environment.revisionRef,
            digest: nodeProof.environment.digest,
          },
        },
        {
          agentVersion: nodeProof.agentVersion,
          environmentBinding: nodeProof.binding,
        },
        nodeProof.impact,
        { items: [], nextPageToken: "" },
        nodeProof.assignedAgents,
      ];
      const bytes = new TextEncoder().encode(
        JSON.stringify({ ...bodies[index % 6], unknown: sentinel }),
      );
      let done = false;
      return {
        status: 200,
        url: new URL(path, profile.pageURL).href,
        body: {
          getReader: () => ({
            read: async () =>
              done
                ? { done: true }
                : ((done = true), { done: false, value: bytes }),
            cancel: async () => {},
          }),
        },
      };
    },
  };
  return {
    field,
    name,
    type,
    calls,
    events,
    location,
    nodeProof,
    posts: () => posts,
    invoke: () =>
      vm.runInNewContext(`(${browserInputScript(port, profile)})`, context)(),
  };
}

test("Native JS: два fresh proof до broker и третий до fill, only Boolean, exact empty masked DOM, no submit/reveal", async () => {
  const f = browserFixture();
  assertOwnerPins(f.nodeProof, profile);
  const script = browserInputScript(port, profile);
  assert(!script.includes(sentinel));
  assert(!script.includes(".click("));
  assert(!script.includes("console."));
  assert.equal(await f.invoke(), true);
  assert.equal(f.field.value, sentinel);
  assert.deepEqual(f.events, ["input", "change"]);
  assert.equal(f.posts(), 1);
  assert.equal(f.calls.filter((x) => x.method === "GET").length, 18);
  assert(f.calls.slice(0, 12).every((x) => x.method === "GET"));
  assert.equal(f.calls[12].method, "POST");
  assert.throws(() => browserInputScript(0, profile));
});

test("Native proof/form/pagination/extraconsumer deny before broker, raw exceptions только false", async () => {
  const mutations = [
    (v) => (v.platformRole = "ADMINISTRATOR"),
    (v) => (v.ownerRef = "usr_foreign"),
    (v) => (v.organizationRef = "org_foreign"),
    (v) => (v.environment.projectRef = "prj_foreign"),
    (v) => (v.environment.name = "selfdev-review"),
    (v) => (v.environment.ready = false),
    (v) => v.environment.version++,
    (v) => v.binding.version++,
    (v) => (v.binding.versionRef = "renvv_old"),
    (v) => (v.binding.digest = "c".repeat(64)),
    (v) => (v.assignedAgents.items = []),
    (v) =>
      v.assignedAgents.items.push({
        ...v.assignedAgents.items[0],
        ref: "agt_foreign",
      }),
    (v) => (v.assignedAgents.nextPageToken = "agt_more"),
    (v) => (v.assignedAgents.items[0].projectRef = "prj_foreign"),
    (v) => (v.impact.total = 1),
  ];
  for (const change of mutations) {
    const f = browserFixture({
      onRound: (round, v) => {
        if (round === 1) change(v);
      },
    });
    assert.equal(await f.invoke(), false);
    assert.equal(f.posts(), 0);
    assert.equal(f.field.value, "");
  }
  for (const options of [
    { masked: false },
    { duplicate: true },
    { detached: true },
    {
      onRound: () => {
        throw new Error(sentinel);
      },
    },
    {
      onRound: (round, v) => {
        if (round === 2) v.assignedAgents.items = [];
      },
    },
  ]) {
    const f = browserFixture(options);
    assert.equal(await f.invoke(), false);
    assert.equal(f.posts(), 0);
    assert.equal(f.field.value, "");
  }
  for (const change of [
    (f) => (f.name.value = "OTHER"),
    (f) => (f.type.value = "JSON"),
    (f) => (f.field.value = "existing"),
    (f) => (f.location.href = "https://foreign.invalid"),
  ]) {
    const f = browserFixture();
    change(f);
    assert.equal(await f.invoke(), false);
    assert.equal(f.posts(), 0);
  }
});

test("Native post-consume drift/navigation/replaced DOM/malformed value не заполняют; partial event failure очищает exact field", async () => {
  for (const options of [
    { postStatus: 403 },
    { secretResponse: { value: sentinel, extra: "unexpected" } },
    { secretResponse: { value: "bad\nvalue" } },
    {
      afterPost: ({ location }) => (location.href = "https://foreign.invalid"),
    },
    { afterPost: ({ document }) => (document.contains = () => false) },
    { afterPost: ({ document }) => (document.querySelectorAll = () => []) },
    {
      onRound: (round, v) => {
        if (round === 3) v.assignedAgents.items = [];
      },
    },
    {
      onRound: (round) => {
        if (round === 3) throw new Error(sentinel);
      },
    },
  ]) {
    const f = browserFixture(options);
    const result = await f.invoke();
    assert.equal(result, false);
    assert.equal(f.field.value, "");
    assert.equal(f.posts(), 1);
  }
  const f = browserFixture({
    onEvent: (_field, event) => {
      if (event.type === "change") throw new Error(sentinel);
    },
  });
  assert.equal(await f.invoke(), false);
  assert.equal(f.field.value, "");
  assert.equal(f.posts(), 1);
});
