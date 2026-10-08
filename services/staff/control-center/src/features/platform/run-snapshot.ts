import type { Run, RunGraph } from "@/shared/api/generated/openapi/types.gen";
import { assertRunOwner } from "@/features/runs/run-owner";

const refPattern = /^[A-Za-z0-9_-]{8,128}$/;
const storageStates = new Set([
  "UNTRACKED",
  "LIVE",
  "SNAPSHOT_READY",
  "SNAPSHOTTING",
  "DELETE_PVC_READY",
  "ARCHIVED",
  "RESTORE_READY",
  "RESTORING",
  "ERROR",
  "PURGED",
]);
const reasons = new Set([
  "NO_SESSION_BLOCKER",
  "STORAGE_NOT_LIVE",
  "SESSION_ACCOUNT_UNAVAILABLE",
  "EARLIER_EXECUTION",
]);
const archiveErrors = new Set([
  "NONE",
  "UNKNOWN",
  "SESSION_ARCHIVE_SOURCE_INVALID",
  "SESSION_ARCHIVE_OBJECT_WRITE_FAILED",
  "SESSION_ARCHIVE_OBJECT_READBACK_FAILED",
  "SESSION_ARCHIVE_OBJECT_DELETE_FAILED",
  "SESSION_ARCHIVE_RESTORE_INVALID",
  "SESSION_ARCHIVE_PVC_BUSY",
  "SESSION_ARCHIVE_PVC_MISSING",
  "SESSION_ARCHIVE_PVC_REPLACED",
  "SESSION_ARCHIVE_KUBERNETES_UNAVAILABLE",
  "SESSION_ARCHIVE_WORKER_FAILED",
  "SESSION_ARCHIVE_TIMEOUT",
  "SESSION_ARCHIVE_LEASE_EXPIRED",
  "SESSION_BECAME_ACTIVE",
]);

// Проверка всего owner snapshot предшествует первому изменению cache.
export function validateRunSnapshot(
  graph: RunGraph,
  values: unknown,
  organizationRef: string | undefined,
  previous: Readonly<Record<string, Run>>,
  current: RunGraph | undefined,
): Run[] {
  if (
    !Array.isArray(values) ||
    values.length < 1 ||
    values.length > 128 ||
    !Number.isSafeInteger(graph.sequence) ||
    graph.sequence < (current?.sequence ?? 0) ||
    !Number.isSafeInteger(graph.revision) ||
    graph.revision < (current?.revision ?? 0)
  )
    throw new Error("Invalid run snapshot bounds or cursor");
  const nodes = new Set<string>(),
    edges = new Set<string>();
  for (const node of graph.nodes) {
    if (!refPattern.test(node.ref) || nodes.has(node.ref))
      throw new Error("Invalid graph node identity");
    nodes.add(node.ref);
  }
  for (const edge of graph.edges) {
    if (
      !refPattern.test(edge.ref) ||
      edges.has(edge.ref) ||
      !nodes.has(edge.sourceNodeRef) ||
      !nodes.has(edge.targetNodeRef)
    )
      throw new Error("Invalid graph edge binding");
    edges.add(edge.ref);
  }
  const expected = new Set([
    graph.runRef,
    ...graph.nodes.flatMap((node) => [node.runRef, ...node.childRunRefs]),
  ]);
  if (
    [...expected].some(
      (ref) => typeof ref !== "string" || !refPattern.test(ref),
    )
  )
    throw new Error("Invalid graph run locator");
  if (expected.size !== values.length)
    throw new Error("Incomplete run snapshot");
  // Непроверенный wire может содержать null или отсутствующий элемент вопреки DTO.
  const candidates = values as (Run | null | undefined)[];
  const root = candidates.find((value) => value?.ref === graph.runRef);
  if (
    !root ||
    root.rootRunRef !== graph.runRef ||
    root.graphRevision !== graph.revision ||
    root.lastEventSequence !== graph.sequence
  )
    throw new Error("Invalid run snapshot root");
  const seen = new Set<string>();
  const predecessors = snapshotRetryPredecessors(graph, candidates);
  for (const candidate of candidates) {
    if (
      !candidate ||
      !refPattern.test(candidate.ref) ||
      !expected.has(candidate.ref) ||
      seen.has(candidate.ref) ||
      candidate.projectRef !== root.projectRef ||
      !Number.isSafeInteger(candidate.version) ||
      candidate.version < 1 ||
      candidate.version < (previous[candidate.ref]?.version ?? 0)
    )
      throw new Error("Invalid run snapshot identity or version");
    const crossRoot =
      candidate.ref === candidate.rootRunRef &&
      candidate.parentRunRef !== undefined &&
      candidate.parentRunRef !== candidate.ref &&
      expected.has(candidate.parentRunRef);
    if (
      candidate.rootRunRef !== graph.runRef &&
      !crossRoot &&
      !predecessors.has(candidate.ref)
    )
      throw new Error("Invalid run snapshot lineage");
    assertRunOwner(candidate, organizationRef);
    const readiness = candidate.sessionReadiness;
    if (
      (candidate.sessionRef && !readiness) ||
      (readiness &&
        (!candidate.sessionRef ||
          readiness.sessionRef !== candidate.sessionRef ||
          !storageStates.has(readiness.storageState) ||
          !reasons.has(readiness.reason)))
    )
      throw new Error("Invalid run snapshot session readiness");
    const task = readiness?.latestArchiveTask;
    if (
      task &&
      (!refPattern.test(task.ref) ||
        !archiveErrors.has(task.safeErrorCode) ||
        !["SNAPSHOT", "RESTORE", "DELETE_PVC"].includes(task.kind) ||
        !["READY", "CLAIMED", "SUCCEEDED", "DEAD_LETTER", "CANCELLED"].includes(
          task.state,
        ) ||
        !Number.isSafeInteger(task.attempt) ||
        !Number.isSafeInteger(task.maximumAttempts) ||
        task.attempt < 0 ||
        task.maximumAttempts < 1 ||
        task.maximumAttempts > 5 ||
        task.attempt > task.maximumAttempts)
    )
      throw new Error("Invalid run snapshot archive task");
    seen.add(candidate.ref);
  }
  return values as Run[];
}

function snapshotRetryPredecessors(
  graph: RunGraph,
  values: (Run | null | undefined)[],
): Set<string> {
  const nodes = new Map(graph.nodes.map((node) => [node.ref, node]));
  const runs = new Map(values.map((run) => [run?.ref, run]));
  const links = new Map<string, string>(),
    predecessors = new Set<string>();
  for (const edge of graph.edges) {
    if (edge.type !== "RETRY_OF") continue;
    const oldNode = nodes.get(edge.sourceNodeRef),
      newNode = nodes.get(edge.targetNodeRef);
    const old = oldNode && runs.get(oldNode.runRef),
      fresh = newNode && runs.get(newNode.runRef);
    if (
      !old ||
      !fresh ||
      oldNode.type !== "ROOT_PROCESS" ||
      newNode.type !== "ROOT_PROCESS" ||
      old.ref !== old.rootRunRef ||
      fresh.ref !== fresh.rootRunRef ||
      fresh.retryOfRunRef !== old.ref ||
      edge.runRef !== fresh.ref ||
      old.ref === fresh.ref ||
      links.has(fresh.ref)
    )
      throw new Error("Invalid retry snapshot lineage");
    links.set(fresh.ref, old.ref);
    predecessors.add(old.ref);
  }
  for (const run of values)
    if (
      run?.retryOfRunRef &&
      runs.has(run.retryOfRunRef) &&
      links.get(run.ref) !== run.retryOfRunRef
    )
      throw new Error("Missing retry snapshot edge");
  for (const ref of links.keys()) {
    const seen = new Set<string>();
    for (
      let current: string | undefined = ref;
      current;
      current = links.get(current)
    ) {
      if (seen.has(current)) throw new Error("Cyclic retry snapshot lineage");
      seen.add(current);
    }
  }
  return predecessors;
}
