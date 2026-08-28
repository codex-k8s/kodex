<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { usePlatformStore } from "@/features/platform/store";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
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
const filter = ref<"ALL" | "ACTIVE" | "TERMINAL">("ALL");
const list = computed(() =>
  platform.runList
    .filter(
      (run) =>
        (!projectRef.value || run.projectRef === projectRef.value) &&
        (filter.value === "ALL" ||
          (filter.value === "ACTIVE"
            ? ["QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"].includes(
                run.state,
              )
            : ["SUCCEEDED", "FAILED", "CANCELLED"].includes(run.state))),
    )
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt)),
);
onMounted(
  () =>
    void Promise.all([
      platform.loadRuns(projectRef.value),
      ...(projectRef.value ? [platform.loadProject(projectRef.value)] : []),
    ]),
);
</script>
<template>
  <PageFrame :title="$t('runs.title')" :subtitle="$t('runs.subtitle')"
    ><template #actions
      ><RouterLink
        v-if="projectRef && canCreateRun"
        class="button button--primary"
        :to="`/projects/${projectRef}/runs/new`"
        >{{ $t("runs.new") }}</RouterLink
      ></template
    >
    <div class="filter-bar" role="group" :aria-label="$t('common.status')">
      <button
        v-for="value in ['ALL', 'ACTIVE', 'TERMINAL'] as const"
        :key="value"
        class="button"
        :class="{ 'button--primary': filter === value }"
        type="button"
        @click="filter = value"
      >
        {{
          value === "ALL"
            ? $t("common.all")
            : value === "ACTIVE"
              ? $t("common.active")
              : $t("common.result")
        }}
      </button>
    </div>
    <AsyncState
      :loading="platform.loading.runs"
      :problem="platform.problems.runs"
      :empty="list.length === 0"
      :empty-title="$t('common.empty')"
      @retry="platform.loadRuns(projectRef)"
      ><div class="entity-list">
        <RouterLink
          v-for="run in list"
          :key="run.ref"
          :to="`/runs/${run.ref}`"
          class="entity-row"
          ><div>
            <h3>{{ run.title }}</h3>
            <SafeSummary
              :content="run.currentActivity ?? run.resultSummary"
              :fallback="run.target.displayName"
            />
          </div>
          <StatusBadge :state="run.state" />
          <div class="run-meta">
            <span>{{ run.target.displayName }}</span
            ><small>{{ new Date(run.createdAt).toLocaleString() }}</small>
          </div></RouterLink
        >
      </div></AsyncState
    ></PageFrame
  >
</template>
<style scoped>
.filter-bar {
  display: flex;
  gap: 7px;
  margin-bottom: 16px;
}
.run-meta {
  display: flex;
  flex-direction: column;
  text-align: right;
  color: var(--muted);
}
@media (max-width: 700px) {
  .filter-bar {
    overflow-x: auto;
  }
  .run-meta {
    text-align: left;
  }
}
</style>
