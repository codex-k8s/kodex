import type {
  Artifact,
  OwnerGate,
  Run,
  RunEvent,
  RunGraph,
} from "@/shared/api/generated/openapi/types.gen";

export type RunEventOutcome = "applied" | "duplicate" | "gap" | "invalid";

export interface RunProjection {
  runs: Record<string, Run>;
  graphs: Record<string, RunGraph>;
  events: Record<string, Record<number, RunEvent>>;
  gates: Record<string, OwnerGate>;
  artifacts: Record<string, Artifact>;
}

function replaceOrAppend<T extends { ref: string }>(
  items: T[],
  value: T,
): void {
  const index = items.findIndex((item) => item.ref === value.ref);
  if (index === -1) items.push(value);
  else items[index] = value;
}

function isConsistent(event: RunEvent): boolean {
  if (
    event.run.ref !== event.runRef ||
    event.run.lastEventSequence !== event.sequence ||
    event.run.graphRevision !== event.graphRevision
  )
    return false;
  if (event.runState && event.runState !== event.run.state) return false;
  if (event.nodeRef && (!event.node || event.node.ref !== event.nodeRef))
    return false;
  if (event.nodeState && (!event.node || event.nodeState !== event.node.state))
    return false;
  if (event.edgeRef && (!event.edge || event.edge.ref !== event.edgeRef))
    return false;
  if (event.gateRef && (!event.gate || event.gate.ref !== event.gateRef))
    return false;
  if (event.type === "INCIDENT_LINKED" && !event.incident) return false;
  return !(
    event.artifactRef &&
    (!event.artifact || event.artifact.ref !== event.artifactRef)
  );
}

export function reduceRunEvent(
  projection: RunProjection,
  event: RunEvent,
): RunEventOutcome {
  const graph = projection.graphs[event.runRef];
  const currentSequence = graph?.sequence ?? 0;
  if (event.sequence <= currentSequence) return "duplicate";
  if (!graph || event.sequence !== currentSequence + 1) return "gap";
  if (!isConsistent(event) || event.graphRevision <= graph.revision)
    return "invalid";

  if (event.node) replaceOrAppend(graph.nodes, event.node);
  if (event.edge) {
    const nodeRefs = new Set(graph.nodes.map((node) => node.ref));
    if (
      !nodeRefs.has(event.edge.sourceNodeRef) ||
      !nodeRefs.has(event.edge.targetNodeRef)
    )
      return "invalid";
    replaceOrAppend(graph.edges, event.edge);
  }
  if (event.gate) projection.gates[event.gate.ref] = event.gate;
  if (event.artifact) projection.artifacts[event.artifact.ref] = event.artifact;

  const run = projection.runs[event.runRef];
  if (run) {
    run.version = event.run.version;
    run.state = event.run.state;
    run.graphRevision = event.run.graphRevision;
    run.lastEventSequence = event.run.lastEventSequence;
    run.resultSummary = event.run.resultSummary;
    run.safeErrorCode = event.run.safeErrorCode;
    run.safeErrorMessage = event.run.safeErrorMessage;
    run.startedAt = event.run.startedAt;
    run.finishedAt = event.run.finishedAt;
    run.nextActions = event.run.nextActions;
    if (event.incident) {
      const incidents = run.incidents ?? [];
      replaceOrAppend(incidents, event.incident);
      run.incidents = incidents;
    }
  }

  graph.revision = event.graphRevision;
  graph.sequence = event.sequence;
  const bucket = projection.events[event.runRef] ?? {};
  bucket[event.sequence] = event;
  projection.events[event.runRef] = bucket;
  return "applied";
}
