import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { createSSRApp, h } from "vue";
import { describe, expect, it } from "vitest";

import IntegrationCatalogPanel from "@/features/integrations/ui/IntegrationCatalogPanel.vue";
import { buildIntegrationPackages } from "@/features/integrations/ui/model";
import type { IntegrationDefinition } from "@/shared/api/generated/openapi/types.gen";

const messages = {
  ru: {
    integrations: {
      connect: "Подключить",
      unavailable: "Сейчас недоступна",
      risk: {
        READ: "чтение",
        WRITE: "изменение",
        SENSITIVE: "чувствительные данные",
        DESTRUCTIVE: "необратимое действие",
      },
    },
    integrationsRedesign: {
      catalogTitle: "Каталог пакетов",
      catalogDescription: "Описание",
      packageCount: "Пакетов: {count}",
      searchPackages: "Найти",
      category: "Категория",
      allCategories: "Все",
      firstParty: "first-party",
      customPackage: "пользовательский",
      connectionCount: "Подключений: {count}",
      capabilityCount: "Возможностей: {count}",
      approvalCapabilityCount: "Human Gate: {count}",
      packageDetails: "Описание пакета",
      packageDetailsUnavailable: "Backend manifest недоступен",
      connectUnavailable: "Подключение недоступно",
      noPackages: "Не найдено",
      noPackagesHint: "Измените фильтр",
      zeroConnectionsReady: "Платформа работает без подключений",
    },
  },
};

function githubDefinition(): IntegrationDefinition {
  return {
    key: "github",
    name: "GitHub",
    description: "Репозитории и задачи",
    category: "source-control",
    builtIn: true,
    available: true,
    capabilities: [
      {
        key: "github.repository.read",
        name: "Чтение репозитория",
        description: "Чтение разрешённого репозитория",
        risk: "READ",
        approvalRequired: false,
        operation: "github.repository.read",
        approvalPolicy: "NONE",
        resourceKind: "GITHUB_REPOSITORY",
        inputFields: [],
      },
    ],
    configurationFields: [],
    schemaVersion: "integrations.kodex.io/v1",
    definitionVersion: "1.0.0",
    origin: "SHIPPED",
    digest: "a".repeat(64),
    adapter: "GITHUB",
  };
}

describe("IntegrationCatalogPanel", () => {
  it("не выдаёт отсутствующие YAML packages за доступные подключения", async () => {
    const packages = buildIntegrationPackages([githubDefinition()], [], true);
    const app = createSSRApp({
      render: () =>
        h(IntegrationCatalogPanel, {
          packages,
          categories: ["source-control"],
          search: "",
          category: "",
        }),
    });
    app.use(
      createI18n({ legacy: false, locale: "ru", messages, missingWarn: false }),
    );

    const html = await renderToString(app);

    for (const name of [
      "GitHub",
      "GitLab",
      "Jira",
      "Confluence",
      "Email",
      "Custom HTTP",
    ]) {
      expect(html).toContain(name);
    }
    expect(html).toContain("YAML · API —");
    expect(html.match(/<button[^>]*disabled/g)).toHaveLength(5);
  });
});
