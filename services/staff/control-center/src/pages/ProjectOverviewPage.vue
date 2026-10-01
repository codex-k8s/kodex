<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import { useRuntimeStore } from "@/features/runtime/store";
import { useRealtimeStore } from "@/features/realtime/store";
import ArtifactList from "@/features/workboard/components/ArtifactList.vue";
import AttentionList from "@/features/workboard/components/AttentionList.vue";
import ProjectAgentList from "@/features/workboard/components/ProjectAgentList.vue";
import ProjectResources from "@/features/workboard/components/ProjectResources.vue";
import RunWorkItem from "@/features/workboard/components/RunWorkItem.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import {
  collectAttention,
  projectArtifacts,
  projectRuntimeEnvironments,
  projectSchedules,
} from "@/features/workboard/model";
import PageFrame from "@/shared/ui/PageFrame.vue";

const platform = usePlatformStore();
const runtime = useRuntimeStore();
const realtime = useRealtimeStore();
const route = useRoute();
const assistantForm = computed(() => route.query.assistantForm === "1");
const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const snapshot = (kind: Parameters<typeof platform.realtimeSnapshot>[0]) =>
  platform.realtimeSnapshot(kind, projectRef.value);
const projectReady = computed(() =>
  Boolean(snapshot("PROJECT") && project.value),
);
const overviewReady = computed(() => Boolean(snapshot("PROJECT")));
const runsReady = computed(() => Boolean(snapshot("RUN")));
const agentsReady = computed(() => Boolean(snapshot("AGENT")));
const schedulesReady = computed(() => Boolean(snapshot("SCHEDULE")));
const environmentsReady = computed(() =>
  Boolean(snapshot("RUNTIME_ENVIRONMENT")),
);
const environmentNextPageToken = computed(
  () => snapshot("RUNTIME_ENVIRONMENT")?.nextPageToken,
);

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
const projectAgents = computed(() =>
  Object.values(platform.agents)
    .filter(
      (agent) =>
        agent.projectRef === projectRef.value &&
        !agent.system &&
        agent.state !== "ARCHIVED",
    )
    .sort(
      (left, right) =>
        Number(right.enabled) - Number(left.enabled) ||
        left.name.localeCompare(right.name),
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
  projectRuntimeEnvironments(
    Object.values(runtime.environments),
    projectRef.value,
  ),
);
const refreshing = computed(
  () =>
    ["connecting", "recovering"].includes(realtime.platformState.state) &&
    [
      projectReady.value,
      overviewReady.value,
      runsReady.value,
      agentsReady.value,
      schedulesReady.value,
      environmentsReady.value,
    ].some(Boolean),
);
function refresh(): void {
  realtime.refreshSession();
}
</script>

<template>
  <Teleport to="#assistant-form-slot" :disabled="!assistantForm" defer>
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

      <div class="project-workboard">
        <div class="project-workboard__main">
          <WorkboardSection
            :title="$t('workboard.attention')"
            :count="attention.length"
            :loading="!overviewReady && !runsReady"
            :refreshing="refreshing"
            :ready="overviewReady || runsReady"
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
            :loading="!runsReady"
            :refreshing="refreshing"
            :ready="runsReady"
            :empty="activeRuns.length === 0"
            :empty-text="$t('workboard.noActiveRuns')"
            @retry="refresh"
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
            :loading="!overviewReady"
            :refreshing="refreshing"
            :ready="overviewReady"
            :empty="recentArtifacts.length === 0"
            :empty-text="$t('workboard.noRecentResults')"
            @retry="refresh"
          >
            <template #action>
              <RouterLink :to="`/projects/${projectRef}/files`">{{
                $t("workboard.allProjectFiles")
              }}</RouterLink>
            </template>
            <ArtifactList :artifacts="recentArtifacts" />
          </WorkboardSection>

          <WorkboardSection
            :title="$t('agents.title')"
            :count="projectAgents.length"
            :loading="!agentsReady"
            :refreshing="refreshing"
            :ready="agentsReady"
            :empty="projectAgents.length === 0"
            :empty-text="$t('agents.emptyTitle')"
            @retry="refresh"
          >
            <template #action>
              <RouterLink :to="`/projects/${projectRef}/agents`">{{
                $t("common.all")
              }}</RouterLink>
            </template>
            <ProjectAgentList :agents="projectAgents.slice(0, 8)" />
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
            :schedules-loading="!schedulesReady"
            :environments-loading="!environmentsReady"
            :schedules-unavailable="false"
            :environments-unavailable="false"
            :environments-truncated="Boolean(environmentNextPageToken)"
            @retry-schedules="refresh"
            @retry-environments="refresh"
          />
        </aside>
      </div>
    </PageFrame>
  </Teleport>
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
