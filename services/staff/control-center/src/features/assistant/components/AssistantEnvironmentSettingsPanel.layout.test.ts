import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const panel = readFileSync(
  new URL("./AssistantEnvironmentSettingsPanel.vue", import.meta.url),
  "utf8",
);
const runtime = readFileSync(
  new URL("../../agents/detail/AgentRuntimePanel.vue", import.meta.url),
  "utf8",
);

describe("Полная форма настройки помощника", () => {
  it("сохраняет immutable snapshot вместо фиктивного образа и очистки инструментов/секретов", () => {
    expect(panel).toContain(
      "editableAssistantEnvironment(current, props.resourceScope)",
    );
    expect(panel).not.toContain("imgart_system_assistant");
    expect(panel).not.toContain("input.tools = []");
    expect(panel).not.toContain("input.secretBindings = []");
    expect(panel).not.toContain('item.field !== "imageArtifactRef"');
    expect(panel).toContain("validateEnvironmentInput(normalized.value)");
  });
  it("явно различает области ресурсов и не вызывает project API с пустым locator", () => {
    expect(panel).toContain("resourceScope: RuntimeResourceScope");
    expect(panel).toContain(':resource-scope="resourceScope"');
    expect(panel).toContain(':secret-catalog="secretCatalog"');
    expect(panel).toContain(':data-runtime-scope="resourceScope.kind"');
    expect(panel).toContain("imageCatalogUnavailable");
    expect(panel).toContain("secretCatalogUnavailable");
  });
  it("сначала показывает основные параметры, а ресурсы и TOML раскрываются отдельно", () => {
    expect(panel).toContain(
      '<details class="panel assistant-environment-settings__advanced">',
    );
    expect(panel).toContain("RuntimeEnvironmentImageToolsSelector");
    expect(runtime).toContain(':open="!advancedCollapsed"');
    expect(runtime).toContain("runtime-layout--compact");
  });
});
