<script setup lang="ts">
import { Check, ChevronDown } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { AssistantPlan } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ plan: AssistantPlan; variant: number }>();
const { t } = useI18n();
const summary = computed(() =>
  props.plan.operations.map((operation) => operation.title).join(" · "),
);
</script>

<template>
  <!-- Native disclosure сохраняет mounted readback и controls, без нового state/cache. -->
  <details
    v-if="plan.state === 'APPLIED'"
    class="assistant-plan-card assistant-plan-record"
    :data-plan-ref="plan.ref"
  >
    <summary class="assistant-plan-record__summary">
      <Check :size="17" aria-hidden="true" />
      <div class="assistant-plan-record__heading">
        <div class="assistant-plan-record__title">
          <strong>{{ t("assistant.planVariant", { variant }) }}</strong>
          <StatusBadge :state="plan.state" />
        </div>
        <span class="assistant-plan-record__description">{{ summary }}</span>
        <small>{{
          t("assistant.planEditor.revision", {
            revision: plan.revision,
            count: plan.operations.length,
          })
        }}</small>
      </div>
      <ChevronDown
        class="assistant-plan-record__chevron"
        :size="17"
        aria-hidden="true"
      />
    </summary>
    <div class="assistant-plan-record__body"><slot /></div>
  </details>
  <section v-else class="assistant-plan-card"><slot /></section>
</template>

<style scoped>
.assistant-plan-card.assistant-plan-record {
  display: block;
  padding: 0;
  border-color: var(--border);
}
.assistant-plan-record__summary {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 44px;
  padding: 8px 10px;
  cursor: pointer;
  list-style: none;
}
.assistant-plan-record__summary::-webkit-details-marker {
  display: none;
}
.assistant-plan-record__summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
  border-radius: 8px;
}
.assistant-plan-record__heading {
  display: grid;
  flex: 1;
  gap: 3px;
  min-width: 0;
}
.assistant-plan-record__title {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: 0.84rem;
}
.assistant-plan-record__description {
  overflow: hidden;
  color: var(--muted);
  font-size: 0.8rem;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.assistant-plan-record__heading small {
  color: var(--subtle);
  font-size: 0.72rem;
}
.assistant-plan-record__chevron {
  flex-shrink: 0;
}
.assistant-plan-record[open] .assistant-plan-record__chevron {
  transform: rotate(180deg);
}
.assistant-plan-record__body {
  display: grid;
  gap: 10px;
  padding: 10px;
  border-top: 1px solid var(--border);
}
</style>
