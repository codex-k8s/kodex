<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Search, RefreshCw, Plus } from "@lucide/vue";
import { useRoute } from "vue-router";
import { storeToRefs } from "pinia";

import { usePlatformStore } from "@/features/platform/store";
import RunsBoard from "@/features/workboard/components/RunsBoard.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import { filterRuns, type RunFilter } from "@/features/workboard/model";
import { useRunCatalogStore } from "@/features/workboard/run-catalog";
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
const catalog = useRunCatalogStore();
const {
  items: scopedRuns,
  ready: runsReady,
  pageToken,
  loading,
  problem,
} = storeToRefs(catalog);
const projectReady = ref(!projectRef.value || Boolean(project.value));
const search = ref("");
const query = ref("");
let timer: ReturnType<typeof setTimeout> | undefined;
const list = computed(() =>
  filterRuns(
    scopedRuns.value.map((run) => {
      const fresh = platform.runs[run.ref];
      return fresh &&
        fresh.projectRef === run.projectRef &&
        fresh.version > run.version
        ? fresh
        : run;
    }),
    filter.value,
  ),
);

async function refreshRuns(): Promise<void> {
  await loadRuns();
}
async function loadRuns(more = false): Promise<void> {
  await catalog.load(
    { projectRef: projectRef.value, query: query.value, filter: filter.value },
    more,
  );
}

async function refreshProject(): Promise<void> {
  if (!projectRef.value) return;
  await platform.loadProject(projectRef.value);
  if (!platform.problems.project) projectReady.value = true;
}

const refreshing = computed(() => runsReady.value && loading.value);

async function refresh(): Promise<void> {
  await Promise.all([refreshRuns(), refreshProject()]);
}

watch(
  projectRef,
  (next) => {
    runsReady.value = false;
    projectReady.value = !next || Boolean(project.value);
    void refresh();
  },
  { immediate: true },
);
watch(search, () => {
  clearTimeout(timer);
  catalog.reset();
  timer = setTimeout(() => {
    query.value = search.value.trim();
    void refreshRuns();
  }, 500);
});
watch(filter, () => {
  clearTimeout(timer);
  query.value = search.value.trim();
  void refreshRuns();
});
watch(
  () =>
    Object.values(platform.runs)
      .filter((run) => !projectRef.value || run.projectRef === projectRef.value)
      .map((run) => `${run.ref}:${String(run.version)}`)
      .sort()
      .join("|"),
  () =>
    catalog.invalidate({
      projectRef: projectRef.value,
      query: query.value,
      filter: filter.value,
    }),
);
onBeforeUnmount(() => {
  clearTimeout(timer);
  catalog.reset();
});
</script>

<template>
  <PageFrame
    :title="$t('runs.title')"
    :subtitle="project?.name"
    :eyebrow="project ? $t('app.project') : undefined"
  >
    <template #actions>
      <RouterLink
        v-if="projectRef && canCreateRun"
        class="button button--primary"
        :to="`/projects/${projectRef}/runs/new`"
      >
        <Plus :size="18" />
        {{ $t("runs.new") }}
      </RouterLink>
    </template>

    <ProblemNotice
      v-if="projectRef && platform.problems.project && !projectReady"
      :problem="platform.problems.project"
      @retry="refreshProject"
    />

    <div class="runs-controls" role="group" :aria-label="$t('common.status')">
      <label class="runs-search"
        ><Search :size="18" /><input
          v-model="search"
          :aria-label="$t('runs.search')"
          :placeholder="$t('runs.search')"
      /></label>
      <button
        class="icon-button"
        :disabled="loading"
        :title="$t('common.refresh')"
        :aria-label="$t('common.refresh')"
        @click="refreshRuns"
      >
        <RefreshCw :size="18" />
      </button>
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
      :loading="loading"
      :refreshing="refreshing"
      :ready="runsReady"
      :problem="problem"
      :empty="list.length === 0"
      :empty-text="$t('workboard.noRuns')"
      @retry="refreshRuns"
    >
      <RunsBoard
        :runs="list"
        :has-more="Boolean(pageToken)"
        :loading-more="loading"
        @more="loadRuns(true)"
        :preserve-project="Boolean(projectRef)"
      />
    </WorkboardSection>
    <button
      v-if="pageToken"
      class="button"
      :disabled="loading"
      @click="loadRuns(true)"
    >
      {{ $t("common.loadMore") }}
    </button>
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
.runs-search {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 180px;
}
.runs-search input {
  width: 100%;
  min-width: 0;
}
.runs-controls .button {
  flex: 0 0 auto;
}
</style>
