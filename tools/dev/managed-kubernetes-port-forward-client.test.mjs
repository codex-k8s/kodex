import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { inspect } from "node:util";
import { test } from "node:test";
import {
  input as baseInput,
  connection,
  page,
  submission,
} from "../../services/staff/control-center/src/shared/api/kubernetes-port-forward-fixtures.test-support.ts";
import { encodePortForwardInput } from "../../services/staff/control-center/src/shared/lib/kubernetes-port-forward-frame.ts";
import {
  createPortForwardOwnerClient,
  parsePortForwardInputView,
  parsePortForwardOutput,
  parsePortForwardProfile,
} from "./managed-kubernetes-port-forward-client.mjs";

const input = () => ({
  ...baseInput(),
  maximumStagedBytes: 1073741824,
  maximumStagedObjects: 32768,
});

const origin = "https://control.disposable.invalid";
const storage = {
  runtimeOrigin: origin,
  cookies: ["__Host-kodex-session", "__Host-kodex-csrf"].map((name, index) => ({
    name,
    value: index ? "1".repeat(43) : "v1." + "1".repeat(64),
    domain: "control.disposable.invalid",
    path: "/",
    secure: true,
    httpOnly: index === 0,
    sameSite: "Strict",
    expires: -1,
  })),
  origins: [],
};
const json = (value, status = 200, type = "application/json") =>
  new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": type, "Cache-Control": "no-store" },
  });
function fixture(handler) {
  const calls = [];
  const client = createPortForwardOwnerClient({
    origin,
    storage,
    fetchAPI: async (url, options) => {
      assert.equal(url.origin, origin);
      assert.equal(
        options.redirect,
        url.pathname === "/api/v1/session" && calls.length ? "manual" : "error",
      );
      const headers = new Headers(options.headers);
      assert.equal(headers.get("origin"), origin);
      assert.equal(headers.get("authorization"), null);
      assert.ok(headers.get("cookie"));
      assert.ok(options.signal instanceof AbortSignal);
      calls.push({ url, options });
      if (url.pathname === "/api/v1/session") {
        const now = Date.now();
        return json({
          generation: "11111111-1111-4111-8111-111111111111",
          version: 1,
          sessionRevision: 1,
          renewalMode: "BACKEND_REFRESH",
          serverTime: new Date(now).toISOString(),
          accessExpiresAt: new Date(now + 300000).toISOString(),
          expiresAt: new Date(now + 1800000).toISOString(),
          absoluteExpiresAt: new Date(now + 3600000).toISOString(),
          renewAfter: new Date(now + 180000).toISOString(),
        });
      }
      return handler(url, options);
    },
  });
  return { client, calls };
}

test("owner metadata rejects changed immutable graph, malformed resource and unknown options", () => {
  const initial = input();
  assert.equal(parsePortForwardInputView(initial, initial), initial);
  for (const mutate of [
    (v) => (v.targetRef = "foreign.target"),
    (v) => (v.resource.uid = "not-a-uuid"),
    (v) => (v.extra = "unknown"),
    (v) => (v.maximumFrameBytes = 32769),
    (v) => (v.inputChannels = ["STDIN"]),
    (v) => delete v.maximumStagedBytes,
    (v) => (v.maximumStagedObjects = 32769),
    (v) => (v.maximumStagedBytes = 1073741825),
  ]) {
    const v = structuredClone(initial);
    mutate(v);
    assert.throws(() => parsePortForwardInputView(v, initial, initial));
  }
  for (const targetRef of ["registered.target:prod", "кластер-prod"]) {
    const opaque = { ...initial, targetRef };
    assert.equal(parsePortForwardInputView(opaque, opaque), opaque);
  }
  for (const targetRef of ["bad\nref", "x".repeat(97), "unpaired\ud800"]) {
    assert.throws(() =>
      parsePortForwardInputView({ ...initial, targetRef }, initial),
    );
  }
  const current = { ...initial, version: 5, acknowledgedSequence: 3 };
  assert.throws(() => parsePortForwardInputView(initial, current, current));
});

test("profile exposes exact approved mappings and explicit active quota", () => {
  const profile = page().profile;
  assert.deepEqual(parsePortForwardProfile(profile), profile);
  for (const mutate of [
    (v) => (v.maximumActiveConnections = 17),
    (v) => (v.ports[0].targetPort = 0),
    (v) => v.ports.push({ ...v.ports[0] }),
    (v) => (v.remoteURL = "https://foreign.invalid"),
    (v) => (v.admissionReasonCode = "RAW_PROVIDER_ERROR"),
    (v) => (v.admissionReasonCode = "KUBERNETES_PORT_FORWARD_CAPACITY"),
  ]) {
    const v = structuredClone(profile);
    mutate(v);
    assert.throws(() => parsePortForwardProfile(v));
  }
});

test("actual session transport performs one typed mutation and clears exclusive input bytes", async () => {
  const initial = input(),
    bytes = encodePortForwardInput(connection(), Uint8Array.of(42, 43)),
    hash = createHash("sha256").update(bytes).digest("hex");
  const f = fixture((url, options) => {
    assert.ok(url.pathname.endsWith("/input"));
    assert.equal(options.method, "POST");
    assert.equal(new Headers(options.headers).get("if-match"), '"4"');
    assert.equal(
      new Headers(options.headers).get("idempotency-key"),
      "submit-key-0001",
    );
    const body = JSON.parse(options.body);
    assert.equal(body.payloadDigest, hash);
    assert.equal(body.endOfInput, false);
    return json(
      {
        view: { ...initial, version: 5 },
        submission: {
          ...submission(),
          payloadDigest: hash,
          byteCount: bytes.length,
        },
      },
      201,
    );
  });
  const result = await f.client.submit(initial, bytes, "submit-key-0001");
  assert.equal(result.view.version, 5);
  assert.equal(
    bytes.every((b) => b === 0),
    true,
  );
  assert.equal(f.calls.filter((c) => c.options.method === "POST").length, 1);
});

test("lost mutation ACK is never retried, and caller buffer is cleared", async () => {
  const initial = input(),
    bytes = encodePortForwardInput(connection(), Uint8Array.of(7));
  const f = fixture(() => {
    throw Error("private upstream diagnostic");
  });
  await assert.rejects(
    f.client.submit(initial, bytes, "submit-key-0002"),
    (error) =>
      error.code === "REQUEST_UNKNOWN" && !error.message.includes("private"),
  );
  assert.equal(
    bytes.every((b) => b === 0),
    true,
  );
  assert.equal(f.calls.filter((c) => c.options.method === "POST").length, 1);
});

test("wrong success status or unregistered error diagnostics never become accepted mutation", async () => {
  for (const response of [
    () => json({ view: input(), submission: submission() }, 200),
    () =>
      json({ code: "RAW_SECRET_DIAGNOSTIC" }, 500, "application/problem+json"),
  ]) {
    const f = fixture(response),
      bytes = encodePortForwardInput(connection(), Uint8Array.of(1));
    await assert.rejects(f.client.submit(input(), bytes, "submit-key-0003"));
    assert.equal(
      bytes.every((b) => b === 0),
      true,
    );
    assert.equal(f.calls.filter((c) => c.options.method === "POST").length, 1);
  }
});

test("output preserves full bigint cursor, binds hash and hides private payload from default inspection", () => {
  const initial = input(),
    after = 9007199254740993n,
    now = Date.now(),
    payload = Buffer.from("synthetic-private-TCP-payload".padEnd(52, "!"));
  const value = {
    invocationRef: initial.invocationRef,
    streamRef: initial.streamRef,
    generation: String(initial.generation),
    afterSequence: String(after),
    outputWatermark: String(after + 1n),
    droppedBytes: "0",
    status: "FRAME",
    authorizedUntil: new Date(now + 4000).toISOString(),
    nativeTerminalKnown: false,
    transportJoined: false,
    frame: {
      stageRef: "kof_synthetic",
      sequence: String(after + 1n),
      channel: "PORT_FORWARD",
      payloadDigest: createHash("sha256").update(payload).digest("hex"),
      payloadBase64: payload.toString("base64"),
    },
  };
  const result = parsePortForwardOutput(value, initial, after, now);
  assert.equal(result.sequence, after + 1n);
  assert.deepEqual(result.payload, payload);
  assert.ok(!inspect(result).includes("synthetic-private"));
  result.payload.fill(0);
  for (const mutate of [
    (v) => (v.frame.sequence = String(after + 2n)),
    (v) => (v.authorizedUntil = new Date(now - 1).toISOString()),
    (v) => (v.frame.payloadDigest = "0".repeat(64)),
    (v) => (v.frame.payloadBase64 += "="),
    (v) => (v.frame.extra = "unknown"),
    (v) => (v.status = "ENDED"),
  ]) {
    const v = structuredClone(value);
    mutate(v);
    assert.throws(() => parsePortForwardOutput(v, initial, after, now));
  }
  payload.fill(0);
});

test("discovery retains distinct streams instead of selecting an arbitrary first item", async () => {
  const a = input(),
    b = { ...input(), streamRef: "kst_secondstream" };
  const f = fixture((url) => {
    assert.ok(url.pathname.endsWith("/kubernetes-stream-inputs"));
    return json({ items: [a, b], nextPageToken: "" });
  });
  const result = await f.client.listInputs(a);
  assert.equal(result.items.length, 2);
  assert.deepEqual(
    result.items.map((item) => item.streamRef),
    [a.streamRef, b.streamRef],
  );
});
