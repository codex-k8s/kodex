<script setup lang="ts">
import { KeyRound, Plus, ShieldCheck, Trash2 } from "@lucide/vue";
import { computed, nextTick, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { safeSecretReference } from "@/features/runtime/environment-capabilities";
import { runtimeEnvironmentCollectionLimit } from "@/features/runtime/environment-form";
import { loadRuntimeSecretPage } from "@/features/runtime-secrets/api";
import {
  maskedSecretHint,
  type RuntimeSecret,
} from "@/features/runtime-secrets/model";
import type {
  RuntimeEnvironmentValue,
  RuntimeSecretBinding,
  RuntimeSecretDescriptor,
} from "@/shared/api/generated/openapi/types.gen";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";

const props = withDefaults(
  defineProps<{
    values: RuntimeEnvironmentValue[];
    secretBindings: RuntimeSecretBinding[];
    projectRef: string;
    disabled: boolean;
    mode?: "VALUES" | "SECRETS" | "BOTH";
    descriptors?: RuntimeSecretDescriptor[];
  }>(),
  { mode: "BOTH" },
);
const emit = defineEmits<{
  "update:values": [value: RuntimeEnvironmentValue[]];
  "update:secretBindings": [value: RuntimeSecretBinding[]];
}>();
const { t } = useI18n();
const root = ref<HTMLElement>();
const selectedSecrets = reactive<Record<string, AsyncEntityOption>>({});

watch(
  () => props.projectRef,
  () => {
    for (const key of Object.keys(selectedSecrets))
      Reflect.deleteProperty(selectedSecrets, key);
  },
);

function changeValue(
  index: number,
  field: "name" | "value",
  value: string,
): void {
  const next = props.values.map((item) => ({ ...item }));
  const item = next[index];
  if (!item) return;
  item[field] = value;
  emit("update:values", next);
}

async function addValue(): Promise<void> {
  if (
    props.disabled ||
    props.values.length >= runtimeEnvironmentCollectionLimit
  )
    return;
  emit("update:values", [...props.values, { name: "", value: "" }]);
  await nextTick();
  const names = root.value?.querySelectorAll<HTMLInputElement>(
    "[data-environment-variable-name]",
  );
  if (names?.length) names.item(names.length - 1).focus();
}

function removeValue(index: number): void {
  emit(
    "update:values",
    props.values.filter((_, current) => current !== index),
  );
}

function changeSecret(
  index: number,
  field: "name" | "secretRef",
  value: string,
): void {
  const next = props.secretBindings.map((item) => ({ ...item }));
  const item = next[index];
  if (!item) return;
  item[field] = value;
  if (field === "secretRef") item.revision = 0;
  emit("update:secretBindings", next);
}

async function addSecret(): Promise<void> {
  if (
    props.disabled ||
    props.secretBindings.length >= runtimeEnvironmentCollectionLimit
  )
    return;
  emit("update:secretBindings", [
    ...props.secretBindings,
    { name: "", secretRef: "", revision: 0 },
  ]);
  await nextTick();
  const names = root.value?.querySelectorAll<HTMLInputElement>(
    "[data-environment-secret-name]",
  );
  if (names?.length) names.item(names.length - 1).focus();
}

function removeSecret(index: number): void {
  emit(
    "update:secretBindings",
    props.secretBindings.filter((_, current) => current !== index),
  );
}

async function loadSecretPage(
  query: string,
  cursor?: string,
  signal?: AbortSignal,
  pageSize = 20,
) {
  const page = await loadRuntimeSecretPage(
    props.projectRef,
    query,
    cursor,
    signal,
    pageSize,
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

function currentDescriptor(
  binding: RuntimeSecretBinding,
): RuntimeSecretDescriptor | undefined {
  return props.descriptors?.find(
    (descriptor) =>
      descriptor.name === binding.name &&
      descriptor.secretRef === binding.secretRef,
  );
}

function selectedSecret(
  binding: RuntimeSecretBinding,
): AsyncEntityOption | undefined {
  if (!binding.secretRef) return undefined;
  if (selectedSecrets[binding.secretRef])
    return selectedSecrets[binding.secretRef];
  const descriptor = currentDescriptor(binding);
  return descriptor
    ? {
        ref: descriptor.secretRef,
        title:
          [descriptor.secretName, descriptor.secretKey]
            .filter(Boolean)
            .join(" / ") || binding.name,
        description: t("runtime.currentPublishedSecret"),
        meta: `rev ${descriptor.secretResourceVersion}`,
      }
    : {
        ref: binding.secretRef,
        title: binding.secretRef,
        description: t("runtime.restoredSecretSelection"),
      };
}

function selectSecret(index: number, option: AsyncEntityOption): void {
  selectedSecrets[option.ref] = option;
  changeSecret(index, "secretRef", option.ref);
}
</script>

<template>
  <div ref="root" class="runtime-environment-field-lists">
    <section
      v-if="mode !== 'SECRETS'"
      class="runtime-environment-field-lists__section"
    >
      <div class="section-header">
        <div>
          <h2>{{ $t("runtime.variables") }}</h2>
          <p>{{ $t("runtime.variablesHelp") }}</p>
        </div>
        <button
          class="button"
          type="button"
          :disabled="
            disabled || values.length >= runtimeEnvironmentCollectionLimit
          "
          :title="
            values.length >= runtimeEnvironmentCollectionLimit
              ? $t('runtime.errors.collectionLimit')
              : undefined
          "
          @click="addValue"
        >
          <Plus :size="15" aria-hidden="true" />{{ $t("runtime.addVariable") }}
        </button>
      </div>
      <div v-if="values.length" class="environment-fields">
        <div
          v-for="(item, index) in values"
          :key="index"
          class="environment-field-row"
        >
          <label class="field"
            ><span>{{ $t("runtime.variableName") }}</span
            ><input
              :value="item.name"
              :name="`runtime-public-value-name-${index}`"
              data-environment-variable-name
              placeholder="VAR_NAME"
              :disabled="disabled"
              @input="
                changeValue(
                  index,
                  'name',
                  ($event.target as HTMLInputElement).value,
                )
              "
          /></label>
          <label class="field"
            ><span>{{ $t("runtime.nonSecretValue") }}</span
            ><input
              :value="item.value"
              :name="`runtime-public-value-${index}`"
              maxlength="8192"
              :disabled="disabled"
              @input="
                changeValue(
                  index,
                  'value',
                  ($event.target as HTMLInputElement).value,
                )
              "
          /></label>
          <button
            class="icon-button icon-button--danger"
            type="button"
            :disabled="disabled"
            :aria-label="$t('common.delete')"
            @click="removeValue(index)"
          >
            <Trash2 :size="16" aria-hidden="true" />
          </button>
        </div>
      </div>
      <p v-else class="secondary-text">{{ $t("common.empty") }}</p>
    </section>
    <section
      v-if="mode !== 'VALUES'"
      class="runtime-environment-field-lists__section"
    >
      <div class="section-header">
        <div>
          <h2>{{ $t("runtime.secretReferences") }}</h2>
          <p>{{ $t("runtime.secretBindingsHelp") }}</p>
        </div>
        <button
          class="button"
          type="button"
          :disabled="
            disabled ||
            secretBindings.length >= runtimeEnvironmentCollectionLimit
          "
          :title="
            secretBindings.length >= runtimeEnvironmentCollectionLimit
              ? $t('runtime.errors.collectionLimit')
              : undefined
          "
          @click="addSecret"
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
      <article
        v-for="(item, index) in secretBindings"
        :key="index"
        class="secret-descriptor"
      >
        <div class="section-header">
          <div>
            <strong>{{
              item.name || $t("runtime.secretBinding", { number: index + 1 })
            }}</strong>
            <p>
              {{
                selectedSecret(item)?.title || $t("runtime.secretNotSelected")
              }}
            </p>
          </div>
          <button
            class="icon-button icon-button--danger"
            type="button"
            :disabled="disabled"
            :aria-label="$t('common.delete')"
            @click="removeSecret(index)"
          >
            <Trash2 :size="16" aria-hidden="true" />
          </button>
        </div>
        <div class="secret-binding-fields">
          <label class="field"
            ><span>{{ $t("runtime.variableName") }}</span
            ><input
              :value="item.name"
              :name="`runtime-secret-binding-name-${index}`"
              data-environment-secret-name
              placeholder="SECRET_NAME"
              :disabled="disabled"
              @input="
                changeSecret(
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
                changeSecret(
                  index,
                  'secretRef',
                  typeof $event === 'string' ? $event : '',
                )
              "
              @select="selectSecret(index, $event)"
            />
          </div>
        </div>
        <dl
          v-if="currentDescriptor(item)"
          class="secret-safe-meta"
          :aria-label="$t('runtime.currentImmutableDescriptor')"
        >
          <div>
            <dt>{{ $t("runtime.secretTarget") }}</dt>
            <dd>{{ safeSecretReference(currentDescriptor(item)!).target }}</dd>
          </div>
          <div>
            <dt>{{ $t("runtime.secretResourceVersion") }}</dt>
            <dd>
              {{ safeSecretReference(currentDescriptor(item)!).revision }}
            </dd>
          </div>
          <div>
            <dt>UID</dt>
            <dd>
              <code>{{
                safeSecretReference(currentDescriptor(item)!).uidHint
              }}</code>
            </dd>
          </div>
          <div>
            <dt>SHA-256</dt>
            <dd>
              <code>{{
                safeSecretReference(currentDescriptor(item)!).digestHint
              }}</code>
            </dd>
          </div>
        </dl>
        <p v-else class="secondary-text">
          {{ $t("runtime.descriptorGeneratedOnPublish") }}
        </p>
      </article>
      <p v-if="!secretBindings.length" class="secondary-text">
        {{ $t("runtime.noSecretReferences") }}
      </p>
    </section>
  </div>
</template>

<style scoped>
.runtime-environment-field-lists {
  display: grid;
  gap: 18px;
  min-width: 0;
}
.runtime-environment-field-lists__section {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.section-header {
  display: flex;
  justify-content: space-between;
  align-items: start;
  gap: 12px;
}
.section-header h2 {
  margin: 0 0 4px;
  font-size: 1rem;
}
.section-header p {
  margin: 0;
  color: var(--text-muted, #59677a);
}
.environment-fields,
.secret-descriptor {
  display: grid;
  gap: 12px;
}
.environment-field-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.5fr) auto;
  align-items: end;
  gap: 10px;
}
.secret-binding-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.secret-descriptor {
  padding: 12px;
  border: 1px solid var(--border, #d5dfed);
  border-radius: 8px;
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
.secret-safe-meta {
  display: grid;
  gap: 6px;
  margin: 0;
}
.secret-safe-meta div {
  display: flex;
  gap: 8px;
}
.secret-safe-meta dt {
  color: var(--text-muted, #59677a);
}
.secret-safe-meta dd {
  margin: 0;
  overflow-wrap: anywhere;
}
@media (max-width: 720px) {
  .environment-field-row,
  .secret-binding-fields {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
