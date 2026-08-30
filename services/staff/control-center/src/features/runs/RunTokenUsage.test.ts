import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import RunTokenUsage from "@/features/runs/RunTokenUsage.vue";

describe("RunTokenUsage", () => {
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
