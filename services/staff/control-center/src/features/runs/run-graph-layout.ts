import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

export const runGraphNodeWidth = 224;
export const runGraphNodeHeight = 132;

const horizontalGap = 96;
const verticalGap = 36;
const canvasPadding = 34;

export interface PositionedRunNode {
  node: RunNode;
  x: number;
  y: number;
}

export interface PositionedRunEdge {
  edge: RunEdge;
  path: string;
  labelX: number;
  labelY: number;
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
  const nodeByRef = new Map(nodes.map((node) => [node.ref, node]));
  const rank = new Map(nodes.map((node) => [node.ref, 0]));
  const forwardEdges = edges.filter(
    (edge) =>
      nodeByRef.has(edge.sourceNodeRef) &&
      nodeByRef.has(edge.targetNodeRef) &&
      edge.type !== "CALLBACK_TO" &&
      edge.type !== "RETRY_OF",
  );
  const outgoing = new Map<string, RunEdge[]>();
  const indegree = new Map(nodes.map((node) => [node.ref, 0]));
  for (const edge of forwardEdges) {
    outgoing.set(edge.sourceNodeRef, [
      ...(outgoing.get(edge.sourceNodeRef) ?? []),
      edge,
    ]);
    indegree.set(
      edge.targetNodeRef,
      (indegree.get(edge.targetNodeRef) ?? 0) + 1,
    );
  }
  const queue = nodes
    .filter((node) => indegree.get(node.ref) === 0)
    .sort(compareNodes);
  for (let index = 0; index < queue.length; index += 1) {
    const source = queue[index];
    if (!source) continue;
    for (const edge of outgoing.get(source.ref) ?? []) {
      rank.set(
        edge.targetNodeRef,
        Math.max(
          rank.get(edge.targetNodeRef) ?? 0,
          (rank.get(source.ref) ?? 0) + 1,
        ),
      );
      const remaining = (indegree.get(edge.targetNodeRef) ?? 1) - 1;
      indegree.set(edge.targetNodeRef, remaining);
      if (remaining === 0) {
        const target = nodeByRef.get(edge.targetNodeRef);
        if (target) queue.push(target);
      }
    }
  }

  const groups = new Map<number, RunNode[]>();
  for (const node of nodes) {
    const column = rank.get(node.ref) ?? 0;
    groups.set(column, [...(groups.get(column) ?? []), node]);
  }
  const positioned: PositionedRunNode[] = [];
  for (const [column, group] of [...groups.entries()].sort(
    ([left], [right]) => left - right,
  )) {
    group.sort(compareNodes).forEach((node, row) => {
      positioned.push({
        node,
        x: canvasPadding + column * (runGraphNodeWidth + horizontalGap),
        y: canvasPadding + row * (runGraphNodeHeight + verticalGap),
      });
    });
  }
  const positionByRef = new Map(
    positioned.map((position) => [position.node.ref, position]),
  );
  const positionedEdges: PositionedRunEdge[] = [];
  for (const edge of edges) {
    const source = positionByRef.get(edge.sourceNodeRef);
    const target = positionByRef.get(edge.targetNodeRef);
    if (!source || !target) continue;
    const sourceX = source.x + runGraphNodeWidth;
    const sourceY = source.y + runGraphNodeHeight / 2;
    const targetX = target.x;
    const targetY = target.y + runGraphNodeHeight / 2;
    const direction = targetX >= sourceX ? 1 : -1;
    const curve = Math.max(54, Math.abs(targetX - sourceX) / 2);
    positionedEdges.push({
      edge,
      path: [
        "M",
        sourceX,
        sourceY,
        "C",
        sourceX + curve * direction,
        sourceY,
        targetX - curve * direction,
        targetY,
        targetX,
        targetY,
      ].join(" "),
      labelX: (sourceX + targetX) / 2,
      labelY: (sourceY + targetY) / 2 - 7,
    });
  }
  const maximumColumn = Math.max(0, ...rank.values());
  const maximumRows = Math.max(
    1,
    ...[...groups.values()].map((items) => items.length),
  );
  return {
    nodes: positioned,
    edges: positionedEdges,
    width:
      canvasPadding * 2 +
      (maximumColumn + 1) * runGraphNodeWidth +
      maximumColumn * horizontalGap,
    height:
      canvasPadding * 2 +
      maximumRows * runGraphNodeHeight +
      (maximumRows - 1) * verticalGap,
  };
}

function compareNodes(left: RunNode, right: RunNode): number {
  return (
    left.createdAt.localeCompare(right.createdAt) ||
    left.ref.localeCompare(right.ref)
  );
}
