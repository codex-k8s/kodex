import type {
  MissionCanvasInitiative,
  MissionCanvasNode,
  MissionCanvasNodeBadge,
  MissionCanvasNodeView,
  MissionCanvasRelation,
  MissionCanvasRelationView,
  MissionCanvasScenario,
  MissionCanvasViewport,
  MissionDrawerRecord,
  MissionDrawerRelatedNodeView,
  MissionDrawerView,
  MissionInitiativeView,
} from "./types.ts";

export const missionControlPrototypeCardWidth = 232;
export const missionControlPrototypeCardHeight = 132;

const missionControlInitiativePadding = 56;

function normalizeToken(value: string): string {
  return value.trim().toLowerCase();
}

function buildNodeSearchTokens(node: MissionCanvasNode): string[] {
  return [node.title, node.stageLabel, ...node.meta, ...node.badges].map(normalizeToken).filter(Boolean);
}

function nodeMatchesSearch(node: MissionCanvasNode, search: string): boolean {
  const needle = normalizeToken(search);
  if (needle === "") {
    return true;
  }

  return buildNodeSearchTokens(node).some((token) => token.includes(needle));
}

function buildInitiativeBounds(nodes: MissionCanvasNode[]) {
  const left = Math.min(...nodes.map((node) => node.layoutX)) - missionControlInitiativePadding;
  const top = Math.min(...nodes.map((node) => node.layoutY)) - missionControlInitiativePadding;
  const right =
    Math.max(...nodes.map((node) => node.layoutX + missionControlPrototypeCardWidth)) + missionControlInitiativePadding;
  const bottom =
    Math.max(...nodes.map((node) => node.layoutY + missionControlPrototypeCardHeight)) + missionControlInitiativePadding;

  return {
    left,
    top,
    width: right - left,
    height: bottom - top,
  };
}

function buildNodeView(
  node: MissionCanvasNode,
  options: {
    initiative: MissionCanvasInitiative;
    search: string;
    focusedInitiativeId: string | null;
    selectedNodeId: string | null;
  },
): MissionCanvasNodeView {
  const matchesSearch = nodeMatchesSearch(node, options.search);
  const initiativeFiltered = options.focusedInitiativeId ? options.focusedInitiativeId !== node.initiativeId : false;
  const dimmed = initiativeFiltered || !matchesSearch;

  return {
    nodeId: node.nodeId,
    initiativeId: node.initiativeId,
    initiativeAccentToken: options.initiative.accentToken,
    nodeKind: node.nodeKind,
    title: node.title,
    state: node.state,
    stageLabel: node.stageLabel,
    meta: node.meta,
    badges: node.badges,
    layoutX: node.layoutX,
    layoutY: node.layoutY,
    selected: options.selectedNodeId === node.nodeId,
    dimmed,
    highlighted: options.selectedNodeId === node.nodeId || (!dimmed && options.search.trim() !== ""),
    matchesSearch,
  };
}

function buildRelationPath(
  leftX: number,
  leftY: number,
  rightX: number,
  rightY: number,
): { path: string; labelX: number; labelY: number } {
  const controlOffset = Math.max(80, Math.abs(rightX - leftX) * 0.32);
  const midpointX = (leftX + rightX) / 2;
  const midpointY = (leftY + rightY) / 2;

  return {
    path: `M ${leftX} ${leftY} C ${leftX + controlOffset} ${leftY}, ${rightX - controlOffset} ${rightY}, ${rightX} ${rightY}`,
    labelX: midpointX,
    labelY: midpointY - 12,
  };
}

export function missionControlPrototypeBadgeTone(badge: MissionCanvasNodeBadge): string {
  switch (badge) {
    case "blocked":
      return "error";
    case "owner-review":
    case "needs-evidence":
      return "warning";
    case "demo-ready":
      return "success";
    case "handover":
    case "deferred":
    case "live-wait":
      return "info";
  }
}

export function missionControlPrototypeStateTone(state: MissionCanvasNode["state"]): string {
  switch (state) {
    case "working":
      return "info";
    case "review":
      return "warning";
    case "blocked":
      return "error";
    case "waiting":
      return "secondary";
  }
}

export function buildInitiativeViews(
  scenario: MissionCanvasScenario | null,
  focusedInitiativeId: string | null,
): MissionInitiativeView[] {
  if (!scenario) {
    return [];
  }

  return scenario.initiatives
    .map((initiative) => {
      const nodes = scenario.nodes.filter((node) => node.initiativeId === initiative.initiativeId);
      return {
        initiativeId: initiative.initiativeId,
        label: initiative.label,
        accentToken: initiative.accentToken,
        nodeCount: nodes.length,
        focused: focusedInitiativeId === initiative.initiativeId,
        dimmed: Boolean(focusedInitiativeId) && focusedInitiativeId !== initiative.initiativeId,
        bounds: buildInitiativeBounds(nodes),
      };
    })
    .sort((left, right) => {
      const leftInitiative = scenario.initiatives.find((initiative) => initiative.initiativeId === left.initiativeId);
      const rightInitiative = scenario.initiatives.find((initiative) => initiative.initiativeId === right.initiativeId);
      return (leftInitiative?.focusOrder ?? 0) - (rightInitiative?.focusOrder ?? 0);
    });
}

export function buildNodeViews(
  scenario: MissionCanvasScenario | null,
  options: {
    search: string;
    focusedInitiativeId: string | null;
    selectedNodeId: string | null;
  },
): MissionCanvasNodeView[] {
  if (!scenario) {
    return [];
  }

  const initiativeById = new Map(scenario.initiatives.map((initiative) => [initiative.initiativeId, initiative]));

  return scenario.nodes.map((node) =>
    buildNodeView(node, {
      initiative: initiativeById.get(node.initiativeId) ?? scenario.initiatives[0],
      search: options.search,
      focusedInitiativeId: options.focusedInitiativeId,
      selectedNodeId: options.selectedNodeId,
    }),
  );
}

export function buildRelationViews(
  scenario: MissionCanvasScenario | null,
  nodeViews: MissionCanvasNodeView[],
  selectedNodeId: string | null,
): MissionCanvasRelationView[] {
  if (!scenario) {
    return [];
  }

  const nodeViewById = new Map(nodeViews.map((node) => [node.nodeId, node]));

  return scenario.relations
    .map((relation) => {
      const sourceNode = nodeViewById.get(relation.sourceNodeId);
      const targetNode = nodeViewById.get(relation.targetNodeId);
      if (!sourceNode || !targetNode) {
        return null;
      }

      const startX = sourceNode.layoutX + missionControlPrototypeCardWidth - 8;
      const startY = sourceNode.layoutY + missionControlPrototypeCardHeight / 2;
      const endX = targetNode.layoutX + 8;
      const endY = targetNode.layoutY + missionControlPrototypeCardHeight / 2;
      const geometry = buildRelationPath(startX, startY, endX, endY);
      const highlighted = selectedNodeId
        ? relation.sourceNodeId === selectedNodeId || relation.targetNodeId === selectedNodeId
        : relation.importance === "primary";
      const dimmed =
        (!highlighted && (sourceNode.dimmed || targetNode.dimmed)) ||
        (relation.importance === "supporting" && !highlighted);

      return {
        relationId: relation.relationId,
        relationKind: relation.relationKind,
        label: relation.label,
        startX,
        startY,
        endX,
        endY,
        labelX: geometry.labelX,
        labelY: geometry.labelY,
        path: geometry.path,
        highlighted,
        dimmed,
      };
    })
    .filter((relation): relation is MissionCanvasRelationView => relation !== null);
}

export function buildDrawerView(
  scenario: MissionCanvasScenario | null,
  nodeId: string | null,
  drawerRecord: MissionDrawerRecord | null,
): MissionDrawerView | null {
  if (!scenario || !nodeId || !drawerRecord) {
    return null;
  }

  const node = scenario.nodes.find((candidate) => candidate.nodeId === nodeId);
  if (!node) {
    return null;
  }
  const initiative = scenario.initiatives.find((candidate) => candidate.initiativeId === node.initiativeId);
  const relatedNodeById = new Map(scenario.nodes.map((candidate) => [candidate.nodeId, candidate]));

  const relatedNodes: MissionDrawerRelatedNodeView[] = drawerRecord.relatedNodeIds
    .map((relatedNodeId) => relatedNodeById.get(relatedNodeId))
    .filter((relatedNode): relatedNode is MissionCanvasNode => relatedNode !== undefined)
    .map((relatedNode) => ({
      nodeId: relatedNode.nodeId,
      nodeKind: relatedNode.nodeKind,
      title: relatedNode.title,
      stageLabel: relatedNode.stageLabel,
      state: relatedNode.state,
    }));

  return {
    nodeId: node.nodeId,
    nodeKind: node.nodeKind,
    title: node.title,
    stageLabel: node.stageLabel,
    state: node.state,
    initiativeLabel: initiative?.label ?? "",
    overviewMarkdown: drawerRecord.overviewMarkdown,
    timelineItems: drawerRecord.timelineItems,
    relatedNodes,
    safeActions: drawerRecord.safeActions,
    sourceRefs: drawerRecord.sourceRefs,
  };
}

export function buildViewportForNodes(
  nodes: MissionCanvasNode[],
  baseViewport: MissionCanvasViewport,
): MissionCanvasViewport {
  if (nodes.length === 0) {
    return baseViewport;
  }

  const left = Math.min(...nodes.map((node) => node.layoutX));
  const top = Math.min(...nodes.map((node) => node.layoutY));
  const right = Math.max(...nodes.map((node) => node.layoutX + missionControlPrototypeCardWidth));
  const bottom = Math.max(...nodes.map((node) => node.layoutY + missionControlPrototypeCardHeight));

  const contentWidth = right - left;
  const contentHeight = bottom - top;
  const paddedWidth = contentWidth + 220;
  const paddedHeight = contentHeight + 180;

  const fitZoom = Math.min(
    1.15,
    Math.max(
      0.72,
      Math.min(baseViewport.canvasWidth / paddedWidth, baseViewport.canvasHeight / paddedHeight),
    ),
  );

  return {
    ...baseViewport,
    zoomLevel: fitZoom,
    panX: Math.max(0, (baseViewport.canvasWidth - contentWidth * fitZoom) / 2 - left * fitZoom),
    panY: Math.max(0, (baseViewport.canvasHeight - contentHeight * fitZoom) / 2 - top * fitZoom),
  };
}
