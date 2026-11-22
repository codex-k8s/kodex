import assert from "node:assert/strict";
import { once } from "node:events";
import { connect } from "node:net";
import { setTimeout as delay } from "node:timers/promises";
import { test } from "node:test";
import {
  input,
  connection,
  page,
  submission,
} from "../../services/staff/control-center/src/shared/api/kubernetes-port-forward-fixtures.test-support.ts";
import { createPortForwardBridge } from "./managed-kubernetes-port-forward-bridge.mjs";

async function ownedBridge(t, options) {
  const bridge = await createPortForwardBridge(options);
  t.after(() => bridge.stop());
  return bridge;
}
function ownedSocket(t, options) {
  const socket = connect(options);
  t.after(() => socket.destroy());
  return socket;
}
function fixture() {
  let view = input(),
    sequence = 0,
    ordinal = 0n,
    conn,
    outputs = [],
    submitted = 0,
    opens = 0,
    closes = 0;
  let drained;
  const drainedPromise = new Promise((resolve) => {
    drained = resolve;
  });
  const keys = [];
  const control = {
    uncertain: false,
    rejectProfile: false,
    versionConflicts: 0,
    outputFlood: false,
  };
  function emit(action, body = Buffer.alloc(0)) {
    const wire = Buffer.alloc(51 + body.length);
    wire.set([75, 80, 70, 1, action, 0]);
    wire.writeBigUInt64BE(BigInt(conn.generation), 6);
    wire[14] = 28;
    wire.write(conn.connectionRef, 15);
    wire.writeBigUInt64BE(++ordinal, 43);
    body.copy(wire, 51);
    outputs.push(wire);
  }
  const client = {
    async synchronizeClock() {},
    serverNow: () => Date.now(),
    async listConnections() {
      return {
        ...page(),
        profile: {
          ...page().profile,
          protocol: control.rejectProfile
            ? "PORT_FORWARD_WEBSOCKET_V4"
            : "PORT_FORWARD_SPDY_V1",
        },
      };
    },
    async readInput(_input, _signal, key) {
      return {
        view,
        ...(key
          ? {
              submission: {
                ...submission(),
                sequence,
                state: control.uncertain ? "UNKNOWN" : "ACKNOWLEDGED",
              },
            }
          : {}),
      };
    },
    async open(_input, slot) {
      opens++;
      ordinal = 0n;
      conn = {
        ...connection(),
        connectionRef: "kpf_" + String(opens).padStart(24, "0"),
        generation: opens,
        slot,
      };
      view = { ...view, version: view.version + 1 };
      return {
        input: view,
        connection: conn,
        submission: {
          ...submission(),
          sequence: ++sequence,
          state: "ACKNOWLEDGED",
        },
      };
    },
    async getConnection() {
      return { connection: conn };
    },
    async close() {
      closes++;
      conn = { ...conn, state: "CLOSED" };
      return { input: view, connection: conn };
    },
    async submit(_input, bytes, key) {
      submitted++;
      keys.push(key);
      if (control.versionConflicts > 0) {
        control.versionConflicts--;
        view = { ...view, version: view.version + 1 };
        bytes.fill(0);
        throw Object.assign(Error("pre-intent OCC"), {
          code: "VERSION_MISMATCH",
          status: 409,
        });
      }
      if (control.uncertain) {
        bytes.fill(0);
        throw Object.assign(Error("uncertain"), { code: "REQUEST_UNKNOWN" });
      }
      const action = bytes[4];
      if (action === 2 && control.outputFlood) {
        for (let index = 0; index < 160; index++)
          emit(2, Buffer.alloc(32717, 88));
      } else if (action === 2)
        emit(
          2,
          Buffer.from(bytes.subarray(51)).map((b) =>
            b >= 97 && b <= 122 ? b - 32 : b,
          ),
        );
      else {
        emit(3);
        emit(4);
        conn = {
          ...conn,
          state: "CLOSED",
          nativeJoined: true,
          readEndObserved: true,
        };
      }
      bytes.fill(0);
      view = { ...view, version: view.version + 1 };
      return {
        view,
        submission: {
          ...submission(),
          sequence: ++sequence,
          state: "ACKNOWLEDGED",
        },
      };
    },
    async readOutput(_input, after, signal) {
      if (after === 3n) drained();
      await delay(5, undefined, { signal });
      const payload = outputs.shift();
      return payload
        ? {
            status: "FRAME",
            payload,
            sequence: after + 1n,
            deadline: Date.now() + 4000,
          }
        : { status: "WAITING" };
    },
  };
  return {
    client,
    control,
    drained: drainedPromise,
    keys,
    counters: () => ({ opens, submitted, closes }),
    view: () => view,
  };
}

test(
  "real loopback socket uses exact owner OPEN, ordered input and native output; EOF remains half-close",
  { timeout: 5000 },
  async (t) => {
    const f = fixture(),
      bridge = await ownedBridge(t, {
        client: f.client,
        input: f.view(),
        mappings: [{ slot: 0, localPort: 0 }],
      });
    assert.equal(bridge.addresses[0].host, "127.0.0.1");
    const socket = ownedSocket(t, {
      host: "127.0.0.1",
      port: bridge.addresses[0].localPort,
      allowHalfOpen: true,
    });
    const received = [];
    socket.on("data", (body) => received.push(body));
    await once(socket, "connect");
    socket.end("hello");
    await once(socket, "close");
    await f.drained;
    const result = await bridge.stop();
    assert.equal(Buffer.concat(received).toString(), "HELLO");
    assert.equal(f.counters().opens, 1);
    assert.equal(f.counters().submitted, 2);
    assert.equal(result.deliveryComplete, true);
    assert.equal(result.nativeConnectionsJoined, true);
    assert.equal(result.afterSequence, "3");
  },
);

test(
  "unsupported WS profile refuses before listening or owner OPEN",
  { timeout: 1000 },
  async (t) => {
    const f = fixture();
    f.control.rejectProfile = true;
    await assert.rejects(
      ownedBridge(t, {
        client: f.client,
        input: f.view(),
        mappings: [{ slot: 0, localPort: 0 }],
      }),
    );
    assert.equal(f.counters().opens, 0);
  },
);

test(
  "explicit shutdown joins listener and sockets without claiming an unobserved native join",
  { timeout: 5000 },
  async (t) => {
    const f = fixture(),
      bridge = await ownedBridge(t, {
        client: f.client,
        input: f.view(),
        mappings: [{ slot: 0, localPort: 0 }],
      });
    const socket = ownedSocket(t, {
      host: "127.0.0.1",
      port: bridge.addresses[0].localPort,
    });
    socket.on("error", () => {});
    await once(socket, "connect");
    await delay(30);
    const result = await bridge.stop();
    assert.equal(result.nativeConnectionsJoined, false);
    assert.equal(f.counters().closes, 1);
    await new Promise((resolve) => {
      const retry = ownedSocket(t, {
        host: "127.0.0.1",
        port: bridge.addresses[0].localPort,
      });
      retry.once("error", (error) => {
        assert.equal(error.code, "ECONNREFUSED");
        resolve();
      });
      retry.once("connect", () => {
        retry.destroy();
        assert.fail("owned listener survived join");
      });
    });
  },
);

test(
  "repeated sequential TCP connections do not acquire a lifetime count cap",
  { timeout: 10000 },
  async (t) => {
    const f = fixture(),
      bridge = await ownedBridge(t, {
        client: f.client,
        input: f.view(),
        mappings: [{ slot: 0, localPort: 0 }],
      });
    for (let index = 0; index < 20; index++) {
      const socket = ownedSocket(t, {
        host: "127.0.0.1",
        port: bridge.addresses[0].localPort,
        allowHalfOpen: true,
      });
      const chunks = [];
      socket.on("data", (part) => chunks.push(part));
      await once(socket, "connect");
      socket.end("x");
      await once(socket, "close");
      assert.equal(Buffer.concat(chunks).toString(), "X");
      // Ждём owner read после CLOSE; socket EOF не является native receipt.
      await delay(10);
    }
    const result = await bridge.stop();
    assert.equal(f.counters().opens, 20);
    assert.equal(result.deliveryComplete, true);
  },
);

test(
  "uncertain input resolves metadata once and never replays TCP bytes",
  { timeout: 5000 },
  async (t) => {
    const f = fixture(),
      bridge = await ownedBridge(t, {
        client: f.client,
        input: f.view(),
        mappings: [{ slot: 0, localPort: 0 }],
      });
    const socket = ownedSocket(t, {
      host: "127.0.0.1",
      port: bridge.addresses[0].localPort,
    });
    socket.on("error", () => {});
    await once(socket, "connect");
    await delay(20);
    f.control.uncertain = true;
    socket.write("x");
    await delay(80);
    const result = await bridge.stop();
    assert.equal(f.counters().submitted, 1);
    assert.equal(result.deliveryComplete, false);
    assert.equal(result.nativeConnectionsJoined, false);
  },
);

test(
  "only definitive pre-intent VERSION_MISMATCH retries the unchanged input key",
  { timeout: 5000 },
  async (t) => {
    const f = fixture(),
      bridge = await ownedBridge(t, {
        client: f.client,
        input: f.view(),
        mappings: [{ slot: 0, localPort: 0 }],
      });
    const socket = ownedSocket(t, {
        host: "127.0.0.1",
        port: bridge.addresses[0].localPort,
        allowHalfOpen: true,
      }),
      chunks = [];
    socket.on("data", (body) => chunks.push(body));
    await once(socket, "connect");
    f.control.versionConflicts = 2;
    socket.end("hello");
    await once(socket, "close");
    await f.drained;
    const result = await bridge.stop();
    assert.equal(Buffer.concat(chunks).toString(), "HELLO");
    assert.equal(f.counters().submitted, 4);
    assert.equal(new Set(f.keys.slice(0, 3)).size, 1);
    assert.equal(result.deliveryComplete, true);
  },
);

test(
  "sixteen simultaneous loopback sockets bound admission without a sequential lifetime limit",
  { timeout: 5000 },
  async (t) => {
    const f = fixture(),
      bridge = await ownedBridge(t, {
        client: f.client,
        input: f.view(),
        mappings: [{ slot: 0, localPort: 0 }],
      });
    const sockets = [];
    for (let index = 0; index < 17; index++) {
      const socket = ownedSocket(t, {
        host: "127.0.0.1",
        port: bridge.addresses[0].localPort,
      });
      socket.on("error", () => {});
      sockets.push(socket);
      await once(socket, "connect");
    }
    await delay(50);
    assert.equal(f.counters().opens, 16);
    const result = await bridge.stop();
    for (const socket of sockets) socket.destroy();
    assert.equal(result.nativeConnectionsJoined, false);
  },
);

test(
  "actual TCP backpressure expires a socket write without replay or a positive delivery result",
  { timeout: 8000 },
  async (t) => {
    const f = fixture();
    f.control.outputFlood = true;
    f.view().maximumTotalBytes = 8 * 1024 * 1024;
    const originalRead = f.client.readOutput;
    f.client.readOutput = async (...args) => {
      const result = await originalRead(...args);
      if (result.status === "FRAME") result.deadline = Date.now() + 25;
      return result;
    };
    const bridge = await ownedBridge(t, {
      client: f.client,
      input: f.view(),
      mappings: [{ slot: 0, localPort: 0 }],
    });
    const socket = ownedSocket(t, {
      host: "127.0.0.1",
      port: bridge.addresses[0].localPort,
      allowHalfOpen: true,
    });
    socket.on("error", () => {});
    await once(socket, "connect");
    socket.pause();
    socket.write("trigger");
    // Остановленный peer препятствует completion после заполнения настоящих kernel buffers.
    await delay(2200);
    assert.equal(
      f.counters().closes,
      1,
      "write failure must close the exact owner connection before explicit shutdown",
    );
    const result = await bridge.stop();
    socket.destroy();
    assert.equal(result.deliveryComplete, false);
    assert.equal(f.counters().submitted, 1);
    assert.ok(f.counters().closes <= 1);
  },
);
