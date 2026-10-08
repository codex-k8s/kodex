import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import type { Run, RunGraph } from "@/shared/api/generated/openapi/types.gen";
import { usePlatformStore } from "./store";

function requiredFixture<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Missing required test fixture");
  return value;
}

function fixture(): { graph: RunGraph; runs: Run[] } {
  const root: Run = {
    ref: "run_root0001",
    rootRunRef: "run_root0001",
    version: 4,
    projectRef: "prj_project1",
    sessionRef: "ses_root0001",
    sessionReadiness: {
      sessionRef: "ses_root0001",
      storageState: "LIVE",
      reason: "NO_SESSION_BLOCKER",
    },
    source: "CONTROL_CENTER",
    target: {
      type: "AGENT",
      ref: "agt_fixture1",
      displayName: "Сотрудник",
      version: 1,
    },
    title: "Синтетический запуск",
    titleSource: "SERVER_DEFAULT",
    activitySummary: "",
    initiator: { ref: "actor_fixture1", displayName: "Владелец" },
    attempt: 1,
    state: "RUNNING",
    graphRevision: 4,
    lastEventSequence: 7,
    createdAt: "2026-08-23T00:00:00Z",
    artifactRefs: [],
    gateRefs: [],
    nextActions: [],
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      outputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
  };
  const child = structuredClone(root);
  child.ref = "run_child001";
  child.parentRunRef = root.ref;
  child.sessionRef = "ses_child001";
  requiredFixture(child.sessionReadiness).sessionRef = child.sessionRef;
  const graph: RunGraph = {
    runRef: root.ref,
    revision: 4,
    sequence: 7,
    edges: [],
    nodes: [
      {
        ref: "nod_child001",
        runRef: child.ref,
        type: "AGENT_EXECUTION",
        state: "RUNNING",
        displayName: "Сотрудник",
        attempt: 1,
        createdAt: root.createdAt,
        artifactRefs: [],
        childRunRefs: [],
        nextActions: [],
      },
    ],
  };
  return { graph, runs: [root, child] };
}

describe("полный owner run snapshot", () => {
  beforeEach(() => setActivePinia(createPinia()));
  it("обновляет shared child storage при прежних Run.version и graph cursor без HTTP", () => {
    const store = usePlatformStore(),
      value = fixture();
    store.applyRunReadinessSnapshot(value.graph, value.runs);
    requiredFixture(
      requiredFixture(value.runs[1]).sessionReadiness,
    ).storageState = "ERROR";
    requiredFixture(requiredFixture(value.runs[1]).sessionReadiness).reason =
      "STORAGE_NOT_LIVE";
    store.applyRunReadinessSnapshot(value.graph, structuredClone(value.runs));
    expect(store.runs.run_child001?.sessionReadiness?.storageState).toBe(
      "ERROR",
    );
    expect(store.hasRunReadinessSnapshot(value.graph.runRef, 7)).toBe(true);
  });
  it("принимает точный canonical child root из graph", () => {
    const store = usePlatformStore(),
      value = fixture();
    requiredFixture(value.runs[1]).rootRunRef = requiredFixture(
      value.runs[1],
    ).ref;
    requiredFixture(value.graph.nodes[0]).runRef = value.graph.runRef;
    requiredFixture(value.graph.nodes[0]).childRunRefs = [
      requiredFixture(value.runs[1]).ref,
    ];
    store.applyRunReadinessSnapshot(value.graph, value.runs);
    expect(store.runs.run_child001?.parentRunRef).toBe(value.graph.runRef);
  });
  for (const [name, mutate] of [
    ["omitted", (v: ReturnType<typeof fixture>) => v.runs.pop()],
    [
      "duplicate graph node",
      (v: ReturnType<typeof fixture>) =>
        v.graph.nodes.push(requiredFixture(v.graph.nodes[0])),
    ],
    [
      "duplicate",
      (v: ReturnType<typeof fixture>) =>
        (v.runs[1] = requiredFixture(v.runs[0])),
    ],
    [
      "malformed child locator",
      (v: ReturnType<typeof fixture>) =>
        requiredFixture(v.graph.nodes[0]).childRunRefs.push("private/foreign"),
    ],
    [
      "foreign child locator",
      (v: ReturnType<typeof fixture>) =>
        requiredFixture(v.graph.nodes[0]).childRunRefs.push("run_foreign1"),
    ],
    [
      "unknown storage",
      (v: ReturnType<typeof fixture>) => {
        requiredFixture(
          requiredFixture(v.runs[1]).sessionReadiness,
        ).storageState = "UNKNOWN_STATE" as "ERROR";
      },
    ],
    [
      "foreign",
      (v: ReturnType<typeof fixture>) =>
        (requiredFixture(v.runs[1]).projectRef = "prj_foreign1"),
    ],
    [
      "unrelated",
      (v: ReturnType<typeof fixture>) =>
        (requiredFixture(v.runs[1]).rootRunRef = "run_foreign1"),
    ],
    [
      "missing parent",
      (v: ReturnType<typeof fixture>) => {
        requiredFixture(v.runs[1]).rootRunRef = requiredFixture(v.runs[1]).ref;
        delete requiredFixture(v.runs[1]).parentRunRef;
      },
    ],
    [
      "foreign parent",
      (v: ReturnType<typeof fixture>) => {
        requiredFixture(v.runs[1]).rootRunRef = requiredFixture(v.runs[1]).ref;
        requiredFixture(v.runs[1]).parentRunRef = "run_foreign1";
      },
    ],
    [
      "missing readiness",
      (v: ReturnType<typeof fixture>) =>
        delete requiredFixture(v.runs[1]).sessionReadiness,
    ],
    [
      "session mismatch",
      (v: ReturnType<typeof fixture>) =>
        (requiredFixture(
          requiredFixture(v.runs[1]).sessionReadiness,
        ).sessionRef = "ses_foreign1"),
    ],
    ["stale graph", (v: ReturnType<typeof fixture>) => v.graph.sequence--],
    [
      "stale version",
      (v: ReturnType<typeof fixture>) => requiredFixture(v.runs[1]).version--,
    ],
    [
      "overflow",
      (v: ReturnType<typeof fixture>) => {
        while (v.runs.length <= 128) v.runs.push(requiredFixture(v.runs[1]));
      },
    ],
  ] as const) {
    it("атомарно отклоняет " + name, () => {
      const store = usePlatformStore(),
        initial = fixture();
      store.applyRunReadinessSnapshot(
        initial.graph,
        structuredClone(initial.runs),
      );
      const before = JSON.stringify({ runs: store.runs, graphs: store.graphs });
      const corrupt = fixture();
      mutate(corrupt);
      expect(() =>
        store.applyRunReadinessSnapshot(corrupt.graph, corrupt.runs),
      ).toThrow();
      expect(JSON.stringify({ runs: store.runs, graphs: store.graphs })).toBe(
        before,
      );
    });
  }
  it("отзыв очищает только ранее подтверждённые root/child refs", () => {
    const store = usePlatformStore(),
      value = fixture();
    requiredFixture(value.runs[1]).rootRunRef = requiredFixture(
      value.runs[1],
    ).ref;
    store.applyRunReadinessSnapshot(value.graph, value.runs);
    const foreign = structuredClone(requiredFixture(value.runs[0]));
    foreign.ref = foreign.rootRunRef = "run_foreign1";
    store.runs[foreign.ref] = foreign;
    store.graphs.run_child001 = { ...value.graph, runRef: "run_child001" };
    store.clearRunReadinessSnapshot(value.graph.runRef);
    expect(store.runs.run_root0001).toBeUndefined();
    expect(store.runs.run_child001).toBeUndefined();
    expect(store.graphs.run_child001).toBeUndefined();
    expect(store.runs.run_foreign1).toBeDefined();
    expect(store.hasRunReadinessSnapshot(value.graph.runRef, 7)).toBe(false);
  });
  it("принимает root без graph nodes", () => {
    const store = usePlatformStore(),
      value = fixture();
    value.graph.nodes = [];
    value.runs.pop();
    store.applyRunReadinessSnapshot(value.graph, value.runs);
    expect(store.runs.run_root0001).toBeDefined();
  });
  const retryFixture = () => {
    const value = fixture(),
      root = requiredFixture(value.runs[0]),
      old = requiredFixture(value.runs[1]);
    old.rootRunRef = old.ref;
    delete old.parentRunRef;
    old.state = "CANCELLED";
    root.retryOfRunRef = old.ref;
    const previous = {
      ...requiredFixture(value.graph.nodes[0]),
      ref: "nod_previous",
      runRef: old.ref,
      type: "ROOT_PROCESS" as const,
    };
    const current = { ...previous, ref: "nod_current1", runRef: root.ref };
    value.graph.nodes = [previous, current];
    value.graph.edges = [
      {
        ref: "edg_retry001",
        runRef: root.ref,
        sourceNodeRef: previous.ref,
        targetNodeRef: current.ref,
        type: "RETRY_OF",
        label: "",
      },
    ];
    return value;
  };
  it("принимает cancelled retry predecessor по exact edge и owner pin", () => {
    const store = usePlatformStore(),
      value = retryFixture();
    store.applyRunReadinessSnapshot(value.graph, value.runs);
    expect(store.runs.run_child001?.state).toBe("CANCELLED");
  });
  for (const [name, mutate] of [
    [
      "missing retry edge",
      (v: ReturnType<typeof fixture>) => {
        v.graph.edges = [];
      },
    ],
    [
      "foreign retry node",
      (v: ReturnType<typeof fixture>) => {
        requiredFixture(v.graph.edges[0]).sourceNodeRef = "nod_foreign1";
      },
    ],
    [
      "retry pin mismatch",
      (v: ReturnType<typeof fixture>) => {
        requiredFixture(v.runs[0]).retryOfRunRef = "run_foreign1";
      },
    ],
    [
      "duplicate retry edge",
      (v: ReturnType<typeof fixture>) => {
        v.graph.edges.push(requiredFixture(v.graph.edges[0]));
      },
    ],
    [
      "retry cycle",
      (v: ReturnType<typeof fixture>) => {
        requiredFixture(v.runs[1]).retryOfRunRef = requiredFixture(
          v.runs[0],
        ).ref;
        v.graph.edges.push({
          ...requiredFixture(v.graph.edges[0]),
          ref: "edg_reverse1",
          runRef: requiredFixture(v.runs[1]).ref,
          sourceNodeRef: "nod_current1",
          targetNodeRef: "nod_previous",
        });
      },
    ],
  ] as const)
    it("отклоняет " + name, () => {
      const store = usePlatformStore(),
        value = retryFixture();
      mutate(value);
      expect(() =>
        store.applyRunReadinessSnapshot(value.graph, value.runs),
      ).toThrow();
      expect(Object.keys(store.runs)).toEqual([]);
    });
});
