import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const manual = readFileSync(
  new URL("../../../pages/IntegrationsPage.vue", import.meta.url),
  "utf8",
);
const assistant = readFileSync(
  new URL(
    "../../assistant/components/AssistantPlanEditor.vue",
    import.meta.url,
  ),
  "utf8",
);
const shared = readFileSync(
  new URL("./IntegrationPublicConfigurationFields.vue", import.meta.url),
  "utf8",
);

describe("публичные поля подключения", () => {
  it("использует одну форму в обычном редакторе и плане помощника", () => {
    expect(manual).toContain("<IntegrationPublicConfigurationFields");
    expect(assistant).toContain("<IntegrationPublicConfigurationFields");
    expect(shared).toContain("<IntegrationIntegerBounds");
    expect(shared).toContain("field.maximumLength");
    expect(shared).not.toContain("credentialValue");
  });
});
