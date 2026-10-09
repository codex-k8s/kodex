import { describe, expect, it } from "vitest";

import {
  runGraphFitTransform,
  runGraphFitViewOptions,
  runGraphInitialFitOptions,
} from "@/features/runs/run-graph-flow";
import {
  layoutRunGraph,
  runGraphContentBounds,
  runGraphNodeHeight,
  runGraphNodeWidth,
} from "@/features/runs/run-graph-layout";
import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

const nodes: RunNode[] = ["original", "retry", "execution"].map(
  (ref, index) => ({
    ref,
    runRef: "run_terminal",
    type: index === 2 ? "AGENT_EXECUTION" : "ROOT_PROCESS",
    state: "FAILED",
    displayName: `Завершённый шаг ${String(index + 1)}`,
    attempt: 1,
    artifactRefs: [],
    childRunRefs: [],
    createdAt: `2026-10-10T00:00:0${String(index)}Z`,
    nextActions: [],
  }),
);
const edges: RunEdge[] = [
  {
    ref: "retry_edge",
    runRef: "run_terminal",
    sourceNodeRef: "original",
    targetNodeRef: "retry",
    type: "RETRY_OF",
    label: "",
  },
  {
    ref: "delegation_edge",
    runRef: "run_terminal",
    sourceNodeRef: "retry",
    targetNodeRef: "execution",
    type: "DELEGATED_TO",
    label: "",
  },
];

describe("runGraphInitialFitOptions", () => {
  it.each([
    { width: 1184, height: 787, compact: false },
    { width: 700, height: 480, compact: true },
    { width: 412, height: 480, compact: true },
  ])(
    "завершённые три узла открываются читаемо при ширине $width",
    ({ width, height, compact }) => {
      const layout = layoutRunGraph(nodes, edges);
      const overview = runGraphFitViewOptions(width, compact);
      const overviewViewport = runGraphFitTransform(
        layout.bounds,
        width,
        height,
        overview,
      );
      expect(overview.nodes).toBeUndefined();
      expect(overviewViewport.zoom).toBeLessThan(0.85);

      for (const selectedRef of ["original", "execution"]) {
        const initial = runGraphInitialFitOptions(
          width,
          nodes,
          edges,
          selectedRef,
          compact,
          height,
        );
        expect(initial.nodes?.[0]).toBe(selectedRef);
        const viewport = runGraphFitTransform(
          runGraphContentBounds(layout, initial.nodes),
          width,
          height,
          initial,
        );
        expect(viewport.zoom).toBeGreaterThanOrEqual(0.85);
        for (const item of layout.nodes.filter((item) =>
          initial.nodes?.includes(item.node.ref),
        )) {
          expect(item.x * viewport.zoom + viewport.x).toBeGreaterThan(0);
          expect(item.y * viewport.zoom + viewport.y).toBeGreaterThan(0);
          expect(
            (item.x + runGraphNodeWidth) * viewport.zoom + viewport.x,
          ).toBeLessThan(width);
          expect(
            (item.y + runGraphNodeHeight) * viewport.zoom + viewport.y,
          ).toBeLessThan(height);
        }
      }
    },
  );

  it("добавляет ближайшего предшественника выбранной завершённой карточки, если хватает места", () => {
    expect(
      runGraphInitialFitOptions(1184, nodes, edges, "execution", false, 787)
        .nodes,
    ).toEqual(["execution", "retry"]);
  });

  it("сохраняет полный начальный обзор малого графа, когда текст остаётся читаемым", () => {
    expect(
      runGraphInitialFitOptions(1920, nodes, edges, "execution", true, 780),
    ).toEqual(runGraphFitViewOptions(1920, true));
  });
});
