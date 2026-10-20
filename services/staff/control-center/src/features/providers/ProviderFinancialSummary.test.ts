import { createPinia, setActivePinia } from "pinia";
import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import type { ProviderFinancialOverview } from "@/shared/api/generated/openapi/types.gen";
import { useProvidersStore } from "./store";
import ProviderFinancialSummary from "./ProviderFinancialSummary.vue";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

async function renderFinancial(live: boolean, used: number[]) {
  const pinia = createPinia();
  setActivePinia(pinia);
  const now = Date.now();
  const overview: ProviderFinancialOverview = {
    accounts: used.map((usedPercent, index) => ({
      ref: `pac_fixture_${String(index)}`,
      name: `Аккаунт ${String(index + 1)}`,
      financialSummary: {
        version: 1,
        receivedAt: new Date(now).toISOString(),
        observation: {
          sequence: 1,
          state: "AVAILABLE",
          windows: [
            {
              usedPercent,
              durationMinutes: 10080,
              resetsAt: Math.floor(now / 1000) + 3600,
            },
          ],
          credits: { hasCredits: true, balance: "59999.1234567890123456789" },
        },
      },
    })),
    total: used.length,
    exhausted: used.filter((value) => value === 100).length,
    unknown: 0,
    observedAt: new Date(now).toISOString(),
    validUntil: new Date(now + 300000).toISOString(),
  };
  useProvidersStore().applyFinancialSnapshot(overview);
  const app = createSSRApp({
    render: () => h(ProviderFinancialSummary, { live }),
  });
  app.use(pinia);
  app.use(i18n);
  return renderToString(app);
}

describe("компактная финансовая сводка", () => {
  it("группирует exhausted внутри трёх слотов, оставляя credits и reset", async () => {
    const html = await renderFinancial(true, [100, 10, 20]);
    expect((html.match(/<li(?:\s|>)/g) ?? []).length).toBe(3);
    expect((html.match(/<details(?:\s|>)/g) ?? []).length).toBe(1);
    const summary = html.match(/<summary[^>]*>([\s\S]*?)<\/summary>/)?.[1];
    expect(summary).toContain("59999.1234567890123456789");
    expect(summary).toContain("Кредиты");
    expect(html).toContain("Сброс:");
    expect(html).toContain(
      "Единица баланса и денежный эквивалент не установлены",
    );
    expect(html).not.toContain("Оплаченные");
  });

  it("три exhausted аккаунта дают один раскрываемый элемент", async () => {
    const html = await renderFinancial(true, [100, 100, 100]);
    expect((html.match(/<li(?:\s|>)/g) ?? []).length).toBe(1);
    expect(html).toContain("Включённый лимит исчерпан: 3");
  });

  it("offline данные не становятся исчерпанием или доступным балансом", async () => {
    const html = await renderFinancial(false, [100]);
    expect(html).toContain("Данные устарели");
    expect(html).not.toMatch(/<details(?:\s|>)/);
    expect(html).not.toContain("59999.1234567890123456789");
    expect(html).not.toContain("Сброс:");
  });
});
