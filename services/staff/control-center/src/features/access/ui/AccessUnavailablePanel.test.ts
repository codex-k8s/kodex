import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import AccessUnavailablePanel from "@/features/access/ui/AccessUnavailablePanel.vue";

const messages = {
  ru: {
    accessRedesign: {
      backendUnavailableShort: "backend недоступен",
      subject: "Участник",
      resource: "Ресурс",
      action: "Действие",
      why: "Почему?",
      failClosed: "UI не вычисляет authority, bindings или access локально.",
      surface: {
        EFFECTIVE: {
          title: "Эффективный доступ",
          description: "Описание",
          emptyTitle: "Explain-access недоступен",
          gap: "Нет серверного расчёта",
        },
      },
    },
  },
};

describe("AccessUnavailablePanel", () => {
  it("не разрешает локальный explain-access без backend расчёта", async () => {
    const app = createSSRApp({
      render: () => h(AccessUnavailablePanel, { section: "EFFECTIVE" }),
    });
    app.use(
      createI18n({ legacy: false, locale: "ru", messages, missingWarn: false }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Explain-access недоступен");
    expect(html.match(/<select disabled/g)).toHaveLength(3);
    expect(html).toContain("UI не вычисляет authority");
  });
});
