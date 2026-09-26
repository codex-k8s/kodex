<script setup lang="ts">
import { Play } from "@lucide/vue";
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
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
import HomeResultCatalog from "@/features/home/components/HomeResultCatalog.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import { searchProjects } from "@/features/projects/api";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import type { ProviderAccount } from "@/shared/api/generated/openapi/types.gen";
import { listProviderAccounts } from "@/shared/api/generated/openapi/sdk.gen";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";
import { requestSignal } from "@/shared/api/client";
import { invalidSearchResult } from "@/shared/api/search-result";

const platform = usePlatformStore();
const router = useRouter();
const { t } = useI18n();
const projectAction = ref(false);
const overviewReady = ref(Boolean(platform.overview));
const projectsReady = ref(false);
const visibleProjects = ref<Project[]>([]);
const projectLoading = ref(false);
const projectProblem = ref<AppProblem>();
let projectController: AbortController | undefined;
const providerAccountsNeedingAuthorization = ref<ProviderAccount[]>([]);
const providerNextPageToken = ref<string>();
const providerProblem = ref<AppProblem>();
const providerMoreProblem = ref<AppProblem>();
const providerReady = ref(false);
const providerLoading = ref(false);
const providerLoadingMore = ref(false);
const consumedProviderCursors = new Set<string>();
let providerController: AbortController | undefined;
const runsReady = ref(platform.runList.length > 0);

const runCatalogTotal = ref<number>();
const sessionCatalogTotal = ref<number>();
const runsSettled = ref(false);
const artifactCatalogTotal = ref<number>();
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
    (platform.loading.overview && overviewReady.value) ||
    (projectLoading.value && projectsReady.value) ||
    (platform.loading.runs && runsReady.value) ||
    (providerLoading.value && providerReady.value) ||
    providerLoadingMore.value,
);
const showRuns = computed(() => runCatalogTotal.value !== 0);
const showSessions = computed(() => sessionCatalogTotal.value !== 0);
const showResults = computed(() => artifactCatalogTotal.value !== 0);
const singleBlock = computed(
  () => !showRuns.value && !showSessions.value && !showResults.value,
);

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

async function refreshOverview(): Promise<void> {
  await platform.loadOverview();
  if (!platform.problems.overview) overviewReady.value = true;
}

async function refreshProjects(): Promise<void> {
  projectController?.abort();
  const controller = new AbortController();
  projectController = controller;
  projectLoading.value = true;
  projectProblem.value = undefined;
  try {
    const page = await searchProjects("", undefined, controller.signal);
    if (controller.signal.aborted) return;
    if (new Set(page.items.map((item) => item.ref)).size !== page.items.length)
      throw invalidSearchResult();
    visibleProjects.value = page.items;
    projectsReady.value = true;
  } catch (error) {
    if (!controller.signal.aborted) projectProblem.value = asProblem(error);
  } finally {
    if (projectController === controller) projectLoading.value = false;
  }
}

async function refreshRuns(): Promise<void> {
  try {
    await platform.loadRuns();
    if (!platform.problems.runs) runsReady.value = true;
  } finally {
    runsSettled.value = true;
  }
}

function validateProviderAttentionPage(
  items: ProviderAccount[],
  existingRefs: ReadonlySet<string>,
  requestedCursor?: string,
  nextCursor?: string,
): void {
  const refs = items.map((item) => item.ref);
  if (
    items.some((item) => item.state !== "REAUTHORIZATION_REQUIRED") ||
    new Set(refs).size !== refs.length ||
    refs.some((ref) => existingRefs.has(ref)) ||
    (nextCursor !== undefined &&
      (nextCursor === requestedCursor ||
        consumedProviderCursors.has(nextCursor)))
  )
    throw invalidSearchResult();
}

async function refreshProviderAttention(pageSize = 6): Promise<void> {
  const role = platform.bootstrap?.platformRole;
  if (role !== "OWNER" && role !== "ADMINISTRATOR") {
    providerController?.abort();
    providerAccountsNeedingAuthorization.value = [];
    providerNextPageToken.value = undefined;
    providerProblem.value = undefined;
    providerMoreProblem.value = undefined;
    providerLoading.value = false;
    providerLoadingMore.value = false;
    providerReady.value = true;
    consumedProviderCursors.clear();
    return;
  }
  providerController?.abort();
  const controller = new AbortController();
  providerController = controller;
  providerLoading.value = true;
  providerLoadingMore.value = false;
  providerProblem.value = undefined;
  providerMoreProblem.value = undefined;
  consumedProviderCursors.clear();
  try {
    const page = (
      await unwrap(
        listProviderAccounts({
          query: { state: "REAUTHORIZATION_REQUIRED", pageSize },
          signal: requestSignal(controller.signal),
        }),
      )
    ).data;
    if (controller.signal.aborted) return;
    const nextCursor = page.nextPageToken || undefined;
    validateProviderAttentionPage(page.items, new Set(), undefined, nextCursor);
    providerAccountsNeedingAuthorization.value = page.items;
    providerNextPageToken.value = nextCursor;
    providerReady.value = true;
  } catch (error) {
    if (!controller.signal.aborted) providerProblem.value = asProblem(error);
  } finally {
    if (providerController === controller) providerLoading.value = false;
  }
}

async function loadMoreProviderAttention(pageSize: number): Promise<void> {
  const pageToken = providerNextPageToken.value;
  if (!pageToken || providerLoadingMore.value || providerMoreProblem.value)
    return;
  const controller = new AbortController();
  providerController = controller;
  providerLoadingMore.value = true;
  providerMoreProblem.value = undefined;
  try {
    const page = (
      await unwrap(
        listProviderAccounts({
          query: {
            state: "REAUTHORIZATION_REQUIRED",
            pageSize,
            pageToken,
          },
          signal: requestSignal(controller.signal),
        }),
      )
    ).data;
    if (controller.signal.aborted || providerNextPageToken.value !== pageToken)
      return;
    const nextCursor = page.nextPageToken || undefined;
    validateProviderAttentionPage(
      page.items,
      new Set(
        providerAccountsNeedingAuthorization.value.map(
          (account) => account.ref,
        ),
      ),
      pageToken,
      nextCursor,
    );
    consumedProviderCursors.add(pageToken);
    providerAccountsNeedingAuthorization.value = [
      ...providerAccountsNeedingAuthorization.value,
      ...page.items,
    ];
    providerNextPageToken.value = nextCursor;
  } catch (error) {
    if (!controller.signal.aborted)
      providerMoreProblem.value = asProblem(error);
  } finally {
    if (providerController === controller) providerLoadingMore.value = false;
  }
}

function retryMoreProviderAttention(pageSize: number): void {
  providerMoreProblem.value = undefined;
  void loadMoreProviderAttention(pageSize);
}

async function refresh(): Promise<void> {
  await Promise.all([
    refreshOverview(),
    refreshProjects(),
    refreshRuns(),
    refreshProviderAttention(),
  ]);
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
onMounted(() => void refresh());
watch(
  () => platform.bootstrap?.platformRole,
  (role, previous) => {
    if (role && role !== previous) void refreshProviderAttention();
  },
);
onBeforeUnmount(() => {
  projectController?.abort();
  providerController?.abort();
});
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
      :provider-next-page-token="providerNextPageToken"
      :projects="visibleProjects"
      :gates-ready="overviewReady"
      :runs-ready="runsReady"
      :provider-ready="providerReady"
      :gates-loading="platform.loading.overview"
      :runs-loading="platform.loading.runs"
      :provider-loading="providerLoading"
      :provider-loading-more="providerLoadingMore"
      :gates-problem="platform.problems.overview"
      :runs-problem="platform.problems.runs"
      :provider-problem="providerProblem"
      :provider-more-problem="providerMoreProblem"
      :refreshing="refreshing"
      @retry-gates="refreshOverview"
      @retry-runs="refreshRuns"
      @retry-providers="refreshProviderAttention"
      @more-providers="loadMoreProviderAttention"
      @retry-more-providers="retryMoreProviderAttention"
    />

    <div
      class="home-dashboard"
      :class="{ 'home-dashboard--single': singleBlock }"
    >
      <div class="home-dashboard__main">
        <HomeResultCatalog
          v-show="showRuns"
          kind="RUN"
          dashboard
          class="home-running-section"
          :ready="runsSettled"
          @total="runCatalogTotal = $event"
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
          :loading="projectLoading"
          :refreshing="refreshing"
          :ready="projectsReady"
          :problem="projectProblem"
          :empty="visibleProjects.length === 0"
          :empty-text="$t('projects.emptyText')"
          @retry="refreshProjects()"
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
  min-height: 38px;
  padding-inline: 16px;
}
.home-dashboard {
  display: grid;
  grid-template-columns: minmax(0, 1.7fr) minmax(320px, 0.95fr);
  align-items: start;
  gap: 16px;
  margin-top: 16px;
}
.home-dashboard--single {
  grid-template-columns: minmax(0, 1fr);
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
