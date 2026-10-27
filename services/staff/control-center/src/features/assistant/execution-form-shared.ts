import { computed, type Ref } from "vue";
import { operationParameter, type EditablePlanOperation } from "./model";
import {
  createExecutionTargetPickerLoader,
  selectedExecutionTargetOption,
  type ExecutionTargetPickerOption,
  type ExecutionTargetType,
} from "@/shared/api/execution-target-picker";
import type {
  Workflow,
  WorkflowInputField,
} from "@/shared/api/generated/openapi/types.gen";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";

// Общая реактивная проекция цели без доступа к сети и без проверки полномочий.
export function useAssistantExecutionSelection(
  operation: () => EditablePlanOperation,
  selected: Readonly<Ref<ExecutionTargetPickerOption | undefined>>,
) {
  function parameter(key: string): unknown {
    try {
      return operationParameter(operation(), key);
    } catch {
      return undefined;
    }
  }
  function stringParameter(key: string): string {
    const value = parameter(key);
    return typeof value === "string" ? value : "";
  }
  const targetType = computed<ExecutionTargetType | undefined>(() => {
    const value = stringParameter("targetType");
    return value === "AGENT" || value === "WORKFLOW" ? value : undefined;
  });
  const targetRef = computed(() => stringParameter("targetRef"));
  const workflow = computed(() =>
    selected.value?.targetType === "WORKFLOW"
      ? (selected.value.target as Workflow)
      : undefined,
  );
  const selectedOption = computed(() =>
    targetType.value && selected.value?.ref === targetRef.value
      ? selectedExecutionTargetOption(targetType.value, selected.value.target)
      : undefined,
  );
  return {
    parameter,
    stringParameter,
    targetType,
    targetRef,
    workflow,
    selectedOption,
  };
}

type PreparedWorkflowInput = {
  value: Record<string, unknown>;
  problems: Record<string, boolean>;
};

// Публикует только валидный изменённый ввод; порядок вычисления и событий задаёт вызывающая форма.
export function synchronizeWorkflowInput(
  current: Workflow | undefined,
  prepare: () => PreparedWorkflowInput,
  existingInput: () => Record<string, unknown> | undefined,
  publish: (value: Record<string, unknown>) => void,
): void {
  if (!current) return;
  const prepared = prepare();
  if (Object.keys(prepared.problems).length) return;
  const existing = existingInput();
  if (
    existing &&
    Object.keys(existing).length === Object.keys(prepared.value).length &&
    Object.entries(prepared.value).every(
      ([key, value]) => existing[key] === value,
    )
  )
    return;
  publish(prepared.value);
}

// Меняет только локальный ввод и недействительные ключи, затем передаёт валидный ввод и dirty.
export function changeAssistantWorkflowInput(
  field: WorkflowInputField,
  value: string,
  rawInput: Ref<Record<string, string>>,
  invalidKeys: Ref<Set<string>>,
  prepare: () => PreparedWorkflowInput,
  publish: (value: Record<string, unknown>) => void,
  dirty: () => void,
): void {
  rawInput.value = { ...rawInput.value, [field.key]: value };
  invalidKeys.value = new Set(
    [...invalidKeys.value].filter((key) => key !== field.key),
  );
  const prepared = prepare();
  if (!Object.keys(prepared.problems).length) publish(prepared.value);
  dirty();
}

// Пустая область или тип дают пустую страницу; иначе параметры и signal передаются исходному loader.
export function loadAssistantExecutionTargets(
  projectRef: string | undefined,
  targetType: ExecutionTargetType | undefined,
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 40,
): Promise<AsyncEntityOptionPage> {
  if (!projectRef || !targetType) return Promise.resolve({ items: [] });
  return createExecutionTargetPickerLoader(projectRef, targetType)(
    query,
    cursor,
    signal,
    pageSize,
  );
}

// Связывает действия каждой формы с её актуальной областью, локальными refs и событиями.
export function createAssistantExecutionFormActions(
  projectRef: () => string | undefined,
  targetType: Readonly<Ref<ExecutionTargetType | undefined>>,
  rawInput: Ref<Record<string, string>>,
  invalidKeys: Ref<Set<string>>,
  prepare: () => PreparedWorkflowInput,
  publish: (value: Record<string, unknown>) => void,
  dirty: () => void,
) {
  return {
    load: (
      query: string,
      cursor: string | undefined,
      signal: AbortSignal,
      pageSize = 40,
    ): Promise<AsyncEntityOptionPage> => {
      return loadAssistantExecutionTargets(
        projectRef(),
        targetType.value,
        query,
        cursor,
        signal,
        pageSize,
      );
    },
    change: (field: WorkflowInputField, value: string): void => {
      changeAssistantWorkflowInput(
        field,
        value,
        rawInput,
        invalidKeys,
        prepare,
        publish,
        dirty,
      );
    },
  };
}
