import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { inspect } from "node:util";
import {
  decodePortForwardOutput,
  encodePortForwardInput,
  validPortForwardInput,
} from "../../services/staff/control-center/src/shared/lib/kubernetes-port-forward-frame.ts";

const cases = JSON.parse(
  readFileSync(
    new URL(
      "../../libs/go/managedkubernetes/testdata/port_forward_frames.json",
      import.meta.url,
    ),
    "utf8",
  ),
);
// Тот же corpus проверяется actual Go encoder/parser, а не независимым Node генератором.
for (const fixture of cases)
  test(`shared KPF canonical ${fixture.name}`, () => {
    const raw = Buffer.from(fixture.rawBase64, "base64"),
      body = Buffer.from(fixture.bodyBase64, "base64");
    try {
      if (fixture.direction === "INPUT") {
        assert.equal(validPortForwardInput(raw, false), true);
        assert.equal(validPortForwardInput(raw, true), false);
        assert.throws(() => decodePortForwardOutput(raw));
        const encoded = encodePortForwardInput(
          {
            connectionRef: fixture.connectionRef,
            generation: Number(fixture.generation),
            slot: fixture.slot,
          },
          body,
          fixture.action === 3,
        );
        assert.deepEqual(Buffer.from(encoded), raw);
        encoded.fill(0);
        return;
      }
      const frame = decodePortForwardOutput(raw);
      assert.equal(frame.connectionRef, fixture.connectionRef);
      assert.equal(frame.generation, BigInt(fixture.generation));
      assert.equal(frame.nativeOrdinal, BigInt(fixture.nativeOrdinal));
      assert.equal(frame.slot, fixture.slot);
      assert.deepEqual(Buffer.from(frame.body), body);
      assert.equal(validPortForwardInput(raw, false), false);
      assert.equal(Object.keys(frame).includes("body"), false);
      assert.equal(inspect(frame).includes("body"), false);
      assert.deepEqual(JSON.parse(JSON.stringify(frame)), {
        connectionRef: fixture.connectionRef,
        generation: fixture.generation,
        slot: fixture.slot,
        nativeOrdinal: fixture.nativeOrdinal,
        action:
          fixture.action === 2
            ? "DATA"
            : fixture.action === 3
              ? "HALF_CLOSE"
              : "CLOSE",
      });
      raw.fill(0);
      assert.deepEqual(Buffer.from(frame.body), body);
      frame.body.fill(0);
    } finally {
      raw.fill(0);
      body.fill(0);
    }
  });

test("OUTPUT rejects malformed coordinates, legacy frames and unbounded private bodies", () => {
  const good = Buffer.from(
    cases.find((item) => item.name === "output-data-bigint").rawBase64,
    "base64",
  );
  for (const mode of [
    "truncated",
    "legacy",
    "magic",
    "version",
    "action",
    "slot",
    "ref-length",
    "ref",
    "zero-generation",
    "overflow-generation",
    "zero-ordinal",
    "overflow-ordinal",
    "empty-data",
    "control-body",
    "open",
    "oversize",
  ]) {
    let raw = Buffer.from(good);
    switch (mode) {
      case "truncated":
        raw = raw.subarray(0, 50);
        break;
      case "legacy":
        raw = Buffer.from([0, 7]);
        break;
      case "magic":
        raw[0] = 0;
        break;
      case "version":
        raw[3] = 2;
        break;
      case "action":
        raw[4] = 9;
        break;
      case "slot":
        raw[5] = 16;
        break;
      case "ref-length":
        raw[14] = 27;
        break;
      case "ref":
        raw[15] = 33;
        break;
      case "zero-generation":
        raw.writeBigUInt64BE(0n, 6);
        break;
      case "overflow-generation":
        raw.writeBigUInt64BE(9223372036854775808n, 6);
        break;
      case "zero-ordinal":
        raw.writeBigUInt64BE(0n, 43);
        break;
      case "overflow-ordinal":
        raw.writeBigUInt64BE(9223372036854775808n, 43);
        break;
      case "empty-data":
        raw = raw.subarray(0, 51);
        break;
      case "control-body":
        raw[4] = 3;
        break;
      case "open":
        raw = raw.subarray(0, 51);
        raw[4] = 1;
        break;
      case "oversize":
        raw = Buffer.concat([good, Buffer.alloc(32769 - good.length)]);
        break;
    }
    assert.throws(
      () => decodePortForwardOutput(raw),
      /output frame is invalid/,
      mode,
    );
    raw.fill(0);
  }
  for (const slots of [0, 15, 17, NaN])
    assert.throws(() => decodePortForwardOutput(good, slots));
  good.fill(0);
});
