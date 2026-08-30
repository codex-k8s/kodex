import { describe, expect, it } from "vitest";

import type {
  Run,
  RunEvent,
  RunGraph,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import {
  mergeRunGraph,
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
    titleSource: "USER_EDITED",
    activitySummary: "Координатор обрабатывает обращение",
    state: "RUNNING",
    source: "CONTROL_CENTER",
    initiator: { ref: "user_owner01", displayName: "Владелец" },
    attempt: 1,
    graphRevision: 2,
    lastEventSequence: 1,
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      outputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
    artifactRefs: [],
    gateRefs: [],
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
      usage: {
        totalTokens: 120,
        inputTokens: 100,
        cachedInputTokens: 40,
        cacheWriteInputTokens: 0,
        outputTokens: 20,
        reasoningOutputTokens: 5,
        modelContextWindow: 200000,
      },
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
    expect(state.runs[rootRunRef]?.usage.totalTokens).toBe(120);
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
    expect(state.graphs[rootRunRef]?.nodes).toHaveLength(1);
    expect(state.events[rootRunRef]).toEqual({});
  });

  it("не теряет topology и terminal-state при racing snapshot", () => {
    const current = projection().graphs[rootRunRef];
    expect(current).toBeDefined();
    const completed = delegationEvent().node;
    expect(completed).toBeDefined();
    if (!current || !completed) return;
    completed.state = "SUCCEEDED";
    completed.finishedAt = occurredAt;
    current.sequence = 5;
    current.revision = 6;
    current.nodes.push(completed);
    const edge = delegationEvent().edge;
    expect(edge).toBeDefined();
    if (!edge) return;
    current.edges.push(edge);

    const staleSnapshot: RunGraph = {
      runRef: rootRunRef,
      sequence: 5,
      revision: 6,
      nodes: [
        rootNode(),
        { ...completed, state: "RUNNING", finishedAt: undefined },
      ],
      edges: [],
    };
    const merged = mergeRunGraph(current, staleSnapshot);

    expect(merged.nodes).toHaveLength(2);
    expect(merged.nodes.find((node) => node.ref === completed.ref)?.state).toBe(
      "SUCCEEDED",
    );
    expect(merged.edges).toHaveLength(1);
  });

  it("добавляет и обновляет server-owned инцидент необязательной доставки", () => {
    const state = projection();
    const event: RunEvent = {
      ref: "event_incident02",
      runRef: rootRunRef,
      sequence: 2,
      graphRevision: 3,
      type: "INCIDENT_LINKED",
      summary: "Доставка во внешний канал не выполнена",
      occurredAt,
      run: {
        ref: rootRunRef,
        version: 2,
        state: "RUNNING",
        graphRevision: 3,
        lastEventSequence: 2,
        usage: rootRun().usage,
        artifactRefs: [],
        gateRefs: [],
        nextActions: ["OPEN", "CANCEL"],
      },
      incident: {
        ref: "delivery_0001",
        projectRef: "project_0001",
        runRef: rootRunRef,
        category: "OPTIONAL_INTERACTION_DELIVERY",
        severity: "WARNING",
        state: "RECOVERING",
        safeSummary: "Доставка во внешний канал не выполнена",
        safeNextStep: "Доставка будет повторена автоматически",
        coreAffected: false,
        createdAt: occurredAt,
      },
    };

    expect(reduceRunEvent(state, event)).toBe("applied");
    expect(state.runs[rootRunRef]?.incidents).toEqual([event.incident]);

    event.ref = "event_incident03";
    event.sequence = 3;
    event.graphRevision = 4;
    event.run.version = 3;
    event.run.graphRevision = 4;
    event.run.lastEventSequence = 3;
    if (event.incident) {
      event.incident.state = "RESOLVED";
      event.incident.severity = "INFO";
    }
    expect(reduceRunEvent(state, event)).toBe("applied");
    expect(state.runs[rootRunRef]?.incidents).toHaveLength(1);
    expect(state.runs[rootRunRef]?.incidents?.[0]?.state).toBe("RESOLVED");
  });
});
