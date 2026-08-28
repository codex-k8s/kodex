<script setup lang="ts">
import { CircleAlert, Clock3, CloudCheck, FilePenLine } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { agentDetailCopy } from "@/features/agents/detail/copy";
import type { ApplyBoundary } from "@/features/agents/detail/model";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  state: "APPLIED" | "DRAFT" | "RUNNING" | "FAILED";
  scope: string;
  boundary: ApplyBoundary;
}>();
const { locale } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
const detail = computed(() => {
  if (props.state === "DRAFT") return copy.value.apply.localDraft;
  if (props.state === "RUNNING") return copy.value.apply.saving;
  if (props.state === "FAILED") return copy.value.apply.failed;
  return copy.value.apply.apiReadback;
});
</script>

<template>
  <section
    class="apply-state"
    :class="`apply-state--${state.toLocaleLowerCase()}`"
    aria-live="polite"
  >
    <CloudCheck v-if="state === 'APPLIED'" :size="19" aria-hidden="true" />
    <FilePenLine v-else-if="state === 'DRAFT'" :size="19" aria-hidden="true" />
    <Clock3 v-else-if="state === 'RUNNING'" :size="19" aria-hidden="true" />
    <CircleAlert v-else :size="19" aria-hidden="true" />
    <div>
      <strong>{{ copy.apply.title }} · {{ scope }}</strong>
      <span>{{ detail }} · {{ copy.apply.boundaries[boundary] }}</span>
    </div>
    <StatusBadge :state="state" />
  </section>
</template>

<style scoped>
.apply-state {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  min-height: 54px;
  padding: 9px 12px;
  border: 1px solid var(--border);
  border-left: 3px solid var(--success);
  border-radius: 8px;
  background: var(--surface);
}
.apply-state > svg {
  color: var(--success);
}
.apply-state div {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.apply-state strong {
  font-size: 0.86rem;
}
.apply-state span:not(.status-badge) {
  color: var(--muted);
  font-size: 0.78rem;
  overflow-wrap: anywhere;
}
.apply-state--draft,
.apply-state--running {
  border-left-color: var(--warning);
}
.apply-state--draft > svg,
.apply-state--running > svg {
  color: var(--warning);
}
.apply-state--failed {
  border-left-color: var(--danger);
}
.apply-state--failed > svg {
  color: var(--danger);
}
@media (max-width: 640px) {
  .apply-state {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .apply-state .status-badge {
    grid-column: 2;
  }
}
</style>
