import { describe, expect, it } from "vitest";
import { getTransformForBounds } from "@vue-flow/core";

import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import {
  layoutRunGraph,
  runGraphNodeHeight,
  runGraphNodeWidth,
  runGraphContentBounds,
  smoothRunEdgePath,
} from "@/features/runs/run-graph-layout";
import {
  createRunGraphFlowElements,
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

// Проверяет положение выбранных узлов в заданном viewport без изменения layout.
function expectSelectedNodesWithinViewport(
  layout: ReturnType<typeof layoutRunGraph>,
  viewport: ReturnType<typeof runGraphFitTransform>,
  refs: readonly string[] | undefined,
  width: number,
  height: number,
): void {
  for (const ref of refs ?? []) {
    const item = required(layout.nodes.find((item) => item.node.ref === ref));
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
  it("не сжимает небольшой граф между боковыми резервами в узком canvas", () => {
    const bounds = { x: 0, y: 0, width: 984, height: 450 };
    const options = runGraphFitViewOptions(809);
    const viewport = runGraphFitTransform(bounds, 809, 596, options);
    expect(viewport.zoom).toBeGreaterThan(0.5);
    expect(viewport.x).toBeGreaterThanOrEqual(32);
    expect(viewport.x + bounds.width * viewport.zoom).toBeLessThanOrEqual(
      809 - 32,
    );
    expect(viewport.y).toBeGreaterThanOrEqual(180);
    expect(viewport.y + bounds.height * viewport.zoom).toBeLessThanOrEqual(
      596 - 160,
    );
  });

  it.each([1920, 1201, 700, 412])(
    "активный Architect читаем при ширине %i; 24 узла и 26 связей остаются доступны",
    (width) => {
      const height = 780;
      const nodes = Array.from({ length: 24 }, (_, index) => ({
        ...node(
          `step_${String(index).padStart(2, "0")}`,
          `2026-01-01T00:00:${String(index).padStart(2, "0")}Z`,
        ),
        type:
          index === 0
            ? ("ROOT_PROCESS" as const)
            : ("AGENT_EXECUTION" as const),
        state:
          index === 0 || index === 2
            ? ("RUNNING" as const)
            : index === 1
              ? ("SUCCEEDED" as const)
              : ("PLANNED" as const),
        displayName: index === 2 ? "Архитектор" : `Шаг ${String(index)}`,
      }));
      const edges = nodes
        .slice(1)
        .map((item, index) =>
          edge(
            `edge_${String(index)}`,
            required(nodes[index]).ref,
            item.ref,
            "CONTINUES",
          ),
        );
      edges.push(
        edge("callback", "step_02", "step_00", "CALLBACK_TO"),
        edge("optional_retry", "step_23", "step_19", "RETRY_OF"),
        edge("optional_callback", "step_23", "step_00", "CALLBACK_TO"),
      );
      const options = runGraphInitialFitOptions(
        width,
        nodes,
        edges,
        "step_00",
        true,
        height,
        ["step_00", "step_02"],
      );
      expect(options.nodes?.[0]).toBe("step_02");
      expect(options.nodes).not.toContain("step_23");
      const layout = layoutRunGraph(nodes, edges);
      const viewport = runGraphFitTransform(
        runGraphContentBounds(layout, options.nodes),
        width,
        height,
        options,
      );
      expect(viewport.zoom).toBeGreaterThanOrEqual(0.85);
      expectSelectedNodesWithinViewport(
        layout,
        viewport,
        options.nodes,
        width,
        height,
      );
      expect(layout.nodes).toHaveLength(24);
      expect(layout.edges).toHaveLength(26);
      const elements = createRunGraphFlowElements(nodes, edges, {
        selectedRef: "step_00",
        futureRefs: new Set(nodes.slice(3).map((node) => node.ref)),
        activeRefs: new Set(["step_00", "step_02"]),
        nodeAccessibleLabel: (node) => node.displayName,
        edgeAccessibleLabel: (edge) => edge.ref,
      });
      expect(elements.nodes).toHaveLength(24);
      expect(elements.edges).toHaveLength(26);
      expect(runGraphFitViewOptions(width, true).nodes).toBeUndefined();
    },
  );

  it("малый граф тоже фокусирует execution, а не root; параллельный выбор стабилен", () => {
    const root = {
      ...node("root", "2026-01-01T00:00:00Z"),
      type: "ROOT_PROCESS" as const,
    };
    const first = node("first", "2026-01-01T00:00:01Z");
    const second = node("second", "2026-01-01T00:00:02Z");
    const nodes = [root, second, first];
    const active = ["root", "first", "second", "missing"];
    expect(
      runGraphInitialFitOptions(1201, nodes, [], "root", true, 780, active)
        .nodes?.[0],
    ).toBe("first");
    expect(
      runGraphInitialFitOptions(1201, nodes, [], "second", true, 780, active)
        .nodes?.[0],
    ).toBe("second");
    expect(
      runGraphInitialFitOptions(
        1201,
        [root, { ...first, state: "QUEUED" }],
        [],
        "root",
        true,
        780,
        active,
      ).nodes,
    ).toBeUndefined();
  });

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
        expectSelectedNodesWithinViewport(
          layout,
          viewport,
          options.nodes,
          width,
          height,
        );
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
      expect(layout.bounds.y).toBeGreaterThanOrEqual(
        Math.min(...layout.nodes.map((item) => item.y)) - 40,
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
    expect(paired.x + paired.width).toBeGreaterThanOrEqual(
      developer.x + runGraphNodeWidth,
    );
  });

  it("возвращает результат локально рядом с делегированием без обхода всего графа", () => {
    const nodes = [
      node("parent", "01"),
      node("child", "02"),
      node("unrelated", "03"),
    ];
    const layout = layoutRunGraph(nodes, [
      edge("delegate", "parent", "child"),
      edge("return", "child", "parent", "CALLBACK_TO"),
    ]);
    const parent = required(
      layout.nodes.find((item) => item.node.ref === "parent"),
    );
    const child = required(
      layout.nodes.find((item) => item.node.ref === "child"),
    );
    const returning = required(
      layout.edges.find((item) => item.edge.ref === "return"),
    );
    const points = sampleCurve(required(returning.path));
    expect(
      points.every(
        (p) => p.x >= parent.x + runGraphNodeWidth - 1 && p.x <= child.x + 1,
      ),
    ).toBe(true);
    expect(Math.min(...points.map((p) => p.y))).toBeGreaterThanOrEqual(
      Math.min(parent.y, child.y),
    );
    expect(returning.path).not.toBe(layout.edges[0]?.path);
    expect(returning.edge.sourceNodeRef).toBe("child");
    expect(returning.edge.targetNodeRef).toBe("parent");
  });

  it("сохраняет локальные параллельные и вложенные возвраты при перестановке снимка", () => {
    const nodes = [
      node("parent", "01"),
      node("child_a", "02"),
      node("child_b", "03"),
      node("nested", "04"),
    ];
    const edges = [
      edge("a", "parent", "child_a"),
      edge("b", "parent", "child_b"),
      edge("nested", "child_a", "nested"),
      edge("return_a", "child_a", "parent", "CALLBACK_TO"),
      edge("return_b", "child_b", "parent", "CALLBACK_TO"),
      edge("return_nested", "nested", "child_a", "CALLBACK_TO"),
    ];
    const layout = layoutRunGraph(nodes, edges);
    const reversed = layoutRunGraph([...nodes].reverse(), [...edges].reverse());
    for (const item of layout.edges) {
      expect(item.path).toBe(
        reversed.edges.find((other) => other.edge.ref === item.edge.ref)?.path,
      );
      for (const point of sampleCurve(required(item.path))) {
        expect(
          layout.nodes.some(
            (card) =>
              point.x > card.x + 1 &&
              point.x < card.x + runGraphNodeWidth - 1 &&
              point.y > card.y + 1 &&
              point.y < card.y + runGraphNodeHeight - 1,
          ),
        ).toBe(false);
      }
    }
    expect(layout.bounds.height).toBeLessThan(500);
  });

  it("показывает завершённого исполнителя до подтверждённого продолжения родителя", () => {
    const parent = { ...node("parent", "01"), agentRef: "agent_parent" };
    const child = {
      ...node("child", "02"),
      finishedAt: "2026-01-01T01:00:00Z",
    };
    const next = {
      ...node("next", "03"),
      agentRef: parent.agentRef,
      parentNodeRef: parent.ref,
      startedAt: "2026-01-01T01:00:01Z",
    };
    const edges = [
      edge("delegate", parent.ref, child.ref),
      edge("return", child.ref, parent.ref, "CALLBACK_TO"),
      edge("next", parent.ref, next.ref, "CONTINUES"),
    ];
    const layout = layoutRunGraph([parent, child, next], edges);
    expect(
      required(layout.nodes.find((item) => item.node.ref === "next")).x,
    ).toBeGreaterThan(
      required(layout.nodes.find((item) => item.node.ref === "child")).x,
    );
    expect(layout.edges.map((item) => item.edge)).toEqual(edges);
    const overlapping = layoutRunGraph(
      [parent, { ...child, finishedAt: "2026-01-01T02:00:00Z" }, next],
      edges,
    );
    expect(
      required(overlapping.nodes.find((item) => item.node.ref === "next")).x,
    ).toBe(
      required(overlapping.nodes.find((item) => item.node.ref === "child")).x,
    );
  });

  it("маршрутизирует дальний возврат без пересечения промежуточных карточек", () => {
    const nodes = Array.from({ length: 6 }, (_, i) =>
      node(`node_${String(i)}`, String(i)),
    );
    const edges = nodes
      .slice(1)
      .map((n, i) =>
        edge(`delegate_${String(i)}`, required(nodes[i]).ref, n.ref),
      );
    edges.push(edge("return", "node_5", "node_0", "CALLBACK_TO"));
    const layout = layoutRunGraph(nodes, edges);
    const points = sampleCurve(required(layout.edges.at(-1)?.path));
    for (const point of points) {
      expect(
        layout.nodes.some(
          (card) =>
            point.x > card.x + 1 &&
            point.x < card.x + runGraphNodeWidth - 1 &&
            point.y > card.y + 1 &&
            point.y < card.y + runGraphNodeHeight - 1,
        ),
      ).toBe(false);
    }
    expect(layout.bounds.height).toBeLessThan(300);
  });

  it("сохраняет конечные координаты и исходные связи при цикле и неполной истории", () => {
    const nodes = [
      node("first", "01"),
      node("second", "02"),
      node("third", "03"),
    ];
    const edges = [
      edge("a", "first", "second"),
      edge("b", "second", "third"),
      edge("cycle", "third", "first", "CONTINUES"),
      edge("return", "third", "first", "CALLBACK_TO"),
    ];
    const result = layoutRunGraph(nodes, edges);
    expect(result.edges.map((item) => item.edge)).toEqual(edges);
    expect(
      result.nodes.every(
        (item) => Number.isFinite(item.x) && Number.isFinite(item.y),
      ),
    ).toBe(true);
    expect(
      result.edges.every((item) => !/NaN|Infinity/.test(item.path ?? "")),
    ).toBe(true);
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
