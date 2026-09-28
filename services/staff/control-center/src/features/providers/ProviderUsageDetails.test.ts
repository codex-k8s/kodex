import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import { providerUsageFixture } from "@/test-utils/provider-usage-fixture";
import ProviderUsageDetails from "./ProviderUsageDetails.vue";
import { providerUsageMessages } from "./usage-messages";

async function render(expiresAt: string): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(ProviderUsageDetails, {
        usage: providerUsageFixture(undefined, { expiresAt }),
      }),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: { ru: { providerUsage: providerUsageMessages.ru } },
    }),
  );
  return renderToString(app);
}

describe("срок действия доступности учётной записи", () => {
  it("не выдаёт просроченный снимок за текущую проверку", async () => {
    const html = await render("2020-01-01T00:00:00Z");

    expect(html).toContain("Данные проверки устарели");
    expect(html).not.toContain("Есть свободная ёмкость выполнения");
    expect(html).not.toContain("Ёмкость выполнения");
  });

  it("показывает действующие измерения до срока истечения", async () => {
    const html = await render("2099-01-01T00:00:00Z");

    expect(html).toContain("Ёмкость выполнения");
    expect(html).not.toContain("Данные проверки устарели");
  });
});
