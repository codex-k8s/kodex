import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const environmentSource = readFileSync(
  new URL("./RuntimeEnvironmentEditorPage.vue", import.meta.url),
  "utf8",
);
const environmentCatalogSource = readFileSync(
  new URL("./RuntimeEnvironmentsPage.vue", import.meta.url),
  "utf8",
);
const runtimeSource = readFileSync(
  new URL("../features/agents/detail/AgentRuntimePanel.vue", import.meta.url),
  "utf8",
);
const fieldListsSource = readFileSync(
  new URL(
    "../features/runtime/RuntimeEnvironmentFieldListsEditor.vue",
    import.meta.url,
  ),
  "utf8",
);
const publicationImpactSource = readFileSync(
  new URL(
    "../features/runtime/PublicationImpactSelection.vue",
    import.meta.url,
  ),
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
    expect(fieldListsSource).toContain('$t("runtime.addVariable")');
    expect(template).not.toContain("openSection('IMAGE_TOOLS')");
    expect(template).not.toContain("openSection('POLICY')");
    expect(fieldListsSource).toContain("data-environment-variable-name");
    const values = template.indexOf("activeSection === 'VALUES'");
    expect(fieldListsSource).toContain('@click="addValue"');
    expect(fieldListsSource).toContain('@click="addSecret"');
    expect(template.indexOf('@click="save"')).toBeLessThan(values);
    expect(template).toContain('name="runtime-environment-name"');
    expect(template).toContain('name="runtime-environment-description"');
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

  it("закрывает terminal publication plan после успешной публикации", () => {
    const published = environmentSource.indexOf(
      'if (!ref) throw new Error("Published environment reference is missing")',
    );
    const close = environmentSource.indexOf(
      "publicationPlan.value = undefined",
      published,
    );
    const navigate = environmentSource.indexOf("await router.replace({", close);

    expect(published).toBeGreaterThan(-1);
    expect(close).toBeGreaterThan(published);
    expect(navigate).toBeGreaterThan(close);
    expect(publicationImpactSource).toContain(
      'name="publication-impact-search"',
    );
  });

  it("не предлагает полноэкранный каталог окружений для короткого списка", () => {
    expect(environmentCatalogSource).toContain(
      'v-if="!expanded && (items.length > 6 || cursor)"',
    );
  });
});
