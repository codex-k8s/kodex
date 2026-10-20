import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

const source = readFileSync(new URL("./build-release.sh", import.meta.url), "utf8");
const waitFunction = source.slice(source.indexOf("wait_build_batch() {"), source.indexOf("build_component() {"));
const pool = source.slice(source.indexOf("while IFS=$'\\t' read -r component dockerfile target; do"),
  source.indexOf('role_image_input_result="$temporary_directory/role-image-input.json"'));

function runPool(failed = "") {
  const directory = mkdtempSync(join(tmpdir(), "kodex-release-pool-"));
  try {
    const script = `set -euo pipefail
parallelism=2
build_pids=()
manifest=fixture
fail() { printf 'FAIL\\n' >>"$TRACE"; exit 1; }
jq() { printf 'one\\ta\\t\\ntwo\\tb\\t\\nagent-runner\\tc\\t\\nrole-base-documents\\td\\t\\nlast\\te\\t\\n'; }
build_component() {
  printf 'start:%s\\n' "$1" >>"$TRACE"
  sleep 0.03
  printf 'end:%s\\n' "$1" >>"$TRACE"
  [[ "$1" != "$FAILED" ]]
}
${waitFunction}
${pool}
printf 'COMPLETE\\n' >>"$TRACE"
`;
    let success = true;
    try { execFileSync("/bin/bash", ["-s"], { input: script, timeout: 5000,
      env: { PATH: "/usr/bin:/bin", TRACE: join(directory, "trace"), FAILED: failed }, stdio: ["pipe", "pipe", "pipe"] }); }
    catch { success = false; }
    return { success, lines: readFileSync(join(directory, "trace"), "utf8").trim().split("\n") };
  } finally { rmSync(directory, { recursive: true, force: true }); }
}

test("release pool is bounded and runner/base dependencies join before publication", () => {
  const { success, lines } = runPool();
  assert.equal(success, true);
  let current = 0, maximum = 0;
  for (const line of lines) {
    if (line.startsWith("start:")) current++;
    if (line.startsWith("end:")) current--;
    maximum = Math.max(maximum, current);
  }
  assert.equal(current, 0); assert.equal(maximum, 2);
  assert.ok(lines.indexOf("start:agent-runner") > lines.indexOf("end:one"));
  assert.ok(lines.indexOf("start:agent-runner") > lines.indexOf("end:two"));
  assert.ok(lines.indexOf("start:role-base-documents") > lines.indexOf("end:agent-runner"));
  assert.equal(lines.at(-1), "COMPLETE");
});

test("failed build joins the entire batch and forbids later phases", () => {
  const { success, lines } = runPool("one");
  assert.equal(success, false);
  assert.ok(lines.includes("end:one") && lines.includes("end:two"));
  assert.equal(lines.some(x => x.includes("agent-runner") || x === "COMPLETE"), false);
  assert.equal(lines.at(-1), "FAIL");
});
