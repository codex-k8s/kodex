#!/usr/bin/env node
import { execFile } from "node:child_process";
import { open } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { privateArchiveHash, validateStableReceipt } from "./stable-image-publication.mjs";
import { validateNodeContainer } from "./stable-node-registry-model.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
const importer = root + "tools/dev/import-local-image.sh", repository = "registry.local.kodex/kodex/registry-pull-authorizer";
const socket = "/run/k3s/containerd/containerd.sock";
function requireValue(value, code) { if (!value) throw Error(code); }
async function createPrivate(path, value) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(JSON.stringify(value) + "\n"); await file.sync(); } finally { await file.close(); }
}

export async function bootstrapPull(args, commandPort) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--publication-plan", "--plan", "--output", "--approved-plan-sha"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "PULL_BOOTSTRAP_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  const context = options["--context"];
  requireValue(["plan", "apply", "readback", "inspect"].includes(mode) && /^k3d-[a-z0-9-]+$/.test(context ?? "") &&
    !/prod(?:uction)?/i.test(context) && options["--output"], "PULL_BOOTSTRAP_ARGUMENTS_INVALID");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = commandPort ?? (async (program, argv, timeout = 35000) => {
    try { return (await execute(program, argv, { cwd: root, env, timeout, maxBuffer: 4 << 20 })).stdout; }
    catch { throw Error("PULL_BOOTSTRAP_OPERATION_FAILED_OR_UNKNOWN"); }
  });
  const kube = argv => run("kubectl", ["--context", context, "--request-timeout=15s", ...argv]);
  const get = argv => kube([...argv, "-o", "json"]).then(JSON.parse);
  const source = (await run("git", ["rev-parse", "HEAD"])).trim();
  requireValue(/^[a-f0-9]{40}$/.test(source) && (await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim() === "",
    "PULL_BOOTSTRAP_CLEAN_SOURCE_REQUIRED");
  requireValue((await run("kubectl", ["config", "current-context"])).trim() === context, "PULL_BOOTSTRAP_CURRENT_CONTEXT_MISMATCH");
  const clusterUID = (await get(["get", "namespace", "kube-system"])).metadata.uid;
  const appUID = (await get(["get", "namespace", "kodex-system"])).metadata.uid;
  const artifact = async path => {
    const value = await readPrivateJSON(path), image = validateStableReceipt(value.receipt);
    requireValue(value.schema === "kodex.stable-image-publication/v2" && value.artifactProfile === "built" && value.context === context &&
      value.clusterUID === clusterUID && value.namespaceUID === appUID && image.component === "registry-pull-authorizer" &&
      value.receipt.dockerfile === "services/jobs/role-image-builder/Dockerfile" && value.receipt.target === "registry-pull-runtime" &&
      fingerprint(image) === fingerprint(value.image), "PULL_BOOTSTRAP_PUBLICATION_PROOF_INVALID");
    await run("git", ["merge-base", "--is-ancestor", value.source, source]);
    await run("git", ["merge-base", "--is-ancestor", image.sourceSHA, source]);
    await run("git", ["diff", "--quiet", image.sourceSHA, source, "--", "services/jobs/role-image-builder", "libs", "contracts", "go.mod", "go.sum", "go.work", "go.work.sum"]);
    const archive = join(dirname(path), "image.oci.tar");
    requireValue(await privateArchiveHash(archive) === value.archiveSHA256 && value.archiveSHA256 === value.receipt.archiveSHA256,
      "PULL_BOOTSTRAP_ARCHIVE_CHANGED");
    return { publicationPlan: path, publicationSHA256: fingerprint(value), archive, archiveSHA256: value.archiveSHA256,
      sourceSHA: image.sourceSHA, image: `${repository}@${image.digest}`, tag: `${repository}:${image.sourceSHA}`, digest: image.digest };
  };
  const snapshot = async image => {
    requireValue((await get(["get", "namespace", "kube-system"])).metadata.uid === clusterUID &&
      (await get(["get", "namespace", "kodex-system"])).metadata.uid === appUID, "PULL_BOOTSTRAP_CLUSTER_DRIFT");
    const list = await get(["get", "nodes"]);
    requireValue(Array.isArray(list.items) && list.items.length > 0 && list.items.length <= 8, "PULL_BOOTSTRAP_NODE_SET_INVALID");
    const nodes = [];
    for (const node of list.items.sort((a, b) => a.metadata.name.localeCompare(b.metadata.name, "en"))) {
      requireValue(node.status?.nodeInfo?.operatingSystem === "linux" && node.status.nodeInfo.architecture === "amd64", "PULL_BOOTSTRAP_PLATFORM_INVALID");
      const container = JSON.parse(await run("docker", ["inspect", "--format", "{{json .}}", node.metadata.name]));
      const binding = validateNodeContainer(node, container, context.slice(4)), cached = [];
      for (const reference of [image.tag, image.image]) {
        const raw = await run("docker", ["exec", binding.containerID, "ctr", "--address", socket, "-n", "k8s.io", "images", "list", `name==${reference}`]);
        const rows = raw.trim().split("\n").filter(Boolean).map(r => r.trim().split(/\s+/)).filter(r => r[0] !== "REF");
        requireValue(rows.length <= 1 && rows.every(r => r[0] === reference && r[2] === image.digest && r.at(-2) === "linux/amd64" &&
          r.at(-1).split(",").includes("io.cri-containerd.image=managed") && r.at(-1).split(",").includes("io.cri-containerd.pinned=pinned")),
        "PULL_BOOTSTRAP_FOREIGN_IMAGE_ALIAS");
        cached.push(rows.length === 1);
      }
      nodes.push({ ...binding, tagged: cached[0], exact: cached[1] });
    }
    requireValue(new Set(nodes.map(n => n.uid)).size === nodes.length && new Set(nodes.map(n => n.containerID)).size === nodes.length,
      "PULL_BOOTSTRAP_DUPLICATE_NODE");
    return nodes;
  };
  if (mode === "plan") {
    requireValue(options["--publication-plan"] && !options["--plan"] && !options["--approved-plan-sha"], "PULL_BOOTSTRAP_ARGUMENTS_INVALID");
    const image = await artifact(resolve(options["--publication-plan"])), nodes = await snapshot(image);
    const plan = { schema: "kodex.stable-pull-bootstrap/v1", executorSHA: source, context, clusterUID, appUID, image, nodes };
    await createPrivate(options["--output"], plan);
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", planSHA256: fingerprint(plan), nodes: nodes.length }) + "\n"); return;
  }
  requireValue(options["--plan"] && !options["--publication-plan"], "PULL_BOOTSTRAP_ARGUMENTS_INVALID");
  const plan = await readPrivateJSON(options["--plan"]), hash = fingerprint(plan);
  requireValue(plan.schema === "kodex.stable-pull-bootstrap/v1" && plan.executorSHA === source && plan.context === context &&
    plan.clusterUID === clusterUID && plan.appUID === appUID && Array.isArray(plan.nodes) && plan.nodes.length > 0 && plan.nodes.length <= 8 &&
    fingerprint(await artifact(plan.image.publicationPlan)) === fingerprint(plan.image), "PULL_BOOTSTRAP_PLAN_DRIFT");
  const nodes = await snapshot(plan.image);
  const bindings = values => values.map(({ tagged: _tagged, exact: _exact, ...value }) => value);
  requireValue(fingerprint(bindings(nodes)) === fingerprint(bindings(plan.nodes)), "PULL_BOOTSTRAP_NODE_IDENTITY_DRIFT");
  const complete = values => values.every(n => n.tagged && n.exact);
  const importerArgs = ["--context", context, "--repository", repository, "--tag", plan.image.tag, "--exact-reference", plan.image.image];
  const readback = async () => {
    const current = await snapshot(plan.image);
    requireValue(fingerprint(bindings(current)) === fingerprint(bindings(plan.nodes)) && complete(current), "PULL_BOOTSTRAP_CACHE_INCOMPLETE");
    await run("bash", [importer, ...importerArgs, "--mode", "readback"], 90000);
    requireValue(fingerprint(await snapshot(plan.image)) === fingerprint(current), "PULL_BOOTSTRAP_CACHE_CHANGED");
    return { status: "PASS", phase: "NODE_CACHE_ONLY", nodes: current.map(n => n.name), registryPublication: "NOT_RUN", freshPull: "NOT_RUN", helperActivation: "NOT_RUN" };
  };
  if (mode === "apply") {
    requireValue(options["--approved-plan-sha"] === hash && fingerprint(nodes) === fingerprint(plan.nodes), "PULL_BOOTSTRAP_EXACT_APPROVAL_OR_STATE_REQUIRED");
    await createPrivate(options["--output"], { schema: "kodex.stable-pull-bootstrap-journal/v1", planSHA256: hash, status: "IN_PROGRESS" });
    const journal = await open(options["--output"], "a"), record = async row => { await journal.writeFile(JSON.stringify(row) + "\n"); await journal.sync(); };
    try {
      if (!complete(nodes)) {
        await record({ status: "IMPORT_ATTEMPT", image: plan.image.image });
        await run("bash", [importer, ...importerArgs, "--mode", "import", "--archive", plan.image.archive], 120000);
      }
      const result = await readback(); await record(result); process.stdout.write(JSON.stringify(result) + "\n");
    } catch (error) { await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "PULL_BOOTSTRAP_FAILED" }); throw error; }
    finally { await journal.close(); }
    return;
  }
  requireValue(!options["--approved-plan-sha"], "PULL_BOOTSTRAP_ARGUMENTS_INVALID");
  const result = mode === "inspect" ? { status: "OBSERVED", phase: "CACHE_INSPECTION_ONLY", complete: complete(nodes),
    nodes: nodes.map(n => ({ name: n.name, tagged: n.tagged, exact: n.exact })), helperActivation: "NOT_RUN", freshPull: "NOT_RUN" } : await readback();
  await createPrivate(options["--output"], result); process.stdout.write(JSON.stringify(result) + "\n");
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  bootstrapPull(process.argv.slice(2)).catch(error => { process.stderr.write(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code:
    /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "PULL_BOOTSTRAP_FAILED" }) + "\n"); process.exitCode = 1; });
