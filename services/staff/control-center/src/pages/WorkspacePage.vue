<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import ProjectDetailSurface from "@/features/projects/ProjectDetailSurface.vue";
import WorkspaceResourceLifecycle from "@/features/workspace-resources/WorkspaceResourceLifecycle.vue";
import WorkspaceTeamPanel from "@/features/workspace-team/WorkspaceTeamPanel.vue";

const route = useRoute();
const projectId = computed(() => String(route.params.projectId));
</script>

<template>
  <div class="page">
    <ProjectDetailSurface />
    <div class="section-stack">
      <WorkspaceTeamPanel />
      <WorkspaceResourceLifecycle :project-id="projectId" />
    </div>
  </div>
</template>
