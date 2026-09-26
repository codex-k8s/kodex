import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./IntegrationsPage.vue", import.meta.url),
  "utf8",
);

describe("IntegrationsPage assistant credential handoff", () => {
  it("разрешает opaque ref через свежий owner readback и проверяет доступное действие", () => {
    expect(source).toContain("route.query.assistantCredentialRef");
    expect(source).toContain("platform.readConnection(ref)");
    expect(source).toContain("connection.ref !== ref");
    expect(source).toContain("canConfigureCredential(definition, connection)");
    expect(source).toContain("openCredential(connection, definition)");
  });

  it("очищает route и одноразовое значение секрета при закрытии", () => {
    expect(source).toContain("assistantCredentialRef: undefined");
    expect(source).toContain('credentialValue.value = ""');
  });
});
