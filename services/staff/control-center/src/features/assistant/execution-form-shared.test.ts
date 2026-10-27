import { ref } from "vue";
import { beforeEach, expect, it, vi } from "vitest";
import type { EditablePlanOperation } from "./model";
import type {
  Workflow,
  WorkflowInputField,
} from "@/shared/api/generated/openapi/types.gen";
import type { ExecutionTargetPickerOption } from "@/shared/api/execution-target-picker";

const mocks = vi.hoisted(() => ({ factory: vi.fn(), load: vi.fn() }));
vi.mock("@/shared/api/execution-target-picker", async (original) => ({
  ...(await original<typeof import("@/shared/api/execution-target-picker")>()),
  createExecutionTargetPickerLoader: mocks.factory,
}));
import {
  useAssistantExecutionSelection,
  synchronizeWorkflowInput,
  changeAssistantWorkflowInput,
  loadAssistantExecutionTargets,
  createAssistantExecutionFormActions,
} from "./execution-form-shared";

beforeEach(() => {
  vi.clearAllMocks();
  mocks.factory.mockReturnValue(mocks.load);
  mocks.load.mockResolvedValue({ items: [] });
});

it("реактивно отображает только точную цель и закрыто обрабатывает повреждённые параметры", () => {
  const operation = ref({
    parametersText: JSON.stringify({ targetType: "WORKFLOW", targetRef: "w1" }),
  } as EditablePlanOperation);
  const target = { ref: "w1", name: "Процесс", version: 2 } as Workflow;
  const selected = ref<ExecutionTargetPickerOption>({
    ref: "w1",
    title: "Процесс",
    targetType: "WORKFLOW",
    target,
  });
  const state = useAssistantExecutionSelection(() => operation.value, selected);
  expect(state.targetType.value).toBe("WORKFLOW");
  expect(state.workflow.value).toEqual(target);
  expect(state.selectedOption.value?.ref).toBe("w1");
  operation.value.parametersText = JSON.stringify({
    targetType: "AGENT",
    targetRef: "other",
  });
  expect(state.targetType.value).toBe("AGENT");
  expect(state.selectedOption.value).toBeUndefined();
  operation.value.parametersText = "[]";
  expect(state.parameter("targetRef")).toBeUndefined();
  expect(state.stringParameter("targetRef")).toBe("");
  expect(state.targetType.value).toBeUndefined();
});

it("синхронизирует только валидный изменённый workflow input с прежним порядком проверок", () => {
  const workflow = {} as Workflow;
  const prepare = vi.fn(() => ({
    value: { x: "a" } as Record<string, unknown>,
    problems: {} as Record<string, boolean>,
  }));
  const existing = vi.fn(() => ({ x: "a" }));
  const publish = vi.fn();
  synchronizeWorkflowInput(undefined, prepare, existing, publish);
  expect(prepare).not.toHaveBeenCalled();
  synchronizeWorkflowInput(workflow, prepare, existing, publish);
  expect(publish).not.toHaveBeenCalled();
  existing.mockReturnValue({ x: "b" });
  synchronizeWorkflowInput(workflow, prepare, existing, publish);
  expect(publish).toHaveBeenCalledExactlyOnceWith({ x: "a" });
  prepare.mockReturnValue({ value: {}, problems: { x: true } });
  existing.mockClear();
  synchronizeWorkflowInput(workflow, prepare, existing, publish);
  expect(existing).not.toHaveBeenCalled();
});

it("изменяет поле, сохраняет другие ошибки и выдаёт dirty после допустимого input", () => {
  const field = { key: "x" } as WorkflowInputField;
  const raw = ref<Record<string, string>>({ y: "old" });
  const invalid = ref(new Set(["x", "y"]));
  const events: string[] = [];
  const prepare = () => ({ value: { x: raw.value.x }, problems: {} });
  changeAssistantWorkflowInput(
    field,
    "new",
    raw,
    invalid,
    prepare,
    () => events.push("input"),
    () => events.push("dirty"),
  );
  expect(raw.value).toEqual({ x: "new", y: "old" });
  expect([...invalid.value]).toEqual(["y"]);
  expect(events).toEqual(["input", "dirty"]);
  changeAssistantWorkflowInput(
    field,
    "bad",
    raw,
    invalid,
    () => ({ value: {}, problems: { x: true } }),
    () => events.push("input"),
    () => events.push("dirty"),
  );
  expect(events).toEqual(["input", "dirty", "dirty"]);
});

it("не загружает цель без project/type и точно передаёт cursor, signal и размер", async () => {
  const signal = new AbortController().signal;
  await expect(
    loadAssistantExecutionTargets(undefined, "AGENT", "q", "c", signal),
  ).resolves.toEqual({ items: [] });
  await expect(
    loadAssistantExecutionTargets("p", undefined, "q", "c", signal),
  ).resolves.toEqual({ items: [] });
  expect(mocks.factory).not.toHaveBeenCalled();
  await loadAssistantExecutionTargets("p", "WORKFLOW", "q", "c", signal);
  expect(mocks.factory).toHaveBeenCalledExactlyOnceWith("p", "WORKFLOW");
  expect(mocks.load).toHaveBeenCalledExactlyOnceWith("q", "c", signal, 40);
});

it("действия читают актуальные project/type и передают события конкретной формы", async () => {
  const project = ref<string | undefined>("project-a");
  const kind = ref<"AGENT" | "WORKFLOW" | undefined>("AGENT");
  const raw = ref<Record<string, string>>({});
  const invalid = ref(new Set<string>());
  const events: string[] = [];
  const actions = createAssistantExecutionFormActions(
    () => project.value,
    kind,
    raw,
    invalid,
    () => ({ value: { x: raw.value.x }, problems: {} }),
    () => events.push("input"),
    () => events.push("dirty"),
  );
  project.value = "project-b";
  kind.value = "WORKFLOW";
  const signal = new AbortController().signal;
  await actions.load("query", "cursor", signal, 7);
  expect(mocks.factory).toHaveBeenCalledExactlyOnceWith(
    "project-b",
    "WORKFLOW",
  );
  expect(mocks.load).toHaveBeenCalledExactlyOnceWith(
    "query",
    "cursor",
    signal,
    7,
  );
  actions.change({ key: "x" } as WorkflowInputField, "value");
  expect(raw.value.x).toBe("value");
  expect(events).toEqual(["input", "dirty"]);
});
