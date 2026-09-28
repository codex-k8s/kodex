import { describe, expect, it } from "vitest";

import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import {
  layoutRunGraph,
  smoothRunEdgePath,
} from "@/features/runs/run-graph-layout";

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

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Missing graph test fixture");
  return value;
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

  it("ставит повторную попытку между прежним запуском и новым исполнителем", () => {
    const layout = layoutRunGraph(
      [
        { ...node("old", "2026-01-01T00:00:00Z"), type: "ROOT_PROCESS" },
        { ...node("retry", "2026-01-01T00:00:01Z"), type: "ROOT_PROCESS" },
        node("agent", "2026-01-01T00:00:02Z"),
      ],
      [
        edge("retry_edge", "old", "retry", "RETRY_OF"),
        edge("delegation", "retry", "agent"),
      ],
    );
    const positions = new Map(
      layout.nodes.map((item) => [item.node.ref, item]),
    );
    expect(required(positions.get("old")).x).toBeLessThan(
      required(positions.get("retry")).x,
    );
    expect(required(positions.get("retry")).x).toBeLessThan(
      required(positions.get("agent")).x,
    );
    expect(layout.edges.every((item) => item.path?.startsWith("M "))).toBe(
      true,
    );
    expect(layout.edges.every((item) => item.path?.includes(" C "))).toBe(true);
  });

  it("соединяет маршрут гладкой кривой Безье без изломов", () => {
    expect(smoothRunEdgePath([])).toBeUndefined();
    const path = smoothRunEdgePath([
      { x: 0, y: 0 },
      { x: 50, y: 0 },
      { x: 100, y: 50 },
    ]);
    expect(path).toMatch(/^M 0 0 C /);
    expect(path).toContain(" C ");
    expect(path).toMatch(/ 100 50$/);
    expect(path).not.toContain(" L ");
  });

  it("разводит десятки узлов без наложения и сохраняет все связи", () => {
    const nodes = Array.from({ length: 48 }, (_, index) =>
      node(
        `node_${String(index).padStart(2, "0")}`,
        `2026-01-01T00:00:${String(index).padStart(2, "0")}Z`,
      ),
    );
    const edges = nodes
      .slice(1)
      .map((item, index) =>
        edge(
          `edge_${String(index)}`,
          required(nodes[Math.floor(index / 3)]).ref,
          item.ref,
        ),
      );
    const layout = layoutRunGraph(nodes, edges);
    expect(layout.nodes).toHaveLength(48);
    expect(layout.edges).toHaveLength(47);
    for (let left = 0; left < layout.nodes.length; left += 1) {
      for (let right = left + 1; right < layout.nodes.length; right += 1) {
        const a = required(layout.nodes[left]);
        const b = required(layout.nodes[right]);
        expect(
          a.x + 244 <= b.x ||
            b.x + 244 <= a.x ||
            a.y + 132 <= b.y ||
            b.y + 132 <= a.y,
        ).toBe(true);
      }
    }
    expect(layout.width).toBeGreaterThan(0);
    expect(layout.height).toBeGreaterThan(0);
  });
});
