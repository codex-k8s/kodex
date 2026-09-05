<script setup lang="ts">
import { computed, ref, watch } from "vue";
import IntegrationPackageField from "./IntegrationPackageField.vue";
import { emptyPackageField, packageSchema } from "./integration-package";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import { providerAccount, providerAccounts } from "./api";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import type { ConfigurationKind } from "./api";
import {
  parseConfigurationDocument,
  serializeConfigurationDocument,
} from "./document";
const props = defineProps<{
  kind: ConfigurationKind;
  modelValue: string;
  name: string;
  format: "JSON" | "YAML";
  disabled?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const parsed = computed(() => {
  try {
    return {
      value: props.modelValue.trim()
        ? parseConfigurationDocument(props.modelValue, props.format)
        : props.kind === "INTEGRATION_DEFINITION"
          ? (emptyPackageField(packageSchema) as Record<string, unknown>)
          : { name: props.name },
      valid: true,
    };
  } catch {
    return { value: {} as Record<string, unknown>, valid: false };
  }
});
function object(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}
function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}
const stt = computed(() => object(parsed.value.value.stt));
const sttParameters = computed(() => object(stt.value.parameters));
function sttList(key: string): string {
  const value = sttParameters.value[key];
  return Array.isArray(value)
    ? value
        .filter((item): item is string => typeof item === "string")
        .join("\n")
    : "";
}
function updateSttParameter(key: string, value: unknown): void {
  write({
    ...parsed.value.value,
    stt: {
      ...stt.value,
      permissionKey: "platform.stt.use",
      parameters: { ...sttParameters.value, [key]: value },
    },
  });
}
function updateSttNumber(key: string, event: Event, parameter = false): void {
  if (
    !(event.target instanceof HTMLInputElement) ||
    !Number.isFinite(event.target.valueAsNumber)
  )
    return;
  if (parameter) updateSttParameter(key, event.target.valueAsNumber);
  else
    write({
      ...parsed.value.value,
      stt: {
        ...stt.value,
        permissionKey: "platform.stt.use",
        [key]: event.target.valueAsNumber,
      },
    });
}
const selectedAccount = ref<AsyncEntityOption>();
watch(
  () => text(stt.value.providerAccountRef),
  async (reference, _previous, onCleanup) => {
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    selectedAccount.value = undefined;
    if (!reference) return;
    try {
      const item = await providerAccount(reference, controller.signal);
      if (!controller.signal.aborted)
        selectedAccount.value = {
          ref: item.ref,
          title: item.name,
          description: item.externalAccountMasked,
        };
    } catch {
      /* Выбранная недоступная identity не заменяется автоматически. */
    }
  },
  { immediate: true },
);
async function loadAccounts(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = await providerAccounts(query, cursor, signal);
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.externalAccountMasked,
      meta: item.state,
      disabled: !item.ready || item.authorization?.method !== "API_KEY",
    })),
    nextPageToken: page.nextPageToken,
  };
}
function selectAccount(option: AsyncEntityOption): void {
  selectedAccount.value = option;
  write({
    ...parsed.value.value,
    stt: {
      ...stt.value,
      providerAccountRef: option.ref,
      permissionKey: "platform.stt.use",
    },
  });
}
function toggleEnabled(event: Event): void {
  if (!(event.target instanceof HTMLInputElement)) return;
  write({
    ...parsed.value.value,
    stt: {
      ...stt.value,
      enabled: event.target.checked,
      permissionKey: "platform.stt.use",
    },
  });
}
const packages = computed(() =>
  Array.isArray(parsed.value.value.packages)
    ? parsed.value.value.packages
        .filter((value): value is string => typeof value === "string")
        .join("\n")
    : "",
);
function write(value: Record<string, unknown>): void {
  if (!props.disabled && parsed.value.valid)
    emit(
      "update:modelValue",
      serializeConfigurationDocument(
        props.kind === "INTEGRATION_DEFINITION"
          ? value
          : { ...value, name: props.name },
        props.format,
      ),
    );
}
function update(key: string, event: Event, group?: "stt"): void {
  if (
    !(
      event.target instanceof HTMLInputElement ||
      event.target instanceof HTMLSelectElement
    )
  )
    return;
  const value = parsed.value.value;
  write(
    group
      ? {
          ...value,
          [group]: {
            ...object(value[group]),
            [key]: event.target.value,
            permissionKey: "platform.stt.use",
          },
        }
      : { ...value, [key]: event.target.value },
  );
}
</script>
<template>
  <p v-if="!parsed.valid" role="alert">{{ $t("managed.invalidDocument") }}</p>
  <fieldset v-else class="configuration-fields" :disabled="disabled">
    <label v-if="kind !== 'INTEGRATION_DEFINITION'"
      >{{ $t("common.description")
      }}<VoiceTextarea
        :disabled="disabled"
        :model-value="text(parsed.value.description)"
        @update:model-value="write({ ...parsed.value, description: $event })"
    /></label>
    <template v-if="kind === 'ROLE_IMAGE'">
      <label
        >{{ $t("managed.baseImage")
        }}<input
          :value="text(parsed.value.baseImage)"
          @input="update('baseImage', $event)"
      /></label>
      <label
        >{{ $t("managed.packages")
        }}<VoiceTextarea
          :disabled="disabled"
          :model-value="packages"
          @update:model-value="
            write({
              ...parsed.value,
              packages: $event
                .split('\n')
                .map((value) => value.trim())
                .filter(Boolean),
            })
          "
      /></label>
    </template>
    <IntegrationPackageField
      v-if="kind === 'INTEGRATION_DEFINITION'"
      :schema="packageSchema"
      :model-value="parsed.value"
      field-key="spec"
      :disabled="disabled"
      @update:model-value="write(object($event))"
    />
    <template v-if="kind === 'SYSTEM_STT'">
      <label class="configuration-fields__toggle">
        <input
          type="checkbox"
          :checked="stt.enabled === true"
          @change="toggleEnabled"
        />
        <span>{{ $t("managed.sttEnabled") }}</span>
      </label>
      <label
        >{{ $t("managed.fields.providerAccountRef")
        }}<AsyncEntityPicker
          :model-value="text(stt.providerAccountRef)"
          :selected="selectedAccount"
          :load-page="loadAccounts"
          :disabled="disabled"
          :placeholder="$t('providers.selectorLabel')"
          @select="selectAccount"
      /></label>
      <label v-for="key in ['model', 'language']" :key="key"
        >{{ $t(`managed.fields.${key}`)
        }}<input :value="text(stt[key])" @input="update(key, $event, 'stt')"
      /></label>
      <label v-for="key in ['languages', 'keywords']" :key="key">
        {{ $t(`managed.sttParameters.${key}`) }}
        <VoiceTextarea
          :model-value="sttList(key)"
          :disabled="disabled"
          rows="3"
          @update:model-value="
            updateSttParameter(key, $event ? $event.split('\n') : [])
          "
        />
      </label>
      <label
        >{{ $t("managed.sttParameters.prompt") }}
        <VoiceTextarea
          :model-value="text(sttParameters.prompt)"
          :disabled="disabled"
          rows="4"
          @update:model-value="updateSttParameter('prompt', $event)"
        />
      </label>
      <label
        >{{ $t("managed.sttParameters.temperature") }}
        <input
          type="number"
          min="0"
          max="1"
          step="0.05"
          :value="sttParameters.temperature ?? 0"
          @input="updateSttNumber('temperature', $event, true)"
        />
      </label>
      <label
        >{{ $t("managed.sttParameters.chunkingStrategy") }}
        <select
          :value="text(sttParameters.chunkingStrategy)"
          @change="
            updateSttParameter(
              'chunkingStrategy',
              ($event.target as HTMLSelectElement).value,
            )
          "
        >
          <option value="">{{ $t("managed.sttParameters.default") }}</option>
          <option value="auto">auto</option>
        </select>
      </label>
      <label class="configuration-fields__toggle"
        ><input
          type="checkbox"
          :checked="sttParameters.stream === true"
          disabled
        /><span>{{ $t("managed.sttParameters.stream") }}</span></label
      >
      <label
        v-for="limit in [
          { key: 'maximumAudioBytes', max: 26214400 },
          { key: 'maximumAudioDurationMilliseconds', max: 600000 },
          { key: 'providerTimeoutMilliseconds', max: 120000 },
        ]"
        :key="limit.key"
      >
        {{ $t(`managed.sttParameters.${limit.key}`) }}
        <input
          type="number"
          min="1"
          :max="limit.max"
          step="1"
          :value="stt[limit.key]"
          @input="updateSttNumber(limit.key, $event)"
        />
      </label>
    </template>
  </fieldset>
</template>
<style scoped>
.configuration-fields {
  margin: 0;
  padding: 0;
  border: 0;
  display: grid;
  gap: 16px;
  min-width: 0;
}
.configuration-fields label {
  display: grid;
  min-width: 0;
  gap: 6px;
}
.configuration-fields .configuration-fields__toggle {
  display: flex;
  align-items: center;
  gap: 8px;
}
.configuration-fields__row {
  display: flex;
  align-items: end;
  flex-wrap: wrap;
  gap: 12px;
}
.configuration-fields__row label {
  flex: 1 1 180px;
}
.configuration-fields__operation {
  display: grid;
  gap: 12px;
  border-top: 1px solid var(--border);
  padding-top: 12px;
}
</style>
