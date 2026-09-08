import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { closeSync, fstatSync, openSync, readFileSync, readlinkSync } from "node:fs";
import { fingerprint } from "./scoped-release.mjs";

// Единственная repo-owned запись. CLI не принимает внешний allowlist/manifest.
const capability = JSON.parse(readFileSync(new URL("./role-image-builder-writer-capability.json", import.meta.url), "utf8"));
const uidPattern = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const containerIDPattern = /^containerd:\/\/[a-f0-9]{64}$/;
function requireValue(value, code) { if (!value) throw new Error(code); }

export function writerCapability() { return structuredClone(capability); }

export function requireImageWriterTarget(target, container) {
  requireValue(target === "role-image-builder" && capability.version === 1 && capability.target === target &&
    container?.name === capability.container && container.image === capability.image &&
    /^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(capability.image) &&
    fingerprint(container.command) === fingerprint([capability.executable]) &&
    (!container.args || container.args.length === 0) &&
    !(container.volumeMounts ?? []).some((mount) => mount.mountPath === "/workspace"), "IMAGE_WRITER_CAPABILITY_REQUIRED");
}

export function verifyImageWriterBinding(pod, container, runtime) {
  requireImageWriterTarget("role-image-builder", container);
  requireValue(pod?.metadata?.namespace === "kodex-system" && uidPattern.test(pod.metadata.uid) &&
    pod.metadata.labels?.["app.kubernetes.io/name"] === "role-image-builder" &&
    !pod.metadata.deletionTimestamp && pod.status?.phase === "Running", "EXACT_IMAGE_WRITER_POD_REQUIRED");
  const declared = [...(pod.spec?.containers ?? []), ...(pod.spec?.initContainers ?? [])]
    .filter((item) => item.name === container.name);
  requireValue(declared.length === 1 && fingerprint(declared[0]) === fingerprint(container), "IMAGE_WRITER_SPEC_BINDING_REJECTED");
  const statuses = [...(pod.status.containerStatuses ?? []), ...(pod.status.initContainerStatuses ?? [])]
    .filter((status) => status.name === container.name);
  requireValue(statuses.length === 1 && statuses[0].ready === true && statuses[0].state?.running &&
    Number.isSafeInteger(statuses[0].restartCount) && statuses[0].restartCount >= 0 &&
    containerIDPattern.test(statuses[0].containerID) && statuses[0].imageID === capability.image,
  "EXACT_IMAGE_WRITER_CONTAINER_REQUIRED");
  const status = statuses[0], id = status.containerID.slice("containerd://".length);
  const labels = runtime.status?.labels;
  requireValue(runtime.status?.id === id && runtime.status.state === "CONTAINER_RUNNING" &&
    runtime.status.imageRef === capability.image && runtime.status.metadata?.name === container.name &&
    runtime.status.metadata.attempt === status.restartCount &&
    labels?.["io.kubernetes.pod.uid"] === pod.metadata.uid &&
    labels?.["io.kubernetes.pod.name"] === pod.metadata.name &&
    labels?.["io.kubernetes.pod.namespace"] === pod.metadata.namespace &&
    labels?.["io.kubernetes.container.name"] === container.name &&
    Number.isSafeInteger(runtime.info?.pid) && runtime.info.pid > 1 &&
    fingerprint(runtime.info.runtimeSpec?.process?.args) === fingerprint([capability.executable]),
  "IMAGE_WRITER_RUNTIME_BINDING_REJECTED");
  return { podUID: pod.metadata.uid, podName: pod.metadata.name, container: container.name,
    containerID: id, imageID: status.imageID, restartCount: status.restartCount, pid: runtime.info.pid };
}

export function processStartTicks(stat) {
  const fields = stat.slice(stat.lastIndexOf(") ") + 2).trim().split(/\s+/);
  requireValue(stat.includes(") ") && /^\d+$/.test(fields[19]), "IMAGE_WRITER_PROCESS_IDENTITY_REQUIRED");
  return fields[19];
}

function readProcess(pid) {
  const root = `/proc/${pid}`;
  const before = processStartTicks(readFileSync(`${root}/stat`, "utf8"));
  requireValue(readlinkSync(`${root}/exe`) === capability.executable, "IMAGE_WRITER_EXECUTABLE_REJECTED");
  const fd = openSync(`${root}/exe`, "r");
  try {
    const stat = fstatSync(fd);
    requireValue(stat.isFile() && stat.size > 0 && stat.size <= (64 << 20), "BOUNDED_WRITER_EXECUTABLE_REQUIRED");
    const sha256 = createHash("sha256").update(readFileSync(fd)).digest("hex");
    requireValue(processStartTicks(readFileSync(`${root}/stat`, "utf8")) === before &&
      readlinkSync(`${root}/exe`) === capability.executable, "IMAGE_WRITER_PROCESS_CHANGED");
    return { startTicks: before, executableSHA256: sha256, executable: capability.executable,
      device: String(stat.dev), inode: String(stat.ino) };
  } finally { closeSync(fd); }
}

// Raw CRI output остаётся только в памяти. Ни env, ни cmdline, ни Secret не пишутся.
export function inspectImageWriter(pod, container, io = {}) {
  const readRuntime = io.readRuntime ?? ((id) => JSON.parse(execFileSync("k3s", ["crictl", "inspect", id],
    { encoding: "utf8", stdio: "pipe", timeout: 10_000, maxBuffer: 4 << 20 })));
  const processReader = io.readProcess ?? readProcess;
  if (!io.readRuntime || !io.readProcess) requireValue(process.getuid?.() === 0, "SRE_HOST_ROOT_REQUIRED");
  const statuses = [...(pod.status?.containerStatuses ?? []), ...(pod.status?.initContainerStatuses ?? [])]
    .filter((status) => status.name === container.name);
  requireValue(statuses.length === 1 && containerIDPattern.test(statuses[0].containerID), "EXACT_IMAGE_WRITER_CONTAINER_REQUIRED");
  const id = statuses[0].containerID.slice("containerd://".length);
  const binding = verifyImageWriterBinding(pod, container, readRuntime(id));
  const processState = processReader(binding.pid);
  requireValue(processState.executable === capability.executable && processState.executableSHA256 === capability.executableSHA256 &&
    /^\d+$/.test(processState.startTicks) && /^\d+$/.test(processState.device) && /^\d+$/.test(processState.inode), "IMAGE_WRITER_EXECUTABLE_REJECTED");
  requireValue(fingerprint(verifyImageWriterBinding(pod, container, readRuntime(id))) === fingerprint(binding) &&
    fingerprint(processReader(binding.pid)) === fingerprint(processState), "IMAGE_WRITER_PROCESS_CHANGED");
  return { kind: "image", capabilitySHA256: fingerprint(capability), sourceRevision: capability.source.revision,
    ...binding, ...processState };
}
