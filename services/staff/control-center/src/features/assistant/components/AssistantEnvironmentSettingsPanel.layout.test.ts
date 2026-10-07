import { readFileSync } from "node:fs";
import { reactive, type ComputedRef } from "vue";
import { describe, expect, it, vi } from "vitest";
import { createI18n } from "vue-i18n";
import { captureSetupState } from "@/test-utils/setup-harness";
import type { RuntimeResourceScope } from "@/features/runtime/resource-scope";

const platform = reactive<{
  bootstrap:
    | { organizationRef: string; assistant: { ref: string; name: string } }
    | undefined;
}>({ bootstrap: undefined });
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => platform,
}));
vi.mock("@/features/session/store", () => ({ useSessionStore: () => ({}) }));
vi.mock("vue-router", () => ({ useRoute: () => ({}), useRouter: () => ({}) }));
vi.mock("@/features/agents/detail/runtime-api", () => ({
  loadAgentRuntime: () => new Promise(() => {}),
}));
import Panel from "./AssistantEnvironmentSettingsPanel.vue";

const panel = readFileSync(
  new URL("./AssistantEnvironmentSettingsPanel.vue", import.meta.url),
  "utf8",
);
const runtime = readFileSync(
  new URL("../../agents/detail/AgentRuntimePanel.vue", import.meta.url),
  "utf8",
);

describe("Полная форма настройки помощника", () => {
  it("подписывает только текущего системного потребителя из метаданных его организации", async () => {
    platform.bootstrap = {
      organizationRef: "org_synthetic",
      assistant: { ref: "agt_synthetic", name: "Системный помощник" },
    };
    const props = reactive<{
      agentRef: string;
      canEdit: boolean;
      resourceScope: RuntimeResourceScope;
      imageCatalog: undefined;
    }>({
      agentRef: "agt_synthetic",
      canEdit: true,
      resourceScope: { kind: "ORGANIZATION", organizationRef: "org_synthetic" },
      imageCatalog: undefined,
    });
    const state = (await captureSetupState(
      Panel,
      (app) => app.use(createI18n({ legacy: false, locale: "ru" })),
      props,
    )) as {
      consumerNames: ComputedRef<Record<string, string>>;
    };
    expect(state.consumerNames.value).toEqual({
      agt_synthetic: "Системный помощник",
    });
    platform.bootstrap.assistant.name = "Новое имя помощника";
    expect(state.consumerNames.value).toEqual({
      agt_synthetic: "Новое имя помощника",
    });
    props.agentRef = "agt_unknown";
    expect(state.consumerNames.value).toEqual({});
    props.agentRef = "agt_synthetic";
    props.resourceScope = {
      kind: "PROJECT",
      projectRef: "prj_synthetic",
    };
    expect(state.consumerNames.value).toEqual({});
    props.resourceScope = {
      kind: "ORGANIZATION",
      organizationRef: "org_other",
    };
    expect(state.consumerNames.value).toEqual({});
    props.resourceScope = {
      kind: "ORGANIZATION",
      organizationRef: "org_synthetic",
    };
    platform.bootstrap = undefined;
    expect(state.consumerNames.value).toEqual({});
    expect(panel).toContain(':consumer-names="consumerNames"');
  });
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
