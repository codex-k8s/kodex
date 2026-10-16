import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { bootstrapPull } from "./stable-pull-bootstrap.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { validateStableReceipt } from "./stable-image-publication.mjs";

function fixture() {
  const directory = mkdtempSync(join(tmpdir(), "kodex-pull-bootstrap-fixture-")), trace = [], cache = new Map(), flags = {};
  const bytes = "verified publication fixture bytes", digest = "sha256:" + "b".repeat(64), sourceSHA = "a".repeat(40);
  const receipt = { schema: "kodex.stable-local-build/v1", component: "registry-pull-authorizer", sourceSHA,
    contextSHA256: "c".repeat(64), imageDigest: digest, archiveSHA256: createHash("sha256").update(bytes).digest("hex"),
    dockerfile: "services/jobs/role-image-builder/Dockerfile", target: "registry-pull-runtime", admission: "NOT_RUN", publication: "NOT_RUN", nodePull: "NOT_RUN" };
  const publication = { schema: "kodex.stable-image-publication/v2", artifactProfile: "built", context: "k3d-fixture",
    clusterUID: "system-uid", namespaceUID: "app-uid", source: sourceSHA, receipt, image: validateStableReceipt(receipt), archiveSHA256: receipt.archiveSHA256 };
  writeFileSync(join(directory, "image.oci.tar"), bytes, { mode: 0o400 });
  writeFileSync(join(directory, "publication.json"), JSON.stringify(publication), { mode: 0o600 });
  const nodes = ["agent", "server"].map((role, index) => ({ metadata: { name: `k3d-fixture-${role}-0`, uid: role },
    status: { nodeInfo: { operatingSystem: "linux", architecture: "amd64" }, conditions: [{ type: "Ready", status: "True" }] },
    container: { Id: String(index + 1).repeat(64), Image: "sha256:" + "d".repeat(64), Name: `/k3d-fixture-${role}-0`, State: { Running: true },
      Config: { Labels: { "k3d.cluster": "fixture", "k3d.role": role }, Cmd: [role], Env: ["PRIVATE=BOOTSTRAP_PRIVATE_SENTINEL"] } } }));
  for (const node of nodes) cache.set(node.container.Id, { tagged: false, exact: false });
  const port = async (program, argv) => {
    trace.push({ program, argv });
    if (program === "git") return argv[0] === "rev-parse" ? "f".repeat(40) : "";
    if (program === "kubectl") {
      if (argv[0] === "config") return "k3d-fixture";
      const i = argv.indexOf("get");
      if (argv[i + 1] === "namespace") return JSON.stringify({ metadata: { uid: argv[i + 2] === "kube-system" ? "system-uid" : "app-uid" } });
      if (argv[i + 1] === "nodes") return JSON.stringify({ items: nodes.map(({ container: _container, ...node }) => ({ ...node,
        metadata: flags.identityDrift ? { ...node.metadata, uid: node.metadata.uid + "-replaced" } : node.metadata })) });
    }
    if (program === "docker") {
      if (argv[0] === "inspect") return JSON.stringify(nodes.find(n => n.metadata.name === argv.at(-1)).container);
      const state = cache.get(argv[1]), reference = argv.at(-1).slice(6), exact = reference.includes("@");
      const present = exact ? state.exact : state.tagged;
      const wanted = flags.foreignAlias ? "sha256:" + "e".repeat(64) : digest;
      return "REF TYPE DIGEST SIZE PLATFORMS LABELS\n" + (present ? `${reference} oci ${wanted} 1 MiB linux/amd64 io.cri-containerd.image=managed,io.cri-containerd.pinned=pinned\n` : "");
    }
    if (program === "bash") {
      assert.equal(argv[argv.indexOf("--repository") + 1], "registry.local.kodex/kodex/registry-pull-authorizer");
      if (argv[argv.indexOf("--mode") + 1] === "import") {
        cache.set(nodes[0].container.Id, { tagged: true, exact: true });
        if (flags.loseImport) throw Error("LOST_IMPORT_RESPONSE");
        cache.set(nodes[1].container.Id, { tagged: true, exact: true });
      } else {
        assert.equal([...cache.values()].every(n => n.tagged && n.exact), true);
        if (flags.criFailure) throw Error("CRI_PIN_MISMATCH");
      }
      return "verified";
    }
    throw Error("UNEXPECTED_FIXTURE_COMMAND");
  };
  const run = (mode, output, extra = []) => bootstrapPull([mode, "--context", "k3d-fixture", "--output", join(directory, output), ...extra], port);
  const plan = async () => { await run("plan", "plan.json", ["--publication-plan", join(directory, "publication.json")]); return JSON.parse(readFileSync(join(directory, "plan.json"), "utf8")); };
  const imports = () => trace.filter(r => r.program === "bash" && r.argv[r.argv.indexOf("--mode") + 1] === "import").length;
  return { directory, trace, flags, nodes, cache, publication, run, plan, imports };
}

test("bootstrap seeds only exact helper OCI on both nodes and readback never reimports", async () => {
  const f = fixture();
  try {
    const plan = await f.plan(), args = ["--plan", join(f.directory, "plan.json")];
    assert.equal(JSON.stringify(plan).includes("BOOTSTRAP_PRIVATE_SENTINEL"), false);
    await f.run("apply", "apply.jsonl", [...args, "--approved-plan-sha", fingerprint(plan)]);
    await f.run("readback", "readback.json", args);
    assert.equal(f.imports(), 1);
    assert.equal(JSON.parse(readFileSync(join(f.directory, "readback.json"), "utf8")).freshPull, "NOT_RUN");
    assert.equal(f.trace.some(r => r.program === "kubectl" && r.argv.some(a => ["patch", "create", "delete", "exec", "apply"].includes(a))), false);
    assert.equal(readFileSync(join(f.directory, "apply.jsonl"), "utf8").includes("BOOTSTRAP_PRIVATE_SENTINEL"), false);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("lost seed response is inspected as partial and old plan cannot retry import", async () => {
  const f = fixture();
  try {
    const plan = await f.plan(), args = ["--plan", join(f.directory, "plan.json")]; f.flags.loseImport = true;
    await assert.rejects(f.run("apply", "unknown.jsonl", [...args, "--approved-plan-sha", fingerprint(plan)]), /LOST_IMPORT_RESPONSE/);
    await f.run("inspect", "inspect.json", args);
    const observed = JSON.parse(readFileSync(join(f.directory, "inspect.json"), "utf8"));
    assert.equal(observed.complete, false); assert.equal(observed.nodes.filter(n => n.exact).length, 1);
    await assert.rejects(f.run("apply", "retry.jsonl", [...args, "--approved-plan-sha", fingerprint(plan)]), /EXACT_APPROVAL_OR_STATE_REQUIRED/);
    await assert.rejects(f.run("readback", "readback.json", args), /CACHE_INCOMPLETE/);
    assert.equal(f.imports(), 1);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("approval node archive or alias drift stops bootstrap before import", async () => {
  for (const scenario of ["approval", "identity", "archive", "alias", "profile"]) {
    const f = fixture();
    try {
      const plan = await f.plan();
      if (scenario === "identity") f.flags.identityDrift = true;
      if (scenario === "archive") { chmodSync(join(f.directory, "image.oci.tar"), 0o600); writeFileSync(join(f.directory, "image.oci.tar"), "changed"); chmodSync(join(f.directory, "image.oci.tar"), 0o400); }
      if (scenario === "alias") { f.cache.set(f.nodes[0].container.Id, { tagged: true, exact: true }); f.flags.foreignAlias = true; }
      if (scenario === "profile") { f.publication.schema = "kodex.stable-image-publication/v1"; writeFileSync(join(f.directory, "publication.json"), JSON.stringify(f.publication)); }
      await assert.rejects(f.run("apply", "apply.jsonl", ["--plan", join(f.directory, "plan.json"), "--approved-plan-sha", scenario === "approval" ? "0".repeat(64) : fingerprint(plan)]));
      assert.equal(f.imports(), 0);
    } finally { rmSync(f.directory, { recursive: true, force: true }); }
  }
});

test("CTR metadata alone cannot replace CRI pinned readback", async () => {
  const f = fixture();
  try {
    const plan = await f.plan(); f.flags.criFailure = true;
    await assert.rejects(f.run("apply", "apply.jsonl", ["--plan", join(f.directory, "plan.json"), "--approved-plan-sha", fingerprint(plan)]), /CRI_PIN_MISMATCH/);
    assert.equal(f.imports(), 1);
    assert.match(readFileSync(join(f.directory, "apply.jsonl"), "utf8"), /FAIL_OR_UNKNOWN/);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});
