<script setup lang="ts">
import { File } from "@lucide/vue";

import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<{ artifacts: Artifact[] }>();

function artifactPath(artifact: Artifact): string {
  if (!artifact.projectRef) return "/projects";
  return `/projects/${encodeURIComponent(artifact.projectRef)}/files?artifactRef=${encodeURIComponent(artifact.ref)}`;
}
</script>

<template>
  <div class="artifact-list">
    <RouterLink
      v-for="artifact in artifacts"
      :key="artifact.ref"
      :to="artifactPath(artifact)"
      class="artifact-item"
    >
      <span class="artifact-item__icon"
        ><File :size="17" aria-hidden="true"
      /></span>
      <div class="artifact-item__body">
        <h3>{{ artifact.fileName }}</h3>
        <p>
          {{ $t(`files.source.${artifact.source}`) }} ·
          {{ new Date(artifact.createdAt).toLocaleString() }}
        </p>
      </div>
      <StatusBadge :state="artifact.scanState" />
    </RouterLink>
  </div>
</template>

<style scoped>
.artifact-item {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  min-height: 58px;
  padding: 9px 16px;
  border-bottom: 1px solid var(--hairline);
  color: inherit;
  text-decoration: none;
}
.artifact-item:last-child {
  border-bottom: 0;
}
.artifact-item:hover {
  background: var(--panel);
  text-decoration: none;
}
.artifact-item__icon {
  display: grid;
  place-items: center;
  width: 30px;
  height: 34px;
  border-radius: 6px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.artifact-item__body {
  min-width: 0;
}
.artifact-item h3,
.artifact-item p {
  margin: 0;
  overflow-wrap: anywhere;
}
.artifact-item p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 0.75rem;
}
@media (max-width: 560px) {
  .artifact-item {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .artifact-item > :last-child {
    grid-column: 2;
  }
}
</style>
