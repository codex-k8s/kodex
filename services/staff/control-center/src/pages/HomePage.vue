<script setup lang="ts">
import {
  Bot,
  Play,
  Upload,
  Workflow,
  MoreHorizontal,
  Maximize2,
  Search,
} from "@lucide/vue";
import DismissiblePopover from "@/shared/ui/DismissiblePopover.vue";
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import { openAssistantWorkspace } from "@/features/assistant/events";
import HomeAttentionCenter from "@/features/home/components/HomeAttentionCenter.vue";
import HomeGateCatalog from "@/features/home/components/HomeGateCatalog.vue";
import HomeProjectsList from "@/features/home/components/HomeProjectsList.vue";
import { homeFailedRuns, homeOpenGates } from "@/features/home/model";
import { usePlatformStore } from "@/features/platform/store";
import HomeResultCatalog from "@/features/home/components/HomeResultCatalog.vue";
import WorkboardSection from "@/features/workboard/components/WorkboardSection.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import { searchProjects } from "@/features/projects/api";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { invalidSearchResult } from "@/shared/api/search-result";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

type ProjectAction = "RUN" | "AGENT" | "WORKFLOW" | "FILE";

const platform = usePlatformStore();
const router = useRouter();
const { t } = useI18n();
const projectAction = ref<ProjectAction>();
const actionsOpen = ref(false);
const overviewReady = ref(Boolean(platform.overview));
const projectsReady = ref(false);
const visibleProjects = ref<Project[]>([]);
const projectQuery = ref("");
const projectCursor = ref<string>();
const projectLoading = ref(false);
const projectProblem = ref<AppProblem>();
const projectsExpanded = ref(false);
const projectCursors = new Set<string>();
let projectController: AbortController | undefined;
let projectTimer: ReturnType<typeof setTimeout> | undefined;
const runsReady = ref(platform.runList.length > 0);

const runCatalogTotal = ref<number>();
const sessionCatalogTotal = ref<number>();
const artifactCatalogTotal = ref<number>();
const failedCatalogTotal = ref<number>();
const pendingGates = computed(() => platform.overview?.pendingGates ?? []);
const openGates = computed(() => homeOpenGates(pendingGates.value));
const failedRuns = computed(() =>
  homeFailedRuns(platform.runList, platform.runList.length),
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
    (platform.loading.runs && runsReady.value),
);
const showRuns = computed(() => runCatalogTotal.value !== 0);
const showSessions = computed(() => sessionCatalogTotal.value !== 0);
const showResults = computed(() => artifactCatalogTotal.value !== 0);
const singleBlock = computed(
  () => !showRuns.value && !showSessions.value && !showResults.value,
);

const projectActionPermission = computed(() => {
  switch (projectAction.value) {
    case "AGENT":
      return "CREATE_AGENT";
    case "WORKFLOW":
      return "CREATE_WORKFLOW";
    case "FILE":
      return "UPLOAD_ARTIFACT";
    default:
      return "CREATE_RUN";
  }
});
async function loadActionProjects(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = await searchProjects(query, cursor, signal);
  return {
    items: page.items.map((project) => ({
      ref: project.ref,
      title: project.name,
      description: project.purpose,
      meta: t(`states.${project.lifecycle}`),
      disabled: !project.nextActions.includes(projectActionPermission.value),
      disabledReason: project.nextActions.includes(
        projectActionPermission.value,
      )
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

async function refreshProjects(append = false): Promise<void> {
  if (append && (!projectCursor.value || projectLoading.value)) return;
  projectController?.abort();
  const controller = new AbortController();
  projectController = controller;
  const cursor = append ? projectCursor.value : undefined;
  if (!append) projectCursors.clear();
  projectLoading.value = true;
  projectProblem.value = undefined;
  try {
    const page = await searchProjects(
      projectQuery.value,
      cursor,
      controller.signal,
    );
    if (controller.signal.aborted) return;
    if (
      (page.nextPageToken &&
        (page.nextPageToken === cursor ||
          projectCursors.has(page.nextPageToken))) ||
      new Set(page.items.map((item) => item.ref)).size !== page.items.length ||
      (append &&
        page.items.some((item) =>
          visibleProjects.value.some((existing) => existing.ref === item.ref),
        ))
    )
      throw invalidSearchResult();
    if (cursor) projectCursors.add(cursor);
    visibleProjects.value = append
      ? [...visibleProjects.value, ...page.items]
      : page.items;
    projectCursor.value = page.nextPageToken;
    projectsReady.value = true;
  } catch (error) {
    if (!controller.signal.aborted) projectProblem.value = asProblem(error);
  } finally {
    if (projectController === controller) projectLoading.value = false;
  }
}

async function refreshRuns(): Promise<void> {
  await platform.loadRuns();
  if (!platform.problems.runs) runsReady.value = true;
}

async function refresh(): Promise<void> {
  await Promise.all([refreshOverview(), refreshProjects(), refreshRuns()]);
}

function openProjectAction(action: ProjectAction): void {
  actionsOpen.value = false;
  projectAction.value = action;
}

function projectActionPath(projectRef: string): string {
  const prefix = `/projects/${encodeURIComponent(projectRef)}`;
  switch (projectAction.value) {
    case "AGENT":
      return `${prefix}/agents?create=1`;
    case "WORKFLOW":
      return `${prefix}/workflows?create=1`;
    case "FILE":
      return `${prefix}/files`;
    default:
      return `${prefix}/runs/new`;
  }
}

async function chooseProject(projectRef: string): Promise<void> {
  const path = projectActionPath(projectRef);
  projectAction.value = undefined;
  await router.push(path);
}
function chooseActionProject(value: unknown): void {
  if (typeof value === "string") void chooseProject(value);
}
function closeProjects(): void {
  projectsExpanded.value = false;
  projectQuery.value = "";
}

onMounted(() => void refresh());
watch(projectQuery, () => {
  projectController?.abort();
  if (projectTimer) clearTimeout(projectTimer);
  projectTimer = setTimeout(() => void refreshProjects(), 500);
});
onBeforeUnmount(() => {
  projectController?.abort();
  if (projectTimer) clearTimeout(projectTimer);
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
        @click="openProjectAction('RUN')"
      >
        <Play :size="16" aria-hidden="true" />
        {{ $t("home.launchWork") }}
      </button>
      <div class="home-primary-actions">
        <button
          class="button"
          type="button"
          @click="openProjectAction('AGENT')"
        >
          <Bot :size="16" aria-hidden="true" />{{ $t("project.createAgent") }}
        </button>
        <button
          class="button"
          type="button"
          @click="openProjectAction('WORKFLOW')"
        >
          <Workflow :size="16" aria-hidden="true" />{{
            $t("project.createWorkflow")
          }}
        </button>
        <button class="button" type="button" @click="openProjectAction('FILE')">
          <Upload :size="16" aria-hidden="true" />{{ $t("common.upload") }}
        </button>
      </div>
      <div class="home-mobile-actions">
        <DismissiblePopover
          v-model:open="actionsOpen"
          :ariaLabel="$t('common.actions')"
        >
          <template #trigger="{ toggle, attrs }"
            ><button
              v-bind="attrs"
              class="icon-button"
              type="button"
              :title="$t('common.actions')"
              :aria-label="$t('common.actions')"
              @click="toggle"
            >
              <MoreHorizontal :size="20" /></button
          ></template>
          <div class="home-action-menu">
            <button
              class="button"
              type="button"
              @click="openProjectAction('AGENT')"
            >
              <Bot :size="16" />{{ $t("project.createAgent") }}
            </button>
            <button
              class="button"
              type="button"
              @click="openProjectAction('WORKFLOW')"
            >
              <Workflow :size="16" />{{ $t("project.createWorkflow") }}
            </button>
            <button
              class="button"
              type="button"
              @click="openProjectAction('FILE')"
            >
              <Upload :size="16" />{{ $t("common.upload") }}
            </button>
          </div>
        </DismissiblePopover>
      </div>
    </template>

    <HomeAttentionCenter
      class="home-attention-section"
      :gates="openGates"
      :gates-count="platform.overview?.pendingGateCount"
      :failed-runs="failedRuns"
      :failed-runs-count="failedCatalogTotal"
      :projects="visibleProjects"
      :gates-ready="overviewReady"
      :runs-ready="runsReady"
      :gates-loading="platform.loading.overview"
      :runs-loading="platform.loading.runs"
      :gates-problem="platform.problems.overview"
      :runs-problem="platform.problems.runs"
      :refreshing="refreshing"
      @retry-gates="refreshOverview"
      @retry-runs="refreshRuns"
      ><template #gates><HomeGateCatalog /></template>
      <template #failed
        ><HomeResultCatalog
          v-show="failedCatalogTotal !== 0"
          kind="RUN"
          fixed-filter="FAILED"
          @total="failedCatalogTotal = $event"
      /></template>
    </HomeAttentionCenter>

    <div
      class="home-dashboard"
      :class="{ 'home-dashboard--single': singleBlock }"
    >
      <div class="home-dashboard__main">
        <HomeResultCatalog
          v-show="showRuns"
          kind="RUN"
          class="home-running-section"
          @total="runCatalogTotal = $event"
        />

        <HomeResultCatalog
          v-show="showSessions"
          kind="SESSION"
          class="home-session-section"
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
            <RouterLink to="/projects">{{ $t("common.all") }}</RouterLink>
            <button
              class="icon-button"
              :title="$t('catalog.expand')"
              :aria-label="$t('catalog.expand')"
              @click="projectsExpanded = true"
            >
              <Maximize2 :size="16" />
            </button>
          </template>
          <HomeProjectsList
            :items="visibleProjects"
            @more="refreshProjects(true)"
          />
          <button
            v-if="projectCursor"
            class="button"
            :disabled="projectLoading"
            @click="refreshProjects(true)"
          >
            {{ $t("providers.loadMore") }}
          </button>
        </WorkboardSection>

        <HomeResultCatalog
          v-show="showResults"
          kind="ARTIFACT"
          @total="artifactCatalogTotal = $event"
        />
      </aside>
    </div>

    <ModalDialog
      v-if="projectsExpanded"
      :title="$t('home.projects')"
      size="full"
      @close="closeProjects"
    >
      <label class="home-project-search"
        ><Search :size="16" /><span class="sr-only">{{
          $t("common.search")
        }}</span
        ><input
          v-model="projectQuery"
          type="search"
          :placeholder="$t('common.search')"
      /></label>
      <ProblemNotice
        v-if="projectProblem"
        :problem="projectProblem"
        @retry="refreshProjects()"
      />
      <HomeProjectsList
        :items="visibleProjects"
        expanded
        @more="refreshProjects(true)"
      />
      <button
        v-if="projectCursor"
        class="button"
        :disabled="projectLoading"
        @click="refreshProjects(true)"
      >
        {{ $t("providers.loadMore") }}
      </button>
      <p v-if="projectLoading" role="status">{{ $t("common.loading") }}</p>
    </ModalDialog>
    <ModalDialog
      v-if="projectAction"
      :title="$t('home.chooseProject')"
      @close="projectAction = undefined"
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
.home-bounded-runs {
  max-height: 552px;
  overflow: auto;
}
.home-bounded-results {
  max-height: 348px;
  overflow: auto;
}
.home-project-search {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
.home-project-search input {
  flex: 1;
  min-width: 0;
}
.home-primary-actions {
  display: flex;
  gap: 8px;
}
.home-mobile-actions {
  display: none;
}
.home-action-menu {
  display: grid;
  gap: 8px;
  padding: 12px;
}
@media (max-width: 1200px) {
  .home-primary-actions {
    display: none;
  }
  .home-mobile-actions {
    display: block;
  }
  .home-page :deep(.page-header__actions) {
    display: flex;
    flex-direction: row;
    flex-wrap: nowrap;
    align-items: center;
  }
  .home-page :deep(.page-header__actions > .button) {
    width: auto;
    flex: 1;
  }
  .home-mobile-actions {
    flex: 0 0 42px;
  }
}
.home-dashboard {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
  gap: 16px;
  margin-top: 16px;
}
.home-dashboard--single {
  grid-template-columns: minmax(0, 1fr);
}
.home-dashboard__main,
.home-dashboard__aside {
  display: contents;
}
.home-quick-actions,
.project-choice-list {
  display: grid;
  gap: 8px;
  padding: 14px 16px;
}
.home-quick-actions .button {
  justify-content: flex-start;
}
.project-choice {
  display: grid;
  gap: 4px;
  width: 100%;
  min-height: 58px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--text);
  background: var(--surface);
  text-align: left;
  cursor: pointer;
}
.project-choice span {
  color: var(--muted);
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
