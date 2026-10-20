import assert from "node:assert/strict";
import { chmod, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import test from "node:test";
import { planPublicIdentity } from "./stable-public-identity-plan.mjs";

test("public identity planner rejects foreign context, duplicate options and missing private output before cluster access", async t => {
  const dir = await mkdtemp(tmpdir() + "/kodex-public-identity-plan-"); t.after(() => rm(dir, { recursive: true, force: true }));
  const calls = [], port = async (...args) => { calls.push(args); throw Error("UNEXPECTED_CLUSTER_ACCESS"); };
  for (const args of [["--context", "foreign", "--output", dir + "/plan.json"], ["--context", "k3d-kodex", "--context", "k3d-kodex", "--output", dir + "/plan.json"],
    ["--context", "k3d-kodex", "--output", "relative.json"]]) await assert.rejects(planPublicIdentity(args, port), /PUBLIC_IDENTITY_ARGUMENTS_INVALID/);
  await chmod(dir, 0o755);
  await assert.rejects(planPublicIdentity(["--context", "k3d-kodex", "--output", dir + "/plan.json"], port), /PUBLIC_IDENTITY_PRIVATE_DIRECTORY_REQUIRED/);
  assert.equal(calls.length, 0);
});

test("dirty or unproven executor prevents all identity preflight commands", async t => {
  const dir = await mkdtemp(tmpdir() + "/kodex-public-identity-plan-"); t.after(() => rm(dir, { recursive: true, force: true }));
  for (const dirty of [false, true]) {
    const calls = [], port = async (program, args) => {
      calls.push(program); assert.equal(program, "git"); return args[0] === "rev-parse" ? (dirty ? "a".repeat(40) : "invalid") : " M source";
    };
    await assert.rejects(planPublicIdentity(["--context", "k3d-kodex", "--output", dir + "/plan.json"], port), /PUBLIC_IDENTITY_CLEAN_SOURCE_REQUIRED/);
    assert.equal(calls.every(p => p === "git"), true);
  }
});
