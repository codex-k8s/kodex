<script setup lang="ts">
import { computed, ref, useId, watch } from "vue";

import {
  operationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import { prepareAssistantWorkflowInput } from "@/features/assistant/run-input";
import {
  createExecutionTargetPickerLoader,
  isEligibleAgent,
  isEligibleWorkflow,
  selectedExecutionTargetOption,
  toExecutionTargetOption,
  type ExecutionTargetPickerOption,
  type ExecutionTargetType,
} from "@/shared/api/execution-target-picker";
import { requestSignal } from "@/shared/api/client";
import { getAgent, getWorkflow } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  Workflow,
  WorkflowInputField,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";

const props = defineProps<{
  operation: EditablePlanOperation;
  projectRef?: string;
  disabled: boolean;
}>();
const fieldPrefix = `assistant-launch-${useId()}`;
const workflowInputName = (key: string) => `${fieldPrefix}-input-${key}`;
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: string | Record<string, unknown>];
  target: [
    value: { kind: string; ref?: string; name: string; version?: number },
  ];
}>();
const selected = ref<ExecutionTargetPickerOption>();
const targetProblem = ref(false);
const rawInput = ref<Record<string, string>>({});
const invalidInitialInputKeys = ref<Set<string>>(new Set());

function parameter(key: string): unknown {
  try {
    return operationParameter(props.operation, key);
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

watch(
  () => JSON.stringify([props.operation.value.ref, parameter("input")]),
  () => {
    const input = parameter("input");
    invalidInitialInputKeys.value = new Set(
      typeof input === "object" && input !== null && !Array.isArray(input)
        ? Object.entries(input)
            .filter(
              ([, value]) =>
                value !== null &&
                typeof value !== "string" &&
                typeof value !== "number" &&
                typeof value !== "boolean",
            )
            .map(([key]) => key)
        : [],
    );
    rawInput.value =
      typeof input === "object" && input !== null && !Array.isArray(input)
        ? Object.fromEntries(
            Object.entries(input).map(([key, value]) => [key, String(value)]),
          )
        : {};
  },
  { immediate: true },
);

watch(
  [() => props.projectRef, targetType, targetRef] as const,
  ([projectRef, type, ref], _previous, onCleanup) => {
    if (
      selected.value?.ref === ref &&
      selected.value.targetType === type &&
      selected.value.target.projectRef === projectRef
    )
      return;
    selected.value = undefined;
    targetProblem.value = false;
    if (!projectRef || !type || !ref) return;
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    void (async () => {
      try {
        const target =
          type === "AGENT"
            ? (
                await unwrap(
                  getAgent({
                    path: { agentRef: ref },
                    signal: requestSignal(controller.signal),
                  }),
                )
              ).data
            : (
                await unwrap(
                  getWorkflow({
                    path: { workflowRef: ref },
                    signal: requestSignal(controller.signal),
                  }),
                )
              ).data;
        if (controller.signal.aborted) return;
        if (
          target.ref !== ref ||
          target.projectRef !== projectRef ||
          (type === "AGENT"
            ? !isEligibleAgent(target as Agent)
            : !isEligibleWorkflow(target as Workflow))
        )
          throw new Error("Assistant run target readback mismatch");
        selected.value = toExecutionTargetOption(type, target);
      } catch {
        if (!controller.signal.aborted) targetProblem.value = true;
      }
    })();
  },
  { immediate: true },
);

function inputObject(): Record<string, unknown> | undefined {
  const value = parameter("input");
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function preparedWorkflowInput(): {
  value: Record<string, unknown>;
  problems: Record<string, boolean>;
} {
  const prepared = prepareAssistantWorkflowInput(
    workflow.value?.inputFields ?? [],
    rawInput.value,
  );
  for (const key of invalidInitialInputKeys.value)
    prepared.problems[key] = true;
  return prepared;
}

watch(workflow, (current) => {
  if (!current) return;
  const prepared = preparedWorkflowInput();
  if (Object.keys(prepared.problems).length) return;
  const existing = inputObject();
  if (
    existing &&
    Object.keys(existing).length === Object.keys(prepared.value).length &&
    Object.entries(prepared.value).every(
      ([key, value]) => existing[key] === value,
    )
  )
    return;
  emit("parameter", "input", prepared.value);
  emit("dirty");
});

const inputProblems = computed(() => preparedWorkflowInput().problems);
const valid = computed(() => {
  if (
    !props.projectRef ||
    !targetType.value ||
    !targetRef.value ||
    selected.value?.ref !== targetRef.value ||
    selected.value.targetType !== targetType.value ||
    selected.value.target.projectRef !== props.projectRef ||
    !stringParameter("title").trim() ||
    !stringParameter("task").trim() ||
    !inputObject()
  )
    return false;
  return (
    targetType.value !== "WORKFLOW" || !Object.keys(inputProblems.value).length
  );
});
watch(valid, (value) => emit("valid", value), { immediate: true });

function changed(key: string, value: string): void {
  emit("parameter", key, value);
  emit("dirty");
}
function setTargetType(event: Event): void {
  const value = (event.target as HTMLSelectElement).value;
  if (value !== "AGENT" && value !== "WORKFLOW") return;
  emit("parameter", "targetType", value);
  emit("parameter", "targetRef", "");
  emit("parameter", "input", {});
  emit("target", { kind: value, name: "" });
  selected.value = undefined;
  rawInput.value = {};
  emit("dirty");
}
function setTargetRef(value: string | null | readonly string[]): void {
  if (value !== null && typeof value !== "string") return;
  const ref = value ?? "";
  emit("parameter", "targetRef", ref);
  if (!ref) {
    selected.value = undefined;
    emit("target", { kind: targetType.value ?? "AGENT", name: "" });
  }
  emit("dirty");
}
function selectTarget(option: ExecutionTargetPickerOption): void {
  if (
    option.targetType !== targetType.value ||
    option.target.projectRef !== props.projectRef ||
    (option.targetType === "AGENT"
      ? !isEligibleAgent(option.target as Agent)
      : !isEligibleWorkflow(option.target as Workflow))
  )
    return;
  selected.value = option;
  emit("target", {
    kind: option.targetType,
    ref: option.ref,
    name: option.title,
    version: option.target.version,
  });
  emit("parameter", "targetRef", option.ref);
  emit("parameter", "input", {});
  rawInput.value = {};
  emit("dirty");
}
function loadPage(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 40,
): Promise<AsyncEntityOptionPage> {
  if (!props.projectRef || !targetType.value)
    return Promise.resolve({ items: [] });
  return createExecutionTargetPickerLoader(props.projectRef, targetType.value)(
    query,
    cursor,
    signal,
    pageSize,
  );
}
function setWorkflowInput(field: WorkflowInputField, value: string): void {
  rawInput.value = { ...rawInput.value, [field.key]: value };
  invalidInitialInputKeys.value = new Set(
    [...invalidInitialInputKeys.value].filter((key) => key !== field.key),
  );
  const prepared = preparedWorkflowInput();
  if (!Object.keys(prepared.problems).length)
    emit("parameter", "input", prepared.value);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-launch-form">
    <label class="field">
      <span>{{ $t("assistant.planEditor.runTitle") }}</span>
      <input
        :value="stringParameter('title')"
        :id="`${fieldPrefix}-title`"
        :name="`${fieldPrefix}-title`"
        maxlength="240"
        :disabled="disabled"
        @input="changed('title', ($event.target as HTMLInputElement).value)"
      />
    </label>
    <label class="field">
      <span>{{ $t("assistant.planEditor.runTargetType") }}</span>
      <select
        :value="targetType"
        :id="`${fieldPrefix}-target-type`"
        :name="`${fieldPrefix}-target-type`"
        :disabled="disabled"
        @change="setTargetType"
      >
        <option value="AGENT">{{ $t("assistant.planEditor.runAgent") }}</option>
        <option value="WORKFLOW">
          {{ $t("assistant.planEditor.runWorkflow") }}
        </option>
      </select>
    </label>
    <div class="field">
      <span>{{ $t("assistant.planEditor.runTarget") }}</span>
      <AsyncEntityPicker
        :key="targetType"
        :model-value="targetRef"
        :selected="selectedOption"
        :load-page="loadPage"
        :context-key="`${projectRef}:${targetType}`"
        :trigger-label="$t('assistant.planEditor.runTarget')"
        :placeholder="$t('assistant.planEditor.runChooseTarget')"
        :search-placeholder="$t('assistant.planEditor.runSearchTarget')"
        :disabled="disabled || !projectRef || !targetType"
        @update:model-value="setTargetRef"
        @select="selectTarget"
      />
      <small v-if="targetProblem" class="field-error" role="alert">
        {{ $t("assistant.planEditor.runTargetUnavailable") }}
      </small>
    </div>
    <label class="field">
      <span>{{ $t("assistant.planEditor.runTask") }}</span>
      <textarea
        :value="stringParameter('task')"
        :id="`${fieldPrefix}-task`"
        :name="`${fieldPrefix}-task`"
        rows="5"
        maxlength="32768"
        :disabled="disabled"
        @input="changed('task', ($event.target as HTMLTextAreaElement).value)"
      />
    </label>
    <template v-if="workflow?.inputFields.length">
      <strong>{{ $t("assistant.planEditor.runWorkflowInput") }}</strong>
      <label
        v-for="field in workflow.inputFields"
        :key="field.key"
        class="field"
      >
        <span>{{ field.label }}</span>
        <select
          v-if="field.valueType === 'SELECT'"
          :value="rawInput[field.key] ?? ''"
          :id="workflowInputName(field.key)"
          :name="workflowInputName(field.key)"
          :disabled="disabled"
          @change="
            setWorkflowInput(field, ($event.target as HTMLSelectElement).value)
          "
        >
          <option value=""></option>
          <option v-for="choice in field.options" :key="choice" :value="choice">
            {{ choice }}
          </option>
        </select>
        <input
          v-else-if="field.valueType === 'BOOLEAN'"
          :id="workflowInputName(field.key)"
          :name="workflowInputName(field.key)"
          type="checkbox"
          :checked="rawInput[field.key] === 'true'"
          :disabled="disabled"
          @change="
            setWorkflowInput(
              field,
              ($event.target as HTMLInputElement).checked ? 'true' : 'false',
            )
          "
        />
        <textarea
          v-else-if="field.valueType === 'LONG_TEXT'"
          :value="rawInput[field.key] ?? ''"
          :id="workflowInputName(field.key)"
          :name="workflowInputName(field.key)"
          rows="4"
          maxlength="32768"
          :disabled="disabled"
          @input="
            setWorkflowInput(
              field,
              ($event.target as HTMLTextAreaElement).value,
            )
          "
        />
        <input
          v-else
          :value="rawInput[field.key] ?? ''"
          :id="workflowInputName(field.key)"
          :name="workflowInputName(field.key)"
          :type="
            field.valueType === 'NUMBER'
              ? 'number'
              : field.valueType === 'DATE'
                ? 'date'
                : 'text'
          "
          :maxlength="field.valueType === 'TEXT' ? 4000 : undefined"
          :required="field.required"
          :disabled="disabled"
          @input="
            setWorkflowInput(field, ($event.target as HTMLInputElement).value)
          "
        />
        <small v-if="field.description">{{ field.description }}</small>
        <small v-if="inputProblems[field.key]" class="field-error">
          {{ $t("assistant.planEditor.runInputInvalid") }}
        </small>
      </label>
      <p v-if="inputProblems.unknown" class="field-error" role="alert">
        {{ $t("assistant.planEditor.runUnknownInput") }}
      </p>
    </template>
    <details
      v-if="
        targetType === 'AGENT' &&
        inputObject() &&
        Object.keys(inputObject() ?? {}).length
      "
    >
      <summary>{{ $t("assistant.planEditor.runAdditionalInput") }}</summary>
      <SafeStructuredData :value="inputObject() ?? {}" literal />
    </details>
    <p v-if="!valid" class="field-error" role="status">
      {{ $t("assistant.planEditor.runNotReady") }}
    </p>
    <p class="assistant-plan-friendly__hint">
      {{ $t("assistant.planEditor.runNextSteps") }}
    </p>
  </div>
</template>

<style scoped>
.assistant-launch-form {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.assistant-launch-form .field {
  min-width: 0;
}
.assistant-launch-form details {
  min-width: 0;
}
</style>
