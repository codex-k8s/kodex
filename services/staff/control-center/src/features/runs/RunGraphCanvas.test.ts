import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import RunGraphCanvas from "@/features/runs/RunGraphCanvas.vue";
import {
  createRunGraphFlowElements,
  runGraphFitViewOptions,
} from "@/features/runs/run-graph-flow";
import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

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

async function render(): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(RunGraphCanvas, {
        nodes,
        edges,
        selectedRef: "node_agent",
        futureNodeRefs: ["node_agent"],
        activeNodeRefs: ["node_root"],
      }),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          common: { unknownStatus: "Статус недоступен" },
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
            controlNode: "Контрольный этап",
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
  it("резервирует место для detached-панелей при начальном fit", () => {
    expect(runGraphFitViewOptions(1440).padding).toEqual({
      top: "180px",
      right: "210px",
      bottom: "160px",
      left: "380px",
    });
    expect(runGraphFitViewOptions(412).padding).toBe(0.14);
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
    expect(html).not.toContain("graph-edge-label");
    expect(html).not.toContain(">DELEGATED_TO<");
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
