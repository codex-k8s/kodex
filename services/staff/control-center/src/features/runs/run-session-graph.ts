import type {
  RunEdge,
  RunGraph,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

const sessionNodeTypes = new Set<RunNode["type"]>([
  "ROOT_PROCESS",
  "AGENT_EXECUTION",
]);

export function isRunSessionNode(node: RunNode): boolean {
  return sessionNodeTypes.has(node.type);
}

export function indexRunSessionOwnership(
  nodes: RunNode[],
): ReadonlyMap<string, string> {
  const nodeByRef = new Map(nodes.map((node) => [node.ref, node]));
  const ownership = new Map<string, string>();

  for (const node of nodes) {
    const visited = new Set<string>();
    let current: RunNode | undefined = node;

    while (current && !visited.has(current.ref)) {
      visited.add(current.ref);
      if (isRunSessionNode(current)) {
        for (const ref of visited) ownership.set(ref, current.ref);
        break;
      }
      current = current.parentNodeRef
        ? nodeByRef.get(current.parentNodeRef)
        : undefined;
    }
  }

  return ownership;
}

export function projectRunSessionGraph(graph: RunGraph): RunGraph {
  const ownership = indexRunSessionOwnership(graph.nodes);
  const nodes = graph.nodes.filter(isRunSessionNode);
  const nodeRefs = new Set(nodes.map((node) => node.ref));
  const edges: RunEdge[] = [];
  const projectedEdges = new Set<string>();

  for (const edge of graph.edges) {
    const sourceNodeRef = ownership.get(edge.sourceNodeRef);
    const targetNodeRef = ownership.get(edge.targetNodeRef);
    if (
      !sourceNodeRef ||
      !targetNodeRef ||
      sourceNodeRef === targetNodeRef ||
      !nodeRefs.has(sourceNodeRef) ||
      !nodeRefs.has(targetNodeRef)
    ) {
      continue;
    }
    const key = `${sourceNodeRef}\u0000${targetNodeRef}\u0000${edge.type}`;
    if (projectedEdges.has(key)) continue;
    projectedEdges.add(key);
    edges.push({ ...edge, sourceNodeRef, targetNodeRef });
  }

  return { ...graph, nodes, edges };
}

export function resolveRunSessionSelection(
  nodes: RunNode[],
  ownership: ReadonlyMap<string, string>,
  currentRef?: string,
  requestedRef?: string,
): string | undefined {
  const visibleRefs = new Set(nodes.map((node) => node.ref));
  const requestedSessionRef = requestedRef
    ? ownership.get(requestedRef)
    : undefined;
  if (requestedSessionRef && visibleRefs.has(requestedSessionRef))
    return requestedSessionRef;
  if (currentRef && visibleRefs.has(currentRef)) return currentRef;
  return (
    nodes.find((node) => node.state === "RUNNING")?.ref ??
    nodes.find(
      (node) => node.state === "SUCCEEDED" && node.artifactRefs.length > 0,
    )?.ref ??
    nodes.find((node) => node.state === "SUCCEEDED")?.ref ??
    nodes[0]?.ref
  );
}

// Сворачиваются только успешные пустые обёртки, связанные с другим запуском.
// Узлы с результатом, действием, ошибкой или делегированием всегда видимы.
export function runGraphHistoryRoots(
  nodes: RunNode[],
  edges: RunEdge[],
): Set<string> {
  const roots = new Map(
    nodes
      .filter((node) => node.type === "ROOT_PROCESS")
      .map((node) => [node.ref, node]),
  );
  const adjacent = new Map<string, RunEdge[]>();
  for (const edge of edges) {
    for (const ref of [edge.sourceNodeRef, edge.targetNodeRef]) {
      const items = adjacent.get(ref) ?? [];
      items.push(edge);
      adjacent.set(ref, items);
    }
  }
  return new Set(
    nodes
      .filter((node) => {
        if (
          node.type !== "ROOT_PROCESS" ||
          node.state !== "SUCCEEDED" ||
          node.artifactRefs.length ||
          node.childRunRefs.length ||
          node.nextActions.length
        )
          return false;
        const links = adjacent.get(node.ref) ?? [];
        return (
          links.some(
            (edge) =>
              edge.sourceNodeRef === node.ref &&
              Date.parse(roots.get(edge.targetNodeRef)?.createdAt ?? "") >
                Date.parse(node.createdAt),
          ) &&
          links.every(
            (edge) =>
              (edge.type === "CONTINUES" || edge.type === "RETRY_OF") &&
              roots.has(edge.sourceNodeRef) &&
              roots.has(edge.targetNodeRef),
          )
        );
      })
      .map((node) => node.ref),
  );
}
