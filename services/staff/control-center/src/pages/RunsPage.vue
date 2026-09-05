<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Search, RefreshCw, Plus } from "@lucide/vue";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import RunsBoard from "@/features/workboard/components/RunsBoard.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import { filterRuns, type RunFilter } from "@/features/workboard/model";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { listRuns } from "@/shared/api/generated/openapi/sdk.gen";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";

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
const runsReady = ref(false);
const projectReady = ref(!projectRef.value || Boolean(project.value));
const scopedRuns = ref<Run[]>([]);
const search = ref("");
const query = ref("");
const pageToken = ref<string>();
const loading = ref(false);
const problem = ref<AppProblem>();
const cursors = new Set<string>();
let controller: AbortController | undefined;
let generation = 0;
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
  if (more && (loading.value || !pageToken.value)) return;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  const current = ++generation;
  const project = projectRef.value;
  const cursor = more ? pageToken.value : undefined;
  loading.value = true;
  problem.value = undefined;
  if (!more) {
    scopedRuns.value = [];
    pageToken.value = undefined;
    cursors.clear();
  }
  try {
    const page = (
      await unwrap(
        listRuns({
          query: {
            projectRef: project,
            query: query.value,
            pageSize: 40,
            pageToken: cursor,
          },
          signal: requestSignal(active.signal),
        }),
      )
    ).data;
    if (current !== generation) return;
    if (
      !Array.isArray(page.items) ||
      page.items.some((run) => project && run.projectRef !== project) ||
      (page.nextPageToken &&
        (page.nextPageToken === cursor || cursors.has(page.nextPageToken)))
    )
      throw new Error("Invalid run catalog page");
    const items = more ? [...scopedRuns.value, ...page.items] : page.items;
    if (new Set(items.map((run) => run.ref)).size !== items.length)
      throw new Error("Repeated run catalog item");
    scopedRuns.value = items;
    for (const run of page.items) {
      const cached = platform.runs[run.ref];
      if (!cached || cached.version <= run.version)
        platform.runs[run.ref] = run;
    }
    pageToken.value = page.nextPageToken;
    if (page.nextPageToken) cursors.add(page.nextPageToken);
    runsReady.value = true;
  } catch (error) {
    if (current === generation && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
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
  controller?.abort();
  generation++;
  loading.value = false;
  pageToken.value = undefined;
  scopedRuns.value = [];
  timer = setTimeout(() => {
    query.value = search.value.trim();
    void refreshRuns();
  }, 500);
});
onBeforeUnmount(() => {
  controller?.abort();
  clearTimeout(timer);
  generation++;
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
