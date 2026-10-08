import { readFileSync } from "node:fs";
import {
  createRenderer,
  defineComponent,
  h,
  nextTick,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { afterEach, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import type {
  Workflow,
  WorkflowInput,
} from "@/shared/api/generated/openapi/types.gen";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
vi.mock("vue-router", () => ({
  useRoute: () => ({
    params: { projectRef: "prj_fixture", workflowRef: "wfl_fixture" },
    query: {},
  }),
}));
vi.mock("@/shared/ui/unsaved-changes", () => ({ useUnsavedChanges: vi.fn() }));
vi.mock("@/features/platform/store", () => ({ usePlatformStore: () => store }));
vi.mock("@/features/agents/catalog/api", () => ({
  loadAgentCatalogPage: vi.fn(),
  loadAssignedAgent: (projectRef: string, ref: string) =>
    Promise.resolve({ ref, projectRef, name: "Исполнитель" }),
}));
vi.mock("@/features/agents/detail/TemplateSourceField.vue", () => ({
  default: defineComponent({ render: () => h("span") }),
}));
vi.mock("@/features/agents/detail/PromptTargetPreview.vue", () => ({
  default: defineComponent({ render: () => h("span") }),
}));
vi.mock("@/features/agents/detail/EffectiveCapabilityCatalog.vue", () => ({
  default: defineComponent({ render: () => h("span") }),
}));
import Component from "./WorkflowDetailPage.vue";

const workflow: Workflow = {
  ref: "wfl_fixture",
  version: 1,
  projectRef: "prj_fixture",
  name: "Процесс",
  purpose: "Проверка",
  coordinatorAgentRef: "agt_fixture",
  state: "DRAFT",
  launchReadiness: {
    allowedToSubmit: false,
    reason: "UNPUBLISHED",
    workflowVersion: 1,
    operationalState: "UNKNOWN",
    contextDigest: "a".repeat(64),
  },
  completionCriteria: "Исходный критерий",
  inputFields: [],
  steps: [
    {
      ref: "stage_fixture",
      position: 1,
      name: "Этап",
      purpose: "Задача",
      expectedResult: "Результат",
      agentRef: "agt_fixture",
      parallel: false,
      parallelGroup: 0,
      humanGate: false,
      timeoutSeconds: 600,
      gateDecisions: [],
      requiredCapabilityKeys: [],
    },
  ],
  cardSummary: {
    stageCount: 1,
    uniqueAgentCount: 1,
    parallelGroupCount: 0,
    hasHumanGate: false,
    activeRunCount: 0,
    pendingGateCount: 0,
  },
  validationMessages: [],
  updatedAt: "2026-10-07T00:00:00Z",
  nextActions: ["EDIT"],
};
const store = {
  workflows: { [workflow.ref]: workflow },
  loadWorkflow: vi.fn(() => Promise.resolve()),
  loadCapabilities: vi.fn(() => Promise.resolve()),
  saveWorkflow: vi.fn(
    (projectRef: string, input: Required<WorkflowInput>, workflow: Workflow) =>
      Promise.resolve({
        projectRef,
        completionCriteria: input.completionCriteria,
        workflowRef: workflow.ref,
      }),
  ),
};
const disposers: Array<() => void> = [];
afterEach(() => disposers.splice(0).forEach((dispose) => dispose()));
function mount() {
  let state!: {
    form: Required<WorkflowInput>;
    validCompletionText: Ref<boolean>;
    dirty: Ref<boolean>;
    save: () => Promise<void>;
  };
  const renderer = createRenderer<object, object>({
    insert() {},
    remove() {},
    patchProp() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
  });
  const app = renderer
    .createApp(
      defineComponent({
        setup(_, context) {
          state = (
            Component as unknown as {
              setup: (props: object, context: SetupContext) => typeof state;
            }
          ).setup({}, context);
          return () => null;
        },
      }),
    )
    .use(i18n);
  app.provide(ssrContextKey, {});
  app.mount({});
  disposers.push(() => app.unmount());
  return state;
}

it.each(["я", "🧭"])(
  "не отправляет save при 2001 Unicode-символе, сохраняя грязный текст (%s)",
  async (character) => {
    const state = mount();
    await vi.waitFor(() =>
      expect(state.form.completionCriteria).toBe(workflow.completionCriteria),
    );
    const value = character.repeat(2001);
    state.form.completionCriteria = value;
    await nextTick();
    expect(state.validCompletionText.value).toBe(false);
    expect(state.dirty.value).toBe(true);
    const before = JSON.stringify(state.form);
    await state.save();
    expect(store.saveWorkflow).not.toHaveBeenCalled();
    expect(JSON.stringify(state.form)).toBe(before);
    expect(state.form.completionCriteria).toBe(value);
    expect(state.dirty.value).toBe(true);
  },
);

it.each(["я", "🧭"])(
  "разрешает save ровно 2000 Unicode-символов без изменения текста (%s)",
  async (character) => {
    const state = mount();
    await vi.waitFor(() =>
      expect(state.form.completionCriteria).toBe(workflow.completionCriteria),
    );
    const value = character.repeat(2000);
    state.form.completionCriteria = value;
    await nextTick();
    expect(state.validCompletionText.value).toBe(true);
    await state.save();
    expect(store.saveWorkflow).toHaveBeenCalledTimes(1);
    expect(store.saveWorkflow.mock.calls[0]?.[0]).toBe(workflow.projectRef);
    expect(store.saveWorkflow.mock.calls[0]?.[2]).toEqual(workflow);
    // Снимок аргумента берётся в момент save, до последующего load формы.
    await expect(store.saveWorkflow.mock.results[0]?.value).resolves.toEqual({
      projectRef: workflow.projectRef,
      completionCriteria: value,
      workflowRef: workflow.ref,
    });
  },
);

it("связывает доступность кнопки сохранения с тем же ограничением", () => {
  const source = readFileSync(
    new URL("./WorkflowDetailPage.vue", import.meta.url),
    "utf8",
  );
  expect(source).toContain(
    ':disabled="busy || !validStepText || !validCompletionText"',
  );
});
