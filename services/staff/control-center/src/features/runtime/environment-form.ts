import type {
  RuntimeEnvironmentInput,
  RuntimeEnvironmentPolicy,
  RuntimeEnvironmentPolicyInput,
  RuntimeKubernetesAccessKind,
  RuntimeNetworkDestination,
  RuntimeSecretBinding,
  RuntimeSecretDescriptor,
  RuntimeVolumeInput,
} from "@/shared/api/generated/openapi/types.gen";

const variableName = /^[A-Z_][A-Z0-9_]{0,126}$/;
const toolCommand = /^[A-Za-z0-9][A-Za-z0-9._+-]{0,159}$/;
const volumeName = /^[a-z](?:[a-z0-9-]{0,30}[a-z0-9])?$/;
const sha256 = /^[a-f0-9]{64}$/;
const reservedVolumeNames = new Set([
  "callback-ca",
  "callback-client",
  "kube-api-access",
  "provider-auth",
  "provider-socket",
  "provider-tmp",
  "runtime-input",
  "session",
  "tmp",
]);
const reservedVariablePrefixes = [
  "KODEX_",
  "CODEX_",
  "OPENAI_",
  "OTEL_",
  "AWS_",
  "AZURE_",
  "GOOGLE_",
  "KUBERNETES_",
];
const reservedVariableNames = new Set([
  "HOME",
  "PATH",
  "PWD",
  "SHELL",
  "USER",
  "LOGNAME",
  "TMPDIR",
  "HTTP_PROXY",
  "HTTPS_PROXY",
  "NO_PROXY",
  "SSL_CERT_FILE",
  "SSL_CERT_DIR",
]);

export const mandatoryRuntimeNetworkDestinations = [
  "DNS",
  "PROVIDER_PROXY",
  "RUNTIME_CALLBACK",
] as const satisfies readonly RuntimeNetworkDestination[];

export const runtimeResourceBounds = {
  cpuRequestMilli: { min: 100, max: 8000 },
  cpuLimitMilli: { min: 100, max: 16000 },
  memoryRequestMib: { min: 128, max: 32768 },
  memoryLimitMib: { min: 128, max: 65536 },
  ephemeralStorageRequestMib: { min: 256, max: 20480 },
  ephemeralStorageLimitMib: { min: 256, max: 102400 },
} as const;

export const runtimeVolumeBounds = {
  minSizeMib: 16,
  maxSizeMib: 10240,
  maxItems: 16,
} as const;

export function defaultRuntimeEnvironmentPolicy(): RuntimeEnvironmentPolicyInput {
  return {
    resources: {
      cpuRequestMilli: 2000,
      cpuLimitMilli: 2000,
      memoryRequestMib: 4096,
      memoryLimitMib: 4096,
      ephemeralStorageRequestMib: 1024,
      ephemeralStorageLimitMib: 4096,
    },
    volumes: [],
    networkDestinations: [...mandatoryRuntimeNetworkDestinations],
    kubernetesAccess: "NONE",
  };
}

export function editableRuntimeEnvironmentPolicy(
  policy: RuntimeEnvironmentPolicy,
): RuntimeEnvironmentPolicyInput {
  return {
    resources: { ...policy.resources },
    volumes: policy.volumes.map(({ name, kind, sizeMib }) => ({
      name,
      kind,
      sizeMib,
    })),
    networkDestinations: runtimeNetworkDestinations(
      policy.kubernetesAccess.kind,
    ),
    kubernetesAccess: policy.kubernetesAccess.kind,
  };
}

export function runtimeNetworkDestinations(
  access: RuntimeKubernetesAccessKind,
): RuntimeNetworkDestination[] {
  return access === "READ_OWN_EXECUTION"
    ? [...mandatoryRuntimeNetworkDestinations, "KUBERNETES_API"]
    : [...mandatoryRuntimeNetworkDestinations];
}

export function setRuntimeKubernetesAccess(
  policy: RuntimeEnvironmentPolicyInput,
  access: RuntimeKubernetesAccessKind,
): void {
  policy.kubernetesAccess = access;
  policy.networkDestinations = runtimeNetworkDestinations(access);
}

export function emptyRuntimeVolume(): RuntimeVolumeInput {
  return { name: "", kind: "EPHEMERAL_DISK", sizeMib: 1024 };
}

export interface EnvironmentFormProblem {
  field: string;
  message: string;
}

export function validateEnvironmentInput(
  input: RuntimeEnvironmentInput,
): EnvironmentFormProblem[] {
  const problems: EnvironmentFormProblem[] = [];
  if (!input.name.trim())
    problems.push({ field: "name", message: "runtime.errors.nameRequired" });
  if (!input.imageArtifactRef.trim())
    problems.push({
      field: "imageArtifactRef",
      message: "runtime.errors.imageRequired",
    });
  if (input.name.length > 120)
    problems.push({
      field: "name",
      message: "runtime.errors.nameTooLong",
    });
  if (input.description.length > 1000)
    problems.push({
      field: "description",
      message: "runtime.errors.descriptionTooLong",
    });
  validateCollectionLimit(input.values, "values", problems);
  validateCollectionLimit(input.secretBindings, "secretBindings", problems);
  validateCollectionLimit(input.tools, "tools", problems);
  const names = new Set<string>();
  for (const [index, item] of input.values.entries()) {
    if (!variableName.test(item.name))
      problems.push({
        field: `values.${String(index)}.name`,
        message: "runtime.errors.variableName",
      });
    else if (isReservedVariableName(item.name))
      problems.push({
        field: `values.${String(index)}.name`,
        message: "runtime.errors.reservedVariableName",
      });
    if (names.has(item.name))
      problems.push({
        field: `values.${String(index)}.name`,
        message: "runtime.errors.duplicateVariable",
      });
    names.add(item.name);
  }
  for (const [index, item] of input.secretBindings.entries()) {
    validateSecret(item, index, names, problems);
    names.add(item.name);
  }
  const toolCommands = new Set<string>();
  for (const [index, item] of input.tools.entries()) {
    if (!item.name.trim() || item.name.trim() !== item.name)
      problems.push({
        field: `tools.${String(index)}.name`,
        message: "runtime.errors.toolNameRequired",
      });
    else if (item.name.length > 160)
      problems.push({
        field: `tools.${String(index)}.name`,
        message: "runtime.errors.toolNameTooLong",
      });
    if (!toolCommand.test(item.command))
      problems.push({
        field: `tools.${String(index)}.command`,
        message: "runtime.errors.toolCommand",
      });
    if (
      !item.description.trim() ||
      item.description.trim() !== item.description
    )
      problems.push({
        field: `tools.${String(index)}.description`,
        message: "runtime.errors.toolDescriptionRequired",
      });
    else if (item.description.length > 500)
      problems.push({
        field: `tools.${String(index)}.description`,
        message: "runtime.errors.toolDescriptionTooLong",
      });
    if (item.usageHint.length > 500)
      problems.push({
        field: `tools.${String(index)}.usageHint`,
        message: "runtime.errors.toolUsageHintTooLong",
      });
    if (toolCommands.has(item.command))
      problems.push({
        field: `tools.${String(index)}.command`,
        message: "runtime.errors.duplicateTool",
      });
    toolCommands.add(item.command);
  }
  validateRuntimePolicy(input.policy, problems);
  return problems;
}

export function normalizeRuntimeEnvironmentInput(
  input: RuntimeEnvironmentInput,
): RuntimeEnvironmentInput {
  return {
    name: input.name.trim(),
    description: input.description.trim(),
    imageArtifactRef: input.imageArtifactRef,
    tools: input.tools.map((item) => ({
      name: item.name.trim(),
      command: item.command,
      description: item.description.trim(),
      usageHint: item.usageHint.trim(),
    })),
    values: input.values.map((item) => ({
      name: item.name.trim(),
      value: item.value,
    })),
    secretBindings: input.secretBindings.map((item) => ({
      name: item.name.trim(),
      secretRef: item.secretRef,
    })),
    policy: {
      resources: { ...input.policy.resources },
      volumes: input.policy.volumes.map((item) => ({
        name: item.name.trim(),
        kind: item.kind,
        sizeMib: item.sizeMib,
      })),
      networkDestinations: [...input.policy.networkDestinations],
      kubernetesAccess: input.policy.kubernetesAccess,
    },
  };
}

function validateCollectionLimit(
  items: readonly unknown[],
  field: "values" | "secretBindings" | "tools",
  problems: EnvironmentFormProblem[],
): void {
  if (items.length > 128)
    problems.push({ field, message: "runtime.errors.collectionLimit" });
}

function validateRuntimePolicy(
  policy: RuntimeEnvironmentPolicyInput,
  problems: EnvironmentFormProblem[],
): void {
  const resources = policy.resources;
  validateIntegerRange(
    resources.cpuRequestMilli,
    runtimeResourceBounds.cpuRequestMilli,
    "policy.resources.cpuRequestMilli",
    "runtime.errors.cpuRequestRange",
    problems,
  );
  validateIntegerRange(
    resources.cpuLimitMilli,
    runtimeResourceBounds.cpuLimitMilli,
    "policy.resources.cpuLimitMilli",
    "runtime.errors.cpuLimitRange",
    problems,
  );
  validateIntegerRange(
    resources.memoryRequestMib,
    runtimeResourceBounds.memoryRequestMib,
    "policy.resources.memoryRequestMib",
    "runtime.errors.memoryRequestRange",
    problems,
  );
  validateIntegerRange(
    resources.memoryLimitMib,
    runtimeResourceBounds.memoryLimitMib,
    "policy.resources.memoryLimitMib",
    "runtime.errors.memoryLimitRange",
    problems,
  );
  validateIntegerRange(
    resources.ephemeralStorageRequestMib,
    runtimeResourceBounds.ephemeralStorageRequestMib,
    "policy.resources.ephemeralStorageRequestMib",
    "runtime.errors.ephemeralStorageRequestRange",
    problems,
  );
  validateIntegerRange(
    resources.ephemeralStorageLimitMib,
    runtimeResourceBounds.ephemeralStorageLimitMib,
    "policy.resources.ephemeralStorageLimitMib",
    "runtime.errors.ephemeralStorageLimitRange",
    problems,
  );
  if (resources.cpuLimitMilli < resources.cpuRequestMilli)
    problems.push({
      field: "policy.resources.cpuLimitMilli",
      message: "runtime.errors.cpuLimitBelowRequest",
    });
  if (resources.memoryLimitMib < resources.memoryRequestMib)
    problems.push({
      field: "policy.resources.memoryLimitMib",
      message: "runtime.errors.memoryLimitBelowRequest",
    });
  if (resources.ephemeralStorageLimitMib < resources.ephemeralStorageRequestMib)
    problems.push({
      field: "policy.resources.ephemeralStorageLimitMib",
      message: "runtime.errors.ephemeralStorageLimitBelowRequest",
    });

  if (policy.volumes.length > runtimeVolumeBounds.maxItems)
    problems.push({
      field: "policy.volumes",
      message: "runtime.errors.volumeLimit",
    });
  const volumeNames = new Set<string>();
  for (const [index, volume] of policy.volumes.entries()) {
    if (!volumeName.test(volume.name))
      problems.push({
        field: `policy.volumes.${String(index)}.name`,
        message: "runtime.errors.volumeName",
      });
    else if (reservedVolumeNames.has(volume.name))
      problems.push({
        field: `policy.volumes.${String(index)}.name`,
        message: "runtime.errors.reservedVolumeName",
      });
    if (volumeNames.has(volume.name))
      problems.push({
        field: `policy.volumes.${String(index)}.name`,
        message: "runtime.errors.duplicateVolume",
      });
    volumeNames.add(volume.name);
    validateIntegerRange(
      volume.sizeMib,
      {
        min: runtimeVolumeBounds.minSizeMib,
        max: runtimeVolumeBounds.maxSizeMib,
      },
      `policy.volumes.${String(index)}.sizeMib`,
      "runtime.errors.volumeSizeRange",
      problems,
    );
  }

  const expectedDestinations = runtimeNetworkDestinations(
    policy.kubernetesAccess,
  );
  if (
    policy.networkDestinations.length !== expectedDestinations.length ||
    expectedDestinations.some(
      (destination) =>
        policy.networkDestinations.filter((item) => item === destination)
          .length !== 1,
    )
  )
    problems.push({
      field: "policy.networkDestinations",
      message: "runtime.errors.networkDestinations",
    });
}

function validateIntegerRange(
  value: number,
  bounds: { min: number; max: number },
  field: string,
  message: string,
  problems: EnvironmentFormProblem[],
): void {
  if (!Number.isInteger(value) || value < bounds.min || value > bounds.max)
    problems.push({ field, message });
}

function validateSecret(
  item: RuntimeSecretBinding,
  index: number,
  names: Set<string>,
  problems: EnvironmentFormProblem[],
): void {
  if (!variableName.test(item.name))
    problems.push({
      field: `secretBindings.${String(index)}.name`,
      message: "runtime.errors.variableName",
    });
  else if (isReservedVariableName(item.name))
    problems.push({
      field: `secretBindings.${String(index)}.name`,
      message: "runtime.errors.reservedVariableName",
    });
  if (names.has(item.name))
    problems.push({
      field: `secretBindings.${String(index)}.name`,
      message: "runtime.errors.duplicateVariable",
    });
  if (!item.secretRef.trim())
    problems.push({
      field: `secretBindings.${String(index)}.secretRef`,
      message: "runtime.errors.secretBindingRequired",
    });
}

function isReservedVariableName(name: string): boolean {
  return (
    reservedVariableNames.has(name) ||
    reservedVariablePrefixes.some((prefix) => name.startsWith(prefix))
  );
}

export function emptySecretBinding(): RuntimeSecretBinding {
  return {
    name: "",
    secretRef: "",
  };
}

export function editableSecretBindings(
  descriptors: readonly RuntimeSecretDescriptor[],
): RuntimeSecretBinding[] {
  return descriptors.map(({ name, secretRef }) => ({ name, secretRef }));
}

export function hasEffectivePolicyDigests(
  policy: RuntimeEnvironmentPolicy,
): boolean {
  return [
    policy.resourcesDigest,
    policy.volumesDigest,
    policy.networkDigest,
    policy.rbacDigest,
  ].every((value) => sha256.test(value));
}
