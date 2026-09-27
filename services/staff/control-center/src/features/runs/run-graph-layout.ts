import { graphlib, layout as layoutDagre } from "@dagrejs/dagre";
import type { EdgeLabel, GraphLabel, NodeLabel } from "@dagrejs/dagre";

import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

export const runGraphNodeWidth = 244;
export const runGraphNodeHeight = 132;

const canvasPadding = 34;
const horizontalGap = 100;
const verticalGap = 44;

export interface PositionedRunNode {
  node: RunNode;
  x: number;
  y: number;
}

export interface PositionedRunEdge {
  edge: RunEdge;
  path?: string;
}

export interface RunGraphLayout {
  nodes: PositionedRunNode[];
  edges: PositionedRunEdge[];
  width: number;
  height: number;
}

export function layoutRunGraph(
  nodes: RunNode[],
  edges: RunEdge[],
): RunGraphLayout {
  if (nodes.length === 0) {
    return { nodes: [], edges: [], width: 0, height: 0 };
  }

  const nodeByRef = new Map(nodes.map((node) => [node.ref, node]));
  const validEdges = edges.filter(
    (edge) =>
      nodeByRef.has(edge.sourceNodeRef) && nodeByRef.has(edge.targetNodeRef),
  );
  const graph = new graphlib.Graph<GraphLabel, NodeLabel, EdgeLabel>({
    multigraph: true,
  });
  graph.setGraph({
    rankdir: "LR",
    ranksep: horizontalGap,
    nodesep: verticalGap,
    edgesep: 20,
    marginx: canvasPadding,
    marginy: canvasPadding,
    acyclicer: "greedy",
  });
  graph.setDefaultEdgeLabel(() => ({}));

  for (const node of [...nodes].sort(compareNodes)) {
    graph.setNode(node.ref, {
      width: runGraphNodeWidth,
      height: runGraphNodeHeight,
    });
  }
  for (const edge of [...validEdges].sort(compareEdges)) {
    // Ответ дочернего запуска возвращается к предку: это обратная связь,
    // которая не должна переставлять основную цепочку делегирования и повторов.
    if (edge.type === "CALLBACK_TO") continue;
    graph.setEdge(
      edge.sourceNodeRef,
      edge.targetNodeRef,
      { weight: edge.type === "RETRY_OF" ? 2 : 4 },
      edge.ref,
    );
  }

  layoutDagre(graph);
  const positioned = nodes.map((node) => {
    const position = graph.node(node.ref);
    if (typeof position.x !== "number" || typeof position.y !== "number") {
      throw new Error("Dagre layout returned incomplete node coordinates");
    }
    return {
      node,
      x: position.x - runGraphNodeWidth / 2,
      y: position.y - runGraphNodeHeight / 2,
    };
  });
  const positionedEdges = validEdges.map((edge) => {
    const points =
      edge.type === "CALLBACK_TO"
        ? undefined
        : graph.edge({
            v: edge.sourceNodeRef,
            w: edge.targetNodeRef,
            name: edge.ref,
          }).points;
    return {
      edge,
      path: points ? smoothRunEdgePath(points) : undefined,
    };
  });
  const bounds = graph.graph();
  return {
    nodes: positioned,
    edges: positionedEdges,
    width: bounds.width ?? 0,
    height: bounds.height ?? 0,
  };
}

export function smoothRunEdgePath(
  points: ReadonlyArray<{ x: number; y: number }>,
): string | undefined {
  const first = points[0];
  if (!first || points.length < 2) return undefined;
  const segments = [["M", first.x, first.y].join(" ")];
  for (let index = 0; index < points.length - 1; index += 1) {
    const start = points[index];
    const end = points[index + 1];
    if (!start || !end) continue;
    const before = points[index - 1] ?? start;
    const after = points[index + 2] ?? end;
    const tension = 1 / 8;
    segments.push(
      [
        "C",
        start.x + (end.x - before.x) * tension,
        start.y + (end.y - before.y) * tension,
        end.x - (after.x - start.x) * tension,
        end.y - (after.y - start.y) * tension,
        end.x,
        end.y,
      ].join(" "),
    );
  }
  return segments.join(" ");
}

function compareNodes(left: RunNode, right: RunNode): number {
  return (
    left.createdAt.localeCompare(right.createdAt) ||
    left.ref.localeCompare(right.ref)
  );
}

function compareEdges(left: RunEdge, right: RunEdge): number {
  return (
    left.sourceNodeRef.localeCompare(right.sourceNodeRef) ||
    left.targetNodeRef.localeCompare(right.targetNodeRef) ||
    left.ref.localeCompare(right.ref)
  );
}
