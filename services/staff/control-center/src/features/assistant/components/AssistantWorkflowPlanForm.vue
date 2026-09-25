<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { loadAgentCatalogPage } from "@/features/agents/catalog/api";
import EffectiveCapabilityCatalog from "@/features/agents/detail/EffectiveCapabilityCatalog.vue";
import TemplateSourceField from "@/features/agents/detail/TemplateSourceField.vue";
import WorkflowOverviewFields from "@/features/workflows/WorkflowOverviewFields.vue";
import {
  operationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import { requestSignal } from "@/shared/api/client";
import { getAgent } from "@/shared/api/generated/openapi/sdk.gen";
import type { Agent } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";

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

type Item = Record<string, unknown>;
const allowedFieldTypes = new Set([
  "TEXT",
  "LONG_TEXT",
  "NUMBER",
  "BOOLEAN",
  "DATE",
  "SELECT",
]);
const allowedDecisions = [
  "APPROVE",
  "REJECT",
  "REQUEST_CHANGES",
  "CANCEL",
] as const;
const fieldKeys = new Set([
  "key",
  "label",
  "description",
  "valueType",
  "required",
  "options",
]);
const stepKeys = new Set([
  "key",
  "name",
  "purpose",
  "agentRef",
  "parallel",
  "parallelGroup",
  "timeoutSeconds",
  "expectedResult",
  "humanGate",
  "gateDecisions",
  "requiredCapabilityKeys",
]);
const agentReadback = ref<Record<string, Agent | null>>({});
const selectableAgents = new Map<string, Agent>();
const isUpdate = computed(() => props.operation.value.type === "UPDATE_WORKFLOW");

function parameter(key: string): unknown {
  try {
    return operationParameter(props.operation, key);
  } catch {
    return undefined;
  }
}
function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}
function itemArray(value: unknown): Item[] | undefined {
  return Array.isArray(value) &&
    value.every(
      (item) =>
        typeof item === "object" && item !== null && !Array.isArray(item),
    )
    ? (value as Item[])
    : undefined;
}
const fields = computed(() =>
  parameter("inputFields") === undefined
    ? []
    : itemArray(parameter("inputFields")),
);
const steps = computed(() => itemArray(parameter("steps")));
const coordinatorRef = computed(() => text(parameter("coordinatorAgentRef")));
const agentRefs = computed(() => [
  ...new Set(
    [
      coordinatorRef.value,
      ...(steps.value ?? []).map((step) => text(step.agentRef)),
    ].filter(Boolean),
  ),
]);
const agentReadbackKey = computed(() =>
  JSON.stringify([props.projectRef, agentRefs.value]),
);

watch(
  agentReadbackKey,
  (_key, _previous, onCleanup) => {
    const projectRef = props.projectRef;
    const refs = agentRefs.value;
    agentReadback.value = {};
    selectableAgents.clear();
    if (!projectRef || !refs.length) return;
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    void (async () => {
      for (let index = 0; index < refs.length; index += 8) {
        const batch = refs.slice(index, index + 8);
        const read = await Promise.all(
          batch.map(async (ref): Promise<[string, Agent | null]> => {
            try {
              const agent = (
                await unwrap(
                  getAgent({
                    path: { agentRef: ref },
                    signal: requestSignal(controller.signal),
                  }),
                )
              ).data;
              return [
                ref,
                agent.ref === ref &&
                agent.projectRef === projectRef &&
                !agent.system
                  ? agent
                  : null,
              ];
            } catch {
              return [ref, null];
            }
          }),
        );
        if (controller.signal.aborted) return;
        agentReadback.value = {
          ...agentReadback.value,
          ...Object.fromEntries(read),
        };
      }
    })();
  },
  { immediate: true },
);

function agentOption(ref: unknown): AsyncEntityOption | undefined {
  const agent = agentReadback.value[text(ref)];
  return agent
    ? { ref: agent.ref, title: agent.name, description: agent.purpose }
    : undefined;
}
async function loadAgents(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  if (!props.projectRef) return { items: [] };
  const page = await loadAgentCatalogPage(
    { projectRef: props.projectRef, query, pageToken, pageSize: 40 },
    signal,
  );
  if (page.items.some((agent) => agent.projectRef !== props.projectRef))
    throw new Error("Assistant workflow agent catalog project mismatch");
  const eligible = page.items.filter((agent) => !agent.system);
  eligible.forEach((agent) => selectableAgents.set(agent.ref, agent));
  return {
    items: eligible.map((agent) => ({
      ref: agent.ref,
      title: agent.name,
      description: agent.purpose,
    })),
    nextPageToken: page.nextPageToken,
  };
}

function change(key: string, value: unknown): void {
  emit("parameter", key, value);
  emit("dirty");
}
function changeStep(index: number, key: string, value: unknown): void {
  const current = steps.value;
  if (!current?.[index]) return;
  change(
    "steps",
    current.map((step, position) =>
      position === index ? { ...step, [key]: value } : step,
    ),
  );
}
function changeField(index: number, key: string, value: unknown): void {
  const current = fields.value;
  if (!current?.[index]) return;
  change(
    "inputFields",
    current.map((field, position) =>
      position === index ? { ...field, [key]: value } : field,
    ),
  );
}
function changeFieldType(index: number, value: string): void {
  const current = fields.value;
  if (!current?.[index] || !allowedFieldTypes.has(value)) return;
  change(
    "inputFields",
    current.map((field, position) =>
      position === index ? { ...field, valueType: value, options: [] } : field,
    ),
  );
}
function chooseAgent(option: AsyncEntityOption, stepIndex?: number): void {
  const agent = selectableAgents.get(option.ref);
  if (!agent || agent.projectRef !== props.projectRef || agent.system) return;
  if (stepIndex === undefined) change("coordinatorAgentRef", option.ref);
  else changeStepAgent(stepIndex, option.ref);
}
function clearAgent(stepIndex?: number): void {
  if (stepIndex === undefined) change("coordinatorAgentRef", "");
  else changeStepAgent(stepIndex, "");
}
function changeStepAgent(index: number, agentRef: string): void {
  const current = steps.value;
  if (!current?.[index]) return;
  change(
    "steps",
    current.map((step, position) =>
      position === index
        ? {
            ...step,
            agentRef,
            requiredCapabilityKeys:
              step.agentRef === agentRef ? step.requiredCapabilityKeys : [],
          }
        : step,
    ),
  );
}
function toggleCapability(index: number, key: string, enabled: boolean): void {
  const selected = steps.value?.[index]?.requiredCapabilityKeys;
  if (!Array.isArray(selected) || typeof key !== "string") return;
  const keys = selected.filter(
    (value): value is string => typeof value === "string",
  );
  changeStep(
    index,
    "requiredCapabilityKeys",
    enabled
      ? [...new Set([...keys, key])]
      : keys.filter((value) => value !== key),
  );
}
function addStep(): void {
  if (!steps.value || steps.value.length >= 200) return;
  change("steps", [
    ...steps.value,
    {
      name: "",
      purpose: "",
      agentRef: coordinatorRef.value,
      parallel: false,
      parallelGroup: 0,
      timeoutSeconds: 1800,
      expectedResult: "",
      humanGate: false,
      gateDecisions: [],
      requiredCapabilityKeys: [],
    },
  ]);
}
function toggleDecision(index: number, decision: string): void {
  const values = Array.isArray(steps.value?.[index]?.gateDecisions)
    ? (steps.value[index].gateDecisions as string[])
    : [];
  changeStep(
    index,
    "gateDecisions",
    values.includes(decision)
      ? values.filter((item) => item !== decision)
      : [...values, decision],
  );
}
function addField(): void {
  if (!fields.value || fields.value.length >= 100) return;
  change("inputFields", [
    ...fields.value,
    {
      label: "",
      description: "",
      valueType: "TEXT",
      required: false,
      options: [],
    },
  ]);
}
function validField(field: Item): boolean {
  const options = field.options;
  return (
    Object.keys(field).every((key) => fieldKeys.has(key) && (key !== "key" || isUpdate.value)) &&
    (field.key === undefined ||
      (typeof field.key === "string" && /^[a-z][a-z0-9_-]{0,79}$/.test(field.key))) &&
    text(field.label).trim().length > 0 &&
    text(field.label).length <= 160 &&
    text(field.description).length <= 500 &&
    allowedFieldTypes.has(text(field.valueType)) &&
    typeof field.required === "boolean" &&
    Array.isArray(options) &&
    options.length <= 50 &&
    options.every(
      (option) =>
        typeof option === "string" && option.trim() && option.length <= 160,
    ) &&
    new Set(options).size === options.length &&
    (field.valueType === "SELECT" ? options.length > 0 : options.length === 0)
  );
}
function validStep(step: Item): boolean {
  const group = step.parallelGroup;
  const decisions = step.gateDecisions;
  const capabilities = step.requiredCapabilityKeys;
  return (
    Object.keys(step).every((key) => stepKeys.has(key) && (key !== "key" || isUpdate.value)) &&
    (step.key === undefined ||
      (typeof step.key === "string" && step.key.length > 0 && step.key.length <= 96)) &&
    text(step.name).trim().length > 0 &&
    text(step.name).length <= 160 &&
    text(step.purpose).trim().length > 0 &&
    text(step.purpose).length <= 1000 &&
    !!agentReadback.value[text(step.agentRef)] &&
    typeof step.parallel === "boolean" &&
    (typeof group === "string"
      ? group.trim().length > 0 && group.length <= 80
      : typeof group === "number" &&
        Number.isInteger(group) &&
        group >= 0 &&
        group <= 50) &&
    typeof step.timeoutSeconds === "number" &&
    Number.isInteger(step.timeoutSeconds) &&
    step.timeoutSeconds >= 1 &&
    step.timeoutSeconds <= 86400 &&
    text(step.expectedResult).length <= 1000 &&
    typeof step.humanGate === "boolean" &&
    Array.isArray(decisions) &&
    decisions.length <= 4 &&
    decisions.every(
      (decision) =>
        typeof decision === "string" &&
        (allowedDecisions as readonly string[]).includes(decision),
    ) &&
    new Set(decisions).size === decisions.length &&
    (!step.humanGate || decisions.length > 0) &&
    Array.isArray(capabilities) &&
    capabilities.length <= 50 &&
    new Set(capabilities).size === capabilities.length &&
    capabilities.every(
      (key) =>
        typeof key === "string" && /^[a-z0-9][a-z0-9._-]{0,79}$/.test(key),
    )
  );
}
const valid = computed(() =>
  Boolean(
    props.projectRef &&
    (isUpdate.value
      ? text(parameter("workflowRef")) === props.operation.value.target.ref &&
        text(parameter("workflowRef")).length > 0 &&
        text(parameter("instructions")).length <= 65536
      : props.operation.value.type === "CREATE_WORKFLOW") &&
    (parameter("projectRef") === props.projectRef ||
      parameter("projectRef") === "current") &&
    text(parameter("name")).trim() &&
    text(parameter("name")).length <= 160 &&
    text(parameter("purpose")).trim() &&
    text(parameter("purpose")).length <= 1000 &&
    agentReadback.value[coordinatorRef.value] &&
    (parameter("maxConcurrency") === undefined ||
      typeof parameter("maxConcurrency") === "number") &&
    Number.isInteger(Number(parameter("maxConcurrency") ?? 1)) &&
    Number(parameter("maxConcurrency") ?? 1) >= 1 &&
    Number(parameter("maxConcurrency") ?? 1) <= 100 &&
    (parameter("timeoutSeconds") === undefined ||
      typeof parameter("timeoutSeconds") === "number") &&
    Number.isInteger(Number(parameter("timeoutSeconds") ?? 7200)) &&
    Number(parameter("timeoutSeconds") ?? 7200) >= 1 &&
    Number(parameter("timeoutSeconds") ?? 7200) <= 604800 &&
    text(parameter("completionCriteria")).length <= 2000 &&
    fields.value &&
    fields.value.length <= 100 &&
    fields.value.every(validField) &&
    steps.value &&
    steps.value.length > 0 &&
    steps.value.length <= 200 &&
    steps.value.every(validStep) &&
    new TextEncoder().encode(
      text(parameter("completionCriteria")) +
        steps.value
          .map(
            (step) =>
              text(step.name) + text(step.purpose) + text(step.expectedResult),
          )
          .join(""),
    ).length <=
      64 * 1024,
  ),
);
watch(valid, (value) => emit("valid", value), { immediate: true });
</script>

<template>
  <div class="assistant-workflow-form">
    <p v-if="isUpdate" class="assistant-plan-friendly__hint">
      {{ $t("assistant.planEditor.workflowUpdateBoundary") }}
    </p>
    <WorkflowOverviewFields
      :name="text(parameter('name'))"
      :purpose="text(parameter('purpose'))"
      :coordinator-agent-ref="coordinatorRef"
      :selected-coordinator="agentOption(coordinatorRef)"
      :load-agents="loadAgents"
      :project-ref="projectRef || ''"
      :timeout-seconds="Number(parameter('timeoutSeconds') ?? 7200)"
      :max-concurrency="Number(parameter('maxConcurrency') ?? 1)"
      :completion-criteria="text(parameter('completionCriteria'))"
      :disabled="disabled"
      @update:name="change('name', $event)"
      @update:purpose="change('purpose', $event)"
      @select-coordinator="chooseAgent($event)"
      @clear-coordinator="clearAgent()"
      @update:timeout-seconds="change('timeoutSeconds', $event)"
      @update:max-concurrency="change('maxConcurrency', $event)"
      @update:completion-criteria="change('completionCriteria', $event)"
    />
    <div v-if="isUpdate" class="field">
      <span>{{ $t("assistant.planEditor.workflowInstructions") }}</span>
      <TemplateSourceField
        :model-value="text(parameter('instructions'))"
        :label="$t('assistant.planEditor.workflowInstructions')"
        :disabled="disabled"
        @update:model-value="change('instructions', $event)"
      />
    </div>

    <section>
      <header class="assistant-workflow-form__section-heading">
        <strong>{{ $t("workflows.inputFields") }}</strong
        ><button
          class="button"
          type="button"
          :disabled="disabled || !fields || fields.length >= 100"
          @click="addField"
        >
          {{ $t("workflows.addInputField") }}
        </button>
      </header>
      <article
        v-for="(field, index) in fields ?? []"
        :key="text(field.key) || index"
        class="assistant-workflow-form__item"
      >
        <label class="field"
          ><span>{{ $t("workflows.inputLabel") }}</span
          ><input
            :value="text(field.label)"
            maxlength="160"
            :disabled="disabled"
            @input="
              changeField(
                index,
                'label',
                ($event.target as HTMLInputElement).value,
              )
            "
        /></label>
        <label class="field"
          ><span>{{ $t("workflows.inputType") }}</span
          ><select
            :value="text(field.valueType)"
            :disabled="disabled"
            @change="
              changeFieldType(index, ($event.target as HTMLSelectElement).value)
            "
          >
            <option
              v-for="kind in [...allowedFieldTypes]"
              :key="kind"
              :value="kind"
            >
              {{ $t(`workflows.inputTypes.${kind}`) }}
            </option>
          </select></label
        >
        <label class="field"
          ><span>{{ $t("workflows.inputDescription") }}</span
          ><input
            :value="text(field.description)"
            maxlength="500"
            :disabled="disabled"
            @input="
              changeField(
                index,
                'description',
                ($event.target as HTMLInputElement).value,
              )
            "
        /></label>
        <label v-if="field.valueType === 'SELECT'" class="field"
          ><span>{{ $t("workflows.inputOptions") }}</span
          ><textarea
            :value="
              Array.isArray(field.options) ? field.options.join('\n') : ''
            "
            rows="3"
            :disabled="disabled"
            @input="
              changeField(
                index,
                'options',
                ($event.target as HTMLTextAreaElement).value
                  .split('\n')
                  .map((value) => value.trim())
                  .filter(Boolean),
              )
            "
          />
        </label>
        <label class="check-field"
          ><input
            :checked="field.required === true"
            type="checkbox"
            :disabled="disabled"
            @change="
              changeField(
                index,
                'required',
                ($event.target as HTMLInputElement).checked,
              )
            "
          />{{ $t("workflows.inputRequired") }}</label
        >
        <button
          class="button button--danger"
          type="button"
          :disabled="disabled"
          @click="
            change(
              'inputFields',
              fields!.filter((_, position) => position !== index),
            )
          "
        >
          {{ $t("common.delete") }}
        </button>
      </article>
    </section>

    <section>
      <header class="assistant-workflow-form__section-heading">
        <strong>{{ $t("workflows.steps") }}</strong
        ><button
          class="button"
          type="button"
          :disabled="disabled || !steps || steps.length >= 200"
          @click="addStep"
        >
          {{ $t("common.create") }}
        </button>
      </header>
      <article
        v-for="(step, index) in steps ?? []"
        :key="text(step.key) || index"
        class="assistant-workflow-form__item"
      >
        <strong>{{ index + 1 }}</strong>
        <label class="field"
          ><span>{{ $t("workflows.stepName") }}</span
          ><input
            :value="text(step.name)"
            maxlength="160"
            :disabled="disabled"
            @input="
              changeStep(
                index,
                'name',
                ($event.target as HTMLInputElement).value,
              )
            "
        /></label>
        <div class="field">
          <span>{{ $t("workflows.stepAgent") }}</span
          ><AsyncEntityPicker
            :model-value="text(step.agentRef)"
            :selected="agentOption(step.agentRef)"
            :load-page="loadAgents"
            :context-key="projectRef"
            :disabled="disabled || !projectRef"
            :trigger-label="$t('workflows.stepAgent')"
            @select="chooseAgent($event, index)"
            @update:model-value="$event === null && clearAgent(index)"
          />
        </div>
        <div class="field">
          <span>{{ $t("common.purpose") }}</span>
          <TemplateSourceField
            :model-value="text(step.purpose)"
            :label="$t('common.purpose')"
            :disabled="disabled"
            @update:model-value="changeStep(index, 'purpose', $event)"
          />
        </div>
        <div class="assistant-workflow-form__advanced">
          <label class="check-field"
            ><input
              :checked="step.parallel === true"
              type="checkbox"
              :disabled="disabled"
              @change="
                changeStep(
                  index,
                  'parallel',
                  ($event.target as HTMLInputElement).checked,
                )
              "
            />{{ $t("workflows.parallel") }}</label
          ><label class="check-field"
            ><input
              :checked="step.humanGate === true"
              type="checkbox"
              :disabled="disabled"
              @change="
                changeStep(
                  index,
                  'humanGate',
                  ($event.target as HTMLInputElement).checked,
                )
              "
            />{{ $t("workflows.humanGate") }}</label
          >
        </div>
        <details>
          <summary>{{ $t("common.advanced") }}</summary>
          <div class="assistant-workflow-form__advanced">
            <label class="field"
              ><span>{{ $t("workflows.parallelGroup") }}</span
              ><input
                :value="step.parallelGroup ?? 0"
                :disabled="disabled || step.parallel !== true"
                @input="
                  changeStep(
                    index,
                    'parallelGroup',
                    ($event.target as HTMLInputElement).value,
                  )
                "
            /></label>
            <label class="field"
              ><span>{{ $t("workflows.stepTimeout") }}</span
              ><input
                :value="step.timeoutSeconds ?? 1800"
                type="number"
                min="1"
                max="86400"
                :disabled="disabled"
                @input="
                  changeStep(
                    index,
                    'timeoutSeconds',
                    Number(($event.target as HTMLInputElement).value),
                  )
                "
            /></label>
          </div>
          <div class="field">
            <span>{{ $t("workflows.expectedResult") }}</span>
            <TemplateSourceField
              :model-value="text(step.expectedResult)"
              :label="$t('workflows.expectedResult')"
              :disabled="disabled"
              @update:model-value="changeStep(index, 'expectedResult', $event)"
            />
            <span>{{ text(step.expectedResult).length }} / 1000</span>
          </div>
          <fieldset v-if="step.humanGate === true">
            <legend>{{ $t("workflows.gateDecisions") }}</legend>
            <label
              v-for="decision in allowedDecisions"
              :key="decision"
              class="check-field"
              ><input
                :checked="
                  Array.isArray(step.gateDecisions) &&
                  step.gateDecisions.includes(decision)
                "
                type="checkbox"
                :disabled="disabled"
                @change="toggleDecision(index, decision)"
              />{{ $t(`workflows.gateDecision.${decision}`) }}</label
            >
          </fieldset>
          <fieldset class="field">
            <legend>{{ $t("workflows.requiredCapabilities") }}</legend>
            <EffectiveCapabilityCatalog
              v-if="text(step.agentRef) && agentReadback[text(step.agentRef)]"
              :agent-ref="text(step.agentRef)"
              :project-ref="projectRef"
              mode="REQUIREMENTS"
              :selected-keys="
                Array.isArray(step.requiredCapabilityKeys)
                  ? (step.requiredCapabilityKeys as string[])
                  : []
              "
              :can-manage="!disabled"
              :busy="disabled"
              @toggle="(key, enabled) => toggleCapability(index, key, enabled)"
            />
          </fieldset>
        </details>
        <button
          class="button button--danger"
          type="button"
          :disabled="disabled || steps!.length <= 1"
          @click="
            change(
              'steps',
              steps!.filter((_, position) => position !== index),
            )
          "
        >
          {{ $t("common.delete") }}
        </button>
      </article>
    </section>
    <p v-if="!valid" class="field-error" role="status">
      {{ $t("assistant.planEditor.workflowNotReady") }}
    </p>
    <p class="assistant-plan-friendly__hint">
      {{ $t(isUpdate ? "assistant.planEditor.workflowUpdateNextSteps" : "assistant.planEditor.workflowNextSteps") }}
    </p>
  </div>
</template>

<style scoped>
.assistant-workflow-form {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.assistant-workflow-form section {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.assistant-workflow-form__section-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}
.assistant-workflow-form__item {
  display: grid;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  min-width: 0;
}
.assistant-workflow-form__advanced {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 10px;
}
.assistant-workflow-form .field,
.assistant-workflow-form details {
  min-width: 0;
}
</style>
