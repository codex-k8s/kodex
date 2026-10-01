<script setup lang="ts">
import { computed, nextTick, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import ConfigurationCatalog from "@/features/managed-configurations/ConfigurationCatalog.vue";
import {
  configurationRequiresProject,
  type ConfigurationKind,
} from "@/features/managed-configurations/api";
import ProjectPicker from "@/features/projects/ProjectPicker.vue";
import { usePlatformStore } from "@/features/platform/store";
import PageFrame from "@/shared/ui/PageFrame.vue";
const route = useRoute();
const router = useRouter();
const platform = usePlatformStore();
const kinds: readonly ConfigurationKind[] = [
  "PROMPT_TEMPLATE",
  "ROLE_IMAGE",
  "INTEGRATION_DEFINITION",
  "SYSTEM_STT",
];
const kind = computed(() => kinds.find((kind) => kind === route.params.kind));
const projectScoped = computed(
  () => !!kind.value && configurationRequiresProject(kind.value),
);
const projectRef = computed(() =>
  projectScoped.value && typeof route.query.projectRef === "string"
    ? route.query.projectRef
    : "",
);
const project = computed(() =>
  projectRef.value ? platform.projects[projectRef.value] : undefined,
);
watch(
  () => [kind.value, route.query.assistantImportOpen],
  async ([, open]) => {
    if (kind.value !== "INTEGRATION_DEFINITION" || open !== "1") return;
    const currentPath = route.fullPath;
    await nextTick();
    if (route.fullPath !== currentPath) return;
    const query = { ...route.query };
    delete query.assistantImportOpen;
    await router.replace({ query });
  },
  { immediate: true, flush: "post" },
);
function changeProject(value: string): void {
  void router.replace({ query: value ? { projectRef: value } : {} });
}
function openCreated(configurationRef: string): void {
  void router.push({
    name: "configuration",
    params: { kind: "INTEGRATION_DEFINITION", configurationRef },
  });
}
</script>
<template>
  <PageFrame :title="kind ? $t(`managed.kinds.${kind}`) : $t('managed.title')">
    <template #actions
      ><ProjectPicker
        v-if="projectScoped"
        :project="project"
        @select="changeProject"
    /></template>
    <ConfigurationCatalog
      v-if="kind"
      :kind="kind"
      :project-ref="projectRef || undefined"
      :auto-open-import="route.query.assistantImportOpen === '1'"
      @created="openCreated"
    />
    <p v-else role="alert">{{ $t("errors.NOT_FOUND") }}</p>
  </PageFrame>
</template>
