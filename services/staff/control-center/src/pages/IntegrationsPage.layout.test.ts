import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const pageSource = readFileSync(
  new URL("./IntegrationsPage.vue", import.meta.url),
  "utf8",
);
const connectionsSource = readFileSync(
  new URL(
    "../features/integrations/ui/IntegrationConnectionsPanel.vue",
    import.meta.url,
  ),
  "utf8",
);
const connectionsTemplate = connectionsSource.slice(
  connectionsSource.indexOf("<template>"),
  connectionsSource.indexOf("<style scoped>"),
);

describe("IntegrationsPage layout", () => {
  it("помечает неполный счётчик подключений при наличии курсора", () => {
    expect(pageSource).toContain(':connections-has-more="!!connectionCursor"');
    expect(connectionsSource).toContain(
      '? "integrationsRedesign.connectionsLoadedCount"',
    );
    expect(connectionsSource).toContain("{ count: connections.length }");
  });
  it("показывает готовность core по авторитетному API-флагу", () => {
    expect(pageSource).toContain(
      ':core-ready="platform.integrationCoreReady === true"',
    );
    expect(connectionsTemplate).toContain(
      'v-if="coreReady && !connections.length && !search?.trim()"',
    );
    expect(connectionsTemplate).toContain('class="core-readiness"');
    expect(connectionsTemplate).toContain(
      't("integrations.noConnectionsTitle")',
    );
    expect(connectionsTemplate).toContain('t("integrations.webOnlyReady")');
  });

  it("не связывает сообщение о готовности с пустотой списка", () => {
    const readiness = connectionsTemplate.indexOf('class="core-readiness"');
    const populated = connectionsTemplate.indexOf('v-if="connections.length"');

    expect(readiness).toBeGreaterThan(-1);
    expect(populated).toBeGreaterThan(readiness);
    expect(connectionsTemplate).toContain(
      '"integrationsRedesign.noConnectionsYet"',
    );
  });

  it("сохраняет компактный статус без зависимости от ширины карточек", () => {
    expect(connectionsSource).toContain(".core-readiness {");
    expect(connectionsSource).toContain("align-items: flex-start");
    expect(connectionsSource).toContain(".core-readiness > svg");
    expect(connectionsSource).toContain("flex: 0 0 auto");
  });

  it("показывает подключения компактной таблицей с курсором и доступными действиями", () => {
    expect(pageSource).toContain('itemSelector: ".connection-row"');
    expect(connectionsTemplate).toContain('<table class="connection-table">');
    expect(connectionsTemplate).toContain('class="connection-row"');
    expect(connectionsTemplate).toContain('ref="sentinel"');
    expect(connectionsTemplate).not.toContain('class="connection-card"');
    expect(connectionsSource).toContain(".connection-table-wrap {");
    expect(connectionsSource).toContain("overflow-x: auto;");
  });

  it("переносит полный статус учётных данных внутри своей ячейки без обрезки", () => {
    const rule = pageSource.match(
      /\.integration-page :deep\(\.connection-table td:nth-child\(4\) > \.status-badge\)\s*\{([^}]+)\}/,
    )?.[1];
    expect(rule).toBeDefined();
    expect(rule).toContain("box-sizing: border-box;");
    expect(rule).toContain("max-width: 100%;");
    expect(rule).toContain("white-space: normal;");
    expect(rule).toContain("overflow-wrap: anywhere;");
    expect(rule).not.toContain("overflow: hidden;");
    expect(rule).not.toContain("text-overflow: ellipsis;");
    expect(connectionsTemplate).toContain(
      ':label="credentialLabel(connection)"',
    );
    expect(connectionsSource).toContain("connection.credentialsHint ||");
    expect(connectionsSource).toContain("grant.enabled");
  });

  it("локализует сведения подключения", () => {
    expect(pageSource).toContain("integrations.detailsCredentialsTitle");
    expect(pageSource).toContain("integrations.publicConfiguration");
    expect(pageSource).toContain("integrations.noCapabilities");
    expect(pageSource).not.toContain("Учётные данные и проверка");
    expect(pageSource).not.toContain("Публичные настройки");
    expect(pageSource).not.toContain("Доступных возможностей пока нет.");
  });
});
