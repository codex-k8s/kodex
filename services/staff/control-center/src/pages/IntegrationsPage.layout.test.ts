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
  it("показывает готовность core по авторитетному API-флагу", () => {
    expect(pageSource).toContain(
      ':core-ready="platform.integrationCoreReady === true"',
    );
    expect(connectionsTemplate).toContain('v-if="coreReady"');
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
      't("integrationsRedesign.noConnectionsYet")',
    );
  });

  it("сохраняет компактный статус без зависимости от ширины карточек", () => {
    expect(connectionsSource).toContain(".core-readiness {");
    expect(connectionsSource).toContain("align-items: flex-start");
    expect(connectionsSource).toContain(".core-readiness > svg");
    expect(connectionsSource).toContain("flex: 0 0 auto");
  });
});
