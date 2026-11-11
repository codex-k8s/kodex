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
  bounds?: RunGraphBounds;
}

export interface RunGraphBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface RunGraphLayout {
  nodes: PositionedRunNode[];
  edges: PositionedRunEdge[];
  width: number;
  height: number;
  bounds: RunGraphBounds;
}

export function layoutRunGraph(
  nodes: RunNode[],
  edges: RunEdge[],
): RunGraphLayout {
  if (nodes.length === 0) {
    return {
      nodes: [],
      edges: [],
      width: 0,
      height: 0,
      bounds: { x: 0, y: 0, width: 0, height: 0 },
    };
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
    // Для раскладки возврат направлен от родителя к исполнителю. Dagre
    // выделяет ему отдельный локальный маршрут рядом с делегированием;
    // при отрисовке точки разворачиваются, исходная связь не изменяется.
    const returning = edge.type === "CALLBACK_TO";
    graph.setEdge(
      returning ? edge.targetNodeRef : edge.sourceNodeRef,
      returning ? edge.sourceNodeRef : edge.targetNodeRef,
      { weight: returning ? 1 : edge.type === "RETRY_OF" ? 2 : 4 },
      edge.ref,
    );
  }

  for (const [child, continuation] of completedReturnContinuations(
    nodeByRef,
    validEdges,
  )) {
    graph.setEdge(
      child,
      continuation,
      { weight: 2 },
      `return-order:${child}:${continuation}`,
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
    const returning = edge.type === "CALLBACK_TO";
    const route = graph.edge({
      v: returning ? edge.targetNodeRef : edge.sourceNodeRef,
      w: returning ? edge.sourceNodeRef : edge.targetNodeRef,
      name: edge.ref,
    }).points;
    const points = returning && route ? [...route].reverse() : route;
    return {
      edge,
      path: points ? smoothRunEdgePath(points) : undefined,
      bounds: points?.length ? pointBounds(points) : undefined,
    };
  });
  const bounds = runGraphContentBounds({
    nodes: positioned,
    edges: positionedEdges,
  });
  return {
    nodes: positioned,
    edges: positionedEdges,
    width: bounds.width,
    height: bounds.height,
    bounds,
  };
}

// Завершённый ребёнок располагается раньше следующего хода того же
// родителя только при подтверждённой временной последовательности.
// Это ограничение раскладки, не новая связь или разрешение на исполнение.
function completedReturnContinuations(
  nodes: ReadonlyMap<string, RunNode>,
  edges: RunEdge[],
): Array<[string, string]> {
  const continuations = new Map<string, RunNode[]>();
  for (const edge of edges) {
    if (edge.type !== "CONTINUES") continue;
    const previous = nodes.get(edge.sourceNodeRef);
    const next = nodes.get(edge.targetNodeRef);
    if (
      !previous ||
      !next ||
      previous.type !== "AGENT_EXECUTION" ||
      next.type !== "AGENT_EXECUTION" ||
      !previous.agentRef ||
      previous.agentRef !== next.agentRef ||
      previous.runRef !== next.runRef ||
      next.parentNodeRef !== previous.ref ||
      !next.startedAt
    )
      continue;
    const items = continuations.get(previous.ref) ?? [];
    items.push(next);
    continuations.set(previous.ref, items);
  }
  const result: Array<[string, string]> = [];
  for (const edge of edges) {
    if (edge.type !== "CALLBACK_TO") continue;
    const child = nodes.get(edge.sourceNodeRef);
    if (!child?.finishedAt) continue;
    const finishedAt = Date.parse(child.finishedAt);
    const next = continuations
      .get(edge.targetNodeRef)
      ?.filter((item) => Date.parse(item.startedAt ?? "") >= finishedAt)
      .sort(compareNodes)[0];
    if (next && next.ref !== child.ref) result.push([child.ref, next.ref]);
  }
  return result.sort(
    ([a, b], [c, d]) => a.localeCompare(c) || b.localeCompare(d),
  );
}

function pointBounds(
  points: ReadonlyArray<{ x: number; y: number }>,
): RunGraphBounds {
  const x = Math.min(...points.map((point) => point.x));
  const y = Math.min(...points.map((point) => point.y));
  return {
    x,
    y,
    width: Math.max(...points.map((point) => point.x)) - x,
    height: Math.max(...points.map((point) => point.y)) - y,
  };
}

export function runGraphContentBounds(
  layout: Pick<RunGraphLayout, "nodes" | "edges">,
  nodeRefs?: readonly string[],
): RunGraphBounds {
  const refs = nodeRefs ? new Set(nodeRefs) : undefined;
  const rectangles: RunGraphBounds[] = layout.nodes
    .filter((item) => !refs || refs.has(item.node.ref))
    .map((item) => ({
      x: item.x,
      y: item.y,
      width: runGraphNodeWidth,
      height: runGraphNodeHeight,
    }));
  for (const item of layout.edges) {
    if (
      item.bounds &&
      (!refs ||
        (refs.has(item.edge.sourceNodeRef) &&
          refs.has(item.edge.targetNodeRef)))
    ) {
      rectangles.push(item.bounds);
    }
  }
  if (!rectangles.length) return { x: 0, y: 0, width: 0, height: 0 };
  const x = Math.min(...rectangles.map((item) => item.x));
  const y = Math.min(...rectangles.map((item) => item.y));
  return {
    x,
    y,
    width: Math.max(...rectangles.map((item) => item.x + item.width)) - x,
    height: Math.max(...rectangles.map((item) => item.y + item.height)) - y,
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
