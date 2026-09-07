import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, lstatSync, readFileSync, realpathSync } from "node:fs";

function requireValue(condition, code) {
  if (!condition) throw new Error(code);
}

export function validSource(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).length === 2 && typeof value.path === "string" &&
    /^\/[A-Za-z0-9_./-]{1,400}$/.test(value.path) && !value.path.endsWith("/") &&
    !value.path.split("/").some((part) => part === "." || part === "..") &&
    /^[a-f0-9]{40}$/.test(value.revision ?? "");
}

// Выполняется на доверенном dev host, где Kubernetes монтирует этот checkout.
// Код sidecar и общая конфигурация из нового checkout не применяются.
export function inspectSource(path, frontend = false) {
  requireValue(realpathSync(path) === path && lstatSync(path).isDirectory(), "SOURCE_ROOT_INVALID");
  const git = (...args) => execFileSync("git", ["-C", path, ...args], {
    encoding: "utf8", timeout: 10000, maxBuffer: 1 << 20, stdio: ["ignore", "pipe", "pipe"],
  }).trim();
  requireValue(git("rev-parse", "--show-toplevel") === path &&
    ["https://github.com/codex-k8s/kodex.git", "git@github.com:codex-k8s/kodex.git"].includes(git("remote", "get-url", "origin")) &&
    git("status", "--porcelain", "--untracked-files=all") === "" &&
    [".env", ".kodex-env", ".kodex-remote-env"].every((name) => !existsSync(`${path}/${name}`)),
  "SOURCE_CHECKOUT_NOT_EXACT");
  const revision = git("rev-parse", "HEAD");
  requireValue(/^[a-f0-9]{40}$/.test(revision), "SOURCE_REVISION_INVALID");
  const result = { revision };
  if (frontend) {
    const hash = createHash("sha256");
    for (const name of ["package.json", "package-lock.json"])
      hash.update(name).update("\0").update(readFileSync(`${path}/services/staff/control-center/${name}`)).update("\0");
    result.dependenciesSHA256 = hash.digest("hex");
  }
  return result;
}

export function planSourceChange(deployment, afterSpec, containerIndex, source, inspect = inspectSource) {
  requireValue(validSource(source), "INVALID_APPLICATION_SOURCE");
  requireValue(deployment.metadata.labels?.["kodex.dev/local-profile"] === "hot-reload", "SOURCE_REQUIRES_HOT_RELOAD");
  const pod = deployment.spec.template.spec;
  const application = pod.containers[containerIndex];
  const frontend = deployment.metadata.name === "staff-control-center";
  const mounts = application.volumeMounts ?? [];
  const candidates = mounts.map((mount, index) => ({ mount, index })).filter(({ mount }) =>
    frontend ? mount.name === "dev-frontend-source" : mount.mountPath === "/workspace");
  requireValue(candidates.length === 1 && candidates[0].mount.readOnly === true, "APPLICATION_SOURCE_MOUNT_AMBIGUOUS");
  const { mount, index: mountIndex } = candidates[0];
  const volumeIndex = (pod.volumes ?? []).findIndex((volume) => volume.name === mount.name);
  const volume = pod.volumes?.[volumeIndex];
  requireValue(volumeIndex >= 0 && volume.hostPath && typeof volume.hostPath.path === "string", "APPLICATION_SOURCE_VOLUME_INVALID");
  const suffix = "/services/staff/control-center";
  requireValue(!frontend || volume.hostPath.path.endsWith(suffix), "FRONTEND_SOURCE_PATH_INVALID");
  const beforePath = frontend ? volume.hostPath.path.slice(0, -suffix.length) : volume.hostPath.path;
  const before = inspect(beforePath, frontend);
  const next = inspect(source.path, frontend);
  requireValue(/^[a-f0-9]{40}$/.test(before.revision) && next.revision === source.revision, "SOURCE_REVISION_MISMATCH");
  if (frontend)
    requireValue(/^[a-f0-9]{64}$/.test(before.dependenciesSHA256 ?? "") &&
      before.dependenciesSHA256 === next.dependenciesSHA256, "FRONTEND_DEPENDENCY_PREPARATION_REQUIRED");
  const patch = [];
  const siblings = [...pod.containers.filter((_, index) => index !== containerIndex), ...(pod.initContainers ?? [])];
  if (frontend) {
    requireValue(!siblings.some((container) => (container.volumeMounts ?? []).some((item) => item.name === mount.name)), "FRONTEND_SOURCE_SHARED");
    afterSpec.template.spec.volumes[volumeIndex].hostPath.path = `${source.path}${suffix}`;
    patch.push({ op: "replace", path: `/spec/template/spec/volumes/${volumeIndex}/hostPath/path`, value: `${source.path}${suffix}` });
  } else {
    const dedicated = "dev-application-source";
    const dedicatedIndex = pod.volumes.findIndex((item) => item.name === dedicated);
    requireValue(!siblings.some((container) => (container.volumeMounts ?? []).some((item) => item.name === dedicated)), "APPLICATION_SOURCE_SHARED_WITH_SIDECAR");
    if (dedicatedIndex >= 0) {
      requireValue(mount.name === dedicated && dedicatedIndex === volumeIndex, "APPLICATION_SOURCE_OWNERSHIP_MISMATCH");
      afterSpec.template.spec.volumes[volumeIndex].hostPath.path = source.path;
      patch.push({ op: "replace", path: `/spec/template/spec/volumes/${volumeIndex}/hostPath/path`, value: source.path });
    } else {
      const nextVolume = { name: dedicated, hostPath: { path: source.path, type: "Directory" } };
      afterSpec.template.spec.volumes.push(nextVolume);
      afterSpec.template.spec.containers[containerIndex].volumeMounts[mountIndex].name = dedicated;
      patch.push({ op: "add", path: "/spec/template/spec/volumes/-", value: nextVolume },
        { op: "replace", path: `/spec/template/spec/containers/${containerIndex}/volumeMounts/${mountIndex}/name`, value: dedicated });
    }
  }
  return { patch, before: { path: beforePath, revision: before.revision }, changed: source.path !== beforePath };
}
