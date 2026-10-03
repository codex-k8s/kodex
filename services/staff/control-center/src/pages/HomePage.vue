<script setup lang="ts">
import { Play } from "@lucide/vue";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import { openAssistantWorkspace } from "@/features/assistant/events";
import HomeAttentionCenter from "@/features/home/components/HomeAttentionCenter.vue";
import HomeProjectsList from "@/features/home/components/HomeProjectsList.vue";
import {
  homeFailedRuns,
  homeOpenGates,
  prioritizeHomeProjects,
} from "@/features/home/model";
import { usePlatformStore } from "@/features/platform/store";
import { useProvidersStore } from "@/features/providers/store";
import { useRealtimeStore } from "@/features/realtime/store";
import HomeResultCatalog from "@/features/home/components/HomeResultCatalog.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import { searchProjects } from "@/features/projects/api";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";

const platform = usePlatformStore();
const providers = useProvidersStore();
const realtime = useRealtimeStore();
const router = useRouter();
const { t } = useI18n();
const projectAction = ref(false);
const sessionCatalogTotal = ref<number>();
const artifactCatalogTotal = ref<number>();
const projectsReady = computed(() =>
  Boolean(platform.realtimeSnapshot("PROJECT")),
);
const overviewReady = projectsReady;
const runsReady = computed(() => Boolean(platform.realtimeSnapshot("RUN")));
const providerReady = computed(() =>
  Boolean(platform.realtimeSnapshot("PROVIDER_ACCOUNT")),
);
const runsSettled = runsReady;
const visibleProjects = computed(() => platform.projectList);
const providerAccountsNeedingAuthorization = computed(() =>
  providers.accounts.filter(
    (account) => account.state === "REAUTHORIZATION_REQUIRED",
  ),
);
const pendingGates = computed(() => platform.overview?.pendingGates ?? []);
const openGates = computed(() => homeOpenGates(pendingGates.value));
const failedRuns = computed(() =>
  homeFailedRuns(platform.runList, platform.runList.length),
);
const dashboardProjects = computed(() =>
  prioritizeHomeProjects(visibleProjects.value, 4),
);
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
    realtime.platformState.state === "connecting" ||
    realtime.platformState.state === "recovering",
);
const showSessions = computed(() => sessionCatalogTotal.value !== 0);
const showResults = computed(() => artifactCatalogTotal.value !== 0);

async function loadActionProjects(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  const page = await searchProjects(query, cursor, signal, pageSize);
  return {
    items: page.items.map((project) => ({
      ref: project.ref,
      title: project.name,
      description: project.purpose,
      meta: t(`states.${project.lifecycle}`),
      disabled: !project.nextActions.includes("CREATE_RUN"),
      disabledReason: project.nextActions.includes("CREATE_RUN")
        ? undefined
        : t("common.forbidden"),
    })),
    nextPageToken: page.nextPageToken,
  };
}

function retryRealtime(): void {
  realtime.refreshSession();
}

function projectActionPath(projectRef: string): string {
  return `/projects/${encodeURIComponent(projectRef)}/runs/new`;
}

async function chooseProject(projectRef: string): Promise<void> {
  const path = projectActionPath(projectRef);
  projectAction.value = false;
  await router.push(path);
}
function chooseActionProject(value: unknown): void {
  if (typeof value === "string") void chooseProject(value);
}
</script>

<template>
  <PageFrame
    class="home-page"
    :title="pageTitle"
    :subtitle="$t('home.subtitle')"
  >
    <template #actions>
      <button
        class="button button--primary"
        type="button"
        @click="projectAction = true"
      >
        <Play :size="16" aria-hidden="true" />
        {{ $t("home.newRun") }}
      </button>
    </template>

    <HomeAttentionCenter
      class="home-attention-section"
      :gates="openGates"
      :gates-count="platform.overview?.pendingGateCount"
      :failed-runs="failedRuns"
      :provider-accounts="providerAccountsNeedingAuthorization"
      :projects="visibleProjects"
      :gates-ready="overviewReady"
      :runs-ready="runsReady"
      :provider-ready="providerReady"
      :gates-loading="!overviewReady"
      :runs-loading="!runsReady"
      :provider-loading="!providerReady"
      :refreshing="refreshing"
      @retry-gates="retryRealtime"
      @retry-runs="retryRealtime"
      @retry-providers="retryRealtime"
    />

    <div class="home-dashboard">
      <div class="home-dashboard__main">
        <HomeResultCatalog
          kind="RUN"
          dashboard
          class="home-running-section"
          :ready="runsSettled"
        />

        <HomeResultCatalog
          v-show="showSessions"
          kind="SESSION"
          dashboard
          class="home-session-section"
          :ready="runsSettled"
          @total="sessionCatalogTotal = $event"
        />
      </div>

      <aside class="home-dashboard__aside">
        <WorkboardSection
          class="home-project-section"
          :title="$t('home.projects')"
          :count="platform.overview?.projectCount"
          :loading="!projectsReady"
          :refreshing="refreshing"
          :ready="projectsReady"
          :empty="visibleProjects.length === 0"
          :empty-text="$t('projects.emptyText')"
          @retry="retryRealtime"
        >
          <template #action>
            <RouterLink to="/projects">{{ $t("home.allProjects") }}</RouterLink>
          </template>
          <HomeProjectsList :items="dashboardProjects" dashboard />
        </WorkboardSection>

        <HomeResultCatalog
          v-show="showResults"
          kind="ARTIFACT"
          dashboard
          @total="artifactCatalogTotal = $event"
        />
      </aside>
    </div>

    <ModalDialog
      v-if="projectAction"
      :title="$t('home.chooseProject')"
      @close="projectAction = false"
    >
      <AsyncEntityPicker
        :load-page="loadActionProjects"
        :trigger-label="$t('home.chooseProject')"
        :placeholder="$t('home.chooseProject')"
        :search-placeholder="$t('common.search')"
        @update:model-value="chooseActionProject"
      />
      <div class="home-empty-action">
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
.home-page :deep(.page-header__actions .button--primary) {
  min-height: var(--control-height);
  padding-inline: 16px;
}
.home-dashboard {
  display: grid;
  grid-template-columns: minmax(0, 1.7fr) minmax(320px, 0.95fr);
  align-items: start;
  gap: 16px;
  margin-top: 16px;
}
.home-dashboard__main,
.home-dashboard__aside {
  display: grid;
  align-content: start;
  min-width: 0;
  gap: 16px;
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
@media (max-width: 1100px) {
  .home-dashboard {
    grid-template-columns: 1fr;
  }
}
</style>
