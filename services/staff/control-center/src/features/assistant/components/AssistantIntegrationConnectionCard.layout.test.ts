import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantIntegrationConnectionCard.vue", import.meta.url),
  "utf8",
);

describe("AssistantIntegrationConnectionCard", () => {
  it("разрешает только точное подключение квитанции через авторитетный readback", () => {
    expect(source).toContain("assistantIntegrationConnectionTarget");
    expect(source).toContain("getIntegrationConnection");
    expect(source).toContain("next.ref !== value.connectionRef");
    expect(source).toContain('connection.value?.state === "TESTING"');
  });

  it("передаёт только ref в защищённую форму учётных данных", () => {
    expect(source).toContain("emit('prepareCredential', connection.ref)");
    expect(source).toContain("refreshToken");
    expect(source).not.toContain("credentialValue");
  });
});
