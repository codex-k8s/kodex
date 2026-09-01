<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { TokenUsage } from "@/shared/api/generated/openapi/types.gen";

const props = withDefaults(
  defineProps<{
    usage: TokenUsage;
    compact?: boolean;
  }>(),
  { compact: false },
);
const { locale } = useI18n();

const items = computed(() => {
  if (props.usage.totalTokens === 0 && props.usage.modelContextWindow === 0)
    return [];
  const all = [
    ["total", props.usage.totalTokens],
    ["input", props.usage.inputTokens],
    ["cached", props.usage.cachedInputTokens],
    ["output", props.usage.outputTokens],
    ["reasoning", props.usage.reasoningOutputTokens],
    ["contextWindow", props.usage.modelContextWindow],
  ] as const;
  return props.compact ? all.slice(0, 4) : all;
});

function formatTokenCount(value: number): string {
  return new Intl.NumberFormat(locale.value).format(value);
}
</script>

<template>
  <dl
    v-if="items.length"
    class="token-usage"
    :class="{ 'token-usage--compact': compact }"
    :aria-label="$t('runs.usage.title')"
  >
    <div v-for="item in items" :key="item[0]">
      <dt>{{ $t(`runs.usage.${item[0]}`) }}</dt>
      <dd>{{ formatTokenCount(item[1]) }}</dd>
    </div>
  </dl>
</template>

<style scoped>
.token-usage {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
}
.token-usage--compact {
  grid-column: 1 / -1;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 6px;
  padding-top: 8px;
  border-top: 1px solid var(--border);
}
.token-usage > div {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.token-usage dt {
  overflow: hidden;
  color: var(--subtle);
  font-size: 0.68rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.token-usage dd {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 0.76rem;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}
@media (max-width: 520px) {
  .token-usage--compact {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
