import { renderToString } from "@vue/server-renderer";
import {
  createRenderer,
  createSSRApp,
  defineComponent,
  h,
  nextTick,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { afterEach, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import { resolveConfirmation } from "@/shared/ui/confirmation";
import {
  editableOperations,
  operationInputs,
  updateOperationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import type { AssistantPlanOperationInput } from "@/shared/api/generated/openapi/types.gen";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/features/agents/catalog/api", () => ({
  loadAgentCatalogPage: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  getAgent: vi.fn(({ path }: { path: { agentRef: string } }) =>
    Promise.resolve({
      data: {
        ref: path.agentRef,
        name: "Исполнитель",
        projectRef: "prj_fixture",
        system: false,
      },
      response: new Response(null, { status: 200 }),
    }),
  ),
}));
vi.mock("@/features/workflows/WorkflowOverviewFields.vue", () => ({
  default: defineComponent({ render: () => h("span") }),
}));
vi.mock("@/features/agents/detail/TemplateSourceField.vue", () => ({
  default: defineComponent({
    render: () => h("span", { class: "test-template" }),
  }),
}));
vi.mock("@/shared/ui/AsyncEntityPicker.vue", () => ({
  default: defineComponent({
    render: () => h("span", { class: "test-picker" }),
  }),
}));
vi.mock("@/features/agents/detail/EffectiveCapabilityCatalog.vue", () => ({
  default: defineComponent({
    render: () => h("span", { class: "test-capabilities" }),
  }),
}));
import Component from "./AssistantWorkflowPlanForm.vue";
import WorkflowStepDisclosure from "@/features/workflows/WorkflowStepDisclosure.vue";

function operation(count = 33): EditablePlanOperation {
  const input: AssistantPlanOperationInput = {
    ref: "op_workflow",
    type: "CREATE_WORKFLOW",
    action: "CREATE",
    title: "Процесс",
    summary: "Процесс",
    selected: true,
    permitted: true,
    validationProblems: [],
    target: { kind: "WORKFLOW", name: "Процесс" },
    parameters: {
      projectRef: "prj_fixture",
      name: "Процесс",
      purpose: "Проверка",
      coordinatorAgentRef: "agt_fixture",
      steps: Array.from({ length: count }, (_, index) => ({
        name: `Этап ${String(index + 1)}`,
        purpose: `Задача ${String(index + 1)}`,
        agentRef: "agt_fixture",
        parallel: index % 2 === 0,
        parallelGroup: 0,
        timeoutSeconds: 1800,
        expectedResult: "Результат",
        humanGate: false,
        gateDecisions: [],
        requiredCapabilityKeys: [],
      })),
    },
    before: {},
    after: {},
  };
  const result = editableOperations([input])[0];
  if (!result) throw new Error("Synthetic workflow operation missing");
  return result;
}
function stepsOf(
  operation: EditablePlanOperation,
): Array<Record<string, unknown>> {
  const input = operationInputs([operation])[0];
  if (!input || !Array.isArray(input.parameters.steps))
    throw new Error("Synthetic workflow steps missing");
  return input.parameters.steps as Array<Record<string, unknown>>;
}
function stepAt(
  steps: Array<Record<string, unknown>>,
  index: number,
): Record<string, unknown> {
  const step = steps[index];
  if (!step) throw new Error("Synthetic workflow step missing");
  return step;
}
const disposers: Array<() => void> = [];
afterEach(() => {
  resolveConfirmation(false);
  disposers.splice(0).forEach((dispose) => dispose());
});
function mount(count = 33) {
  const props = reactive({
    operation: operation(count),
    projectRef: "prj_fixture",
    disabled: false,
  });
  const events: Array<[string, unknown[]]> = [];
  let state!: {
    openStepIndex: Ref<number | undefined>;
    toggleStep: (index: number, event: Event) => void;
    addStep: () => void;
    removeStep: (index: number) => void;
    valid: Ref<boolean>;
    validStep: (step: Record<string, unknown>, pending?: boolean) => boolean;
    changeStep: (index: number, key: string, value: unknown) => void;
    changeMode: (event: Event) => Promise<void>;
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
          ).setup(props, {
            ...context,
            emit: (name: string, ...values: unknown[]) => {
              events.push([name, values]);
              if (name === "parameter")
                updateOperationParameter(
                  props.operation,
                  String(values[0]),
                  values[1],
                );
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
  return { props, events, state };
}
function toggle(open: boolean): Event {
  const event = new Event("toggle");
  Object.defineProperty(event, "currentTarget", { value: { open } });
  return event;
}

it("смена режима требует подтверждения и не применяется после отзыва редактирования", async () => {
  const { props, state } = mount(2);
  await vi.waitFor(() => expect(state.valid.value).toBe(true));
  const change = () =>
    state.changeMode({
      target: { value: "SERVER_DAG_V1" },
    } as unknown as Event);
  const before = operationInputs([props.operation]);
  const rejected = change();
  resolveConfirmation(false);
  await rejected;
  expect(operationInputs([props.operation])).toEqual(before);
  const stale = change();
  props.disabled = true;
  resolveConfirmation(true);
  await stale;
  expect(operationInputs([props.operation])).toEqual(before);
  props.disabled = false;
  const accepted = change();
  resolveConfirmation(true);
  await accepted;
  expect(operationInputs([props.operation])[0]?.parameters.executionMode).toBe(
    "SERVER_DAG_V1",
  );
  expect(stepAt(stepsOf(props.operation), 0)).toMatchObject({
    key: "step-1",
    dependsOn: [],
    resultRequirements: [],
  });
  expect(state.valid.value).toBe(false);
});

it.each([null, "", "AUTOMATIC"])(
  "не превращает недопустимый режим %s в прежний",
  async (mode) => {
    const { props, state } = mount(2);
    await vi.waitFor(() => expect(state.valid.value).toBe(true));
    updateOperationParameter(props.operation, "executionMode", mode);
    await nextTick();
    expect(state.valid.value).toBe(false);
  },
);

it("план DAG сохраняет условия и TTL; изменение зависимости закрывает применение", async () => {
  const { props, state } = mount(2);
  updateOperationParameter(props.operation, "executionMode", "SERVER_DAG_V1");
  updateOperationParameter(props.operation, "maxConcurrency", 2);
  updateOperationParameter(
    props.operation,
    "steps",
    stepsOf(props.operation).map((step, index) => ({
      ...step,
      key: index === 0 ? "code" : "review",
      agentRef: index === 0 ? "agt_developer" : "agt_reviewer",
      dependsOn: index === 0 ? [] : ["code"],
      resultRequirements:
        index === 0
          ? [
              {
                key: "pr",
                kind: "PULL_REQUEST",
                repository: "codex-k8s/test-repo",
              },
            ]
          : [{ key: "review", kind: "REVIEW", subjectStepKey: "code" }],
      ...(index === 1
        ? {
            humanGate: true,
            gateDecisions: ["APPROVE"],
            approvalTimeoutSeconds: 90,
          }
        : {}),
    })),
  );
  await vi.waitFor(() => expect(state.valid.value).toBe(true));
  state.changeStep(1, "name", "Независимое ревью");
  expect(stepAt(stepsOf(props.operation), 1)).toMatchObject({
    dependsOn: ["code"],
    resultRequirements: [
      { key: "review", kind: "REVIEW", subjectStepKey: "code" },
    ],
    approvalTimeoutSeconds: 90,
  });
  state.changeStep(0, "key", "renamed");
  await nextTick();
  expect(state.valid.value).toBe(false);
  expect(stepAt(stepsOf(props.operation), 1).dependsOn).toEqual(["code"]);
  state.changeStep(0, "key", "code");
  await nextTick();
  expect(state.valid.value).toBe(true);
  state.changeStep(1, "agentRef", "agt_developer");
  await vi.waitFor(() => expect(state.valid.value).toBe(false));
});

it.each(["ru", "en"] as const)(
  "33 этапа сначала показывают только заголовки без лишних редакторов (%s)",
  async (locale) => {
    const previous = i18n.global.locale.value;
    i18n.global.locale.value = locale;
    try {
      const html = await renderToString(
        createSSRApp(Component, {
          operation: operation(),
          projectRef: "prj_fixture",
          disabled: false,
        }).use(i18n),
      );
      expect(
        html.match(/class="workflow-step-disclosure__summary"/gu),
      ).toHaveLength(33);
      expect(html).not.toContain('class="workflow-step-disclosure__body"');
      expect(html).not.toContain('class="test-picker"');
      expect(html).not.toContain('class="test-template"');
      expect(html).toContain("Этап 33");
    } finally {
      i18n.global.locale.value = previous;
    }
  },
);

it("переключение этапа не меняет параметры и сохраняет полную проверку", async () => {
  const { props, state, events } = mount();
  await vi.waitFor(() => expect(state.valid.value).toBe(true));
  expect(state.openStepIndex.value).toBeUndefined();
  const original = operationInputs([props.operation]);
  state.toggleStep(20, toggle(true));
  state.toggleStep(0, toggle(false));
  expect(state.openStepIndex.value).toBe(20);
  expect(operationInputs([props.operation])).toEqual(original);
  expect(events.filter(([name]) => name === "parameter")).toHaveLength(0);
  const steps = stepsOf(props.operation);
  const changed = steps.map((step, index) =>
    index === 32 ? { ...step, purpose: "" } : step,
  );
  updateOperationParameter(props.operation, "steps", changed);
  await nextTick();
  expect(state.valid.value).toBe(false);
  expect(state.validStep(stepAt(changed, 32), true)).toBe(false);
});

it("добавление открывает новый этап, удаление сохраняет соседние значения", async () => {
  const { props, state } = mount(3);
  await vi.waitFor(() => expect(state.valid.value).toBe(true));
  state.addStep();
  expect(state.openStepIndex.value).toBe(3);
  const added = stepsOf(props.operation);
  expect(added).toHaveLength(4);
  expect(stepAt(added, 3).name).toBe("");
  state.removeStep(1);
  expect(state.openStepIndex.value).toBe(2);
  const remaining = stepsOf(props.operation);
  expect(remaining).toEqual([added[0], added[2], added[3]]);
  props.disabled = true;
  state.removeStep(0);
  expect(stepsOf(props.operation)).toEqual(remaining);
});

it("ожидание каталога нейтрально, но неполный этап сразу отмечен", () => {
  const { props, state } = mount(2);
  const steps = stepsOf(props.operation);
  expect(state.valid.value).toBe(false);
  expect(state.validStep(stepAt(steps, 0), true)).toBe(true);
  expect(state.validStep({ ...steps[1], purpose: "" }, true)).toBe(false);
});

it("сохраняет срок согласования при правках и удаляет его при отключении Human Gate", async () => {
  const { props, state } = mount(2);
  await vi.waitFor(() => expect(state.valid.value).toBe(true));
  state.changeStep(0, "humanGate", true);
  state.changeStep(0, "gateDecisions", ["APPROVE"]);
  state.changeStep(0, "approvalTimeoutSeconds", 90);
  state.changeStep(0, "name", "Проверка требований");
  await nextTick();
  expect(state.valid.value).toBe(true);
  expect(stepAt(stepsOf(props.operation), 0).approvalTimeoutSeconds).toBe(90);
  state.changeStep(0, "approvalTimeoutSeconds", undefined);
  expect(stepAt(stepsOf(props.operation), 0)).not.toHaveProperty(
    "approvalTimeoutSeconds",
  );
  state.changeStep(0, "approvalTimeoutSeconds", 604800);
  state.changeStep(0, "humanGate", false);
  expect(stepAt(stepsOf(props.operation), 0)).not.toHaveProperty(
    "approvalTimeoutSeconds",
  );
  props.disabled = true;
  state.changeStep(0, "approvalTimeoutSeconds", 5);
  expect(stepAt(stepsOf(props.operation), 0)).not.toHaveProperty(
    "approvalTimeoutSeconds",
  );
});

it.each([0, -1, 604801, 1.5, "90", null])(
  "отклоняет недопустимый срок согласования %s",
  (timeout) => {
    const { props, state } = mount(2);
    const step = {
      ...stepAt(stepsOf(props.operation), 0),
      humanGate: true,
      gateDecisions: ["APPROVE"],
      approvalTimeoutSeconds: timeout,
    };
    expect(state.validStep(step, true)).toBe(false);
  },
);

it.each(["я", "🧭"])(
  "проверяет критерий завершения по Unicode-символам на границе 2000/2001 (%s)",
  async (character) => {
    const { props, state, events } = mount(2);
    await vi.waitFor(() => expect(state.valid.value).toBe(true));
    const value = character.repeat(2000);
    updateOperationParameter(props.operation, "completionCriteria", value);
    await nextTick();
    expect(state.valid.value).toBe(true);
    updateOperationParameter(
      props.operation,
      "completionCriteria",
      `${value}я`,
    );
    await nextTick();
    expect(state.valid.value).toBe(false);
    expect(
      operationInputs([props.operation])[0]?.parameters.completionCriteria,
    ).toBe(`${value}я`);
    expect(events.filter(([name]) => name === "parameter")).toHaveLength(0);
  },
);

it("общий свернутый шаг не монтирует slot, но показывает ошибку и gate", async () => {
  let mounted = 0;
  const app = createSSRApp(
    defineComponent({
      render: () =>
        h(
          WorkflowStepDisclosure,
          {
            open: false,
            number: 33,
            name: "Последний этап",
            agentTitle: "Рецензент",
            parallel: true,
            parallelGroup: 3,
            humanGate: true,
            needsAttention: true,
          },
          {
            default: () => {
              mounted++;
              return h("span", "Editor");
            },
          },
        ),
    }),
  ).use(i18n);
  const html = await renderToString(app);
  expect(mounted).toBe(0);
  expect(html).toContain("Последний этап");
  expect(html).toContain("Рецензент");
  expect(html).toContain("Требуется решение человека");
  expect(html).toContain("Проверьте поля этапа");
  expect(html).not.toContain("Editor");
});
