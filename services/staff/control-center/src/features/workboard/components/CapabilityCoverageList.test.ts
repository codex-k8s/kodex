import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import CapabilityCoverageList from "@/features/workboard/components/CapabilityCoverageList.vue";
import { homeCapabilityCoverage } from "@/features/workboard/model";

describe("CapabilityCoverageList", () => {
  it("показывает отсутствие API как покрытие, а не как найденные события", async () => {
    const app = createSSRApp({
      render: () =>
        h(CapabilityCoverageList, { items: homeCapabilityCoverage() }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            workboard: {
              coverage: {
                title: "Покрытие источников внимания",
                subtitle: "Не входят в Overview API",
                unavailable: "Нет API",
                capabilities: {
                  STOPPED_RUNS: {
                    title: "Остановленные запуски",
                    description: "Источник недоступен",
                  },
                  PROVIDER_AUTH_EXPIRY: {
                    title: "Авторизация провайдеров",
                    description: "Источник недоступен",
                  },
                  SESSION_CONTINUATION: {
                    title: "Продолжение сессий",
                    description: "Источник недоступен",
                  },
                },
              },
            },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Покрытие источников внимания");
    expect(html).toContain("Остановленные запуски");
    expect(html).toContain("Авторизация провайдеров");
    expect(html).toContain("Продолжение сессий");
    expect(html.match(/Нет API/g)).toHaveLength(3);
    expect(html).not.toContain("0 событий");
  });
});
