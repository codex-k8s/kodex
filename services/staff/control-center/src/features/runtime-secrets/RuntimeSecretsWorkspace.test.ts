import { renderToString } from "@vue/server-renderer";
import { createPinia } from "pinia";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it, vi } from "vitest";

vi.mock("./api", () => ({
  createRuntimeSecret: vi.fn(),
  loadRuntimeSecretPage: vi.fn(),
  normalizeRuntimeSecretProblem: vi.fn((error: unknown) => error),
  revealRuntimeSecret: vi.fn(),
  revokeRuntimeSecret: vi.fn(),
  rotateRuntimeSecret: vi.fn(),
}));
vi.mock("@/features/session/store", () => ({
  useSessionStore: () => ({
    pendingRuntimeSecretReveal: () => undefined,
  }),
}));

import RuntimeSecretsWorkspace from "./RuntimeSecretsWorkspace.vue";
import { useRuntimeSecretsStore } from "./store";

describe("RuntimeSecretsWorkspace", () => {
  it("рендерит только masked hint и не содержит plaintext", async () => {
    const pinia = createPinia();
    const store = useRuntimeSecretsStore(pinia);
    store.items = [
      {
        ref: "secret_main",
        version: 3,
        projectRef: "project_sales",
        name: "CRM_TOKEN",
        description: "Токен CRM",
        valueType: "STRING",
        state: "ACTIVE",
        currentRevision: 2,
        displayHint: { prefix: "tok", suffix: "9z" },
        nextActions: ["ROTATE", "REVOKE", "REVEAL"],
        createdAt: "2026-08-29T08:00:00Z",
        updatedAt: "2026-08-29T09:00:00Z",
      },
    ];
    const app = createSSRApp({
      render: () => h(RuntimeSecretsWorkspace, { projectRef: "project_sales" }),
    });
    app.use(pinia);
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        missingWarn: false,
        messages: {
          ru: {
            common: {
              actions: "Действия",
              loading: "Загрузка",
              noData: "Нет данных",
            },
            runtimeSecrets: {
              create: "Создать секрет",
              loadMore: "Загрузить ещё",
              maskedHint: "Безопасная маска",
              reveal: "Показать значение",
              revealNamed: "Показать значение секрета {name}",
              revision: "Ревизия",
              revoke: "Отозвать",
              revokeNamed: "Отозвать секрет {name}",
              rotate: "Ротировать",
              rotateNamed: "Ротировать секрет {name}",
              search: "Поиск",
              searchPlaceholder: "Найти",
              secret: "Секрет",
              shown: "Показано: {count}",
              types: { STRING: "Строка" },
              updatedAt: "Обновлено",
              valueType: "Тип",
            },
            states: { ACTIVE: "Активен" },
          },
        },
      }),
    );

    const html = await renderToString(app);
    expect(html).toContain("CRM_TOKEN");
    expect(html).toContain("tok••••••9z");
    expect(html).not.toContain("raw-secret-plaintext");
    expect(html).toContain("Показать значение секрета CRM_TOKEN");

    const buttonTag = (label: string): string => {
      const position = html.indexOf(label);
      const start = html.lastIndexOf("<button", position);
      return html.slice(start, html.indexOf(">", start) + 1);
    };
    expect(buttonTag("Показать значение секрета CRM_TOKEN")).not.toContain(
      "disabled",
    );
    expect(html).not.toContain("secret.create");
    expect(html).not.toContain("secret.rotate");
    expect(html).not.toContain("secret.revoke");
    expect(html).not.toContain("nextAction");
  });
});
