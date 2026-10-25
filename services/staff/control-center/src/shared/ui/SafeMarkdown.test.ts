import { readFileSync } from "node:fs";
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
  it.each([
    ["заголовке", (text: string) => `# ${text}`],
    ["абзаце", (text: string) => text],
    ["цитате", (text: string) => `> ${text}`],
    ["маркированном списке", (text: string) => `- ${text}`],
    ["нумерованном списке", (text: string) => `1. ${text}`],
    ["заголовке таблицы", (text: string) => `| ${text} |\n| --- |\n| Строка |`],
    ["ячейке таблицы", (text: string) => `| Заголовок |\n| --- |\n| ${text} |`],
  ])(
    "одинаково экранирует текст и ограничивает ссылки в %s",
    async (_label, wrap) => {
      const html = await render(
        wrap(
          "**Жирный** _Курсив_ `a<b` <svg/onload=alert(1)> " +
            "[Внешняя](https://example.com/help) [Внутренняя](/projects/example) " +
            "[Якорь](#result) [Письмо](mailto:qa@example.invalid) " +
            "[Опасная](javascript:bad) ![Картинка](https://example.com/image.png) " +
            "[Локальная](/workspace/report.md?private=hidden) run_1234567890abcdef",
        ),
      );
      expect(html).toMatch(/<strong[^>]*>Жирный<\/strong>/);
      expect(html).toMatch(/<em[^>]*>Курсив<\/em>/);
      expect(html).toMatch(/<code[^>]*>a&lt;b<\/code>/);
      expect(html).toContain("&lt;svg/onload=alert(1)&gt;");
      expect(html).not.toContain("<svg");
      expect(html).not.toContain("<img");
      expect(html).toContain("Картинка");
      for (const [href, target] of [
        ["https://example.com/help", "_blank"],
        ["/projects/example", "_self"],
        ["#result", "_self"],
        ["mailto:qa@example.invalid", "_blank"],
      ] as const) {
        expect(html).toContain(
          `href="${href}" rel="noopener noreferrer" target="${target}"`,
        );
      }
      expect(html).not.toContain('href="javascript:');
      expect(html).not.toContain('href="/workspace');
      expect(html).not.toContain("private=hidden");
      expect(html).not.toContain("run_1234567890abcdef");
      expect(html).toMatch(/<code[^>]*>\/workspace\/report\.md<\/code>/);
    },
  );

  it("сохраняет читаемые колонки и полный текст в локально прокручиваемой таблице", async () => {
    const longText =
      "Подробное описание результата проверки без усечения ".repeat(12);
    const reference = "sha256:" + "a".repeat(64);
    const html = await render(
      `| Попытка | Результат | Источник |\n| --- | --- | --- |\n| Первая попытка проверки | ${longText} | ${reference} |`,
    );
    expect(html).toMatch(/class="markdown-table-wrap"[^>]*tabindex="0"/);
    expect(html).toMatch(/<th[^>]*>[^]*?Попытка/);
    expect(html).toContain("Первая попытка проверки");
    expect(html).toContain(longText.trim());
    expect(html).toContain(reference);
    expect(html.match(/<td\b/g)).toHaveLength(3);
    const source = readFileSync(
      new URL("./SafeMarkdown.vue", import.meta.url),
      "utf8",
    );
    const wrapper = source.match(/\.markdown-table-wrap\s*\{([^}]*)\}/)?.[1];
    const cells = source.match(
      /\.safe-markdown :where\(th, td\)\s*\{([^}]*)\}/,
    )?.[1];
    expect(wrapper).toContain("min-width: 0");
    expect(wrapper).toContain("max-width: 100%");
    expect(wrapper).toContain("overflow-x: auto");
    expect(cells).toContain("min-width: 10rem");
    expect(cells).not.toMatch(/text-overflow|line-clamp|overflow:\s*hidden/);
  });

  it("не выдаёт локальный отчёт выполнения за маршрут приложения", async () => {
    const path = "/workspace/.kodex/outbox/qa1797-manager-smoke.md";
    const html = await render(`[Отчёт](${path})`);

    expect(html).not.toContain("<a");
    expect(html).toContain("Отчёт");
    expect(html).toMatch(
      /<code[^>]*>\/workspace\/\.kodex\/outbox\/qa1797-manager-smoke\.md<\/code>/,
    );
  });

  it.each([
    "/workspace/report.md",
    "/work%73pace/.kodex/outbox/report.md",
    "/projects/../workspace/.kodex/outbox/report.md",
    ".kodex/outbox/report.md",
    "./.kodex/outbox/report.md",
    "file:///workspace/.kodex/outbox/report.md",
  ])(
    "сохраняет локальный путь %s читаемым без подбора artifact ref",
    async (path) => {
      const html = await render(`[Отчёт](${path})`);

      expect(html).not.toContain("<a");
      expect(html).toContain("Отчёт");
      expect(html).toContain("report.md</code>");
      expect(html).not.toContain("/artifacts/");
    },
  );

  it("не раскрывает query локальной ссылки и сохраняет внешние ссылки с похожим путём", async () => {
    const html = await render(
      "[Отчёт](/workspace/report.md?token=SYNTHETIC_PRIVATE_VALUE#fragment) [документ](https://example.com/workspace/report.md)",
    );

    expect(html).not.toContain("SYNTHETIC_PRIVATE_VALUE");
    expect(html).not.toContain("fragment");
    expect(html).toContain('href="https://example.com/workspace/report.md"');
    expect(html).not.toContain('href="/workspace');
  });

  it("сохраняет подчёркивания внутри имён переменных и обычное выделение", async () => {
    const html = await render(
      "Добавить ASSISTANT_REVISION_TEST=draft и _проверить_ результат.",
    );

    expect(html).toContain("ASSISTANT_REVISION_TEST=draft");
    expect(html).toMatch(/<em[^>]*>проверить<\/em>/);
  });

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

  it("открывает подтверждённый переход к проекту в текущей вкладке", async () => {
    const html = await render(
      "[Открыть Marketplace](/projects/prj_marketplace123) [внешняя страница](https://example.com/help) [подмена](//evil.example/project)",
    );

    expect(html).toContain('href="/projects/prj_marketplace123"');
    expect(html).toMatch(
      /href="\/projects\/prj_marketplace123"[^>]*target="_self"/,
    );
    expect(html).toMatch(
      /href="https:\/\/example.com\/help"[^>]*target="_blank"/,
    );
    expect(html).not.toContain('href="//evil.example/project"');
  });
});
