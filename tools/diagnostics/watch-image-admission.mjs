#!/usr/bin/env node
import { execFileSync, spawn } from "node:child_process";
import {
  chmodSync,
  closeSync,
  constants,
  fsyncSync,
  lstatSync,
  mkdtempSync,
  openSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

const runPattern = /^v[0-9]{14}-[a-f0-9]{40}$/;
const artifactPattern = /^imgart_[A-Za-z0-9_-]{8,88}$/;
const digestPattern = /^sha256:[a-f0-9]{64}$/;
const uidPattern =
  /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const jobPattern = /^mc-admit-([a-f0-9]{32})-admit$/;
const countKeys = [
  "highOrCriticalMatchCount",
  "blockingMatchCount",
  "unresolvedNoFixMatchCount",
];
const diagnosticKeys = [
  "event",
  "version",
  "admissionRunId",
  "artifactRef",
  "imageDigest",
  "vulnerabilityEvidenceSha256",
  "verdict",
  "recipeRef",
  "recipeGeneration",
  "buildRef",
  "reason",
  "failureCode",
  ...countKeys,
  "remediation",
];
const exactKeys = (value, keys) =>
  value &&
  typeof value === "object" &&
  !Array.isArray(value) &&
  Object.keys(value).length === keys.length &&
  keys.every((key) => Object.hasOwn(value, key));
const count = (value) =>
  Number.isInteger(value) && value >= 0 && value <= 1000000;
const identifier = (value) =>
  typeof value === "string" && !/[\r\n]/.test(value);
const recipePattern = /^imgrec_[A-Za-z0-9_-]{8,88}$/;
const buildPattern = /^imgbld_[A-Za-z0-9_-]{8,88}$/;
const packageName = (value) =>
  identifier(value) &&
  /^[A-Za-z0-9@][A-Za-z0-9.+_:@/~-]{0,159}$/.test(value) &&
  !value.includes("://");
const version = (value) =>
  identifier(value) && /^[A-Za-z0-9][A-Za-z0-9.+:~_-]{0,159}$/.test(value);

export function admissionBinding(options) {
  if (
    ![
      options.admissionRunId,
      options.artifactRef,
      options.imageDigest,
      options.jobRef,
    ].every(identifier) ||
    !runPattern.test(options.admissionRunId ?? "") ||
    !artifactPattern.test(options.artifactRef ?? "") ||
    !digestPattern.test(options.imageDigest ?? "") ||
    !jobPattern.test(options.jobRef ?? "")
  )
    throw new Error("watch binding is invalid");
  return {
    kind: "artifact",
    admissionRunId: options.admissionRunId,
    artifactRef: options.artifactRef,
    imageDigest: options.imageDigest,
    jobRef: options.jobRef,
    id: options.jobRef.match(jobPattern)[1],
  };
}

export function recipeBinding(options) {
  if (
    !identifier(options.recipeRef) ||
    !recipePattern.test(options.recipeRef) ||
    !Number.isSafeInteger(options.recipeGeneration) ||
    options.recipeGeneration < 1 ||
    (options.buildRef !== undefined &&
      (!identifier(options.buildRef) || !buildPattern.test(options.buildRef)))
  )
    throw new Error("recipe watch binding is invalid");
  return {
    kind: "recipe",
    recipeRef: options.recipeRef,
    recipeGeneration: options.recipeGeneration,
    ...(options.buildRef === undefined ? {} : { buildRef: options.buildRef }),
  };
}

export function parseDiagnostic(line, binding) {
  if (typeof line !== "string" || line.length > 32768) return null;
  let value;
  try {
    value = JSON.parse(line);
  } catch {
    return null;
  }
  if (
    !exactKeys(value, diagnosticKeys) ||
    value.event !== "IMAGE_ADMISSION_DIAGNOSTIC" ||
    value.version !== 1 ||
    !identifier(value.admissionRunId) ||
    !runPattern.test(value.admissionRunId) ||
    !identifier(value.artifactRef) ||
    !artifactPattern.test(value.artifactRef) ||
    !identifier(value.imageDigest) ||
    !digestPattern.test(value.imageDigest) ||
    !identifier(value.vulnerabilityEvidenceSha256) ||
    !/^[a-f0-9]{64}$/.test(value.vulnerabilityEvidenceSha256) ||
    !identifier(value.recipeRef) ||
    !recipePattern.test(value.recipeRef) ||
    !identifier(value.buildRef) ||
    !buildPattern.test(value.buildRef) ||
    !Number.isSafeInteger(value.recipeGeneration) ||
    value.recipeGeneration < 1 ||
    (binding.kind === "recipe"
      ? value.recipeRef !== binding.recipeRef ||
        value.recipeGeneration !== binding.recipeGeneration ||
        (binding.buildRef !== undefined && value.buildRef !== binding.buildRef)
      : value.admissionRunId !== binding.admissionRunId ||
        value.artifactRef !== binding.artifactRef ||
        value.imageDigest !== binding.imageDigest) ||
    !["ACCEPTED", "REJECTED"].includes(value.verdict) ||
    !Array.isArray(value.remediation) ||
    value.remediation.length > 20
  )
    return null;
  const technical = [
    "SCAN_TECHNICAL_REJECTION",
    "SIGN_TECHNICAL_REJECTION",
  ].includes(value.reason);
  if (technical) {
    if (
      ![
        "VULNERABILITY_SCAN_FAILED",
        "SBOM_GENERATION_FAILED",
        "VULNERABILITY_POLICY_FAILED",
        "TECHNICAL_DETAIL_UNKNOWN",
      ].includes(value.failureCode) ||
      value.verdict !== "REJECTED" ||
      countKeys.some((key) => value[key] !== null) ||
      value.remediation.length
    )
      return null;
  } else {
    if (
      value.failureCode !== null ||
      countKeys.some((key) => !count(value[key])) ||
      value.highOrCriticalMatchCount !==
        value.blockingMatchCount + value.unresolvedNoFixMatchCount ||
      !(
        (value.reason === "ACCEPTED" &&
          value.verdict === "ACCEPTED" &&
          value.blockingMatchCount === 0 &&
          !value.remediation.length) ||
        (value.reason === "VULNERABILITY" &&
          value.verdict === "REJECTED" &&
          value.blockingMatchCount > 0)
      )
    )
      return null;
  }
  const seen = new Set();
  for (const item of value.remediation) {
    if (
      !exactKeys(item, ["cve", "package", "version", "fixes"]) ||
      !identifier(item.cve) ||
      !/^CVE-[0-9]{4}-[0-9]{4,12}$/.test(item.cve) ||
      !packageName(item.package) ||
      !version(item.version) ||
      !Array.isArray(item.fixes) ||
      item.fixes.length < 1 ||
      item.fixes.length > 4 ||
      item.fixes.some((fix) => !version(fix)) ||
      new Set(item.fixes).size !== item.fixes.length
    )
      return null;
    const key = JSON.stringify([item.cve, item.package, item.version]);
    if (seen.has(key)) return null;
    seen.add(key);
  }
  return value;
}

const validCommand = (command) =>
  Array.isArray(command) &&
  (command.length === 3 ||
    (command.length === 4 &&
      ["SCAN_PREDECESSOR_FAILED", "SIGN_PREDECESSOR_FAILED"].includes(
        command[3],
      ))) &&
  command[0] === "/bin/sh" &&
  command[1] === "/opt/kodex/image-admission.sh" &&
  command[2] === "admit";
export function validJobPod(job, pod, binding) {
  return (
    job?.name === binding.jobRef &&
    job.namespace === "kodex-system" &&
    identifier(job.uid) &&
    uidPattern.test(job.uid ?? "") &&
    job.managed === "true" &&
    job.id === binding.id &&
    job.phase === "admit" &&
    job.runId === binding.admissionRunId &&
    job.serviceAccount === "image-admission" &&
    job.automount === false &&
    job.restartPolicy === "Never" &&
    job.containers?.length === 1 &&
    job.containers[0].name === "admit" &&
    validCommand(job.containers[0].command) &&
    pod?.namespace === "kodex-system" &&
    identifier(pod.name) &&
    /^[a-z0-9][a-z0-9.-]{0,252}$/.test(pod.name ?? "") &&
    identifier(pod.uid) &&
    uidPattern.test(pod.uid ?? "") &&
    pod.id === binding.id &&
    pod.phase === "admit" &&
    pod.containers?.length === 1 &&
    pod.containers[0].name === "admit" &&
    validCommand(pod.containers[0].command) &&
    pod.owners?.length === 1 &&
    pod.owners[0].kind === "Job" &&
    pod.owners[0].controller === true &&
    pod.owners[0].name === job.name &&
    pod.owners[0].uid === job.uid
  );
}

export function publicDiagnostic(diagnostic) {
  return Object.fromEntries(
    diagnosticKeys
      .filter((key) => key !== "remediation")
      .map((key) => [key, diagnostic[key]]),
  );
}

function savePrivateDiagnostic(path, diagnostic) {
  const target = resolve(path);
  const parent = dirname(target);
  const metadata = lstatSync(parent);
  if (
    !metadata.isDirectory() ||
    metadata.isSymbolicLink() ||
    metadata.uid !== process.getuid() ||
    (metadata.mode & 0o777) !== 0o700
  )
    throw new Error("private output directory is invalid");
  // Не допускаем скрытый переход через symlink в родительских каталогах.
  let component = parent;
  while (component !== dirname(component)) {
    if (lstatSync(component).isSymbolicLink())
      throw new Error("private output path is invalid");
    component = dirname(component);
  }
  const fd = openSync(
    target,
    constants.O_CREAT |
      constants.O_EXCL |
      constants.O_WRONLY |
      constants.O_NOFOLLOW,
    0o600,
  );
  try {
    writeFileSync(
      fd,
      JSON.stringify({
        ...publicDiagnostic(diagnostic),
        remediation: diagnostic.remediation,
        ...(diagnostic.confirmation === "PENDING_OWNER_CONFIRMATION"
          ? { confirmation: "PENDING_OWNER_CONFIRMATION" }
          : {}),
      }) + "\n",
    );
    fsyncSync(fd);
  } finally {
    closeSync(fd);
  }
}

export function saveRemediation(path, diagnostic) {
  if (diagnostic.reason !== "VULNERABILITY")
    throw new Error("remediation is unavailable");
  savePrivateDiagnostic(path, diagnostic);
}

export function savePendingDiagnostic(path, diagnostic, binding) {
  if (
    binding.kind !== "recipe" ||
    !parseDiagnostic(JSON.stringify(diagnostic), binding)
  )
    throw new Error("pending diagnostic is invalid");
  savePrivateDiagnostic(path, {
    ...diagnostic,
    confirmation: "PENDING_OWNER_CONFIRMATION",
  });
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
  "[{{range $i,$p := .items}}{{if $i}},{{end}}{{with $p}}{" +
  fields +
  "," +
  containers +
  ',"owners":[{{range $j,$o := .metadata.ownerReferences}}{{if $j}},{{end}}{"kind":{{printf "%q" $o.kind}},"name":{{printf "%q" $o.name}},"uid":{{printf "%q" $o.uid}},"controller":{{$o.controller}}}{{end}}]}{{end}}{{end}}]';

const jobListTemplate =
  "[{{range $i,$j := .items}}{{if $i}},{{end}}{{with $j}}" +
  jobTemplate +
  "{{end}}{{end}}]";

// До owner GET это лишь приватный кандидат; verdict и remediation не выдаются наружу.
export async function watchRecipe(options, runtime = {}) {
  const binding = recipeBinding(options);
  const timeout = options.timeoutSeconds ?? 1200;
  if (
    !Number.isInteger(timeout) ||
    timeout < 1 ||
    timeout > 1800 ||
    typeof options.pendingOutput !== "string" ||
    !options.pendingOutput ||
    process.env.KUBECONFIG !== "/home/s/.kube/config"
  )
    throw new Error("recipe watch configuration is invalid");
  const config = runtime.configMetadata ?? lstatSync(process.env.KUBECONFIG);
  if (
    !config.isFile() ||
    config.isSymbolicLink() ||
    config.uid !== process.getuid() ||
    (config.mode & 0o777) !== 0o600
  )
    throw new Error("kubeconfig metadata is invalid");
  const cache = mkdtempSync(join(tmpdir(), "kodex-admission-watch-"));
  chmodSync(cache, 0o700);
  const env = { PATH: process.env.PATH, KUBECONFIG: process.env.KUBECONFIG };
  const base = [
    "--context=k3d-kodex",
    `--cache-dir=${cache}`,
    "--request-timeout=5s",
    "-n",
    "kodex-system",
  ];
  const deadline = Date.now() + timeout * 1000;
  const children = new Map();
  const seen = new Set();
  let observed = false;
  let failed = false;
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
  const capture = (line, sourceBinding) => {
    if (observed || failed || runtime.signal?.aborted) return;
    const diagnostic = parseDiagnostic(line, binding);
    if (
      !diagnostic ||
      diagnostic.admissionRunId !== sourceBinding.admissionRunId
    )
      return;
    try {
      savePendingDiagnostic(options.pendingOutput, diagnostic, binding);
      observed = true;
    } catch {
      failed = true;
    }
  };
  try {
    (runtime.emit ?? console.log)(
      JSON.stringify({
        event: "WATCH_STARTED",
        recipeRef: binding.recipeRef,
        recipeGeneration: binding.recipeGeneration,
      }),
    );
    while (
      Date.now() < deadline &&
      !observed &&
      !failed &&
      !runtime.signal?.aborted
    ) {
      const jobs = read(
        [
          "get",
          "jobs",
          "-l",
          "kodex.dev/image-admission-orchestrated=true,kodex.dev/image-admission-phase=admit",
        ],
        jobListTemplate,
      );
      if (Array.isArray(jobs) && jobs.length > 8)
        throw new Error("admission discovery exceeds bound");
      for (const job of Array.isArray(jobs) ? jobs : []) {
        if (
          !identifier(job.runId) ||
          !runPattern.test(job.runId) ||
          !jobPattern.test(job.name ?? "")
        )
          continue;
        const sourceBinding = {
          jobRef: job.name,
          id: job.name.match(jobPattern)[1],
          admissionRunId: job.runId,
        };
        const pods = read(
          [
            "get",
            "pods",
            "-l",
            `kodex.dev/image-admission-id=${sourceBinding.id},kodex.dev/image-admission-phase=admit`,
          ],
          podTemplate,
        );
        if (Array.isArray(pods) && pods.length > 8)
          throw new Error("admission discovery exceeds bound");
        for (const pod of Array.isArray(pods) ? pods : []) {
          if (seen.has(pod.uid) || !validJobPod(job, pod, sourceBinding))
            continue;
          const freshJob = read(["get", "job", job.name], jobTemplate);
          const freshPods = read(
            [
              "get",
              "pods",
              "-l",
              `kodex.dev/image-admission-id=${sourceBinding.id},kodex.dev/image-admission-phase=admit`,
            ],
            podTemplate,
          );
          const freshPod = Array.isArray(freshPods)
            ? freshPods.find((candidate) => candidate.uid === pod.uid)
            : null;
          if (
            freshJob?.uid !== job.uid ||
            !validJobPod(freshJob, freshPod, sourceBinding)
          )
            continue;
          seen.add(pod.uid);
          const child = (runtime.spawn ?? spawn)(
            "kubectl",
            [
              ...base,
              "--request-timeout=0",
              "logs",
              pod.name,
              "-c",
              "admit",
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
          let closed;
          const joined = new Promise((done) => {
            closed = done;
          });
          children.set(child, joined);
          let pending = "",
            oversized = false;
          child.stdout.setEncoding("utf8");
          child.stdout.on("data", (chunk) => {
            for (const fragment of chunk.split(/(?<=\n)/)) {
              if (!oversized && pending.length + fragment.length <= 32768)
                pending += fragment;
              else {
                pending = "";
                oversized = true;
              }
              if (fragment.endsWith("\n")) {
                if (!oversized) capture(pending.slice(0, -1), sourceBinding);
                pending = "";
                oversized = false;
              }
            }
          });
          child.on("error", () => {});
          child.once("close", () => {
            if (pending && !oversized) capture(pending, sourceBinding);
            closed();
            children.delete(child);
            if (!observed && !failed) seen.delete(pod.uid);
          });
        }
      }
      if (!observed && !failed)
        await delay(Math.min(200, Math.max(1, deadline - Date.now())));
    }
    if (failed) throw new Error("pending diagnostic output is unavailable");
    (runtime.emit ?? console.log)(
      JSON.stringify({
        event: observed
          ? "OBSERVED_PENDING_OWNER_CONFIRMATION"
          : "WATCH_FINISHED",
        recipeRef: binding.recipeRef,
        recipeGeneration: binding.recipeGeneration,
        ...(observed
          ? { pendingSaved: true }
          : {
              observation: runtime.signal?.aborted
                ? "UNKNOWN_CANCELLED"
                : "UNKNOWN_NO_DIAGNOSTIC",
            }),
      }),
    );
    return observed ? "PENDING_OWNER_CONFIRMATION" : "UNKNOWN";
  } finally {
    const joins = [...children.entries()].map(([child, joined]) => {
      child.kill("SIGKILL");
      return joined;
    });
    await Promise.all(joins);
    rmSync(cache, { recursive: true, force: true });
  }
}

export async function watch(options, signal) {
  const binding = admissionBinding(options);
  const timeout = options.timeoutSeconds ?? 120;
  if (
    !Number.isInteger(timeout) ||
    timeout < 1 ||
    timeout > 180 ||
    process.env.KUBECONFIG !== "/home/s/.kube/config"
  )
    throw new Error("watch configuration is invalid");
  const config = lstatSync(process.env.KUBECONFIG);
  if (
    !config.isFile() ||
    config.isSymbolicLink() ||
    config.uid !== process.getuid() ||
    (config.mode & 0o777) !== 0o600
  )
    throw new Error("kubeconfig metadata is invalid");
  const cache = mkdtempSync(join(tmpdir(), "kodex-admission-watch-"));
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
  const read = (args, template) => {
    const remaining = deadline - Date.now();
    if (remaining <= 0 || signal?.aborted) return null;
    try {
      return execFileSync(
        "kubectl",
        [
          ...base,
          ...args,
          ...(template ? ["-o", `go-template=${template}`] : []),
        ],
        {
          env,
          encoding: "utf8",
          stdio: ["ignore", "pipe", "ignore"],
          timeout: Math.min(6000, remaining),
          killSignal: "SIGKILL",
          maxBuffer: template ? 1 << 20 : 32768,
        },
      );
    } catch {
      return null;
    }
  };
  const metadata = (args, template) => {
    try {
      return JSON.parse(read(args, template));
    } catch {
      return null;
    }
  };
  try {
    console.log(
      JSON.stringify({
        event: "WATCH_STARTED",
        artifactRef: binding.artifactRef,
      }),
    );
    while (Date.now() < deadline && !signal?.aborted) {
      const job = metadata(["get", "job", binding.jobRef], jobTemplate);
      const pods = metadata(
        [
          "get",
          "pods",
          "-l",
          `kodex.dev/image-admission-id=${binding.id},kodex.dev/image-admission-phase=admit`,
        ],
        podTemplate,
      );
      for (const pod of Array.isArray(pods) ? pods : []) {
        if (!validJobPod(job, pod, binding)) continue;
        const logs = read(["logs", pod.name, "-c", "admit", "--tail=20"]);
        if (!logs) continue;
        const freshJob = metadata(["get", "job", binding.jobRef], jobTemplate);
        const freshPods = metadata(
          [
            "get",
            "pods",
            "-l",
            `kodex.dev/image-admission-id=${binding.id},kodex.dev/image-admission-phase=admit`,
          ],
          podTemplate,
        );
        const freshPod = Array.isArray(freshPods)
          ? freshPods.find((candidate) => candidate.uid === pod.uid)
          : null;
        if (
          freshJob?.uid !== job.uid ||
          !validJobPod(freshJob, freshPod, binding)
        )
          continue;
        for (const line of logs.split("\n")) {
          const diagnostic = parseDiagnostic(line, binding);
          if (!diagnostic) continue;
          let remediationSaved = false;
          if (options.output && diagnostic.reason === "VULNERABILITY") {
            saveRemediation(options.output, diagnostic);
            remediationSaved = true;
          }
          console.log(
            JSON.stringify({
              ...publicDiagnostic(diagnostic),
              remediationSaved,
            }),
          );
          return "OBSERVED";
        }
      }
      await delay(Math.min(1000, Math.max(1, deadline - Date.now())));
    }
    console.log(
      JSON.stringify({
        event: "WATCH_FINISHED",
        observation: signal?.aborted
          ? "UNKNOWN_CANCELLED"
          : "UNKNOWN_NO_DIAGNOSTIC",
        artifactRef: binding.artifactRef,
      }),
    );
    return "UNKNOWN";
  } finally {
    rmSync(cache, { recursive: true, force: true });
  }
}

async function main() {
  const args = process.argv.slice(2);
  if (args.length === 1 && args[0] === "--help") {
    console.log(
      "Read-only admission diagnostic. Before REQUEST_BUILD: --recipe-ref REF --recipe-generation N --pending-output NEW_FILE [--build-ref REF] [--timeout-seconds 1..1800]. This mode emits ONLY OBSERVED_PENDING_OWNER_CONFIRMATION; ROOT must compare private candidate recipe/generation/build/artifact/digest/verdict against protected owner GET before accepting it. Known artifact mode: --job-ref mc-admit-ID-admit --admission-run-id RUN --artifact-ref REF --image-digest sha256:DIGEST [--timeout-seconds 1..180] [--output NEW_FILE]. KUBECONFIG must be /home/s/.kube/config. Exact Job/Pod UID lineage and bounded diagnostic only; no cluster mutation/auth/cookie/Secret/DB reads. Output parent owned0700; new output0600/exclusive. No diagnostic means UNKNOWN, not successful admission.",
    );
    return;
  }
  const keys = new Map([
    ["--job-ref", "jobRef"],
    ["--admission-run-id", "admissionRunId"],
    ["--artifact-ref", "artifactRef"],
    ["--image-digest", "imageDigest"],
    ["--timeout-seconds", "timeoutSeconds"],
    ["--output", "output"],
    ["--recipe-ref", "recipeRef"],
    ["--recipe-generation", "recipeGeneration"],
    ["--build-ref", "buildRef"],
    ["--pending-output", "pendingOutput"],
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
      throw new Error("watch arguments are invalid");
    options[key] = ["timeoutSeconds", "recipeGeneration"].includes(key)
      ? Number(args[i + 1])
      : args[i + 1];
  }
  const shutdown = new AbortController();
  const stop = () => shutdown.abort();
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
  try {
    if (Object.hasOwn(options, "recipeRef")) {
      if (
        [
          "jobRef",
          "admissionRunId",
          "artifactRef",
          "imageDigest",
          "output",
        ].some((key) => Object.hasOwn(options, key))
      )
        throw new Error("watch modes are mixed");
      if (
        (await watchRecipe(options, { signal: shutdown.signal })) !==
        "PENDING_OWNER_CONFIRMATION"
      )
        process.exitCode = 2;
    } else {
      if (
        ["recipeGeneration", "buildRef", "pendingOutput"].some((key) =>
          Object.hasOwn(options, key),
        )
      )
        throw new Error("watch modes are mixed");
      if ((await watch(options, shutdown.signal)) !== "OBSERVED")
        process.exitCode = 2;
    }
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
    console.log('{"event":"WATCH_FAILED","code":"DIAGNOSTIC_UNAVAILABLE"}');
    process.exitCode = 1;
  });
