import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import RunGraphCanvas from "@/features/runs/RunGraphCanvas.vue";
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
            waitingForActivity: "Ожидает начала работы",
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
          },
        },
      },
    }),
  );
  return renderToString(app);
}

describe("RunGraphCanvas", () => {
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
  });

  it("показывает направление и локализует пустую подпись ребра", async () => {
    const html = await render();

    expect(html).toContain("Подготовка отчёта");
    expect(html).toContain("Делегирование ИИ-сотрудника");
    expect(html).toContain("Аналитик продаж с подробным понятным названием");
    expect(html).not.toContain(">DELEGATED_TO<");
  });

  it("отмечает будущие и активные узлы без подмены состояния", async () => {
    const html = await render();

    expect(html).toContain("canvas-node--future");
    expect(html).toContain("canvas-node--active");
    expect(html).toContain('data-node-future="true"');
    expect(html).toContain("В очереди");
  });
});
