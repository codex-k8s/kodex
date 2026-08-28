import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import StatusBadge from "@/shared/ui/StatusBadge.vue";

async function render(props: {
  state: string;
  label?: string;
  tone?: "success" | "danger" | "warning" | "neutral";
}): Promise<string> {
  const app = createSSRApp({ render: () => h(StatusBadge, props) });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          common: { unknownStatus: "Статус недоступен" },
          states: { SUCCEEDED: "Завершён", PUBLISHED: "Опубликован" },
        },
      },
    }),
  );
  return renderToString(app);
}

describe("StatusBadge", () => {
  it("сохраняет семантический tone состояния по умолчанию", async () => {
    const html = await render({ state: "SUCCEEDED" });

    expect(html).toContain("Завершён");
    expect(html).toContain("status-badge--success");
    expect(html).toContain('data-state="SUCCEEDED"');
  });

  it("позволяет честно показать terminal lifecycle нейтральным", async () => {
    const html = await render({ state: "SUCCEEDED", tone: "neutral" });

    expect(html).toContain("Завершён");
    expect(html).toContain("status-badge--neutral");
    expect(html).not.toContain("status-badge--success");
  });

  it("не показывает пользователю неизвестный backend enum", async () => {
    const html = await render({ state: "INTERNAL_FUTURE_STATE" });

    expect(html).toContain("Статус недоступен");
    expect(html).not.toContain(">INTERNAL_FUTURE_STATE<");
    expect(html).toContain("status-badge--neutral");
  });

  it("отличает опубликованное состояние акцентом от успеха", async () => {
    const html = await render({ state: "PUBLISHED" });

    expect(html).toContain("Опубликован");
    expect(html).toContain("status-badge--accent");
  });
});
