import assert from "node:assert/strict";
import test from "node:test";
import {
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
