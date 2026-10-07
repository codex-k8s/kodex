import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { Readable } from "node:stream";
import { test } from "node:test";
import { runInNewContext } from "node:vm";
import { lstat } from "node:fs/promises";
import {
  assertInputEnvironment,
  assertInputRequest,
  assertPeerUID,
  browserInputScript,
  createInputHandler,
  inputHost,
  inputIntent,
  inputOrigin,
  inputPath,
  parseIngress,
  parseInputBody,
  parseInputCLI,
  parseTLSSecret,
  ownerTLSReadArguments,
  withPrivateKubectlCache,
} from "./protected-owner-ui-input.mjs";

const port = 23456;
const body = () => Buffer.from(JSON.stringify({ intent: inputIntent }));
function request(changes = {}, content = body()) {
  const value = Readable.from([content]);
  Object.assign(
    value,
    {
      socket: {
        servername: inputHost,
        getProtocol: () => "TLSv1.3",
        localAddress: "127.0.0.2",
        localPort: port,
        remoteAddress: "127.0.0.1",
        remotePort: 45678,
        destroyed: false,
      },
      method: "POST",
      url: inputPath,
      headers: {
        host: `${inputHost}:${port}`,
        origin: inputOrigin,
        "content-type": "application/json",
        "content-length": String(content.length),
      },
    },
    changes,
  );
  value.rawHeaders = Object.entries(value.headers).flat();
  return value;
}
function response() {
  const value = new EventEmitter();
  value.headers = {};
  value.setHeader = (name, text) => {
    value.headers[name] = text;
  };
  value.writeHead = (status) => {
    value.status = status;
  };
  value.end = (text) => {
    value.body = text;
    value.emit("finish");
  };
  return value;
}
function fixture(changes = {}) {
  const reads = [],
    states = [];
  let closed = 0;
  const options = {
    port,
    deadline: 60000,
    now: () => 1000,
    readPeer: async () => {},
    readSecrets: (keys) => {
      reads.push(keys);
      return {
        KODEX_LOCAL_OWNER_USERNAME: "SYNTHETIC_USER_SENTINEL",
        KODEX_LOCAL_OWNER_PASSWORD: "SYNTHETIC_PASSWORD_SENTINEL",
      };
    },
    close: async () => {
      closed++;
    },
    output: (state) => states.push(state),
    ...changes,
  };
  return {
    handler: createInputHandler(options),
    reads,
    states,
    closed: () => closed,
  };
}

test("CLI допускает только публичное подтверждение одной операции", () => {
  parseInputCLI(["--confirm", inputIntent]);
  for (const args of [
    [],
    ["--password", "SYNTHETIC"],
    ["--confirm", inputIntent, "--port", "1"],
    ["--confirm", "OTHER"],
  ])
    assert.throws(
      () => parseInputCLI(args),
      /^Error: OWNER_UI_INPUT_REJECTED$/,
    );
});

test("kubectl получает только exact GET и явный одноразовый cache-dir, без HOME/project fallback", () => {
  const directory = "/tmp/kodex-owner-ui-cache-aB123C";
  for (const [kind, name] of [
    ["ingress", "staff-control-center"],
    ["secret", "staff-control-center-public-tls"],
  ]) {
    assert.deepEqual(ownerTLSReadArguments(kind, name, directory), [
      "--kubeconfig",
      "/home/s/.kube/config",
      "--context",
      "k3d-kodex",
      "--namespace",
      "kodex-system",
      "--cache-dir",
      directory,
      "get",
      kind,
      name,
      "--output",
      "json",
    ]);
  }
  for (const [kind, name, path] of [
    ["secret", "other", directory],
    ["pods", "staff-control-center", directory],
    [
      "secret",
      "staff-control-center-public-tls",
      "/home/s/projects/kodex/.kube/cache",
    ],
  ])
    assert.throws(() => ownerTLSReadArguments(kind, name, path));
});

test("temporary cache0700: awaited cleanup только exact created uid/inode, включая ошибку GET", async () => {
  const directory = "/tmp/kodex-owner-ui-cache-aB123C";
  const info = () => ({
    isDirectory: () => true,
    isSymbolicLink: () => false,
    uid: 1000,
    mode: 0o40700,
    dev: 1,
    ino: 2,
  });
  for (const fail of [false, true]) {
    const trace = [];
    const io = {
      tmpdir: () => "/tmp",
      realpath: async (value) => value,
      mkdtemp: async (prefix) => {
        assert.equal(prefix, "/tmp/kodex-owner-ui-cache-");
        return directory;
      },
      lstat: async () => info(),
      uid: 1000,
      rm: async (path, options) => {
        trace.push("cleanup");
        assert.equal(path, directory);
        assert.deepEqual(options, {
          recursive: true,
          force: false,
          maxRetries: 0,
        });
      },
    };
    const result = withPrivateKubectlCache(async (path) => {
      assert.equal(path, directory);
      trace.push("read");
      if (fail) throw new Error("GET_FAILED");
      return "DONE";
    }, io);
    if (fail) await assert.rejects(result, /GET_FAILED/);
    else assert.equal(await result, "DONE");
    assert.deepEqual(trace, ["read", "cleanup"]);
  }
});

test("заменённый cache/symlink/foreign UID/broad mode не удаляются рекурсивно", async () => {
  for (const bad of ["INODE", "SYMLINK", "UID", "MODE"]) {
    let calls = 0,
      removed = false;
    const io = {
      tmpdir: () => "/tmp",
      realpath: async (value) => value,
      mkdtemp: async () => "/tmp/kodex-owner-ui-cache-aB123C",
      uid: 1000,
      lstat: async () => {
        calls++;
        return {
          isDirectory: () => true,
          isSymbolicLink: () => calls > 1 && bad === "SYMLINK",
          uid: calls > 1 && bad === "UID" ? 1001 : 1000,
          mode: calls > 1 && bad === "MODE" ? 0o40755 : 0o40700,
          dev: 1,
          ino: calls > 1 && bad === "INODE" ? 3 : 2,
        };
      },
      rm: async () => {
        removed = true;
      },
    };
    await assert.rejects(
      withPrivateKubectlCache(async () => {}, io),
      /OWNER_UI_INPUT_REJECTED/,
    );
    assert.equal(removed, false);
  }
});

test("project TMPDIR не создаёт каталог, native temporary cache имеет0700 и удалён до return", async () => {
  let created = false;
  await assert.rejects(
    withPrivateKubectlCache(async () => {}, {
      tmpdir: () => "/home/s/projects/kodex",
      realpath: async (value) => value,
      mkdtemp: async () => {
        created = true;
      },
    }),
    /OWNER_UI_INPUT_REJECTED/,
  );
  assert.equal(created, false);
  let path;
  await withPrivateKubectlCache(async (directory) => {
    path = directory;
    const info = await lstat(directory);
    assert.equal(info.mode & 0o777, 0o700);
    assert.equal(info.uid, process.getuid());
  });
  await assert.rejects(lstat(path), { code: "ENOENT" });
});

test("debug/keylog/inspector отклоняются до TLS и credential чтения", () => {
  assertInputEnvironment({ OTHER_UNSELECTED_SECRET: "SYNTHETIC" });
  for (const key of [
    "DEBUG",
    "PWDEBUG",
    "NODE_OPTIONS",
    "NODE_DEBUG",
    "NODE_DEBUG_NATIVE",
    "SSLKEYLOGFILE",
  ])
    assert.throws(() => assertInputEnvironment({ [key]: "enabled" }));
  assert.throws(() => assertInputEnvironment({}, ["--inspect"]));
});

test("request требует exact Origin/Host/SNI/path/TLS и body framing", () => {
  assert.equal(assertInputRequest(request(), port), "INPUT");
  const good = request();
  for (const change of [
    { method: "GET" },
    { url: `${inputPath}?token=x` },
    { headers: { ...good.headers, origin: "https://other.invalid" } },
    { headers: { ...good.headers, host: "127.0.0.2:23456" } },
    { headers: { ...good.headers, authorization: "Bearer SYNTHETIC" } },
    { headers: { ...good.headers, cookie: "SYNTHETIC" } },
    { headers: { ...good.headers, "content-length": "257" } },
    { headers: { ...good.headers, "transfer-encoding": "chunked" } },
    { headers: { ...good.headers, "content-type": "text/plain" } },
    { socket: { ...good.socket, servername: false } },
    { socket: { ...good.socket, getProtocol: () => "TLSv1.2" } },
    { socket: { ...good.socket, remoteAddress: "10.0.0.1" } },
  ])
    assert.throws(() => assertInputRequest(request(change), port));
  const duplicate = request();
  duplicate.rawHeaders.push("Origin", inputOrigin);
  assert.throws(() => assertInputRequest(duplicate, port));
});

test("preflight ограничен POST+Content-Type и exact private-network", async () => {
  const headers = {
    host: `${inputHost}:${port}`,
    origin: inputOrigin,
    "access-control-request-method": "POST",
    "access-control-request-headers": "content-type",
    "access-control-request-private-network": "true",
  };
  const f = fixture(),
    res = response();
  await f.handler(request({ method: "OPTIONS", headers }), res);
  assert.equal(res.status, 204);
  assert.equal(f.reads.length, 0);
  assert.equal(res.headers["Access-Control-Allow-Origin"], inputOrigin);
  assert.equal(res.headers["Access-Control-Allow-Private-Network"], "true");
  assert.equal(res.headers["Access-Control-Allow-Credentials"], undefined);
  for (const replacement of [
    { "access-control-request-method": "GET" },
    { "access-control-request-headers": "authorization" },
    { "access-control-request-private-network": "false" },
  ])
    assert.throws(() =>
      assertInputRequest(
        request({ method: "OPTIONS", headers: { ...headers, ...replacement } }),
        port,
      ),
    );
});

test("тело закрытое: размер, UTF8/дубликаты/дополнительные поля не допускаются", () => {
  parseInputBody(body());
  for (const content of [
    Buffer.alloc(257),
    Buffer.from("{}"),
    Buffer.from(JSON.stringify({ intent: inputIntent, password: "SYNTHETIC" })),
    Buffer.from(`{"intent":"${inputIntent}","intent":"${inputIntent}"}`),
    Buffer.from([255]),
    Buffer.from(`${body()}\n`),
  ])
    assert.throws(() => parseInputBody(content));
});

test("peer UID из единственной established клиентской reverse tuple, не listening/server row", () => {
  const socket = request().socket;
  const header =
    "sl local_address rem_address st tx_queue rx_queue tr uid timeout inode\n";
  const client = "0: 0100007F:B26E 0200007F:5BA0 01 0:0 00:0 0 1000 0 1";
  const server = "1: 0200007F:5BA0 0100007F:B26E 01 0:0 00:0 0 0 0 2";
  assertPeerUID(`${header}${client}\n${server}`, socket, 1000);
  for (const source of [
    header + server,
    header + client.replace("1000", "1001"),
    header + client.replace(" 01 ", " 0A "),
    `${header}${client}\n${client}`,
    header + client.replace("B26E", "B26F"),
  ])
    assert.throws(() => assertPeerUID(source, socket, 1000));
});

test("exact ingress→TLS Secret selector и canonical base64, без произвольного locator", () => {
  const ingress = {
    kind: "Ingress",
    metadata: { name: "staff-control-center", namespace: "kodex-system" },
    spec: {
      rules: [{ host: inputHost }],
      tls: [
        { hosts: [inputHost], secretName: "staff-control-center-public-tls" },
      ],
    },
  };
  parseIngress(JSON.stringify(ingress));
  assert.throws(() =>
    parseIngress(
      JSON.stringify({
        ...ingress,
        metadata: { ...ingress.metadata, name: "other" },
      }),
    ),
  );
  assert.throws(() =>
    parseIngress(
      JSON.stringify({
        ...ingress,
        spec: {
          ...ingress.spec,
          tls: [{ hosts: [inputHost], secretName: "other" }],
        },
      }),
    ),
  );
  const secret = {
    kind: "Secret",
    type: "kubernetes.io/tls",
    metadata: {
      name: "staff-control-center-public-tls",
      namespace: "kodex-system",
    },
    data: {
      "tls.crt": Buffer.from("SYNTHETIC_CERT").toString("base64"),
      "tls.key": Buffer.from("SYNTHETIC_KEY").toString("base64"),
    },
  };
  assert.equal(
    parseTLSSecret(JSON.stringify(secret)).key.toString(),
    "SYNTHETIC_KEY",
  );
  assert.throws(() =>
    parseTLSSecret(JSON.stringify({ ...secret, type: "Opaque" })),
  );
  assert.throws(() =>
    parseTLSSecret(
      JSON.stringify({
        ...secret,
        data: { ...secret.data, "tls.key": "not canonical!" },
      }),
    ),
  );
});

test("одна выдача: два peer checks до двух выбранных ключей, no-store, stdout только state", async () => {
  const trace = [];
  const f = fixture({
    readPeer: async () => trace.push("peer"),
    readSecrets: (keys) => {
      trace.push(keys);
      return {
        KODEX_LOCAL_OWNER_USERNAME: "SYNTHETIC_USER_SENTINEL",
        KODEX_LOCAL_OWNER_PASSWORD: "SYNTHETIC_PASSWORD_SENTINEL",
      };
    },
  });
  const res = response();
  await f.handler(request(), res);
  assert.deepEqual(trace, [
    "peer",
    "peer",
    ["KODEX_LOCAL_OWNER_USERNAME", "KODEX_LOCAL_OWNER_PASSWORD"],
  ]);
  assert.deepEqual(JSON.parse(res.body), {
    username: "SYNTHETIC_USER_SENTINEL",
    password: "SYNTHETIC_PASSWORD_SENTINEL",
  });
  assert.equal(res.headers["Cache-Control"], "no-store");
  assert.equal(res.headers["Connection"], "close");
  assert.deepEqual(f.states, [{ state: "CONSUMED" }]);
  assert.equal(f.closed(), 1);
  const replay = response();
  await f.handler(request(), replay);
  assert.equal(replay.status, 403);
  assert.equal(trace.length, 3);
});

test("foreign UID/expiry/oversize/unknown intent отклоняются до чтения ключей", async () => {
  for (const options of [
    {
      readPeer: async () => {
        throw new Error("SYNTHETIC_PASSWORD_SENTINEL");
      },
    },
    { now: () => 60000 },
  ]) {
    const f = fixture(options),
      res = response();
    await f.handler(request(), res);
    assert.equal(res.status, 403);
    assert.equal(f.reads.length, 0);
    assert.equal(res.body, undefined);
  }
  for (const content of [Buffer.alloc(257), Buffer.from("{}")]) {
    const f = fixture(),
      res = response();
    await f.handler(request({}, content), res);
    assert.equal(res.status, 403);
    assert.equal(f.reads.length, 0);
  }
});

test("конкурентные POST и исчезнувший peer не дают второй выдачи", async () => {
  const f = fixture(),
    a = response(),
    b = response();
  await Promise.all([f.handler(request(), a), f.handler(request(), b)]);
  assert.equal(f.reads.length, 1);
  assert.equal([a, b].filter((res) => res.status === 403).length, 1);
  let checks = 0;
  const lost = fixture({
      readPeer: async () => {
        if (++checks === 2) throw new Error("REJECTED");
      },
    }),
    res = response();
  await lost.handler(request(), res);
  assert.equal(lost.reads.length, 0);
  assert.equal(lost.closed(), 1);
});

test("browser snippet содержит только publicport и fixedform, результат не возвращает credential", () => {
  const script = browserInputScript(port);
  assert.ok(
    script.includes(inputOrigin) &&
      script.includes(`#username`) &&
      script.includes(`#password`) &&
      script.includes(`#kc-login`),
  );
  assert.ok(
    script.includes('credentials:"omit"') &&
      script.includes('cache:"no-store"') &&
      script.includes('redirect:"error"'),
  );
  assert.ok(
    !script.includes("SYNTHETIC_PASSWORD_SENTINEL") &&
      !script.includes("console.") &&
      !script.includes("localStorage") &&
      !script.includes("document.cookie"),
  );
  assert.ok(
    script.includes("return true") &&
      script.includes("return false") &&
      !script.includes("return value") &&
      !script.includes("return {"),
  );
  assert.throws(() => browserInputScript(0));
});

test("native form только exact SSO: Boolean результат и failclosed CSP/чужая форма", async () => {
  for (const state of [
    "VALID",
    "FOREIGN",
    "CSP",
    "FORM",
    "NAV",
    "SWAP",
    "ACTION",
    "DISABLED",
    "EMPTY",
    "OVERSIZE",
  ]) {
    let clicked = 0;
    const form = {
      action: `${inputOrigin}/realms/kodex/login-actions/authenticate`,
      method: "post",
    };
    class Input {
      value = "";
      disabled = false;
      form = form;
      constructor(type) {
        this.type = type;
      }
      dispatchEvent() {}
    }
    const username = new Input("text"),
      password = new Input("password");
    const button = {
      disabled: false,
      form: state === "FORM" ? {} : form,
      click: () => {
        clicked++;
      },
    };
    const location = {
      origin: state === "FOREIGN" ? "https://foreign.invalid" : inputOrigin,
    };
    let present = true;
    const invoke = runInNewContext(`(${browserInputScript(port)})`, {
      location,
      document: {
        contains: () => present,
        querySelector: (selector) =>
          ({
            "#username": username,
            "#password": password,
            "#kc-login": button,
          })[selector],
      },
      HTMLInputElement: Input,
      Event: class {},
      URL,
      fetch: async (url, init) => {
        assert.equal(url, `https://${inputHost}:${port}${inputPath}`);
        assert.equal(init.credentials, "omit");
        if (state === "CSP") throw new Error("SYNTHETIC_SECRET_NEVER_RETURN");
        if (state === "NAV") location.origin = "https://foreign.invalid";
        if (state === "SWAP") present = false;
        if (state === "ACTION") form.action += "&changed=1";
        if (state === "DISABLED") password.disabled = true;
        return {
          ok: true,
          json: async () => ({
            username: "SYNTHETIC_USER",
            password:
              state === "EMPTY"
                ? ""
                : state === "OVERSIZE"
                  ? "x".repeat(16385)
                  : "SYNTHETIC_PASSWORD",
          }),
        };
      },
    });
    assert.equal(await invoke(), state === "VALID");
    assert.equal(clicked, state === "VALID" ? 1 : 0);
    assert.equal(password.value, state === "VALID" ? "SYNTHETIC_PASSWORD" : "");
  }
});

test("password-only reauth требует неизменную readonly owner identity и ту же native форму", async () => {
  for (const state of [
    "VALID",
    "MISMATCH",
    "CASE_MISMATCH",
    "READONLY",
    "ATTRIBUTE",
    "IDENTITY_FORM",
    "IDENTITY_TYPE",
    "FORM_ID",
    "FOREIGN_ACTION",
    "CSP",
    "RESPONSE",
    "JSON",
    "FETCH_IDENTITY",
    "FETCH_PASSWORD",
    "FETCH_READONLY",
    "FETCH_ATTRIBUTE",
    "FETCH_SWAP_IDENTITY",
    "FETCH_SWAP_PASSWORD",
    "FETCH_SWAP_BUTTON",
    "FETCH_FORM",
    "FETCH_ACTION",
    "FETCH_METHOD",
    "FETCH_ORIGIN",
    "FETCH_USERNAME",
    "JSON_SWAP_IDENTITY",
    "EVENT_IDENTITY",
    "EVENT_PASSWORD",
    "EVENT_SWAP_PASSWORD",
    "EVENT_ACTION",
    "EVENT_THROW",
    "CLICK_THROW",
  ]) {
    let clicked = 0,
      fetched = 0;
    const events = [];
    const form = {
      id: state === "FORM_ID" ? "other-form" : "kc-form-login",
      action: `${inputOrigin}/realms/kodex/login-actions/authenticate`,
      method: "post",
    };
    const location = { origin: inputOrigin };
    class Input {
      value = "";
      disabled = false;
      readOnly = false;
      form = form;
      constructor(type) {
        this.type = type;
      }
      hasAttribute(name) {
        return name === "readonly" && this.readOnlyAttribute === true;
      }
      dispatchEvent(event) {
        events.push(event.type);
        assert.equal(this, password);
        if (state === "EVENT_IDENTITY") attempted.value = "SYNTHETIC_OTHER";
        if (state === "EVENT_PASSWORD") password.value = "SYNTHETIC_OTHER";
        if (state === "EVENT_SWAP_PASSWORD")
          elements["#password"] = new Input("password");
        if (state === "EVENT_ACTION") form.action += "?changed=1";
        if (state === "EVENT_THROW")
          throw new Error("SYNTHETIC_SECRET_NEVER_RETURN");
      }
    }
    const attempted = new Input("text"),
      password = new Input("password");
    attempted.form = null;
    attempted.readOnly = true;
    attempted.readOnlyAttribute = true;
    attempted.value =
      state === "MISMATCH"
        ? "SYNTHETIC_OTHER"
        : state === "CASE_MISMATCH"
          ? "synthetic_user"
          : "SYNTHETIC_USER";
    const button = {
      disabled: false,
      form,
      click: () => {
        if (state === "CLICK_THROW")
          throw new Error("SYNTHETIC_SECRET_NEVER_RETURN");
        clicked++;
      },
    };
    const elements = {
      "#username": null,
      "#kc-attempted-username": attempted,
      "#password": password,
      "#kc-login": button,
      "#kc-form-login": form,
    };
    if (state === "READONLY") attempted.readOnly = false;
    if (state === "ATTRIBUTE") attempted.readOnlyAttribute = false;
    if (state === "IDENTITY_FORM") attempted.form = form;
    if (state === "IDENTITY_TYPE") attempted.type = "hidden";
    if (state === "FOREIGN_ACTION")
      form.action = "https://foreign.invalid/login";
    const value = {
      username: "SYNTHETIC_USER",
      password: "SYNTHETIC_PASSWORD",
    };
    const invoke = runInNewContext(`(${browserInputScript(port)})`, {
      location,
      document: {
        contains: (element) => Object.values(elements).includes(element),
        querySelector: (selector) => elements[selector] ?? null,
      },
      HTMLInputElement: Input,
      Event: class {
        constructor(type) {
          this.type = type;
        }
      },
      URL,
      fetch: async () => {
        fetched++;
        if (state === "CSP") throw new Error("SYNTHETIC_SECRET_NEVER_RETURN");
        if (state === "FETCH_IDENTITY") attempted.value = "SYNTHETIC_OTHER";
        if (state === "FETCH_PASSWORD") password.value = "SYNTHETIC_USER_INPUT";
        if (state === "FETCH_READONLY") attempted.readOnly = false;
        if (state === "FETCH_ATTRIBUTE") attempted.readOnlyAttribute = false;
        if (state === "FETCH_SWAP_IDENTITY")
          elements["#kc-attempted-username"] = new Input("text");
        if (state === "FETCH_SWAP_PASSWORD")
          elements["#password"] = new Input("password");
        if (state === "FETCH_SWAP_BUTTON")
          elements["#kc-login"] = { ...button };
        if (state === "FETCH_FORM") {
          password.form = { ...form };
          button.form = password.form;
          elements["#kc-form-login"] = password.form;
        }
        if (state === "FETCH_ACTION") form.action += "?changed=1";
        if (state === "FETCH_METHOD") form.method = "get";
        if (state === "FETCH_ORIGIN")
          location.origin = "https://foreign.invalid";
        if (state === "FETCH_USERNAME")
          elements["#username"] = new Input("text");
        return {
          ok: state !== "RESPONSE",
          json: async () => {
            if (state === "JSON")
              throw new Error("SYNTHETIC_SECRET_NEVER_RETURN");
            if (state === "JSON_SWAP_IDENTITY")
              elements["#kc-attempted-username"] = new Input("text");
            return value;
          },
        };
      },
    });
    assert.equal(await invoke(), state === "VALID", state);
    assert.equal(clicked, state === "VALID" ? 1 : 0, state);
    assert.equal(
      password.value,
      state === "VALID"
        ? "SYNTHETIC_PASSWORD"
        : state === "FETCH_PASSWORD"
          ? "SYNTHETIC_USER_INPUT"
          : "",
      state,
    );
    assert.equal(
      attempted.value,
      state === "MISMATCH"
        ? "SYNTHETIC_OTHER"
        : state === "CASE_MISMATCH"
          ? "synthetic_user"
          : ["FETCH_IDENTITY", "EVENT_IDENTITY"].includes(state)
            ? "SYNTHETIC_OTHER"
            : "SYNTHETIC_USER",
      state,
    );
    if (state === "VALID") assert.deepEqual(events, ["input", "change"]);
    if (
      [
        "READONLY",
        "ATTRIBUTE",
        "IDENTITY_FORM",
        "IDENTITY_TYPE",
        "FORM_ID",
        "FOREIGN_ACTION",
      ].includes(state)
    )
      assert.equal(fetched, 0, state);
    if (fetched && !["CSP", "RESPONSE", "JSON"].includes(state))
      assert.deepEqual(value, { username: "", password: "" }, state);
  }
});

test("raw exception чтения ключей не утекает, истечение после peer check закрывает выдачу", async () => {
  const f = fixture({
      readSecrets: () => {
        throw new Error("SYNTHETIC_PASSWORD_SENTINEL");
      },
    }),
    res = response();
  await f.handler(request(), res);
  assert.equal(res.status, 403);
  assert.equal(res.body, undefined);
  assert.deepEqual(f.states, []);
  assert.equal(f.closed(), 1);
  let time = 1000;
  const expired = fixture({
      now: () => time,
      readPeer: async () => {
        time = 60000;
      },
    }),
    timeout = response();
  await expired.handler(request(), timeout);
  assert.equal(timeout.status, 403);
  assert.equal(expired.reads.length, 0);
});
