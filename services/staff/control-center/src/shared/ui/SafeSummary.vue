<script setup lang="ts">
import { useServerMessage } from "@/shared/ui/server-message";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { safeSummary } from "@/shared/ui/safe-summary";

const props = defineProps<{
  content?: string | null;
  fallback?: string;
  maximumLength?: number;
}>();
const { t } = useI18n();
const summary = computed(() =>
  safeSummary(
    props.content ? serverMessage(props.content) : props.content,
    props.maximumLength,
  ),
);
const visibleText = computed(() => {
  if (summary.value.structured) return t("common.structuredResult");
  return summary.value.text || props.fallback || "—";
});
const serverMessage = useServerMessage();
</script>

<template>
  <span
    class="safe-summary"
    :title="summary.truncated ? summary.text : undefined"
    >{{ visibleText }}</span
  >
</template>

<style scoped>
.safe-summary {
  display: -webkit-box;
  overflow: hidden;
  overflow-wrap: anywhere;
  color: var(--muted);
  line-height: 1.4;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
</style>
