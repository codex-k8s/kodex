import { renderToString } from "@vue/server-renderer";
import {
  createSSRApp,
  createRenderer,
  defineComponent,
  h,
  nextTick,
  ssrContextKey,
  type App,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const flow = vi.hoisted(() => ({
  dimensions: undefined as Ref<{ width: number; height: number }> | undefined,
  viewport: { x: 0, y: 0, zoom: 1 },
  setViewport: vi.fn(),
}));

vi.mock("@vue-flow/core", async (importOriginal) => {
  const { getTransformForBounds } =
    await importOriginal<typeof import("@vue-flow/core")>();
  const { defineComponent, h, ref } = await import("vue");
  flow.dimensions = ref({ width: 1921, height: 780 });
  flow.setViewport.mockImplementation((viewport: typeof flow.viewport) => {
    flow.viewport = viewport;
    return Promise.resolve();
  });
  return {
    getTransformForBounds,
    BaseEdge: defineComponent({
      setup() {
        return () => h("path");
      },
    }),
    MarkerType: { ArrowClosed: "arrowclosed" },
    Position: { Left: "left", Right: "right" },
    VueFlow: defineComponent({
      setup(_props, { slots }) {
        return () => h("div", { class: "vue-flow" }, slots.default?.());
      },
    }),
    getBezierPath: () => ["M0 0"],
    useVueFlow: () => ({
      dimensions: flow.dimensions,
      fitView: vi.fn().mockResolvedValue(undefined),
      getViewport: () => ({ ...flow.viewport }),
      onInit: vi.fn(),
      setViewport: flow.setViewport,
      zoomIn: vi.fn().mockResolvedValue(undefined),
      zoomOut: vi.fn().mockResolvedValue(undefined),
    }),
  };
});
vi.mock("@vue-flow/background", async () => {
  const { defineComponent, h } = await import("vue");
  return {
    Background: defineComponent({
      setup() {
        return () => h("div", { class: "vue-flow__background" });
      },
    }),
  };
});
vi.mock("@vue-flow/minimap", async () => {
  const { defineComponent, h } = await import("vue");
  return {
    MiniMap: defineComponent({
      inheritAttrs: false,
      setup(_props, { attrs }) {
        return () => h("div", { ...attrs, class: "vue-flow__minimap" });
      },
    }),
  };
});

import RunGraphCanvas from "@/features/runs/RunGraphCanvas.vue";
import {
  createRunGraphFlowElements,
  runGraphFitViewOptions,
  runGraphInitialFitOptions,
  runGraphRetryAttempts,
} from "@/features/runs/run-graph-flow";
import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import {
  layoutRunGraph,
  runGraphNodeWidth,
  runGraphNodeHeight,
} from "./run-graph-layout";

const nodes: RunNode[] = [
  {
    ref: "node_root",
    runRef: "run_example",
    type: "ROOT_PROCESS",
    state: "RUNNING",
    displayName: "Подготовка отчёта",
    attempt: 1,
    artifactRefs: [],
    childRunRefs: [],
    createdAt: "2026-08-28T08:00:00Z",
    nextActions: [],
  },
  {
    ref: "node_agent",
    runRef: "run_example",
    parentNodeRef: "node_root",
    type: "AGENT_EXECUTION",
    state: "QUEUED",
    displayName: "Аналитик продаж с подробным понятным названием",
    role: "Аналитик",
    attempt: 1,
    artifactRefs: [],
    childRunRefs: [],
    createdAt: "2026-08-28T08:00:01Z",
    nextActions: [],
  },
];

const edges: RunEdge[] = [
  {
    ref: "edge_delegation",
    runRef: "run_example",
    sourceNodeRef: "node_root",
    targetNodeRef: "node_agent",
    type: "DELEGATED_TO",
    label: "",
  },
];

const apps: App[] = [];
afterEach(() => apps.splice(0).forEach((app) => app.unmount()));
beforeEach(() => {
  vi.clearAllMocks();
  if (flow.dimensions) flow.dimensions.value = { width: 1921, height: 780 };
  flow.viewport = { x: 0, y: 0, zoom: 1 };
});

interface CanvasState {
  userAdjustedView: Ref<boolean>;
  markUserAdjusted(): void;
}
function mountCanvas(): CanvasState {
  let state: CanvasState | undefined;
  const renderer = createRenderer<object, object>({
    patchProp() {},
    insert() {},
    remove() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
  });
  const original = (
    RunGraphCanvas as unknown as {
      setup: (props: object, context: SetupContext) => CanvasState;
    }
  ).setup;
  const app = renderer.createApp(
    defineComponent({
      setup(_, context) {
        state = original(
          {
            nodes,
            edges,
            selectedRef: "node_root",
            futureNodeRefs: [],
            activeNodeRefs: [],
            executionLabels: {},
            compact: true,
          },
          context,
        );
        return () => null;
      },
    }),
  );
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      missingWarn: false,
      fallbackWarn: false,
    }),
  );
  app.provide(ssrContextKey, { modules: new Set<string>() });
  app.mount({});
  apps.push(app);
  if (!state) throw new Error("Missing canvas fixture");
  return state;
}

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Missing graph test fixture");
  return value;
}

async function render(
  executionLabels: Record<string, "ASSISTANT" | "EMPLOYEE" | "SESSION"> = {},
): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(RunGraphCanvas, {
        nodes,
        edges,
        selectedRef: "node_agent",
        futureNodeRefs: ["node_agent"],
        activeNodeRefs: ["node_root"],
        executionLabels,
      }),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          common: { unknownStatus: "Статус недоступен", details: "Подробнее" },
          runs: {
            graph: "Граф выполнения",
            graphControls: "Управление графом",
            connections: "Связи графа",
            zoom: "Масштаб графа",
            zoomIn: "Увеличить масштаб",
            zoomOut: "Уменьшить масштаб",
            fitGraph: "Вместить",
            minimap: "Мини-карта графа",
            waitingForActivity: "Ожидает начала работы",
            sessionNode: "Сессия",
            assistantNode: "Помощник Kodex",
            controlNode: "Контрольный этап",
            graphRunAttempt: "Запуск №{attempt}",
            graphNodes: "Узлы: {count}",
            graphEdges: "Связи: {count}",
            callback: "Ответ дочернего запуска",
            retry: "Повторить попытку",
            continueTask: "Дополнительное задание",
            source: { AGENT_DELEGATION: "Делегирование ИИ-сотрудника" },
            nodeTypes: {
              ROOT_PROCESS: "Основной процесс",
              AGENT_EXECUTION: "ИИ-сотрудник",
              HUMAN_GATE: "Решение человека",
              EXTERNAL_ACTION: "Внешнее действие",
            },
          },
          states: {
            RUNNING: "Выполняется",
            QUEUED: "В очереди",
            WAITING: "Ожидает",
            SUCCEEDED: "Завершено",
            FAILED: "Ошибка",
          },
        },
      },
    }),
  );
  return renderToString(app);
}

describe("RunGraphCanvas", () => {
  it("поздний resize после открытия drawer пересчитывает untouched viewport по ширине1201", async () => {
    const state = mountCanvas();
    const dimensions = required(flow.dimensions);
    flow.viewport = { x: 700, y: 0, zoom: 1 };
    dimensions.value = { width: 1201, height: 780 };
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    expect(flow.viewport.x).toBeGreaterThan(0);
    expect(flow.viewport.zoom).toBeGreaterThan(0);
    for (const node of layoutRunGraph(nodes, edges).nodes) {
      expect(
        node.x * flow.viewport.zoom + flow.viewport.x,
      ).toBeGreaterThanOrEqual(0);
      expect(
        (node.x + runGraphNodeWidth) * flow.viewport.zoom + flow.viewport.x,
      ).toBeLessThanOrEqual(1201);
      expect(
        node.y * flow.viewport.zoom + flow.viewport.y,
      ).toBeGreaterThanOrEqual(0);
      expect(
        (node.y + runGraphNodeHeight) * flow.viewport.zoom + flow.viewport.y,
      ).toBeLessThanOrEqual(780);
    }
    expect(state.userAdjustedView.value).toBe(false);
    flow.setViewport.mockClear();
    dimensions.value = { width: 1201, height: 780 };
    await nextTick();
    expect(flow.setViewport).not.toHaveBeenCalled();
  });

  it("resize сохраняет ручной world-center и zoom; события без resize их не сбрасывают", async () => {
    const state = mountCanvas();
    const dimensions = required(flow.dimensions);
    flow.viewport = { x: 320, y: 90, zoom: 0.72 };
    state.markUserAdjusted();
    const center = { x: (1921 / 2 - 320) / 0.72, y: (780 / 2 - 90) / 0.72 };
    dimensions.value = { width: 1201, height: 680 };
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    expect(flow.viewport.zoom).toBe(0.72);
    expect((1201 / 2 - flow.viewport.x) / flow.viewport.zoom).toBeCloseTo(
      center.x,
    );
    expect((680 / 2 - flow.viewport.y) / flow.viewport.zoom).toBeCloseTo(
      center.y,
    );
    expect(state.userAdjustedView.value).toBe(true);
    flow.setViewport.mockClear();
    dimensions.value = { width: 1201, height: 680 };
    await nextTick();
    expect(flow.setViewport).not.toHaveBeenCalled();
    dimensions.value = { width: 1921, height: 780 };
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    expect(flow.viewport).toEqual({ x: 320, y: 90, zoom: 0.72 });
  });

  it("пустые dimensions при скрытом canvas не применяют синтетический viewport", async () => {
    mountCanvas();
    required(flow.dimensions).value = { width: 0, height: 0 };
    await nextTick();
    expect(flow.setViewport).not.toHaveBeenCalled();
  });

  it("резервирует место для detached-панелей при начальном fit", () => {
    expect(runGraphFitViewOptions(1440).padding).toEqual({
      top: "180px",
      right: "210px",
      bottom: "160px",
      left: "380px",
    });
    expect(runGraphFitViewOptions(412).padding).toBe(0.14);
    expect(runGraphFitViewOptions(1920, true).padding).toEqual({
      top: "180px",
      right: "32px",
      bottom: "160px",
      left: "32px",
    });
  });

  it("для большого графа открывает читаемую окрестность, сохраняя полный обзор по кнопке", () => {
    const manyNodes = Array.from({ length: 30 }, (_, index) => ({
      ...required(nodes[index ? 1 : 0]),
      ref: `node_${String(index)}`,
      createdAt: `2026-08-28T08:00:${String(index).padStart(2, "0")}Z`,
    }));
    const manyEdges = manyNodes.slice(1).map((node, index) => ({
      ...required(edges[0]),
      ref: `edge_${String(index)}`,
      sourceNodeRef: required(manyNodes[Math.floor(index / 3)]).ref,
      targetNodeRef: node.ref,
    }));
    const initialFit = runGraphInitialFitOptions(1920, manyNodes, manyEdges);
    expect(initialFit.nodes?.[0]).toBe("node_0");
    expect(initialFit.nodes?.length).toBeLessThanOrEqual(4);
    expect(initialFit.nodes).toContain("node_2");
    expect(initialFit.minZoom).toBe(0.85);
    expect(runGraphFitViewOptions(1920).nodes).toBeUndefined();
    expect(runGraphInitialFitOptions(1920, nodes, edges).nodes).toBeUndefined();
  });

  it("различает номера попыток только внутри связанной цепочки повторов", () => {
    const roots = [
      { ...required(nodes[0]), ref: "old" },
      { ...required(nodes[0]), ref: "retry" },
      { ...required(nodes[0]), ref: "other" },
    ];
    const attempts = runGraphRetryAttempts(roots, [
      {
        ...required(edges[0]),
        sourceNodeRef: "old",
        targetNodeRef: "retry",
        type: "RETRY_OF",
      },
    ]);
    expect(attempts.get("old")).toBe(1);
    expect(attempts.get("retry")).toBe(2);
    expect(attempts.has("other")).toBe(false);
  });

  it("держит контролы вне полотна и предоставляет доступное дерево", async () => {
    const html = await render();

    expect(html.indexOf("graph-toolbar")).toBeLessThan(
      html.indexOf("graph-viewport"),
    );
    expect(html).toContain('role="toolbar"');
    expect(html).toContain('role="tree"');
    expect(html).toContain('aria-level="1"');
    expect(html).toContain('aria-level="2"');
    expect(html).toContain('aria-selected="true"');
    expect(html).toContain('class="graph-toolbar"');
    expect(html).toContain('aria-label="Уменьшить масштаб"');
    expect(html).toContain('aria-label="Увеличить масштаб"');
    expect(html).toContain('aria-label="Вместить"');
    expect(html).not.toContain("vue-flow__controls");
    expect(html).toContain("vue-flow__minimap");
    expect(html).toContain("Мини-карта графа");
  });

  it("убирает подписи с рёбер и объясняет связи в легенде", async () => {
    const html = await render();

    expect(html).toContain("Подготовка отчёта");
    expect(html).toContain("Делегирование ИИ-сотрудника");
    expect(html).toContain("Аналитик продаж с подробным понятным названием");
    expect(html).toContain("graph-legend");
    expect(html).toContain("Узлы: 2");
    expect(html).toContain("Связи: 1");
    expect(html).not.toContain("Ответ дочернего запуска");
    expect(html).not.toContain("Ошибка");
    expect(html).not.toContain("graph-edge-label");
    expect(html).not.toContain(">DELEGATED_TO<");
  });

  it("сохраняет легенду, но по умолчанию сворачивает её мобильную панель", async () => {
    const html = await render();
    expect(html).toMatch(
      /class="graph-legend__toggle[^"]*"[^>]*aria-expanded="false"[^>]*aria-controls="run-graph-legend-details"/,
    );
    expect(html).toContain('id="run-graph-legend-details"');
    expect(html).not.toContain("graph-legend__details--expanded");
    expect(html).toContain("Делегирование ИИ-сотрудника");
    expect(html).toContain("Подготовка отчёта");
  });
  it("легенда различает доказанного помощника, сотрудника и неизвестную сессию", async () => {
    const assistant = await render({ node_agent: "ASSISTANT" });
    expect(assistant).toContain("Помощник Kodex");
    expect(assistant).not.toContain("ИИ-сотрудник</span>");
    const unknown = await render({ node_agent: "SESSION" });
    expect(unknown).toContain("Сессия");
    const employee = await render({ node_agent: "EMPLOYEE" });
    expect(employee).toContain("ИИ-сотрудник");
    expect(employee).not.toContain("Помощник Kodex");
  });

  it("отмечает будущие и активные узлы без подмены состояния", () => {
    const flow = createRunGraphFlowElements(nodes, edges, {
      selectedRef: "node_agent",
      futureRefs: new Set(["node_agent"]),
      activeRefs: new Set(["node_root", "node_agent"]),
      nodeAccessibleLabel: (node) => `${node.displayName} · ${node.state}`,
      edgeAccessibleLabel: () => "Делегирование",
    });
    const root = flow.nodes.find((node) => node.id === "node_root");
    const agent = flow.nodes.find((node) => node.id === "node_agent");
    const edge = flow.edges[0];

    expect(root?.data).toMatchObject({ active: true, future: false });
    expect(root?.class).toContain("run-flow-node--active");
    expect(root?.domAttributes).toMatchObject({ "aria-busy": "true" });
    expect(agent?.data).toMatchObject({
      active: false,
      selected: true,
      future: true,
      surface: "session",
    });
    expect(agent?.class).toEqual(
      expect.arrayContaining([
        "run-flow-node--future",
        "run-flow-node--selected",
      ]),
    );
    expect(agent?.domAttributes).toMatchObject({
      "data-node-future": "true",
      "data-node-surface": "session",
      "data-node-state": "QUEUED",
    });
    expect(agent?.domAttributes?.["aria-busy"]).toBeUndefined();
    expect(edge).toMatchObject({
      type: "runEdge",
      source: "node_root",
      target: "node_agent",
      data: {
        accessibleLabel: "Делегирование",
        color: "var(--accent)",
      },
    });
    expect(edge).not.toHaveProperty("label");
  });

  it("сохраняет terminal cancel как публичное состояние canvas-ноды", () => {
    const root = nodes[0];
    const agent = nodes[1];
    if (!root || !agent) {
      throw new Error("Run graph fixture must contain root and agent nodes");
    }
    const cancelled = { ...agent, state: "CANCELLED" as const };
    const flow = createRunGraphFlowElements([root, cancelled], edges, {
      futureRefs: new Set(),
      activeRefs: new Set(),
      nodeAccessibleLabel: (node) => `${node.displayName} · ${node.state}`,
      edgeAccessibleLabel: () => "Делегирование",
    });
    const cancelledNode = flow.nodes.find((node) => node.id === cancelled.ref);

    expect(cancelledNode?.class).toContain("run-flow-node--cancelled");
    expect(cancelledNode?.domAttributes).toMatchObject({
      "data-node-state": "CANCELLED",
    });
    expect(cancelledNode?.ariaLabel).toContain("CANCELLED");
  });
});
