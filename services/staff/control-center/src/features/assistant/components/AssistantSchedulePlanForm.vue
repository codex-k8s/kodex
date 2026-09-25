<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { loadSchedulePreview } from "@/features/automations/api";
import AutomationPromptPreview from "@/features/automations/AutomationPromptPreview.vue";
import {
  automationTimezoneOptions,
  formatAutomationOccurrence,
  scheduleTimePreview,
} from "@/features/automations/prompt-preview";
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
  ScheduleInput,
  SchedulePreview,
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
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: unknown];
}>();
const { locale } = useI18n();
const selected = ref<ExecutionTargetPickerOption>();
const targetProblem = ref(false);
const preview = ref<SchedulePreview>();
const previewIdentity = ref("");
const previewProblem = ref(false);
const rawInput = ref<Record<string, string>>({});
const invalidInitialInputKeys = ref<Set<string>>(new Set());
const presets = ["HOURLY", "DAILY", "WEEKDAYS", "WEEKLY", "CUSTOM"] as const;
const weekdays = [
  "MONDAY",
  "TUESDAY",
  "WEDNESDAY",
  "THURSDAY",
  "FRIDAY",
  "SATURDAY",
  "SUNDAY",
] as const;
const timezoneOptions = computed(() =>
  automationTimezoneOptions(stringParameter("timezone")),
);

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
const preset = computed(() => stringParameter("preset"));

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
          throw new Error("Assistant schedule target readback mismatch");
        selected.value = toExecutionTargetOption(type, target);
      } catch {
        if (!controller.signal.aborted) targetProblem.value = true;
      }
    })();
  },
  { immediate: true },
);

function inputObject(): Record<string, unknown> | undefined {
  const input = parameter("input");
  return typeof input === "object" && input !== null && !Array.isArray(input)
    ? (input as Record<string, unknown>)
    : undefined;
}
function preparedInput() {
  const prepared = prepareAssistantWorkflowInput(
    workflow.value?.inputFields ?? [],
    rawInput.value,
  );
  for (const key of invalidInitialInputKeys.value)
    prepared.problems[key] = true;
  return prepared;
}
const inputProblems = computed(() => preparedInput().problems);
watch(workflow, (current) => {
  if (!current) return;
  const prepared = preparedInput();
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

const previewInput = computed(() => {
  const misfirePolicy = stringParameter("misfirePolicy") || "COALESCE";
  const overlapPolicy = stringParameter("overlapPolicy") || "FORBID";
  if (
    !presets.includes(preset.value as ScheduleInput["preset"]) ||
    !stringParameter("timezone") ||
    !["COALESCE", "CATCH_UP_ONE", "SKIP"].includes(misfirePolicy) ||
    !["FORBID", "ALLOW"].includes(overlapPolicy)
  )
    return undefined;
  return scheduleTimePreview({
    preset: preset.value as ScheduleInput["preset"],
    cronExpression: stringParameter("cronExpression"),
    timeOfDay: stringParameter("timeOfDay"),
    dayOfWeek: stringParameter("dayOfWeek") as ScheduleInput["dayOfWeek"],
    timezone: stringParameter("timezone"),
    misfirePolicy: misfirePolicy as ScheduleInput["misfirePolicy"],
    overlapPolicy: overlapPolicy as ScheduleInput["overlapPolicy"],
  });
});
const previewKey = computed(() => JSON.stringify(previewInput.value));
watch(
  previewKey,
  (key, _previous, onCleanup) => {
    const input = previewInput.value;
    preview.value = undefined;
    previewIdentity.value = "";
    previewProblem.value = false;
    if (!input) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      void loadSchedulePreview(input, controller.signal)
        .then((value) => {
          if (!controller.signal.aborted) {
            preview.value = value;
            previewIdentity.value = key;
          }
        })
        .catch(() => {
          if (!controller.signal.aborted) previewProblem.value = true;
        });
    }, 350);
    onCleanup(() => {
      clearTimeout(timer);
      controller.abort();
    });
  },
  { immediate: true },
);

function objectParameter(key: string): Record<string, unknown> {
  const value = parameter(key);
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? { ...(value as Record<string, unknown>) }
    : {};
}
const scheduleDraft = computed<ScheduleInput>(() => {
  const schedulePreset = presets.includes(
    preset.value as ScheduleInput["preset"],
  )
    ? (preset.value as ScheduleInput["preset"])
    : "DAILY";
  const sessionPolicy = stringParameter("sessionPolicy");
  const notificationPolicy = stringParameter("notificationPolicy");
  const misfirePolicy = stringParameter("misfirePolicy");
  const overlapPolicy = stringParameter("overlapPolicy");
  const dayOfWeek = stringParameter("dayOfWeek");
  return {
    name: stringParameter("name"),
    targetType: targetType.value ?? "AGENT",
    targetRef: targetRef.value,
    preset: schedulePreset,
    ...(schedulePreset === "CUSTOM"
      ? { cronExpression: stringParameter("cronExpression") }
      : {
          timeOfDay:
            schedulePreset === "HOURLY"
              ? "00:00"
              : stringParameter("timeOfDay"),
        }),
    ...(schedulePreset === "WEEKLY" &&
    weekdays.includes(dayOfWeek as NonNullable<ScheduleInput["dayOfWeek"]>)
      ? { dayOfWeek: dayOfWeek as NonNullable<ScheduleInput["dayOfWeek"]> }
      : {}),
    timezone: stringParameter("timezone"),
    input: objectParameter("input"),
    sessionPolicy:
      sessionPolicy === "CONTINUE_ONE" ? "CONTINUE_ONE" : "NEW_EACH_RUN",
    notificationPolicy:
      notificationPolicy === "CONTROL_CENTER_AND_OPTIONAL_CHANNELS"
        ? "CONTROL_CENTER_AND_OPTIONAL_CHANNELS"
        : "CONTROL_CENTER_ONLY",
    dstGapPolicy: "SHIFT_FORWARD",
    dstFoldPolicy: "RUN_ONCE_EARLIEST",
    misfirePolicy:
      misfirePolicy === "CATCH_UP_ONE" || misfirePolicy === "SKIP"
        ? misfirePolicy
        : "COALESCE",
    overlapPolicy: overlapPolicy === "ALLOW" ? "ALLOW" : "FORBID",
    automationText: stringParameter("automationText"),
    promptInputs: objectParameter("promptInputs"),
  };
});

const valid = computed(() =>
  Boolean(
    props.projectRef &&
    (parameter("projectRef") === props.projectRef ||
      parameter("projectRef") === "current") &&
    stringParameter("name").trim() &&
    stringParameter("name").length <= 160 &&
    stringParameter("automationText").trim() &&
    stringParameter("automationText").length <= 32768 &&
    targetType.value &&
    targetRef.value &&
    selected.value?.ref === targetRef.value &&
    selected.value.targetType === targetType.value &&
    selected.value.target.projectRef === props.projectRef &&
    presets.includes(preset.value as ScheduleInput["preset"]) &&
    typeof parameter("timeOfDay") === "string" &&
    (preset.value !== "CUSTOM" || stringParameter("cronExpression").trim()) &&
    (preset.value === "CUSTOM" ||
      preset.value === "HOURLY" ||
      /^\d{2}:\d{2}$/.test(stringParameter("timeOfDay"))) &&
    (preset.value !== "WEEKLY" ||
      weekdays.includes(
        stringParameter("dayOfWeek") as (typeof weekdays)[number],
      )) &&
    stringParameter("timezone") &&
    ["NEW_EACH_RUN", "CONTINUE_ONE"].includes(
      stringParameter("sessionPolicy"),
    ) &&
    ["CONTROL_CENTER_ONLY", "CONTROL_CENTER_AND_OPTIONAL_CHANNELS"].includes(
      stringParameter("notificationPolicy"),
    ) &&
    inputObject() &&
    (targetType.value !== "WORKFLOW" ||
      !Object.keys(inputProblems.value).length) &&
    previewIdentity.value === previewKey.value &&
    preview.value?.occurrences.length,
  ),
);
watch(valid, (value) => emit("valid", value), { immediate: true });

function change(key: string, value: unknown): void {
  emit("parameter", key, value);
  emit("dirty");
}
function changePreset(event: Event): void {
  const value = (event.target as HTMLSelectElement).value;
  if (!presets.includes(value as ScheduleInput["preset"])) return;
  change("preset", value);
  if (value !== "CUSTOM") change("cronExpression", "");
  if (value === "WEEKLY" && !stringParameter("dayOfWeek"))
    change("dayOfWeek", "MONDAY");
}
function selectType(event: Event): void {
  const type = (event.target as HTMLSelectElement).value;
  if (type !== "AGENT" && type !== "WORKFLOW") return;
  change("targetType", type);
  change("targetRef", "");
  change("input", {});
  selected.value = undefined;
  rawInput.value = {};
}
function clearTarget(value: string | null | readonly string[]): void {
  if (value !== null) return;
  change("targetRef", "");
  selected.value = undefined;
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
  change("targetRef", option.ref);
  change("input", {});
  rawInput.value = {};
}
function loadTargets(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  if (!props.projectRef || !targetType.value)
    return Promise.resolve({ items: [] });
  return createExecutionTargetPickerLoader(props.projectRef, targetType.value)(
    query,
    cursor,
    signal,
  );
}
function changeWorkflowInput(field: WorkflowInputField, raw: string): void {
  rawInput.value = { ...rawInput.value, [field.key]: raw };
  invalidInitialInputKeys.value = new Set(
    [...invalidInitialInputKeys.value].filter((key) => key !== field.key),
  );
  const prepared = preparedInput();
  if (!Object.keys(prepared.problems).length)
    emit("parameter", "input", prepared.value);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-schedule-form">
    <label class="field"
      ><span>{{ $t("common.name") }}</span
      ><input
        :value="stringParameter('name')"
        maxlength="160"
        :disabled="disabled"
        @input="change('name', ($event.target as HTMLInputElement).value)"
    /></label>
    <label class="field"
      ><span>{{ $t("automations.automationText") }}</span
      ><textarea
        :value="stringParameter('automationText')"
        rows="4"
        maxlength="32768"
        :disabled="disabled"
        @input="
          change('automationText', ($event.target as HTMLTextAreaElement).value)
        "
      />
    </label>
    <div class="assistant-schedule-form__grid">
      <label class="field"
        ><span>{{ $t("assistant.planEditor.runTargetType") }}</span
        ><select :value="targetType" :disabled="disabled" @change="selectType">
          <option value="AGENT">
            {{ $t("assistant.planEditor.runAgent") }}
          </option>
          <option value="WORKFLOW">
            {{ $t("assistant.planEditor.runWorkflow") }}
          </option>
        </select></label
      >
      <div class="field">
        <span>{{ $t("assistant.planEditor.runTarget") }}</span
        ><AsyncEntityPicker
          :key="targetType"
          :model-value="targetRef"
          :selected="selectedOption"
          :load-page="loadTargets"
          :context-key="`${projectRef}:${targetType}`"
          :disabled="disabled || !projectRef || !targetType"
          :trigger-label="$t('assistant.planEditor.runTarget')"
          @update:model-value="clearTarget"
          @select="selectTarget"
        /><small v-if="targetProblem" class="field-error">{{
          $t("assistant.planEditor.runTargetUnavailable")
        }}</small>
      </div>
    </div>
    <div class="assistant-schedule-form__grid">
      <label class="field"
        ><span>{{ $t("automations.misfire") }}</span
        ><select
          :value="stringParameter('misfirePolicy') || 'COALESCE'"
          :disabled="disabled"
          @change="
            change('misfirePolicy', ($event.target as HTMLSelectElement).value)
          "
        >
          <option value="COALESCE">
            {{ $t("automations.policies.COALESCE") }}
          </option>
          <option value="CATCH_UP_ONE">
            {{ $t("automations.policies.CATCH_UP_ONE") }}
          </option>
          <option value="SKIP">
            {{ $t("automations.policies.SKIP") }}
          </option>
        </select></label
      >
      <label class="field"
        ><span>{{ $t("automations.overlap") }}</span
        ><select
          :value="stringParameter('overlapPolicy') || 'FORBID'"
          :disabled="disabled"
          @change="
            change('overlapPolicy', ($event.target as HTMLSelectElement).value)
          "
        >
          <option value="FORBID">
            {{ $t("automations.policies.FORBID") }}
          </option>
          <option value="ALLOW">
            {{ $t("automations.policies.ALLOW") }}
          </option>
        </select></label
      >
    </div>
    <div class="assistant-schedule-form__grid">
      <label class="field"
        ><span>{{ $t("automations.preset") }}</span
        ><select :value="preset" :disabled="disabled" @change="changePreset">
          <option v-for="item in presets" :key="item" :value="item">
            {{
              item === "CUSTOM" ? "Cron" : $t(`automations.presetValue.${item}`)
            }}
          </option>
        </select></label
      >
      <label v-if="preset === 'CUSTOM'" class="field"
        ><span>Cron</span
        ><input
          :value="stringParameter('cronExpression')"
          maxlength="120"
          spellcheck="false"
          :disabled="disabled"
          @input="
            change('cronExpression', ($event.target as HTMLInputElement).value)
          "
      /></label>
      <label v-if="preset !== 'CUSTOM' && preset !== 'HOURLY'" class="field"
        ><span>{{ $t("automations.timeOfDay") }}</span
        ><input
          :value="stringParameter('timeOfDay')"
          type="time"
          :disabled="disabled"
          @input="
            change('timeOfDay', ($event.target as HTMLInputElement).value)
          "
      /></label>
      <label v-if="preset === 'WEEKLY'" class="field"
        ><span>{{ $t("automations.dayOfWeek") }}</span
        ><select
          :value="stringParameter('dayOfWeek')"
          :disabled="disabled"
          @change="
            change('dayOfWeek', ($event.target as HTMLSelectElement).value)
          "
        >
          <option v-for="day in weekdays" :key="day" :value="day">
            {{ $t(`automations.day.${day}`) }}
          </option>
        </select></label
      >
      <label class="field"
        ><span>{{ $t("automations.timezone") }}</span
        ><select
          :value="stringParameter('timezone')"
          required
          :disabled="disabled"
          @change="
            change('timezone', ($event.target as HTMLSelectElement).value)
          "
        >
          <option v-for="zone in timezoneOptions" :key="zone" :value="zone">
            {{ zone }}
          </option>
        </select></label
      >
    </div>
    <div class="assistant-schedule-form__grid">
      <label class="field"
        ><span>{{ $t("automations.sessionPolicy") }}</span
        ><select
          :value="stringParameter('sessionPolicy')"
          :disabled="disabled"
          @change="
            change('sessionPolicy', ($event.target as HTMLSelectElement).value)
          "
        >
          <option value="NEW_EACH_RUN">
            {{ $t("automations.newSession") }}
          </option>
          <option value="CONTINUE_ONE">
            {{ $t("automations.continueSession") }}
          </option>
        </select></label
      >
      <label class="field"
        ><span>{{ $t("automations.notifications") }}</span
        ><select
          :value="stringParameter('notificationPolicy')"
          :disabled="disabled"
          @change="
            change(
              'notificationPolicy',
              ($event.target as HTMLSelectElement).value,
            )
          "
        >
          <option value="CONTROL_CENTER_ONLY">
            {{ $t("automations.controlCenterOnly") }}
          </option>
          <option value="CONTROL_CENTER_AND_OPTIONAL_CHANNELS">
            {{ $t("automations.optionalChannels") }}
          </option>
        </select></label
      >
    </div>
    <template v-if="workflow?.inputFields.length">
      <strong>{{ $t("assistant.planEditor.runWorkflowInput") }}</strong>
      <label
        v-for="field in workflow.inputFields"
        :key="field.key"
        class="field"
        ><span>{{ field.label }}</span>
        <select
          v-if="field.valueType === 'SELECT'"
          :value="rawInput[field.key] ?? ''"
          :disabled="disabled"
          @change="
            changeWorkflowInput(
              field,
              ($event.target as HTMLSelectElement).value,
            )
          "
        >
          <option value=""></option>
          <option v-for="choice in field.options" :key="choice" :value="choice">
            {{ choice }}
          </option>
        </select>
        <input
          v-else-if="field.valueType === 'BOOLEAN'"
          type="checkbox"
          :checked="rawInput[field.key] === 'true'"
          :disabled="disabled"
          @change="
            changeWorkflowInput(
              field,
              ($event.target as HTMLInputElement).checked ? 'true' : 'false',
            )
          "
        />
        <textarea
          v-else-if="field.valueType === 'LONG_TEXT'"
          :value="rawInput[field.key] ?? ''"
          rows="3"
          maxlength="32768"
          :disabled="disabled"
          @input="
            changeWorkflowInput(
              field,
              ($event.target as HTMLTextAreaElement).value,
            )
          "
        />
        <input
          v-else
          :value="rawInput[field.key] ?? ''"
          :type="
            field.valueType === 'NUMBER'
              ? 'number'
              : field.valueType === 'DATE'
                ? 'date'
                : 'text'
          "
          :maxlength="field.valueType === 'TEXT' ? 4000 : undefined"
          :disabled="disabled"
          @input="
            changeWorkflowInput(
              field,
              ($event.target as HTMLInputElement).value,
            )
          "
        />
        <small v-if="field.description">{{ field.description }}</small
        ><small v-if="inputProblems[field.key]" class="field-error">{{
          $t("assistant.planEditor.runInputInvalid")
        }}</small>
      </label>
      <p v-if="inputProblems.unknown" class="field-error">
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
    <p v-if="previewProblem" class="field-error" role="alert">
      {{ $t("assistant.planEditor.schedulePreviewFailed") }}
    </p>
    <div
      v-else-if="preview?.occurrences.length"
      class="assistant-schedule-form__preview"
    >
      <strong>{{ $t("assistant.planEditor.scheduleNextRuns") }}</strong>
      <ol>
        <li v-for="item in preview.occurrences.slice(0, 5)" :key="item">
          {{
            formatAutomationOccurrence(
              item,
              locale,
              stringParameter("timezone"),
            )
          }}
        </li>
      </ol>
    </div>
    <p v-if="!valid" class="field-error" role="status">
      {{ $t("assistant.planEditor.scheduleNotReady") }}
    </p>
    <p class="assistant-plan-friendly__hint">
      {{ $t("assistant.planEditor.scheduleNextSteps") }}
    </p>
    <AutomationPromptPreview
      v-if="projectRef"
      :project-ref="projectRef"
      :draft="scheduleDraft"
      :disabled="disabled"
    />
  </div>
</template>

<style scoped>
.assistant-schedule-form {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.assistant-schedule-form__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
}
.assistant-schedule-form .field {
  min-width: 0;
}
.assistant-schedule-form__preview {
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.assistant-schedule-form__preview ol {
  margin: 8px 0 0;
}
</style>
