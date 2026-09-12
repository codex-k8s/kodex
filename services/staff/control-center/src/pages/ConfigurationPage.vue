<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import ConfigurationEditor from "@/features/managed-configurations/ConfigurationEditor.vue";
import {
  configurationRequiresProject,
  type ConfigurationKind,
} from "@/features/managed-configurations/api";
import ProjectPicker from "@/features/projects/ProjectPicker.vue";
import { loadProject } from "@/features/projects/api";
import type { ManagedConfiguration } from "@/shared/api/generated/openapi/types.gen";
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
const kind = computed(() => kinds.find((value) => value === route.params.kind));
const configurationRef = computed(() =>
  typeof route.params.configurationRef === "string" &&
  route.params.configurationRef !== "new"
    ? route.params.configurationRef
    : undefined,
);
const projectScoped = computed(
  () => !!kind.value && configurationRequiresProject(kind.value),
);
const projectRef = computed(() =>
  projectScoped.value && typeof route.query.projectRef === "string"
    ? route.query.projectRef
    : undefined,
);
const project = ref<Project>();
const problem = ref<AppProblem>();
watch(
  projectRef,
  async (reference, _previous, cleanup) => {
    project.value = undefined;
    problem.value = undefined;
    if (!reference) return;
    const controller = new AbortController();
    cleanup(() => controller.abort());
    try {
      const result = await loadProject(reference, controller.signal);
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
function created(configuration: ManagedConfiguration): void {
  void router.replace({
    name: "configuration",
    params: { kind: configuration.kind, configurationRef: configuration.ref },
    query: configuration.projectRef
      ? { projectRef: configuration.projectRef }
      : {},
  });
}
</script>
<template>
  <PageFrame :title="kind ? $t(`managed.kinds.${kind}`) : $t('managed.title')">
    <template v-if="projectScoped && !configurationRef" #actions>
      <ProjectPicker :project="project" @select="changeProject" />
    </template>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <ConfigurationEditor
      v-if="kind"
      :key="route.fullPath"
      :kind="kind"
      :configuration-ref="configurationRef"
      :project-ref="projectRef"
      @created="created"
    />
    <p v-else role="alert">{{ $t("errors.NOT_FOUND") }}</p>
  </PageFrame>
</template>
