<script setup lang="ts">
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
defineProps<{ gate: OwnerGate }>();
</script>
<template>
  <section v-if="gate.reworkPlan || gate.resolutionKind === 'SUPERSEDED'" class="workflow-rework-preview">
    <p v-if="gate.resolutionKind === 'SUPERSEDED'" role="status">{{ $t("workflowResults.superseded") }}</p>
    <template v-if="gate.reworkPlan">
      <h3>{{ $t(gate.state === "OPEN" ? "workflowResults.reworkPlan" : gate.state === "APPROVED" ? "workflowResults.reworkApprovedPlan" : "workflowResults.reworkDeclinedPlan") }}</h3>
      <p>{{ $t("workflowResults.reworkBoundary") }}</p>
      <p v-if="gate.reworkPlan.blockReason" class="field-error" role="status">{{ $t(`workflowResults.blockers.${gate.reworkPlan.blockReason}`) }}</p>
      <template v-for="group in ['authors', 'rechecks'] as const" :key="group">
        <h4>{{ $t(group === 'authors' ? 'workflowResults.reworkAuthors' : 'workflowResults.reworkChecks') }}</h4>
        <ul><li v-for="step in gate.reworkPlan[group]" :key="step.stepKey"><code>{{ step.stepKey }}</code> · {{ step.nextAttempt === 0 ? $t('workflowResults.reworkExhausted') : $t('workflowResults.reworkAttempt', { previous: step.previousAttempt, next: step.nextAttempt }) }}</li></ul>
      </template>
    </template>
  </section>
</template>
<style scoped>
.workflow-rework-preview { min-width: 0; padding: 12px; border: 1px solid var(--border); border-radius: 8px; overflow-wrap: anywhere; }
.workflow-rework-preview h3, .workflow-rework-preview h4 { margin-block: 8px; }
.workflow-rework-preview ul { padding-inline-start: 20px; }
</style>
