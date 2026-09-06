import { defineComponent, ref, type Ref, type SetupContext } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import type { AgentRuntimeConfigurationView } from "@/shared/api/generated/openapi/types.gen";
import {
  overlaySchemaFixture,
  catalogStatusFixture,
} from "@/test-utils/runtime-catalog-fixture";
import type { RuntimeForm } from "./runtime-input";
import type { ModelSelection } from "@/features/providers/model-catalog";
const api = vi.hoisted(() => ({
  loadAgentRuntime: vi.fn(),
  loadRuntimeCatalog: vi.fn(),
  saveAgentRuntime: vi.fn(),
  saveOverlayDraft: vi.fn(),
  changeOverlay: vi.fn(),
}));
vi.mock("./runtime-api", () => api);
vi.mock("@/shared/ui/unsaved-changes", () => ({ useUnsavedChanges: vi.fn() }));
vi.mock("vue-i18n", () => ({
  useI18n: () => ({ locale: ref("ru"), t: (key: string) => key }),
}));
import AgentRuntimePanel from "./AgentRuntimePanel.vue";

function runtimeView(
  model = "model-one",
  content = "original",
): AgentRuntimeConfigurationView {
  return {
    agentVersion: 3,
    overlaySchema: overlaySchemaFixture,
    configuration: {
      runtimeProfileRef: "profile-one",
      model,
      provider: "openai-codex",
      providerPolicy: {
        mode: "FIXED",
        accountCandidates: [{ accountRef: "account-one", weight: 1 }],
      },
    },
    publishedOverlay: { content },
  } as AgentRuntimeConfigurationView;
}
interface State {
  view: Ref<AgentRuntimeConfigurationView | undefined>;
  form: RuntimeForm;
  modelSelection: Ref<ModelSelection | undefined>;
  overlayContent: Ref<string>;
  modelAvailable: Ref<boolean>;
  providerAccountEligibility: Ref<string>;
  providerSubmissionAllowed: Ref<boolean>;
  busy: Ref<boolean>;
  load(): Promise<void>;
  reset(): void;
  updateOverlay(value: string): void;
  saveRuntime(): Promise<void>;
  saveOverlay(): Promise<void>;
  changeOverlay(action: "VALIDATE" | "PUBLISH"): Promise<void>;
}
async function panel(): Promise<State> {
  const setup = (
    AgentRuntimePanel as unknown as {
      setup: (
        props: { agentRef: string; canEdit: boolean },
        context: SetupContext,
      ) => Record<string, unknown>;
    }
  ).setup;
  const state = (await captureSetupState(
    defineComponent({
      setup(_props, context) {
        return setup({ agentRef: "agent-one", canEdit: true }, context);
      },
    }),
  )) as unknown as State;
  await vi.waitFor(() => expect(state.view.value).toBeDefined());
  return state;
}
beforeEach(() => {
  vi.clearAllMocks();
  api.loadAgentRuntime.mockResolvedValue(runtimeView());
  api.loadRuntimeCatalog.mockResolvedValue([]);
});
describe("runtime editor: сохранение независимых черновиков", () => {
  it("требует повторной проверки после изменения схемы перед публикацией", async () => {
    const state = await panel();
    const current = runtimeView();
    current.draftOverlay = {
      ref: "draft-one",
      version: 1,
      revision: 1,
      content: "original",
      state: "VALID",
      digest: "a".repeat(64),
      createdAt: "2026-09-05T00:00:00Z",
      validationMessages: [],
      diagnostics: [],
      schemaRevision: "old-schema",
      schemaDigest: "b".repeat(64),
    };
    state.view.value = current;
    await state.changeOverlay("PUBLISH");
    expect(api.changeOverlay).not.toHaveBeenCalled();
    current.draftOverlay.schemaRevision = current.overlaySchema.revision;
    current.draftOverlay.schemaDigest = current.overlaySchema.digest;
    state.view.value = { ...current };
    api.changeOverlay.mockResolvedValue(runtimeView());
    await state.changeOverlay("PUBLISH");
    expect(api.changeOverlay).toHaveBeenCalledWith("agent-one", "PUBLISH", 3);
  });
  it("сохраняет несохранённый overlay при публикации модели и блокирует ввод во время запроса", async () => {
    const state = await panel();
    state.updateOverlay("unsaved overlay");
    state.form.model = "model-two";
    state.modelAvailable.value = true;
    state.modelSelection.value = {
      model: state.form.model,
      providerDefinitionKey: "openai-codex",
      accounts: [
        {
          accountRef: "account-one",
          providerDefinitionKey: "openai-codex",
          catalogRevision: `mcat_${"a".repeat(64)}`,
          catalogDigest: "a".repeat(64),
          catalogStatus: catalogStatusFixture,
          model: {
            id: state.form.model,
            providerDefinitionKey: "openai-codex",
            available: true,
            eligibleProviderAccountRefs: ["account-one"],
            readinessBlockers: [],
            reasoningEfforts: ["low", "high"],
            defaultReasoningEffort: "high",
          },
        },
      ],
    };
    state.providerAccountEligibility.value = "READY";
    state.providerSubmissionAllowed.value = true;
    api.saveAgentRuntime.mockResolvedValue(runtimeView("model-two"));
    const saving = state.saveRuntime();
    expect(state.busy.value).toBe(true);
    state.updateOverlay("late input");
    await state.saveRuntime();
    expect(api.saveAgentRuntime).toHaveBeenCalledOnce();
    await saving;
    expect(state.overlayContent.value).toBe("unsaved overlay");
    expect(state.form.model).toBe("model-two");
    expect(state.busy.value).toBe(false);
  });
  it("сохраняет несохранённую модель при сохранении overlay", async () => {
    const state = await panel();
    state.form.model = "unsaved-model";
    state.updateOverlay("saved overlay");
    api.saveOverlayDraft.mockResolvedValue(
      runtimeView("model-one", "saved overlay"),
    );
    await state.saveOverlay();
    expect(state.form.model).toBe("unsaved-model");
    expect(state.overlayContent.value).toBe("saved overlay");
  });
  it("не возвращает прежнего агента после закрытия или смены контекста", async () => {
    const state = await panel();
    let finish!: (value: AgentRuntimeConfigurationView) => void;
    api.saveOverlayDraft.mockReturnValue(
      new Promise<AgentRuntimeConfigurationView>((resolve) => {
        finish = resolve;
      }),
    );
    state.updateOverlay("pending");
    const saving = state.saveOverlay();
    state.reset();
    finish(runtimeView("old", "late"));
    await saving;
    expect(state.view.value).toBeUndefined();
    expect(state.overlayContent.value).toBe("");
    expect(state.busy.value).toBe(false);
  });
});
