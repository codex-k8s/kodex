<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import RunsBoard from "@/features/workboard/components/RunsBoard.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import {
  filterRuns,
  type RunFilter,
  type RunView,
} from "@/features/workboard/model";
import { useBackgroundRefresh } from "@/features/workboard/use-background-refresh";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const platform = usePlatformStore();
const route = useRoute();
const projectRef = computed(() =>
  typeof route.params.projectRef === "string"
    ? route.params.projectRef
    : undefined,
);
const project = computed(() =>
  projectRef.value ? platform.projects[projectRef.value] : undefined,
);
const canCreateRun = computed(() =>
  project.value?.nextActions.includes("CREATE_RUN"),
);
const filter = ref<RunFilter>("ALL");
const view = ref<RunView>(projectRef.value ? "KANBAN" : "LIST");
const runsReady = ref(false);
const projectReady = ref(!projectRef.value || Boolean(project.value));
const scopedRuns = computed(() =>
  platform.runList.filter(
    (run) => !projectRef.value || run.projectRef === projectRef.value,
  ),
);
const list = computed(() => filterRuns(scopedRuns.value, filter.value));

async function refreshRuns(): Promise<void> {
  await platform.loadRuns(projectRef.value);
  if (!platform.problems.runs) runsReady.value = true;
}

async function refreshProject(): Promise<void> {
  if (!projectRef.value) return;
  await platform.loadProject(projectRef.value);
  if (!platform.problems.project) projectReady.value = true;
}

const { refreshing, run: refresh } = useBackgroundRefresh(async () => {
  await Promise.all([refreshRuns(), refreshProject()]);
});

watch(projectRef, (next) => {
  runsReady.value = false;
  projectReady.value = !next || Boolean(project.value);
  view.value = next ? "KANBAN" : "LIST";
  void refresh();
});
</script>

<template>
  <PageFrame
    :title="$t('runs.title')"
    :subtitle="project?.name ?? $t('runs.subtitle')"
    :eyebrow="project ? $t('app.project') : undefined"
  >
    <template #actions>
      <RouterLink
        v-if="projectRef && canCreateRun"
        class="button button--primary"
        :to="`/projects/${projectRef}/runs/new`"
      >
        {{ $t("runs.new") }}
      </RouterLink>
    </template>

    <ProblemNotice
      v-if="projectRef && platform.problems.project && !projectReady"
      :problem="platform.problems.project"
      @retry="refreshProject"
    />

    <div class="runs-controls" role="group" :aria-label="$t('common.status')">
      <button
        v-for="value in ['ALL', 'ACTIVE', 'TERMINAL'] as const"
        :key="value"
        class="button"
        :class="{ 'button--primary': filter === value }"
        type="button"
        :aria-pressed="filter === value"
        @click="filter = value"
      >
        {{ $t(`workboard.filters.${value}`) }}
      </button>
    </div>

    <WorkboardSection
      :title="project ? $t('workboard.projectRuns') : $t('runs.title')"
      :count="list.length"
      :loading="platform.loading.runs"
      :refreshing="refreshing"
      :ready="runsReady"
      :problem="platform.problems.runs"
      :empty="list.length === 0"
      :empty-text="$t('workboard.noRuns')"
      @retry="refreshRuns"
    >
      <RunsBoard
        v-model:view="view"
        :runs="list"
        :preserve-project="Boolean(projectRef)"
      />
    </WorkboardSection>
  </PageFrame>
</template>

<style scoped>
.runs-controls {
  display: flex;
  gap: 7px;
  margin-bottom: 16px;
  overflow-x: auto;
  padding-bottom: 2px;
}
.runs-controls .button {
  flex: 0 0 auto;
}
</style>
