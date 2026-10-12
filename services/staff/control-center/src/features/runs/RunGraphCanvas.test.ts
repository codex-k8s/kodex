import { renderToString } from "@vue/server-renderer";
import { readFileSync } from "node:fs";
import {
  createSSRApp,
  createRenderer,
  defineComponent,
  h,
  nextTick,
  reactive,
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
  minimumZoom: Ref<number>;
  markUserAdjusted(): void;
  fit(userInitiated?: boolean, currentWork?: boolean): Promise<void>;
}
function canvasProps() {
  return reactive({
    nodes: [...nodes],
    edges: [...edges],
    rootRef: "run_example",
    selectedRef: "node_root",
    futureNodeRefs: [] as string[],
    activeNodeRefs: [] as string[],
    executionLabels: {},
    compact: true,
  });
}
function mountCanvas(props = canvasProps()): CanvasState {
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
        state = original(props, context);
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

function expectGraphVisible(props: ReturnType<typeof canvasProps>): void {
  const bounds = layoutRunGraph(props.nodes, props.edges).bounds;
  const { width, height } = required(flow.dimensions).value;
  expect(
    bounds.x * flow.viewport.zoom + flow.viewport.x,
  ).toBeGreaterThanOrEqual(0);
  expect(
    (bounds.x + bounds.width) * flow.viewport.zoom + flow.viewport.x,
  ).toBeLessThanOrEqual(width);
  expect(
    bounds.y * flow.viewport.zoom + flow.viewport.y,
  ).toBeGreaterThanOrEqual(0);
  expect(
    (bounds.y + bounds.height) * flow.viewport.zoom + flow.viewport.y,
  ).toBeLessThanOrEqual(height);
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
            nodeList: "Список узлов",
            zoom: "Масштаб графа",
            zoomIn: "Увеличить масштаб",
            zoomOut: "Уменьшить масштаб",
            fitGraph: "Весь граф",
            focusCurrentWork: "Текущая работа",
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
  it.each([0, -1, Number.NaN, Number.POSITIVE_INFINITY])(
    "не применяет fit до валидного размера canvas %s",
    async (height) => {
      const state = mountCanvas();
      required(flow.dimensions).value = { width: 1201, height };
      await state.fit(false);
      expect(flow.setViewport).not.toHaveBeenCalled();
    },
  );

  it("ручное изменение во время ожидающего fit отменяет этот автофокус", async () => {
    const state = mountCanvas();
    const pending = state.fit(false);
    state.userAdjustedView.value = true;
    await pending;
    expect(flow.setViewport).not.toHaveBeenCalled();
  });

  it("конкурирующие realtime fit и короткий resize применяют только свежий положительный viewport без анимации", async () => {
    const state = mountCanvas();
    const dimensions = required(flow.dimensions);
    const first = state.fit(false);
    dimensions.value = { width: 1201, height: 160 };
    const second = state.fit(false);
    await Promise.all([first, second]);
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    expect(flow.viewport.zoom).toBeGreaterThan(0);
    expect(Object.values(flow.viewport).every(Number.isFinite)).toBe(true);
    expect(flow.setViewport).toHaveBeenLastCalledWith(flow.viewport, {
      duration: 0,
    });
    flow.setViewport.mockClear();
    await state.fit(true);
    expect(flow.setViewport).toHaveBeenLastCalledWith(flow.viewport, {
      duration: 180,
    });
  });
  it("отложенный автофокус не перезаписывает ручной viewport", async () => {
    const state = mountCanvas();
    flow.viewport = { x: -420, y: 90, zoom: 0.72 };
    state.markUserAdjusted();
    await state.fit(false);
    expect(flow.setViewport).not.toHaveBeenCalled();
    expect(flow.viewport).toEqual({ x: -420, y: 90, zoom: 0.72 });
  });

  it("смена активного исполнения фокусирует untouched viewport, не меняя выбранный root", async () => {
    const props = canvasProps();
    const state = mountCanvas(props);
    props.nodes = [
      required(props.nodes[0]),
      { ...required(props.nodes[1]), state: "RUNNING" },
    ];
    props.activeNodeRefs = ["node_root", "node_agent"];
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    expect(props.selectedRef).toBe("node_root");
    expect(state.userAdjustedView.value).toBe(false);
    const focused = flow.viewport;
    await state.fit(true, true);
    expect(flow.viewport).toEqual(focused);
    expect(state.userAdjustedView.value).toBe(true);
  });

  it("ручной viewport переживает новый child run, активное состояние и переключение selection", async () => {
    const props = canvasProps();
    const state = mountCanvas(props);
    flow.viewport = { x: -420, y: 90, zoom: 0.72 };
    state.markUserAdjusted();
    props.nodes = [
      required(props.nodes[0]),
      { ...required(props.nodes[1]), runRef: "run_child", state: "RUNNING" },
    ];
    props.activeNodeRefs = ["node_root", "node_agent"];
    props.edges = [
      ...props.edges,
      { ...required(edges[0]), ref: "new_edge", type: "CONTINUES" },
    ];
    props.selectedRef = "node_agent";
    await nextTick();
    await nextTick();
    expect(flow.setViewport).not.toHaveBeenCalled();
    expect(flow.viewport).toEqual({ x: -420, y: 90, zoom: 0.72 });
    expect(state.userAdjustedView.value).toBe(true);
  });

  it("только новый корневой граф снимает защиту ручного viewport", async () => {
    const props = canvasProps();
    const state = mountCanvas(props);
    state.markUserAdjusted();
    props.rootRef = "another_root_run";
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    expect(state.userAdjustedView.value).toBe(false);
  });

  it("явный обзор всего графа не включает последующее автослежение", async () => {
    const props = canvasProps();
    const state = mountCanvas(props);
    await state.fit(true);
    expect(state.userAdjustedView.value).toBe(true);
    flow.setViewport.mockClear();
    props.nodes = [
      required(props.nodes[0]),
      { ...required(props.nodes[1]), state: "RUNNING" },
    ];
    props.activeNodeRefs = ["node_agent"];
    await nextTick();
    await nextTick();
    expect(flow.setViewport).not.toHaveBeenCalled();
  });

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

  it("открытие и закрытие drawer возвращают весь граф после ручного pan/zoom без realtime-автослежения", async () => {
    const props = canvasProps();
    const state = mountCanvas(props);
    const dimensions = required(flow.dimensions);
    flow.viewport = { x: 320, y: 90, zoom: 0.72 };
    state.markUserAdjusted();
    // Старое сохранение центра сдвигало корень за левый край нового canvas.
    const root = required(layoutRunGraph(props.nodes, props.edges).nodes[0]);
    expect(root.x * 0.72 + 320 + (1201 - 1921) / 2).toBeLessThan(0);
    dimensions.value = { width: 1201, height: 680 };
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    expectGraphVisible(props);
    expect(flow.setViewport).toHaveBeenLastCalledWith(flow.viewport, {
      duration: 0,
    });
    expect(state.userAdjustedView.value).toBe(true);
    flow.setViewport.mockClear();
    dimensions.value = { width: 1201, height: 680 };
    await nextTick();
    expect(flow.setViewport).not.toHaveBeenCalled();
    props.compact = false;
    dimensions.value = { width: 1921, height: 780 };
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    expectGraphVisible(props);
    expect(state.userAdjustedView.value).toBe(true);
    const resizedViewport = { ...flow.viewport };
    flow.setViewport.mockClear();
    props.nodes = [
      required(props.nodes[0]),
      { ...required(props.nodes[1]), state: "RUNNING" },
    ];
    props.activeNodeRefs = ["node_agent"];
    props.edges = [...props.edges, { ...required(edges[0]), ref: "new_edge" }];
    await nextTick();
    await nextTick();
    expect(flow.setViewport).not.toHaveBeenCalled();
    expect(flow.viewport).toEqual(resizedViewport);
  });

  it("resize не превращает начальную загрузку из нулевых размеров в обзор большого графа", async () => {
    const props = canvasProps();
    props.nodes = [
      required(props.nodes[0]),
      { ...required(props.nodes[1]), state: "RUNNING" },
    ];
    props.activeNodeRefs = ["node_agent"];
    required(flow.dimensions).value = { width: 0, height: 0 };
    const state = mountCanvas(props);
    required(flow.dimensions).value = { width: 1201, height: 160 };
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    const initialViewport = { ...flow.viewport };
    await state.fit(true, true);
    expect(flow.viewport).toEqual(initialViewport);
  });

  it("игнорирует subpixel jitter и повторный ResizeObserver callback без смены размеров", async () => {
    const state = mountCanvas();
    state.markUserAdjusted();
    required(flow.dimensions).value = { width: 1921.5, height: 780.5 };
    await nextTick();
    expect(flow.setViewport).not.toHaveBeenCalled();
    required(flow.dimensions).value = { width: 1201, height: 680 };
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    flow.setViewport.mockClear();
    required(flow.dimensions).value = { width: 1201, height: 680 };
    await nextTick();
    await nextTick();
    expect(flow.setViewport).not.toHaveBeenCalled();
  });

  it("resize помещает большой граф вместе с обратной связью ниже обычного minZoom", async () => {
    const props = canvasProps();
    props.nodes = Array.from({ length: 80 }, (_, index) => ({
      ...required(nodes[index === 0 ? 0 : 1]),
      ref: `node_${String(index)}`,
    }));
    props.edges = props.nodes.slice(1).map((node, index) => ({
      ...required(edges[0]),
      ref: `edge_${String(index)}`,
      sourceNodeRef: required(props.nodes[index]).ref,
      targetNodeRef: node.ref,
    }));
    props.edges.push({
      ...required(edges[0]),
      ref: "edge_callback",
      sourceNodeRef: required(props.nodes.at(-1)).ref,
      targetNodeRef: required(props.nodes[0]).ref,
      type: "CALLBACK_TO",
    });
    const state = mountCanvas(props);
    state.markUserAdjusted();
    required(flow.dimensions).value = { width: 942, height: 680 };
    await vi.waitFor(() => expect(flow.setViewport).toHaveBeenCalledOnce());
    expectGraphVisible(props);
    expect(flow.viewport.zoom).toBeGreaterThan(0);
    expect(flow.viewport.zoom).toBeLessThan(0.15);
    expect(state.minimumZoom.value).toBe(flow.viewport.zoom);
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

  it("резервирует отдельную мобильную полосу, не обрезая счётчики и кнопки", () => {
    const source = readFileSync(
      new URL("./RunGraphCanvas.vue", import.meta.url),
      "utf8",
    );
    const mobile = source.slice(source.indexOf("@media (max-width: 760px)"));
    expect(mobile).toMatch(
      /\.graph-toolbar\s*\{[^}]*top: var\(--run-graph-toolbar-top, 8px\)/,
    );
    expect(mobile).toMatch(
      /\.graph-toolbar\s*\{[^}]*left: 8px;[^}]*right: 8px/,
    );
    expect(mobile).toMatch(
      /\.graph-toolbar__count\s*\{[^}]*min-width: 0;[^}]*white-space: normal/,
    );
    expect(mobile).toMatch(
      /\.graph-toolbar__count\s*\{[^}]*overflow-wrap: anywhere/,
    );
    expect(mobile).toMatch(/\.graph-view-switch\s*\{[^}]*flex: 0 0 auto/);
  });

  it("держит контролы вне полотна и предоставляет доступное дерево", async () => {
    const html = await render();

    expect(html.indexOf("graph-toolbar")).toBeLessThan(
      html.indexOf("graph-viewport"),
    );
    expect(html).toContain('role="toolbar"');
    expect(html).toContain('aria-label="Список узлов"');
    expect(html).toContain('title="Список узлов"');
    expect(html).toContain('role="tree"');
    expect(html).toContain('aria-level="1"');
    expect(html).toContain('aria-level="2"');
    expect(html).toContain('aria-selected="true"');
    expect(html).toContain('class="graph-toolbar"');
    expect(html).toContain('aria-label="Уменьшить масштаб"');
    expect(html).toContain('aria-label="Увеличить масштаб"');
    expect(html).toContain('aria-label="Весь граф"');
    expect(html).toContain('title="Весь граф"');
    expect(html).toContain('aria-label="Текущая работа"');
    expect(html).toContain('title="Текущая работа"');
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
    expect(unknown).toContain("ИИ-сотрудник");
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
