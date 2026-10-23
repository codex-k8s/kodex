import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { reportPaths, writePrivateReport } from "./report-io.mjs";

test("report paths require an absolute input and a private report target", () => {
  assert.deepEqual(reportPaths(["/tmp/input.json", "/tmp/output.gen.json"]), {
    input: "/tmp/input.json",
    output: "/tmp/output.gen.json",
  });
  for (const args of [
    [],
    ["input.json", "/tmp/output.gen.json"],
    ["/tmp/input.json", "output.gen.json"],
    ["/tmp/input.json", "/tmp/output.json"],
  ]) {
    assert.throws(() => reportPaths(args));
  }
});

test("report writer emits readable JSON with owner-only access", (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kodex-report-"));
  t.after(() => fs.rmSync(directory, { recursive: true }));
  const output = path.join(directory, "sample.gen.json");
  writePrivateReport(output, { groups: 2 });
  assert.deepEqual(JSON.parse(fs.readFileSync(output, "utf8")), { groups: 2 });
  assert.equal(fs.statSync(output).mode & 0o777, 0o600);
});
