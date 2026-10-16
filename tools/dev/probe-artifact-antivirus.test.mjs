import test from "node:test";
import assert from "node:assert/strict";
import { verifyScannerControl } from "./probe-artifact-antivirus.mjs";

test("AV control requires real clean and positive EICAR exit statuses", () => {
  const clean = { status: 0, stdout: "stdin: OK\n" };
  const infected = { status: 1, stdout: "stdin: Win.Test.EICAR_HDB-1 FOUND\n" };
  assert.deepEqual(verifyScannerControl(clean, infected), { clean: "PASS", eicar: "PASS" });
  for (const result of [{status:2,stdout:"stdin: OK\n"},{status:0,stdout:""},{status:0,stdout:"other: OK\n"}])
    assert.throws(() => verifyScannerControl(result, infected), /CLEAN_CONTROL_FAILED/);
  for (const result of [{status:0,stdout:infected.stdout},{status:1,stdout:"stdin: other FOUND\n"},{status:2,stdout:infected.stdout}])
    assert.throws(() => verifyScannerControl(clean, result), /EICAR_CONTROL_FAILED/);
});
