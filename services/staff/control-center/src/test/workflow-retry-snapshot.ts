import type {
  Run,
  RunEdge,
  RunGraph,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

// Только тестовая owner-проекция: цепочка business attempts с отдельным
// technical final; значения не являются разрешением на создание retry.
export function workflowRetrySnapshot(
  rootInput: Run,
  previous = 1,
  accepted = false,
) {
  const root = structuredClone(rootInput);
  root.target = {
    type: "WORKFLOW",
    ref: "wfl_fixture1",
    displayName: "Процесс",
    version: 1,
  };
  const agentRef = "agt_fixture1";
  const child = (ref: string): Run => {
    const value = structuredClone(root);
    value.ref = ref;
    value.rootRunRef = root.ref;
    value.parentRunRef = root.ref;
    value.source = "AGENT_DELEGATION";
    value.target = {
      type: "AGENT",
      ref: agentRef,
      displayName: "Исполнитель",
      version: 1,
    };
    value.attempt = 1;
    value.sessionRef = `ses_${ref}`;
    value.sessionReadiness = {
      sessionRef: value.sessionRef,
      storageState: "LIVE",
      reason: "NO_SESSION_BLOCKER",
    };
    delete value.retryOfRunRef;
    return value;
  };
  const prior = Array.from({ length: previous }, (_, index) =>
    child(
      index + 1 === previous
        ? "run_previous"
        : `run_history${String(index + 1)}`,
    ),
  );
  const fresh = child("run_current1");
  for (const run of prior) run.state = "SUCCEEDED";
  const rootNode: RunNode = {
    ref: "nod_root0001",
    runRef: root.ref,
    type: "ROOT_PROCESS",
    state: "RUNNING",
    displayName: "Процесс",
    attempt: 1,
    createdAt: root.createdAt,
    artifactRefs: [],
    childRunRefs: [],
    nextActions: [],
  };
  const physical = (ref: string, run: Run, attempt: number): RunNode => ({
    ref,
    runRef: run.ref,
    parentNodeRef: rootNode.ref,
    type: "AGENT_EXECUTION",
    state: "SUCCEEDED",
    displayName: "Исполнитель",
    agentRef,
    attempt,
    createdAt: root.createdAt,
    artifactRefs: [],
    childRunRefs: [],
    nextActions: [],
  });
  const anchors = prior.map((run, index) =>
    physical(
      index === 0 ? "nod_origin01" : `nod_history${String(index + 1)}`,
      run,
      1,
    ),
  );
  const origin = anchors[0],
    oldRun = prior[prior.length - 1],
    oldAnchor = anchors[anchors.length - 1];
  if (!origin || !oldRun || !oldAnchor)
    throw new Error("Missing bounded business retry fixture");
  origin.workflowResults = prior.map((run, index) => ({
    attempt: index + 1,
    runRef: run.ref,
    accepted,
    reason: accepted ? "" : "RESULT_INVALID",
    results: [],
  }));
  const middle = physical("nod_technical2", oldRun, 2),
    final = physical("nod_previous", oldRun, 3),
    current = physical("nod_current1", fresh, 1);
  middle.parentNodeRef = oldAnchor.ref;
  final.parentNodeRef = middle.ref;
  current.state = "QUEUED";
  const retryEdge = (
    source: RunNode,
    target: RunNode,
    attempt: number,
  ): RunEdge => ({
    ref: `edg_business${String(attempt)}`,
    runRef: root.ref,
    sourceNodeRef: source.ref,
    targetNodeRef: target.ref,
    type: "RETRY_OF",
    label: "",
    serverWorkflowRetry: {
      workflowVersionRef: "wfv_fixture1",
      workflowVersionDigest: "a".repeat(64),
      originNodeRef: origin.ref,
      stepKey: "work",
      previousBusinessAttempt: attempt - 1,
      businessAttempt: attempt,
    },
  });
  const edges: RunEdge[] = [];
  for (let index = 1; index < anchors.length; index++) {
    const source = anchors[index - 1],
      target = anchors[index];
    if (!source || !target)
      throw new Error("Missing business retry fixture anchor");
    edges.push(retryEdge(source, target, index + 1));
  }
  edges.push({
    ref: "edg_technical1",
    runRef: root.ref,
    sourceNodeRef: oldAnchor.ref,
    targetNodeRef: middle.ref,
    type: "CONTINUES",
    label: "",
  });
  edges.push({
    ref: "edg_technical2",
    runRef: root.ref,
    sourceNodeRef: middle.ref,
    targetNodeRef: final.ref,
    type: "CONTINUES",
    label: "",
  });
  const retry = retryEdge(final, current, previous + 1);
  edges.push(retry);
  const graph: RunGraph = {
    runRef: root.ref,
    revision: root.graphRevision,
    sequence: root.lastEventSequence,
    nodes: [rootNode, ...anchors, middle, final, current],
    edges,
  };
  return {
    graph,
    runs: [root, ...prior, fresh],
    origin,
    retry,
    fresh,
    current,
    final,
  };
}
