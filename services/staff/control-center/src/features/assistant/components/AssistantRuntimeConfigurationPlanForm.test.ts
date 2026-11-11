import { createNoopRenderer } from "@/test-utils/create-noop-renderer";
import { renderToString } from "@vue/server-renderer";
import {
  createSSRApp,
  defineComponent,
  h,
  reactive,
  ssrContextKey,
  type SetupContext,
} from "vue";
import { afterEach, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import {
  editableOperations,
  operationInputs,
  updateOperationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import type { AssistantPlanOperationInput } from "@/shared/api/generated/openapi/types.gen";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
vi.mock("@/features/agents/detail/runtime-api", () => ({
  loadRuntimeCatalog: vi.fn().mockResolvedValue([]),
}));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => ({ bootstrap: { organizationRef: "org_synthetic" } }),
}));
vi.mock("@/features/providers/ProviderModelSelector.vue", () => ({
  default: defineComponent({ render: () => h("span") }),
}));
vi.mock("@/features/providers", () => ({
  ProviderAccountSelector: defineComponent({ render: () => h("span") }),
}));
import Component from "./AssistantRuntimeConfigurationPlanForm.vue";

const disposers: Array<() => void> = [];
afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose();
});
function operation(ordinary = false): EditablePlanOperation {
  const owner = {
    agentRef: "agt_assistant",
    assistantScope: "SYSTEM",
    scopeKind: "ORGANIZATION",
    organizationRef: "org_synthetic",
    projectRef: "",
    assistantProfileRef: "",
  };
  const runtimeProfilePin = {
    ref: "runtime_synthetic",
    version: 1,
    runtimeRevision: "revision_synthetic",
  };
  const input: AssistantPlanOperationInput = {
    ref: "op_runtime",
    type: "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION",
    action: "UPDATE",
    title: "Настройки модели",
    summary: "Обновить настройки",
    selected: true,
    permitted: true,
    validationProblems: [],
    expectedVersion: 3,
    target: { kind: "AGENT", ref: owner.agentRef, name: "Kodex", version: 3 },
    parameters: {
      ...owner,
      runtimeProfilePin,
      runtimeProfileRef: runtimeProfilePin.ref,
      model: "model_current",
      reasoningEffort: "medium",
      providerPolicyMode: "FIXED",
      providerAccounts: [{ accountRef: "pacc_synthetic", weight: 1 }],
    },
    before: {
      ...owner,
      agentVersion: 3,
      runtimeProfilePin,
    },
    after: { ...owner, runtimeProfilePin },
  };
  if (ordinary) {
    input.type = "PREPARE_AGENT_RUNTIME_CONFIGURATION";
    for (const fields of [input.parameters, input.before, input.after]) {
      delete fields.assistantScope;
      delete fields.assistantProfileRef;
      fields.scopeKind = "PROJECT";
      fields.projectRef = "prj_synthetic";
      fields.targetKind = "AGENT";
    }
  }
  const [editable] = editableOperations([input]);
  if (!editable) throw new Error("Synthetic operation is missing");
  return editable;
}
function mount(disabled = false, ordinary = false) {
  const props = reactive({ operation: operation(ordinary), disabled });
  const events: Array<[string, unknown[]]> = [];
  let setup!: {
    change: (key: string, event: Event) => void;
  };
  const renderer = createNoopRenderer();
  const app = renderer
    .createApp(
      defineComponent({
        setup(_, context) {
          setup = (
            Component as unknown as {
              setup: (props: object, context: SetupContext) => typeof setup;
            }
          ).setup(props, {
            ...context,
            emit: (name: string, ...values: unknown[]) => {
              events.push([name, values]);
              if (name === "parameter" && typeof values[0] === "string")
                updateOperationParameter(props.operation, values[0], values[1]);
            },
          });
          return () => null;
        },
      }),
    )
    .use(i18n);
  app.provide(ssrContextKey, {});
  app.mount({});
  disposers.push(() => app.unmount());
  return { props, events, setup };
}
function event(mode: string): Event {
  const value = new Event("change");
  Object.defineProperty(value, "target", { value: { value: mode } });
  return value;
}

it.each(["ru", "en"] as const)(
  "показывает native runtime без удалённых настроек поиска (%s)",
  async (locale) => {
    i18n.global.locale.value = locale;
    const input = operation();
    const before = structuredClone(operationInputs([input]));
    const html = await renderToString(
      createSSRApp({
        render: () => h(Component, { operation: input, disabled: false }),
      }).use(i18n),
    );
    expect(html).not.toContain("-web-search");
    expect(html).toContain("-effort");
    expect(operationInputs([input])).toEqual(before);
  },
);
it("сохраняет owner pins при изменении модели и не меняет их при открытии", () => {
  const { props, events, setup } = mount();
  const original = operationInputs([props.operation])[0];
  expect(events.filter(([name]) => name === "parameter")).toEqual([]);
  setup.change("model", event("model_new"));
  const saved = operationInputs([props.operation])[0];
  expect(saved?.parameters.model).toBe("model_new");
  expect(saved?.before).toEqual(original?.before);
  expect(saved?.target).toEqual(original?.target);
  expect(saved?.expectedVersion).toEqual(original?.expectedVersion);
  expect(saved?.parameters.runtimeProfilePin).toEqual(
    original?.parameters.runtimeProfilePin,
  );
  expect(saved?.parameters).not.toHaveProperty("webSearchMode");
});
it("readonly и удалённое поле не меняют параметры", () => {
  const readonly = mount(true);
  readonly.setup.change("model", event("model_new"));
  expect(readonly.events.filter(([name]) => name === "parameter")).toEqual([]);
  const editable = mount();
  editable.setup.change("webSearchMode", event("live"));
  expect(editable.events.filter(([name]) => name === "parameter")).toEqual([]);
});
it("ordinary-план сохраняет project boundary при изменении модели", () => {
  const { props, setup } = mount(false, true);
  const original = operationInputs([props.operation])[0];
  setup.change("model", event("model_new"));
  const saved = operationInputs([props.operation])[0];
  expect(saved?.type).toBe("PREPARE_AGENT_RUNTIME_CONFIGURATION");
  expect(saved?.parameters.model).toBe("model_new");
  expect(saved?.before).toEqual(original?.before);
  expect(saved?.parameters.targetKind).toBe("AGENT");
  expect(saved?.parameters.projectRef).toBe("prj_synthetic");
  expect(saved?.parameters.assistantScope).toBeUndefined();
});
