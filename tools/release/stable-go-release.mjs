#!/usr/bin/env node
import { execFile } from "node:child_process";
import { open, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { prepareGoCutover, validateGoReleaseReference, goResourceComponent } from "./stable-cutover-model.mjs";
import { requireIdle } from "./runner-policy-model.mjs";
import { archiveTransition, sameArchiveBoundary, activeDeploymentPods } from "./stable-archive-transition.mjs";
import { prepareGoRecovery } from "./stable-go-recovery.mjs";
import { constants } from "node:fs";

const execute = promisify(execFile);
const sqlPath = fileURLToPath(new URL("../dev/supply-chain-owner-readback.sql", import.meta.url));
const archiveSQLPath = fileURLToPath(new URL("./stable-archive-idle.sql", import.meta.url));
const archiveRolePath = fileURLToPath(new URL("../../deploy/k8s/base/session-archive/runtime/rbac.yaml", import.meta.url));
function requireValue(value, code) { if (!value) throw Error(code); }
function publicPlan(target) {
  const { patch: _patch, ...result } = target;
  return result;
}
async function createPrivate(path, value) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(JSON.stringify(value) + "\n"); await file.sync(); }
  finally { await file.close(); }
}

async function main(args) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--manifest", "--plan", "--output", "--approved-plan-sha", "--journal"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "STABLE_ARGUMENTS_INVALID");
    options[key] = args.shift();
  }
  const context = options["--context"];
  requireValue(["plan", "apply", "readback", "inspect", "rollback"].includes(mode) && /^[A-Za-z0-9_.:@/-]{1,160}$/.test(context ?? "") &&
    !/prod(?:uction)?/i.test(context) && options["--output"], "STABLE_ARGUMENTS_INVALID");
  requireValue(mode === "rollback" ? options["--journal"] : !options["--journal"], "STABLE_RECOVERY_JOURNAL_REQUIRED");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const root = fileURLToPath(new URL("../../", import.meta.url));
  const git = async argv => (await execute("git", argv, { cwd: root, env, timeout: 15000, maxBuffer: 1 << 20 })).stdout.trim();
  const executorSHA = await git(["rev-parse", "HEAD"]);
  requireValue(/^[a-f0-9]{40}$/.test(executorSHA) && await git(["status", "--porcelain", "--untracked-files=normal"]) === "",
    "STABLE_CLEAN_SOURCE_REQUIRED");
  const kubectl = async (arguments_, input, timeout = 35000) => {
    // Node stdin — socketpair. cat создаёт настоящий pipe для /dev/stdin.
    // Полный patch не попадает в argv или вывод вызывающей стороны.
    const command = input === undefined ? "kubectl" : "/bin/sh";
    const argv = ["--context", context, "--request-timeout=30s", ...arguments_];
    const invocation = input === undefined ? argv : ["-c", 'cat | kubectl "$@"', "kodex-stable-pipe", ...argv];
    try {
      const pending = execute(command, invocation, { env, timeout, maxBuffer: 8 << 20 });
      if (input !== undefined) {
        pending.child.stdin.on("error", () => {});
        pending.child.stdin.end(input);
      }
      const { stdout } = await pending;
      return stdout;
    } catch { throw Error("STABLE_KUBERNETES_OPERATION_FAILED"); }
  };
  const get = async (kind, name, namespace = "kodex-system") =>
    JSON.parse(await kubectl(["-n", namespace, "get", kind, name, "-o", "json"]));
  const clusterUID = (await get("namespace", "kube-system", "default")).metadata.uid;
  const appUID = (await get("namespace", "kodex-system", "default")).metadata.uid;
  let desiredArchiveRole;
  const archiveRole = async () => {
    if (!desiredArchiveRole) {
      try {
        const { stdout } = await execute("yq", ["eval-all", "-o=json", "-I=0",
          'select(.kind == "Role" and .metadata.name == "session-archive-controller")', archiveRolePath],
        { env, timeout: 10000, maxBuffer: 64 << 10 });
        desiredArchiveRole = JSON.parse(stdout);
      } catch { throw Error("STABLE_ARCHIVE_DESIRED_ROLE_UNAVAILABLE"); }
    }
    return archiveTransition(await get("role", "session-archive-controller", "kodex-runtime"),
      await get("rolebinding", "session-archive-controller", "kodex-runtime"), desiredArchiveRole);
  };
  const podList = async name => {
    // У migrator то же app name, но иной component. Его завершённые Pod не
    // являются serving replicas; lineage выбранного компонента проверяется ниже.
    const deployment = await get("deployment", name);
    const component = deployment.spec?.template?.metadata?.labels?.["app.kubernetes.io/component"];
    requireValue(/^[a-z][a-z0-9-]{0,62}$/.test(component ?? ""), "STABLE_COMPONENT_LABEL_REQUIRED");
    return JSON.parse(await kubectl(["-n", "kodex-system", "get", "replicasets,pods", "-l",
      `app.kubernetes.io/name=${name},app.kubernetes.io/component=${component}`, "-o", "json"]));
  };
  const storageFor = async deployment => {
    const claims = [];
    for (const v of deployment.spec.template.spec.volumes ?? []) {
      if (!v.persistentVolumeClaim) continue;
      const name = v.persistentVolumeClaim.claimName, claim = await get("pvc", name);
      requireValue(claim.status.phase === "Bound" && claim.spec.volumeName, "STORAGE_CLAIM_NOT_BOUND");
      const pv = await get("pv", claim.spec.volumeName);
      const terms = pv.spec.nodeAffinity?.required?.nodeSelectorTerms ?? [];
      const nodeName = terms.length === 1 ? terms[0].matchExpressions?.find(e => e.key === "kubernetes.io/hostname" &&
        e.operator === "In" && e.values?.length === 1)?.values[0] : undefined;
      claims.push({ name, bound: true, accessModes: claim.spec.accessModes, nodeName,
        commitment: fingerprint({ claimUID: claim.metadata.uid, spec: claim.spec, pvUID: pv.metadata.uid, pvSpec: pv.spec }) });
    }
    return claims;
  };
  const prepare = async requested => {
    const deployment = await get("deployment", requested.name);
    const storageClaims = await storageFor(deployment);
    const renderer = await readFile(fileURLToPath(new URL("../dev/render-local.sh", import.meta.url)), "utf8");
    const scripts = [...renderer.matchAll(/\.args = (\["(?:[^"\\]|\\.)*"\])/g)].map(m => JSON.parse(m[1]))
      .filter(a => a[0].includes("--prepare-artifact-spool"));
    requireValue(scripts.length === 1, "STABLE_SPOOL_SOURCE_UNAVAILABLE");
    const component = goResourceComponent(requested.name);
    let resourceProfile;
    try {
      const { stdout } = await execute("yq", ["eval", "-o=json", "-I=0",
        `{"main": ([.spec.template.spec.containers[] | select(.name == "${component}") | .resources] | .[0]), "spoolInit": ([.spec.template.spec.initContainers[]? | select(.name == "artifact-spool-init") | .resources] | .[0])}`,
        fileURLToPath(new URL(`../../deploy/k8s/base/${component}/deployment.yaml`, import.meta.url))],
      { env, timeout: 10000, maxBuffer: 64 << 10 });
      resourceProfile = JSON.parse(stdout);
    } catch { throw Error("STABLE_CANONICAL_RESOURCES_UNAVAILABLE"); }
    const prepared = prepareGoCutover(deployment, requested.release, { storageClaims, sourceSpoolScript: scripts[0][0], resourceProfile });
    const dry = JSON.parse(await kubectl(["-n", "kodex-system", "patch", "deployment", requested.name, "--type=json",
      "--dry-run=server", "--patch-file=/dev/stdin", "-o", "json"], JSON.stringify(prepared.patch)));
    requireValue(fingerprint(dry.spec) === prepared.afterSpecSHA256, "STABLE_SERVER_DEFAULTS_MISMATCH");
    return { ...prepared,
      storageSHA256: fingerprint(storageClaims), requested,
      ...(requested.name === "session-archive" ? { archive: (await archiveRole()).proof } : {}) };
  };
  if (mode === "plan") {
    requireValue(options["--manifest"] && !options["--plan"] && !options["--approved-plan-sha"], "STABLE_ARGUMENTS_INVALID");
    const manifest = await readPrivateJSON(options["--manifest"]);
    requireValue(manifest.schema === "kodex.stable-go-manifest/v1" && Array.isArray(manifest.targets) &&
      manifest.targets.length > 0 && manifest.targets.length <= 16 &&
      new Set(manifest.targets.map(t => t.name)).size === manifest.targets.length, "STABLE_MANIFEST_INVALID");
    const targets = [];
    for (const requested of manifest.targets) {
      validateGoReleaseReference(requested.name, requested.release);
      targets.push(publicPlan(await prepare(requested)));
    }
    const plan = { schema: "kodex.stable-go-plan/v3", context, clusterUID, appUID, executorSHA, targets };
    await createPrivate(options["--output"], plan);
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", targets: targets.length,
      planSHA256: fingerprint(plan) }) + "\n");
    return;
  }
  requireValue(options["--plan"] && !options["--manifest"], "STABLE_ARGUMENTS_INVALID");
  const plan = await readPrivateJSON(options["--plan"]);
  requireValue(plan.schema === "kodex.stable-go-plan/v3" && plan.context === context && plan.clusterUID === clusterUID &&
    plan.appUID === appUID && plan.executorSHA === executorSHA &&
    Array.isArray(plan.targets) && plan.targets.length > 0 && plan.targets.length <= 16 &&
    new Set(plan.targets.map(t => t.name)).size === plan.targets.length, "STABLE_PLAN_IDENTITY_MISMATCH");
  for (const item of plan.targets) {
    validateGoReleaseReference(item.name, item.requested?.release);
    requireValue(item.requested.name === item.name && item.recovery &&
      item.recovery.mode === (item.name === "session-archive" ? "ARCHIVE_PAUSE_FORWARD_ONLY" : "RESTORE_SOURCE") &&
      Array.isArray(item.recovery.inverse) && Number.isSafeInteger(item.recovery.beforeReplicas) && item.recovery.beforeReplicas >= 0 &&
      item.recovery.beforeReplicas <= 256 && ["beforeLabelsSHA256", "afterLabelsSHA256", "quiescedAfterSpecSHA256"]
        .every(k => /^[a-f0-9]{64}$/.test(item.recovery[k] ?? "")), "STABLE_RECOVERY_PLAN_INVALID");
    if (item.name === "session-archive") requireValue(item.archive &&
      /^[a-f0-9]{64}$/.test(item.quiescedBeforeSpecSHA256 ?? ""), "STABLE_ARCHIVE_PLAN_INVALID");
  }
  const idle = async () => {
    const stdout = await kubectl(["-n", "kodex-system", "exec", "-i", "kodex-postgresql-0", "--",
      "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "control_plane"], await readFile(sqlPath, "utf8"));
    const value = JSON.parse(stdout.trim());
    requireIdle(value);
    if (plan.targets.some(target => target.name === "session-archive")) {
      const archive = JSON.parse((await kubectl(["-n", "kodex-system", "exec", "-i", "kodex-postgresql-0", "--",
        "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "control_plane"],
      await readFile(archiveSQLPath, "utf8"))).trim());
      requireValue(archive.openArchiveTasks === 0 && archive.transitionalStorage === 0, "STABLE_ARCHIVE_NOT_IDLE");
      const workers = JSON.parse(await kubectl(["-n", "kodex-runtime", "get", "jobs,pods", "-l",
        "session-archive.kodex.dev/managed=true", "-o", "json"]));
      requireValue(Array.isArray(workers.items) && workers.items.length === 0, "STABLE_ARCHIVE_WORKERS_PRESENT");
    }
    return { promotedArtifactCount: value.promotedArtifactCount, promotedPinsSHA256: value.promotedPinsSHA256 };
  };
  const readback = async item => {
    const current = await get("deployment", item.name);
    requireValue(current.metadata.uid === item.uid && fingerprint(current.spec) === item.afterSpecSHA256 &&
      fingerprint(current.metadata.labels ?? {}) === item.recovery.afterLabelsSHA256 &&
      current.spec.template.metadata.annotations?.["kodex.dev/source-sha"] === item.requested.release.sourceSHA &&
      !current.spec.template.spec.volumes?.some(v => v.hostPath) &&
      current.spec.template.spec.containers.some(c => c.name === item.name && c.command?.length === 1 &&
        c.command[0].startsWith("/usr/local/bin/")) &&
      current.spec.replicas === 2 && current.status.observedGeneration >= current.metadata.generation &&
      current.status.replicas === 2 && current.status.readyReplicas === 2 && current.status.availableReplicas === 2 && current.status.updatedReplicas === 2,
      "STABLE_READBACK_MISMATCH");
    if (item.name === "session-archive") requireValue(sameArchiveBoundary((await archiveRole()).proof, item.archive, "after"),
      "STABLE_ARCHIVE_ROLE_READBACK_MISMATCH");
    let pods = activeDeploymentPods(await podList(item.name), item.uid);
    const desiredPod = p => p.spec.containers.some(c => c.name === item.name && c.image === item.requested.release.image);
    // rollout status подтверждает новые реплики раньше завершения старого
    // terminating Pod. Не считаем его исчезнувшим и не повторяем mutation:
    // ждём только exact owned старый image при двух уже наблюдаемых новых Pod.
    const deadline = Date.now() + Math.min(330, Math.max(30, current.spec.template.spec.terminationGracePeriodSeconds ?? 30) + 30) * 1000;
    while (pods.length > 2 && pods.filter(desiredPod).length === 2 &&
      pods.filter(p => !desiredPod(p)).every(p => p.metadata.deletionTimestamp)) {
      requireValue(Date.now() < deadline, "STABLE_OLD_PODS_JOIN_TIMEOUT");
      await new Promise(resolve => setTimeout(resolve, 250));
      const fresh = await get("deployment", item.name);
      requireValue(fresh.metadata.uid === item.uid && fingerprint(fresh.spec) === item.afterSpecSHA256 &&
        fingerprint(fresh.metadata.labels ?? {}) === item.recovery.afterLabelsSHA256, "STABLE_CONVERGENCE_DRIFT");
      pods = activeDeploymentPods(await podList(item.name), item.uid);
    }
    const digest = item.requested.release.image.split("@").at(-1);
    requireValue(pods.length === 2 && pods.every(p => !p.metadata.deletionTimestamp &&
      p.spec.containers.some(c => c.name === item.name && c.image === item.requested.release.image) &&
      p.status.conditions?.some(c => c.type === "Ready" && c.status === "True") &&
      p.status.containerStatuses?.some(c => c.name === item.name && c.ready === true && c.imageID?.endsWith(digest))),
      "STABLE_SERVING_POD_MISMATCH");
    return { name: item.name, status: "PASS", specSHA256: item.afterSpecSHA256,
      pods: pods.map(p => ({ uid: p.metadata.uid, servedDigest: digest })) };
  };
  if (mode === "apply") {
    requireValue(options["--approved-plan-sha"] === fingerprint(plan), "STABLE_EXACT_OWNER_CONFIRMATION_REQUIRED");
    // Обязательные review/owner решение принадлежат процессу поставки.
    // Этот аргумент подтверждает exact plan, но сам не заменяет решение владельца.
    for (const item of plan.targets) {
      const prepared = await prepare(item.requested);
      requireValue(prepared.uid === item.uid && prepared.beforeSpecSHA256 === item.beforeSpecSHA256 &&
        prepared.afterSpecSHA256 === item.afterSpecSHA256 && prepared.storageSHA256 === item.storageSHA256 &&
        prepared.recovery.beforeLabelsSHA256 === item.recovery.beforeLabelsSHA256 &&
        prepared.recovery.afterLabelsSHA256 === item.recovery.afterLabelsSHA256,
        "STABLE_PLAN_DRIFT");
      if (item.name === "session-archive") requireValue(sameArchiveBoundary(prepared.archive, item.archive), "STABLE_ARCHIVE_ROLE_DRIFT");
    }
    const preserved = await idle();
    await createPrivate(options["--output"], { schema: "kodex.stable-go-apply/v1", status: "IN_PROGRESS",
      planSHA256: fingerprint(plan), preserved });
    const journal = await open(options["--output"], "a");
    const record = async value => { await journal.writeFile(JSON.stringify(value) + "\n"); await journal.sync(); };
    try {
      for (const item of plan.targets) {
        requireValue(fingerprint(await idle()) === fingerprint(preserved), "STABLE_PINS_CHANGED");
        let prepared = await prepare(item.requested);
        requireValue(prepared.uid === item.uid && prepared.beforeSpecSHA256 === item.beforeSpecSHA256 &&
          prepared.afterSpecSHA256 === item.afterSpecSHA256 && prepared.storageSHA256 === item.storageSHA256 &&
          prepared.recovery.beforeLabelsSHA256 === item.recovery.beforeLabelsSHA256 &&
          prepared.recovery.afterLabelsSHA256 === item.recovery.afterLabelsSHA256, "STABLE_PLAN_DRIFT");
        if (item.name === "session-archive") {
          requireValue(sameArchiveBoundary(prepared.archive, item.archive), "STABLE_ARCHIVE_ROLE_DRIFT");
          const oldSpec = prepared.patch[2].value;
          const quiesced = { ...oldSpec, replicas: 0 };
          requireValue(fingerprint(quiesced) === item.quiescedBeforeSpecSHA256, "STABLE_ARCHIVE_QUIESCE_PLAN_MISMATCH");
          await record({ name: item.name, status: "QUIESCE_ATTEMPT" });
          await kubectl(["-n", "kodex-system", "patch", "deployment", item.name, "--type=json",
            "--patch-file=/dev/stdin", "-o", "name"], JSON.stringify([...prepared.patch.slice(0, 3),
            { op: "replace", path: "/spec", value: quiesced }]));
          const deadline = Date.now() + 120000;
          for (;;) {
            const current = await get("deployment", item.name);
            requireValue(current.metadata.uid === item.uid && fingerprint(current.spec) === item.quiescedBeforeSpecSHA256,
              "STABLE_ARCHIVE_QUIESCE_DRIFT");
            const pods = activeDeploymentPods(await podList(item.name), item.uid);
            if (current.status?.observedGeneration >= current.metadata.generation &&
              (current.status.replicas ?? 0) === 0 && (current.status.readyReplicas ?? 0) === 0 &&
              (current.status.availableReplicas ?? 0) === 0 && pods.length === 0) break;
            requireValue(Date.now() < deadline, "STABLE_ARCHIVE_QUIESCE_TIMEOUT");
            await new Promise(resolve => setTimeout(resolve, 250));
          }
          await record({ name: item.name, status: "QUIESCED", specSHA256: item.quiescedBeforeSpecSHA256 });
          requireValue(fingerprint(await idle()) === fingerprint(preserved), "STABLE_PINS_CHANGED");
          const boundary = await archiveRole();
          requireValue(sameArchiveBoundary(boundary.proof, item.archive), "STABLE_ARCHIVE_ROLE_DRIFT");
          if (boundary.proof.beforeRulesSHA256 !== boundary.proof.afterRulesSHA256) {
            await record({ name: item.name, status: "ROLE_PATCH_ATTEMPT" });
            await kubectl(["-n", "kodex-runtime", "patch", "role", "session-archive-controller", "--type=json",
              "--patch-file=/dev/stdin", "-o", "name"], JSON.stringify(boundary.patch));
          }
          requireValue(sameArchiveBoundary((await archiveRole()).proof, item.archive, "after"), "STABLE_ARCHIVE_ROLE_READBACK_MISMATCH");
          prepared = await prepare(item.requested);
          requireValue(prepared.uid === item.uid && prepared.beforeSpecSHA256 === item.quiescedBeforeSpecSHA256 &&
            prepared.afterSpecSHA256 === item.afterSpecSHA256 && prepared.storageSHA256 === item.storageSHA256 &&
            prepared.recovery.beforeLabelsSHA256 === item.recovery.beforeLabelsSHA256 &&
            prepared.recovery.afterLabelsSHA256 === item.recovery.afterLabelsSHA256,
            "STABLE_ARCHIVE_ACTIVATION_DRIFT");
          requireValue(sameArchiveBoundary(prepared.archive, item.archive, "after"), "STABLE_ARCHIVE_ROLE_DRIFT");
        }
        await record({ name: item.name, status: "PATCH_ATTEMPT" });
        await kubectl(["-n", "kodex-system", "patch", "deployment", item.name, "--type=json",
          "--patch-file=/dev/stdin", "-o", "name"], JSON.stringify(prepared.patch));
        await kubectl(["-n", "kodex-system", "rollout", "status", `deployment/${item.name}`, "--timeout=600s"], undefined, 635000);
        await record(await readback(item));
      }
      requireValue(fingerprint(await idle()) === fingerprint(preserved), "STABLE_PINS_CHANGED");
      await record({ status: "PASS" });
      process.stdout.write(JSON.stringify({ status: "PASS", phase: "GO_CUTOVER", targets: plan.targets.length }) + "\n");
    } catch (error) {
      await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "STABLE_APPLY_FAILED" });
      throw error;
    } finally { await journal.close(); }
    return;
  }
  if (mode === "rollback") {
    requireValue(options["--approved-plan-sha"] === fingerprint(plan), "STABLE_EXACT_OWNER_CONFIRMATION_REQUIRED");
    const input = await open(options["--journal"], constants.O_RDONLY | constants.O_NOFOLLOW);
    let receipt;
    try {
      const stat = await input.stat();
      requireValue(stat.isFile() && stat.uid === process.getuid() && stat.nlink === 1 &&
        (stat.mode & 0o777) === 0o600 && stat.size > 0 && stat.size <= 1 << 20, "STABLE_PRIVATE_JOURNAL_REQUIRED");
      const records = (await input.readFile("utf8")).trim().split("\n").map(JSON.parse);
      receipt = records[0];
      requireValue(records.length <= 256 && receipt.schema === "kodex.stable-go-apply/v1" &&
        receipt.planSHA256 === fingerprint(plan) && receipt.status === "IN_PROGRESS", "STABLE_RECOVERY_JOURNAL_MISMATCH");
    } finally { await input.close(); }
    const preserved = await idle();
    requireValue(fingerprint(preserved) === fingerprint(receipt.preserved), "STABLE_PINS_CHANGED");
    const preflight = async item => {
      const current = await get("deployment", item.name), recovery = prepareGoRecovery(current, item);
      requireValue(fingerprint(await storageFor(current)) === item.storageSHA256, "STABLE_RECOVERY_STORAGE_DRIFT");
      if (item.name === "session-archive") {
        const boundary = (await archiveRole()).proof;
        requireValue(sameArchiveBoundary(boundary, item.archive) || sameArchiveBoundary(boundary, item.archive, "after"),
          "STABLE_ARCHIVE_ROLE_DRIFT");
        requireValue(!recovery.compiled || sameArchiveBoundary(boundary, item.archive, "after"), "STABLE_ARCHIVE_ROLE_DRIFT");
        requireValue(!recovery.unchanged || sameArchiveBoundary(boundary, item.archive), "STABLE_ARCHIVE_ROLE_DRIFT");
      }
      return { current, recovery };
    };
    // Проверяется весь набор до первого effect, включая чужие PVC/UID/Role.
    for (const item of plan.targets) await preflight(item);
    await createPrivate(options["--output"], { schema: "kodex.stable-go-recovery/v1", planSHA256: fingerprint(plan), status: "IN_PROGRESS" });
    const journal = await open(options["--output"], "a");
    const record = async row => { await journal.writeFile(JSON.stringify(row) + "\n"); await journal.sync(); };
    const patch = async (kind, name, namespace, resource, operations) => kubectl(["-n", namespace, "patch", kind, name,
      "--type=json", "--patch-file=/dev/stdin", "-o", "name"], JSON.stringify([
      { op: "test", path: "/metadata/uid", value: resource.metadata.uid },
      { op: "test", path: "/metadata/resourceVersion", value: resource.metadata.resourceVersion }, ...operations]));
    const proof = async (item, expectedSpec) => {
      const current = await get("deployment", item.name), pods = activeDeploymentPods(await podList(item.name), item.uid);
      const replicas = expectedSpec.replicas ?? 1;
      const serving = pods.every(p => {
        if (p.metadata.deletionTimestamp || !p.status?.conditions?.some(c => c.type === "Ready" && c.status === "True")) return false;
        return expectedSpec.template.spec.containers.every(expected => {
          const actual = p.spec.containers?.find(c => c.name === expected.name);
          const status = p.status.containerStatuses?.find(c => c.name === expected.name);
          return actual?.image === expected.image && fingerprint(actual.command ?? []) === fingerprint(expected.command ?? []) &&
            fingerprint(actual.args ?? []) === fingerprint(expected.args ?? []) && status?.ready &&
            (!expected.image.includes("@sha256:") || status.imageID?.endsWith(expected.image.split("@").at(-1)));
        });
      });
      requireValue(current.metadata.uid === item.uid && fingerprint(current.spec) === fingerprint(expectedSpec) &&
        fingerprint(current.metadata.labels ?? {}) === item.recovery.beforeLabelsSHA256 &&
        current.status?.observedGeneration >= current.metadata.generation &&
        ["replicas", "updatedReplicas", "readyReplicas", "availableReplicas"].every(k => (current.status[k] ?? 0) === replicas) &&
        pods.length === replicas && serving,
        "STABLE_RECOVERY_READBACK_MISMATCH");
      requireValue(fingerprint(await storageFor(current)) === item.storageSHA256, "STABLE_RECOVERY_STORAGE_DRIFT");
    };
    const recovered = [];
    let archivePaused = false;
    try {
      for (const item of [...plan.targets].reverse()) {
        requireValue(fingerprint(await idle()) === fingerprint(preserved), "STABLE_PINS_CHANGED");
        let { current, recovery } = await preflight(item);
        if (recovery.unchanged) {
          await proof(item, current.spec); recovered.push({ item, spec: current.spec });
          await record({ name: item.name, status: "SOURCE_UNCHANGED" }); continue;
        }
        if (current.spec.replicas !== 0) {
          await record({ name: item.name, status: "QUIESCE_ATTEMPT" });
          await patch("deployment", item.name, "kodex-system", current, [
            { op: "test", path: "/spec", value: current.spec }, { op: "replace", path: "/spec/replicas", value: 0 }]);
        }
        const expectedQuiesced = fingerprint({ ...current.spec, replicas: 0 }), expectedLabels = fingerprint(current.metadata.labels ?? {});
        const deadline = Date.now() + 120000;
        for (;;) {
          current = await get("deployment", item.name);
          requireValue(current.metadata.uid === item.uid && fingerprint(current.spec) === expectedQuiesced &&
            fingerprint(current.metadata.labels ?? {}) === expectedLabels,
            "STABLE_RECOVERY_QUIESCE_DRIFT");
          const pods = activeDeploymentPods(await podList(item.name), item.uid);
          if (current.status?.observedGeneration >= current.metadata.generation && !(current.status.replicas ?? 0) && pods.length === 0) break;
          requireValue(Date.now() < deadline, "STABLE_RECOVERY_QUIESCE_TIMEOUT");
          await new Promise(resolve => setTimeout(resolve, 250));
        }
        // Собственное quiesce меняет только replicas; generic source0 не является
        // отдельной фазой плана, поэтому inverse выводится из исходного snapshot.
        requireValue(fingerprint(await idle()) === fingerprint(preserved), "STABLE_PINS_CHANGED");
        requireValue(fingerprint(await storageFor(current)) === item.storageSHA256, "STABLE_RECOVERY_STORAGE_DRIFT");
        if (item.name === "session-archive") {
          const boundary = await archiveRole();
          requireValue(sameArchiveBoundary(boundary.proof, item.archive) || sameArchiveBoundary(boundary.proof, item.archive, "after"),
            "STABLE_ARCHIVE_ROLE_DRIFT");
          if (!sameArchiveBoundary(boundary.proof, item.archive, "after")) {
            await record({ name: item.name, status: "SAFE_ROLE_PATCH_ATTEMPT" });
            await kubectl(["-n", "kodex-runtime", "patch", "role", "session-archive-controller", "--type=json", "--patch-file=/dev/stdin", "-o", "name"],
              JSON.stringify(boundary.patch));
          }
          requireValue(sameArchiveBoundary((await archiveRole()).proof, item.archive, "after"), "STABLE_ARCHIVE_ROLE_DRIFT");
          archivePaused = true;
        }
        const labelOperation = item.recovery.beforeLabelsPresent ?
          { op: "add", path: "/metadata/labels", value: recovery.restoredLabels } :
          { op: "remove", path: "/metadata/labels" };
        await record({ name: item.name, status: "RESTORE_ATTEMPT" });
        await patch("deployment", item.name, "kodex-system", current, [
          { op: "test", path: "/spec", value: current.spec },
          { op: "replace", path: "/spec", value: recovery.restoredSpec },
          ...(Object.hasOwn(current.metadata, "labels") || item.recovery.beforeLabelsPresent ? [labelOperation] : [])]);
        await kubectl(["-n", "kodex-system", "rollout", "status", `deployment/${item.name}`, "--timeout=600s"], undefined, 635000);
        await proof(item, recovery.restoredSpec); recovered.push({ item, spec: recovery.restoredSpec });
        await record({ name: item.name, status: recovery.archivePause ? "ARCHIVE_PAUSED_FORWARD_ONLY" : "SOURCE_PROFILE_RESTORED" });
      }
      requireValue(fingerprint(await idle()) === fingerprint(preserved), "STABLE_PINS_CHANGED");
      for (const { item, spec } of recovered) {
        await proof(item, spec);
        if (item.name === "session-archive") requireValue(sameArchiveBoundary((await archiveRole()).proof, item.archive,
          archivePaused ? "after" : "before"), "STABLE_ARCHIVE_ROLE_DRIFT");
      }
      const result = { status: "PASS", phase: "GO_RECOVERY", archivePaused, releaseComplete: false, browser: "NOT_RUN" };
      await record(result); process.stdout.write(JSON.stringify(result) + "\n");
    } catch (error) {
      await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "STABLE_RECOVERY_FAILED" }); throw error;
    } finally { await journal.close(); }
    return;
  }
  requireValue(!options["--approved-plan-sha"], "STABLE_ARGUMENTS_INVALID");
  if (mode === "inspect") {
    const results = [];
    for (const item of plan.targets) {
      const current = await get("deployment", item.name), specSHA256 = fingerprint(current.spec);
      requireValue(current.metadata.uid === item.uid, "STABLE_INSPECTION_IDENTITY_DRIFT");
      let phase;
      if (specSHA256 === item.afterSpecSHA256) {
        await readback(item);
        phase = "COMPILED_TWO_READY";
      } else if (specSHA256 === item.recovery.quiescedAfterSpecSHA256) {
        prepareGoRecovery(current, item);
        if (item.name === "session-archive") requireValue(sameArchiveBoundary((await archiveRole()).proof, item.archive, "after"), "STABLE_ARCHIVE_ROLE_DRIFT");
        const pods = activeDeploymentPods(await podList(item.name), item.uid);
        phase = current.status?.observedGeneration >= current.metadata.generation && !(current.status.replicas ?? 0) && !pods.length ?
          "COMPILED_PAUSED" : "COMPILED_QUIESCING";
      } else if (item.name === "session-archive" && specSHA256 === item.quiescedBeforeSpecSHA256) {
        const boundary = (await archiveRole()).proof;
        const before = sameArchiveBoundary(boundary, item.archive), after = sameArchiveBoundary(boundary, item.archive, "after");
        requireValue(before || after, "STABLE_ARCHIVE_ROLE_DRIFT");
        const pods = activeDeploymentPods(await podList(item.name), item.uid);
        const joined = current.status?.observedGeneration >= current.metadata.generation &&
          (current.status.replicas ?? 0) === 0 && (current.status.readyReplicas ?? 0) === 0 &&
          (current.status.availableReplicas ?? 0) === 0 && pods.length === 0;
        requireValue(joined || before, "STABLE_ARCHIVE_ROLE_ACTIVATED_BEFORE_JOIN");
        phase = joined ? (after ? "QUIESCED_ROLE_READY" : "QUIESCED") : "QUIESCING";
      } else {
        requireValue(specSHA256 === item.beforeSpecSHA256, "STABLE_INSPECTION_SPEC_DRIFT");
        if (item.name === "session-archive") requireValue(sameArchiveBoundary((await archiveRole()).proof, item.archive),
          "STABLE_ARCHIVE_ROLE_DRIFT");
        phase = "SOURCE_UNCHANGED";
      }
      results.push({ name: item.name, uid: item.uid, phase, complete: phase === "COMPILED_TWO_READY", specSHA256 });
    }
    await createPrivate(options["--output"], { schema: "kodex.stable-go-inspection/v1", planSHA256: fingerprint(plan), results });
    process.stdout.write(JSON.stringify({ status: "OBSERVED", phase: "INSPECTION_ONLY", results }) + "\n");
    return;
  }
  const results = [];
  for (const item of plan.targets) {
    results.push(await readback(item));
  }
  await createPrivate(options["--output"], { schema: "kodex.stable-go-readback/v1", planSHA256: fingerprint(plan), results });
  process.stdout.write(JSON.stringify({ status: "PASS", phase: "DEPLOYMENT_READBACK", results }) + "\n");
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  main(process.argv.slice(2)).catch(error => {
    const code = /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "STABLE_RELEASE_FAILED";
    process.stderr.write(JSON.stringify({ status: "FAIL", code }) + "\n"); process.exitCode = 1;
  });
