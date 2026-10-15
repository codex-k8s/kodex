import { fingerprint } from "./stable-inventory.mjs";

export const logicalRegistryHost = "registry.local.kodex";
export const nodeRegistryFiles = { registry: "/etc/rancher/k3s/registries.yaml", config: "/etc/rancher/k3s/config.yaml" };
export const nodeRegistryTLSFiles = ["ca.crt", "client.crt", "client.key"].map(name => `/etc/rancher/k3s/kodex-registry/${name}`);
function requireValue(value, code) { if (!value) throw Error(code); }
function object(value) { return value && typeof value === "object" && !Array.isArray(value); }

export function nativeRegistryMirror(registries, config, physicalHost) {
  requireValue(typeof physicalHost === "string" && /^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$/.test(physicalHost) &&
    physicalHost.includes(".") && !physicalHost.includes("..") && physicalHost !== logicalRegistryHost,
    "NODE_MIRROR_HOST_INVALID");
  requireValue(object(registries) && object(registries.mirrors) && object(registries.configs) && object(config) &&
    !Object.hasOwn(registries.mirrors, "*") && !Object.hasOwn(registries.configs, "*"), "NODE_MIRROR_CONFIGURATION_INVALID");
  const endpoint = { endpoint: [`https://${physicalHost}`] }, credentials = registries.configs[physicalHost];
  requireValue(fingerprint(registries.mirrors[physicalHost] ?? null) === fingerprint(endpoint) && object(credentials?.auth) &&
    typeof credentials.auth.username === "string" && credentials.auth.username.length > 0 &&
    typeof credentials.auth.password === "string" && credentials.auth.password.length >= 32 && object(credentials.tls) &&
    credentials.tls.ca_file === nodeRegistryTLSFiles[0] && credentials.tls.cert_file === nodeRegistryTLSFiles[1] &&
    credentials.tls.key_file === nodeRegistryTLSFiles[2] && !credentials.tls.insecure_skip_verify,
    "NODE_MIRROR_CANONICAL_TRANSPORT_INVALID");
  requireValue(Object.keys(credentials).every(k => ["auth", "tls"].includes(k)) &&
    Object.keys(credentials.auth).every(k => ["username", "password"].includes(k)) &&
    Object.keys(credentials.tls).every(k => ["ca_file", "cert_file", "key_file", "insecure_skip_verify"].includes(k)),
    "NODE_MIRROR_CANONICAL_TRANSPORT_INVALID");
  requireValue(!Object.hasOwn(registries.configs, logicalRegistryHost) &&
    (!Object.hasOwn(registries.mirrors, logicalRegistryHost) ||
      fingerprint(registries.mirrors[logicalRegistryHost]) === fingerprint(endpoint)), "NODE_MIRROR_LOGICAL_SOURCE_CONFLICT");
  requireValue(!Object.hasOwn(config, "private-registry") &&
    (!Object.hasOwn(config, "disable-default-registry-endpoint") || typeof config["disable-default-registry-endpoint"] === "boolean"),
    "NODE_MIRROR_NATIVE_OVERRIDE_INVALID");
  const afterRegistry = structuredClone(registries), afterConfig = structuredClone(config);
  afterRegistry.mirrors[logicalRegistryHost] = endpoint;
  afterConfig["disable-default-registry-endpoint"] = true;
  requireValue(fingerprint(afterRegistry.configs) === fingerprint(registries.configs), "NODE_MIRROR_AUTH_CHANGED");
  return { registry: afterRegistry, config: afterConfig };
}

export function validateNodeContainer(node, container, cluster) {
  requireValue(node?.metadata?.uid && node.metadata.name && /^[a-z0-9-]+$/.test(cluster) &&
    /^sha256:[a-f0-9]{64}$/.test(container?.Image ?? "") && /^[a-f0-9]{64}$/.test(container?.Id ?? "") &&
    container.Config?.Labels?.["k3d.cluster"] === cluster &&
    ["server", "agent"].includes(container.Config.Labels["k3d.role"]) &&
    new RegExp(`^k3d-${cluster}-${container.Config.Labels["k3d.role"]}-[0-9]+$`).test(node.metadata.name) &&
    container.Name === `/${node.metadata.name}` && container.State?.Running === true, "NODE_MIRROR_CONTAINER_IDENTITY_INVALID");
  requireValue(!(container.Config.Cmd ?? []).some(value => /^--(?:config|private-registry|disable-default-registry-endpoint)(?:=|$)/.test(value)) &&
    !(container.Config.Env ?? []).some(value => /^K3S_(?:CONFIG_FILE|PRIVATE_REGISTRY|DISABLE_DEFAULT_REGISTRY_ENDPOINT)=/.test(value)),
    "NODE_MIRROR_RUNTIME_OVERRIDE_UNSUPPORTED");
  requireValue(node.status?.conditions?.some(c => c.type === "Ready" && c.status === "True"), "NODE_MIRROR_NODE_NOT_READY");
  return { name: node.metadata.name, uid: node.metadata.uid, containerID: container.Id, imageID: container.Image,
    commandSHA256: fingerprint({ cmd: container.Config.Cmd, entrypoint: container.Config.Entrypoint,
      // Полный env не сохраняется и не выдаётся; hash связывает identity/restart.
      environment: container.Config.Env, labels: container.Config.Labels }) };
}
