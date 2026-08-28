<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import { openAssistantWorkspace } from "@/features/assistant/events";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

type ProjectAction = "RUN" | "AGENT" | "WORKFLOW" | "FILE";

const platform = usePlatformStore();
const router = useRouter();
const projectAction = ref<ProjectAction>();
const activeRuns = computed(() => platform.overview?.activeRuns ?? []);
const pendingGates = computed(() => platform.overview?.pendingGates ?? []);
const recentArtifacts = computed(
  () => platform.overview?.recentArtifacts ?? [],
);
const projectActionPermission = computed(() => {
  switch (projectAction.value) {
    case "AGENT":
      return "CREATE_AGENT";
    case "WORKFLOW":
      return "CREATE_WORKFLOW";
    case "FILE":
      return "UPLOAD_ARTIFACT";
    default:
      return "CREATE_RUN";
  }
});
const eligibleProjects = computed(() =>
  platform.projectList.filter((project) =>
    project.nextActions.includes(projectActionPermission.value),
  ),
);

function openProjectAction(action: ProjectAction): void {
  projectAction.value = action;
}

function projectActionPath(projectRef: string): string {
  const prefix = `/projects/${encodeURIComponent(projectRef)}`;
  switch (projectAction.value) {
    case "AGENT":
      return `${prefix}/agents?create=1`;
    case "WORKFLOW":
      return `${prefix}/workflows?create=1`;
    case "FILE":
      return `${prefix}/files`;
    default:
      return `${prefix}/runs/new`;
  }
}

async function chooseProject(projectRef: string): Promise<void> {
  const path = projectActionPath(projectRef);
  projectAction.value = undefined;
  await router.push(path);
}

function artifactPath(projectRef: string, artifactRef: string): string {
  return `/projects/${encodeURIComponent(projectRef)}/files?artifactRef=${encodeURIComponent(artifactRef)}`;
}

async function load(): Promise<void> {
  await Promise.all([platform.loadOverview(), platform.loadProjects()]);
}

onMounted(() => void load());
</script>

<template>
  <PageFrame :title="$t('home.title')" :subtitle="$t('home.subtitle')">
    <template #actions>
      <button
        class="button button--primary"
        type="button"
        @click="openProjectAction('RUN')"
      >
        {{ $t("home.launchWork") }}
      </button>
    </template>
    <AsyncState
      :loading="platform.loading.overview"
      :problem="platform.problems.overview"
      :empty="platform.overview?.projectCount === 0"
      :empty-title="$t('projects.emptyTitle')"
      :empty-text="$t('projects.emptyText')"
      @retry="load"
    >
      <template #empty-action>
        <div class="inline-actions">
          <RouterLink class="button button--primary" to="/projects?create=1">
            {{ $t("projects.new") }}
          </RouterLink>
          <button class="button" type="button" @click="openAssistantWorkspace">
            {{ $t("onboarding.startAssistant") }}
          </button>
        </div>
      </template>

      <section class="metric-grid" :aria-label="$t('home.metrics')">
        <article class="metric-card">
          <span>{{ $t("home.projects") }}</span
          ><strong>{{ platform.overview?.projectCount ?? 0 }}</strong>
        </article>
        <article class="metric-card">
          <span>{{ $t("home.agents") }}</span
          ><strong>{{ platform.overview?.agentCount ?? 0 }}</strong>
        </article>
        <article class="metric-card">
          <span>{{ $t("home.activeRuns") }}</span
          ><strong>{{ platform.overview?.activeRunCount ?? 0 }}</strong>
        </article>
        <article class="metric-card">
          <span>{{ $t("home.pending") }}</span
          ><strong>{{ platform.overview?.pendingGateCount ?? 0 }}</strong>
        </article>
      </section>

      <div class="home-layout">
        <div class="home-main">
          <section class="panel home-section">
            <div class="section-header">
              <h2>{{ $t("home.activeRuns") }}</h2>
              <RouterLink to="/runs">{{ $t("common.all") }}</RouterLink>
            </div>
            <div v-if="activeRuns.length" class="entity-list">
              <RouterLink
                v-for="run in activeRuns"
                :key="run.ref"
                :to="`/runs/${run.ref}`"
                class="entity-row"
              >
                <div>
                  <h3>{{ run.title }}</h3>
                  <SafeSummary
                    :content="run.currentActivity"
                    :fallback="run.target.displayName"
                  />
                </div>
                <StatusBadge :state="run.state" />
                <span>{{ run.target.displayName }}</span>
              </RouterLink>
            </div>
            <div v-else class="empty-compact">{{ $t("common.empty") }}</div>
          </section>

          <section class="panel home-section">
            <div class="section-header">
              <h2>{{ $t("home.recentArtifacts") }}</h2>
            </div>
            <div v-if="recentArtifacts.length" class="entity-list">
              <RouterLink
                v-for="artifact in recentArtifacts"
                :key="artifact.ref"
                :to="artifactPath(artifact.projectRef, artifact.ref)"
                class="entity-row"
              >
                <div>
                  <h3>{{ artifact.fileName }}</h3>
                  <p>
                    {{
                      platform.projects[artifact.projectRef]?.name ??
                      $t("app.project")
                    }}
                  </p>
                </div>
                <StatusBadge :state="artifact.scanState" />
                <span>{{ $t(`files.source.${artifact.source}`) }}</span>
              </RouterLink>
            </div>
            <div v-else class="empty-compact">{{ $t("common.empty") }}</div>
          </section>
        </div>

        <aside class="home-aside">
          <section class="panel home-section decision-panel">
            <div class="section-header">
              <h2>{{ $t("home.pending") }}</h2>
              <RouterLink to="/decisions">{{ $t("common.all") }}</RouterLink>
            </div>
            <div v-if="pendingGates.length" class="entity-list">
              <RouterLink
                v-for="gate in pendingGates"
                :key="gate.ref"
                :to="`/runs/${gate.runRef}`"
                class="entity-row entity-row--compact"
              >
                <div>
                  <h3>{{ gate.title }}</h3>
                  <SafeSummary :content="gate.contextSummary" />
                </div>
                <StatusBadge :state="gate.state" />
              </RouterLink>
            </div>
            <div v-else class="empty-compact">{{ $t("common.empty") }}</div>
          </section>

          <section class="panel home-section">
            <h2>{{ $t("home.quickStart") }}</h2>
            <div class="quick-start-actions">
              <button
                class="button"
                type="button"
                @click="openProjectAction('AGENT')"
              >
                {{ $t("project.createAgent") }}
              </button>
              <button
                class="button"
                type="button"
                @click="openProjectAction('WORKFLOW')"
              >
                {{ $t("project.createWorkflow") }}
              </button>
              <button
                class="button"
                type="button"
                @click="openProjectAction('FILE')"
              >
                {{ $t("common.upload") }}
              </button>
            </div>
          </section>
        </aside>
      </div>
    </AsyncState>

    <ModalDialog
      v-if="projectAction"
      :title="$t('home.chooseProject')"
      @close="projectAction = undefined"
    >
      <p>{{ $t("home.chooseProjectText") }}</p>
      <div v-if="eligibleProjects.length" class="project-choice-list">
        <button
          v-for="project in eligibleProjects"
          :key="project.ref"
          class="project-choice"
          type="button"
          @click="chooseProject(project.ref)"
        >
          <strong>{{ project.name }}</strong>
          <span>{{ project.purpose }}</span>
        </button>
      </div>
      <div v-else class="empty-compact">
        <p>{{ $t("home.noEligibleProject") }}</p>
        <RouterLink class="button button--primary" to="/projects?create=1">
          {{ $t("projects.new") }}
        </RouterLink>
      </div>
    </ModalDialog>
  </PageFrame>
</template>

<style scoped>
.home-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(300px, 0.8fr);
  gap: 18px;
  margin-top: 18px;
}
.home-main,
.home-aside {
  display: grid;
  align-content: start;
  gap: 18px;
}
.home-section {
  padding: 16px;
}
.home-section h2 {
  margin: 0;
  font-size: 0.95rem;
}
.entity-row--compact {
  grid-template-columns: minmax(0, 1fr) auto;
}
.quick-start-actions,
.project-choice-list {
  display: grid;
  gap: 9px;
  margin-top: 14px;
}
.quick-start-actions .button {
  justify-content: flex-start;
}
.project-choice {
  display: grid;
  gap: 4px;
  width: 100%;
  min-height: 56px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--surface);
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.project-choice span {
  color: var(--muted);
}
.empty-compact {
  padding: 20px;
  border: 1px dashed var(--border);
  border-radius: 9px;
  color: var(--muted);
  text-align: center;
}
@media (max-width: 1000px) {
  .home-layout {
    grid-template-columns: 1fr;
  }
  .home-aside {
    grid-row: 1;
  }
}
</style>
