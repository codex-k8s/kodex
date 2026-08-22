import { describe, expect, it } from "vitest";

import type {
  Run,
  RunEvent,
  RunGraph,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import {
  reduceRunEvent,
  type RunProjection,
} from "@/features/platform/run-reducer";

const occurredAt = "2026-08-22T12:00:00Z";
const rootRunRef = "run_root0001";
const rootNodeRef = "node_root001";

function rootRun(): Run {
  return {
    ref: rootRunRef,
    version: 1,
    projectRef: "project_0001",
    sessionRef: "session_0001",
    rootRunRef,
    target: {
      type: "AGENT",
      ref: "agent_root01",
      displayName: "Координатор",
      version: 1,
    },
    title: "Обработка обращения",
    state: "RUNNING",
    source: "CONTROL_CENTER",
    initiator: { ref: "user_owner01", displayName: "Владелец" },
    attempt: 1,
    graphRevision: 2,
    lastEventSequence: 1,
    createdAt: occurredAt,
    nextActions: ["OPEN", "CANCEL"],
  };
}

function rootNode(): RunNode {
  return {
    ref: rootNodeRef,
    runRef: rootRunRef,
    type: "ROOT_PROCESS",
    state: "RUNNING",
    displayName: "Обработка обращения",
    attempt: 1,
    artifactRefs: [],
    childRunRefs: [],
    createdAt: occurredAt,
    nextActions: [],
  };
}

function projection(): RunProjection {
  const run = rootRun();
  const graph: RunGraph = {
    runRef: rootRunRef,
    revision: 2,
    sequence: 1,
    nodes: [rootNode()],
    edges: [],
  };
  return {
    runs: { [rootRunRef]: run },
    graphs: { [rootRunRef]: graph },
    events: { [rootRunRef]: {} },
    gates: {},
    artifacts: {},
  };
}

function delegationEvent(): RunEvent {
  const childNode: RunNode = {
    ref: "node_child01",
    runRef: "run_child001",
    parentNodeRef: rootNodeRef,
    type: "AGENT_EXECUTION",
    state: "QUEUED",
    displayName: "Специалист поддержки",
    attempt: 1,
    artifactRefs: [],
    childRunRefs: [],
    createdAt: occurredAt,
    nextActions: [],
  };
  return {
    ref: "event_000002",
    runRef: rootRunRef,
    sequence: 2,
    graphRevision: 3,
    type: "DELEGATION_CREATED",
    nodeRef: childNode.ref,
    edgeRef: "edge_delegate1",
    summary: "Дочерний агент запущен",
    runState: "RUNNING",
    nodeState: "QUEUED",
    occurredAt,
    run: {
      ref: rootRunRef,
      version: 1,
      state: "RUNNING",
      graphRevision: 3,
      lastEventSequence: 2,
      artifactRefs: [],
      gateRefs: [],
      nextActions: ["OPEN", "CANCEL"],
    },
    node: childNode,
    edge: {
      ref: "edge_delegate1",
      runRef: rootRunRef,
      sourceNodeRef: rootNodeRef,
      targetNodeRef: childNode.ref,
      type: "DELEGATED_TO",
      label: "",
    },
  };
}

describe("reduceRunEvent", () => {
  it("атомарно добавляет server-owned node и edge", () => {
    const state = projection();
    const outcome = reduceRunEvent(state, delegationEvent());

    expect(outcome).toBe("applied");
    expect(state.graphs[rootRunRef]?.sequence).toBe(2);
    expect(state.graphs[rootRunRef]?.revision).toBe(3);
    expect(state.graphs[rootRunRef]?.nodes.map((node) => node.ref)).toContain(
      "node_child01",
    );
    expect(state.graphs[rootRunRef]?.edges).toHaveLength(1);
    expect(state.runs[rootRunRef]?.lastEventSequence).toBe(2);
  });

  it("игнорирует повтор at-least-once delivery", () => {
    const state = projection();
    const event = delegationEvent();
    expect(reduceRunEvent(state, event)).toBe("applied");
    expect(reduceRunEvent(state, event)).toBe("duplicate");
    expect(state.graphs[rootRunRef]?.nodes).toHaveLength(2);
    expect(state.graphs[rootRunRef]?.edges).toHaveLength(1);
  });

  it("обнаруживает sequence gap без создания phantom node", () => {
    const state = projection();
    const event = delegationEvent();
    event.sequence = 3;
    event.run.lastEventSequence = 3;

    expect(reduceRunEvent(state, event)).toBe("gap");
    expect(state.graphs[rootRunRef]?.nodes).toHaveLength(1);
    expect(state.events[rootRunRef]).toEqual({});
  });

  it("закрыто отклоняет edge к неизвестной вершине", () => {
    const state = projection();
    const event = delegationEvent();
    if (event.edge) event.edge.sourceNodeRef = "node_missing1";

    expect(reduceRunEvent(state, event)).toBe("invalid");
    expect(state.graphs[rootRunRef]?.sequence).toBe(1);
    expect(state.events[rootRunRef]).toEqual({});
  });
});
