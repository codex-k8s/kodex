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
import RuntimeEnvironmentFieldListsEditor from "@/features/runtime/RuntimeEnvironmentFieldListsEditor.vue";
import type {
  RuntimeEnvironmentValue,
  RuntimeSecretBinding,
} from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  operation: EditablePlanOperation;
  projectRef: string;
  disabled: boolean;
}>();
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: unknown];
}>();
const sensitiveName = /SECRET|PASSWORD|TOKEN|CREDENTIAL|PRIVATE_KEY|API_KEY/;
const secretRef = /^sec_[A-Za-z0-9_-]{4,92}$/;

function object(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function snapshotItems(key: "Values" | "SecretBindings"): unknown {
  return object(props.operation.value.before.specification)?.[key];
}

function proposedItems(key: "publicValues" | "secretBindings"): unknown {
  try {
    const proposed = operationParameter(props.operation, key);
    return proposed === undefined
      ? snapshotItems(key === "publicValues" ? "Values" : "SecretBindings")
      : proposed;
  } catch {
    return undefined;
  }
}

function decodeValues(raw: unknown): RuntimeEnvironmentValue[] | undefined {
  if (raw === undefined || raw === null) return [];
  if (!Array.isArray(raw) || raw.length > 128) return undefined;
  const result: RuntimeEnvironmentValue[] = [];
  for (const entry of raw) {
    const item = object(entry);
    if (
      !item ||
      Object.keys(item).some(
        (key) => !["name", "value", "Name", "Value"].includes(key),
      )
    )
      return undefined;
    const name = item.name ?? item.Name;
    const value = item.value ?? item.Value;
    if (typeof name !== "string" || typeof value !== "string") return undefined;
    result.push({ name, value });
  }
  return result;
}

function decodeBindings(raw: unknown): RuntimeSecretBinding[] | undefined {
  if (raw === undefined || raw === null) return [];
  if (!Array.isArray(raw) || raw.length > 128) return undefined;
  const result: RuntimeSecretBinding[] = [];
  for (const entry of raw) {
    const item = object(entry);
    if (
      !item ||
      Object.keys(item).some(
        (key) =>
          ![
            "name",
            "secretRef",
            "revision",
            "Name",
            "SecretRef",
            "Revision",
          ].includes(key),
      )
    )
      return undefined;
    const name = item.name ?? item.Name;
    const ref = item.secretRef ?? item.SecretRef;
    const revision = item.revision ?? item.Revision;
    if (
      typeof name !== "string" ||
      typeof ref !== "string" ||
      (revision !== undefined &&
        (typeof revision !== "number" || !Number.isSafeInteger(revision)))
    )
      return undefined;
    result.push({
      name,
      secretRef: ref,
      ...(revision === undefined ? {} : { revision }),
    });
  }
  return result;
}

const values = computed(() => decodeValues(proposedItems("publicValues")));
const bindings = computed(() =>
  decodeBindings(proposedItems("secretBindings")),
);
const problems = computed(() => {
  if (!values.value || !bindings.value) return ["runtime.errors.variableName"];
  const result = validateEnvironmentInput({
    name: "Assistant draft",
    description: "",
    imageArtifactRef: "imgart_example",
    tools: [],
    values: values.value,
    secretBindings: bindings.value,
    policy: defaultRuntimeEnvironmentPolicy(),
  })
    .filter(
      (problem) =>
        problem.field.startsWith("values") ||
        problem.field.startsWith("secretBindings"),
    )
    .map((problem) => problem.message);
  if (
    values.value.some(
      (item) => sensitiveName.test(item.name) || item.value.length > 8192,
    )
  )
    result.push("runtime.secretValuesForbidden");
  if (bindings.value.some((item) => !secretRef.test(item.secretRef)))
    result.push("runtime.errors.secretBindingRequired");
  return [...new Set(result)];
});
watch(
  () => problems.value.length === 0,
  (valid) => emit("valid", valid),
  { immediate: true },
);

function updateValues(next: RuntimeEnvironmentValue[]): void {
  emit("parameter", "publicValues", next);
  emit("dirty");
}

function updateBindings(next: RuntimeSecretBinding[]): void {
  emit("parameter", "secretBindings", next);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-environment-fields">
    <RuntimeEnvironmentFieldListsEditor
      :values="values ?? []"
      :secret-bindings="bindings ?? []"
      :project-ref="projectRef"
      :disabled="disabled || !values || !bindings"
      @update:values="updateValues"
      @update:secret-bindings="updateBindings"
    />
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
.assistant-environment-fields {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.field-error {
  color: var(--color-danger, #b42318);
}
</style>
