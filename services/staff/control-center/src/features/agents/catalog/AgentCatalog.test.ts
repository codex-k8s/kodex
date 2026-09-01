import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import AgentCatalog from "./AgentCatalog.vue";
import type { Agent } from "@/shared/api/generated/openapi/types.gen";

const agent: Agent = {
  ref: "agent_sales",
  version: 1,
  projectRef: "project_sales",
  name: "Аналитик продаж",
  purpose: "Проверяет обращения",
  roleDescription: "Работает с фактами",
  state: "READY",
  enabled: true,
  system: false,
  runtimeRef: "runtime_standard",
  runtimeName: "Стандартный runtime",
  runtimeReady: true,
  capabilities: [],
  integrations: [],
  knowledgeArtifactRefs: [],
  updatedAt: "2026-08-30T10:00:00Z",
  nextActions: [],
};

describe("AgentCatalog", () => {
  it("показывает серверный query и доступный fallback cursor-подгрузки", async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/:pathMatch(.*)*", component: { render: () => null } }],
    });
    await router.push("/");
    await router.isReady();
    const app = createSSRApp({
      render: () =>
        h(AgentCatalog, {
          agents: [agent],
          projectRef: "project_sales",
          view: "grid",
          query: "аналитик",
          hasMore: true,
          loadingMore: false,
        }),
    });
    app.use(router);
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            agents: {
              title: "ИИ-сотрудники",
              catalogSearch: "Поиск сотрудников",
              catalogSearchPlaceholder: "Найти сотрудника",
              catalogClearSearch: "Очистить поиск",
              catalogLoaded: "Загружено: {count}",
              catalogLoadMore: "Загрузить ещё",
              catalogLoadingMore: "Загрузка",
              catalogView: "Вид",
              catalogGrid: "Карточки",
              catalogTable: "Таблица",
              currentActivity: "Сейчас",
            },
            common: { empty: "Пусто", open: "Открыть", unknownStatus: "—" },
            states: { READY: "Готов" },
          },
        },
      }),
    );

    const html = await renderToString(app);
    expect(html).toContain('value="аналитик"');
    expect(html).toContain("Загружено: 1");
    expect(html).toContain("Загрузить ещё");
    expect(html).not.toContain("agent-catalog__filter");
  });
});
