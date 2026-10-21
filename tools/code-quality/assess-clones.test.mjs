import assert from "node:assert/strict";
import test from "node:test";
import { assessClones } from "./assess-clones.mjs";

const digest = "a".repeat(64);
const reason =
  "Разные типизированные операции объявляют одинаковую обязательную форму параметров; вынос изменил бы явные границы этих контрактов.";
const clone = (first, second, fragmentSHA256 = digest) => ({
  format: "yaml",
  first: { path: first, start: 10, end: 28 },
  second: { path: second, start: 50, end: 68 },
  fragmentSHA256,
});
const summary = (clones) => ({
  generatedBy: "tools/code-quality/summarize-report.mjs",
  sourceSHA: "f".repeat(40),
  reportSHA256: "b".repeat(64),
  clones,
});
const entry = (count = 2) => ({
  format: "yaml",
  firstPath: "a.yaml",
  secondPath: "b.yaml",
  fragmentSHA256: digest,
  count,
  reason,
});

test("keeps every exact retained occurrence and leaves new groups unresolved", () => {
  const report = summary([
    clone("a.yaml", "b.yaml"),
    clone("b.yaml", "a.yaml"),
    clone("a.yaml", "c.yaml", "c".repeat(64)),
  ]);
  const result = assessClones(report, { version: 1, entries: [entry()] });
  assert.equal(result.rawGroups, 3);
  assert.equal(result.retainedGroups, 2);
  assert.equal(result.unresolvedGroups, 1);
  assert.deepEqual(result.retained[0].indices, [0, 1]);
  assert.equal(result.unresolved[0].index, 2);
  assert.equal(result.retained[0].key[3], digest);
});

test("location-only shifts remain stable while changed fragment or count fails closed", () => {
  const moved = clone("a.yaml", "b.yaml");
  moved.first.start = 100;
  moved.first.end = 118;
  assert.equal(
    assessClones(summary([moved]), { version: 1, entries: [entry(1)] })
      .retainedGroups,
    1,
  );
  assert.throws(() =>
    assessClones(summary([moved, moved]), {
      version: 1,
      entries: [entry(1)],
    }),
  );
  assert.throws(() =>
    assessClones(summary([clone("a.yaml", "b.yaml", "d".repeat(64))]), {
      version: 1,
      entries: [entry(1)],
    }),
  );
});

test("rejects duplicate, broad or unexplained decisions", () => {
  const report = summary([clone("a.yaml", "b.yaml")]);
  assert.throws(() =>
    assessClones(report, { version: 1, entries: [entry(1), entry(1)] }),
  );
  assert.throws(() =>
    assessClones(report, {
      version: 1,
      entries: [{ ...entry(1), fragmentSHA256: undefined }],
    }),
  );
  assert.throws(() =>
    assessClones(report, {
      version: 1,
      entries: [{ ...entry(1), firstPath: "../a.yaml" }],
    }),
  );
  assert.throws(() =>
    assessClones(report, {
      version: 1,
      entries: [{ ...entry(1), reason: "same" }],
    }),
  );
});
