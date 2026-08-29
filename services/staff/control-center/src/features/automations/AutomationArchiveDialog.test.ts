import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import AutomationArchiveDialog from "@/features/automations/AutomationArchiveDialog.vue";
import type { Schedule } from "@/shared/api/generated/openapi/types.gen";

const schedule = {
  ref: "schedule_daily",
  version: 4,
  projectRef: "project_sales",
  name: "Ежедневная сводка",
  target: {
    type: "AGENT",
    ref: "agent_sales",
    displayName: "Аналитик продаж",
    version: 2,
  },
  state: "ACTIVE",
  preset: "DAILY",
  timeOfDay: "09:00",
  timezone: "Europe/Saratov",
  input: { task: "Подготовить сводку" },
  sessionPolicy: "NEW_EACH_RUN",
  notificationPolicy: "CONTROL_CENTER_ONLY",
  nextActions: ["EDIT", "DISABLE", "ARCHIVE"],
} as Schedule;

describe("AutomationArchiveDialog", () => {
  it("называет terminal-команду архивацией и объясняет сохранение истории", async () => {
    const app = createSSRApp({
      render: () =>
        h(AutomationArchiveDialog, {
          schedule,
          title: "Архивировать автоматизацию?",
          description:
            "Будущие запуски будут отменены. История останется доступна только для чтения. Это не безвозвратное удаление.",
          cancelLabel: "Отмена",
          confirmLabel: "Переместить в архив",
        }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: { common: { close: "Закрыть" } } },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Ежедневная сводка");
    expect(html).toContain("Будущие запуски будут отменены");
    expect(html).toContain("только для чтения");
    expect(html).toContain("не безвозвратное удаление");
    expect(html).toContain("Переместить в архив");
    expect(html).not.toContain(">Удалить<");
  });
});
