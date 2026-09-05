<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { ModelCapability } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import {
  accountModelAvailable,
  loadModelCatalog,
  resolveAccountModel,
  type ModelCatalogSnapshot,
} from "./model-catalog";

const props = defineProps<{
  modelValue: string;
  definitionKey: string;
  accountRefs: readonly string[];
  disabled?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: string];
  "availability-change": [available: boolean];
}>();
const { t } = useI18n();
const models = ref<ModelCapability[]>([]);
const resolving = ref(false);
const problem = ref<AppProblem>();
const accounts = computed(() => [...new Set(props.accountRefs)].sort());
const scopeKey = computed(() =>
  JSON.stringify([props.definitionKey, accounts.value]),
);
const available = computed(
  () =>
    accounts.value.length > 0 &&
    models.value.length === accounts.value.length &&
    models.value.every((model, index) =>
      accountModelAvailable(model, accounts.value[index] ?? ""),
    ),
);
const selected = computed<AsyncEntityOption | undefined>(() =>
  props.modelValue
    ? {
        ref: props.modelValue,
        title: props.modelValue,
        description: resolving.value
          ? t("common.loading")
          : available.value
            ? models.value[0]?.reasoningEfforts.join(" · ")
            : t("providers.modelUnavailable"),
        disabled: !available.value,
      }
    : undefined,
);
watch(
  () => [scopeKey.value, props.modelValue],
  async (_value, _previous, cleanup) => {
    const controller = new AbortController();
    cleanup(() => controller.abort());
    models.value = [];
    problem.value = undefined;
    emit("availability-change", false);
    if (!props.modelValue || !props.definitionKey || !accounts.value.length) {
      resolving.value = false;
      return;
    }
    resolving.value = true;
    const key = props.definitionKey;
    const refs = accounts.value;
    const id = props.modelValue;
    try {
      const values = await Promise.all(
        refs.map((account) =>
          resolveAccountModel(key, account, id, controller.signal),
        ),
      );
      if (controller.signal.aborted) return;
      models.value = values.filter(
        (value): value is ModelCapability => !!value,
      );
      emit("availability-change", available.value);
    } catch (error) {
      if (!controller.signal.aborted) problem.value = asProblem(error);
    } finally {
      if (!controller.signal.aborted) resolving.value = false;
    }
  },
  { immediate: true, flush: "sync" },
);

let catalogSnapshot: ModelCatalogSnapshot | undefined;
let catalogScope = "";
async function loadPage(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const scope = JSON.stringify([scopeKey.value, query.trim()]);
  if (cursor && catalogScope !== scope)
    throw new Error("Model catalog cursor scope mismatch");
  const page = await loadModelCatalog(
    props.definitionKey,
    accounts.value[0],
    query,
    cursor,
    signal,
    cursor ? catalogSnapshot : undefined,
  );
  signal.throwIfAborted();
  if (scope !== JSON.stringify([scopeKey.value, query.trim()]))
    throw new Error("Model catalog scope changed");
  catalogSnapshot = page;
  catalogScope = scope;
  return {
    items: page.items.map((model) => ({
      ref: model.id,
      title: model.id,
      description: model.reasoningEfforts.join(" · "),
      meta: model.readinessBlockers.join(" · "),
      disabled: !accountModelAvailable(model, accounts.value[0] ?? ""),
      disabledReason:
        model.readinessBlockers.join(" · ") || t("providers.modelUnavailable"),
    })),
    nextPageToken: page.nextPageToken || undefined,
  };
}
function choose(value: string | null | readonly string[]): void {
  if (!props.disabled && typeof value === "string")
    emit("update:modelValue", value);
}
</script>
<template>
  <div class="provider-model-selector">
    <AsyncEntityPicker
      :key="scopeKey"
      :model-value="modelValue"
      :selected="selected"
      :load-page="loadPage"
      :placeholder="$t('agents.model')"
      :search-placeholder="$t('common.search')"
      :disabled="disabled || !definitionKey || !accountRefs.length"
      :clearable="false"
      @update:model-value="choose"
    />
    <ProblemNotice v-if="problem" :problem="problem" />
    <small v-else-if="modelValue && !resolving && !available">{{
      $t("providers.modelUnavailable")
    }}</small>
  </div>
</template>
<style scoped>
.provider-model-selector {
  display: grid;
  min-width: 0;
  gap: 6px;
}
.provider-model-selector small {
  color: var(--muted);
  overflow-wrap: anywhere;
}
</style>
