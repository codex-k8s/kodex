<script setup lang="ts">
import { Bot, FolderKanban, Upload, Workflow } from "@lucide/vue";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import { openAssistantWorkspace } from "@/features/assistant/events";
import { usePlatformStore } from "@/features/platform/store";
import ArtifactList from "@/features/workboard/components/ArtifactList.vue";
import AttentionList from "@/features/workboard/components/AttentionList.vue";
import CapabilityCoverageList from "@/features/workboard/components/CapabilityCoverageList.vue";
import RunWorkItem from "@/features/workboard/components/RunWorkItem.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import {
  collectAttention,
  homeCapabilityCoverage,
  projectArtifacts,
} from "@/features/workboard/model";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

type ProjectAction = "RUN" | "AGENT" | "WORKFLOW" | "FILE";

const platform = usePlatformStore();
const router = useRouter();
const { t } = useI18n();
const projectAction = ref<ProjectAction>();
const overviewReady = ref(false);
const projectsReady = ref(platform.projectList.length > 0);

const activeRuns = computed(() => platform.overview?.activeRuns ?? []);
const pendingGates = computed(() => platform.overview?.pendingGates ?? []);
const attention = computed(() =>
  collectAttention(activeRuns.value, pendingGates.value),
);
const capabilityCoverage = homeCapabilityCoverage();
const recentArtifacts = computed(() =>
  projectArtifacts(platform.overview?.recentArtifacts ?? []),
);
const visibleProjects = computed(() => platform.projectList.slice(0, 6));
const currentUserName = computed(
  () => platform.bootstrap?.currentUser.displayName,
);
const pageTitle = computed(() =>
  currentUserName.value
    ? t("workboard.greeting", { name: currentUserName.value })
    : t("home.title"),
);
const refreshing = computed(
  () =>
    (platform.loading.overview && overviewReady.value) ||
    (platform.loading.projects && projectsReady.value),
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

async function refreshOverview(): Promise<void> {
  await platform.loadOverview();
  if (!platform.problems.overview) overviewReady.value = true;
}

async function refreshProjects(): Promise<void> {
  await platform.loadProjects();
  if (!platform.problems.projects) projectsReady.value = true;
}

async function refresh(): Promise<void> {
  await Promise.all([refreshOverview(), refreshProjects()]);
}

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

onMounted(() => void refresh());
</script>

<template>
  <PageFrame :title="pageTitle" :subtitle="$t('home.subtitle')">
    <template #actions>
      <button
        class="button button--primary"
        type="button"
        @click="openProjectAction('RUN')"
      >
        {{ $t("home.launchWork") }}
      </button>
    </template>

    <WorkboardSection
      class="home-attention-section"
      :title="$t('workboard.attention')"
      :count="attention.length"
      :loading="platform.loading.overview"
      :refreshing="refreshing"
      :ready="overviewReady"
      :problem="platform.problems.overview"
      @retry="refreshOverview"
    >
      <template #action>
        <RouterLink to="/decisions">{{ $t("common.all") }}</RouterLink>
      </template>
      <AttentionList v-if="attention.length" :items="attention" />
      <p v-else class="home-section-empty">{{ $t("workboard.noAttention") }}</p>
      <CapabilityCoverageList :items="capabilityCoverage" />
    </WorkboardSection>

    <WorkboardSection
      class="home-running-section"
      :title="$t('workboard.runningNow')"
      :count="activeRuns.length"
      :loading="platform.loading.overview"
      :refreshing="refreshing"
      :ready="overviewReady"
      :problem="platform.problems.overview"
      :empty="activeRuns.length === 0"
      :empty-text="$t('workboard.noActiveRuns')"
      @retry="refreshOverview"
    >
      <template #action>
        <RouterLink to="/runs">{{ $t("common.all") }}</RouterLink>
      </template>
      <RunWorkItem
        v-for="run in activeRuns.slice(0, 6)"
        :key="run.ref"
        :run="run"
      />
    </WorkboardSection>

    <WorkboardSection
      class="home-project-section"
      :title="$t('home.projects')"
      :count="platform.overview?.projectCount ?? platform.projectList.length"
      :loading="platform.loading.projects"
      :refreshing="refreshing"
      :ready="projectsReady"
      :problem="platform.problems.projects"
      :empty="visibleProjects.length === 0"
      :empty-text="$t('projects.emptyText')"
      @retry="refreshProjects"
    >
      <template #action>
        <RouterLink to="/projects">{{ $t("common.all") }}</RouterLink>
      </template>
      <div class="home-projects">
        <RouterLink
          v-for="project in visibleProjects"
          :key="project.ref"
          :to="`/projects/${project.ref}`"
          class="home-project"
        >
          <span class="home-project__icon">
            <FolderKanban :size="20" aria-hidden="true" />
          </span>
          <div class="home-project__copy">
            <h3>{{ project.name }}</h3>
            <p>{{ project.purpose }}</p>
            <small>{{
              $t("workboard.projectActivity", {
                runs: project.activeRunCount,
                gates: project.pendingGateCount,
              })
            }}</small>
          </div>
          <StatusBadge :state="project.lifecycle" />
        </RouterLink>
      </div>
    </WorkboardSection>

    <div class="home-support-grid">
      <WorkboardSection
        :title="$t('workboard.recentResults')"
        :count="recentArtifacts.length"
        :loading="platform.loading.overview"
        :refreshing="refreshing"
        :ready="overviewReady"
        :problem="platform.problems.overview"
        :empty="recentArtifacts.length === 0"
        :empty-text="$t('workboard.noRecentResults')"
        @retry="refreshOverview"
      >
        <ArtifactList :artifacts="recentArtifacts" />
      </WorkboardSection>

      <WorkboardSection :title="$t('home.quickStart')" :ready="true">
        <div class="home-quick-actions">
          <button
            class="button"
            type="button"
            @click="openProjectAction('AGENT')"
          >
            <Bot :size="16" aria-hidden="true" />
            {{ $t("project.createAgent") }}
          </button>
          <button
            class="button"
            type="button"
            @click="openProjectAction('WORKFLOW')"
          >
            <Workflow :size="16" aria-hidden="true" />
            {{ $t("project.createWorkflow") }}
          </button>
          <button
            class="button"
            type="button"
            @click="openProjectAction('FILE')"
          >
            <Upload :size="16" aria-hidden="true" />
            {{ $t("common.upload") }}
          </button>
        </div>
      </WorkboardSection>
    </div>

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
          <strong>{{ project.name }}</strong
          ><span>{{ project.purpose }}</span>
        </button>
      </div>
      <div v-else class="home-empty-action">
        <p>{{ $t("home.noEligibleProject") }}</p>
        <div class="home-empty-actions">
          <RouterLink class="button button--primary" to="/projects?create=1">
            {{ $t("projects.new") }}
          </RouterLink>
          <button class="button" type="button" @click="openAssistantWorkspace">
            {{ $t("onboarding.startAssistant") }}
          </button>
        </div>
      </div>
    </ModalDialog>
  </PageFrame>
</template>

<style scoped>
.home-support-grid {
  display: grid;
  gap: 16px;
  margin-top: 16px;
}
.home-support-grid {
  grid-template-columns: minmax(0, 1.45fr) minmax(280px, 0.55fr);
}
.home-attention-section,
.home-running-section,
.home-project-section {
  margin-top: 16px;
}
.home-section-empty {
  margin: 0;
  padding: 24px 16px;
  color: var(--muted);
  text-align: center;
}
.home-projects {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(290px, 1fr));
  gap: 1px;
  background: var(--hairline);
}
.home-project {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 12px;
  min-height: 108px;
  padding: 15px 16px;
  color: inherit;
  background: var(--surface);
  text-decoration: none;
}
.home-project:hover {
  background: var(--panel);
  text-decoration: none;
}
.home-project__icon {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border-radius: 8px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.home-project__copy {
  min-width: 0;
}
.home-project h3,
.home-project p {
  margin: 0;
}
.home-project p {
  margin-top: 3px;
  color: var(--muted);
}
.home-project small {
  display: block;
  margin-top: 9px;
  color: var(--muted);
}
.home-quick-actions,
.project-choice-list {
  display: grid;
  gap: 8px;
  padding: 14px 16px;
}
.home-quick-actions .button {
  justify-content: flex-start;
}
.project-choice {
  display: grid;
  gap: 4px;
  width: 100%;
  min-height: 58px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--text);
  background: var(--surface);
  text-align: left;
  cursor: pointer;
}
.project-choice span {
  color: var(--muted);
}
.home-empty-action {
  padding: 18px 0 4px;
  text-align: center;
}
.home-empty-actions {
  display: flex;
  justify-content: center;
  gap: 8px;
  flex-wrap: wrap;
}
@media (max-width: 980px) {
  .home-support-grid {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 620px) {
  .home-projects {
    grid-template-columns: 1fr;
  }
}
</style>
