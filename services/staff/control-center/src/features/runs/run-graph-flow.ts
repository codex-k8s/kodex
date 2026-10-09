import {
  MarkerType,
  Position,
  getTransformForBounds,
  type Edge,
  type FitViewParams,
  type Node,
} from "@vue-flow/core";

import {
  layoutRunGraph,
  runGraphContentBounds,
  runGraphNodeHeight,
  runGraphNodeWidth,
  type RunGraphBounds,
} from "@/features/runs/run-graph-layout";
import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

export const runGraphMinimumZoom = 0.15;
export const runGraphMaximumZoom = 1.8;
export function runGraphFitViewOptions(
  viewportWidth: number,
  compact = false,
): FitViewParams {
  return {
    padding:
      viewportWidth <= 760
        ? 0.14
        : compact
          ? {
              top: "180px",
              right: "32px",
              bottom: "160px",
              left: "32px",
            }
          : {
              top: "180px",
              right: "210px",
              bottom: "160px",
              left: "380px",
            },
    minZoom: runGraphMinimumZoom,
    maxZoom: 1.1,
    duration: 180,
  };
}

export function runGraphInitialFitOptions(
  viewportWidth: number,
  nodes: RunNode[],
  edges: RunEdge[],
  selectedRef?: string,
  compact = false,
  viewportHeight = 480,
  activeNodeRefs: readonly string[] = [],
): FitViewParams {
  const options = runGraphFitViewOptions(viewportWidth, compact);
  const activeRefs = new Set(activeNodeRefs);
  const activeExecutions = nodes
    .filter(
      (node) =>
        node.type === "AGENT_EXECUTION" &&
        node.state === "RUNNING" &&
        activeRefs.has(node.ref),
    )
    .sort(
      (left, right) =>
        left.createdAt.localeCompare(right.createdAt) ||
        left.ref.localeCompare(right.ref),
    );
  const activeExecution =
    activeExecutions.find((node) => node.ref === selectedRef) ??
    activeExecutions[0];
  if (!activeExecution && nodes.length <= 16) return options;

  const nodeRefs = new Set(nodes.map((node) => node.ref));
  const root =
    activeExecution?.ref ??
    (selectedRef && nodeRefs.has(selectedRef) ? selectedRef : undefined) ??
    [...nodes].sort(
      (left, right) =>
        left.createdAt.localeCompare(right.createdAt) ||
        left.ref.localeCompare(right.ref),
    )[0]?.ref;
  if (!root) return options;

  const layout = layoutRunGraph(nodes, edges);
  const positions = new Map(layout.nodes.map((item) => [item.node.ref, item]));
  const rootY = positions.get(root)?.y ?? 0;
  const firstChildren = edges
    .filter(
      (edge) =>
        edge.sourceNodeRef === root &&
        edge.type !== "CALLBACK_TO" &&
        nodeRefs.has(edge.targetNodeRef),
    )
    .sort(
      (left, right) =>
        Math.abs((positions.get(left.targetNodeRef)?.y ?? 0) - rootY) -
        Math.abs((positions.get(right.targetNodeRef)?.y ?? 0) - rootY),
    )
    .slice(0, 3)
    .map((edge) => edge.targetNodeRef);
  // Активное исполнение важнее обёртки запуска. Ближайший завершённый
  // предшественник и корень добавляются только без потери читаемости.
  const contextRefs = activeExecution
    ? [
        activeExecution.parentNodeRef,
        ...edges
          .filter(
            (edge) =>
              edge.targetNodeRef === root &&
              edge.type !== "CALLBACK_TO" &&
              edge.type !== "RETRY_OF",
          )
          .sort((left, right) => left.ref.localeCompare(right.ref))
          .map((edge) => edge.sourceNodeRef),
        ...nodes
          .filter((node) => node.type === "ROOT_PROCESS")
          .sort(
            (left, right) =>
              left.createdAt.localeCompare(right.createdAt) ||
              left.ref.localeCompare(right.ref),
          )
          .slice(0, 1)
          .map((node) => node.ref),
      ].filter(
        (ref): ref is string =>
          ref !== undefined && ref !== root && nodeRefs.has(ref),
      )
    : firstChildren;
  const visibleRefs = [root];
  for (const ref of new Set(contextRefs)) {
    if (visibleRefs.length >= 4) break;
    const candidateRefs = [...visibleRefs, ref];
    const viewport = getTransformForBounds(
      runGraphContentBounds(layout, candidateRefs),
      viewportWidth,
      viewportHeight,
      0,
      options.maxZoom ?? 1.1,
      options.padding,
    );
    // Начальная окрестность остаётся читаемой. Далёкая карточка или внешняя
    // обратная дуга не должны сдвигать выбранный узел за границу экрана.
    if (viewport.zoom >= 0.85) visibleRefs.push(ref);
  }
  const viewport = getTransformForBounds(
    runGraphContentBounds(layout, visibleRefs),
    viewportWidth,
    viewportHeight,
    0,
    options.maxZoom ?? 1.1,
    options.padding,
  );
  return {
    ...options,
    nodes: visibleRefs,
    minZoom: Math.min(0.85, viewport.zoom),
  };
}

export function runGraphFitTransform(
  bounds: RunGraphBounds,
  viewportWidth: number,
  viewportHeight: number,
  options: FitViewParams,
) {
  return getTransformForBounds(
    bounds,
    viewportWidth,
    viewportHeight,
    options.nodes ? (options.minZoom ?? runGraphMinimumZoom) : 0,
    options.maxZoom ?? runGraphMaximumZoom,
    options.padding,
  );
}

export type RunGraphNodeSurface = "session" | "control";

export interface RunGraphNodeData {
  node: RunNode;
  executionLabel?: "ASSISTANT" | "EMPLOYEE" | "SESSION";
  retryAttempt?: number;
  surface: RunGraphNodeSurface;
  selected: boolean;
  future: boolean;
  active: boolean;
  accessibleLabel: string;
}

export interface RunGraphEdgeData {
  edge: RunEdge;
  accessibleLabel: string;
  color: string;
  path?: string;
  dasharray?: string;
  strokeWidth: number;
}

export type RunGraphFlowNode = Node<
  RunGraphNodeData,
  Record<string, never>,
  "runNode"
>;
export type RunGraphFlowEdge = Edge<
  RunGraphEdgeData,
  Record<string, never>,
  "runEdge"
>;

export interface RunGraphFlowElements {
  nodes: RunGraphFlowNode[];
  edges: RunGraphFlowEdge[];
}

export interface RunGraphFlowOptions {
  executionLabels?: Readonly<
    Record<string, "ASSISTANT" | "EMPLOYEE" | "SESSION">
  >;
  selectedRef?: string;
  futureRefs: ReadonlySet<string>;
  activeRefs: ReadonlySet<string>;
  nodeAccessibleLabel: (node: RunNode, retryAttempt?: number) => string;
  edgeAccessibleLabel: (edge: RunEdge) => string;
}

export function createRunGraphFlowElements(
  nodes: RunNode[],
  edges: RunEdge[],
  options: RunGraphFlowOptions,
): RunGraphFlowElements {
  const layout = layoutRunGraph(nodes, edges);
  const retryAttempts = runGraphRetryAttempts(nodes, edges);

  return {
    nodes: layout.nodes.map(({ node, x, y }) => {
      const future = isFutureNode(node, options.futureRefs);
      const active =
        node.state === "RUNNING" && options.activeRefs.has(node.ref);
      const selected = node.ref === options.selectedRef;
      const surface = nodeSurface(node);
      const retryAttempt = retryAttempts.get(node.ref);

      return {
        id: node.ref,
        type: "runNode",
        position: { x, y },
        width: runGraphNodeWidth,
        height: runGraphNodeHeight,
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
        draggable: false,
        connectable: false,
        selectable: false,
        focusable: false,
        deletable: false,
        ariaLabel: options.nodeAccessibleLabel(node, retryAttempt),
        class: [
          "run-flow-node",
          `run-flow-node--${node.state.toLowerCase()}`,
          `run-flow-node--${surface}`,
          future ? "run-flow-node--future" : "",
          active ? "run-flow-node--active" : "",
          selected ? "run-flow-node--selected" : "",
        ].filter(Boolean),
        domAttributes: {
          "aria-busy": active ? "true" : undefined,
          "aria-pressed": selected ? "true" : "false",
          "data-node-ref": node.ref,
          "data-node-type": node.type,
          "data-node-surface": surface,
          "data-node-state": node.state,
          "data-node-future": future ? "true" : undefined,
        },
        data: {
          node,
          executionLabel: options.executionLabels?.[node.ref],
          retryAttempt,
          surface,
          selected,
          future,
          active,
          accessibleLabel: options.nodeAccessibleLabel(node, retryAttempt),
        },
      };
    }),
    edges: layout.edges.map(({ edge, path }) => {
      const visual = edgeVisual(edge.type);
      return {
        id: edge.ref,
        type: "runEdge",
        source: edge.sourceNodeRef,
        target: edge.targetNodeRef,
        sourceHandle: "source",
        targetHandle: "target",
        selectable: false,
        focusable: false,
        deletable: false,
        updatable: false,
        interactionWidth: 0,
        ariaLabel: options.edgeAccessibleLabel(edge),
        class: ["run-flow-edge", `run-flow-edge--${edge.type.toLowerCase()}`],
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color: visual.color,
          width: 18,
          height: 18,
        },
        data: {
          edge,
          path,
          accessibleLabel: options.edgeAccessibleLabel(edge),
          ...visual,
        },
      };
    }),
  };
}

export function runGraphRetryAttempts(
  nodes: RunNode[],
  edges: RunEdge[],
): Map<string, number> {
  const roots = new Set(
    nodes
      .filter((node) => node.type === "ROOT_PROCESS")
      .map((node) => node.ref),
  );
  const predecessor = new Map<string, string>();
  const participants = new Set<string>();
  for (const edge of edges) {
    if (
      edge.type !== "RETRY_OF" ||
      !roots.has(edge.sourceNodeRef) ||
      !roots.has(edge.targetNodeRef)
    )
      continue;
    predecessor.set(edge.targetNodeRef, edge.sourceNodeRef);
    participants.add(edge.sourceNodeRef);
    participants.add(edge.targetNodeRef);
  }
  const attempts = new Map<string, number>();
  for (const ref of participants) {
    let current = ref;
    const visited = new Set([ref]);
    while (predecessor.has(current)) {
      const previous = predecessor.get(current);
      if (!previous) break;
      if (visited.has(previous)) break;
      visited.add(previous);
      current = previous;
    }
    attempts.set(ref, visited.size);
  }
  return attempts;
}

function nodeSurface(node: RunNode): RunGraphNodeSurface {
  return node.type === "ROOT_PROCESS" || node.type === "AGENT_EXECUTION"
    ? "session"
    : "control";
}

function isFutureNode(node: RunNode, futureRefs: ReadonlySet<string>): boolean {
  return (
    futureRefs.has(node.ref) ||
    node.planned === true ||
    node.state === "PLANNED"
  );
}

function edgeVisual(
  type: RunEdge["type"],
): Omit<RunGraphEdgeData, "edge" | "accessibleLabel"> {
  switch (type) {
    case "DELEGATED_TO":
      return { color: "var(--accent)", strokeWidth: 2.4 };
    case "CALLBACK_TO":
      return {
        color: "var(--success)",
        dasharray: "9 5",
        strokeWidth: 2.2,
      };
    case "RETRY_OF":
      return {
        color: "var(--warning)",
        dasharray: "2 5",
        strokeWidth: 2.2,
      };
    case "CONTINUES":
      return {
        color: "color-mix(in srgb, var(--accent) 58%, var(--muted))",
        strokeWidth: 3,
      };
    case "WAITING_FOR":
      return {
        color: "var(--subtle)",
        dasharray: "6 5",
        strokeWidth: 2,
      };
  }
}
