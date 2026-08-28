<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import ArtifactList from "@/features/workboard/components/ArtifactList.vue";
import AttentionList from "@/features/workboard/components/AttentionList.vue";
import RunWorkItem from "@/features/workboard/components/RunWorkItem.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import { collectAttention, projectArtifacts } from "@/features/workboard/model";
import { useBackgroundRefresh } from "@/features/workboard/use-background-refresh";
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

const { refreshing } = useBackgroundRefresh(async () => {
  await Promise.all([refreshOverview(), refreshProjects()]);
});

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
      :title="$t('workboard.attention')"
      :count="attention.length"
      :loading="platform.loading.overview"
      :refreshing="refreshing"
      :ready="overviewReady"
      :problem="platform.problems.overview"
      :empty="attention.length === 0"
      :empty-text="$t('workboard.noAttention')"
      @retry="refreshOverview"
    >
      <template #action
        ><RouterLink to="/decisions">{{
          $t("common.all")
        }}</RouterLink></template
      >
      <AttentionList :items="attention" />
    </WorkboardSection>

    <div class="home-workboard">
      <div class="home-workboard__main">
        <WorkboardSection
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
          <template #action
            ><RouterLink to="/runs">{{
              $t("common.all")
            }}</RouterLink></template
          >
          <RunWorkItem v-for="run in activeRuns" :key="run.ref" :run="run" />
        </WorkboardSection>

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
      </div>

      <aside class="home-workboard__aside">
        <WorkboardSection
          :title="$t('home.projects')"
          :count="
            platform.overview?.projectCount ?? platform.projectList.length
          "
          :loading="platform.loading.projects"
          :refreshing="refreshing"
          :ready="projectsReady"
          :problem="platform.problems.projects"
          :empty="visibleProjects.length === 0"
          :empty-text="$t('projects.emptyText')"
          @retry="refreshProjects"
        >
          <template #action
            ><RouterLink to="/projects">{{
              $t("common.all")
            }}</RouterLink></template
          >
          <div class="home-projects">
            <RouterLink
              v-for="project in visibleProjects"
              :key="project.ref"
              :to="`/projects/${project.ref}`"
              class="home-project"
            >
              <div>
                <h3>{{ project.name }}</h3>
                <p>{{ project.purpose }}</p>
              </div>
              <StatusBadge :state="project.lifecycle" />
              <small>{{
                $t("workboard.projectActivity", {
                  runs: project.activeRunCount,
                  gates: project.pendingGateCount,
                })
              }}</small>
            </RouterLink>
          </div>
        </WorkboardSection>

        <WorkboardSection :title="$t('home.quickStart')" :ready="true">
          <div class="home-quick-actions">
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
        </WorkboardSection>
      </aside>
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
        <RouterLink class="button button--primary" to="/projects?create=1">{{
          $t("projects.new")
        }}</RouterLink>
      </div>
    </ModalDialog>
  </PageFrame>
</template>

<style scoped>
.home-workboard {
  display: grid;
  grid-template-columns: minmax(0, 1.55fr) minmax(290px, 0.75fr);
  gap: 16px;
  margin-top: 16px;
}
.home-workboard__main,
.home-workboard__aside {
  display: grid;
  align-content: start;
  gap: 16px;
}
.home-project {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 6px 12px;
  min-height: 72px;
  padding: 11px 16px;
  border-bottom: 1px solid var(--hairline);
  color: inherit;
  text-decoration: none;
}
.home-project:last-child {
  border-bottom: 0;
}
.home-project:hover {
  background: var(--panel);
  text-decoration: none;
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
  grid-column: 1 / -1;
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
@media (max-width: 980px) {
  .home-workboard {
    grid-template-columns: 1fr;
  }
}
</style>
