<script setup lang="ts">
import { computed, watch } from "vue";

import {
  operationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import {
  defaultRuntimeEnvironmentPolicy,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";
import RuntimeEnvironmentPolicyFields from "@/features/runtime/RuntimeEnvironmentPolicyFields.vue";
import type {
  RuntimeEnvironmentPolicyInput,
  RuntimeNetworkDestination,
  RuntimeVolumeInput,
} from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  operation: EditablePlanOperation;
  disabled: boolean;
}>();
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: unknown];
}>();

function record(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function exactKeys(
  value: Record<string, unknown>,
  keys: readonly string[],
): boolean {
  return (
    Object.keys(value).length === keys.length &&
    keys.every((key) => key in value)
  );
}

function parsePolicy(raw: unknown): RuntimeEnvironmentPolicyInput | undefined {
  const value = record(raw);
  if (
    !value ||
    !exactKeys(value, [
      "resources",
      "volumes",
      "networkDestinations",
      "kubernetesAccess",
    ])
  )
    return undefined;
  const resources = record(value.resources);
  const resourceKeys = [
    "cpuRequestMilli",
    "cpuLimitMilli",
    "memoryRequestMib",
    "memoryLimitMib",
    "ephemeralStorageRequestMib",
    "ephemeralStorageLimitMib",
  ] as const;
  if (
    !resources ||
    !exactKeys(resources, resourceKeys) ||
    resourceKeys.some((key) => typeof resources[key] !== "number")
  )
    return undefined;
  if (!Array.isArray(value.volumes) || value.volumes.length > 16)
    return undefined;
  const volumes: RuntimeVolumeInput[] = [];
  for (const rawVolume of value.volumes) {
    const volume = record(rawVolume);
    if (
      !volume ||
      !exactKeys(volume, ["name", "kind", "sizeMib"]) ||
      typeof volume.name !== "string" ||
      (volume.kind !== "EPHEMERAL_DISK" &&
        volume.kind !== "EPHEMERAL_MEMORY") ||
      typeof volume.sizeMib !== "number"
    )
      return undefined;
    volumes.push({
      name: volume.name,
      kind: volume.kind,
      sizeMib: volume.sizeMib,
    });
  }
  const allowed = [
    "DNS",
    "PROVIDER_PROXY",
    "RUNTIME_CALLBACK",
    "KUBERNETES_API",
  ];
  if (
    !Array.isArray(value.networkDestinations) ||
    value.networkDestinations.length > 4 ||
    value.networkDestinations.some(
      (item) => typeof item !== "string" || !allowed.includes(item),
    ) ||
    (value.kubernetesAccess !== "NONE" &&
      value.kubernetesAccess !== "READ_OWN_EXECUTION")
  )
    return undefined;
  return {
    resources: {
      cpuRequestMilli: resources.cpuRequestMilli as number,
      cpuLimitMilli: resources.cpuLimitMilli as number,
      memoryRequestMib: resources.memoryRequestMib as number,
      memoryLimitMib: resources.memoryLimitMib as number,
      ephemeralStorageRequestMib:
        resources.ephemeralStorageRequestMib as number,
      ephemeralStorageLimitMib: resources.ephemeralStorageLimitMib as number,
    },
    volumes,
    networkDestinations:
      value.networkDestinations as RuntimeNetworkDestination[],
    kubernetesAccess: value.kubernetesAccess,
  };
}

const proposed = computed(() => {
  try {
    return operationParameter(props.operation, "policy");
  } catch {
    return undefined;
  }
});
const policy = computed(() =>
  proposed.value === undefined
    ? props.operation.value.type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION"
      ? parsePolicy(props.operation.value.before.policyInput)
      : defaultRuntimeEnvironmentPolicy()
    : parsePolicy(proposed.value),
);
const problems = computed(() => {
  if (!policy.value) return ["assistant.planEditor.environmentPolicyInvalid"];
  return [
    ...new Set(
      validateEnvironmentInput({
        name: "Assistant draft",
        description: "",
        imageArtifactRef: "imgart_example",
        tools: [],
        values: [],
        secretBindings: [],
        policy: policy.value,
      })
        .filter((problem) => problem.field.startsWith("policy"))
        .map((problem) => problem.message),
    ),
  ];
});
watch(
  () => problems.value.length === 0,
  (valid) => emit("valid", valid),
  { immediate: true },
);

function update(value: RuntimeEnvironmentPolicyInput): void {
  emit("parameter", "policy", value);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-environment-policy">
    <RuntimeEnvironmentPolicyFields
      :policy="policy ?? defaultRuntimeEnvironmentPolicy()"
      :disabled="disabled || !policy"
      @update:policy="update"
    />
    <p
      v-if="policy?.kubernetesAccess === 'READ_OWN_EXECUTION'"
      class="assistant-plan-friendly__hint"
    >
      {{ $t("assistant.planEditor.environmentPolicyFreshAuthentication") }}
    </p>
    <p
      v-for="problem in problems"
      :key="problem"
      class="field-error"
      role="alert"
    >
      {{ $t(problem) }}
    </p>
  </div>
</template>

<style scoped>
.assistant-environment-policy {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.field-error {
  color: var(--color-danger, #b42318);
}
</style>
