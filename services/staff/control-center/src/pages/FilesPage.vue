<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowLeft } from "@lucide/vue";
import { safeVfsReturn } from "@/features/files/vfs-location";

import FilesWorkspace from "@/features/files/FilesWorkspace.vue";
import VfsBrowser from "@/features/files/VfsBrowser.vue";
import ContextCatalog from "@/features/context-resources/ContextCatalog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";

const route = useRoute();
const router = useRouter();
const view = computed<"artifacts" | "vfs" | "skills" | "memory">({
  get: () =>
    route.query.view === "skills" || route.query.view === "memory"
      ? route.query.view
      : route.query.view === "vfs"
        ? "vfs"
        : route.query.artifactRef || route.query.view === "artifacts"
          ? "artifacts"
          : route.query.vfsTrail || route.query.vfsQuery
            ? "vfs"
            : "artifacts",
  set: (value) => {
    void router.replace({
      query: { ...route.query, view: value, artifactRef: undefined },
    });
  },
});
const projectRef = computed(() => String(route.params.projectRef));
const agentRef = computed(() =>
  typeof route.query.agentRef === "string" ? route.query.agentRef : undefined,
);
const returnTo = computed(() =>
  safeVfsReturn(route.query.vfsReturn, projectRef.value),
);
const mode = computed(() =>
  route.name === "files-trash" ? "TRASH" : "ACTIVE",
);
const initialArtifactRef = computed(() =>
  typeof route.query.artifactRef === "string"
    ? route.query.artifactRef
    : undefined,
);
function selectArtifact(ref: string): void {
  if ((initialArtifactRef.value ?? "") === ref) return;
  void router.replace({
    query: { ...route.query, artifactRef: ref || undefined },
  });
}
</script>

<template>
  <PageFrame class="files-page" :title="$t('files.title')">
    <template #actions>
      <RouterLink v-if="returnTo" :to="returnTo" class="button"
        ><ArrowLeft :size="16" />{{ $t("vfs.back") }}</RouterLink
      >
      <div
        v-if="mode === 'ACTIVE'"
        class="segmented-control"
        role="tablist"
        :aria-label="$t('files.tabs')"
      >
        <button
          role="tab"
          :aria-selected="view === 'artifacts'"
          @click="view = 'artifacts'"
        >
          {{ $t("files.tab.FILES") }}
        </button>
        <button
          role="tab"
          :aria-selected="view === 'vfs'"
          @click="view = 'vfs'"
        >
          {{ $t("vfs.structure") }}
        </button>
        <button
          v-for="kind in ['skills', 'memory'] as const"
          :key="kind"
          role="tab"
          :aria-selected="view === kind"
          @click="view = kind"
        >
          {{ $t(`contextResources.${kind}`) }}
        </button>
      </div>
    </template>
    <ContextCatalog
      v-if="(view === 'skills' || view === 'memory') && mode === 'ACTIVE'"
      :key="`${projectRef}:${view}`"
      :kind="view"
      :project-ref="projectRef"
      :agent-ref="agentRef"
    />
    <VfsBrowser
      v-else-if="view === 'vfs' && mode === 'ACTIVE'"
      :project-ref="projectRef"
    />
    <FilesWorkspace
      v-else
      :key="`${projectRef}:${mode}`"
      :project-ref="projectRef"
      :mode="mode"
      :initial-artifact-ref="initialArtifactRef"
      @selection="selectArtifact"
    />
  </PageFrame>
</template>

<style scoped>
.files-page {
  width: 100%;
  max-width: none;
}
</style>
