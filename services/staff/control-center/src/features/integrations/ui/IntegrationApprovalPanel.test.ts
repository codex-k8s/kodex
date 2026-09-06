import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";
import { createRouter, createMemoryHistory } from "vue-router";

import IntegrationApprovalPanel from "@/features/integrations/ui/IntegrationApprovalPanel.vue";

const messages = {
  ru: {
    common: {
      approve: "Одобрить",
      requestChanges: "Запросить изменения",
      reject: "Отклонить",
    },
    integrationsRedesign: {
      approvalsTitle: "Решения Human Gate",
      approvalsDescription: "Описание",
      decisionsEntry: "Выберите решение с точным намерением интеграции",
      openDecisions: "Открыть решения",
      backendUnavailableShort: "backend недоступен",
      approvalQueue: "Ожидают решения",
      approvalReadUnavailable: "Очередь интеграционных решений недоступна",
      approvalReadGap: "Пробел API",
      effectPreview: "Что изменится",
      noIntentSelected: "намерение не загружено",
      approvalFailClosed: "Действия отключены",
    },
  },
};

describe("IntegrationApprovalPanel", () => {
  it("открывает реальную защищённую очередь без выдуманного connection filter", async () => {
    const app = createSSRApp({
      render: () => h(IntegrationApprovalPanel),
    });
    app.use(
      createI18n({ legacy: false, locale: "ru", messages, missingWarn: false }),
    );
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/decisions", component: { render: () => null } }],
    });
    app.use(router);
    await router.push("/decisions");
    await router.isReady();

    const html = await renderToString(app);

    expect(html).toContain('href="/decisions"');
    expect(html).toContain("Открыть решения");
    expect(html).not.toContain("connectionRef");
    expect(html).not.toContain("disabled");
  });
});
