<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import ArtifactList from "@/features/workboard/components/ArtifactList.vue";
import AttentionList from "@/features/workboard/components/AttentionList.vue";
import ProjectResources from "@/features/workboard/components/ProjectResources.vue";
import RunWorkItem from "@/features/workboard/components/RunWorkItem.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import { collectAttention, projectArtifacts } from "@/features/workboard/model";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const platform = usePlatformStore();
const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const projectReady = ref(Boolean(project.value));
const overviewReady = ref(false);
const runsReady = ref(false);

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
const refreshing = computed(
  () =>
    (platform.loading.project && projectReady.value) ||
    (platform.loading.overview && overviewReady.value) ||
    (platform.loading.runs && runsReady.value),
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

async function refresh(): Promise<void> {
  await Promise.all([refreshProject(), refreshOverview(), refreshRuns()]);
}

watch(
  projectRef,
  () => {
    projectReady.value = Boolean(project.value);
    overviewReady.value = false;
    runsReady.value = false;
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

    <WorkboardSection
      class="project-resources-section"
      :title="$t('workboard.resources')"
      :loading="platform.loading.project"
      :refreshing="refreshing"
      :ready="projectReady"
      :problem="platform.problems.project"
      :empty="!project"
      @retry="refreshProject"
    >
      <ProjectResources v-if="project" :project="project" />
    </WorkboardSection>

    <div class="project-focus-grid">
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
    </div>

    <WorkboardSection
      class="project-results-section"
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
  </PageFrame>
</template>

<style scoped>
.project-resources-section,
.project-results-section,
.project-focus-grid {
  margin-top: 16px;
}
.project-focus-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
@media (max-width: 980px) {
  .project-focus-grid {
    grid-template-columns: 1fr;
  }
}
</style>
