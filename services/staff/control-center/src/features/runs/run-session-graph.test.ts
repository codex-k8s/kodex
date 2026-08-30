import { describe, expect, it } from "vitest";

import {
  indexRunSessionOwnership,
  isRunSessionNode,
  projectRunSessionGraph,
  resolveRunSessionSelection,
} from "@/features/runs/run-session-graph";
import type {
  RunEdge,
  RunGraph,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

function node(
  ref: string,
  type: RunNode["type"],
  parentNodeRef?: string,
): RunNode {
  return {
    ref,
    runRef: "run_example",
    parentNodeRef,
    type,
    state: type === "AGENT_EXECUTION" ? "RUNNING" : "WAITING",
    displayName: ref,
    attempt: 1,
    artifactRefs: [],
    childRunRefs: [],
    createdAt: "2026-08-30T08:00:00Z",
    nextActions: [],
  };
}

function edge(
  ref: string,
  sourceNodeRef: string,
  targetNodeRef: string,
): RunEdge {
  return {
    ref,
    runRef: "run_example",
    sourceNodeRef,
    targetNodeRef,
    type: "DELEGATED_TO",
    label: "Делегирование",
  };
}

describe("run session graph", () => {
  it("оставляет на canvas только логические Session nodes", () => {
    const root = node("node_root", "ROOT_PROCESS");
    const agent = node("node_agent", "AGENT_EXECUTION", root.ref);
    const tool = node("node_tool", "EXTERNAL_ACTION", agent.ref);
    const gate = node("node_gate", "HUMAN_GATE", agent.ref);
    const graph: RunGraph = {
      runRef: root.runRef,
      revision: 4,
      sequence: 9,
      nodes: [root, agent, tool, gate],
      edges: [
        edge("edge_session", root.ref, agent.ref),
        edge("edge_tool", agent.ref, tool.ref),
        edge("edge_gate", agent.ref, gate.ref),
      ],
    };

    const projected = projectRunSessionGraph(graph);

    expect(projected.nodes.map((item) => item.ref)).toEqual([
      root.ref,
      agent.ref,
    ]);
    expect(projected.edges.map((item) => item.ref)).toEqual(["edge_session"]);
    expect(projected.sequence).toBe(graph.sequence);
    expect(projected.revision).toBe(graph.revision);
    expect(projected.nodes.every(isRunSessionNode)).toBe(true);
  });

  it("сворачивает служебный control-узел в связь между Session", () => {
    const root = node("node_root", "ROOT_PROCESS");
    const tool = node("node_tool", "EXTERNAL_ACTION", root.ref);
    const agent = node("node_agent", "AGENT_EXECUTION", tool.ref);
    const graph: RunGraph = {
      runRef: root.runRef,
      revision: 4,
      sequence: 9,
      nodes: [root, tool, agent],
      edges: [
        edge("edge_tool", root.ref, tool.ref),
        edge("edge_agent", tool.ref, agent.ref),
      ],
    };

    const projected = projectRunSessionGraph(graph);

    expect(projected.nodes.map((item) => item.ref)).toEqual([
      root.ref,
      agent.ref,
    ]);
    expect(projected.edges).toEqual([
      expect.objectContaining({
        ref: "edge_agent",
        sourceNodeRef: root.ref,
        targetNodeRef: agent.ref,
      }),
    ]);
  });

  it("привязывает tool и gate события к ближайшей родительской Session", () => {
    const root = node("node_root", "ROOT_PROCESS");
    const agent = node("node_agent", "AGENT_EXECUTION", root.ref);
    const tool = node("node_tool", "EXTERNAL_ACTION", agent.ref);
    const gate = node("node_gate", "HUMAN_GATE", tool.ref);

    const ownership = indexRunSessionOwnership([root, agent, tool, gate]);

    expect(ownership.get(root.ref)).toBe(root.ref);
    expect(ownership.get(agent.ref)).toBe(agent.ref);
    expect(ownership.get(tool.ref)).toBe(agent.ref);
    expect(ownership.get(gate.ref)).toBe(agent.ref);
  });

  it("закрыто обрабатывает поврежденный parent cycle без Session", () => {
    const left = node("node_left", "EXTERNAL_ACTION", "node_right");
    const right = node("node_right", "HUMAN_GATE", "node_left");

    const ownership = indexRunSessionOwnership([left, right]);

    expect(ownership.size).toBe(0);
  });

  it("сохраняет выбранную Session при realtime обновлении графа", () => {
    const root = node("node_root", "ROOT_PROCESS");
    const selected = node("node_selected", "AGENT_EXECUTION", root.ref);
    selected.state = "SUCCEEDED";
    const active = node("node_active", "AGENT_EXECUTION", root.ref);
    const ownership = indexRunSessionOwnership([root, selected, active]);

    expect(
      resolveRunSessionSelection(
        [root, selected, active],
        ownership,
        selected.ref,
      ),
    ).toBe(selected.ref);
    expect(
      resolveRunSessionSelection(
        [root, selected, active],
        ownership,
        undefined,
        "node_tool",
      ),
    ).toBe(active.ref);
  });

  it("маршрутизирует requested control node к родительской Session", () => {
    const root = node("node_root", "ROOT_PROCESS");
    const agent = node("node_agent", "AGENT_EXECUTION", root.ref);
    const tool = node("node_tool", "EXTERNAL_ACTION", agent.ref);
    const ownership = indexRunSessionOwnership([root, agent, tool]);

    expect(
      resolveRunSessionSelection([root, agent], ownership, root.ref, tool.ref),
    ).toBe(agent.ref);
  });

  it("после reload выбирает завершенную Session с результатами", () => {
    const root = node("node_root", "ROOT_PROCESS");
    root.state = "SUCCEEDED";
    const agent = node("node_agent", "AGENT_EXECUTION", root.ref);
    agent.state = "SUCCEEDED";
    agent.artifactRefs = ["art_result"];
    const ownership = indexRunSessionOwnership([root, agent]);

    expect(resolveRunSessionSelection([root, agent], ownership)).toBe(
      agent.ref,
    );
  });
});
