<script setup lang="ts">
import { AlertTriangle, ShieldQuestion } from "@lucide/vue";

import type { AttentionItem } from "@/features/workboard/model";
import { runPath } from "@/shared/routes";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  items: AttentionItem[];
  preserveProject?: boolean;
}>();

function itemPath(item: AttentionItem): string {
  const run = item.run;
  if (run) {
    return runPath(run.ref, props.preserveProject ? run.projectRef : undefined);
  }
  const ref = item.kind === "GATE" ? item.gate.runRef : item.run.ref;
  const projectRef =
    props.preserveProject && item.kind === "GATE"
      ? item.gate.projectRef
      : undefined;
  return runPath(ref, projectRef);
}
</script>

<template>
  <div class="attention-list">
    <RouterLink
      v-for="item in items"
      :key="`${item.kind}:${item.ref}`"
      :to="itemPath(item)"
      class="attention-item"
    >
      <span
        class="attention-item__icon"
        :class="`attention-item__icon--${item.kind.toLowerCase()}`"
      >
        <ShieldQuestion
          v-if="item.kind === 'GATE'"
          :size="18"
          aria-hidden="true"
        />
        <AlertTriangle v-else :size="18" aria-hidden="true" />
      </span>
      <div class="attention-item__body">
        <h3>{{ item.kind === "GATE" ? item.gate.title : item.run.title }}</h3>
        <SafeSummary
          :content="
            item.kind === 'GATE'
              ? item.gate.contextSummary
              : item.incident.safeSummary
          "
        />
        <p>
          {{
            item.kind === "GATE"
              ? item.gate.requestedBy.displayName
              : item.incident.safeNextStep
          }}
        </p>
      </div>
      <StatusBadge
        :state="item.kind === 'GATE' ? item.gate.state : item.incident.severity"
        :tone="item.kind === 'GATE' ? 'warning' : 'danger'"
      />
    </RouterLink>
  </div>
</template>

<style scoped>
.attention-item {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  min-height: 72px;
  padding: 11px 16px;
  border-bottom: 1px solid var(--hairline);
  color: inherit;
  text-decoration: none;
}
.attention-item:last-child {
  border-bottom: 0;
}
.attention-item:hover {
  background: var(--panel);
  text-decoration: none;
}
.attention-item__icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 8px;
  color: var(--warning);
  background: var(--warning-soft);
}
.attention-item__icon--incident {
  color: var(--danger);
  background: var(--danger-soft);
}
.attention-item__body {
  min-width: 0;
}
.attention-item h3,
.attention-item p {
  margin: 0;
}
.attention-item h3 {
  margin-bottom: 3px;
}
.attention-item :deep(.safe-summary),
.attention-item__body > p {
  color: var(--muted);
  font-size: 0.78rem;
}
@media (max-width: 620px) {
  .attention-item {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .attention-item > :last-child {
    grid-column: 2;
  }
}
</style>
