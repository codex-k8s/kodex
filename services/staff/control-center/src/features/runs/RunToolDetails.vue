<script setup lang="ts">
import { ArrowLeft, Wrench } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { RunNode } from "@/shared/api/generated/openapi/types.gen";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = withDefaults(defineProps<{ node?: RunNode }>(), {
  node: undefined,
});
defineEmits<{ back: [] }>();
const { locale } = useI18n();

const source = computed(
  () => props.node?.integrationNames?.join(", ") || props.node?.role,
);
const duration = computed(() => {
  if (!props.node?.startedAt || !props.node.finishedAt) return undefined;
  const milliseconds =
    new Date(props.node.finishedAt).getTime() -
    new Date(props.node.startedAt).getTime();
  return `${new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: 1,
  }).format(Math.max(0, milliseconds) / 1000)} s`;
});
</script>

<template>
  <section class="run-tool-details">
    <button
      class="button button--ghost run-tool-details__back"
      type="button"
      @click="$emit('back')"
    >
      <ArrowLeft :size="17" aria-hidden="true" />
      {{ $t("common.previous") }}
    </button>
    <header>
      <span><Wrench :size="20" aria-hidden="true" /></span>
      <div>
        <h3>
          {{ node?.displayName || $t("runs.nodeTypes.EXTERNAL_ACTION") }}
        </h3>
        <p>{{ node?.role || $t("common.unavailable") }}</p>
      </div>
    </header>
    <dl>
      <div>
        <dt>{{ $t("common.source") }}</dt>
        <dd>{{ source || $t("common.noData") }}</dd>
      </div>
      <div>
        <dt>{{ $t("common.input") }}</dt>
        <dd>
          <SafeMarkdown
            v-if="node?.inputSummary"
            :content="node.inputSummary"
          />
          <template v-else>{{ $t("common.noData") }}</template>
        </dd>
      </div>
      <div>
        <dt>{{ $t("common.status") }}</dt>
        <dd>
          <StatusBadge v-if="node" :state="node.state" />
          <template v-else>{{ $t("common.unavailable") }}</template>
        </dd>
      </div>
      <div>
        <dt>{{ $t("common.duration") }}</dt>
        <dd>{{ duration || $t("common.noData") }}</dd>
      </div>
      <div>
        <dt>{{ $t("common.result") }}</dt>
        <dd>
          <SafeMarkdown
            v-if="node?.progressSummary || node?.safeErrorMessage"
            :content="
              node.progressSummary ||
              node.safeErrorMessage ||
              $t('common.noData')
            "
          />
          <template v-else>{{ $t("common.noData") }}</template>
        </dd>
      </div>
    </dl>
  </section>
</template>

<style scoped>
.run-tool-details {
  display: grid;
  align-content: start;
  gap: 16px;
  padding: 14px 16px 24px;
}
.run-tool-details__back {
  width: fit-content;
}
.run-tool-details header {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 10px;
}
.run-tool-details header > span {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--accent);
  background: var(--panel);
}
.run-tool-details h3,
.run-tool-details p {
  margin: 0;
}
.run-tool-details p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 0.82rem;
}
.run-tool-details dl {
  display: grid;
  margin: 0;
  border-top: 1px solid var(--border);
}
.run-tool-details dl > div {
  display: grid;
  grid-template-columns: minmax(110px, 0.35fr) minmax(0, 1fr);
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
}
.run-tool-details dt {
  color: var(--subtle);
  font-size: 0.78rem;
}
.run-tool-details dd {
  margin: 0;
}
.run-tool-details dd :deep(p) {
  margin: 0;
}
</style>
