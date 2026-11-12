<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { loadProviderDefinitions } from "./api";
import type { ProviderDefinition } from "./model";

const props = defineProps<{ definitionKey: string; model: string }>();
const { t } = useI18n();
const definition = ref<ProviderDefinition>();
const loading = ref(false);
const unavailable = ref(false);
const policy = computed(() =>
  definition.value?.runtimePolicies?.find(
    (item) => item.modelId === props.model,
  ),
);
watch(
  () => [props.definitionKey, props.model],
  async (_value, _previous, cleanup) => {
    const controller = new AbortController();
    cleanup(() => controller.abort());
    definition.value = undefined;
    unavailable.value = false;
    loading.value = false;
    if (!props.definitionKey || !props.model) return;
    loading.value = true;
    try {
      const page = await loadProviderDefinitions(
        props.definitionKey,
        undefined,
        controller.signal,
        40,
      );
      if (controller.signal.aborted) return;
      definition.value = page.items.find(
        (item) => item.key === props.definitionKey,
      );
      unavailable.value = !definition.value;
    } catch {
      if (!controller.signal.aborted) unavailable.value = true;
    } finally {
      if (!controller.signal.aborted) loading.value = false;
    }
  },
  { immediate: true, flush: "sync" },
);
</script>

<template>
  <details v-if="model && definitionKey" class="model-execution-policy">
    <summary>{{ t("providers.runtimePolicy.title") }}</summary>
    <p v-if="loading">{{ t("common.loading") }}</p>
    <p v-else-if="unavailable || !policy">
      {{ t("providers.runtimePolicy.unknown") }}
    </p>
    <template v-else>
      <p>{{ t("providers.runtimePolicy.ownership") }}</p>
      <dl>
        <dt>{{ t("providers.runtimePolicy.runtime") }}</dt>
        <dd>OpenCode {{ definition?.engineVersion }}</dd>
        <dt>{{ t("providers.runtimePolicy.context") }}</dt>
        <dd>
          {{ policy.contextWindowTokens }} / {{ policy.maximumOutputTokens }}
        </dd>
        <dt>{{ t("providers.runtimePolicy.compaction") }}</dt>
        <dd>
          {{
            policy.context.autoCompaction
              ? t("providers.runtimePolicy.enabled")
              : t("providers.runtimePolicy.disabled")
          }}
        </dd>
        <dt>{{ t("providers.runtimePolicy.pruning") }}</dt>
        <dd>
          {{
            policy.context.prune
              ? t("providers.runtimePolicy.enabled")
              : t("providers.runtimePolicy.disabled")
          }}
        </dd>
        <dt>{{ t("providers.runtimePolicy.requested") }}</dt>
        <dd>
          {{ policy.cache.requested?.optimization ?? "NO_HINTS" }} ·
          {{ policy.cache.requested?.reuseScope ?? "PROVIDER_BOUNDARY" }}
        </dd>
        <dt>{{ t("providers.runtimePolicy.effective") }}</dt>
        <dd>
          {{ policy.cache.mechanism }} · {{ policy.cache.status }} ·
          {{ policy.cache.reuseScope }}
        </dd>
        <dt>{{ t("providers.runtimePolicy.strict") }}</dt>
        <dd>
          {{
            t("providers.runtimePolicy.guarantees", {
              reuse: String(policy.cache.noReuse),
              store: String(policy.cache.noStore),
            })
          }}
        </dd>
        <dt>{{ t("providers.runtimePolicy.response") }}</dt>
        <dd>{{ policy.cache.proxyResponseCache }}</dd>
      </dl>
      <p v-if="!policy.eligible" role="alert">
        {{ t("providers.runtimePolicy.blocked") }}: {{ policy.cache.reason }}
      </p>
      <p v-if="policy.cache.mechanism === 'NONE'">
        {{ t("providers.runtimePolicy.noHints") }}
      </p>
      <p>{{ t("providers.runtimePolicy.physical") }}</p>
    </template>
  </details>
</template>

<style scoped>
.model-execution-policy {
  color: var(--muted);
  font-size: 0.875rem;
}
.model-execution-policy summary {
  cursor: pointer;
}
.model-execution-policy dl {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: 0.4rem 1rem;
}
.model-execution-policy dd {
  margin: 0;
  overflow-wrap: anywhere;
}
@media (max-width: 640px) {
  .model-execution-policy dl {
    grid-template-columns: 1fr;
  }
}
</style>
