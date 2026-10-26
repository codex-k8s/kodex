import { readFileSync } from "node:fs";
import { renderToString } from "@vue/server-renderer";
import { createSSRApp, defineComponent, h } from "vue";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

import { i18n } from "@/app/i18n";
import type { RunNode } from "@/shared/api/generated/openapi/types.gen";
import RunNodeInspector from "./RunNodeInspector.vue";

const node: RunNode = {
  ref: "nod_synthetic_inspector",
  runRef: "run_synthetic_history",
  type: "ROOT_PROCESS",
  state: "FAILED",
  displayName: "Предыдущая попытка",
  attempt: 1,
  artifactRefs: [],
  childRunRefs: [],
  nextActions: [],
  createdAt: "2026-10-10T18:00:00Z",
  finishedAt: "2026-10-10T18:12:58Z",
};

async function renderNode(value: RunNode): Promise<string> {
  const app = createSSRApp(RunNodeInspector, {
    node: value,
    nodes: [value],
    artifacts: [],
  });
  app.use(i18n);
  app.component(
    "RouterLink",
    defineComponent({
      setup:
        (_props, { slots }) =>
        () =>
          h("a", slots.default?.()),
    }),
  );
  return renderToString(app);
}

describe("контекст исторического узла без сводки", () => {
  it("показывает отказ обязательного результата независимо от успешного завершения модели", async () => {
    const html = await renderNode({
      ...node,
      state: "SUCCEEDED",
      workflowResults: [
        {
          attempt: 1,
          runRef: node.runRef,
          accepted: false,
          reason: "MISSING_RESULT",
          results: [],
        },
      ],
    });
    expect(html).toContain("Обязательные результаты не приняты");
    expect(html).toContain("MISSING_RESULT");
    expect(html).not.toContain("Обязательные результаты приняты");
  });
  it("сохраняет exact evidence и не подменяет неподтверждённую проверку состояния успехом", async () => {
    const digest = "a".repeat(64);
    const html = await renderNode({
      ...node,
      workflowResults: [
        {
          attempt: 2,
          runRef: node.runRef,
          accepted: true,
          reason: "",
          results: [
            {
              key: "deploy",
              kind: "DEPLOYMENT",
              digest,
              headSha: "b".repeat(40),
              repository: "codex-k8s/test-repo",
              healthOk: false,
            },
          ],
        },
      ],
    });
    expect(html).toContain(digest);
    expect(html).toContain("b".repeat(40));
    expect(html).toContain("Проверка состояния не подтверждена");
    expect(html).not.toContain("Проверка состояния пройдена");
  });
  it("на mobile отдаёт названию полную строку отдельно от статуса и кнопок", () => {
    const source = readFileSync(
      new URL("./RunNodeInspector.vue", import.meta.url),
      "utf8",
    );
    const mobile = source.slice(source.indexOf("@media (max-width: 760px)"));
    expect(mobile).toContain('"identity identity identity"');
    expect(mobile).toContain("grid-area: identity");
    expect(source).toContain('class="run-node-inspector__identity"');
  });
  it.each([
    ["FAILED", "Ошибка"],
    ["CANCELLED", "Отменён"],
    ["SUCCEEDED", "Завершён"],
    ["SKIPPED", "Пропущен"],
  ] as const)("показывает %s, а не ожидание начала", async (state, text) => {
    const html = await renderNode({ ...node, state });
    const summary = html.match(/run-node-inspector__status[^]*?<\/div>/)?.[0];
    expect(summary).toContain(text);
    expect(html).not.toContain("Ожидает начала работы");
    expect(html).toContain("Предыдущая попытка");
    expect(html).toContain("Завершение");
  });

  it("не заменяет существующую сводку и полный входной текст", async () => {
    const progressSummary = "Сохранённый итог предыдущей попытки";
    const inputSummary = "Исходное задание без изменений";
    const html = await renderNode({ ...node, progressSummary, inputSummary });
    expect(html).toContain(progressSummary);
    expect(html).toContain(inputSummary);
    expect(node.state).toBe("FAILED");
  });
});
