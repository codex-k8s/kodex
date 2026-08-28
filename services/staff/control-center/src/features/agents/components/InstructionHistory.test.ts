import { createSSRApp } from "vue";
import { createI18n } from "vue-i18n";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import InstructionHistory from "@/features/agents/components/InstructionHistory.vue";

describe("InstructionHistory", () => {
  it("показывает revisions без внутренних refs и не предлагает rollback текущей версии", async () => {
    const app = createSSRApp(InstructionHistory, {
      versions: [
        {
          ref: "ins_current",
          version: 3,
          revision: 3,
          state: "PUBLISHED",
          content: "Текущие инструкции",
          validationMessages: [],
          createdAt: "2026-08-27T10:00:00Z",
          publishedAt: "2026-08-27T10:00:00Z",
        },
        {
          ref: "ins_previous",
          version: 2,
          revision: 2,
          state: "PUBLISHED",
          content: "Предыдущие инструкции",
          validationMessages: [],
          createdAt: "2026-08-26T10:00:00Z",
          publishedAt: "2026-08-26T10:00:00Z",
        },
      ],
      currentRef: "ins_current",
      canRollback: true,
      busy: false,
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            common: {
              noData: "Нет данных",
              details: "Подробнее",
              cancel: "Отмена",
            },
            agents: {
              history: "История публикаций",
              historyHelp: "Справка",
              historyEmpty: "Пусто",
              revision: "Ревизия {revision}",
              currentRevision: "Текущая",
              rollback: "Вернуть",
              rollbackConfirm: "Вернуть {revision}?",
            },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Ревизия 3");
    expect(html).toContain("Ревизия 2");
    expect(html.match(/>Вернуть</g)).toHaveLength(1);
    expect(html).not.toContain("ins_current");
    expect(html).not.toContain("ins_previous");
  });
});
