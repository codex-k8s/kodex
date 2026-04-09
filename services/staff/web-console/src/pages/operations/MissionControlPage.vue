<template>
  <div class="mission-control-prototype-page">
    <PageHeader
      :title="t('pages.missionControlPrototype.title')"
      :hint="t('pages.missionControlPrototype.hint')"
    />

    <VAlert v-if="prototype.error" type="error" variant="tonal" class="mt-4">
      {{ t(prototype.error.messageKey) }}
    </VAlert>

    <VCard class="mt-4 mission-control-prototype-shell" rounded="xl" variant="flat">
      <MissionControlPrototypeToolbar
        :catalog="prototype.catalog"
        :active-scenario-id="activeRouteState.scenarioId"
        :initiatives="prototype.initiativeViews"
        :focused-initiative-id="prototype.focusedInitiativeId"
        :search="prototype.search"
        :selected-node-title="prototype.selectedNodeTitle"
        :zoom-label="zoomLabel"
        @select-scenario="onSelectScenario"
        @select-initiative="onSelectInitiative"
        @clear-initiative="onClearInitiative"
        @update-search="onUpdateSearch"
        @zoom-in="prototype.zoomBy(0.08)"
        @zoom-out="prototype.zoomBy(-0.08)"
        @fit="prototype.fitViewport()"
        @reset="prototype.resetViewport()"
        @open-workflow="onOpenWorkflow"
      />

      <div class="mission-control-prototype-shell__summary">
        <div class="mission-control-prototype-shell__summary-copy">
          <div class="mission-control-prototype-shell__summary-title">{{ prototype.scenario?.title }}</div>
          <div class="mission-control-prototype-shell__summary-text">{{ prototype.scenario?.summary }}</div>
        </div>

        <div class="mission-control-prototype-shell__metrics">
          <VChip size="small" variant="tonal" color="primary">
            {{ t("pages.missionControlPrototype.summary.initiatives", { count: prototype.scenario?.initiatives.length ?? 0 }) }}
          </VChip>
          <VChip size="small" variant="tonal" color="info">
            {{ t("pages.missionControlPrototype.summary.nodes", { count: prototype.scenario?.nodes.length ?? 0 }) }}
          </VChip>
          <VChip size="small" variant="tonal" color="warning">
            {{ t("pages.missionControlPrototype.summary.visible", { count: visibleNodeCount }) }}
          </VChip>
          <VChip size="small" variant="tonal" color="success">
            {{ t("pages.missionControlPrototype.summary.prototypeOnly") }}
          </VChip>
        </div>
      </div>

      <div class="mission-control-prototype-shell__body">
        <div class="mission-control-prototype-shell__canvas-pane">
          <div class="mission-control-prototype-shell__refs">
            <VChip v-for="sourceRef in prototype.sourceRefs" :key="sourceRef" size="small" variant="outlined">
              {{ sourceRef }}
            </VChip>
          </div>

          <div v-if="prototype.loading" class="mission-control-prototype-shell__loading">
            <VSkeletonLoader type="article, article, article" />
          </div>

          <MissionControlPrototypeCanvas
            v-else
            :initiatives="prototype.initiativeViews"
            :nodes="prototype.nodeViews"
            :relations="prototype.relationViews"
            :viewport="prototype.viewport"
            @select-node="onSelectNode"
          />
        </div>

        <aside v-if="!display.mobile.value" class="mission-control-prototype-shell__drawer">
          <MissionControlPrototypeDrawer
            :drawer="prototype.drawerView"
            :tab="activeRouteState.tab"
            :drawer-loading="prototype.drawerLoading"
            :workflow-loading="prototype.workflowLoading"
            :scenario-source-refs="prototype.sourceRefs"
            :workflow-preset-options="prototype.availableWorkflowPresetOptions"
            :active-workflow-preset-id="prototype.activeWorkflowPresetId"
            :workflow-draft="prototype.workflowDraft"
            :workflow-preview="prototype.workflowPreview"
            :error="prototype.error"
            @close="onCloseDrawer"
            @update-tab="onUpdateTab"
            @select-node="onSelectNode"
            @select-workflow-preset="prototype.selectWorkflowPreset"
            @patch-workflow-draft="prototype.patchWorkflowDraft"
          />
        </aside>
      </div>
    </VCard>

    <VDialog v-model="mobileDrawerOpen" fullscreen transition="dialog-bottom-transition">
      <VCard class="mission-control-prototype-mobile-drawer">
        <MissionControlPrototypeDrawer
          :drawer="prototype.drawerView"
          :tab="activeRouteState.tab"
          :drawer-loading="prototype.drawerLoading"
          :workflow-loading="prototype.workflowLoading"
          :scenario-source-refs="prototype.sourceRefs"
          :workflow-preset-options="prototype.availableWorkflowPresetOptions"
          :active-workflow-preset-id="prototype.activeWorkflowPresetId"
          :workflow-draft="prototype.workflowDraft"
          :workflow-preview="prototype.workflowPreview"
          :error="prototype.error"
          @close="onCloseDrawer"
          @update-tab="onUpdateTab"
          @select-node="onSelectNode"
          @select-workflow-preset="prototype.selectWorkflowPreset"
          @patch-workflow-draft="prototype.patchWorkflowDraft"
        />
      </VCard>
    </VDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { useDisplay } from "vuetify";

import MissionControlPrototypeCanvas from "../../features/mission-control/prototype/MissionControlPrototypeCanvas.vue";
import MissionControlPrototypeDrawer from "../../features/mission-control/prototype/MissionControlPrototypeDrawer.vue";
import MissionControlPrototypeToolbar from "../../features/mission-control/prototype/MissionControlPrototypeToolbar.vue";
import {
  buildMissionControlPrototypeRouteQuery,
  missionControlPrototypeRouteStateEquals,
  normalizeMissionControlPrototypeRouteQuery,
  patchMissionControlPrototypeRouteState,
} from "../../features/mission-control/prototype/route";
import { useMissionControlPrototypeStore } from "../../features/mission-control/prototype/store";
import type { MissionControlPrototypeRouteState, MissionDrawerTab } from "../../features/mission-control/prototype/types";
import PageHeader from "../../shared/ui/PageHeader.vue";

const route = useRoute();
const router = useRouter();
const display = useDisplay();
const prototype = useMissionControlPrototypeStore();
const { t } = useI18n({ useScope: "global" });

const mobileDrawerOpen = ref(false);
const activeRouteState = ref<MissionControlPrototypeRouteState>({
  scenarioId: "",
  initiativeId: "",
  nodeId: "",
  search: "",
  tab: "details",
});

const routeState = computed(() => normalizeMissionControlPrototypeRouteQuery(route.query));
const visibleNodeCount = computed(() => prototype.nodeViews.filter((node) => !node.dimmed).length);
const zoomLabel = computed(() => `${Math.round(prototype.viewport.zoomLevel * 100)}%`);

watch(
  routeState,
  async (nextState) => {
    const normalizedState = await prototype.syncRouteState(nextState);
    activeRouteState.value = normalizedState;

    if (!missionControlPrototypeRouteStateEquals(nextState, normalizedState)) {
      await replaceRoute(normalizedState);
    }

    mobileDrawerOpen.value = display.mobile.value && normalizedState.nodeId !== "";
  },
  { immediate: true, deep: true },
);

watch(
  () => display.mobile.value,
  (isMobile) => {
    mobileDrawerOpen.value = isMobile && activeRouteState.value.nodeId !== "";
  },
  { immediate: true },
);

async function replaceRoute(nextState: MissionControlPrototypeRouteState): Promise<void> {
  const defaultScenarioId = prototype.defaultScenarioId || nextState.scenarioId;
  await router.replace({
    name: "mission-control",
    query: buildMissionControlPrototypeRouteQuery(nextState, {
      scenarioId: defaultScenarioId,
    }),
  });
}

function updateRoute(patch: Partial<MissionControlPrototypeRouteState>): void {
  const nextState = patchMissionControlPrototypeRouteState(activeRouteState.value, patch);
  if (missionControlPrototypeRouteStateEquals(activeRouteState.value, nextState)) {
    return;
  }
  void replaceRoute(nextState);
}

function onSelectScenario(scenarioId: string): void {
  updateRoute({
    scenarioId,
    initiativeId: "",
    nodeId: "",
    search: "",
    tab: "details",
  });
}

function onSelectInitiative(initiativeId: string): void {
  updateRoute({
    initiativeId,
    nodeId: "",
    tab: "details",
  });
}

function onClearInitiative(): void {
  updateRoute({
    initiativeId: "",
    nodeId: "",
    tab: "details",
  });
}

function onUpdateSearch(search: string): void {
  updateRoute({
    search,
  });
}

function onSelectNode(nodeId: string): void {
  updateRoute({
    nodeId,
    tab: activeRouteState.value.tab,
  });
}

function onCloseDrawer(): void {
  updateRoute({
    nodeId: "",
    tab: "details",
  });
}

function onUpdateTab(tab: MissionDrawerTab): void {
  updateRoute({
    tab,
  });
}

function onOpenWorkflow(): void {
  if (!activeRouteState.value.nodeId) {
    return;
  }
  updateRoute({
    tab: "workflow",
  });
}
</script>

<style scoped>
.mission-control-prototype-page {
  display: flex;
  flex-direction: column;
}

.mission-control-prototype-shell {
  overflow: hidden;
  border: 1px solid rgba(148, 163, 184, 0.18);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.98), rgba(248, 250, 252, 0.98)),
    radial-gradient(circle at top left, rgba(217, 119, 6, 0.12), transparent 32%);
}

.mission-control-prototype-shell__summary {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 18px 20px;
  border-bottom: 1px solid rgba(148, 163, 184, 0.14);
  background: rgba(255, 255, 255, 0.9);
}

.mission-control-prototype-shell__summary-copy {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.mission-control-prototype-shell__summary-title {
  font-size: 1.08rem;
  font-weight: 800;
  color: rgb(15, 23, 42);
}

.mission-control-prototype-shell__summary-text {
  max-width: 820px;
  color: rgb(71, 85, 105);
  line-height: 1.6;
}

.mission-control-prototype-shell__metrics {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.mission-control-prototype-shell__body {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 420px;
  min-height: calc(100vh - 270px);
}

.mission-control-prototype-shell__canvas-pane {
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: rgba(248, 250, 252, 0.7);
}

.mission-control-prototype-shell__refs {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  padding: 16px 20px 0;
}

.mission-control-prototype-shell__loading {
  padding: 24px;
}

.mission-control-prototype-shell__drawer {
  border-left: 1px solid rgba(148, 163, 184, 0.14);
  min-height: 100%;
  background: rgba(255, 255, 255, 0.9);
}

.mission-control-prototype-mobile-drawer {
  min-height: 100%;
}

@media (max-width: 1180px) {
  .mission-control-prototype-shell__body {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 960px) {
  .mission-control-prototype-shell__summary {
    flex-direction: column;
  }

  .mission-control-prototype-shell__metrics {
    justify-content: flex-start;
  }
}
</style>
