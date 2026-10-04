#!/usr/bin/env node
import { createHash } from "node:crypto";
import { execFileSync, spawn } from "node:child_process";
import { lstatSync, mkdtempSync, chmodSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

const prefix = "session archive worker failed: ";
const entryStages = new Map([
  ["session archive worker configuration is invalid", "CONFIGURATION_INVALID"],
  ["encode session archive worker result", "RESULT_ENCODE_FAILED"],
  ["write session archive worker result", "RESULT_WRITE_FAILED"],
  ["session-archive mode is required", "MODE_INVALID"],
]);
const stages = new Map([
  ["session archive task is missing", "TASK_MISSING"],
  ["read session archive task", "TASK_READ_FAILED"],
  ["decode session archive task", "TASK_DECODE_FAILED"],
  ["session archive task identity is invalid", "TASK_IDENTITY_INVALID"],
  ["session archive task binding is invalid", "TASK_BINDING_INVALID"],
  ["session archive PVC binding is invalid", "TASK_PVC_BINDING_INVALID"],
  ["session snapshot task is invalid", "SNAPSHOT_TASK_INVALID"],
  ["worker task kind is unsupported", "TASK_KIND_UNSUPPORTED"],
  ["session source path is unsafe", "SOURCE_PATH_UNSAFE"],
  ["session source path contains an unsafe component", "SOURCE_PATH_UNSAFE"],
  ["session source file identity is invalid", "SOURCE_IDENTITY_INVALID"],
  ["open session source file", "SOURCE_READ_FAILED"],
  ["session source file digest mismatch", "SOURCE_DIGEST_MISMATCH"],
  ["encode session archive manifest", "ARCHIVE_ENCODE_FAILED"],
  ["write session archive", "ARCHIVE_ENCODE_FAILED"],
  ["session archive exceeds the object budget", "ARCHIVE_BOUND_EXCEEDED"],
  ["put session archive object", "OBJECT_PUT_FAILED"],
  ["read back session archive object", "OBJECT_READBACK_FAILED"],
  ["session archive object readback mismatch", "OBJECT_READBACK_MISMATCH"],
  ["session archive size is invalid", "ARCHIVE_VALIDATION_FAILED"],
  ["session archive manifest is invalid", "ARCHIVE_VALIDATION_FAILED"],
  ["read session archive manifest", "ARCHIVE_VALIDATION_FAILED"],
  ["session archive manifest binding mismatch", "ARCHIVE_VALIDATION_FAILED"],
  ["session archive source entry is invalid", "ARCHIVE_VALIDATION_FAILED"],
  ["session archive source digest mismatch", "ARCHIVE_VALIDATION_FAILED"],
  ["session archive contains unexpected entries", "ARCHIVE_VALIDATION_FAILED"],
  ["object storage is unavailable", "OBJECT_STORAGE_UNAVAILABLE"],
  ["read object storage credential", "CREDENTIAL_UNAVAILABLE"],
  ["object storage credential is invalid", "CREDENTIAL_INVALID"],
]);

// Сопоставление только полного фиксированного producer сообщения, без вывода текста.
export function classifyWorkerLine(line) {
  if (entryStages.has(line)) return entryStages.get(line);
  if (
    typeof line !== "string" ||
    line.length > 4096 ||
    !line.startsWith(prefix)
  )
    return "UNKNOWN";
  const message = line.slice(prefix.length);
  if (
    /^inspect session source component (?:[0-9]|[12][0-9]|3[01])$/.test(message)
  )
    return "SOURCE_COMPONENT_UNAVAILABLE";
  return stages.get(message) ?? "UNKNOWN";
}

// Неизвестный текст не возвращается: только разрешённые shape/producer markers.
export function workerLineShape(line) {
  if (typeof line !== "string" || line.length > 4096) return { bounded: false };
  return {
    bounded: true,
    length: line.length,
    numberLines: line.split("\n").length,
    workerPrefix: line.startsWith(prefix),
    entryStage: entryStages.get(line) ?? "UNKNOWN",
    knownProducerToken: [...entryStages.keys(), ...stages.keys()].some(
      (token) => line.includes(token),
    ),
    carriageReturn: line.includes("\r"),
    jsonObjectPrefix: line.startsWith("{"),
    runtimePermissionDenied: line.includes("permission denied"),
    runtimeExecFormat: line.includes("exec format error"),
    runtimeMissingFile: line.includes("no such file or directory"),
  };
}

const refPattern = /^[A-Za-z0-9_-]{8,128}$/;
const kubeName = /^[a-z0-9][a-z0-9.-]{0,252}$/;
const hash = (value) =>
  createHash("sha256").update(value).digest("hex").slice(0, 16);

export function sessionBinding({
  sessionRef,
  organizationRef,
  projectRef = "",
}) {
  if (
    !refPattern.test(sessionRef) ||
    !refPattern.test(organizationRef) ||
    (projectRef !== "" && !refPattern.test(projectRef))
  )
    throw new Error("session binding is invalid");
  return {
    pvc: `runtime-session-${hash(sessionRef)}`,
    session: hash(sessionRef),
    organization: hash(organizationRef),
    project: hash(projectRef),
  };
}

export function validPVC(pvc, binding) {
  return (
    typeof pvc?.uid === "string" &&
    pvc.uid.length > 0 &&
    pvc.phase === "Bound" &&
    !pvc.deleting &&
    pvc.managed === "true" &&
    pvc.session === binding.session &&
    pvc.organization === binding.organization &&
    pvc.project === binding.project
  );
}

export function validJobPod(pod, job, pvc, binding) {
  return (
    kubeName.test(pod?.name ?? "") &&
    typeof pod?.uid === "string" &&
    pod.uid.length > 0 &&
    pod.managed === "true" &&
    ["Pending", "Running", "Succeeded", "Failed"].includes(pod.phase) &&
    pod.worker === true &&
    pod.pvcs?.length === 1 &&
    pod.pvcs[0] === binding.pvc &&
    pod.owners?.length === 1 &&
    pod.owners[0].kind === "Job" &&
    pod.owners[0].controller === true &&
    kubeName.test(pod.owners[0].name ?? "") &&
    typeof job?.uid === "string" &&
    job.uid.length > 0 &&
    job?.uid === pod.owners[0].uid &&
    job.managed === "true" &&
    job.pvcUID === pvc.uid
  );
}

const pvcTemplate =
  '{"uid":{{printf "%q" .metadata.uid}},"phase":{{printf "%q" .status.phase}},"deleting":{{if .metadata.deletionTimestamp}}true{{else}}false{{end}},"managed":{{printf "%q" (index .metadata.labels "runtime.kodex.dev/managed")}},"session":{{printf "%q" (index .metadata.labels "runtime.kodex.dev/session-hash")}},"organization":{{printf "%q" (index .metadata.annotations "runtime.kodex.dev/organization-hash")}},"project":{{printf "%q" (index .metadata.annotations "runtime.kodex.dev/project-hash")}}}';
const jobTemplate =
  '{"uid":{{printf "%q" .metadata.uid}},"managed":{{printf "%q" (index .metadata.labels "session-archive.kodex.dev/managed")}},"pvcUID":{{printf "%q" (index .metadata.annotations "session-archive.kodex.dev/source-pvc-uid")}}}';
const podTemplate =
  '[{{range $i,$p := .items}}{{if $i}},{{end}}{"name":{{printf "%q" $p.metadata.name}},"uid":{{printf "%q" $p.metadata.uid}},"managed":{{printf "%q" (index $p.metadata.labels "session-archive.kodex.dev/managed")}},"phase":{{printf "%q" $p.status.phase}},"worker":{{range $p.spec.containers}}{{if eq .name "worker"}}true{{end}}{{end}},"owners":[{{range $j,$o := $p.metadata.ownerReferences}}{{if $j}},{{end}}{"kind":{{printf "%q" $o.kind}},"name":{{printf "%q" $o.name}},"uid":{{printf "%q" $o.uid}},"controller":{{$o.controller}}}{{end}}],"pvcs":[{{$sep := ""}}{{range $p.spec.volumes}}{{if .persistentVolumeClaim}}{{$sep}}{{printf "%q" .persistentVolumeClaim.claimName}}{{$sep = ","}}{{end}}{{end}}]}{{end}}]';

export const metadataTemplates = Object.freeze({
  pvc: pvcTemplate,
  job: jobTemplate,
});

export async function watch(options) {
  const binding = sessionBinding(options);
  const timeout = options.timeoutSeconds ?? 120;
  if (
    !Number.isInteger(timeout) ||
    timeout < 1 ||
    timeout > 180 ||
    process.env.KUBECONFIG !== "/home/s/.kube/config"
  )
    throw new Error("watch configuration is invalid");
  const metadata = lstatSync(process.env.KUBECONFIG);
  if (
    !metadata.isFile() ||
    metadata.uid !== process.getuid() ||
    (metadata.mode & 0o777) !== 0o600
  )
    throw new Error("kubeconfig metadata is invalid");
  const cache = mkdtempSync(join(tmpdir(), "kodex-archive-watch-"));
  chmodSync(cache, 0o700);
  const base = [
    "--context=k3d-kodex",
    `--cache-dir=${cache}`,
    "--request-timeout=5s",
    "-n",
    "kodex-runtime",
  ];
  const env = { PATH: process.env.PATH, KUBECONFIG: process.env.KUBECONFIG };
  const children = new Set();
  const seen = new Set();
  const emit = (event, fields = {}) =>
    console.log(JSON.stringify({ event, ...fields }));
  const read = (args, template) => {
    try {
      return JSON.parse(
        execFileSync(
          "kubectl",
          [...base, ...args, "-o", `go-template=${template}`],
          {
            env,
            encoding: "utf8",
            stdio: ["ignore", "pipe", "ignore"],
            timeout: 6000,
            maxBuffer: 1 << 20,
          },
        ),
      );
    } catch {
      return null;
    }
  };
  const deadline = Date.now() + timeout * 1000;
  let observed = false;
  let bindingReported = false;
  try {
    emit("WATCH_STARTED", { sessionRef: options.sessionRef });
    while (Date.now() < deadline) {
      const pvc = read(["get", "pvc", binding.pvc], pvcTemplate);
      if (validPVC(pvc, binding)) {
        if (!bindingReported) {
          emit("PVC_BINDING_VERIFIED", { sessionRef: options.sessionRef });
          bindingReported = true;
        }
        const pods = read(
          ["get", "pods", "-l", "session-archive.kodex.dev/managed=true"],
          podTemplate,
        );
        for (const pod of Array.isArray(pods) ? pods : []) {
          if (
            seen.has(pod.uid) ||
            pod.phase === "Pending" ||
            !pod.pvcs?.includes(binding.pvc) ||
            !kubeName.test(pod.owners?.[0]?.name ?? "")
          )
            continue;
          const job = read(["get", "job", pod.owners[0].name], jobTemplate);
          if (!validJobPod(pod, job, pvc, binding)) continue;
          seen.add(pod.uid);
          observed = true;
          const fields = {
            sessionRef: options.sessionRef,
            podRef: pod.name,
            jobRef: pod.owners[0].name,
          };
          emit("WORKER_OBSERVED", fields);
          const child = spawn(
            "kubectl",
            [
              ...base,
              "--request-timeout=0",
              "logs",
              pod.name,
              "-c",
              "worker",
              "--follow",
              "--tail=20",
            ],
            {
              env,
              stdio: ["ignore", "pipe", "ignore"],
              timeout: Math.max(1, deadline - Date.now()),
              killSignal: "SIGKILL",
            },
          );
          children.add(child);
          let pending = "",
            oversized = false;
          const report = (line) => {
            const stage = classifyWorkerLine(line);
            emit("WORKER_LOG_STAGE", {
              ...fields,
              stage,
              ...(stage === "UNKNOWN" ? { shape: workerLineShape(line) } : {}),
            });
          };
          child.stdout.setEncoding("utf8");
          child.stdout.on("data", (chunk) => {
            for (const fragment of chunk.split(/(?<=\n)/)) {
              if (!oversized && pending.length + fragment.length <= 4096)
                pending += fragment;
              else {
                pending = "";
                oversized = true;
              }
              if (fragment.endsWith("\n")) {
                report(oversized ? "" : pending.slice(0, -1));
                pending = "";
                oversized = false;
              }
            }
          });
          child.on("error", () => emit("WORKER_LOG_UNAVAILABLE", fields));
          child.on("close", (code) => {
            if (pending || oversized) report(oversized ? "" : pending);
            pending = "";
            if (code !== 0) emit("WORKER_LOG_UNAVAILABLE", fields);
            children.delete(child);
          });
        }
      }
      await delay(Math.min(1000, Math.max(1, deadline - Date.now())));
    }
    emit("WATCH_FINISHED", {
      observation: observed ? "WORKER_OBSERVED" : "UNKNOWN_NO_WORKER",
    });
  } finally {
    const joins = [...children].map(
      (child) =>
        new Promise((done) => {
          child.once("close", done);
          child.kill("SIGKILL");
        }),
    );
    await Promise.all(joins);
    rmSync(cache, { recursive: true, force: true });
  }
}

async function main() {
  const args = process.argv.slice(2);
  if (args.length === 1 && args[0] === "--help") {
    console.log(
      "Read-only archive watcher: --session-ref REF --organization-ref REF [--project-ref REF] [--timeout-seconds 1..180]. KUBECONFIG must be /home/s/.kube/config. Uses metadata-only kubectl reads and worker log follow; never retries or changes cluster state. Private temporary cache is removed after child processes join. Raw logs, inputs and credentials are never printed. No worker observed means UNKNOWN, not success.",
    );
    return;
  }
  const options = {};
  const keys = new Map([
    ["--session-ref", "sessionRef"],
    ["--organization-ref", "organizationRef"],
    ["--project-ref", "projectRef"],
    ["--timeout-seconds", "timeoutSeconds"],
  ]);
  for (let i = 0; i < args.length; i += 2) {
    const key = keys.get(args[i]);
    if (!key || Object.hasOwn(options, key) || args[i + 1] === undefined)
      throw new Error("watch arguments are invalid");
    options[key] = key === "timeoutSeconds" ? Number(args[i + 1]) : args[i + 1];
  }
  await watch(options);
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  main().catch(() => {
    console.error('{"event":"WATCH_FAILED","stage":"UNKNOWN"}');
    process.exitCode = 1;
  });
}
