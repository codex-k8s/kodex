#!/usr/bin/env node
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { lstat, open, readFile } from "node:fs/promises";
import { dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { fingerprint } from "./stable-inventory.mjs";
import { validateNodeContainer } from "./stable-node-registry-model.mjs";
import { systemDesiredValues, systemServing } from "./stable-system-helm-state.mjs";
import { nativeAddonBinding, nativeTraefikTransition, prepareNativeDeployment, traefikHelmConfig, verifyNativeImage } from "./stable-native-model.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(value, code) { if (!value) throw Error(code); }
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
async function writePrivate(path, value) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(value); await file.sync(); } finally { await file.close(); }
}

export async function planNative(args, port) {
  const options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--output"].includes(key) && !Object.hasOwn(options, key) && args.length,
      "NATIVE_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  requireValue(options["--context"] === "k3d-kodex" && options["--output"]?.startsWith("/"), "NATIVE_ARGUMENTS_INVALID");
  const parent = await lstat(dirname(options["--output"]));
  requireValue(parent.isDirectory() && parent.uid === process.getuid() && (parent.mode & 0o777) === 0o700,
    "NATIVE_PRIVATE_OUTPUT_DIRECTORY_REQUIRED");
  const context = options["--context"], env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = port ?? (async (program, argv, input, timeout = 30000) => {
    try {
      const pending = execute(program, argv, { cwd: root, env, timeout, encoding: "buffer", maxBuffer: 16 << 20 });
      if (input !== undefined) { pending.child.stdin.on("error", () => {}); pending.child.stdin.end(input); }
      return (await pending).stdout;
    } catch { throw Error("NATIVE_PREFLIGHT_FAILED"); }
  });
  const text = async (...a) => (await run(...a)).toString("utf8");
  const kube = (argv, input) => input === undefined ? text("kubectl", ["--context", context, "--request-timeout=15s", ...argv]) :
    text("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-native-pipe", "--context", context, "--request-timeout=15s", ...argv], input, 180000);
  const get = async (kind, name, optional = false) => {
    const raw = await kube(["-n", "kube-system", "get", kind, name, "-o", "json", ...(optional ? ["--ignore-not-found"] : [])]);
    return raw.trim() ? JSON.parse(raw) : null;
  };
  const source = (await text("git", ["rev-parse", "HEAD"])).trim();
  requireValue(/^[a-f0-9]{40}$/.test(source) && !(await text("git", ["status", "--porcelain", "--untracked-files=normal"])).trim(),
    "NATIVE_CLEAN_SOURCE_REQUIRED");
  const config = JSON.parse(await readFile(root + "infra/stable-system/native-profiles.json", "utf8"));
  requireValue(config.schema === "kodex.stable-native/v1" && config.profiles.length === 3 &&
    config.profiles.map(p => p.name).join(",") === "coredns,metrics-server,traefik", "NATIVE_PROFILE_INVALID");
  const clusterUID = (await get("namespace", "kube-system")).metadata.uid;
  const nodes = JSON.parse(await kube(["get", "nodes", "-o", "json"])), bindings = [];
  for (const node of nodes.items) {
    requireValue(node.status?.nodeInfo?.operatingSystem === "linux" && node.status.nodeInfo.architecture === "amd64", "NATIVE_NODE_PLATFORM_INVALID");
    const c = JSON.parse(await text("docker", ["inspect", "--format", "{{json .}}", node.metadata.name]));
    bindings.push({ ...validateNodeContainer(node, c, "kodex"), role: c.Config.Labels["k3d.role"] });
  }
  const servers = bindings.filter(n => n.role === "server");
  requireValue(servers.length > 0 && bindings.length <= 8, "NATIVE_SERVER_SET_INVALID");
  const serving = await systemServing({ namespace: "kube-system", deployments: config.profiles.map(p => p.name) }, kube, undefined, 1);
  const chart = await get("helmchart", "traefik");
  requireValue(!await get("helmchartconfig", "traefik", true) && chart?.spec?.chart === "https://%{KUBERNETES_API}%" + config.traefikChart.apiPath,
    "NATIVE_TRAEFIK_SOURCE_CONFLICT");
  const intent = fingerprint({ source, clusterUID, chartUID: chart.metadata.uid, config });
  const targets = [], addonRows = [];
  for (const profile of config.profiles) {
    const d = await get("deployment", profile.name), transition = prepareNativeDeployment(d, profile);
    const dry = JSON.parse(await kube(["-n", "kube-system", "patch", "deployment", profile.name, "--type=json", "--patch-file=/dev/stdin", "--dry-run=server", "-o", "json"], JSON.stringify([
      { op: "test", path: "/metadata/uid", value: d.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: d.metadata.resourceVersion },
      { op: "test", path: "/spec", value: d.spec }, { op: "replace", path: "/spec", value: transition.after }])));
    requireValue(fingerprint(dry.spec) === transition.afterSHA256, "NATIVE_DEPLOYMENT_DRY_RUN_DRIFT");
    const selector = Object.entries(d.spec.selector?.matchLabels ?? {}).sort().map(([k, v]) => `${k}=${v}`).join(",");
    requireValue(selector && !d.spec.selector.matchExpressions?.length, "NATIVE_SELECTOR_INVALID");
    const pods = JSON.parse(await kube(["-n", "kube-system", "get", "pods", "-l", selector, "-o", "json"]));
    const candidates = pods.items.filter(p => !p.metadata.deletionTimestamp && p.status?.conditions?.some(c => c.type === "Ready" && c.status === "True"));
    requireValue(candidates.length === 1, "NATIVE_SOURCE_POD_AMBIGUOUS");
    const cache = bindings.find(b => b.name === candidates[0].spec.nodeName);
    requireValue(cache, "NATIVE_CACHE_NODE_UNKNOWN");
    const blobs = [];
    for (const digest of [profile.indexDigest, profile.manifestDigest, profile.configDigest]) blobs.push(await run("docker", ["exec", cache.containerID,
      "ctr", "--address", "/run/k3s/containerd/containerd.sock", "-n", "k8s.io", "content", "get", digest]));
    const image = verifyNativeImage(profile, ...blobs), observed = serving.find(s => s.name === profile.name);
    requireValue(observed.uid === d.metadata.uid && observed.images.length === 1 && observed.images.every(i =>
      [image.index, image.manifest, image.config].includes(i.imageID.match(/sha256:[a-f0-9]{64}$/)?.[0])), "NATIVE_SERVING_IMAGE_MISMATCH");
    const { after: _after, ...safe } = transition;
    targets.push({ name: profile.name, ...safe, metadataSHA256: fingerprint({ labels: d.metadata.labels, annotations: d.metadata.annotations }),
      serving: observed, image, cacheNodeUID: cache.uid });
    if (profile.owner === "addon") {
      const addon = await get("addons.k3s.cattle.io", profile.addon), files = [];
      for (const server of servers) {
        const metadata = (await text("docker", ["exec", server.containerID, "sh", "-ec",
          'test -f "$1" && test ! -L "$1" && test ! -e "$1.skip" && test ! -L "$1.skip"; stat -c "%u:%g:%a:%h:%d:%i:%s" "$1"', "kodex-native-file", profile.manifest])).trim();
        const fields = metadata.split(":");
        requireValue(fields.length === 7 && fields.every(v => /^[0-9]+$/.test(v)) && fields[0] === "0" && fields[3] === "1" &&
          !(parseInt(fields[2], 8) & 0o022) && Number(fields[6]) > 0 && Number(fields[6]) < (1 << 20), "NATIVE_MANIFEST_FILE_INVALID");
        const fileHash = (await text("docker", ["exec", server.containerID, "sha256sum", profile.manifest])).split(/\s+/)[0];
        const binding = nativeAddonBinding(profile, addon, fileHash);
        requireValue((await text("docker", ["exec", server.containerID, "stat", "-c", "%u:%g:%a:%h:%d:%i:%s", profile.manifest])).trim() === metadata,
          "NATIVE_MANIFEST_CHANGED");
        files.push({ serverUID: server.uid, containerID: server.containerID, metadata, ...binding });
      }
      const pdb = { apiVersion: "policy/v1", kind: "PodDisruptionBudget", metadata: { namespace: "kube-system", name: "kodex-stable-" + profile.name,
        labels: { "app.kubernetes.io/managed-by": "kodex-stable-native" }, annotations: { "kodex.dev/stable-native-intent": intent } },
      spec: { minAvailable: 1, selector: structuredClone(d.spec.selector) } };
      requireValue(!await get("poddisruptionbudget", pdb.metadata.name, true), "NATIVE_PDB_COLLISION");
      await kube(["create", "--dry-run=server", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(pdb));
      addonRows.push({ name: profile.name, files, pdb });
    }
  }
  const traefik = config.profiles.find(p => p.name === "traefik"), hcc = traefikHelmConfig(chart, traefik, intent);
  await kube(["create", "--dry-run=server", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(hcc));
  const chartBytes = await run("kubectl", ["--context", context, "--request-timeout=15s", "get", "--raw=" + config.traefikChart.apiPath]);
  requireValue(hash(chartBytes) === config.traefikChart.sha256, "NATIVE_TRAEFIK_CHART_HASH_MISMATCH");
  const chartPath = options["--output"] + ".traefik.tgz"; await writePrivate(chartPath, chartBytes);
  const decode = async raw => (await text("yq", ["eval-all", "-N", "-o=json", "-I=0", ".", "-"], raw)).trim().split("\n").filter(Boolean).map(JSON.parse).filter(Boolean);
  const metadata = JSON.parse(await text("helm", ["get", "metadata", "traefik", "--namespace", "kube-system", "--kube-context", context, "-o", "json"]));
  requireValue(metadata.name === "traefik" && metadata.chart === "traefik" && metadata.version === config.traefikChart.version &&
    metadata.status === "deployed" && Number.isSafeInteger(metadata.revision), "NATIVE_TRAEFIK_RELEASE_INVALID");
  const before = await decode(await text("helm", ["get", "manifest", "traefik", "--namespace", "kube-system", "--kube-context", context, "--revision", String(metadata.revision)]));
  const sourceValues = (await decode(chart.spec.valuesContent))[0];
  requireValue(sourceValues && typeof sourceValues === "object" && !Array.isArray(sourceValues), "NATIVE_TRAEFIK_VALUES_INVALID");
  if (Object.hasOwn(chart.spec.set ?? {}, "global.systemDefaultRegistry")) {
    requireValue(typeof chart.spec.set["global.systemDefaultRegistry"] === "string", "NATIVE_TRAEFIK_VALUES_INVALID");
    sourceValues.global = { ...sourceValues.global, systemDefaultRegistry: chart.spec.set["global.systemDefaultRegistry"] };
  }
  const values = systemDesiredValues(sourceValues, JSON.parse(hcc.spec.valuesContent));
  const dryRun = JSON.parse(await text("helm", ["upgrade", "traefik", chartPath, "--namespace", "kube-system", "--kube-context", context,
    "--reset-values", "--values", "-", "--dry-run=server", "--no-hooks", "-o", "json"], JSON.stringify(values), 180000));
  const transition = nativeTraefikTransition(before, await decode(dryRun.manifest), traefik);
  for (const target of targets) {
    const d = await get("deployment", target.name);
    requireValue(d.metadata.uid === target.uid && fingerprint(d.spec) === target.beforeSHA256, "NATIVE_SOURCE_CHANGED_DURING_PLAN");
  }
  requireValue((await get("helmchart", "traefik")).metadata.uid === chart.metadata.uid &&
    fingerprint((await get("helmchart", "traefik")).spec) === fingerprint(chart.spec) &&
    (await get("namespace", "kube-system")).metadata.uid === clusterUID && (await text("git", ["rev-parse", "HEAD"])).trim() === source &&
    !(await text("git", ["status", "--porcelain", "--untracked-files=normal"])).trim(), "NATIVE_EXECUTOR_OR_CLUSTER_DRIFT");
  const plan = { schema: "kodex.stable-native-plan/v1", source, context, clusterUID, intent, profileSHA256: fingerprint(config),
    nodes: bindings, targets, addons: addonRows, traefik: { uid: chart.metadata.uid, specSHA256: fingerprint(chart.spec),
      releaseRevision: metadata.revision, metadataSHA256: fingerprint(metadata), chartSHA256: hash(chartBytes), chartPath, hcc, ...transition }, apply: "NOT_RUN" };
  const document = { plan, planSHA256: fingerprint(plan) };
  await writePrivate(options["--output"], JSON.stringify(document) + "\n"); return document;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  planNative(process.argv.slice(2)).then(d => console.log(JSON.stringify({ status: "PASS", planSHA256: d.planSHA256,
    source: d.plan.source, targets: d.plan.targets.map(t => t.name), apply: "NOT_RUN" })))
    .catch(error => { console.error(JSON.stringify({ status: "FAIL", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "NATIVE_PLAN_FAILED" })); process.exitCode = 1; });
