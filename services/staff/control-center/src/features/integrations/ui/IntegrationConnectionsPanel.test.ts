import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { createSSRApp, h } from "vue";
import { describe, expect, it } from "vitest";

import IntegrationConnectionsPanel from "@/features/integrations/ui/IntegrationConnectionsPanel.vue";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";

const definition: IntegrationDefinition = {
  key: "synthetic",
  name: "Synthetic HTTP",
  description: "Локальная проверка lifecycle",
  category: "testing",
  builtIn: true,
  available: true,
  capabilities: [
    {
      key: "synthetic.journal.write",
      name: "Изменить journal",
      description: "Versioned write",
      risk: "WRITE",
      approvalRequired: true,
      operation: "synthetic.journal.write",
      approvalPolicy: "HUMAN_EACH_EFFECT",
      resourceKind: "SYNTHETIC_JOURNAL",
      inputFields: [],
    },
  ],
  configurationFields: [
    {
      key: "journal",
      label: "Журнал",
      help: "Exact journal",
      valueType: "TEXT",
      required: true,
    },
  ],
  schemaVersion: "integrations.kodex.io/v1",
  definitionVersion: "3.0.0",
  origin: "SHIPPED",
  digest: "a".repeat(64),
  adapter: "SYNTHETIC_HTTP",
};

const connection: IntegrationConnection = {
  ref: "connection_synthetic",
  version: 3,
  definitionKey: definition.key,
  name: "Synthetic lifecycle",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "Не требуются",
  lastTestedAt: "2026-08-30T10:00:00Z",
  lastTestOutcome: "READY",
  capabilities: definition.capabilities,
  grants: [],
  nextActions: ["TEST", "DISABLE", "MANAGE_GRANTS", "UPDATE", "DELETE"],
  definitionVersion: definition.definitionVersion,
  definitionDigest: definition.digest,
  publicConfiguration: {
    journal: "ui-lifecycle",
    token: "must-never-be-rendered",
  },
};

const messages = {
  ru: {
    common: {
      test: "Проверить",
      enable: "Включить",
      disable: "Отключить",
      edit: "Изменить",
      delete: "Удалить",
    },
    integrations: {
      credentialsConfigured: "Учётные данные настроены",
      credentialsNotConfigured: "Учётные данные не настроены",
      configureCredential: "Настроить учётные данные",
      lastTest: "Последняя проверка",
      manageGrants: "Настроить разрешения",
      noConnectionsTitle: "Платформа работает без интеграций",
      noConnections: "Откройте каталог, чтобы настроить подключение.",
      webOnlyReady:
        "Подключения необязательны: core-сценарии работают без внешних систем.",
      risk: { WRITE: "изменение" },
    },
    integrationsRedesign: {
      connectionsTitle: "Рабочие подключения",
      connectionsDescription: "Описание",
      connectionCount: "Подключений: {count}",
      noConnectionsYet: "Подключений пока нет",
      activeGrants: "разрешений",
      capabilitiesShort: "возможностей",
    },
  },
};

async function renderPanel(
  values: readonly IntegrationConnection[],
  coreReady: boolean,
): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(IntegrationConnectionsPanel, {
        connections: values,
        definitions: { synthetic: definition },
        coreReady,
        busyRef: "",
      }),
  });
  app.use(
    createI18n({ legacy: false, locale: "ru", messages, missingWarn: false }),
  );
  return renderToString(app);
}

describe("IntegrationConnectionsPanel", () => {
  it("показывает только разрешённые server-owned lifecycle действия", async () => {
    const html = await renderPanel([connection], true);

    expect(html).toContain("Платформа работает без интеграций");
    expect(html).toContain("Подключения необязательны");
    expect(html).toContain("Synthetic lifecycle");
    expect(html).toContain("ui-lifecycle");
    expect(html).toContain("SYNTHETIC_JOURNAL");
    expect(html).not.toContain("must-never-be-rendered");
    expect(html).toContain("Проверить");
    expect(html).toContain("Отключить");
    expect(html).toContain("Изменить");
    expect(html).toContain("Удалить");
    expect(html).not.toMatch(/<button[^>]*disabled/);
  });

  it("отделяет пустой список от неподтверждённой готовности core", async () => {
    const html = await renderPanel([], false);

    expect(html).toContain("Подключений пока нет");
    expect(html).toContain("Откройте каталог, чтобы настроить подключение.");
    expect(html).not.toContain("Платформа работает без интеграций");
    expect(html).not.toContain("Подключения необязательны");
  });
});
