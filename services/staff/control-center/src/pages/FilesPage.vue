<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import FilesWorkspace from "@/features/files/FilesWorkspace.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";

const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
const initialArtifactRef = computed(() =>
  typeof route.query.artifactRef === "string"
    ? route.query.artifactRef
    : undefined,
);
</script>

<template>
  <PageFrame
    class="files-page"
    :title="$t('files.title')"
    :subtitle="$t('files.subtitle')"
  >
    <FilesWorkspace
      :key="projectRef"
      :project-ref="projectRef"
      :initial-artifact-ref="initialArtifactRef"
    />
  </PageFrame>
</template>

<style scoped>
.files-page {
  width: 100%;
  max-width: none;
}
</style>
