import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import RunTokenUsage from "@/features/runs/RunTokenUsage.vue";

async function renderContextOnly(compact: boolean): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(RunTokenUsage, {
        compact,
        usage: {
          totalTokens: 0,
          inputTokens: 0,
          cachedInputTokens: 0,
          cacheWriteInputTokens: 0,
          outputTokens: 0,
          reasoningOutputTokens: 0,
          modelContextWindow: 258400,
        },
      }),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      missingWarn: false,
      fallbackWarn: false,
      messages: { ru: {} },
    }),
  );
  return renderToString(app);
}

describe("RunTokenUsage", () => {
  it("не выдаёт размер контекстного окна за расход в компактном виде", async () => {
    expect(await renderContextOnly(true)).not.toContain("token-usage");
  });

  it("показывает только контекстное окно до появления измеренного расхода", async () => {
    const html = await renderContextOnly(false);
    expect(html).toContain("runs.usage.contextWindow");
    expect(html).toContain(new Intl.NumberFormat("ru").format(258400));
    expect(html.match(/<dt\b/gu)).toHaveLength(1);
    expect(html).not.toContain("runs.usage.total");
    expect(html).not.toContain("runs.usage.input");
    expect(html).not.toContain("runs.usage.cached");
    expect(html).not.toContain("runs.usage.output");
    expect(html).not.toContain("runs.usage.reasoning");
  });

  it("показывает измеренную сводку запуска в локали пользователя", async () => {
    const usage = {
      totalTokens: 43034,
      inputTokens: 42100,
      cachedInputTokens: 38000,
      cacheWriteInputTokens: 0,
      outputTokens: 934,
      reasoningOutputTokens: 210,
      modelContextWindow: 258400,
    };
    const app = createSSRApp({
      render: () => h(RunTokenUsage, { compact: true, usage }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            runs: {
              usage: {
                title: "Использование токенов",
                total: "Всего",
                input: "Вход",
                cached: "Из кэша",
                output: "Выход",
                reasoning: "Рассуждение",
                contextWindow: "Контекст",
              },
            },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain('class="token-usage token-usage--compact"');
    expect(html).toContain('aria-label="Использование токенов"');
    expect(html).toContain(new Intl.NumberFormat("ru").format(43034));
    expect(html).toContain(new Intl.NumberFormat("ru").format(38000));
    expect(html).not.toContain("Рассуждение");
  });

  it("не создаёт пустую панель до появления измерений", async () => {
    const app = createSSRApp({
      render: () =>
        h(RunTokenUsage, {
          usage: {
            totalTokens: 0,
            inputTokens: 0,
            cachedInputTokens: 0,
            cacheWriteInputTokens: 0,
            outputTokens: 0,
            reasoningOutputTokens: 0,
            modelContextWindow: 0,
          },
        }),
    });
    app.use(createI18n({ legacy: false, locale: "ru", messages: { ru: {} } }));

    expect(await renderToString(app)).not.toContain("token-usage");
  });
});
