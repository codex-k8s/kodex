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

it.each(["ru", "en"] as const)(
  "33 этапа оставляют один lazy editor, все заголовки видимы (%s)",
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
      expect(
        html.match(/class="workflow-step-disclosure__body"/gu),
      ).toHaveLength(1);
      expect(html.match(/class="test-picker"/gu)).toHaveLength(1);
      expect(html.match(/class="test-template"/gu)).toHaveLength(2);
      expect(html).toContain("Этап 33");
    } finally {
      i18n.global.locale.value = previous;
    }
  },
);

it("переключение этапа не меняет параметры и сохраняет полную проверку", async () => {
  const { props, state, events } = mount();
  await vi.waitFor(() => expect(state.valid.value).toBe(true));
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
