import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const environmentSource = readFileSync(
  new URL("./RuntimeEnvironmentEditorPage.vue", import.meta.url),
  "utf8",
);
const runtimeSource = readFileSync(
  new URL("../features/agents/detail/AgentRuntimePanel.vue", import.meta.url),
  "utf8",
);

describe("runtime editors layout", () => {
  it("разделяет постоянный draft lifecycle и контекстные действия вкладок", () => {
    const template = environmentSource.slice(
      environmentSource.indexOf("<template>"),
      environmentSource.indexOf("<style scoped>"),
    );

    expect(template).toContain('role="tablist"');
    expect(template).toContain('role="tab"');
    expect(template).toContain('role="tabpanel"');
    expect(template).not.toContain('class="environment-command-bar"');
    expect(template).toContain('$t("runtime.addVariable")');
    expect(template).not.toContain("openSection('IMAGE_TOOLS')");
    expect(template).not.toContain("openSection('POLICY')");
    expect(template).toContain("data-environment-variable-name");
    const values = template.indexOf("activeSection === 'VALUES'");
    const secrets = template.indexOf("activeSection === 'SECRETS'");
    expect(template.indexOf('@click="addValue"')).toBeGreaterThan(values);
    expect(template.indexOf('@click="addValue"')).toBeLessThan(secrets);
    expect(template.indexOf('@click="addSecret"')).toBeGreaterThan(secrets);
    expect(template.indexOf('@click="save"')).toBeLessThan(values);
  });

  it("показывает отдельный config.toml draft lifecycle и safe effective readback", () => {
    expect(runtimeSource).toContain('$t("runtime.saveDraft")');
    expect(runtimeSource).toContain('$t("runtime.validate")');
    expect(runtimeSource).toContain('$t("runtime.publishOverlay")');
    expect(runtimeSource).toContain(':model-value="view.safeEffectiveConfig"');
    expect(runtimeSource).toContain(":label=\"$t('runtime.effectiveConfig')\"");
    expect(runtimeSource).not.toContain('$t("agents.validate")');
    expect(runtimeSource).not.toContain('$t("agents.publish")');
  });
});
