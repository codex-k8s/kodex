<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import { useRuntimeStore } from "@/features/runtime/store";
import ArtifactList from "@/features/workboard/components/ArtifactList.vue";
import AttentionList from "@/features/workboard/components/AttentionList.vue";
import ProjectResources from "@/features/workboard/components/ProjectResources.vue";
import RunWorkItem from "@/features/workboard/components/RunWorkItem.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import {
  collectAttention,
  projectArtifacts,
  projectRuntimeEnvironments,
  projectSchedules,
} from "@/features/workboard/model";
import type { RuntimeEnvironmentSet } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const platform = usePlatformStore();
const runtime = useRuntimeStore();
const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const projectReady = ref(Boolean(project.value));
const overviewReady = ref(false);
const runsReady = ref(false);
const schedulesReady = ref(false);
const environmentsReady = ref(false);
const environmentsLoading = ref(false);
const environmentItems = ref<RuntimeEnvironmentSet[]>([]);
const environmentNextPageToken = ref<string>();
const environmentProblem = ref<AppProblem>();
let environmentGeneration = 0;

const canCreateRun = computed(() =>
  project.value?.nextActions.includes("CREATE_RUN"),
);
const projectRuns = computed(() =>
  platform.runList
    .filter((run) => run.projectRef === projectRef.value)
    .sort((left, right) => right.createdAt.localeCompare(left.createdAt)),
);
const activeRuns = computed(() =>
  projectRuns.value.filter((run) =>
    ["QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"].includes(run.state),
  ),
);
const pendingGates = computed(() =>
  (platform.overview?.pendingGates ?? []).filter(
    (gate) => gate.projectRef === projectRef.value,
  ),
);
const attention = computed(() =>
  collectAttention(projectRuns.value, pendingGates.value),
);
const recentArtifacts = computed(() =>
  projectArtifacts(platform.overview?.recentArtifacts ?? [], projectRef.value),
);
const schedules = computed(() =>
  projectSchedules(Object.values(platform.schedules), projectRef.value),
);
const environments = computed(() =>
  projectRuntimeEnvironments(environmentItems.value, projectRef.value),
);
const refreshing = computed(
  () =>
    (platform.loading.project && projectReady.value) ||
    (platform.loading.overview && overviewReady.value) ||
    (platform.loading.runs && runsReady.value) ||
    (platform.loading.schedules && schedulesReady.value) ||
    (environmentsLoading.value && environmentsReady.value),
);

async function refreshProject(): Promise<void> {
  await platform.loadProject(projectRef.value);
  if (!platform.problems.project) projectReady.value = true;
}
async function refreshOverview(): Promise<void> {
  await platform.loadOverview(projectRef.value);
  if (!platform.problems.overview) overviewReady.value = true;
}
async function refreshRuns(): Promise<void> {
  await platform.loadRuns(projectRef.value);
  if (!platform.problems.runs) runsReady.value = true;
}
async function refreshSchedules(): Promise<void> {
  await platform.loadSchedules(projectRef.value);
  if (!platform.problems.schedules) schedulesReady.value = true;
}
async function refreshEnvironments(): Promise<void> {
  const generation = ++environmentGeneration;
  const scope = projectRef.value;
  environmentsLoading.value = true;
  environmentProblem.value = undefined;
  try {
    const page = await runtime.searchEnvironmentPage(scope, "");
    if (generation !== environmentGeneration || scope !== projectRef.value)
      return;
    environmentItems.value = page.items;
    environmentNextPageToken.value = page.nextPageToken;
    environmentsReady.value = true;
  } catch (error) {
    if (generation === environmentGeneration)
      environmentProblem.value = asProblem(error);
  } finally {
    if (generation === environmentGeneration) environmentsLoading.value = false;
  }
}

async function refresh(): Promise<void> {
  await Promise.all([
    refreshProject(),
    refreshOverview(),
    refreshRuns(),
    refreshSchedules(),
    refreshEnvironments(),
  ]);
}

watch(
  projectRef,
  () => {
    projectReady.value = Boolean(project.value);
    overviewReady.value = false;
    runsReady.value = false;
    schedulesReady.value = false;
    environmentsReady.value = false;
    environmentItems.value = [];
    environmentNextPageToken.value = undefined;
    environmentProblem.value = undefined;
    environmentGeneration += 1;
    void refresh();
  },
  { immediate: true },
);
</script>

<template>
  <PageFrame
    :title="project?.name ?? $t('app.project')"
    :subtitle="project?.purpose ?? $t('project.subtitle')"
    :eyebrow="$t('app.project')"
  >
    <template v-if="canCreateRun" #actions>
      <RouterLink
        class="button button--primary"
        :to="`/projects/${projectRef}/runs/new`"
      >
        {{ $t("runs.new") }}
      </RouterLink>
    </template>

    <ProblemNotice
      v-if="platform.problems.project && !projectReady"
      :problem="platform.problems.project"
      @retry="refreshProject"
    />

    <div class="project-workboard">
      <div class="project-workboard__main">
        <WorkboardSection
          :title="$t('workboard.attention')"
          :count="attention.length"
          :loading="platform.loading.overview || platform.loading.runs"
          :refreshing="refreshing"
          :ready="overviewReady || runsReady"
          :problem="platform.problems.overview ?? platform.problems.runs"
          :empty="attention.length === 0"
          :empty-text="$t('workboard.noAttention')"
          @retry="refresh"
        >
          <template #action>
            <RouterLink
              :to="`/decisions?projectRef=${encodeURIComponent(projectRef)}`"
              >{{ $t("common.all") }}</RouterLink
            >
          </template>
          <AttentionList :items="attention" preserve-project />
        </WorkboardSection>

        <WorkboardSection
          :title="$t('workboard.runningNow')"
          :count="activeRuns.length"
          :loading="platform.loading.runs"
          :refreshing="refreshing"
          :ready="runsReady"
          :problem="platform.problems.runs"
          :empty="activeRuns.length === 0"
          :empty-text="$t('workboard.noActiveRuns')"
          @retry="refreshRuns"
        >
          <template #action>
            <RouterLink :to="`/projects/${projectRef}/runs`">{{
              $t("workboard.allProjectRuns")
            }}</RouterLink>
          </template>
          <RunWorkItem
            v-for="run in activeRuns.slice(0, 8)"
            :key="run.ref"
            :run="run"
            preserve-project
          />
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
          <template #action>
            <RouterLink :to="`/projects/${projectRef}/files`">{{
              $t("workboard.allProjectFiles")
            }}</RouterLink>
          </template>
          <ArtifactList :artifacts="recentArtifacts" />
        </WorkboardSection>
      </div>

      <aside
        v-if="project"
        class="project-workboard__resources"
        :aria-label="$t('workboard.resources')"
      >
        <ProjectResources
          :project="project"
          :schedules="schedules"
          :environments="environments"
          :schedules-ready="schedulesReady"
          :environments-ready="environmentsReady"
          :schedules-loading="platform.loading.schedules"
          :environments-loading="environmentsLoading"
          :schedules-unavailable="Boolean(platform.problems.schedules)"
          :environments-unavailable="Boolean(environmentProblem)"
          :environments-truncated="Boolean(environmentNextPageToken)"
          @retry-schedules="refreshSchedules"
          @retry-environments="refreshEnvironments"
        />
      </aside>
    </div>
  </PageFrame>
</template>

<style scoped>
.project-workboard {
  display: grid;
  grid-template-columns: minmax(0, 2.15fr) minmax(300px, 0.85fr);
  align-items: start;
  gap: 16px;
  margin-top: 16px;
}
.project-workboard__main {
  display: grid;
  gap: 16px;
}
@media (max-width: 980px) {
  .project-workboard {
    grid-template-columns: 1fr;
  }
}
</style>
