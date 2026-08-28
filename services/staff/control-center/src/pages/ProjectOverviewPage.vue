<script setup lang="ts">
import { computed, onMounted, watch } from "vue";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const canCreateAgent = computed(() =>
  project.value?.nextActions.includes("CREATE_AGENT"),
);
const canCreateWorkflow = computed(() =>
  project.value?.nextActions.includes("CREATE_WORKFLOW"),
);
const canCreateRun = computed(() =>
  project.value?.nextActions.includes("CREATE_RUN"),
);
const hasQuickActions = computed(
  () => canCreateAgent.value || canCreateWorkflow.value || canCreateRun.value,
);
const projectRuns = computed(() =>
  platform.runList.filter((run) => run.projectRef === projectRef.value),
);

async function load(): Promise<void> {
  await Promise.all([
    platform.loadProject(projectRef.value),
    platform.loadOverview(projectRef.value),
    platform.loadRuns(projectRef.value),
  ]);
}
watch(projectRef, () => void load());
onMounted(() => void load());
</script>

<template>
  <PageFrame
    :title="project?.name ?? $t('app.project')"
    :subtitle="project?.purpose ?? $t('project.subtitle')"
    :eyebrow="$t('app.project')"
  >
    <template v-if="canCreateRun" #actions
      ><RouterLink
        class="button button--primary"
        :to="`/projects/${projectRef}/runs/new`"
        >{{ $t("runs.new") }}</RouterLink
      ></template
    >
    <AsyncState
      :loading="platform.loading.project"
      :problem="platform.problems.project"
      @retry="load"
    >
      <section class="metric-grid">
        <article class="metric-card">
          <span>{{ $t("project.agents") }}</span
          ><strong>{{ project?.agentCount ?? 0 }}</strong>
        </article>
        <article class="metric-card">
          <span>{{ $t("project.workflows") }}</span
          ><strong>{{ project?.workflowCount ?? 0 }}</strong>
        </article>
        <article class="metric-card">
          <span>{{ $t("project.activeRuns") }}</span
          ><strong>{{ project?.activeRunCount ?? 0 }}</strong>
        </article>
        <article class="metric-card">
          <span>{{ $t("project.decisions") }}</span
          ><strong>{{ project?.pendingGateCount ?? 0 }}</strong>
        </article>
      </section>
      <section v-if="hasQuickActions" class="quick-actions panel">
        <h2>{{ $t("project.quickActions") }}</h2>
        <div>
          <RouterLink
            v-if="canCreateAgent"
            class="button"
            :to="`/projects/${projectRef}/agents`"
            >{{ $t("project.createAgent") }}</RouterLink
          ><RouterLink
            v-if="canCreateWorkflow"
            class="button"
            :to="`/projects/${projectRef}/workflows`"
            >{{ $t("project.createWorkflow") }}</RouterLink
          ><RouterLink
            v-if="canCreateRun"
            class="button button--primary"
            :to="`/projects/${projectRef}/runs/new`"
            >{{ $t("runs.new") }}</RouterLink
          >
        </div>
      </section>
      <div class="section-header">
        <h2>{{ $t("project.recentRuns") }}</h2>
        <RouterLink :to="`/projects/${projectRef}/runs`">{{
          $t("common.all")
        }}</RouterLink>
      </div>
      <div class="entity-list">
        <RouterLink
          v-for="run in projectRuns"
          :key="run.ref"
          :to="`/runs/${run.ref}`"
          class="entity-row"
          ><div>
            <h3>{{ run.title }}</h3>
            <SafeSummary
              :content="run.currentActivity"
              :fallback="run.target.displayName"
            />
          </div>
          <StatusBadge :state="run.state" /><span>{{
            $t(`runs.source.${run.source}`)
          }}</span></RouterLink
        >
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.quick-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.quick-actions h2 {
  margin: 0;
}
.quick-actions div {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
@media (max-width: 700px) {
  .quick-actions {
    align-items: stretch;
    flex-direction: column;
  }
  .quick-actions div {
    display: grid;
  }
}
</style>
