<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import ConfigurationCatalog from "@/features/managed-configurations/ConfigurationCatalog.vue";
import {
  configurationRequiresProject,
  type ConfigurationKind,
} from "@/features/managed-configurations/api";
import ProjectPicker from "@/features/projects/ProjectPicker.vue";
import { loadProject } from "@/features/projects/api";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
const route = useRoute();
const router = useRouter();
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
const project = ref<Project>();
const problem = ref<AppProblem>();
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
watch(
  projectRef,
  async (ref, _previous, cleanup) => {
    project.value = undefined;
    problem.value = undefined;
    if (!ref) return;
    const controller = new AbortController();
    cleanup(() => controller.abort());
    try {
      const result = await loadProject(ref, controller.signal);
      if (!controller.signal.aborted) project.value = result;
    } catch (error) {
      if (!controller.signal.aborted) problem.value = asProblem(error);
    }
  },
  { immediate: true },
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
    <ProblemNotice v-if="problem" :problem="problem" compact />
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
