#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import {
  closeSync,
  constants,
  fsyncSync,
  lstatSync,
  mkdtempSync,
  openSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeSync,
} from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint } from "../release/scoped-release.mjs";
import { inspectSource } from "../release/application-source.mjs";

const requireValue = (value, code) => {
  if (!value) throw new Error(code);
};
const namespace = "kodex-system",
  controllerName = "image-admission-controller";
const imagePattern =
  /^registry\.local\.kodex\/kodex\/image-admission@sha256:[a-f0-9]{64}$/;
const uidPattern =
  /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const runPattern = /^v[0-9]{14}-[a-f0-9]{40}$/;
const label = "kodex.dev/image-admission-orchestrated",
  idLabel = "kodex.dev/image-admission-id",
  runKey = "kodex.dev/admission-run-id";
const terminal = (job) =>
  job.status?.active !== 1 &&
  (job.status?.active ?? 0) === 0 &&
  job.status?.conditions?.some(
    (c) => ["Complete", "Failed"].includes(c.type) && c.status === "True",
  );
function literal(app, key) {
  const found = app.env?.filter((e) => e.name === key) ?? [];
  requireValue(
    found.length === 1 &&
      typeof found[0].value === "string" &&
      !found[0].valueFrom,
    "EXACT_LITERAL_CONFIGURATION_REQUIRED",
  );
  return found[0];
}
function identity(resource, name, kind) {
  requireValue(
    resource?.kind === kind &&
      resource.metadata?.name === name &&
      (kind === "ValidatingAdmissionPolicyBinding"
        ? resource.metadata.namespace === undefined
        : resource.metadata.namespace === namespace) &&
      uidPattern.test(resource.metadata.uid ?? "") &&
      /^\d+$/.test(resource.metadata.resourceVersion ?? "") &&
      !resource.metadata.deletionTimestamp,
    "EXACT_RESOURCE_REQUIRED",
  );
}
function pin(resource) {
  return {
    uid: resource.metadata.uid,
    resourceVersion: resource.metadata.resourceVersion,
    sha256: fingerprint(
      resource.spec ?? { data: resource.data, immutable: resource.immutable },
    ),
  };
}
function workPin(resource) {
  return {
    ...pin(resource),
    name: resource.metadata.name,
    kind: resource.kind,
    metadataSHA256: fingerprint({
      labels: resource.metadata.labels,
      annotations: resource.metadata.annotations,
    }),
    terminal: resource.kind === "Job" ? terminal(resource) : false,
  };
}

// Только reader image и pause-флаг. Старый owner intent/ABI/run lineage
// не обновляются, существующие supply-chain/maintenance guards не заменяются.
export function buildRecoveryReaderPlan(
  snapshot,
  { phase, readerImage, source, revision, now = Date.now() },
) {
  requireValue(
    phase === "reader" &&
      imagePattern.test(readerImage ?? "") &&
      /^[a-f0-9]{40}$/.test(revision ?? "") &&
      typeof source === "string" &&
      source.startsWith("/"),
    "EXACT_RECOVERY_INPUT_REQUIRED",
  );
  const { controller, policy, parameters, binding, jobs, workspaces } =
    snapshot;
  identity(controller, controllerName, "Deployment");
  requireValue(
    controller.metadata.labels?.["kodex.dev/local-profile"] === "hot-reload" &&
      controller.spec.replicas === 1 &&
      controller.spec.strategy?.type === "Recreate" &&
      controller.spec.paused !== true,
    "EXACT_TRUSTED_CONTROLLER_REQUIRED",
  );
  const apps = controller.spec.template.spec.containers.filter(
    (c) => c.name === controllerName,
  );
  requireValue(
    apps.length === 1 &&
      imagePattern.test(apps[0].image) &&
      fingerprint(apps[0].command) ===
        fingerprint(["/usr/local/bin/image-admission-controller"]) &&
      (!apps[0].args || apps[0].args.length === 0),
    "EXACT_CONTROLLER_COMMAND_REQUIRED",
  );
  const app = apps[0],
    policyName = literal(
      app,
      "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP",
    ).value;
  requireValue(
    literal(app, "KODEX_RPC_PROFILE").value === "trusted-cluster",
    "TRUSTED_RPC_PROFILE_REQUIRED",
  );
  identity(policy, policyName, "ConfigMap");
  identity(parameters, policyName, "ImageAdmissionPolicyParameters");
  identity(
    binding,
    "kodex-image-admission-controller-jobs",
    "ValidatingAdmissionPolicyBinding",
  );
  requireValue(
    policy.immutable === true &&
      policy.metadata.labels?.["kodex.dev/owner-intent"] === "true" &&
      policy.metadata.labels?.["kodex.dev/security-profile"] ===
        "trusted-cluster" &&
      fingerprint(policy.data) === fingerprint(parameters.spec) &&
      binding.spec.paramRef?.name === policyName &&
      binding.spec.paramRef.namespace === namespace &&
      binding.spec.paramRef.parameterNotFoundAction === "Deny" &&
      fingerprint(binding.spec.validationActions) === fingerprint(["Deny"]),
    "EXACT_IMMUTABLE_POLICY_BINDING_REQUIRED",
  );
  requireValue(
    Array.isArray(jobs) &&
      Array.isArray(workspaces) &&
      jobs.length <= 512 &&
      workspaces.length <= 1,
    "BOUNDED_RECOVERY_INVENTORY_REQUIRED",
  );
  const pauses = app.env.filter(
    (e) => e.name === "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS",
  );
  requireValue(
    pauses.length <= 1 &&
      (!pauses.length ||
        (!pauses[0].valueFrom && ["true", "false"].includes(pauses[0].value))),
    "EXACT_PAUSE_CONFIGURATION_REQUIRED",
  );
  if (phase === "reader") {
    requireValue(
      app.image !== readerImage && workspaces.length === 1,
      "STALE_WORKSPACE_AND_NEW_READER_REQUIRED",
    );
    const workspace = workspaces[0],
      id = workspace.metadata.labels?.[idLabel],
      run = workspace.metadata.annotations?.[runKey],
      cursor =
        workspace.metadata.annotations?.["kodex.dev/admission-recovery-uid"];
    identity(workspace, `mc-admit-${id}`, "PersistentVolumeClaim");
    requireValue(
      workspace.metadata.labels?.[label] === "true" &&
        /^[a-f0-9]{32}$/.test(id ?? "") &&
        runPattern.test(run ?? "") &&
        run.endsWith(`-${policy.data.orchestrationRevision}`) &&
        uidPattern.test(cursor ?? ""),
      "EXACT_DURABLE_RECOVERY_CURSOR_REQUIRED",
    );
    const after = Date.parse(
        workspace.metadata.annotations?.["kodex.dev/admission-recovery-after"],
      ),
      created = Date.parse(workspace.metadata.creationTimestamp);
    requireValue(
      Number.isFinite(after) &&
        Number.isFinite(created) &&
        created <= now &&
        now < created + 24 * 60 * 60 * 1000 &&
        after <= created + 24 * 60 * 60 * 1000,
      "BOUNDED_RECOVERY_WINDOW_REQUIRED",
    );
    for (const job of jobs) {
      const phase = job.metadata.labels?.["kodex.dev/image-admission-phase"];
      identity(job, `mc-admit-${id}-${phase}`, "Job");
      requireValue(
        ["claim", "scan", "sign", "admit"].includes(phase) &&
          job.metadata.labels?.[label] === "true" &&
          job.metadata.labels?.[idLabel] === id &&
          job.metadata.annotations?.[runKey] === run &&
          job.spec.template?.spec?.containers?.length === 1 &&
          job.spec.template.spec.containers[0].image ===
            policy.data.admissionImage &&
          fingerprint(
            job.spec.template.spec.containers[0].command.slice(0, 3),
          ) ===
            fingerprint(["/bin/sh", "/opt/kodex/image-admission.sh", phase]) &&
          (phase !== "admit" ||
            job.metadata.uid === cursor ||
            job.metadata.annotations?.[
              "kodex.dev/admission-failed-predecessor-uid"
            ] === cursor),
        "EXACT_RECOVERY_JOB_LINEAGE_REQUIRED",
      );
    }
  }
  const before = structuredClone(controller.spec),
    after = structuredClone(before),
    target = after.template.spec.containers.find(
      (c) => c.name === controllerName,
    );
  target.image = readerImage;
  const pause = target.env.find(
    (e) => e.name === "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS",
  );
  if (pause) pause.value = "true";
  else
    target.env.push({
      name: "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS",
      value: "true",
    });
  return {
    version: 1,
    kind: "TRUSTED_ADMISSION_RECOVERY_READER",
    context: "k3d-kodex",
    phase,
    source,
    revision,
    readerImage,
    createdAt: new Date(now).toISOString(),
    clusterUID: snapshot.clusterUID,
    namespaceUID: snapshot.namespaceUID,
    controller: pin(controller),
    policy: pin(policy),
    parameters: pin(parameters),
    binding: pin(binding),
    work: [...jobs, ...workspaces]
      .map(workPin)
      .sort((a, b) => a.name.localeCompare(b.name)),
    before,
    after,
  };
}

function privateFile(path) {
  const s = lstatSync(path);
  requireValue(
    s.isFile() &&
      !s.isSymbolicLink() &&
      (s.mode & 0o077) === 0 &&
      s.uid === process.getuid() &&
      s.size > 0 &&
      s.size < 4 << 20 &&
      realpathSync(path) === path,
    "PRIVATE_REGULAR_FILE_REQUIRED",
  );
  return readFileSync(path, "utf8");
}
function write(path, value) {
  const fd = openSync(
    path,
    constants.O_WRONLY |
      constants.O_CREAT |
      constants.O_EXCL |
      constants.O_NOFOLLOW,
    0o600,
  );
  try {
    writeSync(fd, JSON.stringify(value) + "\n");
    fsyncSync(fd);
  } finally {
    closeSync(fd);
  }
}
export function sameRecoveryReaderPlan(saved, current, now = Date.now()) {
  requireValue(
    saved.version === 1 &&
      saved.kind === "TRUSTED_ADMISSION_RECOVERY_READER" &&
      Number.isFinite(Date.parse(saved.createdAt)) &&
      now - Date.parse(saved.createdAt) >= 0 &&
      now - Date.parse(saved.createdAt) <= 60_000,
    "FRESH_RECOVERY_PLAN_REQUIRED",
  );
  requireValue(
    fingerprint({ ...saved, createdAt: current.createdAt }) ===
      fingerprint(current),
    "RECOVERY_READER_PLAN_DRIFT",
  );
}
function main(args) {
  const command = args.shift(),
    options = {};
  requireValue(["plan", "apply"].includes(command), "INVALID_COMMAND");
  while (args.length) {
    const key = args.shift();
    requireValue(
      [
        "--context",
        "--phase",
        "--source-root",
        "--expected-sha",
        "--state-directory",
        "--output",
        "--plan",
        "--evidence",
        "--confirm",
      ].includes(key) &&
        !Object.hasOwn(options, key) &&
        args.length,
      "INVALID_ARGUMENT",
    );
    options[key] = args.shift();
  }
  requireValue(
    options["--context"] === "k3d-kodex",
    "EXACT_TRUSTED_CONTEXT_REQUIRED",
  );
  const source = realpathSync(options["--source-root"]),
    revision = options["--expected-sha"],
    state = realpathSync(options["--state-directory"]);
  requireValue(
    source === options["--source-root"] &&
      inspectSource(source).revision === revision &&
      state === options["--state-directory"] &&
      state !== source &&
      !state.startsWith(source + "/") &&
      lstatSync(state).isDirectory() &&
      lstatSync(state).uid === process.getuid() &&
      (lstatSync(state).mode & 0o077) === 0,
    "EXACT_SOURCE_AND_PRIVATE_STATE_REQUIRED",
  );
  const readerImage = privateFile(`${state}/image-admission-image`).trim();
  requireValue(
    imagePattern.test(readerImage),
    "EXACT_PINNED_READER_IMAGE_REQUIRED",
  );
  for (const path of [
    options["--output"],
    options["--plan"],
    options["--evidence"],
  ].filter(Boolean))
    requireValue(
      path === resolve(path) && dirname(path) === state,
      "PRIVATE_STATE_OUTPUT_REQUIRED",
    );
  const cache = mkdtempSync(join(state, ".admission-recovery-kube-cache-")),
    cacheStat = lstatSync(cache);
  const cleanup = () => {
    const current = lstatSync(cache);
    requireValue(
      current.isDirectory() &&
        !current.isSymbolicLink() &&
        current.dev === cacheStat.dev &&
        current.ino === cacheStat.ino &&
        current.uid === process.getuid() &&
        (current.mode & 0o077) === 0 &&
        realpathSync(cache) === cache,
      "EXACT_PRIVATE_CACHE_CLEANUP_REQUIRED",
    );
    rmSync(cache, { recursive: true });
  };
  process.once("exit", cleanup);
  const kube = (args) =>
    execFileSync(
      "kubectl",
      [
        "--context",
        "k3d-kodex",
        `--cache-dir=${cache}`,
        "--request-timeout=15s",
        ...args,
      ],
      { encoding: "utf8", stdio: "pipe", timeout: 20_000, maxBuffer: 4 << 20 },
    );
  requireValue(
    kube(["config", "current-context"]).trim() === "k3d-kodex",
    "CURRENT_TRUSTED_CONTEXT_REQUIRED",
  );
  const server = kube([
    "config",
    "view",
    "--minify",
    "-o",
    "jsonpath={.clusters[0].cluster.server}",
  ]);
  requireValue(
    /^https:\/\/127\.[0-9]+\.[0-9]+\.[0-9]+:[1-9][0-9]*$/.test(server),
    "LOOPBACK_TRUSTED_API_REQUIRED",
  );
  const get = (kind, name, scoped = true) =>
    JSON.parse(
      kube([
        ...(scoped ? ["-n", namespace] : []),
        "get",
        kind,
        name,
        "-o",
        "json",
      ]),
    );
  const list = (kind) => {
    const v = JSON.parse(
      kube(["-n", namespace, "get", kind, "-l", `${label}=true`, "-o", "json"]),
    );
    requireValue(
      !v.metadata?.continue && v.items?.length <= 512,
      "BOUNDED_INVENTORY_REQUIRED",
    );
    return v.items;
  };
  const capture = () => {
    const controller = get("deployment", controllerName),
      app = controller.spec.template.spec.containers.find(
        (c) => c.name === controllerName,
      ),
      name = literal(app, "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP").value;
    const cp = get("deployment", "control-plane");
    requireValue(
      cp.spec.template.metadata.annotations?.["kodex.dev/source-root"] ===
        source &&
        cp.spec.template.metadata.annotations?.["kodex.dev/cache-root"] ===
          `${state}/cache`,
      "EXACT_CONTROL_PLANE_SOURCE_REQUIRED",
    );
    return {
      clusterUID: get("namespace", "kube-system", false).metadata.uid,
      namespaceUID: get("namespace", namespace, false).metadata.uid,
      controller,
      policy: get("configmap", name),
      parameters: get("imageadmissionpolicyparameters", name),
      binding: get(
        "validatingadmissionpolicybinding",
        "kodex-image-admission-controller-jobs",
        false,
      ),
      jobs: list("jobs"),
      workspaces: list("persistentvolumeclaims"),
    };
  };
  const input = { phase: options["--phase"], readerImage, source, revision };
  const current = buildRecoveryReaderPlan(capture(), input);
  if (command === "plan") {
    requireValue(
      options["--output"] && !options["--confirm"],
      "READ_ONLY_PLAN_REQUIRED",
    );
    write(options["--output"], current);
    process.stdout.write("Admission recovery reader plan prepared\n");
    return;
  }
  requireValue(
    options["--confirm"] === "DELIVER-TRUSTED-ADMISSION-RECOVERY-READER" &&
      options["--plan"] &&
      options["--evidence"],
    "EXPLICIT_RECOVERY_DELIVERY_CONFIRMATION_REQUIRED",
  );
  const saved = JSON.parse(privateFile(options["--plan"]));
  sameRecoveryReaderPlan(saved, current);
  const fd = openSync(
    options["--evidence"],
    constants.O_WRONLY |
      constants.O_CREAT |
      constants.O_EXCL |
      constants.O_NOFOLLOW,
    0o600,
  );
  const receipt = (status, code) => {
    writeSync(
      fd,
      JSON.stringify({
        at: new Date().toISOString(),
        planSHA256: fingerprint(saved),
        phase: saved.phase,
        status,
        ...(code ? { code } : {}),
      }) + "\n",
    );
    fsyncSync(fd);
  };
  try {
    receipt("INTENT");
    const patch = [
      { op: "test", path: "/metadata/uid", value: saved.controller.uid },
      {
        op: "test",
        path: "/metadata/resourceVersion",
        value: saved.controller.resourceVersion,
      },
      { op: "test", path: "/spec", value: saved.before },
      { op: "replace", path: "/spec", value: saved.after },
    ];
    kube([
      "-n",
      namespace,
      "patch",
      "deployment",
      controllerName,
      "--type=json",
      "-p",
      JSON.stringify(patch),
    ]);
    const actual = get("deployment", controllerName);
    requireValue(
      actual.metadata.uid === saved.controller.uid &&
        fingerprint(actual.spec) === fingerprint(saved.after),
      "EXACT_READER_DELIVERY_READBACK_REQUIRED",
    );
    const after = capture();
    requireValue(
      fingerprint(pin(after.policy)) === fingerprint(saved.policy) &&
        fingerprint(pin(after.parameters)) === fingerprint(saved.parameters) &&
        fingerprint(pin(after.binding)) === fingerprint(saved.binding),
      "OLD_POLICY_BINDING_CHANGED",
    );
    requireValue(
      inspectSource(source).revision === revision,
      "SOURCE_CHANGED_DURING_DELIVERY",
    );
    receipt("APPLIED");
    process.stdout.write(
      "Admission recovery reader deployment applied; terminal cleanup and fresh claim are not verified\n",
    );
  } catch (error) {
    receipt("UNKNOWN", "RECOVERY_READER_DELIVERY_UNCONFIRMED");
    throw error;
  } finally {
    closeSync(fd);
  }
}
if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
)
  try {
    main(process.argv.slice(2));
  } catch (error) {
    process.stderr.write(
      `Admission recovery reader delivery failed: ${/^[A-Z0-9_]+$/.test(error?.message ?? "") ? error.message : "RECOVERY_READER_DELIVERY_FAILED"}\n`,
    );
    process.exitCode = 1;
  }
