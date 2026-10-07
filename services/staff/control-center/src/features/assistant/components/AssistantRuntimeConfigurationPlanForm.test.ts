import { renderToString } from "@vue/server-renderer";
import {
  createRenderer,
  createSSRApp,
  defineComponent,
  h,
  reactive,
  ssrContextKey,
  type Ref,
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
function operation(mode?: unknown): EditablePlanOperation {
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
      ...(mode === undefined ? {} : { webSearchMode: mode }),
    },
    before: {
      ...owner,
      agentVersion: 3,
      runtimeProfilePin,
      webSearchMode: "cached",
    },
    after: { ...owner, runtimeProfilePin, webSearchMode: mode ?? "cached" },
  };
  const [editable] = editableOperations([input]);
  if (!editable) throw new Error("Synthetic operation is missing");
  return editable;
}
function mount(mode?: unknown, disabled = false) {
  const props = reactive({ operation: operation(mode), disabled });
  const events: Array<[string, unknown[]]> = [];
  let setup!: {
    changeWebSearch: (event: Event) => void;
    webSearchValid: Ref<boolean>;
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
  "показывает actual live и точное изменение cached→live (%s)",
  async (locale) => {
    i18n.global.locale.value = locale;
    const input = operation("live");
    const before = structuredClone(operationInputs([input]));
    const html = await renderToString(
      createSSRApp({
        render: () => h(Component, { operation: input, disabled: false }),
      }).use(i18n),
    );
    expect(html).toContain("-web-search");
    for (const mode of ["disabled", "cached", "indexed", "live"])
      expect(html).toContain(`value="${mode}"`);
    expect(html).toMatch(/<select[^>]*-web-search[^>]*value="live"/);
    expect(html).toContain(
      i18n.global.t("assistant.planEditor.webSearchReview", {
        before: i18n.global.t("assistant.planEditor.webSearchModes.cached"),
        after: i18n.global.t("assistant.planEditor.webSearchModes.live"),
      }),
    );
    expect(operationInputs([input])).toEqual(before);
  },
);

it("показывает Не менять для отсутствующего поля, а не выбирает disabled по умолчанию", async () => {
  const input = operation();
  const html = await renderToString(
    createSSRApp({
      render: () => h(Component, { operation: input, disabled: false }),
    }).use(i18n),
  );
  expect(html).toMatch(/<select[^>]*-web-search[^>]*value=""/);
  expect(html).toContain(
    i18n.global.t("assistant.planEditor.webSearchUnchanged"),
  );
  expect(operationInputs([input])[0]?.parameters).not.toHaveProperty(
    "webSearchMode",
  );
});

it("не меняет отсутствующий optional режим при открытии и изменении модели", () => {
  const { props, events } = mount();
  expect(events.filter(([name]) => name === "parameter")).toEqual([]);
  updateOperationParameter(props.operation, "model", "model_new");
  const saved = operationInputs([props.operation])[0];
  expect(saved?.parameters).not.toHaveProperty("webSearchMode");
  expect(saved?.before.webSearchMode).toBe("cached");
  expect(saved?.after.webSearchMode).toBe("cached");
});

it("сохраняет existing live при новой ревизии других настроек без эмита при открытии", () => {
  const { props, events } = mount("live");
  expect(events.filter(([name]) => name === "parameter")).toEqual([]);
  updateOperationParameter(props.operation, "model", "model_new");
  const saved = operationInputs([props.operation])[0];
  expect(saved?.parameters.webSearchMode).toBe("live");
  expect(saved?.after.webSearchMode).toBe("live");
  expect(saved?.before.webSearchMode).toBe("cached");
});

it.each(["disabled", "cached", "indexed", "live"])(
  "явный выбор %s сохраняет before и readonly pins в новой ревизии",
  (mode) => {
    const { props, setup } = mount();
    const original = operationInputs([props.operation])[0];
    setup.changeWebSearch(event(mode));
    const saved = operationInputs([props.operation])[0];
    expect(saved?.parameters.webSearchMode).toBe(mode);
    expect(saved?.after.webSearchMode).toBe(mode);
    expect(saved?.before).toEqual(original?.before);
    expect(saved?.target).toEqual(original?.target);
    expect(saved?.expectedVersion).toBe(original?.expectedVersion);
    expect(saved?.parameters.runtimeProfilePin).toEqual(
      original?.parameters.runtimeProfilePin,
    );
  },
);

it.each([null, "future-mode", "", false])(
  "не принимает неизвестный optional режим %s",
  (mode) => {
    const { setup } = mount(mode);
    expect(setup.webSearchValid.value).toBe(false);
  },
);

it("readonly или неподдерживаемый выбор не изменяет параметры", () => {
  const readonly = mount("live", true);
  readonly.setup.changeWebSearch(event("disabled"));
  expect(readonly.events.filter(([name]) => name === "parameter")).toEqual([]);
  const editable = mount();
  editable.setup.changeWebSearch(event("future-mode"));
  editable.setup.changeWebSearch(event(""));
  expect(editable.events.filter(([name]) => name === "parameter")).toEqual([]);
});
