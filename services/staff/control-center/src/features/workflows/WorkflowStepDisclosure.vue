<script setup lang="ts">
import { ref, watch, nextTick } from "vue";
const props = defineProps<{
  open: boolean;
  number: number;
  name: string;
  agentTitle: string;
  parallel: boolean;
  parallelGroup?: string | number;
  humanGate: boolean;
  needsAttention: boolean;
}>();
const emit = defineEmits<{ toggle: [event: Event] }>();
const element = ref<HTMLDetailsElement>();
watch(
  () => props.open,
  async (open) => {
    if (!open) return;
    await nextTick();
    element.value?.scrollIntoView({ block: "nearest" });
  },
);
</script>
<template>
  <details
    ref="element"
    class="workflow-step-disclosure"
    :open="open"
    @toggle="emit('toggle', $event)"
  >
    <summary class="workflow-step-disclosure__summary">
      <strong class="workflow-step-disclosure__number">{{ number }}</strong>
      <span class="workflow-step-disclosure__heading">
        <strong>{{ name || $t("workflows.untitledStep") }}</strong>
        <span>{{ agentTitle }}</span>
      </span>
      <span class="workflow-step-disclosure__tags">
        <span
          >{{ $t(parallel ? "workflows.parallel" : "workflows.sequential")
          }}<template v-if="parallel"> · {{ parallelGroup }}</template></span
        >
        <span v-if="humanGate">{{ $t("workflows.humanGate") }}</span>
        <span v-if="needsAttention" class="field-error" role="status">{{
          $t("workflows.stepNeedsAttention")
        }}</span>
      </span>
    </summary>
    <div v-if="open" class="workflow-step-disclosure__body"><slot /></div>
  </details>
</template>
<style scoped>
.workflow-step-disclosure {
  border: 1px solid var(--border);
  border-radius: 8px;
  min-width: 0;
}
.workflow-step-disclosure__summary {
  display: grid;
  grid-template-columns: 36px minmax(0, 1fr) minmax(100px, 35%);
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  cursor: pointer;
  list-style: none;
}
.workflow-step-disclosure__summary::-webkit-details-marker {
  display: none;
}
.workflow-step-disclosure__number::after {
  content: " ▸";
}
.workflow-step-disclosure[open] .workflow-step-disclosure__number::after {
  content: " ▾";
}
.workflow-step-disclosure__heading,
.workflow-step-disclosure__tags {
  display: grid;
  gap: 2px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.workflow-step-disclosure__heading > span,
.workflow-step-disclosure__tags {
  font-size: 12px;
  color: var(--muted);
}
.workflow-step-disclosure__body {
  display: grid;
  gap: 10px;
  padding: 12px;
  border-top: 1px solid var(--border);
  min-width: 0;
}
@media (max-width: 600px) {
  .workflow-step-disclosure__summary {
    grid-template-columns: 36px minmax(0, 1fr);
  }
  .workflow-step-disclosure__tags {
    grid-column: 2;
  }
}
</style>
