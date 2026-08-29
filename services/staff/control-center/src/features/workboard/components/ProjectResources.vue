<script setup lang="ts">
import { Bot, FileStack, Play, ShieldQuestion, Workflow } from "@lucide/vue";

import type { Project } from "@/shared/api/generated/openapi/types.gen";

defineProps<{ project: Project }>();
</script>

<template>
  <nav class="project-resources" :aria-label="$t('workboard.resources')">
    <RouterLink
      :to="`/projects/${project.ref}/agents`"
      class="project-resource"
    >
      <Bot :size="18" aria-hidden="true" /><span
        ><strong>{{ $t("project.agents") }}</strong
        ><small>{{
          $t("workboard.resourceCount", { count: project.agentCount })
        }}</small></span
      ><b>{{ project.agentCount }}</b>
    </RouterLink>
    <RouterLink
      :to="`/projects/${project.ref}/workflows`"
      class="project-resource"
    >
      <Workflow :size="18" aria-hidden="true" /><span
        ><strong>{{ $t("project.workflows") }}</strong
        ><small>{{
          $t("workboard.resourceCount", { count: project.workflowCount })
        }}</small></span
      ><b>{{ project.workflowCount }}</b>
    </RouterLink>
    <RouterLink :to="`/projects/${project.ref}/runs`" class="project-resource">
      <Play :size="18" aria-hidden="true" /><span
        ><strong>{{ $t("runs.title") }}</strong
        ><small>{{
          $t("workboard.activeCount", { count: project.activeRunCount })
        }}</small></span
      ><b>{{ project.activeRunCount }}</b>
    </RouterLink>
    <RouterLink
      :to="`/decisions?projectRef=${encodeURIComponent(project.ref)}`"
      class="project-resource"
    >
      <ShieldQuestion :size="18" aria-hidden="true" /><span
        ><strong>{{ $t("project.decisions") }}</strong
        ><small>{{
          $t("workboard.pendingCount", { count: project.pendingGateCount })
        }}</small></span
      ><b>{{ project.pendingGateCount }}</b>
    </RouterLink>
    <RouterLink :to="`/projects/${project.ref}/files`" class="project-resource">
      <FileStack :size="18" aria-hidden="true" /><span
        ><strong>{{ $t("files.title") }}</strong
        ><small>{{ $t("workboard.openCollection") }}</small></span
      >
    </RouterLink>
  </nav>
</template>

<style scoped>
.project-resources {
  display: grid;
  grid-template-columns: repeat(5, minmax(150px, 1fr));
  gap: 1px;
  background: var(--hairline);
}
.project-resource {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  min-height: 58px;
  padding: 9px 16px;
  color: inherit;
  background: var(--surface);
  text-decoration: none;
}
.project-resource:hover {
  background: var(--panel);
  text-decoration: none;
}
.project-resource > svg {
  color: var(--accent-strong);
}
.project-resource span {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.project-resource small {
  color: var(--muted);
}
.project-resource b {
  font-family: var(--font-mono);
  font-size: 1rem;
}
@media (max-width: 1100px) {
  .project-resources {
    grid-template-columns: repeat(3, minmax(170px, 1fr));
  }
}
@media (max-width: 700px) {
  .project-resources {
    grid-template-columns: 1fr;
  }
}
</style>
