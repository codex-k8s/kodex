<script setup lang="ts">
import { KeyRound, Plus, ShieldCheck, Trash2 } from "@lucide/vue";
import { computed, reactive, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  operationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import {
  defaultRuntimeEnvironmentPolicy,
  runtimeEnvironmentCollectionLimit,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";
import { loadRuntimeSecretPage } from "@/features/runtime-secrets/api";
import {
  maskedSecretHint,
  type RuntimeSecret,
} from "@/features/runtime-secrets/model";
import type {
  RuntimeEnvironmentValue,
  RuntimeSecretBinding,
} from "@/shared/api/generated/openapi/types.gen";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";

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
const { t } = useI18n();
const selectedSecrets = reactive<Record<string, AsyncEntityOption>>({});
const variableName = /^[A-Z_][A-Z0-9_]{0,126}$/;
const secretRef = /^sec_[A-Za-z0-9_-]{4,92}$/;
const sensitiveName = /SECRET|PASSWORD|TOKEN|CREDENTIAL|PRIVATE_KEY|API_KEY/;

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
  if (!Array.isArray(raw) || raw.length > runtimeEnvironmentCollectionLimit)
    return undefined;
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
  if (!Array.isArray(raw) || raw.length > runtimeEnvironmentCollectionLimit)
    return undefined;
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
watch(
  () => props.projectRef,
  () => {
    for (const key of Object.keys(selectedSecrets))
      Reflect.deleteProperty(selectedSecrets, key);
  },
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
  if (
    values.value.some((item) => !variableName.test(item.name)) ||
    bindings.value.some((item) => !variableName.test(item.name))
  )
    result.push("runtime.errors.variableName");
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

function setValue(index: number, field: "name" | "value", event: Event): void {
  if (!values.value) return;
  const next = values.value.map((item) => ({ ...item }));
  const item = next[index];
  if (!item) return;
  item[field] = (event.target as HTMLInputElement).value;
  updateValues(next);
}

function setBinding(
  index: number,
  field: "name" | "secretRef",
  value: string,
): void {
  if (!bindings.value) return;
  const next = bindings.value.map((item) => ({ ...item }));
  const item = next[index];
  if (!item) return;
  item[field] = value;
  if (field === "secretRef") item.revision = 0;
  updateBindings(next);
}

async function loadSecretPage(
  query: string,
  cursor?: string,
  signal?: AbortSignal,
) {
  const page = await loadRuntimeSecretPage(
    props.projectRef,
    query,
    cursor,
    signal,
  );
  return {
    items: page.items.map(
      (secret: RuntimeSecret): AsyncEntityOption => ({
        ref: secret.ref,
        title: secret.name,
        description: secret.description,
        meta: `${maskedSecretHint(secret)} · rev ${String(secret.currentRevision)}`,
        disabled: secret.state !== "ACTIVE",
        disabledReason:
          secret.state === "ACTIVE" ? undefined : t("runtime.secretRevoked"),
      }),
    ),
    nextPageToken: page.nextPageToken || undefined,
  };
}

const secretPickerLabels = computed(() => ({
  label: t("runtime.chooseRuntimeSecret"),
  searchPlaceholder: t("runtime.searchRuntimeSecret"),
  loading: t("runtime.secretPicker.loading"),
  loadingMore: t("runtime.secretPicker.loadingMore"),
  empty: t("runtime.secretPicker.empty"),
  error: t("runtime.secretPicker.error"),
  retry: t("common.retry"),
}));

function selectedSecret(
  binding: RuntimeSecretBinding,
): AsyncEntityOption | undefined {
  return (
    selectedSecrets[binding.secretRef] ??
    (binding.secretRef
      ? { ref: binding.secretRef, title: binding.secretRef }
      : undefined)
  );
}

function selectSecret(index: number, option: AsyncEntityOption): void {
  selectedSecrets[option.ref] = option;
  setBinding(index, "secretRef", option.ref);
}
</script>

<template>
  <div class="assistant-environment-fields">
    <section>
      <div class="section-header">
        <div>
          <h3>{{ $t("runtime.variables") }}</h3>
          <p>{{ $t("runtime.variablesHelp") }}</p>
        </div>
        <button
          class="button"
          type="button"
          :disabled="
            disabled ||
            !values ||
            values.length >= runtimeEnvironmentCollectionLimit
          "
          @click="updateValues([...(values ?? []), { name: '', value: '' }])"
        >
          <Plus :size="15" aria-hidden="true" />{{ $t("runtime.addVariable") }}
        </button>
      </div>
      <div
        v-for="(item, index) in values ?? []"
        :key="index"
        class="environment-field-row"
      >
        <label class="field"
          ><span>{{ $t("runtime.variableName") }}</span
          ><input
            :value="item.name"
            placeholder="VAR_NAME"
            :disabled="disabled"
            @input="setValue(index, 'name', $event)"
        /></label>
        <label class="field"
          ><span>{{ $t("runtime.nonSecretValue") }}</span
          ><input
            :value="item.value"
            maxlength="8192"
            :disabled="disabled"
            @input="setValue(index, 'value', $event)"
        /></label>
        <button
          class="icon-button icon-button--danger"
          type="button"
          :disabled="disabled"
          :aria-label="$t('common.delete')"
          @click="
            updateValues(
              (values ?? []).filter((_, itemIndex) => itemIndex !== index),
            )
          "
        >
          <Trash2 :size="16" aria-hidden="true" />
        </button>
      </div>
      <p v-if="!values?.length" class="secondary-text">
        {{ $t("common.empty") }}
      </p>
    </section>
    <section>
      <div class="section-header">
        <div>
          <h3>{{ $t("runtime.secretReferences") }}</h3>
          <p>{{ $t("runtime.secretBindingsHelp") }}</p>
        </div>
        <button
          class="button"
          type="button"
          :disabled="
            disabled ||
            !bindings ||
            bindings.length >= runtimeEnvironmentCollectionLimit
          "
          @click="
            updateBindings([
              ...(bindings ?? []),
              { name: '', secretRef: '', revision: 0 },
            ])
          "
        >
          <KeyRound :size="15" aria-hidden="true" />{{
            $t("runtime.addSecretBinding")
          }}
        </button>
      </div>
      <div class="secret-warning" role="note">
        <ShieldCheck :size="18" aria-hidden="true" />{{
          $t("runtime.secretValuesForbidden")
        }}
      </div>
      <div
        v-for="(item, index) in bindings ?? []"
        :key="index"
        class="secret-binding-fields"
      >
        <label class="field"
          ><span>{{ $t("runtime.variableName") }}</span
          ><input
            :value="item.name"
            placeholder="SECRET_NAME"
            :disabled="disabled"
            @input="
              setBinding(
                index,
                'name',
                ($event.target as HTMLInputElement).value,
              )
            "
        /></label>
        <div class="field">
          <span>{{ $t("runtime.runtimeSecret") }}</span
          ><AsyncEntityPicker
            :model-value="item.secretRef"
            :selected="selectedSecret(item)"
            :load-page="loadSecretPage"
            :labels="secretPickerLabels"
            :placeholder="$t('runtime.chooseRuntimeSecret')"
            :search-placeholder="$t('runtime.searchRuntimeSecret')"
            :disabled="disabled || !projectRef"
            @update:model-value="
              setBinding(
                index,
                'secretRef',
                typeof $event === 'string' ? $event : '',
              )
            "
            @select="selectSecret(index, $event)"
          />
        </div>
        <button
          class="icon-button icon-button--danger"
          type="button"
          :disabled="disabled"
          :aria-label="$t('common.delete')"
          @click="
            updateBindings(
              (bindings ?? []).filter((_, itemIndex) => itemIndex !== index),
            )
          "
        >
          <Trash2 :size="16" aria-hidden="true" />
        </button>
      </div>
      <p v-if="!bindings?.length" class="secondary-text">
        {{ $t("runtime.noSecretReferences") }}
      </p>
    </section>
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
  gap: 20px;
}
.assistant-environment-fields section {
  display: grid;
  gap: 12px;
}
.section-header {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: 12px;
}
.section-header h3 {
  margin: 0 0 4px;
}
.section-header p {
  margin: 0;
  color: var(--text-muted, #59677a);
}
.environment-field-row,
.secret-binding-fields {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.5fr) auto;
  align-items: end;
  gap: 10px;
}
.field {
  display: grid;
  gap: 5px;
  min-width: 0;
}
.field input {
  width: 100%;
  min-width: 0;
}
.secret-warning {
  display: flex;
  align-items: center;
  gap: 8px;
}
.field-error {
  color: var(--color-danger, #b42318);
}
@media (max-width: 720px) {
  .environment-field-row,
  .secret-binding-fields {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
