import type {
  Run,
  RunEdge,
  RunGraph,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import { assertRunOwner, sameAssistantRunPin } from "@/features/runs/run-owner";

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
  const serverTargets = new Set<string>(),
    serverSlots = new Set<string>();
  let serverVersion: string | undefined;
  for (const edge of graph.edges) {
    if (edge.serverWorkflowRetry !== undefined) {
      const proof = edge.serverWorkflowRetry;
      if (!validServerWorkflowRetrySnapshot(graph, edge, nodes, runs))
        throw new Error("Invalid server workflow retry read proof");
      const version = `${proof.workflowVersionRef}:${proof.workflowVersionDigest}`;
      const slot = `${proof.originNodeRef}:${String(proof.businessAttempt)}`;
      if (
        serverTargets.has(edge.targetNodeRef) ||
        serverSlots.has(slot) ||
        (serverVersion !== undefined && serverVersion !== version)
      )
        throw new Error("Ambiguous server workflow retry read proof");
      serverVersion = version;
      serverTargets.add(edge.targetNodeRef);
      serverSlots.add(slot);
      continue;
    }
    if (edge.type !== "RETRY_OF" && edge.type !== "CONTINUES") continue;
    const oldNode = nodes.get(edge.sourceNodeRef),
      newNode = nodes.get(edge.targetNodeRef);
    if (
      edge.type === "CONTINUES" &&
      oldNode &&
      newNode &&
      oldNode.runRef === newNode.runRef
    )
      continue;
    const old = oldNode && runs.get(oldNode.runRef),
      fresh = newNode && runs.get(newNode.runRef);
    if (
      !old ||
      !fresh ||
      !validPredecessorAnchors(edge.type, oldNode, newNode, old, fresh) ||
      old.ref !== old.rootRunRef ||
      fresh.ref !== fresh.rootRunRef ||
      edge.runRef !== fresh.ref ||
      old.ref === fresh.ref ||
      links.has(fresh.ref)
    )
      throw new Error("Invalid run predecessor snapshot lineage");
    if (
      (edge.type === "RETRY_OF" && fresh.retryOfRunRef !== old.ref) ||
      (edge.type === "CONTINUES" &&
        (!old.sessionRef ||
          fresh.sessionRef !== old.sessionRef ||
          old.state !== "SUCCEEDED" ||
          fresh.retryOfRunRef))
    )
      throw new Error("Invalid run predecessor snapshot pin");
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

// Только типизированное доказательство владельца связывает child retry с историей
// логического этапа. Оно не принимает результат и не разрешает новую mutation.
function validServerWorkflowRetrySnapshot(
  graph: RunGraph,
  edge: RunEdge,
  nodes: ReadonlyMap<string, RunNode>,
  runs: ReadonlyMap<string | undefined, Run | null | undefined>,
): boolean {
  const proof = edge.serverWorkflowRetry;
  if (
    !proof ||
    typeof proof !== "object" ||
    Array.isArray(proof) ||
    Object.keys(proof).length !== 6 ||
    !Object.keys(proof).every((key) =>
      [
        "workflowVersionRef",
        "workflowVersionDigest",
        "originNodeRef",
        "stepKey",
        "previousBusinessAttempt",
        "businessAttempt",
      ].includes(key),
    ) ||
    typeof proof.workflowVersionRef !== "string" ||
    proof.workflowVersionRef !== proof.workflowVersionRef.trim() ||
    !refPattern.test(proof.workflowVersionRef) ||
    typeof proof.originNodeRef !== "string" ||
    proof.originNodeRef !== proof.originNodeRef.trim() ||
    !refPattern.test(proof.originNodeRef) ||
    typeof proof.workflowVersionDigest !== "string" ||
    !/^[a-f0-9]{64}(?![\s\S])/.test(proof.workflowVersionDigest) ||
    typeof proof.stepKey !== "string" ||
    !/^[A-Za-z0-9_-]{1,96}(?![\s\S])/.test(proof.stepKey) ||
    !Number.isSafeInteger(proof.previousBusinessAttempt) ||
    proof.previousBusinessAttempt < 1 ||
    !Number.isSafeInteger(proof.businessAttempt) ||
    proof.businessAttempt !== proof.previousBusinessAttempt + 1 ||
    proof.businessAttempt > 4
  )
    return false;
  const oldNode = nodes.get(edge.sourceNodeRef),
    freshNode = nodes.get(edge.targetNodeRef),
    origin = nodes.get(proof.originNodeRef);
  const parent = origin && nodes.get(origin.parentNodeRef ?? "");
  const root = runs.get(graph.runRef),
    old = oldNode && runs.get(oldNode.runRef),
    fresh = freshNode && runs.get(freshNode.runRef);
  if (
    !root ||
    !old ||
    !fresh ||
    !origin ||
    !parent ||
    edge.type !== "RETRY_OF" ||
    edge.runRef !== graph.runRef ||
    root.ref !== graph.runRef ||
    root.rootRunRef !== graph.runRef ||
    root.target.type !== "WORKFLOW" ||
    oldNode.type !== "AGENT_EXECUTION" ||
    freshNode.type !== oldNode.type ||
    origin.type !== oldNode.type ||
    !oldNode.agentRef ||
    freshNode.agentRef !== oldNode.agentRef ||
    origin.agentRef !== oldNode.agentRef ||
    freshNode.attempt !== 1 ||
    freshNode.parentNodeRef !== origin.parentNodeRef ||
    parent.type !== "ROOT_PROCESS" ||
    !!parent.parentNodeRef ||
    parent.runRef !== graph.runRef ||
    old.ref === fresh.ref ||
    old.ref === root.ref ||
    fresh.ref === root.ref ||
    fresh.retryOfRunRef ||
    fresh.attempt !== 1
  )
    return false;
  for (const run of [old, fresh]) {
    if (
      run.rootRunRef !== root.ref ||
      run.parentRunRef !== root.ref ||
      run.projectRef !== root.projectRef ||
      run.source !== "AGENT_DELEGATION" ||
      run.target.type !== "AGENT" ||
      run.target.ref !== oldNode.agentRef
    )
      return false;
  }
  if (!serverWorkflowRetrySourcePath(graph, edge, nodes)) return false;
  const history = origin.workflowResults ?? [];
  const historyRuns = new Set(history.map((receipt) => receipt.runRef));
  if (
    history.length < proof.previousBusinessAttempt ||
    history.length > 4 ||
    historyRuns.size !== history.length ||
    history[0]?.runRef !== origin.runRef ||
    history.some(
      (receipt, index) =>
        receipt.attempt !== index + 1 ||
        typeof receipt.accepted !== "boolean" ||
        typeof receipt.runRef !== "string" ||
        receipt.runRef !== receipt.runRef.trim() ||
        !refPattern.test(receipt.runRef),
    )
  )
    return false;
  const receipts = history.filter(
    (receipt) => receipt.attempt === proof.previousBusinessAttempt,
  );
  return receipts.length === 1 && receipts[0]?.runRef === oldNode.runRef;
}

// Technical final связан с первым physical node своей business attempt
// единственной обратной CONTINUES цепью; размер уже проверенного графа — bound.
function serverWorkflowRetrySourcePath(
  graph: RunGraph,
  retry: RunEdge,
  nodes: ReadonlyMap<string, RunNode>,
): boolean {
  const proof = retry.serverWorkflowRetry;
  if (!proof) return false;
  const origin = nodes.get(proof.originNodeRef);
  let current = nodes.get(retry.sourceNodeRef);
  if (!origin || !current) return false;
  const runRef = current.runRef,
    agentRef = current.agentRef;
  const seen = new Set<string>();
  for (let depth = 0; depth < nodes.size; depth++) {
    if (
      seen.has(current.ref) ||
      current.type !== "AGENT_EXECUTION" ||
      current.runRef !== runRef ||
      current.agentRef !== agentRef ||
      !Number.isSafeInteger(current.attempt) ||
      current.attempt < 1
    )
      return false;
    const node = current;
    seen.add(node.ref);
    const incoming = graph.edges.filter(
      (edge) => edge.type === "CONTINUES" && edge.targetNodeRef === node.ref,
    );
    if (node.attempt === 1) {
      if (incoming.length !== 0 || node.parentNodeRef !== origin.parentNodeRef)
        return false;
      if (proof.previousBusinessAttempt === 1) return node.ref === origin.ref;
      const predecessors = graph.edges.filter(
        (edge) => edge.type === "RETRY_OF" && edge.targetNodeRef === node.ref,
      );
      const priorEdge = predecessors[0],
        prior = priorEdge?.serverWorkflowRetry;
      return (
        predecessors.length === 1 &&
        !!priorEdge &&
        !!prior &&
        priorEdge.runRef === graph.runRef &&
        prior.originNodeRef === origin.ref &&
        prior.stepKey === proof.stepKey &&
        prior.workflowVersionRef === proof.workflowVersionRef &&
        prior.workflowVersionDigest === proof.workflowVersionDigest &&
        prior.businessAttempt === proof.previousBusinessAttempt &&
        prior.previousBusinessAttempt + 1 === prior.businessAttempt
      );
    }
    const continuation = incoming[0];
    if (
      incoming.length !== 1 ||
      !continuation ||
      continuation.runRef !== graph.runRef ||
      continuation.sourceNodeRef !== node.parentNodeRef
    )
      return false;
    const previous = nodes.get(continuation.sourceNodeRef);
    if (!previous || previous.attempt + 1 !== node.attempt) return false;
    current = previous;
  }
  return false;
}

// Узел исполнения разрешён только для подтверждённого повтора того же помощника.
function validPredecessorAnchors(
  edgeType: string,
  oldNode: RunNode,
  newNode: RunNode,
  old: Run,
  fresh: Run,
): boolean {
  if (oldNode.type === "ROOT_PROCESS" && newNode.type === "ROOT_PROCESS")
    return true;
  const pin = old.assistantPin;
  return (
    edgeType === "RETRY_OF" &&
    oldNode.type === "AGENT_EXECUTION" &&
    newNode.type === "AGENT_EXECUTION" &&
    !oldNode.parentNodeRef &&
    !newNode.parentNodeRef &&
    old.source === "SYSTEM_ASSISTANT" &&
    fresh.source === "SYSTEM_ASSISTANT" &&
    old.target.type === "SYSTEM_ASSISTANT" &&
    fresh.target.type === "SYSTEM_ASSISTANT" &&
    !!pin?.assistantRef &&
    !!fresh.assistantPin &&
    sameAssistantRunPin(old, fresh) &&
    old.target.ref === pin.assistantRef &&
    fresh.target.ref === pin.assistantRef &&
    oldNode.agentRef === pin.assistantRef &&
    newNode.agentRef === pin.assistantRef &&
    !!old.sessionRef &&
    old.sessionRef === fresh.sessionRef &&
    Number.isSafeInteger(old.attempt) &&
    Number.isSafeInteger(fresh.attempt) &&
    old.attempt > 0 &&
    fresh.attempt === old.attempt + 1
  );
}
