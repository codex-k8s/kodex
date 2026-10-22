import assert from "node:assert/strict";
import test from "node:test";
import { createTemplateJSONReader } from "./template-json-reader.mjs";

test("creation is lazy and command arguments and process limits stay exact", (t) => {
  t.mock.method(Date, "now", () => 1000);
  const base = ["--context=synthetic", "-n", "synthetic-namespace"];
  const env = { PATH: "/synthetic/bin", KUBECONFIG: "/synthetic/config" };
  const args = ["get", "job", "synthetic-job"];
  const template = '{{printf "{\\\"ready\\\":true}"}}';
  const calls = [];
  const read = createTemplateJSONReader(base, env, 9000, {}, (...call) => {
    calls.push(call);
    return '{"ready":true}';
  });
  assert.equal(calls.length, 0);
  assert.deepEqual(read(args, template), { ready: true });
  assert.deepEqual(calls, [
    [
      "kubectl",
      [...base, ...args, "-o", `go-template=${template}`],
      {
        env,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
        timeout: 6000,
        killSignal: "SIGKILL",
        maxBuffer: 1 << 20,
      },
    ],
  ]);
  assert.strictEqual(calls[0][2].env, env);
  assert.deepEqual(base, ["--context=synthetic", "-n", "synthetic-namespace"]);
  assert.deepEqual(args, ["get", "job", "synthetic-job"]);
});

test("expired and cancelled reads invoke neither fixture nor child transport", (t) => {
  t.mock.method(Date, "now", () => 1000);
  const forbidden = () => assert.fail("closed read performed an operation");
  for (const deadline of [999, 1000]) {
    const read = createTemplateJSONReader(
      [],
      {},
      deadline,
      { read: forbidden },
      forbidden,
    );
    assert.equal(read([], "{}"), null);
  }
  const controller = new AbortController();
  controller.abort();
  assert.equal(
    createTemplateJSONReader(
      [],
      {},
      2000,
      {
        signal: controller.signal,
        read: forbidden,
      },
      forbidden,
    )([], "{}"),
    null,
  );
});

test("runtime fixture is read dynamically with receiver, result and exceptions preserved", (t) => {
  t.mock.method(Date, "now", () => 1000);
  const args = ["get", "pods"];
  const result = { metadata: "synthetic-only" };
  const runtime = {
    read(actualArgs, template) {
      assert.strictEqual(this, runtime);
      assert.strictEqual(actualArgs, args);
      assert.equal(template, "synthetic-template");
      return result;
    },
  };
  const read = createTemplateJSONReader([], {}, 2000, runtime, () =>
    assert.fail("fixture launched child"),
  );
  assert.strictEqual(read(args, "synthetic-template"), result);
  const failure = new Error("synthetic fixture refusal");
  runtime.read = () => {
    throw failure;
  };
  assert.throws(
    () => read(args, "synthetic-template"),
    (err) => err === failure,
  );
  const cancellation = new AbortController();
  cancellation.abort();
  runtime.signal = cancellation.signal;
  assert.equal(read(args, "synthetic-template"), null);
});

test("transport and JSON decoding errors return null without reflecting private details", (t) => {
  t.mock.method(Date, "now", () => 1000);
  for (const execute of [
    () => {
      throw new Error("synthetic private child output");
    },
    () => "not JSON",
    () => '{"truncated":',
    () => "",
  ]) {
    assert.equal(
      createTemplateJSONReader([], {}, 2000, {}, execute)([], "{}"),
      null,
    );
  }
});

test("JSON transport does not replace the callers' typed metadata validation", (t) => {
  t.mock.method(Date, "now", () => 1000);
  for (const value of [null, false, 0, "text", [], { arbitrary: "data" }]) {
    const read = createTemplateJSONReader([], {}, 2000, {}, () =>
      JSON.stringify(value),
    );
    assert.deepEqual(read([], "{}"), value);
  }
});

test("remaining budget is capped and its original one millisecond floor is preserved", (t) => {
  let clock = 1000;
  t.mock.method(Date, "now", () => clock);
  const timeouts = [];
  const read = createTemplateJSONReader(
    [],
    {},
    5000,
    {},
    (_binary, _args, options) => {
      timeouts.push(options.timeout);
      return "{}";
    },
  );
  read([], "{}");
  clock = 4999;
  read([], "{}");
  clock = 5000;
  assert.equal(read([], "{}"), null);
  assert.deepEqual(timeouts, [4000, 1]);
  let tick = 1000;
  Date.now.mock.mockImplementation(() => tick++);
  const crossed = createTemplateJSONReader(
    [],
    {},
    1001,
    {},
    (_binary, _args, options) => {
      assert.equal(options.timeout, 1);
      return "{}";
    },
  );
  assert.deepEqual(crossed([], "{}"), {});
});
