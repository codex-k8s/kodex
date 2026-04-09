export type MissionCanvasNodeKind = "Issue" | "PR" | "Run";

export type MissionCanvasNodeState = "working" | "review" | "blocked" | "waiting";

export type MissionCanvasRelationKind = "drives" | "produces" | "tracks" | "blocks";

export type MissionCanvasRelationImportance = "primary" | "supporting";

export type MissionCanvasInitiativeAccentToken = "amber" | "teal" | "rose" | "lime" | "slate";

export type MissionCanvasNodeBadge =
  | "owner-review"
  | "needs-evidence"
  | "blocked"
  | "handover"
  | "deferred"
  | "demo-ready"
  | "live-wait";

export type MissionDrawerTab = "details" | "timeline" | "workflow";

export type MissionWorkflowStageSequenceVariant = "full_delivery" | "owner_demo" | "revise_loop";

export type MissionWorkflowAutoReviewPolicy = "required" | "owner_only" | "paired_reviewer";

export type MissionWorkflowFollowUpPolicy = "carry_to_next_stage" | "spawn_issue_on_gap" | "owner_managed";

export type MissionWorkflowSafeActionProfile = "preview_only" | "github_links_only" | "candidate_readonly";

export type MissionWorkflowDraft = {
  stageSequenceVariant: MissionWorkflowStageSequenceVariant;
  autoReviewPolicy: MissionWorkflowAutoReviewPolicy;
  followUpPolicy: MissionWorkflowFollowUpPolicy;
  safeActionProfile: MissionWorkflowSafeActionProfile;
};

export type MissionControlPrototypeError = {
  messageKey: string;
  debugMessage?: string;
};

export type MissionControlPrototypeRouteState = {
  scenarioId: string;
  initiativeId: string;
  nodeId: string;
  search: string;
  tab: MissionDrawerTab;
};

export type MissionControlPrototypeCatalogItem = {
  scenarioId: string;
  title: string;
  summary: string;
  initiativeCount: number;
  nodeCount: number;
  defaultFocusInitiativeId: string;
};

export type MissionCanvasViewport = {
  zoomLevel: number;
  panX: number;
  panY: number;
  canvasWidth: number;
  canvasHeight: number;
};

export type MissionCanvasInitiative = {
  initiativeId: string;
  label: string;
  accentToken: MissionCanvasInitiativeAccentToken;
  focusOrder: number;
  nodeIds: string[];
};

export type MissionCanvasNode = {
  nodeId: string;
  nodeKind: MissionCanvasNodeKind;
  initiativeId: string;
  title: string;
  state: MissionCanvasNodeState;
  stageLabel: string;
  layoutX: number;
  layoutY: number;
  meta: string[];
  badges: MissionCanvasNodeBadge[];
  detailId: string;
  safeActionIds: string[];
};

export type MissionCanvasRelation = {
  relationId: string;
  relationKind: MissionCanvasRelationKind;
  sourceNodeId: string;
  targetNodeId: string;
  label: string;
  importance: MissionCanvasRelationImportance;
};

export type MissionTimelineItemTone = "neutral" | "positive" | "warning" | "attention";

export type MissionDrawerTimelineItem = {
  itemId: string;
  happenedAt: string;
  title: string;
  summary: string;
  tone: MissionTimelineItemTone;
  sourceRef?: string;
};

export type MissionSafeActionKind = "link" | "doc" | "preview";

export type MissionDrawerSafeAction = {
  actionId: string;
  kind: MissionSafeActionKind;
  label: string;
  description: string;
  icon: string;
  href?: string;
};

export type MissionDrawerRecord = {
  detailId: string;
  nodeId: string;
  overviewMarkdown: string;
  timelineItems: MissionDrawerTimelineItem[];
  relatedNodeIds: string[];
  safeActions: MissionDrawerSafeAction[];
  workflowPresetIds: string[];
  sourceRefs: string[];
};

export type MissionWorkflowPreset = {
  presetId: string;
  label: string;
  summary: string;
  defaultDraft: MissionWorkflowDraft;
  stageSequenceOptions: Record<MissionWorkflowStageSequenceVariant, string[]>;
  promptSeedRefs: string[];
  generatedBlockSeed: string;
  allowedOverrides: Array<keyof MissionWorkflowDraft>;
};

export type MissionWorkflowPreviewResult = {
  presetId: string;
  presetLabel: string;
  resolvedDraft: MissionWorkflowDraft;
  stageSequence: string[];
  generatedBlockMarkdown: string;
  sourceRefs: string[];
  changeExplanations: string[];
  warnings: string[];
};

export type MissionCanvasScenario = {
  scenarioId: string;
  title: string;
  summary: string;
  initiatives: MissionCanvasInitiative[];
  nodes: MissionCanvasNode[];
  relations: MissionCanvasRelation[];
  drawerRecords: MissionDrawerRecord[];
  workflowPresets: MissionWorkflowPreset[];
  sourceRefs: string[];
  defaultViewport: MissionCanvasViewport;
  defaultFocusInitiativeId: string;
};

export type MissionInitiativeView = {
  initiativeId: string;
  label: string;
  accentToken: MissionCanvasInitiativeAccentToken;
  nodeCount: number;
  focused: boolean;
  dimmed: boolean;
  bounds: {
    left: number;
    top: number;
    width: number;
    height: number;
  };
};

export type MissionCanvasNodeView = {
  nodeId: string;
  initiativeId: string;
  initiativeAccentToken: MissionCanvasInitiativeAccentToken;
  nodeKind: MissionCanvasNodeKind;
  title: string;
  state: MissionCanvasNodeState;
  stageLabel: string;
  meta: string[];
  badges: MissionCanvasNodeBadge[];
  layoutX: number;
  layoutY: number;
  selected: boolean;
  dimmed: boolean;
  highlighted: boolean;
  matchesSearch: boolean;
};

export type MissionCanvasRelationView = {
  relationId: string;
  relationKind: MissionCanvasRelationKind;
  label: string;
  startX: number;
  startY: number;
  endX: number;
  endY: number;
  labelX: number;
  labelY: number;
  path: string;
  highlighted: boolean;
  dimmed: boolean;
};

export type MissionDrawerRelatedNodeView = {
  nodeId: string;
  nodeKind: MissionCanvasNodeKind;
  title: string;
  stageLabel: string;
  state: MissionCanvasNodeState;
};

export type MissionDrawerView = {
  nodeId: string;
  nodeKind: MissionCanvasNodeKind;
  title: string;
  stageLabel: string;
  state: MissionCanvasNodeState;
  initiativeLabel: string;
  overviewMarkdown: string;
  timelineItems: MissionDrawerTimelineItem[];
  relatedNodes: MissionDrawerRelatedNodeView[];
  safeActions: MissionDrawerSafeAction[];
  sourceRefs: string[];
};

export type MissionWorkflowPresetOption = {
  presetId: string;
  label: string;
  summary: string;
};
