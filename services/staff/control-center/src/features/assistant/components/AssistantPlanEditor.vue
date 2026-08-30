<script setup lang="ts">
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
import {
  editableOperations,
  operationActionLabel,
  operationInputs,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import type {
  AssistantPlan,
  AssistantPlanOperationInput,
  AssistantPlanReceipt,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  plan: AssistantPlan;
  receipt?: AssistantPlanReceipt;
  busy?: boolean;
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
const summary = ref("");
const operations = ref<EditablePlanOperation[]>([]);
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
  inputProblem.value = "";
}

watch(() => props.plan, resetDraft, { immediate: true });

const selectedCount = computed(
  () => operations.value.filter((operation) => operation.value.selected).length,
);
const exactRevisionValidated = computed(
  () =>
    props.plan.state === "VALID" &&
    props.plan.validatedRevision === props.plan.revision,
);
const editable = computed(
  () => !props.busy && !["APPLIED", "REJECTED"].includes(props.plan.state),
);
const canSave = computed(
  () =>
    editable.value &&
    summary.value.trim().length > 0 &&
    selectedCount.value > 0 &&
    !["APPLIED", "REJECTED"].includes(props.plan.state),
);
const canValidate = computed(
  () =>
    !props.busy &&
    !["APPLIED", "REJECTED"].includes(props.plan.state) &&
    props.plan.state !== "VALID",
);
const canApply = computed(
  () =>
    !props.busy &&
    exactRevisionValidated.value &&
    props.plan.nextActions.includes("APPLY_PLAN"),
);
const canReject = computed(() => editable.value);

function save(): void {
  inputProblem.value = "";
  try {
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
        <textarea
          v-model="summary"
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
                {{ operationActionLabel(operation.value.action) }}
              </span>
            </label>
            <span class="assistant-plan-operation__number"
              >#{{ index + 1 }}</span
            >
          </header>

          <dl class="assistant-plan-operation__identity">
            <div>
              <dt>type</dt>
              <dd>{{ operation.value.type }}</dd>
            </div>
            <div>
              <dt>action</dt>
              <dd>{{ operation.value.action }}</dd>
            </div>
            <div>
              <dt>permitted</dt>
              <dd>{{ operation.value.permitted }}</dd>
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
            <textarea
              v-model="operation.value.summary"
              rows="2"
              maxlength="2000"
              :disabled="!editable"
              :aria-label="$t('assistant.planEditor.operationSummary')"
            />
          </div>

          <fieldset class="assistant-plan-target">
            <legend>{{ $t("assistant.planEditor.target") }}</legend>
            <label class="field">
              <span>kind</span>
              <input
                v-model="operation.value.target.kind"
                maxlength="120"
                :disabled="!editable"
              />
            </label>
            <label class="field">
              <span>name</span>
              <input
                v-model="operation.value.target.name"
                maxlength="300"
                :disabled="!editable"
              />
            </label>
            <label class="field">
              <span>ref</span>
              <input
                v-model="operation.value.target.ref"
                maxlength="300"
                :disabled="!editable"
              />
            </label>
            <label class="field">
              <span>version</span>
              <input
                type="number"
                min="0"
                step="1"
                :value="operation.value.target.version"
                :disabled="!editable"
                @input="operation.value.target.version = optionalNumber($event)"
              />
            </label>
            <label class="field">
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

          <div class="field field--code">
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
            <textarea
              v-model="operation.parametersText"
              rows="4"
              spellcheck="false"
              :disabled="!editable"
              :aria-label="$t('assistant.planEditor.parameters')"
            />
          </div>
          <div class="assistant-plan-transition">
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
              <textarea
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
              <textarea
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
  font-family: var(--font-mono);
  font-size: 0.78rem;
  text-transform: uppercase;
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
.field--code textarea {
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
  .assistant-plan-operation__identity,
  .assistant-plan-transition {
    grid-template-columns: 1fr;
  }
}
</style>
