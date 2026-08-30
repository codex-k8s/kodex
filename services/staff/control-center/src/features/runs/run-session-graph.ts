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
  const nodes = graph.nodes.filter(isRunSessionNode);
  const nodeRefs = new Set(nodes.map((node) => node.ref));
  const edges = graph.edges.filter((edge) => sessionEdge(edge, nodeRefs));

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
  return nodes.find((node) => node.state === "RUNNING")?.ref ?? nodes[0]?.ref;
}

function sessionEdge(edge: RunEdge, nodeRefs: ReadonlySet<string>): boolean {
  return nodeRefs.has(edge.sourceNodeRef) && nodeRefs.has(edge.targetNodeRef);
}
