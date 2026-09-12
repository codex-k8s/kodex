import { fingerprint } from "./scoped-release.mjs";
import { validSource } from "./application-source.mjs";
import { planFrontendDrainProfile } from "./frontend-drain-profile.mjs";

const component = "staff-control-center";
const root = "/workspace/services/staff/control-center";
const sha = /^[a-f0-9]{64}$/;
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const requireValue = (value, code) => {
  if (!value) throw new Error(code);
};

export function frontendDependencyState(deployment) {
  const drain = planFrontendDrainProfile(deployment);
  requireValue(
    drain.beforeSpecSHA256 === drain.afterSpecSHA256,
    "FRONTEND_DRAIN_PREPARATION_REQUIRED",
  );
  const pod = deployment.spec.template.spec,
    container = pod.containers[0];
  requireValue(
    uuid.test(deployment.metadata.uid) &&
      /^docker\.io\/library\/node:[^\s]+@sha256:[a-f0-9]{64}$/.test(
        container.image,
      ),
    "FRONTEND_NODE_IMAGE_INVALID",
  );
  requireValue(
    container.command?.length === 1 &&
      container.command[0] === "/workspace/tools/dev/run-frontend.sh" &&
      (container.args ?? []).length === 0 &&
      container.workingDir === root,
    "FRONTEND_RUNTIME_COMMAND_INVALID",
  );
  const imageEnv =
    container.env?.filter((item) => item.name === "KODEX_DEV_NODE_IMAGE") ?? [];
  requireValue(
    imageEnv.length === 1 &&
      imageEnv[0].value === container.image &&
      !imageEnv[0].valueFrom,
    "FRONTEND_RUNTIME_IMAGE_MISMATCH",
  );
  const volumes = pod.volumes ?? [],
    mounts = container.volumeMounts ?? [];
  requireValue(
    new Set(volumes.map((item) => item.name)).size === volumes.length &&
      new Set(mounts.map((item) => item.mountPath)).size === mounts.length,
    "FRONTEND_VOLUME_AMBIGUOUS",
  );
  const volumeAt = (name, mountPath, type) => {
    const index = volumes.findIndex((item) => item.name === name),
      volume = volumes[index];
    const selected = mounts.filter((item) => item.name === name);
    requireValue(
      index >= 0 &&
        volume.hostPath?.type === type &&
        typeof volume.hostPath.path === "string" &&
        selected.length === 1 &&
        selected[0].mountPath === mountPath &&
        selected[0].readOnly === true &&
        !selected[0].subPath &&
        !selected[0].subPathExpr &&
        !(pod.initContainers ?? []).some((item) =>
          item.volumeMounts?.some((mount) => mount.name === name),
        ),
      "FRONTEND_MOUNT_INVALID",
    );
    return { index, path: volume.hostPath.path };
  };
  const source = volumeAt("dev-frontend-source", root, "Directory");
  const cache = volumeAt(
    "dev-node-modules",
    `${root}/node_modules`,
    "Directory",
  );
  const runner = volumeAt(
    "dev-frontend-runner",
    "/workspace/tools/dev/run-frontend.sh",
    "File",
  );
  const identity = volumeAt(
    "dev-frontend-identity",
    "/workspace/tools/dev/frontend-cache-identity.sh",
    "File",
  );
  requireValue(
    source.path.endsWith("/services/staff/control-center") &&
      mounts
        .filter((item) => item.mountPath.startsWith(`${root}/`))
        .every(
          (item) =>
            [`${root}/node_modules`, `${root}/public/config`].includes(
              item.mountPath,
            ) &&
            item.readOnly === true &&
            !item.subPath &&
            !item.subPathExpr,
        ),
    "FRONTEND_NESTED_MOUNT_INVALID",
  );
  return {
    image: container.image,
    source: {
      ...source,
      root: source.path.slice(0, -"/services/staff/control-center".length),
    },
    cache,
    runner,
    identity,
  };
}

// Prepared cache — отдельный переход. Обычный planSourceChange остаётся строгим.
export function planFrontendDependencies(
  deployment,
  { source, previousSource, cache, releaseID },
) {
  const before = frontendDependencyState(deployment);
  requireValue(
    validSource(source) &&
      validSource(previousSource) &&
      previousSource.path === before.source.root &&
      uuid.test(releaseID),
    "FRONTEND_DEPENDENCY_INPUT_INVALID",
  );
  requireValue(
    cache &&
      cache.source === source.path &&
      cache.revision === source.revision &&
      cache.image === before.image &&
      sha.test(cache.identity ?? "") &&
      sha.test(cache.manifestsSHA256 ?? "") &&
      typeof cache.path === "string" &&
      /^\/[A-Za-z0-9_./-]+$/.test(cache.path) &&
      !cache.path.split("/").some((part) => part === "." || part === "..") &&
      cache.path.endsWith(`/frontend-v1/${cache.identity}/node_modules`),
    "FRONTEND_PREPARED_CACHE_INVALID",
  );
  requireValue(
    source.path !== previousSource.path || cache.path !== before.cache.path,
    "FRONTEND_DEPENDENCIES_UNCHANGED",
  );
  const after = structuredClone(deployment.spec);
  after.template.spec.volumes[before.source.index].hostPath.path =
    `${source.path}/services/staff/control-center`;
  after.template.spec.volumes[before.cache.index].hostPath.path = cache.path;
  const annotations = {
    ...after.template.metadata.annotations,
    "kodex.dev/application-release": releaseID,
    "kodex.dev/application-source-sha": source.revision,
    "kodex.dev/frontend-cache-identity": cache.identity,
  };
  after.template.metadata.annotations = annotations;
  return {
    releaseID,
    name: component,
    uid: deployment.metadata.uid,
    resourceVersion: deployment.metadata.resourceVersion,
    image: before.image,
    source,
    cache,
    previousSource,
    previousCache: before.cache.path,
    beforeSpecSHA256: fingerprint(deployment.spec),
    afterSpecSHA256: fingerprint(after),
    patch: [
      { op: "test", path: "/metadata/uid", value: deployment.metadata.uid },
      {
        op: "test",
        path: "/metadata/resourceVersion",
        value: deployment.metadata.resourceVersion,
      },
      {
        op: "replace",
        path: `/spec/template/spec/volumes/${before.source.index}/hostPath/path`,
        value: `${source.path}/services/staff/control-center`,
      },
      {
        op: "replace",
        path: `/spec/template/spec/volumes/${before.cache.index}/hostPath/path`,
        value: cache.path,
      },
      {
        op: "add",
        path: "/spec/template/metadata/annotations",
        value: annotations,
      },
    ],
  };
}
