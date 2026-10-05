#!/usr/bin/env node
import { execFileSync, spawn } from "node:child_process";
import { chmodSync, lstatSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

const uid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const run = /^v[0-9]{14}-[a-f0-9]{40}$/;
const jobName = /^mc-admit-([a-f0-9]{32})-(scan|sign)$/;
const identifier = (value) =>
  typeof value === "string" && !/[\r\n]/.test(value);
const reasons = new Map([
  ["Completed", "COMPLETED"],
  ["Error", "ERROR"],
  ["OOMKilled", "OOM_KILLED"],
  ["ContainerCannotRun", "CONTAINER_CANNOT_RUN"],
  ["StartError", "START_ERROR"],
  ["DeadlineExceeded", "DEADLINE_EXCEEDED"],
]);
const timestamp = (value) =>
  typeof value === "string" &&
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value) &&
  Number.isFinite(Date.parse(value));

export function phaseBinding(options) {
  if (
    !identifier(options.jobRef) ||
    !jobName.test(options.jobRef) ||
    !identifier(options.admissionRunId) ||
    !run.test(options.admissionRunId)
  )
    throw new Error("phase binding is invalid");
  const [, id, phase] = options.jobRef.match(jobName);
  return {
    jobRef: options.jobRef,
    admissionRunId: options.admissionRunId,
    id,
    phase,
    serviceAccount:
      phase === "scan" ? "kodex-image-scanner" : "kodex-image-signer",
  };
}

const commandOK = (containers, phase) =>
  Array.isArray(containers) &&
  containers.length === 1 &&
  containers[0].name === phase &&
  JSON.stringify(containers[0].command) ===
    JSON.stringify(["/bin/sh", "/opt/kodex/image-admission.sh", phase]);
export function validPhaseJob(job, binding) {
  return (
    job?.name === binding.jobRef &&
    job.namespace === "kodex-system" &&
    identifier(job.uid) &&
    uid.test(job.uid) &&
    job.managed === "true" &&
    job.runId === binding.admissionRunId &&
    job.id === binding.id &&
    job.phase === binding.phase &&
    job.serviceAccount === binding.serviceAccount &&
    job.automount === false &&
    job.restartPolicy === "Never" &&
    commandOK(job.containers, binding.phase)
  );
}
export function validPhasePod(pod, job, binding) {
  return (
    validPhaseJob(job, binding) &&
    pod?.namespace === "kodex-system" &&
    identifier(pod.name) &&
    /^[a-z0-9][a-z0-9.-]{0,252}$/.test(pod.name) &&
    identifier(pod.uid) &&
    uid.test(pod.uid) &&
    pod.id === binding.id &&
    pod.phase === binding.phase &&
    commandOK(pod.containers, binding.phase) &&
    pod.owners?.length === 1 &&
    pod.owners[0].kind === "Job" &&
    pod.owners[0].controller === true &&
    pod.owners[0].name === job.name &&
    pod.owners[0].uid === job.uid
  );
}

export function terminationProjection(pod, job, binding, expectedPodUID) {
  if (!validPhasePod(pod, job, binding) || pod.uid !== expectedPodUID)
    return null;
  const terminal = pod.termination;
  if (
    !terminal ||
    !Number.isInteger(terminal.exitCode) ||
    terminal.exitCode < 0 ||
    terminal.exitCode > 255 ||
    !Number.isInteger(terminal.signal) ||
    terminal.signal < 0 ||
    terminal.signal > 64 ||
    !timestamp(terminal.startedAt) ||
    !timestamp(terminal.finishedAt) ||
    Date.parse(terminal.finishedAt) < Date.parse(terminal.startedAt)
  )
    return null;
  return {
    event: "PHASE_TERMINATION_OBSERVED",
    admissionRunId: binding.admissionRunId,
    phase: binding.phase,
    jobRef: job.name,
    jobUID: job.uid,
    podRef: pod.name,
    podUID: pod.uid,
    exitCode: terminal.exitCode,
    signal: terminal.signal,
    reason: reasons.get(terminal.reason) ?? "UNKNOWN",
    startedAt: terminal.startedAt,
    finishedAt: terminal.finishedAt,
  };
}

const fields =
  '"name":{{printf "%q" .metadata.name}},"namespace":{{printf "%q" .metadata.namespace}},"uid":{{printf "%q" .metadata.uid}},"id":{{printf "%q" (index .metadata.labels "kodex.dev/image-admission-id")}},"phase":{{printf "%q" (index .metadata.labels "kodex.dev/image-admission-phase")}}';
const containers =
  '"containers":[{{range $i,$c := .spec.containers}}{{if $i}},{{end}}{"name":{{printf "%q" $c.name}},"command":[{{range $j,$a := $c.command}}{{if $j}},{{end}}{{printf "%q" $a}}{{end}}]}{{end}}]';
export const jobTemplate =
  "{" +
  fields +
  ',"managed":{{printf "%q" (index .metadata.labels "kodex.dev/image-admission-orchestrated")}},"runId":{{printf "%q" (index .metadata.annotations "kodex.dev/admission-run-id")}},"serviceAccount":{{printf "%q" .spec.template.spec.serviceAccountName}},"automount":{{.spec.template.spec.automountServiceAccountToken}},"restartPolicy":{{printf "%q" .spec.template.spec.restartPolicy}},{{with .spec.template}}' +
  containers +
  "{{end}}}";
export const podTemplate =
  "{" +
  fields +
  "," +
  containers +
  ',"owners":[{{range $i,$o := .metadata.ownerReferences}}{{if $i}},{{end}}{"kind":{{printf "%q" $o.kind}},"name":{{printf "%q" $o.name}},"uid":{{printf "%q" $o.uid}},"controller":{{$o.controller}}}{{end}}],"termination":{{range .status.containerStatuses}}{{if or (eq .name "scan") (eq .name "sign")}}{{if .state.terminated}}{"reason":{{printf "%q" .state.terminated.reason}},"exitCode":{{.state.terminated.exitCode}},"signal":{{if .state.terminated.signal}}{{.state.terminated.signal}}{{else}}0{{end}},"startedAt":{{printf "%q" .state.terminated.startedAt}},"finishedAt":{{printf "%q" .state.terminated.finishedAt}}}{{else}}null{{end}}{{end}}{{end}}}';
export const podsTemplate =
  "[{{range $i,$p := .items}}{{if $i}},{{end}}{{with $p}}" +
  podTemplate +
  "{{end}}{{end}}]";

export async function watchPhase(options, runtime = {}) {
  const binding = phaseBinding(options);
  const timeout = options.timeoutSeconds ?? 600;
  if (
    !Number.isInteger(timeout) ||
    timeout < 1 ||
    timeout > 900 ||
    process.env.KUBECONFIG !== "/home/s/.kube/config"
  )
    throw new Error("phase watch configuration is invalid");
  const config = runtime.configMetadata ?? lstatSync(process.env.KUBECONFIG);
  if (
    !config.isFile() ||
    config.isSymbolicLink() ||
    config.uid !== process.getuid() ||
    (config.mode & 0o777) !== 0o600
  )
    throw new Error("kubeconfig metadata is invalid");
  const cache = mkdtempSync(join(tmpdir(), "kodex-admission-phase-"));
  chmodSync(cache, 0o700);
  const base = [
    "--context=k3d-kodex",
    `--cache-dir=${cache}`,
    "--request-timeout=5s",
    "-n",
    "kodex-system",
  ];
  const env = { PATH: process.env.PATH, KUBECONFIG: process.env.KUBECONFIG };
  const deadline = Date.now() + timeout * 1000;
  const emit = runtime.emit ?? ((value) => console.log(JSON.stringify(value)));
  const children = new Map(),
    seen = new Set();
  let observation;
  const read = (args, template) => {
    if (Date.now() >= deadline || runtime.signal?.aborted) return null;
    if (runtime.read) return runtime.read(args, template);
    try {
      return JSON.parse(
        execFileSync(
          "kubectl",
          [...base, ...args, "-o", `go-template=${template}`],
          {
            env,
            encoding: "utf8",
            stdio: ["ignore", "pipe", "ignore"],
            timeout: Math.min(6000, Math.max(1, deadline - Date.now())),
            killSignal: "SIGKILL",
            maxBuffer: 1 << 20,
          },
        ),
      );
    } catch {
      return null;
    }
  };
  try {
    emit({ event: "PHASE_WATCH_STARTED", ...binding });
    while (Date.now() < deadline && !runtime.signal?.aborted && !observation) {
      const job = read(["get", "job", binding.jobRef], jobTemplate);
      if (validPhaseJob(job, binding)) {
        const pods = read(
          [
            "get",
            "pods",
            "-l",
            `kodex.dev/image-admission-id=${binding.id},kodex.dev/image-admission-phase=${binding.phase}`,
          ],
          podsTemplate,
        );
        if (Array.isArray(pods) && pods.length > 8)
          throw new Error("phase discovery exceeds bound");
        for (const pod of Array.isArray(pods) ? pods : []) {
          if (!validPhasePod(pod, job, binding)) continue;
          observation = terminationProjection(pod, job, binding, pod.uid);
          if (observation) break;
          if (seen.has(pod.uid)) continue;
          const freshJob = read(["get", "job", binding.jobRef], jobTemplate);
          const freshPod = read(["get", "pod", pod.name], podTemplate);
          if (
            freshJob?.uid !== job.uid ||
            !validPhasePod(freshPod, freshJob, binding) ||
            freshPod.uid !== pod.uid
          )
            continue;
          seen.add(pod.uid);
          const child = (runtime.spawn ?? spawn)(
            "kubectl",
            [
              ...base,
              "--request-timeout=0",
              "get",
              "pod",
              pod.name,
              "--watch",
              "-o",
              `go-template=${podTemplate}{{"\\n"}}`,
            ],
            {
              env,
              stdio: ["ignore", "pipe", "ignore"],
              timeout: Math.max(1, deadline - Date.now()),
              killSignal: "SIGKILL",
            },
          );
          let done;
          const joined = new Promise((resolveJoin) => {
            done = resolveJoin;
          });
          children.set(child, joined);
          let pending = "",
            oversized = false;
          const capture = (line) => {
            if (observation || runtime.signal?.aborted) return;
            try {
              observation = terminationProjection(
                JSON.parse(line),
                freshJob,
                binding,
                pod.uid,
              );
            } catch {}
          };
          child.stdout.setEncoding("utf8");
          child.stdout.on("data", (chunk) => {
            for (const fragment of chunk.split(/(?<=\n)/)) {
              if (!oversized && pending.length + fragment.length <= 16384)
                pending += fragment;
              else {
                pending = "";
                oversized = true;
              }
              if (fragment.endsWith("\n")) {
                if (!oversized) capture(pending.slice(0, -1));
                pending = "";
                oversized = false;
              }
            }
          });
          child.on("error", () => {});
          child.once("close", () => {
            if (pending && !oversized) capture(pending);
            done();
            children.delete(child);
            if (!observation) seen.delete(pod.uid);
          });
        }
      }
      if (!observation && !runtime.signal?.aborted)
        await delay(Math.min(200, Math.max(1, deadline - Date.now())));
    }
    emit(
      observation ?? {
        event: "PHASE_WATCH_FINISHED",
        admissionRunId: binding.admissionRunId,
        phase: binding.phase,
        observation: runtime.signal?.aborted
          ? "UNKNOWN_CANCELLED"
          : "UNKNOWN_NO_TERMINATION",
      },
    );
    return observation ? "OBSERVED" : "UNKNOWN";
  } finally {
    await Promise.all(
      [...children].map(([child, joined]) => {
        child.kill("SIGKILL");
        return joined;
      }),
    );
    rmSync(cache, { recursive: true, force: true });
  }
}

async function main() {
  const args = process.argv.slice(2);
  if (args.length === 1 && args[0] === "--help") {
    console.log(
      "Read-only phase termination watch: --job-ref mc-admit-ID-scan|sign --admission-run-id RUN [--timeout-seconds 1..900]. Start when the exact managed phase Job appears. KUBECONFIG=/home/s/.kube/config, context k3d-kodex, namespace kodex-system. Only exact UID-bound Pod metadata/termination is watched; no logs/message/env/Secret/DB/auth/mutation. Correlate admissionRunId with owner-confirmed diagnostic; observation is not approval or PASS. Deleted-before-watch means UNKNOWN.",
    );
    return;
  }
  const keys = new Map([
    ["--job-ref", "jobRef"],
    ["--admission-run-id", "admissionRunId"],
    ["--timeout-seconds", "timeoutSeconds"],
  ]);
  const options = {};
  for (let i = 0; i < args.length; i += 2) {
    const key = keys.get(args[i]);
    if (
      !key ||
      Object.hasOwn(options, key) ||
      !args[i + 1] ||
      args[i + 1].startsWith("--")
    )
      throw new Error("phase watch arguments are invalid");
    options[key] = key === "timeoutSeconds" ? Number(args[i + 1]) : args[i + 1];
  }
  const shutdown = new AbortController(),
    stop = () => shutdown.abort();
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
  try {
    if ((await watchPhase(options, { signal: shutdown.signal })) !== "OBSERVED")
      process.exitCode = 2;
    if (shutdown.signal.aborted) process.exitCode = 130;
  } finally {
    process.removeListener("SIGINT", stop);
    process.removeListener("SIGTERM", stop);
  }
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
)
  main().catch(() => {
    console.log('{"event":"PHASE_WATCH_FAILED","code":"METADATA_UNAVAILABLE"}');
    process.exitCode = 1;
  });
