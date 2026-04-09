import { computed, ref } from "vue";
import { defineStore } from "pinia";

import {
  buildDrawerView,
  buildInitiativeViews,
  buildNodeViews,
  buildRelationViews,
  buildViewportForNodes,
} from "./presenters.ts";
import { MissionControlPrototypeSourceError, missionControlPrototypeSource } from "./source.ts";
import type {
  MissionCanvasScenario,
  MissionControlPrototypeCatalogItem,
  MissionControlPrototypeError,
  MissionControlPrototypeRouteState,
  MissionDrawerRecord,
  MissionDrawerTab,
  MissionWorkflowDraft,
  MissionWorkflowPreviewResult,
  MissionWorkflowPreset,
  MissionWorkflowPresetOption,
} from "./types.ts";

const missionControlPrototypeDefaultRouteState: MissionControlPrototypeRouteState = {
  scenarioId: "",
  initiativeId: "",
  nodeId: "",
  search: "",
  tab: "details",
};

function asUiError(error: unknown): MissionControlPrototypeError {
  if (error instanceof MissionControlPrototypeSourceError) {
    return error.uiError;
  }
  return {
    messageKey: "pages.missionControlPrototype.errors.unknown",
    debugMessage: error instanceof Error ? error.message : String(error),
  };
}

function cloneDraft(draft: MissionWorkflowDraft): MissionWorkflowDraft {
  return { ...draft };
}

function resolveWorkflowPresetOptions(scenario: MissionCanvasScenario | null): MissionWorkflowPresetOption[] {
  if (!scenario) {
    return [];
  }

  return scenario.workflowPresets.map((preset) => ({
    presetId: preset.presetId,
    label: preset.label,
    summary: preset.summary,
  }));
}

export const useMissionControlPrototypeStore = defineStore("missionControlPrototype", () => {
  const catalog = ref<MissionControlPrototypeCatalogItem[]>([]);
  const scenario = ref<MissionCanvasScenario | null>(null);
  const loading = ref(false);
  const drawerLoading = ref(false);
  const workflowLoading = ref(false);
  const error = ref<MissionControlPrototypeError | null>(null);
  const selectedNodeDetails = ref<MissionDrawerRecord | null>(null);
  const search = ref("");
  const focusedInitiativeId = ref<string | null>(null);
  const selectedNodeId = ref<string | null>(null);
  const drawerTab = ref<MissionDrawerTab>("details");
  const activeWorkflowPresetId = ref<string | null>(null);
  const workflowDraft = ref<MissionWorkflowDraft | null>(null);
  const workflowPreview = ref<MissionWorkflowPreviewResult | null>(null);
  const zoomLevel = ref(1);
  const panX = ref(0);
  const panY = ref(0);

  const defaultScenarioId = computed(() => catalog.value[0]?.scenarioId ?? "");
  const nodeViews = computed(() =>
    buildNodeViews(scenario.value, {
      search: search.value,
      focusedInitiativeId: focusedInitiativeId.value,
      selectedNodeId: selectedNodeId.value,
    }),
  );
  const relationViews = computed(() => buildRelationViews(scenario.value, nodeViews.value, selectedNodeId.value));
  const initiativeViews = computed(() => buildInitiativeViews(scenario.value, focusedInitiativeId.value));
  const drawerView = computed(() => buildDrawerView(scenario.value, selectedNodeId.value, selectedNodeDetails.value));
  const workflowPresetOptions = computed(() => resolveWorkflowPresetOptions(scenario.value));
  const activeWorkflowPreset = computed<MissionWorkflowPreset | null>(() => {
    if (!scenario.value || !activeWorkflowPresetId.value) {
      return null;
    }
    return scenario.value.workflowPresets.find((preset) => preset.presetId === activeWorkflowPresetId.value) ?? null;
  });
  const availableDrawerWorkflowPresetIds = computed(() => selectedNodeDetails.value?.workflowPresetIds ?? []);
  const availableWorkflowPresetOptions = computed(() => {
    if (availableDrawerWorkflowPresetIds.value.length === 0) {
      return workflowPresetOptions.value;
    }
    const allowed = new Set(availableDrawerWorkflowPresetIds.value);
    return workflowPresetOptions.value.filter((option) => allowed.has(option.presetId));
  });
  const sourceRefs = computed(() => scenario.value?.sourceRefs ?? []);
  const viewport = computed(() => ({
    zoomLevel: zoomLevel.value,
    panX: panX.value,
    panY: panY.value,
    canvasWidth: scenario.value?.defaultViewport.canvasWidth ?? 0,
    canvasHeight: scenario.value?.defaultViewport.canvasHeight ?? 0,
  }));
  const selectedNodeTitle = computed(() => drawerView.value?.title ?? "");

  async function ensureCatalog(): Promise<void> {
    if (catalog.value.length > 0) {
      return;
    }
    catalog.value = await missionControlPrototypeSource.loadCatalog();
  }

  function resolveScenarioId(requestedScenarioId: string): string {
    if (requestedScenarioId !== "" && catalog.value.some((item) => item.scenarioId === requestedScenarioId)) {
      return requestedScenarioId;
    }
    return defaultScenarioId.value;
  }

  function resetViewport(): void {
    if (!scenario.value) {
      return;
    }
    zoomLevel.value = scenario.value.defaultViewport.zoomLevel;
    panX.value = scenario.value.defaultViewport.panX;
    panY.value = scenario.value.defaultViewport.panY;
  }

  function fitViewport(): void {
    if (!scenario.value) {
      return;
    }

    const focusNodes =
      focusedInitiativeId.value !== null
        ? scenario.value.nodes.filter((node) => node.initiativeId === focusedInitiativeId.value)
        : scenario.value.nodes;
    const fittedViewport = buildViewportForNodes(focusNodes, scenario.value.defaultViewport);
    zoomLevel.value = fittedViewport.zoomLevel;
    panX.value = fittedViewport.panX;
    panY.value = fittedViewport.panY;
  }

  function zoomBy(delta: number): void {
    zoomLevel.value = Math.min(1.35, Math.max(0.68, Number((zoomLevel.value + delta).toFixed(2))));
  }

  function setSearch(nextSearch: string): void {
    search.value = nextSearch.trim();
  }

  function setFocusedInitiative(nextInitiativeId: string | null): void {
    if (!scenario.value) {
      focusedInitiativeId.value = null;
      return;
    }

    if (nextInitiativeId && scenario.value.initiatives.some((initiative) => initiative.initiativeId === nextInitiativeId)) {
      focusedInitiativeId.value = nextInitiativeId;
      fitViewport();
      return;
    }

    focusedInitiativeId.value = null;
  }

  async function loadScenario(nextScenarioId: string): Promise<void> {
    loading.value = true;
    error.value = null;

    try {
      const loadedScenario = await missionControlPrototypeSource.loadScenario({ scenarioId: nextScenarioId });
      scenario.value = loadedScenario;
      selectedNodeDetails.value = null;
      selectedNodeId.value = null;
      drawerTab.value = "details";
      activeWorkflowPresetId.value = loadedScenario.workflowPresets[0]?.presetId ?? null;
      workflowDraft.value = loadedScenario.workflowPresets[0]
        ? cloneDraft(loadedScenario.workflowPresets[0].defaultDraft)
        : null;
      workflowPreview.value = null;
      focusedInitiativeId.value = loadedScenario.defaultFocusInitiativeId || null;
      resetViewport();
    } catch (loadError) {
      error.value = asUiError(loadError);
    } finally {
      loading.value = false;
    }
  }

  async function selectNode(nextNodeId: string | null): Promise<void> {
    if (!scenario.value || nextNodeId === null || nextNodeId === "") {
      selectedNodeId.value = null;
      selectedNodeDetails.value = null;
      drawerTab.value = "details";
      workflowPreview.value = null;
      return;
    }

    drawerLoading.value = true;
    error.value = null;

    try {
      selectedNodeId.value = nextNodeId;
      selectedNodeDetails.value = await missionControlPrototypeSource.getNodeDetails({
        scenarioId: scenario.value.scenarioId,
        nodeId: nextNodeId,
      });

      const preferredPresetId =
        selectedNodeDetails.value.workflowPresetIds[0] ?? scenario.value.workflowPresets[0]?.presetId ?? null;
      if (preferredPresetId) {
        await selectWorkflowPreset(preferredPresetId);
      } else {
        activeWorkflowPresetId.value = null;
        workflowDraft.value = null;
        workflowPreview.value = null;
      }
    } catch (selectionError) {
      error.value = asUiError(selectionError);
      selectedNodeId.value = null;
      selectedNodeDetails.value = null;
    } finally {
      drawerLoading.value = false;
    }
  }

  async function regenerateWorkflowPreview(): Promise<void> {
    if (!scenario.value || !selectedNodeId.value || !activeWorkflowPresetId.value || !workflowDraft.value) {
      workflowPreview.value = null;
      return;
    }

    workflowLoading.value = true;

    try {
      workflowPreview.value = await missionControlPrototypeSource.generateWorkflowPreview({
        scenarioId: scenario.value.scenarioId,
        nodeId: selectedNodeId.value,
        presetId: activeWorkflowPresetId.value,
        draft: workflowDraft.value,
      });
    } catch (previewError) {
      error.value = asUiError(previewError);
      workflowPreview.value = null;
    } finally {
      workflowLoading.value = false;
    }
  }

  async function selectWorkflowPreset(presetId: string): Promise<void> {
    if (!scenario.value) {
      return;
    }
    const preset = scenario.value.workflowPresets.find((candidate) => candidate.presetId === presetId);
    if (!preset) {
      error.value = {
        messageKey: "pages.missionControlPrototype.errors.workflowPresetNotFound",
        debugMessage: `preset ${presetId} is not available in scenario ${scenario.value.scenarioId}`,
      };
      return;
    }

    activeWorkflowPresetId.value = presetId;
    workflowDraft.value = cloneDraft(preset.defaultDraft);
    await regenerateWorkflowPreview();
  }

  async function patchWorkflowDraft(patch: Partial<MissionWorkflowDraft>): Promise<void> {
    if (!workflowDraft.value || !activeWorkflowPreset.value) {
      return;
    }

    workflowDraft.value = {
      ...workflowDraft.value,
      ...patch,
    };
    await regenerateWorkflowPreview();
  }

  function setDrawerTab(nextTab: MissionDrawerTab): void {
    drawerTab.value = nextTab;
  }

  async function syncRouteState(
    routeState: MissionControlPrototypeRouteState,
  ): Promise<MissionControlPrototypeRouteState> {
    await ensureCatalog();

    const normalizedState: MissionControlPrototypeRouteState = {
      ...missionControlPrototypeDefaultRouteState,
      ...routeState,
      scenarioId: resolveScenarioId(routeState.scenarioId),
    };

    if (!scenario.value || scenario.value.scenarioId !== normalizedState.scenarioId) {
      await loadScenario(normalizedState.scenarioId);
    }

    setSearch(normalizedState.search);

    if (
      normalizedState.initiativeId !== "" &&
      scenario.value?.initiatives.some((initiative) => initiative.initiativeId === normalizedState.initiativeId)
    ) {
      focusedInitiativeId.value = normalizedState.initiativeId;
    } else {
      focusedInitiativeId.value = null;
      normalizedState.initiativeId = "";
    }

    if (!scenario.value) {
      return normalizedState;
    }

    if (normalizedState.nodeId !== "" && !scenario.value.nodes.some((node) => node.nodeId === normalizedState.nodeId)) {
      normalizedState.nodeId = "";
      normalizedState.tab = "details";
    }

    if (selectedNodeId.value !== normalizedState.nodeId) {
      await selectNode(normalizedState.nodeId || null);
    }

    if (normalizedState.nodeId === "") {
      normalizedState.tab = "details";
    }

    drawerTab.value = normalizedState.tab;

    return normalizedState;
  }

  return {
    catalog,
    scenario,
    loading,
    drawerLoading,
    workflowLoading,
    error,
    search,
    focusedInitiativeId,
    selectedNodeId,
    selectedNodeDetails,
    drawerTab,
    activeWorkflowPresetId,
    activeWorkflowPreset,
    workflowDraft,
    workflowPreview,
    viewport,
    sourceRefs,
    defaultScenarioId,
    nodeViews,
    relationViews,
    initiativeViews,
    drawerView,
    workflowPresetOptions,
    availableWorkflowPresetOptions,
    selectedNodeTitle,
    ensureCatalog,
    resolveScenarioId,
    loadScenario,
    setSearch,
    setFocusedInitiative,
    selectNode,
    setDrawerTab,
    selectWorkflowPreset,
    patchWorkflowDraft,
    zoomBy,
    fitViewport,
    resetViewport,
    syncRouteState,
  };
});
