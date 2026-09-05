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
      packageDetails: "Подробнее",
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
        inputFields: [
          {
            key: "ref",
            label: "Ревизия",
            help: "Exact branch or commit",
            valueType: "TEXT",
            required: true,
          },
        ],
      },
    ],
    configurationFields: [
      {
        key: "base_url",
        label: "Адрес API",
        help: "HTTPS origin",
        valueType: "URL",
        required: true,
      },
    ],
    schemaVersion: "integrations.kodex.io/v1",
    definitionVersion: "1.0.0",
    origin: "SHIPPED",
    digest: "a".repeat(64),
    adapter: "GITHUB",
    adapterOwner: "integration-gateway",
    executionRoute: "MANAGED_MCP",
    adapterReadiness: "READY",
  };
}

describe("IntegrationCatalogPanel", () => {
  it("показывает только определения, подтверждённые сервером", async () => {
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

    expect(html).toContain("GitHub");
    for (const missing of ["GitLab", "Jira", "Confluence", "Email"]) {
      expect(html).not.toContain(missing);
    }
    expect(html).not.toContain("YAML · API —");
    expect(html.match(/<button[^>]*disabled/g)).toBeNull();
    expect(html).toContain('aria-haspopup="dialog"');
    expect(html).toContain("Подробнее");
    expect(html).not.toContain("package-details");
    expect(html).not.toContain("zero-connection-notice");
  });
});
