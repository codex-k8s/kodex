<script setup lang="ts">
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import {
  AlertTriangle,
  ArrowLeft,
  Check,
  Maximize2,
  Save,
  Trash2,
} from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import AssistantCodeEditorModal from "@/features/assistant/components/AssistantCodeEditorModal.vue";
import AssistantLaunchRunForm from "@/features/assistant/components/AssistantLaunchRunForm.vue";
import AssistantWorkflowPlanForm from "@/features/assistant/components/AssistantWorkflowPlanForm.vue";
import { prepareConnectionConfiguration } from "@/features/integrations/connection-setup";
import { loadExactIntegrationDefinition } from "@/features/integrations/definition-lookup";
import { loadRoleEnvironmentCatalog } from "@/features/role-images/api";
import { useRuntimeStore } from "@/features/runtime/store";
import {
  editableOperations,
  friendlyPlanOperationType,
  operationActionLabel,
  operationInputs,
  operationParameter,
  operationTargetLabel,
  updateOperationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import type {
  AssistantPlan,
  AssistantPlanOperationInput,
  AssistantPlanReceipt,
  IntegrationDefinition,
  RoleEnvironment,
} from "@/shared/api/generated/openapi/types.gen";
import { listAgents } from "@/shared/api/generated/openapi/sdk.gen";
import { unwrap } from "@/shared/api/problem";
import type { AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  plan: AssistantPlan;
  receipt?: AssistantPlanReceipt;
  busy?: boolean;
  readonly?: boolean;
  problem?: AppProblem;
}>();
const emit = defineEmits<{
  close: [];
  save: [summary: string, operations: AssistantPlanOperationInput[]];
  validate: [];
  apply: [];
  reject: [];
}>();
const { t } = useI18n();
const runtime = useRuntimeStore();
const summary = ref("");
const operations = ref<EditablePlanOperation[]>([]);
const selectedImages = ref<Record<string, AsyncEntityOption>>({});
const roleImageAgentNames = ref<Record<string, string>>({});
const roleImageEnvironments = ref<RoleEnvironment[]>([]);
const roleImageCatalogProblem = ref(false);
const connectionDefinitions = ref<Record<string, IntegrationDefinition>>({});
const connectionCatalogProblem = ref(false);
const connectionInputs = ref<Record<string, Record<string, string>>>({});
const connectionInputsTouched = ref(false);
const runFormValidity = ref<Record<string, boolean>>({});
const runFormTouched = ref(false);
const workflowFormValidity = ref<Record<string, boolean>>({});
const workflowFormTouched = ref(false);
const inputProblem = ref("");
type EditorTarget =
  | { kind: "SUMMARY" }
  | {
      kind: "OPERATION_SUMMARY" | "PARAMETERS" | "BEFORE" | "AFTER";
      operationIndex: number;
    };
const editorTarget = ref<EditorTarget>();

function resetDraft(): void {
  summary.value = props.plan.auditSummary;
  operations.value = editableOperations(props.plan.operations);
  connectionInputs.value = Object.fromEntries(
    operations.value
      .filter(
        (operation) => operation.value.type === "CREATE_INTEGRATION_CONNECTION",
      )
      .map((operation) => {
        const configuration = operationParameter(
          operation,
          "publicConfiguration",
        );
        const values =
          typeof configuration === "object" &&
          configuration !== null &&
          !Array.isArray(configuration)
            ? (configuration as Record<string, unknown>)
            : {};
        return [
          operation.value.ref,
          Object.fromEntries(
            Object.entries(values).map(([key, value]) => [
              key,
              Array.isArray(value)
                ? value.map(String).join(", ")
                : typeof value === "string" ||
                    typeof value === "number" ||
                    typeof value === "boolean"
                  ? String(value)
                  : "",
            ]),
          ),
        ];
      }),
  );
  connectionInputsTouched.value = false;
  runFormValidity.value = {};
  runFormTouched.value = false;
  workflowFormValidity.value = {};
  workflowFormTouched.value = false;
  inputProblem.value = "";
}

watch(() => props.plan, resetDraft, { immediate: true });

watch(
  () => props.plan,
  (plan, _previous, onCleanup) => {
    connectionDefinitions.value = {};
    connectionCatalogProblem.value = false;
    const keys = [
      ...new Set(
        plan.operations
          .filter(
            (operation) => operation.type === "CREATE_INTEGRATION_CONNECTION",
          )
          .map((operation) => operation.parameters.definitionKey)
          .filter((value): value is string => typeof value === "string"),
      ),
    ];
    if (!keys.length) return;
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    void Promise.all(
      keys.map((key) => loadExactIntegrationDefinition(key, controller.signal)),
    )
      .then((definitions) => {
        if (controller.signal.aborted) return;
        connectionDefinitions.value = Object.fromEntries(
          definitions.map((definition) => [definition.key, definition]),
        );
        for (const operation of operations.value) {
          const definition = definitions.find(
            (item) =>
              item.key === operationParameter(operation, "definitionKey"),
          );
          if (!definition) continue;
          const raw = { ...connectionInputs.value[operation.value.ref] };
          for (const field of definition.configurationFields)
            if (field.valueType === "BOOLEAN" && raw[field.key] === undefined)
              raw[field.key] = "false";
          connectionInputs.value = {
            ...connectionInputs.value,
            [operation.value.ref]: raw,
          };
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) connectionCatalogProblem.value = true;
      });
  },
  { immediate: true },
);

function connectionDefinition(
  operation: EditablePlanOperation,
): IntegrationDefinition | undefined {
  try {
    const key = operationParameter(operation, "definitionKey");
    return typeof key === "string"
      ? connectionDefinitions.value[key]
      : undefined;
  } catch {
    return undefined;
  }
}

function connectionProblems(
  operation: EditablePlanOperation,
): Partial<Record<string, string>> {
  try {
    const definition = connectionDefinition(operation);
    if (!definition?.available) return { definitionKey: "UNAVAILABLE" };
    const raw = connectionInputs.value[operation.value.ref] ?? {};
    const initial = operationParameter(operation, "publicConfiguration");
    if (
      typeof initial !== "object" ||
      initial === null ||
      Array.isArray(initial)
    )
      return { publicConfiguration: "INVALID_VALUE" };
    const known = new Set(
      definition.configurationFields.map((field) => field.key),
    );
    if (Object.keys(raw).some((key) => !known.has(key)))
      return { publicConfiguration: "UNKNOWN_FIELD" };
    return prepareConnectionConfiguration(definition.configurationFields, raw)
      .problems;
  } catch {
    return { publicConfiguration: "INVALID_VALUE" };
  }
}

function connectionProblemLabel(code: string | undefined): string {
  if (code === "REQUIRED") return t("assistant.planEditor.connectionRequired");
  if (code === "INVALID_HTTPS_URL")
    return t("assistant.planEditor.connectionHttpsUrl");
  return t("assistant.planEditor.connectionInvalidValue");
}

function setConnectionField(
  operation: EditablePlanOperation,
  key: string,
  value: string,
): void {
  const definition = connectionDefinition(operation);
  if (!definition?.configurationFields.some((field) => field.key === key))
    return;
  const raw = { ...connectionInputs.value[operation.value.ref], [key]: value };
  connectionInputs.value = {
    ...connectionInputs.value,
    [operation.value.ref]: raw,
  };
  connectionInputsTouched.value = true;
  const prepared = prepareConnectionConfiguration(
    definition.configurationFields,
    raw,
  );
  if (!Object.keys(prepared.problems).length)
    updateOperationParameter(operation, "publicConfiguration", prepared.value);
}

watch(
  () => props.plan,
  (plan, _previous, onCleanup) => {
    roleImageAgentNames.value = {};
    roleImageEnvironments.value = [];
    roleImageCatalogProblem.value = false;
    const projectRef = plan.projectRef;
    if (
      !projectRef ||
      !plan.operations.some(
        (operation) => operation.type === "CREATE_ROLE_IMAGE_RECIPE",
      )
    )
      return;
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    void (async () => {
      try {
        const names: Record<string, string> = {};
        const visitedTokens = new Set<string>();
        let pageToken: string | undefined;
        do {
          const page = (
            await unwrap(
              listAgents({
                path: { projectRef },
                query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
                signal: controller.signal,
              }),
            )
          ).data;
          for (const agent of page.items) names[agent.ref] = agent.name;
          pageToken = page.nextPageToken;
          if (pageToken && visitedTokens.has(pageToken))
            throw new Error("Agent catalog returned a repeated page token");
          if (pageToken) visitedTokens.add(pageToken);
        } while (pageToken);
        const environments = await loadRoleEnvironmentCatalog(
          controller.signal,
        );
        if (controller.signal.aborted) return;
        roleImageAgentNames.value = names;
        roleImageEnvironments.value = environments;
      } catch {
        if (!controller.signal.aborted) roleImageCatalogProblem.value = true;
      }
    })();
  },
  { immediate: true },
);

const selectedCount = computed(
  () => operations.value.filter((operation) => operation.value.selected).length,
);
const exactRevisionValidated = computed(
  () =>
    props.plan.state === "VALID" &&
    props.plan.validatedRevision === props.plan.revision,
);
const draftMatchesSavedPlan = computed(() => {
  try {
    return (
      summary.value === props.plan.auditSummary &&
      !connectionInputsTouched.value &&
      !runFormTouched.value &&
      !workflowFormTouched.value &&
      JSON.stringify(operationInputs(operations.value)) ===
        JSON.stringify(
          operationInputs(editableOperations(props.plan.operations)),
        )
    );
  } catch {
    return false;
  }
});
const editable = computed(
  () =>
    !props.readonly &&
    !props.busy &&
    !["APPLIED", "REJECTED"].includes(props.plan.state),
);
const friendlyInputsReady = computed(() =>
  operations.value.every(
    (operation) =>
      !operation.value.selected ||
      ((operation.value.type !== "CREATE_INTEGRATION_CONNECTION" ||
        !Object.keys(connectionProblems(operation)).length) &&
        (operation.value.type !== "LAUNCH_RUN" ||
          runFormValidity.value[operation.value.ref] === true) &&
        (operation.value.type !== "CREATE_WORKFLOW" ||
          workflowFormValidity.value[operation.value.ref] === true)),
  ),
);
const canSave = computed(
  () =>
    editable.value &&
    summary.value.trim().length > 0 &&
    selectedCount.value > 0 &&
    friendlyInputsReady.value &&
    !["APPLIED", "REJECTED"].includes(props.plan.state),
);
const canValidate = computed(
  () =>
    !props.readonly &&
    !props.busy &&
    !["APPLIED", "REJECTED"].includes(props.plan.state) &&
    props.plan.state !== "VALID",
);
const canApply = computed(
  () =>
    !props.readonly &&
    !props.busy &&
    exactRevisionValidated.value &&
    draftMatchesSavedPlan.value &&
    friendlyInputsReady.value &&
    props.plan.nextActions.includes("APPLY_PLAN"),
);
const canReject = computed(() => editable.value);

function save(): void {
  inputProblem.value = "";
  try {
    for (const operation of operations.value) {
      if (
        !operation.value.selected ||
        operation.value.type !== "CREATE_INTEGRATION_CONNECTION"
      )
        continue;
      const definition = connectionDefinition(operation);
      if (!definition || Object.keys(connectionProblems(operation)).length)
        return;
      const prepared = prepareConnectionConfiguration(
        definition.configurationFields,
        connectionInputs.value[operation.value.ref] ?? {},
      );
      updateOperationParameter(
        operation,
        "publicConfiguration",
        prepared.value,
      );
    }
    emit("save", summary.value.trim(), operationInputs(operations.value));
  } catch {
    inputProblem.value = t("assistant.planEditor.jsonError");
  }
}

const editorValue = computed(() => {
  const target = editorTarget.value;
  if (!target) return "";
  if (target.kind === "SUMMARY") return summary.value;
  const operation = operations.value[target.operationIndex];
  if (!operation) return "";
  if (target.kind === "OPERATION_SUMMARY") return operation.value.summary;
  if (target.kind === "PARAMETERS") return operation.parametersText;
  if (target.kind === "BEFORE") return operation.beforeText;
  return operation.afterText;
});
const editorTitle = computed(() => {
  const target = editorTarget.value;
  if (!target) return "";
  if (target.kind === "SUMMARY") return t("assistant.planEditor.summary");
  if (target.kind === "OPERATION_SUMMARY")
    return t("assistant.planEditor.operationSummary");
  if (target.kind === "PARAMETERS") return t("assistant.planEditor.parameters");
  if (target.kind === "BEFORE") return t("assistant.planEditor.before");
  return t("assistant.planEditor.after");
});
const editorLanguage = computed<"json" | "text">(() =>
  editorTarget.value?.kind === "PARAMETERS" ||
  editorTarget.value?.kind === "BEFORE" ||
  editorTarget.value?.kind === "AFTER"
    ? "json"
    : "text",
);

function saveEditor(value: string): void {
  const target = editorTarget.value;
  if (!target) return;
  if (target.kind === "SUMMARY") summary.value = value;
  else {
    const operation = operations.value[target.operationIndex];
    if (!operation) return;
    if (target.kind === "OPERATION_SUMMARY") operation.value.summary = value;
    else if (target.kind === "PARAMETERS") operation.parametersText = value;
    else if (target.kind === "BEFORE") operation.beforeText = value;
    else operation.afterText = value;
  }
  editorTarget.value = undefined;
}

function optionalNumber(event: Event): number | undefined {
  const value = (event.target as HTMLInputElement).value;
  if (!value) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : undefined;
}

const initialCapabilities = [
  "platform.artifact.manage",
  "platform.run.delegate",
  "platform.run.launch",
] as const;

function fieldValue(operation: EditablePlanOperation, key: string): string {
  const value = operationParameter(operation, key);
  return typeof value === "string" ? value : "";
}

function setField(
  operation: EditablePlanOperation,
  key: string,
  event: Event,
): void {
  updateOperationParameter(
    operation,
    key,
    (event.target as HTMLInputElement | HTMLTextAreaElement).value,
  );
}

function setRunTarget(
  operation: EditablePlanOperation,
  target: { kind: string; ref?: string; name: string; version?: number },
): void {
  operation.value.target = { ...target };
}

function selectedImage(
  operation: EditablePlanOperation,
): AsyncEntityOption | undefined {
  const ref = fieldValue(operation, "imageArtifactRef");
  return ref
    ? (selectedImages.value[ref] ?? {
        ref,
        title: ref,
        description: t("assistant.planEditor.environmentSelectedImage"),
      })
    : undefined;
}

function rememberSelectedImage(option: AsyncEntityOption): void {
  selectedImages.value = { ...selectedImages.value, [option.ref]: option };
}

function loadImagePage(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
) {
  if (!props.plan.projectRef) return Promise.resolve({ items: [] });
  return runtime.searchPromotedRoleImagePage(
    props.plan.projectRef,
    query,
    cursor,
    signal,
  );
}

function setImageArtifact(
  operation: EditablePlanOperation,
  value: string | null | readonly string[],
): void {
  if (typeof value === "string" || value === null) {
    updateOperationParameter(operation, "imageArtifactRef", value ?? "");
  }
}

function capabilityChecked(
  operation: EditablePlanOperation,
  key: string,
): boolean {
  const value = operationParameter(operation, "capabilities");
  return Array.isArray(value) && value.includes(key);
}

function setCapability(
  operation: EditablePlanOperation,
  key: string,
  event: Event,
): void {
  const current = operationParameter(operation, "capabilities");
  const selected = Array.isArray(current)
    ? current.filter((item): item is string => typeof item === "string")
    : [];
  const next = (event.target as HTMLInputElement).checked
    ? [...new Set([...selected, key])]
    : selected.filter((item) => item !== key);
  updateOperationParameter(operation, "capabilities", next);
}

function snapshot(value: string): Record<string, unknown> {
  try {
    const parsed: unknown = JSON.parse(value);
    return typeof parsed === "object" &&
      parsed !== null &&
      !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}
</script>

<template>
  <section class="assistant-plan-editor" aria-labelledby="assistant-plan-title">
    <header class="assistant-plan-editor__header">
      <button
        class="icon-button"
        type="button"
        :aria-label="$t('assistant.planEditor.back')"
        @click="emit('close')"
      >
        <ArrowLeft :size="19" aria-hidden="true" />
      </button>
      <div>
        <h2 id="assistant-plan-title">{{ $t("assistant.plan") }}</h2>
        <p>
          {{
            $t("assistant.planEditor.revision", {
              revision: plan.revision,
              count: plan.operations.length,
            })
          }}
        </p>
      </div>
      <StatusBadge :state="plan.state" />
    </header>

    <div class="assistant-plan-editor__body">
      <section class="assistant-plan-notice">
        <Check :size="18" aria-hidden="true" />
        <span>{{ $t("assistant.planEditor.atomic") }}</span>
      </section>
      <ProblemNotice v-if="problem" :problem="problem" compact />
      <p v-if="inputProblem" class="field-error" role="alert">
        {{ inputProblem }}
      </p>

      <section
        v-if="receipt?.outcome === 'CONFLICT'"
        class="assistant-plan-conflict"
        role="alert"
      >
        <header>
          <AlertTriangle :size="20" aria-hidden="true" />
          <div>
            <h3>{{ $t("assistant.planEditor.conflictTitle") }}</h3>
            <p>{{ $t("assistant.planEditor.conflictText") }}</p>
          </div>
        </header>
        <dl>
          <template
            v-for="conflict in receipt.conflicts"
            :key="`${conflict.operationRef}:${conflict.field}`"
          >
            <dt>{{ conflict.field }}</dt>
            <dd>
              <span>{{ $t("assistant.planEditor.expected") }}</span>
              <SafeStructuredData :value="conflict.expected" />
              <span>{{ $t("assistant.planEditor.actual") }}</span>
              <SafeStructuredData :value="conflict.actual" />
            </dd>
          </template>
        </dl>
      </section>

      <section
        v-else-if="receipt"
        class="assistant-plan-receipt"
        aria-live="polite"
      >
        <StatusBadge :state="receipt.outcome" />
        <p>
          {{
            $t("assistant.planEditor.receipt", {
              revision: receipt.planRevision,
              count: receipt.operationReceipts.length,
            })
          }}
        </p>
      </section>

      <div class="field">
        <span class="assistant-field-label">
          <span>{{ $t("assistant.planEditor.summary") }}</span>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('assistant.planEditor.openFieldEditor')"
            :title="$t('assistant.planEditor.openFieldEditor')"
            @click.prevent="editorTarget = { kind: 'SUMMARY' }"
          >
            <Maximize2 :size="15" aria-hidden="true" />
          </button>
        </span>
        <VoiceTextarea
          v-model="summary"
          :disabled="!editable"
          rows="3"
          maxlength="2000"
          :aria-label="$t('assistant.planEditor.summary')"
        />
      </div>

      <div class="assistant-plan-operations">
        <article
          v-for="(operation, index) in operations"
          :key="operation.value.ref"
          class="assistant-plan-operation"
          :class="`assistant-plan-operation--${operationActionLabel(operation.value.action)}`"
        >
          <header>
            <label class="assistant-plan-operation__select">
              <input
                v-model="operation.value.selected"
                type="checkbox"
                :disabled="!editable || !operation.value.permitted"
              />
              <span class="assistant-operation-kind">
                {{
                  $t(
                    `assistant.planEditor.actions.${operationActionLabel(operation.value.action)}`,
                  )
                }}
              </span>
            </label>
            <span class="assistant-plan-operation__number"
              >#{{ index + 1 }}</span
            >
          </header>

          <dl class="assistant-plan-operation__identity">
            <div>
              <dt>{{ $t("assistant.planEditor.commandType") }}</dt>
              <dd>{{ operation.value.type }}</dd>
            </div>
            <div>
              <dt>{{ $t("assistant.planEditor.action") }}</dt>
              <dd>{{ operation.value.action }}</dd>
            </div>
            <div>
              <dt>{{ $t("assistant.planEditor.permitted") }}</dt>
              <dd>
                {{ $t(operation.value.permitted ? "common.yes" : "common.no") }}
              </dd>
            </div>
          </dl>

          <label class="field">
            <span>{{ $t("assistant.planEditor.operationTitle") }}</span>
            <input
              v-model="operation.value.title"
              maxlength="300"
              :disabled="!editable"
            />
          </label>
          <div class="field">
            <span class="assistant-field-label">
              <span>{{ $t("assistant.planEditor.operationSummary") }}</span>
              <button
                class="icon-button"
                type="button"
                :aria-label="$t('assistant.planEditor.openFieldEditor')"
                :title="$t('assistant.planEditor.openFieldEditor')"
                @click.prevent="
                  editorTarget = {
                    kind: 'OPERATION_SUMMARY',
                    operationIndex: index,
                  }
                "
              >
                <Maximize2 :size="15" aria-hidden="true" />
              </button>
            </span>
            <VoiceTextarea
              v-model="operation.value.summary"
              rows="2"
              maxlength="2000"
              :disabled="!editable"
              :aria-label="$t('assistant.planEditor.operationSummary')"
            />
          </div>

          <fieldset class="assistant-plan-target">
            <legend>{{ $t("assistant.planEditor.target") }}</legend>
            <div class="assistant-plan-target__summary">
              <span class="assistant-plan-target__action">
                {{
                  $t(
                    `assistant.planEditor.actions.${operationActionLabel(operation.value.action)}`,
                  )
                }}
              </span>
              <strong>{{
                operationTargetLabel(operation.value.target)
              }}</strong>
              <small>{{ operation.value.target.kind }}</small>
            </div>
            <label v-if="!friendlyPlanOperationType(operation)" class="field">
              <span>{{ $t("assistant.planEditor.targetKind") }}</span>
              <input
                v-model="operation.value.target.kind"
                maxlength="120"
                :disabled="!editable"
              />
            </label>
            <label v-if="!friendlyPlanOperationType(operation)" class="field">
              <span>{{ $t("assistant.planEditor.targetName") }}</span>
              <input
                v-model="operation.value.target.name"
                maxlength="300"
                :disabled="!editable"
              />
            </label>
            <label v-if="!friendlyPlanOperationType(operation)" class="field">
              <span>{{ $t("assistant.planEditor.targetRef") }}</span>
              <input
                v-model="operation.value.target.ref"
                maxlength="300"
                :disabled="!editable"
              />
            </label>
            <label v-if="!friendlyPlanOperationType(operation)" class="field">
              <span>{{ $t("assistant.planEditor.targetVersion") }}</span>
              <input
                type="number"
                min="0"
                step="1"
                :value="operation.value.target.version"
                :disabled="!editable"
                @input="operation.value.target.version = optionalNumber($event)"
              />
            </label>
            <label v-if="!friendlyPlanOperationType(operation)" class="field">
              <span>{{ $t("assistant.planEditor.expectedVersion") }}</span>
              <input
                type="number"
                min="0"
                step="1"
                :value="operation.value.expectedVersion"
                :disabled="!editable"
                @input="
                  operation.value.expectedVersion = optionalNumber($event)
                "
              />
            </label>
          </fieldset>

          <div
            v-if="friendlyPlanOperationType(operation)"
            class="assistant-plan-friendly"
          >
            <p class="assistant-plan-friendly__hint">
              {{ $t("assistant.planEditor.friendlyHint") }}
            </p>
            <AssistantLaunchRunForm
              v-if="operation.value.type === 'LAUNCH_RUN'"
              :operation="operation"
              :project-ref="plan.projectRef"
              :disabled="!editable"
              @valid="runFormValidity[operation.value.ref] = $event"
              @dirty="runFormTouched = true"
              @parameter="
                (key, value) => updateOperationParameter(operation, key, value)
              "
              @target="setRunTarget(operation, $event)"
            />
            <AssistantWorkflowPlanForm
              v-else-if="operation.value.type === 'CREATE_WORKFLOW'"
              :operation="operation"
              :project-ref="plan.projectRef"
              :disabled="!editable"
              @valid="workflowFormValidity[operation.value.ref] = $event"
              @dirty="workflowFormTouched = true"
              @parameter="
                (key, value) => updateOperationParameter(operation, key, value)
              "
            />
            <template v-else>
              <label class="field">
                <span>{{ $t("assistant.planEditor.entityName") }}</span>
                <input
                  :value="fieldValue(operation, 'name')"
                  :maxlength="
                    operation.value.type === 'CREATE_RUNTIME_ENVIRONMENT_DRAFT'
                      ? 120
                      : 160
                  "
                  :disabled="!editable"
                  @input="setField(operation, 'name', $event)"
                />
              </label>
              <label
                v-if="
                  operation.value.target.kind !== 'RUNTIME_ENVIRONMENT_DRAFT' &&
                  operation.value.target.kind !== 'ROLE_IMAGE_RECIPE' &&
                  operation.value.target.kind !== 'INTEGRATION_CONNECTION'
                "
                class="field"
              >
                <span>{{ $t("assistant.planEditor.entityPurpose") }}</span>
                <textarea
                  :value="fieldValue(operation, 'purpose')"
                  rows="3"
                  maxlength="2000"
                  :disabled="!editable"
                  @input="setField(operation, 'purpose', $event)"
                />
              </label>
              <template v-if="operation.value.target.kind === 'PROJECT'">
                <label class="field">
                  <span>{{ $t("assistant.planEditor.projectLanguage") }}</span>
                  <select
                    :value="fieldValue(operation, 'language')"
                    :disabled="!editable"
                    @change="setField(operation, 'language', $event)"
                  >
                    <option value="ru">Русский</option>
                    <option value="en">English</option>
                  </select>
                </label>
              </template>
              <template
                v-else-if="
                  operation.value.target.kind === 'RUNTIME_ENVIRONMENT_DRAFT'
                "
              >
                <label class="field">
                  <span>{{
                    $t("assistant.planEditor.environmentDescription")
                  }}</span>
                  <textarea
                    :value="fieldValue(operation, 'description')"
                    rows="3"
                    maxlength="1000"
                    :disabled="!editable"
                    @input="setField(operation, 'description', $event)"
                  />
                </label>
                <label class="field">
                  <span>{{
                    $t("assistant.planEditor.environmentImageArtifact")
                  }}</span>
                  <AsyncEntityPicker
                    :model-value="fieldValue(operation, 'imageArtifactRef')"
                    :selected="selectedImage(operation)"
                    :load-page="loadImagePage"
                    :trigger-label="
                      $t('assistant.planEditor.environmentImageArtifact')
                    "
                    :placeholder="
                      $t('assistant.planEditor.environmentChooseImage')
                    "
                    :search-placeholder="
                      $t('assistant.planEditor.environmentSearchImage')
                    "
                    :disabled="!editable || !plan.projectRef"
                    @update:model-value="setImageArtifact(operation, $event)"
                    @select="rememberSelectedImage"
                  />
                </label>
                <p class="assistant-plan-friendly__hint">
                  {{ $t("assistant.planEditor.environmentDraftNextSteps") }}
                </p>
              </template>
              <template
                v-else-if="
                  operation.value.target.kind === 'INTEGRATION_CONNECTION'
                "
              >
                <div class="field">
                  <span>{{
                    $t("assistant.planEditor.connectionDefinition")
                  }}</span>
                  <strong>{{
                    connectionDefinition(operation)?.name ||
                    fieldValue(operation, "definitionKey")
                  }}</strong>
                  <small>{{
                    $t("assistant.planEditor.connectionDefinitionFixed")
                  }}</small>
                </div>
                <p
                  v-if="connectionCatalogProblem"
                  class="field-error"
                  role="alert"
                >
                  {{ $t("assistant.planEditor.connectionCatalogUnavailable") }}
                </p>
                <p
                  v-else-if="
                    connectionDefinition(operation)?.available === false
                  "
                  class="field-error"
                  role="alert"
                >
                  {{ $t("assistant.planEditor.connectionCatalogUnavailable") }}
                </p>
                <template
                  v-for="field in connectionDefinition(operation)
                    ?.configurationFields ?? []"
                  :key="field.key"
                >
                  <label class="field">
                    <span>{{ field.label }}</span>
                    <select
                      v-if="field.allowedValues?.length"
                      :value="
                        connectionInputs[operation.value.ref]?.[field.key] ?? ''
                      "
                      :disabled="!editable"
                      @change="
                        setConnectionField(
                          operation,
                          field.key,
                          ($event.target as HTMLSelectElement).value,
                        )
                      "
                    >
                      <option value=""></option>
                      <option
                        v-for="choice in field.allowedValues"
                        :key="choice"
                        :value="choice"
                      >
                        {{ choice }}
                      </option>
                    </select>
                    <input
                      v-else-if="field.valueType === 'BOOLEAN'"
                      type="checkbox"
                      :checked="
                        connectionInputs[operation.value.ref]?.[field.key] ===
                        'true'
                      "
                      :disabled="!editable"
                      @change="
                        setConnectionField(
                          operation,
                          field.key,
                          ($event.target as HTMLInputElement).checked
                            ? 'true'
                            : 'false',
                        )
                      "
                    />
                    <input
                      v-else
                      :value="
                        connectionInputs[operation.value.ref]?.[field.key] ?? ''
                      "
                      :type="field.valueType === 'URL' ? 'url' : 'text'"
                      :inputmode="
                        field.valueType === 'INTEGER' ? 'numeric' : undefined
                      "
                      :required="field.required"
                      :placeholder="field.placeholder"
                      :maxlength="
                        field.maximumLength ??
                        (field.valueType === 'URL' ? 2048 : 500)
                      "
                      :disabled="!editable"
                      :aria-invalid="
                        Boolean(connectionProblems(operation)[field.key])
                      "
                      autocomplete="off"
                      @input="
                        setConnectionField(
                          operation,
                          field.key,
                          ($event.target as HTMLInputElement).value,
                        )
                      "
                    />
                    <small>{{ field.help }}</small>
                    <small v-if="field.valueType === 'STRING_LIST'">{{
                      $t("assistant.planEditor.connectionListHint")
                    }}</small>
                    <small
                      v-if="connectionProblems(operation)[field.key]"
                      class="field-error"
                    >
                      {{
                        connectionProblemLabel(
                          connectionProblems(operation)[field.key],
                        )
                      }}
                    </small>
                  </label>
                </template>
                <p
                  v-if="connectionProblems(operation).publicConfiguration"
                  class="field-error"
                  role="alert"
                >
                  {{
                    $t("assistant.planEditor.connectionConfigurationInvalid")
                  }}
                </p>
                <p class="assistant-plan-friendly__hint">
                  {{ $t("assistant.planEditor.connectionCredentialNextSteps") }}
                </p>
              </template>
              <template
                v-else-if="operation.value.target.kind === 'ROLE_IMAGE_RECIPE'"
              >
                <div class="field">
                  <span>{{ $t("assistant.planEditor.roleImageAgent") }}</span>
                  <strong>{{
                    roleImageAgentNames[fieldValue(operation, "agentRef")] ||
                    $t("assistant.planEditor.roleImageAgentUnavailable")
                  }}</strong>
                  <small>{{
                    $t("assistant.planEditor.roleImageAgentFixed")
                  }}</small>
                </div>
                <label class="field">
                  <span>{{
                    $t("assistant.planEditor.roleImageEnvironment")
                  }}</span>
                  <select
                    :value="fieldValue(operation, 'environmentKey')"
                    :disabled="
                      !editable ||
                      roleImageCatalogProblem ||
                      !roleImageEnvironments.length
                    "
                    @change="setField(operation, 'environmentKey', $event)"
                  >
                    <option
                      v-if="
                        !roleImageEnvironments.some(
                          (item) =>
                            item.key ===
                            fieldValue(operation, 'environmentKey'),
                        )
                      "
                      :value="fieldValue(operation, 'environmentKey')"
                    >
                      {{ fieldValue(operation, "environmentKey") }}
                    </option>
                    <option
                      v-for="environment in roleImageEnvironments"
                      :key="environment.key"
                      :value="environment.key"
                      :disabled="!environment.available"
                    >
                      {{ $t(environment.nameMessageKey) }}
                      {{
                        environment.recommended
                          ? `· ${$t("roleEnvironments.recommended")}`
                          : ""
                      }}
                    </option>
                  </select>
                </label>
                <p
                  v-if="roleImageCatalogProblem"
                  class="field-error"
                  role="alert"
                >
                  {{ $t("assistant.planEditor.roleImageCatalogUnavailable") }}
                </p>
                <p class="assistant-plan-friendly__hint">
                  {{ $t("assistant.planEditor.roleImageNextSteps") }}
                </p>
              </template>
              <template v-else>
                <label class="field">
                  <span>{{ $t("assistant.planEditor.agentRole") }}</span>
                  <textarea
                    :value="fieldValue(operation, 'roleDescription')"
                    rows="3"
                    maxlength="2000"
                    :disabled="!editable"
                    @input="setField(operation, 'roleDescription', $event)"
                  />
                </label>
                <template v-if="operation.value.type === 'CREATE_AGENT'">
                  <label class="field">
                    <span>{{
                      $t("assistant.planEditor.agentInstructions")
                    }}</span>
                    <textarea
                      :value="fieldValue(operation, 'instructions')"
                      rows="7"
                      maxlength="65536"
                      :disabled="!editable"
                      @input="setField(operation, 'instructions', $event)"
                    />
                  </label>
                  <fieldset class="assistant-plan-friendly__capabilities">
                    <legend>
                      {{ $t("assistant.planEditor.agentCapabilities") }}
                    </legend>
                    <label v-for="key in initialCapabilities" :key="key">
                      <input
                        type="checkbox"
                        :checked="capabilityChecked(operation, key)"
                        :disabled="!editable"
                        @change="setCapability(operation, key, $event)"
                      />
                      {{
                        $t(
                          `assistant.planEditor.capabilities.${key.replaceAll(".", "_")}`,
                        )
                      }}
                    </label>
                  </fieldset>
                  <p class="assistant-plan-friendly__hint">
                    {{ $t("assistant.planEditor.agentNextSteps") }}
                  </p>
                </template>
              </template>
            </template>
            <details class="assistant-plan-friendly__snapshot">
              <summary>
                {{ $t("assistant.planEditor.transitionDetails") }}
              </summary>
              <h4>{{ $t("assistant.planEditor.before") }}</h4>
              <SafeStructuredData
                :value="snapshot(operation.beforeText)"
                literal
              />
              <h4>{{ $t("assistant.planEditor.afterDetails") }}</h4>
              <SafeStructuredData
                :value="snapshot(operation.afterText)"
                literal
              />
            </details>
          </div>

          <div
            v-if="!friendlyPlanOperationType(operation)"
            class="field field--code"
          >
            <span class="assistant-field-label">
              <span>{{ $t("assistant.planEditor.parameters") }}</span>
              <button
                class="icon-button"
                type="button"
                :aria-label="$t('assistant.planEditor.openFieldEditor')"
                :title="$t('assistant.planEditor.openFieldEditor')"
                @click.prevent="
                  editorTarget = { kind: 'PARAMETERS', operationIndex: index }
                "
              >
                <Maximize2 :size="15" aria-hidden="true" />
              </button>
            </span>
            <VoiceTextarea
              v-model="operation.parametersText"
              rows="4"
              spellcheck="false"
              :disabled="!editable"
              :aria-label="$t('assistant.planEditor.parameters')"
            />
          </div>
          <div
            v-if="!friendlyPlanOperationType(operation)"
            class="assistant-plan-transition"
          >
            <div class="field field--code">
              <span class="assistant-field-label">
                <span>{{ $t("assistant.planEditor.before") }}</span>
                <button
                  class="icon-button"
                  type="button"
                  :disabled="!editable"
                  :aria-label="$t('assistant.planEditor.openFieldEditor')"
                  :title="$t('assistant.planEditor.openFieldEditor')"
                  @click.prevent="
                    editorTarget = { kind: 'BEFORE', operationIndex: index }
                  "
                >
                  <Maximize2 :size="15" aria-hidden="true" />
                </button>
              </span>
              <VoiceTextarea
                v-model="operation.beforeText"
                rows="5"
                spellcheck="false"
                :disabled="!editable"
                :aria-label="$t('assistant.planEditor.before')"
              />
            </div>
            <div class="field field--code">
              <span class="assistant-field-label">
                <span>{{ $t("assistant.planEditor.after") }}</span>
                <button
                  class="icon-button"
                  type="button"
                  :aria-label="$t('assistant.planEditor.openFieldEditor')"
                  :title="$t('assistant.planEditor.openFieldEditor')"
                  @click.prevent="
                    editorTarget = { kind: 'AFTER', operationIndex: index }
                  "
                >
                  <Maximize2 :size="15" aria-hidden="true" />
                </button>
              </span>
              <VoiceTextarea
                v-model="operation.afterText"
                rows="4"
                spellcheck="false"
                :disabled="!editable"
                :aria-label="$t('assistant.planEditor.after')"
              />
            </div>
          </div>
          <p v-if="operation.value.unavailableReason" class="field-error">
            {{ operation.value.unavailableReason }}
          </p>
          <ul
            v-if="operation.value.validationProblems.length"
            class="assistant-validation-list"
          >
            <li
              v-for="validationProblem in operation.value.validationProblems"
              :key="validationProblem"
            >
              {{ validationProblem }}
            </li>
          </ul>
        </article>
      </div>
    </div>

    <footer class="assistant-plan-editor__footer">
      <span>
        {{
          $t("assistant.planEditor.selected", {
            selected: selectedCount,
            total: operations.length,
          })
        }}
      </span>
      <div>
        <button
          v-if="canReject"
          class="button button--danger"
          type="button"
          :disabled="busy"
          @click="emit('reject')"
        >
          <Trash2 :size="17" aria-hidden="true" />
          {{ $t("common.reject") }}
        </button>
        <button
          v-if="canSave"
          class="button"
          type="button"
          :disabled="busy"
          @click="save"
        >
          <Save :size="17" aria-hidden="true" />
          {{ $t("assistant.planEditor.saveRevision") }}
        </button>
        <button
          v-if="canValidate"
          class="button"
          type="button"
          :disabled="busy"
          @click="emit('validate')"
        >
          {{ $t("assistant.planEditor.validate") }}
        </button>
        <button
          v-if="canApply"
          class="button button--primary"
          type="button"
          :disabled="busy"
          @click="emit('apply')"
        >
          {{ $t("assistant.planEditor.apply") }}
        </button>
      </div>
    </footer>
    <AssistantCodeEditorModal
      v-if="editorTarget"
      :title="editorTitle"
      :model-value="editorValue"
      :language="editorLanguage"
      :object-required="editorLanguage === 'json'"
      :busy="busy || !editable"
      @close="editorTarget = undefined"
      @save="saveEditor"
    />
  </section>
</template>

<style scoped>
.assistant-plan-editor {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr) auto;
  height: 100%;
  min-height: 0;
}
.assistant-plan-editor__header,
.assistant-plan-editor__footer {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--border);
}
.assistant-plan-editor__header > div {
  flex: 1;
  min-width: 0;
}
.assistant-plan-editor__header h2,
.assistant-plan-editor__header p {
  margin: 0;
}
.assistant-plan-editor__header p,
.assistant-plan-editor__footer > span {
  color: var(--muted);
  font-size: 0.82rem;
}
.assistant-plan-editor__body {
  min-height: 0;
  overflow: auto;
  padding: 16px;
}
.assistant-field-label {
  display: flex;
  min-height: 32px;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.assistant-field-label .icon-button {
  width: 30px;
  height: 30px;
}
.assistant-plan-notice,
.assistant-plan-receipt {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
  padding: 10px 12px;
  border: 1px solid var(--accent);
  border-radius: 8px;
  background: var(--accent-soft);
}
.assistant-plan-receipt p {
  margin: 0;
}
.assistant-plan-conflict {
  margin-bottom: 14px;
  padding: 12px;
  border: 1px solid var(--warning);
  border-radius: 8px;
  background: var(--warning-soft);
}
.assistant-plan-conflict header {
  display: flex;
  gap: 10px;
}
.assistant-plan-conflict h3,
.assistant-plan-conflict p {
  margin: 0;
}
.assistant-plan-conflict dl {
  display: grid;
  gap: 8px;
  margin-bottom: 0;
}
.assistant-plan-conflict dd {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 4px 12px;
  margin: 0;
}
.assistant-plan-operations {
  display: grid;
  gap: 12px;
  margin-top: 14px;
}
.assistant-plan-operation {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-left: 4px solid var(--accent);
  border-radius: 8px;
  background: var(--surface);
}
.assistant-plan-operation--delete {
  border-left-color: var(--danger);
}
.assistant-plan-operation--update {
  border-left-color: var(--warning);
}
.assistant-plan-operation > header,
.assistant-plan-operation__select,
.assistant-plan-editor__footer,
.assistant-plan-editor__footer > div {
  display: flex;
  align-items: center;
  gap: 8px;
}
.assistant-plan-operation > header {
  justify-content: space-between;
}
.assistant-operation-kind {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  font-weight: 700;
}
.assistant-plan-operation__number {
  color: var(--subtle);
  font-size: 0.78rem;
}
.assistant-plan-operation__identity {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
  padding: 9px 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
}
.assistant-plan-operation__identity dt,
.assistant-plan-operation__identity dd {
  overflow-wrap: anywhere;
  font-family: var(--font-mono);
  font-size: 0.75rem;
}
.assistant-plan-operation__identity dt {
  color: var(--subtle);
}
.assistant-plan-operation__identity dd {
  margin: 3px 0 0;
}
.assistant-plan-target {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin: 0;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
}
.assistant-plan-target legend {
  padding-inline: 4px;
  color: var(--muted);
  font-size: 0.82rem;
}
.assistant-plan-target__summary {
  display: grid;
  grid-column: 1 / -1;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px 12px;
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--border-strong);
  border-radius: 6px;
  background: var(--surface);
}
.assistant-plan-target__summary strong {
  min-width: 0;
  overflow-wrap: anywhere;
}
.assistant-plan-target__summary small {
  color: var(--muted);
  font-family: var(--font-mono);
  font-size: 0.72rem;
}
.assistant-plan-target__action {
  padding: 3px 7px;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 0.75rem;
  font-weight: 700;
  white-space: nowrap;
}
.assistant-plan-friendly {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-plan-friendly__hint {
  margin: 0;
  color: var(--muted);
  font-size: 0.83rem;
}
.assistant-plan-friendly__capabilities {
  display: grid;
  gap: 8px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
}
.assistant-plan-friendly__capabilities legend {
  color: var(--muted);
  font-size: 0.83rem;
}
.assistant-plan-friendly__capabilities label {
  display: flex;
  align-items: center;
  gap: 8px;
}
.assistant-plan-friendly__snapshot {
  min-width: 0;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
}
.assistant-plan-friendly__snapshot summary {
  cursor: pointer;
  font-weight: 600;
}
.assistant-plan-friendly__snapshot h4 {
  margin: 12px 0 6px;
}
.field--code :deep(textarea) {
  font-family: var(--font-mono);
  font-size: 0.78rem;
}
.assistant-plan-transition {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.assistant-validation-list,
.field-error {
  margin: 0;
  color: var(--danger);
}
.assistant-plan-editor__footer {
  justify-content: space-between;
  border-top: 1px solid var(--border);
  border-bottom: 0;
}
@media (max-width: 640px) {
  .assistant-plan-editor__footer,
  .assistant-plan-editor__footer > div {
    align-items: stretch;
    flex-direction: column;
  }
  .assistant-plan-editor__footer .button {
    width: 100%;
  }
  .assistant-plan-target {
    grid-template-columns: 1fr;
  }
  .assistant-plan-target__summary {
    grid-template-columns: 1fr;
  }
  .assistant-plan-target__action {
    width: fit-content;
  }
  .assistant-plan-operation__identity,
  .assistant-plan-transition {
    grid-template-columns: 1fr;
  }
}
</style>
