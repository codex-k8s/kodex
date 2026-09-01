import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import AccessModelOverview from "@/features/access/components/AccessModelOverview.vue";

const messages = {
  ru: {
    access: {
      model: {
        title: "Как вычисляется доступ",
        authorityBadge: "Авторитет: control-plane",
        organizationContext: "Организационный контекст",
        projectContext: "Контекст Проекта",
        layers: {
          identity: { title: "Identity", description: "Субъект" },
          platform: {
            title: "Платформенная роль",
            description: "Системная роль",
          },
          project: {
            title: "Доступ к Проекту",
            description: "Членство",
          },
          effective: {
            title: "Эффективное решение",
            description: "Backend",
          },
        },
        rule: "Роль без привязки ничего не разрешает.",
      },
    },
  },
};

describe("AccessModelOverview", () => {
  it("явно разделяет identity, platform role, Project и effective access", async () => {
    const app = createSSRApp({
      render: () => h(AccessModelOverview, { projectContext: true }),
    });
    app.use(
      createI18n({ legacy: false, locale: "ru", messages, missingWarn: false }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Контекст Проекта");
    expect(html).toContain("Платформенная роль");
    expect(html).toContain("Доступ к Проекту");
    expect(html).toContain("Эффективное решение");
    expect(html).toContain("Авторитет: control-plane");
    expect(html).toContain("Роль без привязки ничего не разрешает");
  });
});
