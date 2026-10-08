import { describe, expect, it } from "vitest";
import { getTransformForBounds } from "@vue-flow/core";

import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import {
  callbackRunEdgePath,
  layoutRunGraph,
  runGraphNodeHeight,
  runGraphNodeWidth,
  runGraphContentBounds,
  smoothRunEdgePath,
} from "@/features/runs/run-graph-layout";
import {
  runGraphFitViewOptions,
  runGraphFitTransform,
  runGraphInitialFitOptions,
  runGraphMinimumZoom,
  runGraphMaximumZoom,
} from "@/features/runs/run-graph-flow";

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

function sampleCurve(path: string): Array<{ x: number; y: number }> {
  const coordinates = path.match(/-?\d+(?:\.\d+)?/g)?.map(Number) ?? [];
  const points: Array<{ x: number; y: number }> = [];
  let x = required(coordinates[0]);
  let y = required(coordinates[1]);
  for (let index = 2; index < coordinates.length; index += 6) {
    const cx1 = required(coordinates[index]);
    const cy1 = required(coordinates[index + 1]);
    const cx2 = required(coordinates[index + 2]);
    const cy2 = required(coordinates[index + 3]);
    const endX = required(coordinates[index + 4]);
    const endY = required(coordinates[index + 5]);
    for (let step = 1; step < 100; step += 1) {
      const t = step / 100;
      const remaining = 1 - t;
      points.push({
        x:
          remaining ** 3 * x +
          3 * remaining ** 2 * t * cx1 +
          3 * remaining * t ** 2 * cx2 +
          t ** 3 * endX,
        y:
          remaining ** 3 * y +
          3 * remaining ** 2 * t * cy1 +
          3 * remaining * t ** 2 * cy2 +
          t ** 3 * endY,
      });
    }
    x = endX;
    y = endY;
  }
  return points;
}

describe("layoutRunGraph", () => {
  it.each([700, 412])(
    "сохраняет читаемый начальный узел и полный обзор 48 узлов с callback при ширине %i",
    (width) => {
      const height = 480;
      const nodes = Array.from({ length: 48 }, (_, index) =>
        node(
          `node_${String(index).padStart(2, "0")}`,
          `2026-01-01T00:00:${String(index).padStart(2, "0")}Z`,
        ),
      );
      const forward = nodes
        .slice(1)
        .map((item, index) =>
          edge(
            `delegation_${String(index)}`,
            required(nodes[Math.floor(index / 3)]).ref,
            item.ref,
          ),
        );
      const edges = [
        ...forward,
        ...forward.map((item, index) =>
          edge(
            `callback_${String(index)}`,
            item.targetNodeRef,
            item.sourceNodeRef,
            "CALLBACK_TO",
          ),
        ),
      ];
      const layout = layoutRunGraph(nodes, edges);
      for (const selectedRef of [undefined, "node_25"]) {
        const options = runGraphInitialFitOptions(
          width,
          nodes,
          edges,
          selectedRef,
          true,
          height,
        );
        expect(options.nodes?.[0]).toBe(selectedRef ?? "node_00");
        const bounds = runGraphContentBounds(layout, options.nodes);
        const viewport = runGraphFitTransform(bounds, width, height, options);
        expect(viewport.zoom).toBeGreaterThanOrEqual(0.85);
        for (const ref of options.nodes ?? []) {
          const item = required(
            layout.nodes.find((item) => item.node.ref === ref),
          );
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
      const options = runGraphFitViewOptions(width, true);
      const viewport = runGraphFitTransform(
        layout.bounds,
        width,
        height,
        options,
      );
      expect(viewport.zoom).toBeLessThan(runGraphMinimumZoom);
      for (const point of [
        { x: layout.bounds.x, y: layout.bounds.y },
        {
          x: layout.bounds.x + layout.bounds.width,
          y: layout.bounds.y + layout.bounds.height,
        },
      ]) {
        expect(point.x * viewport.zoom + viewport.x).toBeGreaterThan(0);
        expect(point.y * viewport.zoom + viewport.y).toBeGreaterThan(0);
        expect(point.x * viewport.zoom + viewport.x).toBeLessThan(width);
        expect(point.y * viewport.zoom + viewport.y).toBeLessThan(height);
      }
    },
  );

  it.each([
    { width: 700, height: 480 },
    { width: 412, height: 480 },
  ])(
    "вмещает карточки и все callback-полосы в область $width × $height",
    ({ width, height }) => {
      const nodes = [
        node("root", "2026-01-01T00:00:00Z"),
        node("manager", "2026-01-01T00:00:01Z"),
        node("developer", "2026-01-01T00:00:02Z"),
      ];
      const layout = layoutRunGraph(nodes, [
        edge("root_manager", "root", "manager"),
        edge("manager_developer", "manager", "developer"),
        ...Array.from({ length: 3 }, (_, index) =>
          edge(
            `callback_${String(index)}`,
            "developer",
            "manager",
            "CALLBACK_TO",
          ),
        ),
      ]);
      const options = runGraphFitViewOptions(width, true);
      const viewport = getTransformForBounds(
        layout.bounds,
        width,
        height,
        options.minZoom ?? runGraphMinimumZoom,
        options.maxZoom ?? runGraphMaximumZoom,
        options.padding,
      );
      expect(layout.bounds.y).toBeLessThan(
        Math.min(...layout.nodes.map((item) => item.y)) - 180,
      );
      const points = layout.edges
        .filter((item) => item.edge.type === "CALLBACK_TO")
        .flatMap((item) => sampleCurve(required(item.path)));
      points.push(
        ...layout.nodes.flatMap((item) => [
          { x: item.x, y: item.y },
          { x: item.x + runGraphNodeWidth, y: item.y + runGraphNodeHeight },
        ]),
      );
      for (const point of points) {
        expect(point.x * viewport.zoom + viewport.x).toBeGreaterThan(0);
        expect(point.x * viewport.zoom + viewport.x).toBeLessThan(width);
        expect(point.y * viewport.zoom + viewport.y).toBeGreaterThan(0);
        expect(point.y * viewport.zoom + viewport.y).toBeLessThan(height);
      }
    },
  );

  it("считает bounds выбранной окрестности, включая только её обратные связи", () => {
    const nodes = [
      node("root", "2026-01-01T00:00:00Z"),
      node("manager", "2026-01-01T00:00:01Z"),
      node("developer", "2026-01-01T00:00:02Z"),
    ];
    const layout = layoutRunGraph(nodes, [
      edge("root_manager", "root", "manager"),
      edge("manager_developer", "manager", "developer"),
      edge("callback", "developer", "manager", "CALLBACK_TO"),
    ]);
    const isolated = runGraphContentBounds(layout, ["developer"]);
    const developer = required(
      layout.nodes.find((item) => item.node.ref === "developer"),
    );
    expect(isolated).toEqual({
      x: developer.x,
      y: developer.y,
      width: runGraphNodeWidth,
      height: runGraphNodeHeight,
    });
    const paired = runGraphContentBounds(layout, ["manager", "developer"]);
    expect(paired.y).toBe(layout.bounds.y);
    expect(paired.x + paired.width).toBeGreaterThan(
      developer.x + runGraphNodeWidth,
    );
  });

  it("возвращает callback над четырьмя карточками без изменения основной цепочки", () => {
    const nodes = [
      node("root", "2026-01-01T00:00:00Z"),
      node("manager", "2026-01-01T00:00:01Z"),
      node("developer", "2026-01-01T00:00:02Z"),
      node("continuation", "2026-01-01T00:00:03Z"),
    ];
    const forward = [
      edge("root_manager", "root", "manager"),
      edge("manager_developer", "manager", "developer"),
      edge("manager_continuation", "manager", "continuation", "CONTINUES"),
    ];
    const baseline = layoutRunGraph(nodes, forward);
    const layout = layoutRunGraph(nodes, [
      ...forward,
      edge("callback", "developer", "manager", "CALLBACK_TO"),
    ]);
    expect(layout.nodes).toEqual(baseline.nodes);
    expect(layout.edges.slice(0, forward.length)).toEqual(baseline.edges);
    const path = required(layout.edges.at(-1)?.path);
    const source = required(
      layout.nodes.find((item) => item.node.ref === "developer"),
    );
    const target = required(
      layout.nodes.find((item) => item.node.ref === "manager"),
    );
    expect(
      path.startsWith(
        [
          "M",
          source.x + runGraphNodeWidth,
          source.y + runGraphNodeHeight / 2,
          "C",
          "",
        ].join(" "),
      ),
    ).toBe(true);
    expect(
      path.endsWith([target.x, target.y + runGraphNodeHeight / 2].join(" ")),
    ).toBe(true);
    expect(path).not.toContain(" L ");
    const points = sampleCurve(path);
    expect(Math.min(...points.map((point) => point.y))).toBeLessThan(
      Math.min(...layout.nodes.map((item) => item.y)) - 200,
    );
    for (const point of points) {
      expect(
        layout.nodes.some(
          (item) =>
            point.x > item.x &&
            point.x < item.x + runGraphNodeWidth &&
            point.y > item.y &&
            point.y < item.y + runGraphNodeHeight,
        ),
      ).toBe(false);
    }
  });

  it("разносит callback по стабильным полосам при перестановке snapshot", () => {
    const nodes = [
      node("manager", "2026-01-01T00:00:00Z"),
      node("developer_a", "2026-01-01T00:00:01Z"),
      node("developer_b", "2026-01-01T00:00:02Z"),
    ];
    const edges = [
      edge("delegation_a", "manager", "developer_a"),
      edge("delegation_b", "manager", "developer_b"),
      edge("callback_a", "developer_a", "manager", "CALLBACK_TO"),
      edge("callback_b", "developer_b", "manager", "CALLBACK_TO"),
    ];
    const layout = layoutRunGraph(nodes, edges);
    const reversed = layoutRunGraph([...nodes].reverse(), [...edges].reverse());
    const paths = new Map(
      reversed.edges.map((item) => [item.edge.ref, item.path]),
    );
    for (const item of layout.edges)
      expect(item.path).toBe(paths.get(item.edge.ref));
    const callbacks = layout.edges.filter(
      (item) => item.edge.type === "CALLBACK_TO",
    );
    const highest = callbacks.map((item) =>
      Math.min(...sampleCurve(required(item.path)).map((point) => point.y)),
    );
    expect(Math.abs(required(highest[0]) - required(highest[1]))).toBeCloseTo(
      32,
    );
  });

  it("сохраняет callback вне колонок при возврате через несколько уровней", () => {
    const cards = [
      { x: 0, y: 150 },
      { x: 344, y: 150 },
      { x: 688, y: 150 },
      { x: 1032, y: 150 },
      { x: 1032, y: -26 },
    ];
    const path = callbackRunEdgePath(
      required(cards[3]),
      required(cards[0]),
      cards,
    );
    for (const point of sampleCurve(path)) {
      expect(
        cards.some(
          (item) =>
            point.x > item.x &&
            point.x < item.x + runGraphNodeWidth &&
            point.y > item.y &&
            point.y < item.y + runGraphNodeHeight,
        ),
      ).toBe(false);
    }
  });

  it("не сводит дугу callback к петле в зазоре перед продолжением", () => {
    const cards = [
      { x: 0, y: 0 },
      { x: 344, y: 0 },
    ];
    const path = callbackRunEdgePath(
      required(cards[0]),
      required(cards[1]),
      cards,
    );
    const points = sampleCurve(path);
    const corridor = points.slice(99, 198);
    expect(required(corridor.at(-1)).x).toBeGreaterThan(
      required(corridor[0]).x,
    );
    for (const point of points) {
      expect(
        cards.some(
          (item) =>
            point.x > item.x &&
            point.x < item.x + runGraphNodeWidth &&
            point.y > item.y &&
            point.y < item.y + runGraphNodeHeight,
        ),
      ).toBe(false);
    }
  });

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
