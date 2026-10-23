import assert from "node:assert/strict";
import test from "node:test";
import { cloneSetSHA256 } from "./clone-fingerprint.mjs";

const sample = (fragmentSHA256 = "a".repeat(64)) => ({
  format: "go",
  kind: "code",
  tokens: 120,
  lines: 12,
  first: { path: "a.go", start: 10, end: 21 },
  second: { path: "b.go", start: 40, end: 51 },
  fragmentSHA256,
});

test("clone-set fingerprint ignores report order and reversed locations", () => {
  const first = sample();
  const second = sample("b".repeat(64));
  const reversed = { ...first, first: first.second, second: first.first };
  assert.equal(cloneSetSHA256([first, second]), cloneSetSHA256([second, reversed]));
});

test("clone-set fingerprint preserves occurrence count and exact content", () => {
  const first = sample();
  assert.notEqual(cloneSetSHA256([first]), cloneSetSHA256([first, first]));
  assert.notEqual(cloneSetSHA256([first]), cloneSetSHA256([sample("b".repeat(64))]));
  assert.notEqual(
    cloneSetSHA256([first]),
    cloneSetSHA256([{ ...first, first: { ...first.first, start: 11 } }]),
  );
});

test("clone-set fingerprint rejects malformed detector groups", () => {
  assert.throws(() => cloneSetSHA256(null));
  assert.throws(() => cloneSetSHA256([{ ...sample(), tokens: -1 }]));
  assert.throws(() =>
    cloneSetSHA256([{ ...sample(), first: { path: "../a.go", start: 1, end: 2 } }]),
  );
  assert.throws(() => cloneSetSHA256([sample("not-a-digest")]));
});
