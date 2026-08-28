import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import RunToolDetails from "@/features/runs/RunToolDetails.vue";
import type { RunNode } from "@/shared/api/generated/openapi/types.gen";

const toolNode: RunNode = {
  ref: "nod_tool",
  runRef: "run_example",
  type: "EXTERNAL_ACTION",
  state: "SUCCEEDED",
  displayName: "Поиск по файлам",
  role: "Файлы Проекта",
  attempt: 1,
  inputSummary: "Запрос: квартальный отчёт",
  progressSummary: "Найдено 4 фрагмента",
  integrationNames: ["Корпоративное хранилище"],
  artifactRefs: [],
  childRunRefs: [],
  createdAt: "2026-08-28T08:00:00Z",
  startedAt: "2026-08-28T08:00:01Z",
  finishedAt: "2026-08-28T08:00:02.500Z",
  nextActions: [],
};

async function render(node?: RunNode): Promise<string> {
  const app = createSSRApp({
    render: () => h(RunToolDetails, { node }),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          common: {
            previous: "Назад",
            unavailable: "Функция временно недоступна",
            noData: "Нет данных",
            source: "Источник",
            input: "Входные данные",
            status: "Состояние",
            duration: "Длительность",
            result: "Результат",
          },
          runs: { nodeTypes: { EXTERNAL_ACTION: "Внешнее действие" } },
          states: { SUCCEEDED: "Завершено" },
        },
      },
    }),
  );
  return renderToString(app);
}

describe("RunToolDetails", () => {
  it("показывает только доступные детали EXTERNAL_ACTION", async () => {
    const html = await render(toolNode);

    expect(html).toContain("Поиск по файлам");
    expect(html).toContain("Корпоративное хранилище");
    expect(html).toContain("Запрос: квартальный отчёт");
    expect(html).toContain("Найдено 4 фрагмента");
    expect(html).toContain("1,5 s");
  });

  it("явно показывает unavailable при отсутствии tool-call данных", async () => {
    const html = await render();

    expect(html).toContain("Внешнее действие");
    expect(html).toContain("Функция временно недоступна");
    expect(html).toContain("Нет данных");
  });
});
