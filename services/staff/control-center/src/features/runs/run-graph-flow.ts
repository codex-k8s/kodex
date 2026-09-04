import {
  MarkerType,
  Position,
  type Edge,
  type FitViewParams,
  type Node,
} from "@vue-flow/core";

import {
  layoutRunGraph,
  runGraphNodeHeight,
  runGraphNodeWidth,
} from "@/features/runs/run-graph-layout";
import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

export const runGraphMinimumZoom = 0.3;
export const runGraphMaximumZoom = 1.8;
export function runGraphFitViewOptions(viewportWidth: number): FitViewParams {
  return {
    padding:
      viewportWidth <= 760
        ? 0.14
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

export type RunGraphNodeSurface = "session" | "control";

export interface RunGraphNodeData {
  node: RunNode;
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
  selectedRef?: string;
  futureRefs: ReadonlySet<string>;
  activeRefs: ReadonlySet<string>;
  nodeAccessibleLabel: (node: RunNode) => string;
  edgeAccessibleLabel: (edge: RunEdge) => string;
}

export function createRunGraphFlowElements(
  nodes: RunNode[],
  edges: RunEdge[],
  options: RunGraphFlowOptions,
): RunGraphFlowElements {
  const layout = layoutRunGraph(nodes, edges);

  return {
    nodes: layout.nodes.map(({ node, x, y }) => {
      const future = isFutureNode(node, options.futureRefs);
      const active =
        node.state === "RUNNING" && options.activeRefs.has(node.ref);
      const selected = node.ref === options.selectedRef;
      const surface = nodeSurface(node);

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
        ariaLabel: options.nodeAccessibleLabel(node),
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
          surface,
          selected,
          future,
          active,
          accessibleLabel: options.nodeAccessibleLabel(node),
        },
      };
    }),
    edges: layout.edges.map(({ edge }) => {
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
          accessibleLabel: options.edgeAccessibleLabel(edge),
          ...visual,
        },
      };
    }),
  };
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
