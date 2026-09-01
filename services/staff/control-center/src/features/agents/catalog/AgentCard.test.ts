import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import AgentCard from "@/features/agents/catalog/AgentCard.vue";
import type { AgentCatalogItem } from "@/features/agents/catalog/model";

async function render(item: AgentCatalogItem): Promise<string> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { render: () => null } }],
  });
  await router.push("/");
  await router.isReady();
  const app = createSSRApp({
    render: () =>
      h(AgentCard, {
        item,
        to: `/projects/project_sales/agents/${item.ref}`,
      }),
  });
  app.use(router);
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          agents: { currentActivity: "Сейчас" },
          common: {
            open: "Открыть",
            structuredResult: "Структурированный результат",
            unknownStatus: "Статус недоступен",
          },
          states: { READY: "Готов", RUNNING: "Выполняется" },
        },
      },
    }),
  );
  return renderToString(app);
}

describe("AgentCard", () => {
  const item: AgentCatalogItem = {
    ref: "agent_sales",
    name: "Аналитик продаж",
    purpose: "Проверяет входящие обращения",
    role: "Аналитик",
    roleDescription: "Проверяет факты",
    state: "RUNNING",
    statusTone: "accent",
    initials: "АП",
    avatarTone: 2,
    runtimeName: "Стандартный runtime",
    runtimeProvider: "openai",
    runtimeModel: "gpt-5",
    runtimeRevision: "rev-4",
    runtimeReady: true,
    currentActivity: "Собирает сводку по обращениям",
    updatedAt: "2026-08-28T10:00:00Z",
  };

  it("показывает avatar fallback, состояние, активность и явный переход", async () => {
    const html = await render(item);

    expect(html).toContain("АП");
    expect(html).toContain("Выполняется");
    expect(html).toContain("Собирает сводку по обращениям");
    expect(html).toContain("Стандартный runtime");
    expect(html).toContain("gpt-5");
    expect(html).toContain("/projects/project_sales/agents/agent_sales");
    expect(html).toContain("Открыть");
    expect(html).not.toContain("<img");
  });

  it("не выдумывает активность, когда API её не передал", async () => {
    const html = await render({
      ...item,
      state: "READY",
      currentActivity: undefined,
    });

    expect(html).toContain("Готов");
    expect(html).not.toContain("последний запуск");
  });
});
