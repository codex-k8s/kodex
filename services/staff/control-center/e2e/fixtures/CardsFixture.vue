<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute } from "vue-router";

import AgentCard from "@/features/agents/catalog/AgentCard.vue";
import { toAgentCatalogItem } from "@/features/agents/catalog/model";
import ProjectList from "@/features/projects/ProjectList.vue";
import WorkflowCard from "@/features/workflows/catalog/WorkflowCard.vue";
import type {
  Agent,
  Project,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";

const route = useRoute();
const scope =
  new URLSearchParams(window.location.search).get("scope") === "project"
    ? "project"
    : "global";
const updatedAt = "2026-08-01T09:00:00Z";
const lastActivityAt = "2026-09-06T12:34:00Z";
const projects: Project[] = (
  ["NONE", "READY", "DEGRADED", "UNKNOWN"] as const
).map((integrationState, index) => ({
  ref: `project_cards_${String(index)}`,
  version: 7,
  name: `Проект карточек ${String(index + 1)}`,
  purpose: "Проверка серверной проекции каталога",
  language: "ru",
  lifecycle: "ACTIVE",
  agentCount: 13,
  workflowCount: 17,
  activeRunCount: 19,
  pendingGateCount: 23,
  integrationState,
  updatedAt,
  lastActivityAt: index === 0 ? lastActivityAt : undefined,
  nextActions: index === 0 ? ["OPEN", "CREATE_RUN"] : ["OPEN"],
}));
const visibleProjects = scope === "project" ? projects.slice(0, 1) : projects;
function agent(
  ref: string,
  state: Agent["state"],
  projectRef = "project_cards_0",
): Agent {
  return {
    ref,
    version: 7,
    projectRef,
    name: `Сотрудник ${ref}`,
    purpose: "Подготовка результата",
    roleDescription: "Исследователь",
    state,
    enabled: true,
    system: false,
    runtimeRef: "runtime_cards",
    runtimeName: "Среда карточек",
    runtimeProvider: "OpenAI",
    runtimeModel: "gpt-5.4",
    runtimeRevision: "revision_cards",
    runtimeReady: true,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    updatedAt,
    nextActions: ["OPEN"],
    currentActivity: "Проверяет входные данные",
    currentRunRef: state === "RUNNING" ? "run_cards_first" : undefined,
  };
}
const agents = ref<Agent[]>([
  agent("agent_ready", "READY"),
  agent("agent_running", "RUNNING"),
  { ...agent("agent_denied", "RUNNING"), currentRunRef: undefined },
  agent("agent_other", "READY", "project_cards_1"),
]);
function workflow(
  ref: string,
  state: Workflow["state"],
  allowed: boolean,
  projectRef = "project_cards_0",
): Workflow {
  const published = state === "PUBLISHED";
  return {
    ref,
    version: 7,
    projectRef,
    name: `Процесс ${ref}`,
    purpose: "Проверка карточной проекции",
    state,
    revision: 3,
    revisionRef: `${ref}_revision`,
    publishedRevisionRef: published ? `${ref}_revision` : undefined,
    draftRevisionRef: published ? undefined : `${ref}_revision`,
    launchReadiness: {
      allowedToSubmit: allowed,
      reason: allowed
        ? "READY"
        : published
          ? "PERMISSION_REQUIRED"
          : "UNPUBLISHED",
      workflowVersion: 7,
      revisionRef: published ? `${ref}_revision` : undefined,
      contextDigest: "a".repeat(64),
      operationalState: "READY",
    },
    inputFields: [],
    steps: [],
    validationMessages: [],
    updatedAt,
    nextActions: allowed
      ? ["OPEN", "EDIT", "LAUNCH"]
      : published
        ? ["OPEN"]
        : ["OPEN", "EDIT"],
    // Карточка использует owner summary, а не длину необязательной подробной проекции steps.
    cardSummary: {
      stageCount: 31,
      uniqueAgentCount: 11,
      parallelGroupCount: 5,
      hasHumanGate: published,
      activeRunCount: 7,
      pendingGateCount: 3,
      lastActivityAt: published ? lastActivityAt : undefined,
    },
  };
}
const workflows: Workflow[] = [
  workflow("workflow_published", "PUBLISHED", true),
  workflow("workflow_draft", "DRAFT", false),
  workflow("workflow_denied", "PUBLISHED", false),
  workflow("workflow_other", "PUBLISHED", true, "project_cards_1"),
];
// Группы проверяют только входные DTO компонентов. Server filtering покрывает organization-catalog.
const groups = computed(() =>
  visibleProjects.slice(0, scope === "project" ? 1 : 2).map((project) => ({
    project,
    agents: agents.value.filter((item) => item.projectRef === project.ref),
    workflows: workflows.filter((item) => item.projectRef === project.ref),
  })),
);
function updateProjection(clearRun = false) {
  agents.value = agents.value.map((item) =>
    item.ref === "agent_running"
      ? {
          ...item,
          version: item.version + 1,
          currentActivity: "Готовит итоговый отчёт",
          currentRunRef: clearRun ? undefined : "run_cards_second",
        }
      : item,
  );
}
</script>

<template>
  <main class="cards-fixture" :data-scope="scope">
    <h1>Карточки каталогов</h1>
    <output aria-label="Последний переход">{{ route.fullPath }}</output>
    <section aria-label="Проекты">
      <h2>Проекты</h2>
      <ProjectList :items="visibleProjects" expanded />
    </section>
    <div class="cards-fixture__controls">
      <button type="button" @click="updateProjection()">
        Обновить проекцию
      </button>
      <button type="button" @click="updateProjection(true)">
        Убрать ссылку запуска
      </button>
    </div>
    <section
      v-for="group in groups"
      :key="group.project.ref"
      :data-project="group.project.ref"
    >
      <h2>{{ group.project.name }}</h2>
      <h3>Сотрудники</h3>
      <div class="cards-fixture__grid">
        <AgentCard
          v-for="item in group.agents"
          :key="item.ref"
          :data-card-ref="item.ref"
          :item="toAgentCatalogItem(item)"
          :to="`/projects/${item.projectRef}/agents/${item.ref}`"
        />
      </div>
      <h3>Процессы</h3>
      <div class="cards-fixture__grid">
        <WorkflowCard
          v-for="item in group.workflows"
          :key="item.ref"
          :data-card-ref="item.ref"
          :workflow="item"
        />
      </div>
    </section>
  </main>
</template>

<style scoped>
.cards-fixture {
  display: grid;
  gap: 24px;
  padding: 16px;
}
.cards-fixture__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.cards-fixture__controls {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
@media (max-width: 720px) {
  .cards-fixture__grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
