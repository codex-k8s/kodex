import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";

async function render(content: string): Promise<string> {
  const app = createSSRApp({ render: () => h(SafeMarkdown, { content }) });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          common: {
            status: "Состояние",
            result: "Результат",
            yes: "Да",
            no: "Нет",
          },
          states: {
            SUCCEEDED: "Завершён",
            NEEDS_ATTENTION: "Требует внимания",
            FAILED: "Ошибка",
            CANCELLED: "Отменён",
          },
        },
      },
    }),
  );
  return renderToString(app);
}

describe("SafeMarkdown", () => {
  it("рендерит пользовательский markdown без исполнения HTML и изображений", async () => {
    const html = await render(`# Итог

**Готово**, файл \`result.md\`.

Внутренняя ссылка: \`run_1234567890abcdef\`.

[Документ](https://example.com/report) [опасная](javascript:alert(1))

![внешняя схема](https://example.com/private.png)

<script>alert("xss")</script>`);

    expect(html).toMatch(/<h1[^>]*>/);
    expect(html).toMatch(/<strong[^>]*>Готово<\/strong>/);
    expect(html).toMatch(/<code[^>]*>result\.md<\/code>/);
    expect(html).toContain('href="https://example.com/report"');
    expect(html).not.toContain('href="javascript:');
    expect(html).not.toContain("<img");
    expect(html).toContain("внешняя схема");
    expect(html).not.toContain("run_1234567890abcdef");
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;script&gt;");
  });

  it("показывает JSON как локализованные поля и скрывает opaque ref", async () => {
    const html = await render(
      JSON.stringify({
        status: "blocked",
        run_ref: "run_1234567890abcdef",
        lead_score: null,
        details: { verified: true },
      }),
    );

    expect(html).toContain("Состояние");
    expect(html).toContain("Требует внимания");
    expect(html).toContain("Lead score");
    expect(html).toContain("Да");
    expect(html).not.toContain("run_1234567890abcdef");
    expect(html).not.toContain("&quot;status&quot;");
    expect(html).not.toContain("{&quot;");
  });
});
