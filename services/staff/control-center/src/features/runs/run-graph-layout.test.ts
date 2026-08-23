import { describe, expect, it } from "vitest";

import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import { layoutRunGraph } from "@/features/runs/run-graph-layout";

function node(ref: string, createdAt: string): RunNode {
  return {
    ref,
    runRef: "run_example",
    type: "AGENT_EXECUTION",
    state: "RUNNING",
    displayName: ref,
    attempt: 1,
    artifactRefs: [],
    childRunRefs: [],
    createdAt,
    nextActions: [],
  };
}

function edge(
  ref: string,
  sourceNodeRef: string,
  targetNodeRef: string,
  type: RunEdge["type"] = "DELEGATED_TO",
): RunEdge {
  return {
    ref,
    runRef: "run_example",
    sourceNodeRef,
    targetNodeRef,
    type,
    label: type,
  };
}

describe("layoutRunGraph", () => {
  it("раскладывает authoritative delegation слева направо", () => {
    const layout = layoutRunGraph(
      [
        node("node_root", "2026-01-01T00:00:00Z"),
        node("node_child_a", "2026-01-01T00:00:01Z"),
        node("node_child_b", "2026-01-01T00:00:02Z"),
      ],
      [
        edge("edge_a", "node_root", "node_child_a"),
        edge("edge_b", "node_root", "node_child_b"),
      ],
    );
    const positions = new Map(
      layout.nodes.map((item) => [item.node.ref, item]),
    );
    expect(positions.get("node_child_a")?.x).toBeGreaterThan(
      positions.get("node_root")?.x ?? Number.MAX_SAFE_INTEGER,
    );
    expect(positions.get("node_child_a")?.y).not.toBe(
      positions.get("node_child_b")?.y,
    );
    expect(layout.edges).toHaveLength(2);
  });

  it("не создаёт phantom nodes для неизвестных концов ребра", () => {
    const layout = layoutRunGraph(
      [node("node_root", "2026-01-01T00:00:00Z")],
      [edge("edge_missing", "node_root", "node_missing")],
    );
    expect(layout.nodes.map((item) => item.node.ref)).toEqual(["node_root"]);
    expect(layout.edges).toHaveLength(0);
  });

  it("ограниченно раскладывает повреждённый циклический snapshot", () => {
    const layout = layoutRunGraph(
      [
        node("node_a", "2026-01-01T00:00:00Z"),
        node("node_b", "2026-01-01T00:00:01Z"),
      ],
      [
        edge("edge_a", "node_a", "node_b", "CONTINUES"),
        edge("edge_b", "node_b", "node_a", "CONTINUES"),
      ],
    );
    expect(layout.nodes).toHaveLength(2);
    expect(layout.width).toBeLessThan(1000);
    expect(layout.edges).toHaveLength(2);
  });
});
