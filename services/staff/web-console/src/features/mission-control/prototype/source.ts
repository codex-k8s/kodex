import { missionControlPrototypeScenarios } from "./fixtures.ts";
import type {
  MissionCanvasNode,
  MissionCanvasScenario,
  MissionControlPrototypeCatalogItem,
  MissionControlPrototypeError,
  MissionDrawerRecord,
  MissionWorkflowDraft,
  MissionWorkflowPreviewResult,
  MissionWorkflowPreset,
} from "./types.ts";

type MissionScenarioMaps = {
  scenario: MissionCanvasScenario;
  nodeById: Map<string, MissionCanvasNode>;
  drawerByNodeId: Map<string, MissionDrawerRecord>;
  presetById: Map<string, MissionWorkflowPreset>;
};

type MissionControlPrototypeSource = {
  loadCatalog(): Promise<MissionControlPrototypeCatalogItem[]>;
  loadScenario(input: { scenarioId: string }): Promise<MissionCanvasScenario>;
  getNodeDetails(input: { scenarioId: string; nodeId: string }): Promise<MissionDrawerRecord>;
  generateWorkflowPreview(input: {
    scenarioId: string;
    nodeId: string;
    presetId: string;
    draft: MissionWorkflowDraft;
  }): Promise<MissionWorkflowPreviewResult>;
};

export class MissionControlPrototypeSourceError extends Error {
  readonly uiError: MissionControlPrototypeError;

  constructor(messageKey: string, debugMessage?: string) {
    super(debugMessage || messageKey);
    this.name = "MissionControlPrototypeSourceError";
    this.uiError = {
      messageKey,
      debugMessage,
    };
  }
}

function cloneFixture<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function buildCatalogItem(scenario: MissionCanvasScenario): MissionControlPrototypeCatalogItem {
  return {
    scenarioId: scenario.scenarioId,
    title: scenario.title,
    summary: scenario.summary,
    initiativeCount: scenario.initiatives.length,
    nodeCount: scenario.nodes.length,
    defaultFocusInitiativeId: scenario.defaultFocusInitiativeId,
  };
}

const scenarioMapsById = new Map<string, MissionScenarioMaps>(
  missionControlPrototypeScenarios.map((scenario) => [
    scenario.scenarioId,
    {
      scenario,
      nodeById: new Map(scenario.nodes.map((node) => [node.nodeId, node])),
      drawerByNodeId: new Map(scenario.drawerRecords.map((record) => [record.nodeId, record])),
      presetById: new Map(
        (scenario.workflowPresets.length > 0
          ? scenario.workflowPresets
          : missionControlPrototypeScenarios[0]?.workflowPresets ?? []
        ).map((preset) => [preset.presetId, preset]),
      ),
    },
  ]),
);

const missionControlPrototypeCatalog = missionControlPrototypeScenarios.map(buildCatalogItem);

function getScenarioMapsOrThrow(scenarioId: string): MissionScenarioMaps {
  const maps = scenarioMapsById.get(scenarioId);
  if (!maps) {
    throw new MissionControlPrototypeSourceError(
      "pages.missionControlPrototype.errors.scenarioNotFound",
      `scenario ${scenarioId} was not found in the prototype catalog`,
    );
  }
  return maps;
}

function getNodeOrThrow(maps: MissionScenarioMaps, nodeId: string): MissionCanvasNode {
  const node = maps.nodeById.get(nodeId);
  if (!node) {
    throw new MissionControlPrototypeSourceError(
      "pages.missionControlPrototype.errors.nodeNotFound",
      `node ${nodeId} was not found in scenario ${maps.scenario.scenarioId}`,
    );
  }
  return node;
}

function getDrawerOrThrow(maps: MissionScenarioMaps, nodeId: string): MissionDrawerRecord {
  const drawerRecord = maps.drawerByNodeId.get(nodeId);
  if (!drawerRecord) {
    throw new MissionControlPrototypeSourceError(
      "pages.missionControlPrototype.errors.nodeNotFound",
      `drawer record for node ${nodeId} was not found in scenario ${maps.scenario.scenarioId}`,
    );
  }
  return drawerRecord;
}

function getPresetOrThrow(maps: MissionScenarioMaps, presetId: string): MissionWorkflowPreset {
  const preset = maps.presetById.get(presetId);
  if (!preset) {
    throw new MissionControlPrototypeSourceError(
      "pages.missionControlPrototype.errors.workflowPresetNotFound",
      `preset ${presetId} was not found in scenario ${maps.scenario.scenarioId}`,
    );
  }
  return preset;
}

function resolveStageSequence(preset: MissionWorkflowPreset, draft: MissionWorkflowDraft): string[] {
  const sequence = preset.stageSequenceOptions[draft.stageSequenceVariant];
  if (!sequence) {
    throw new MissionControlPrototypeSourceError(
      "pages.missionControlPrototype.errors.invalidWorkflowDraft",
      `stage sequence variant ${draft.stageSequenceVariant} is not allowed for preset ${preset.presetId}`,
    );
  }
  return sequence;
}

function buildChangeExplanations(preset: MissionWorkflowPreset, draft: MissionWorkflowDraft): string[] {
  const changes: string[] = [];
  const baseline = preset.defaultDraft;

  if (baseline.stageSequenceVariant !== draft.stageSequenceVariant) {
    changes.push(`stage_sequence_variant: ${baseline.stageSequenceVariant} -> ${draft.stageSequenceVariant}`);
  }
  if (baseline.autoReviewPolicy !== draft.autoReviewPolicy) {
    changes.push(`auto_review_policy: ${baseline.autoReviewPolicy} -> ${draft.autoReviewPolicy}`);
  }
  if (baseline.followUpPolicy !== draft.followUpPolicy) {
    changes.push(`follow_up_policy: ${baseline.followUpPolicy} -> ${draft.followUpPolicy}`);
  }
  if (baseline.safeActionProfile !== draft.safeActionProfile) {
    changes.push(`safe_action_profile: ${baseline.safeActionProfile} -> ${draft.safeActionProfile}`);
  }

  return changes.length > 0 ? changes : ["preset-baseline preserved"];
}

function buildWarnings(node: MissionCanvasNode, draft: MissionWorkflowDraft): string[] {
  const warnings: string[] = [];

  if (draft.safeActionProfile === "preview_only") {
    warnings.push("preview_only keeps all actions read-only inside the browser");
  }
  if (node.nodeKind === "PR" && draft.followUpPolicy === "spawn_issue_on_gap") {
    warnings.push("follow_up_policy may create a handover issue later, but not from this prototype");
  }
  if (node.nodeKind === "Issue" && draft.autoReviewPolicy === "owner_only") {
    warnings.push("owner_only keeps reviewer as optional context rather than a required gate");
  }

  return warnings;
}

function buildGeneratedBlock(
  node: MissionCanvasNode,
  preset: MissionWorkflowPreset,
  draft: MissionWorkflowDraft,
  stageSequence: string[],
): string {
  const lines = [
    "workflow-policy:",
    `  seed: ${preset.generatedBlockSeed}`,
    `  node_ref: ${node.nodeKind}:${node.nodeId}`,
    `  node_title: ${node.title}`,
    `  stage_sequence: [${stageSequence.join(", ")}]`,
    `  auto_review_policy: ${draft.autoReviewPolicy}`,
    `  follow_up_policy: ${draft.followUpPolicy}`,
    `  safe_action_profile: ${draft.safeActionProfile}`,
    "  constraints:",
    "    - frontend-only-prototype",
    "    - no-live-provider-mutations",
    "    - prompt-source-truth=repo-seeds",
    "  source_refs:",
    ...preset.promptSeedRefs.map((sourceRef) => `    - ${sourceRef}`),
  ];

  return lines.join("\n");
}

export const missionControlPrototypeSource: MissionControlPrototypeSource = {
  async loadCatalog() {
    return cloneFixture(missionControlPrototypeCatalog);
  },

  async loadScenario({ scenarioId }) {
    const maps = getScenarioMapsOrThrow(scenarioId);
    const scenario = cloneFixture(maps.scenario);

    if (scenario.workflowPresets.length === 0) {
      scenario.workflowPresets = cloneFixture(missionControlPrototypeScenarios[0]?.workflowPresets ?? []);
    }

    return scenario;
  },

  async getNodeDetails({ scenarioId, nodeId }) {
    const maps = getScenarioMapsOrThrow(scenarioId);
    getNodeOrThrow(maps, nodeId);
    return cloneFixture(getDrawerOrThrow(maps, nodeId));
  },

  async generateWorkflowPreview({ scenarioId, nodeId, presetId, draft }) {
    const maps = getScenarioMapsOrThrow(scenarioId);
    const node = getNodeOrThrow(maps, nodeId);
    const preset = getPresetOrThrow(maps, presetId);
    const stageSequence = resolveStageSequence(preset, draft);

    return {
      presetId: preset.presetId,
      presetLabel: preset.label,
      resolvedDraft: cloneFixture(draft),
      stageSequence,
      generatedBlockMarkdown: buildGeneratedBlock(node, preset, draft, stageSequence),
      sourceRefs: Array.from(new Set([...preset.promptSeedRefs, ...maps.scenario.sourceRefs])),
      changeExplanations: buildChangeExplanations(preset, draft),
      warnings: buildWarnings(node, draft),
    };
  },
};
