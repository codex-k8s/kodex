<script setup lang="ts">
import {
  computed,
  onMounted,
  onScopeDispose,
  ref,
  shallowRef,
  useId,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import type { SttModelCatalog } from "@/shared/api/generated/openapi/types.gen";
import {
  readSttCatalog,
  sttFormLimits,
  sttParameterSupported,
  sttChunkingStrategies,
} from "./stt-model-catalog";
import IntegrationPackageField from "./IntegrationPackageField.vue";
import { emptyPackageField, packageSchema } from "./integration-package";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import { providerAccount, providerAccounts } from "./api";
import {
  additionalSttLanguages,
  primarySttLanguage,
  setAdditionalSttLanguages,
  setPrimarySttLanguage,
} from "./stt-language";
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
  initializeStt?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const translator = useI18n();
const { t } = translator;
const fieldPrefix = `configuration-fields-${useId()}`;
const catalog = shallowRef<SttModelCatalog>();
const catalogFailed = ref(false);
const catalogScope = new AbortController();
let catalogGeneration = 0;
let initializedStt = false;
onScopeDispose(() => catalogScope.abort());
const modelProfile = computed(() =>
  catalog.value?.models.find((item) => item.model === text(stt.value.model)),
);
const selectedModel = computed<AsyncEntityOption | undefined>(() =>
  text(stt.value.model)
    ? {
        ref: text(stt.value.model),
        title: text(stt.value.model),
      }
    : undefined,
);
const chunkingStrategies = computed(() =>
  sttChunkingStrategies(modelProfile.value),
);
function supported(key: string): boolean {
  return sttParameterSupported(modelProfile.value, key);
}
async function loadModels(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  const generation = ++catalogGeneration;
  const combined = AbortSignal.any([signal, catalogScope.signal]);
  try {
    const result = await readSttCatalog(combined);
    if (!combined.aborted && generation === catalogGeneration) {
      catalog.value = result;
      catalogFailed.value = false;
      const initialize =
        props.initializeStt && !initializedStt && !props.modelValue.trim();
      initializedStt = true;
      if (initialize) {
        const recommended = result.models.find(
          (item) => item.model === result.recommendedModel,
        );
        write({
          stt: {
            enabled: false,
            model: result.recommendedModel,
            language: sttParameterSupported(recommended, "language")
              ? "ru"
              : "",
            permissionKey: "platform.stt.use",
            parameters: {
              languages: sttParameterSupported(recommended, "languages")
                ? ["ru"]
                : [],
              keywords: [],
              prompt: "",
              temperature: 0,
              chunkingStrategy: "",
              stream: false,
            },
            maximumAudioBytes: result.recommendedMaximumAudioBytes,
            maximumAudioDurationMilliseconds:
              result.recommendedMaximumAudioDurationMilliseconds,
          },
        });
      }
    }
    const filtered = result.models.filter((item) =>
      item.model.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
    );
    const parsedOffset = Number.parseInt(cursor ?? "0", 10);
    const offset =
      Number.isSafeInteger(parsedOffset) && parsedOffset >= 0
        ? parsedOffset
        : 0;
    const items = filtered.slice(offset, offset + pageSize);
    const nextOffset = offset + items.length;
    return {
      items: items.map((item) => ({
        ref: item.model,
        title: item.model,
        meta: [
          item.model === result.recommendedModel
            ? t("managed.sttCatalog.recommended")
            : "",
          item.legacy ? t("managed.sttCatalog.legacy") : "",
        ]
          .filter(Boolean)
          .join(" · "),
      })),
      ...(nextOffset < filtered.length
        ? { nextPageToken: String(nextOffset) }
        : {}),
    };
  } catch (error) {
    if (!combined.aborted && generation === catalogGeneration)
      catalogFailed.value = true;
    throw error;
  }
}
async function refreshCatalog(): Promise<void> {
  try {
    await loadModels("", undefined, catalogScope.signal);
  } catch {
    /* Ошибка чтения показана отдельно от сохраненного документа. */
  }
}
onMounted(() => {
  if (props.kind === "SYSTEM_STT") void refreshCatalog();
});
watch(
  () => props.kind,
  (kind) => {
    if (kind === "SYSTEM_STT") void refreshCatalog();
  },
);
function selectModel(option: AsyncEntityOption): void {
  const profile = catalog.value?.models.find(
    (item) => item.model === option.ref,
  );
  if (!profile) return;
  const multiple = sttParameterSupported(profile, "languages");
  const language = primarySttLanguage(stt.value, multipleLanguages());
  write({
    ...parsed.value.value,
    stt: {
      ...setPrimarySttLanguage(stt.value, multiple, language),
      model: option.ref,
      permissionKey: "platform.stt.use",
    },
  });
}
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
const standardLanguageCodes = ["ru", "en", "de", "es", "fr"] as const;
const customLanguageOpen = ref(false);
const primaryLanguageCode = computed(() =>
  primarySttLanguage(stt.value, multipleLanguages()),
);
const languageChoice = computed(() =>
  customLanguageOpen.value ||
  (primaryLanguageCode.value &&
    !standardLanguageCodes.some((code) => code === primaryLanguageCode.value))
    ? "custom"
    : primaryLanguageCode.value,
);
function multipleLanguages(): boolean {
  return modelProfile.value
    ? supported("languages")
    : Array.isArray(sttParameters.value.languages) &&
        sttParameters.value.languages.length > 0;
}
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
function setLanguage(code: string): void {
  write({
    ...parsed.value.value,
    stt: {
      ...setPrimarySttLanguage(stt.value, multipleLanguages(), code),
      permissionKey: "platform.stt.use",
    },
  });
}
function selectLanguage(event: Event): void {
  if (!(event.target instanceof HTMLSelectElement)) return;
  customLanguageOpen.value = event.target.value === "custom";
  if (!customLanguageOpen.value) setLanguage(event.target.value);
}
function updateLanguage(event: Event): void {
  if (event.target instanceof HTMLInputElement) setLanguage(event.target.value);
}
function updateAdditionalLanguages(value: string): void {
  write({
    ...parsed.value.value,
    stt: {
      ...setAdditionalSttLanguages(stt.value, value),
      permissionKey: "platform.stt.use",
    },
  });
}
function updateSttNumber(key: string, event: Event, parameter = false): void {
  if (!(event.target instanceof HTMLInputElement)) return;
  const fields = { ...(parameter ? sttParameters.value : stt.value) };
  if (event.target.value === "") Reflect.deleteProperty(fields, key);
  else if (Number.isFinite(event.target.valueAsNumber))
    fields[key] = event.target.valueAsNumber;
  else return;
  write({
    ...parsed.value.value,
    stt: {
      ...(parameter ? { ...stt.value, parameters: fields } : fields),
      permissionKey: "platform.stt.use",
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
  pageSize = 30,
): Promise<AsyncEntityOptionPage> {
  const page = await providerAccounts(query, cursor, signal, pageSize);
  return {
    items: page.items.map((item) => {
      const apiKey = item.authorization?.method === "API_KEY";
      const stateKey = `states.${item.state}`;
      return {
        ref: item.ref,
        title: item.name,
        description: apiKey ? item.externalAccountMasked : undefined,
        meta: apiKey
          ? translator.te(stateKey)
            ? t(stateKey)
            : t("common.unknownStatus")
          : undefined,
        disabled: !item.ready || !apiKey,
        disabledReason: !apiKey
          ? t("managed.sttCatalog.apiKeyShort")
          : !item.ready
            ? t("managed.sttCatalog.accountNotReady")
            : undefined,
      };
    }),
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
  <fieldset
    v-else
    class="configuration-fields"
    :class="{ 'configuration-fields--stt': kind === 'SYSTEM_STT' }"
    :disabled="disabled"
  >
    <label v-if="kind !== 'INTEGRATION_DEFINITION' && kind !== 'SYSTEM_STT'"
      >{{ $t("common.description")
      }}<VoiceTextarea
        :id="`${fieldPrefix}-description`"
        :name="`${fieldPrefix}-description`"
        :disabled="disabled"
        :model-value="text(parsed.value.description)"
        @update:model-value="write({ ...parsed.value, description: $event })"
    /></label>
    <template v-if="kind === 'ROLE_IMAGE'">
      <label
        >{{ $t("managed.baseImage")
        }}<input
          :id="`${fieldPrefix}-base-image`"
          :name="`${fieldPrefix}-base-image`"
          :value="text(parsed.value.baseImage)"
          @input="update('baseImage', $event)"
      /></label>
      <label
        >{{ $t("managed.packages")
        }}<VoiceTextarea
          :id="`${fieldPrefix}-packages`"
          :name="`${fieldPrefix}-packages`"
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
          :id="`${fieldPrefix}-stt-enabled`"
          :name="`${fieldPrefix}-stt-enabled`"
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
          :clearable="false"
          :disabled="disabled"
          :placeholder="$t('providers.selectorLabel')"
          @select="selectAccount"
      /></label>
      <label
        >{{ $t("managed.fields.model")
        }}<AsyncEntityPicker
          :model-value="text(stt.model)"
          :selected="selectedModel"
          :load-page="loadModels"
          :clearable="false"
          :disabled="disabled"
          :placeholder="$t('managed.fields.model')"
          @select="selectModel"
      /></label>
      <p v-if="catalogFailed" role="alert">
        {{ $t("managed.sttCatalog.failed") }}
        <button type="button" @click="refreshCatalog">
          {{ $t("common.retry") }}
        </button>
      </p>
      <p v-if="!modelProfile">{{ $t("managed.sttCatalog.unconfirmed") }}</p>
      <label
        >{{ $t("managed.sttParameters.primaryLanguage")
        }}<select
          :id="`${fieldPrefix}-language`"
          :name="`${fieldPrefix}-language`"
          :value="languageChoice"
          @change="selectLanguage"
        >
          <option value="">
            {{ $t("managed.sttParameters.autoLanguage") }}
          </option>
          <option
            v-for="code in standardLanguageCodes"
            :key="code"
            :value="code"
          >
            {{ $t(`managed.sttParameters.language${code.toUpperCase()}`) }}
          </option>
          <option value="custom">
            {{ $t("managed.sttParameters.customLanguage") }}
          </option>
        </select></label
      >
      <label v-if="languageChoice === 'custom'">
        {{ $t("managed.sttParameters.customLanguage") }}
        <small>{{ $t("managed.sttParameters.primaryLanguageHint") }}</small>
        <input
          :id="`${fieldPrefix}-custom-language`"
          :name="`${fieldPrefix}-custom-language`"
          :value="primaryLanguageCode"
          maxlength="2"
          pattern="[a-z]{2}"
          placeholder="pt"
          @input="updateLanguage"
        />
      </label>
      <details class="configuration-fields__advanced">
        <summary>{{ $t("managed.sttParameters.advanced") }}</summary>
        <div class="configuration-fields__advanced-content">
          <label class="configuration-fields__wide"
            >{{ $t("common.description") }}
            <VoiceTextarea
              :id="`${fieldPrefix}-description`"
              :name="`${fieldPrefix}-description`"
              :disabled="disabled"
              :model-value="text(parsed.value.description)"
              @update:model-value="
                write({ ...parsed.value, description: $event })
              "
            />
          </label>
          <p v-if="catalog" class="configuration-fields__note">
            {{
              $t("managed.sttCatalog.metadata", {
                version: catalog.version,
                observedAt: catalog.observedAt,
              })
            }}
          </p>
          <p v-if="catalog" class="configuration-fields__note">
            {{
              $t("managed.sttCatalog.recommendations", {
                model: catalog.recommendedModel,
                bytes: catalog.recommendedMaximumAudioBytes,
                milliseconds:
                  catalog.recommendedMaximumAudioDurationMilliseconds,
              })
            }}
          </p>
          <label v-if="supported('languages') || additionalSttLanguages(stt)">
            {{ $t("managed.sttParameters.additionalLanguages") }}
            <small>{{
              $t("managed.sttParameters.additionalLanguagesHint")
            }}</small>
            <VoiceTextarea
              :id="`${fieldPrefix}-additional-languages`"
              :name="`${fieldPrefix}-additional-languages`"
              :model-value="additionalSttLanguages(stt)"
              :disabled="disabled"
              rows="2"
              @update:model-value="updateAdditionalLanguages"
            />
          </label>
          <label>
            {{ $t("managed.sttParameters.keywords") }}
            <small v-if="!supported('keywords')">{{
              $t("managed.sttCatalog.parameterUnconfirmed")
            }}</small>
            <small v-else-if="modelProfile">{{
              $t("managed.sttCatalog.keywordBounds", {
                count: modelProfile.maximumKeywords,
                bytes: modelProfile.maximumKeywordBytes,
              })
            }}</small>
            <VoiceTextarea
              :id="`${fieldPrefix}-keywords`"
              :name="`${fieldPrefix}-keywords`"
              :model-value="sttList('keywords')"
              :disabled="disabled"
              rows="3"
              @update:model-value="
                updateSttParameter('keywords', $event ? $event.split('\n') : [])
              "
            />
          </label>
          <label class="configuration-fields__wide"
            >{{ $t("managed.sttParameters.prompt") }}
            <small v-if="modelProfile">{{
              $t("managed.sttCatalog.promptBytes", {
                count: modelProfile.maximumPromptBytes,
              })
            }}</small>
            <VoiceTextarea
              :id="`${fieldPrefix}-prompt`"
              :name="`${fieldPrefix}-prompt`"
              :model-value="text(sttParameters.prompt)"
              :disabled="disabled"
              rows="4"
              @update:model-value="updateSttParameter('prompt', $event)"
            />
          </label>
          <label
            >{{ $t("managed.sttParameters.temperature") }}
            <input
              :id="`${fieldPrefix}-temperature`"
              :name="`${fieldPrefix}-temperature`"
              type="number"
              :min="Math.max(0, modelProfile?.minimumTemperature ?? 0)"
              :max="Math.min(1, modelProfile?.maximumTemperature ?? 1)"
              step="0.05"
              :value="sttParameters.temperature"
              @input="updateSttNumber('temperature', $event, true)"
            />
          </label>
          <label
            >{{ $t("managed.sttParameters.chunkingStrategy") }}
            <select
              :id="`${fieldPrefix}-chunking-strategy`"
              :name="`${fieldPrefix}-chunking-strategy`"
              :value="text(sttParameters.chunkingStrategy)"
              @change="
                updateSttParameter(
                  'chunkingStrategy',
                  ($event.target as HTMLSelectElement).value,
                )
              "
            >
              <option
                v-if="
                  !chunkingStrategies.includes(
                    text(sttParameters.chunkingStrategy),
                  )
                "
                :value="text(sttParameters.chunkingStrategy)"
                disabled
              >
                {{
                  text(sttParameters.chunkingStrategy) ||
                  $t("managed.sttParameters.default")
                }}
              </option>
              <option
                v-for="strategy in chunkingStrategies"
                :key="strategy"
                :value="strategy"
              >
                {{ strategy || $t("managed.sttParameters.default") }}
              </option>
            </select>
          </label>
          <label class="configuration-fields__toggle configuration-fields__wide"
            ><input
              :id="`${fieldPrefix}-stream`"
              :name="`${fieldPrefix}-stream`"
              type="checkbox"
              :checked="sttParameters.stream === true"
              disabled
            /><span>{{ $t("managed.sttParameters.stream") }}</span></label
          >
          <div class="configuration-fields__limits">
            <label v-for="limit in sttFormLimits" :key="limit.key">
              {{ $t(`managed.sttParameters.${limit.key}`) }}
              <input
                :id="`${fieldPrefix}-${limit.key}`"
                :name="`${fieldPrefix}-${limit.key}`"
                type="number"
                :min="limit.min"
                :max="limit.max"
                step="1"
                :value="stt[limit.key]"
                @input="updateSttNumber(limit.key, $event)"
              />
            </label>
          </div>
        </div>
      </details>
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
.configuration-fields--stt {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
}
.configuration-fields--stt > .configuration-fields__toggle,
.configuration-fields--stt > .configuration-fields__advanced,
.configuration-fields--stt > p {
  grid-column: 1 / -1;
}
.configuration-fields p,
.configuration-fields small {
  overflow-wrap: anywhere;
}
.configuration-fields .configuration-fields__toggle {
  display: flex;
  align-items: center;
  gap: 8px;
}
.configuration-fields__advanced {
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface, #fff);
}
.configuration-fields__advanced summary {
  cursor: pointer;
  padding: 12px 14px;
  font-weight: 600;
}
.configuration-fields__advanced[open] summary {
  border-bottom: 1px solid var(--border);
}
.configuration-fields__advanced-content {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  min-width: 0;
  padding: 16px;
}
.configuration-fields__advanced-content > .configuration-fields__wide,
.configuration-fields__advanced-content > .configuration-fields__note,
.configuration-fields__limits {
  grid-column: 1 / -1;
}
.configuration-fields__limits {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
  min-width: 0;
}
.configuration-fields__note {
  margin: 0;
  color: var(--text-muted);
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
@media (max-width: 900px) {
  .configuration-fields--stt {
    grid-template-columns: minmax(0, 1fr);
  }
  .configuration-fields__advanced-content,
  .configuration-fields__limits {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
