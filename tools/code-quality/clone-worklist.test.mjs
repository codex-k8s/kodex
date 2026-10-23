import assert from "node:assert/strict";
import test from "node:test";
import { buildCloneWorklist } from "./clone-worklist.mjs";

const location = (file, start, end) => ({ path: file, start, end });
const clone = (first, second) => ({
  format: "go",
  kind: "exact",
  fragmentSHA256: "a".repeat(64),
  first,
  second,
});
const summary = (clones) => ({
  generatedBy: "tools/code-quality/summarize-report.mjs",
  reportSHA256: "b".repeat(64),
  cloneSetSHA256: "c".repeat(64),
  clones,
});

test("groups only materially overlapping locations on both sides", () => {
  const report = summary([
    clone(location("a.go", 10, 30), location("b.go", 50, 70)),
    clone(location("a.go", 12, 29), location("b.go", 52, 68)),
    clone(location("a.go", 14, 28), location("b.go", 90, 105)),
    clone(location("b.go", 100, 120), location("a.go", 35, 55)),
    clone(location("a.go", 200, 215), location("a.go", 230, 245)),
    clone(location("a.go", 231, 246), location("a.go", 201, 216)),
    clone(location("c.go", 10, 25), location("d.go", 30, 45)),
  ]);
  const result = buildCloneWorklist(report);
  assert.equal(result.rawGroups, 7);
  assert.equal(result.cloneSetSHA256, report.cloneSetSHA256);
  assert.equal(result.filePairs, 3);
  assert.equal(result.clusters.length, 5);
  assert.deepEqual(
    result.clusters
      .map((cluster) => cluster.members.map((member) => member.index))
      .sort((a, b) => a[0] - b[0]),
    [[0, 1], [2], [3], [4, 5], [6]],
  );
  assert.equal(
    result.clusters.find((cluster) => cluster.members[0].index === 4)?.first
      .start,
    200,
  );
  assert.deepEqual(buildCloneWorklist(report), result);
  assert.deepEqual(
    buildCloneWorklist({ ...report, reportSHA256: "d".repeat(64) }).clusters.map(
      (cluster) => cluster.id,
    ),
    result.clusters.map((cluster) => cluster.id),
  );
  assert.deepEqual(
    buildCloneWorklist({ ...report, clones: [...report.clones].reverse() }).clusters.map(
      (cluster) => cluster.id,
    ),
    result.clusters.map((cluster) => cluster.id),
  );
  assert.equal(JSON.stringify(result).includes("fragmentSHA256"), false);
});

test("rejects malformed locations and summaries before writing a worklist", () => {
  assert.throws(() =>
    buildCloneWorklist({ ...summary([]), generatedBy: "other" }),
  );
  assert.throws(() =>
    buildCloneWorklist({ ...summary([]), cloneSetSHA256: "invalid" }),
  );
  assert.throws(() =>
    buildCloneWorklist(
      summary([
        clone(location("../outside.go", 1, 11), location("b.go", 1, 11)),
      ]),
    ),
  );
  assert.throws(() =>
    buildCloneWorklist(
      summary([clone(location("a.go", 20, 10), location("b.go", 1, 11))]),
    ),
  );
});
