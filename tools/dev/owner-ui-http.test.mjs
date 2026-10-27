import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";
import {
  attachInputHandler,
  closeInputServer,
  configureInputServer,
  completeInputPreflight,
  readInputBody,
  setInputResponseHeaders,
  singleInputHeader,
} from "./owner-ui-http.mjs";

const requireInput = (valid) => {
  if (!valid) throw new Error("SYNTHETIC_INPUT_REJECTED");
};

test("единственный raw header сохраняет регистр значения и caller-owned отказ", () => {
  assert.equal(
    singleInputHeader(
      { rawHeaders: ["Origin", "ExactValue"] },
      "origin",
      requireInput,
    ),
    "ExactValue",
  );
  for (const rawHeaders of [[], ["Origin", "one", "ORIGIN", "two"]])
    assert.throws(
      () => singleInputHeader({ rawHeaders }, "origin", requireInput),
      /^Error: SYNTHETIC_INPUT_REJECTED$/,
    );
});

test("ограниченный body сохраняет байты, считает все chunks и не разбирает intent", async () => {
  const input = [Buffer.from("untrusted\0"), Buffer.from("body")];
  const result = await readInputBody(input, 14, requireInput);
  assert.equal(result.size, 14);
  assert.deepEqual(result.body, Buffer.concat(input));
  result.body.fill(0);
  assert.equal(
    result.body.every((byte) => byte === 0),
    true,
  );
  await assert.rejects(
    readInputBody(input, 13, requireInput),
    /^Error: SYNTHETIC_INPUT_REJECTED$/,
  );
  const empty = await readInputBody([], 0, requireInput);
  assert.equal(empty.size, 0);
  assert.equal(empty.body.length, 0);
  const failure = new Error("synthetic-stream-failure");
  await assert.rejects(
    readInputBody(
      (async function* () {
        throw failure;
      })(),
      10,
      requireInput,
    ),
    (error) => error === failure,
  );
});

test("preflight не выдаёт body и добавляет private-network только при exact true", () => {
  for (const value of [undefined, "true", "false", "TRUE"]) {
    const headers = {};
    const events = [];
    const response = {
      setHeader: (name, text) => {
        headers[name] = text;
      },
      writeHead: (status) => events.push(status),
      end: (...args) => events.push(args),
    };
    setInputResponseHeaders(response, "https://fixture.invalid");
    completeInputPreflight(
      { headers: { "access-control-request-private-network": value } },
      response,
    );
    assert.equal(
      headers["Access-Control-Allow-Origin"],
      "https://fixture.invalid",
    );
    assert.equal(headers["Cache-Control"], "no-store");
    assert.equal(headers.Vary, "Origin");
    assert.equal(headers.Pragma, "no-cache");
    assert.equal(headers["Access-Control-Allow-Methods"], "POST");
    assert.equal(headers["Access-Control-Allow-Headers"], "Content-Type");
    assert.equal(
      headers["Access-Control-Allow-Private-Network"],
      value === "true" ? "true" : undefined,
    );
    assert.deepEqual(events, [204, []]);
  }
});

test("HTTP timeouts прежние, TLS client error тихий, upgrade socket закрывается", () => {
  const server = new EventEmitter();
  configureInputServer(server);
  assert.equal(server.requestTimeout, 5000);
  assert.equal(server.headersTimeout, 5000);
  assert.equal(server.keepAliveTimeout, 1000);
  assert.equal(
    server.emit("tlsClientError", new Error("synthetic TLS error")),
    true,
  );
  let destroyed = 0;
  server.emit("upgrade", {}, { destroy: () => destroyed++ });
  assert.equal(destroyed, 1);
  assert.equal(server.listenerCount("request"), 0);
});

test("async handler получает исходные request/response один раз без закрытия", async () => {
  const server = new EventEmitter();
  const request = {};
  const response = {};
  const received = [];
  let closed = 0;
  attachInputHandler(
    server,
    (...args) => {
      received.push(args);
      return Promise.resolve();
    },
    () => closed++,
  );
  server.emit("request", request, response);
  await Promise.resolve();
  assert.equal(received.length, 1);
  assert.equal(received[0][0], request);
  assert.equal(received[0][1], response);
  assert.equal(closed, 0);
});

test("rejected handler сначала уничтожает socket, затем закрывает форму без retry", async () => {
  const server = new EventEmitter();
  const events = [];
  let called = 0;
  attachInputHandler(
    server,
    () => {
      called++;
      return Promise.reject(new Error("synthetic handler failure"));
    },
    () => events.push("close"),
  );
  server.emit(
    "request",
    { socket: { destroy: () => events.push("destroy") } },
    {},
  );
  await Promise.resolve();
  assert.equal(called, 1);
  assert.deepEqual(events, ["destroy", "close"]);
});

test("синхронный throw handler сохраняет native throw, без автоматического повтора", () => {
  const server = new EventEmitter();
  const failure = new Error("synthetic synchronous handler failure");
  let closed = 0;
  attachInputHandler(
    server,
    () => {
      throw failure;
    },
    () => closed++,
  );
  assert.throws(
    () => server.emit("request", {}, {}),
    (error) => error === failure,
  );
  assert.equal(closed, 0);
});

test("graceful close отменяет expiry и force до позднего timer tick", async (context) => {
  context.mock.timers.enable({ apis: ["setTimeout"] });
  const events = [];
  const expiry = setTimeout(() => events.push("expired"), 60000);
  const server = {
    close: (callback) => {
      events.push("close");
      callback();
    },
    closeIdleConnections: () => events.push("idle"),
    closeAllConnections: () => events.push("all"),
  };
  const sockets = new Set([{ destroy: () => events.push("socket") }]);
  await closeInputServer(server, sockets, expiry);
  context.mock.timers.tick(60000);
  assert.deepEqual(events, ["close", "idle"]);
});

test("force ровно через секунду закрывает только живые sockets и ждёт close callback", async (context) => {
  context.mock.timers.enable({ apis: ["setTimeout"] });
  const events = [];
  const expiry = setTimeout(() => events.push("expired"), 60000);
  let finish;
  let completed = false;
  const server = {
    close: (callback) => {
      events.push("close");
      finish = callback;
    },
    closeIdleConnections: () => events.push("idle"),
    closeAllConnections: () => events.push("all"),
  };
  const removed = { destroy: () => events.push("removed") };
  const sockets = new Set([removed]);
  const closing = closeInputServer(server, sockets, expiry).then(() => {
    completed = true;
  });
  sockets.delete(removed);
  sockets.add({ destroy: () => events.push("remaining") });
  context.mock.timers.tick(999);
  assert.deepEqual(events, ["close", "idle"]);
  context.mock.timers.tick(1);
  assert.deepEqual(events, ["close", "idle", "all", "remaining"]);
  assert.equal(completed, false);
  finish();
  await closing;
  context.mock.timers.tick(60000);
  assert.equal(completed, true);
  assert.deepEqual(events, ["close", "idle", "all", "remaining"]);
});
