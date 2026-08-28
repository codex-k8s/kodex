import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

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
  it("оставляет все решения отключёнными без integration intent", async () => {
    const app = createSSRApp({
      render: () => h(IntegrationApprovalPanel),
    });
    app.use(
      createI18n({ legacy: false, locale: "ru", messages, missingWarn: false }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Очередь интеграционных решений недоступна");
    expect(html.match(/<button[^>]*disabled/g)).toHaveLength(3);
  });
});
