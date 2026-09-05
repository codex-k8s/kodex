<script setup lang="ts">
import {
  Eye,
  FilePenLine,
  LoaderCircle,
  RefreshCw,
  Save,
  ShieldCheck,
  Sparkles,
} from "@lucide/vue";
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  createTemplateVariableLoader,
  loadMaterializedTemplatePreview,
} from "@/features/agents/detail/api";
import CodeEditorSurface from "@/features/agents/detail/CodeEditorSurface.vue";
import { agentDetailCopy } from "@/features/agents/detail/copy";
import type {
  CodeEditorCompletionItem,
  CodeEditorCompletionProvider,
} from "@/features/agents/detail/code-editor";
import {
  extractTemplateVariables,
  templateVariableInsertion,
  type TemplateVariablePickerItem,
} from "@/features/agents/detail/model";
import TemplateVariableCatalog from "@/features/agents/detail/TemplateVariableCatalog.vue";
import type { PromptTemplatePreview } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  modelValue: string;
  state: "DRAFT" | "VALID" | "INVALID" | "PUBLISHED";
  validationMessages: readonly string[];
  canEdit: boolean;
  canValidate: boolean;
  canPublish: boolean;
  busy: boolean;
  dirty: boolean;
  projectRef: string;
  agentRef?: string;
  runtimeRevisionRef?: string;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: string];
  save: [];
  validate: [];
  publish: [];
}>();
const { locale } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
const mode = ref<"edit" | "preview" | "materialized">("edit");
const editor = shallowRef<{
  insertAtCursor(value: string): void;
}>();
const materializedPreview = ref<PromptTemplatePreview>();
const materializedTemplate = ref("");
const materializedBusy = ref(false);
const materializedProblem = ref<AppProblem>();
let materializedController: AbortController | undefined;
const usedVariables = computed(() =>
  extractTemplateVariables(props.modelValue),
);
const loadVariables = computed(() =>
  createTemplateVariableLoader(props.projectRef, {
    agentRef: props.agentRef,
    runtimeRevisionRef: props.runtimeRevisionRef,
  }),
);
const materializedContent = computed(
  () =>
    materializedPreview.value?.fullMaterializedPrompt ??
    materializedPreview.value?.safePreview ??
    "",
);
const materializedStale = computed(
  () =>
    !materializedPreview.value ||
    materializedTemplate.value !== props.modelValue,
);

function insertVariable(item: TemplateVariablePickerItem): void {
  if (!props.canEdit || props.busy || item.disabled) return;
  editor.value?.insertAtCursor(templateVariableInsertion(item.variable));
}

function selectMode(value: "edit" | "preview" | "materialized"): void {
  mode.value = value;
  if (value === "materialized" && materializedStale.value)
    void refreshMaterializedPreview();
}

async function refreshMaterializedPreview(): Promise<void> {
  if (!props.modelValue.trim() || materializedBusy.value) return;
  materializedController?.abort();
  const controller = new AbortController();
  const template = props.modelValue;
  materializedController = controller;
  materializedBusy.value = true;
  materializedProblem.value = undefined;
  try {
    const preview = await loadMaterializedTemplatePreview(
      template,
      controller.signal,
    );
    if (controller.signal.aborted || materializedController !== controller)
      return;
    materializedPreview.value = preview;
    materializedTemplate.value = template;
  } catch (error) {
    if (!controller.signal.aborted)
      materializedProblem.value = asProblem(error);
  } finally {
    if (materializedController === controller) {
      materializedController = undefined;
      materializedBusy.value = false;
    }
  }
}

const completeVariables: CodeEditorCompletionProvider = async (
  query,
  signal,
): Promise<CodeEditorCompletionItem[]> => {
  const loader = loadVariables.value;
  const page = await loader({ cursor: undefined, query, signal });
  if (signal.aborted || loader !== loadVariables.value) return [];
  return page.items
    .filter((item) => !item.disabled)
    .map((item) => ({
      label: item.variable.name,
      apply: templateVariableInsertion(item.variable),
      detail: [
        item.scope,
        item.variable.valueType,
        item.variable.description,
        item.variable.example,
      ]
        .filter(Boolean)
        .join(" · "),
      type: "variable",
    }));
};

function invalidatePreview(): void {
  materializedController?.abort();
  materializedController = undefined;
  materializedBusy.value = false;
  materializedPreview.value = undefined;
  materializedProblem.value = undefined;
}
watch(
  () => [
    props.modelValue,
    props.projectRef,
    props.agentRef,
    props.runtimeRevisionRef,
  ],
  invalidatePreview,
  {
    flush: "sync",
  },
);
onBeforeUnmount(invalidatePreview);
</script>

<template>
  <article class="instructions-panel panel">
    <div class="instructions-panel__head">
      <div>
        <h2>{{ $t("agents.instructions") }}</h2>
        <p>{{ copy.instructions.markdown }}</p>
      </div>
      <StatusBadge :state="dirty ? 'DRAFT' : state" />
    </div>
    <div
      class="instructions-panel__modes"
      role="group"
      :aria-label="$t('common.details')"
    >
      <button
        class="instructions-panel__mode"
        type="button"
        :aria-pressed="mode === 'edit'"
        @click="selectMode('edit')"
      >
        <FilePenLine :size="15" aria-hidden="true" />{{
          copy.instructions.editor
        }}
      </button>
      <button
        class="instructions-panel__mode"
        type="button"
        :aria-pressed="mode === 'preview'"
        @click="selectMode('preview')"
      >
        <Eye :size="15" aria-hidden="true" />{{ copy.instructions.preview }}
      </button>
      <button
        class="instructions-panel__mode"
        type="button"
        :aria-pressed="mode === 'materialized'"
        @click="selectMode('materialized')"
      >
        <Sparkles :size="15" aria-hidden="true" />
        {{ copy.instructions.materializedPreview }}
      </button>
    </div>

    <div class="instructions-panel__workspace">
      <div class="instructions-panel__editor">
        <CodeEditorSurface
          v-if="mode === 'edit'"
          ref="editor"
          :model-value="modelValue"
          language="markdown"
          :label="$t('agents.instructions')"
          :description="copy.instructions.markdown"
          :readonly="!canEdit"
          :validation-messages="validationMessages"
          :min-lines="18"
          :completion-provider="completeVariables"
          @update:model-value="emit('update:modelValue', $event)"
        />
        <section
          v-else-if="mode === 'preview'"
          class="instructions-panel__preview"
          aria-live="polite"
        >
          <div class="instructions-panel__preview-bar">
            <Eye :size="15" aria-hidden="true" />{{ copy.instructions.preview }}
          </div>
          <SafeMarkdown :content="modelValue" />
        </section>
        <section
          v-else
          class="instructions-panel__preview instructions-panel__preview--materialized"
          aria-live="polite"
          :aria-busy="materializedBusy"
        >
          <div class="instructions-panel__preview-bar">
            <Sparkles :size="15" aria-hidden="true" />
            <span>{{ copy.instructions.materializedPreview }}</span>
            <StatusBadge :state="materializedStale ? 'DRAFT' : 'AVAILABLE'" />
            <button
              class="button"
              type="button"
              :disabled="materializedBusy || !modelValue.trim()"
              @click="refreshMaterializedPreview"
            >
              <RefreshCw :size="15" aria-hidden="true" />
              {{ copy.instructions.refreshPreview }}
            </button>
          </div>
          <p class="instructions-panel__materialized-help">
            {{ copy.instructions.materializedHelp }}
          </p>
          <div
            v-if="materializedBusy"
            class="instructions-panel__materialized-state"
            role="status"
          >
            <LoaderCircle class="spin" :size="18" aria-hidden="true" />
            {{ $t("common.loading") }}
          </div>
          <ProblemNotice
            v-else-if="materializedProblem"
            :problem="materializedProblem"
            compact
          />
          <SafeMarkdown
            v-else-if="materializedContent && !materializedStale"
            :content="materializedContent"
          />
          <p v-else class="instructions-panel__materialized-state">
            {{ copy.instructions.materializedUnavailable }}
          </p>
          <ul
            v-if="materializedPreview?.diagnostics.length"
            class="instructions-panel__materialized-diagnostics"
          >
            <li
              v-for="diagnostic in materializedPreview.diagnostics"
              :key="
                diagnostic.code +
                '-' +
                diagnostic.line +
                '-' +
                diagnostic.column
              "
            >
              <strong>{{ diagnostic.severity }}</strong>
              {{ diagnostic.message }} · {{ diagnostic.line }}:{{
                diagnostic.column
              }}
            </li>
          </ul>
        </section>

        <div v-if="canEdit" class="instructions-panel__actions">
          <button
            class="button"
            type="button"
            :disabled="busy || !dirty || !modelValue.trim()"
            @click="emit('save')"
          >
            <Save :size="16" aria-hidden="true" />{{
              copy.instructions.saveDraft
            }}
          </button>
          <button
            v-if="canValidate"
            class="button"
            type="button"
            :disabled="busy || dirty"
            @click="emit('validate')"
          >
            <ShieldCheck :size="16" aria-hidden="true" />{{
              $t("agents.validate")
            }}
          </button>
          <button
            v-if="canPublish"
            class="button button--primary"
            type="button"
            :disabled="busy || dirty || state !== 'VALID'"
            @click="emit('publish')"
          >
            {{ $t("agents.publish") }}
          </button>
        </div>
      </div>

      <aside class="instructions-panel__variables">
        <div class="instructions-panel__variables-head">
          <h3>{{ copy.instructions.variables }}</h3>
          <StatusBadge state="AVAILABLE" />
        </div>
        <p>{{ copy.instructions.variablesHelp }}</p>
        <TemplateVariableCatalog
          :agent-ref="agentRef"
          :runtime-revision-ref="runtimeRevisionRef"
          :project-ref="projectRef"
          :disabled="!canEdit || busy || mode !== 'edit'"
          @select="insertVariable"
        />
        <div class="instructions-panel__used">
          <strong>{{ copy.instructions.usedVariables }}</strong>
          <div v-if="usedVariables.length" class="instructions-panel__tokens">
            <code v-for="variable in usedVariables" :key="variable">{{
              variable
            }}</code>
          </div>
          <span v-else>{{ copy.instructions.noVariables }}</span>
        </div>
      </aside>
    </div>

    <section
      v-if="validationMessages.length"
      class="instructions-panel__validation"
    >
      <h3>{{ copy.instructions.validation }}</h3>
      <ul>
        <li v-for="message in validationMessages" :key="message">
          {{ message }}
        </li>
      </ul>
    </section>
    <slot name="history" />
  </article>
</template>

<style scoped>
.instructions-panel {
  display: grid;
  gap: 14px;
}
.instructions-panel__head,
.instructions-panel__variables-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.instructions-panel h2,
.instructions-panel h3,
.instructions-panel p {
  margin: 0;
}
.instructions-panel__head p,
.instructions-panel__variables > p {
  margin-top: 4px;
  color: var(--muted);
  font-size: 0.82rem;
}
.instructions-panel__modes {
  display: inline-flex;
  width: max-content;
  max-width: 100%;
  flex-wrap: wrap;
  border: 1px solid var(--border);
  border-radius: 7px;
}
.instructions-panel__mode {
  display: inline-flex;
  min-height: 32px;
  flex: 1 1 auto;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  border: 0;
  border-right: 1px solid var(--border);
  color: var(--muted);
  background: var(--surface);
  cursor: pointer;
  white-space: normal;
  overflow-wrap: anywhere;
}
.instructions-panel__mode:last-child {
  border-right: 0;
}
.instructions-panel__mode[aria-pressed="true"] {
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.instructions-panel__workspace {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(330px, 0.42fr);
  gap: 14px;
  align-items: start;
}
.instructions-panel__editor {
  min-width: 0;
}
.instructions-panel__preview {
  min-height: 458px;
  overflow: auto;
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  background: var(--surface);
}
.instructions-panel__preview-bar {
  display: flex;
  flex-wrap: wrap;
  min-height: 36px;
  align-items: center;
  gap: 6px;
  padding: 7px 11px;
  border-bottom: 1px solid var(--border);
  color: var(--muted);
  font-size: 0.78rem;
}
.instructions-panel__preview-bar .status-badge {
  margin-left: auto;
}
.instructions-panel__preview-bar .button {
  min-height: 30px;
  padding: 4px 8px;
}
.instructions-panel__preview :deep(.safe-markdown) {
  padding: 16px;
}
.instructions-panel__materialized-help {
  padding: 12px 16px 0;
  color: var(--muted);
  font-size: 0.8rem;
}
.instructions-panel__materialized-state {
  display: flex;
  min-height: 280px;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 20px;
  color: var(--muted);
  text-align: center;
}
.instructions-panel__materialized-diagnostics {
  display: grid;
  gap: 6px;
  padding: 12px 32px 16px;
  margin: 0;
  color: var(--warning);
  font-size: 0.78rem;
}
.instructions-panel__variables {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.instructions-panel__variables h3 {
  font-size: 0.92rem;
}
.instructions-panel__used {
  display: grid;
  gap: 8px;
  padding-top: 10px;
  border-top: 1px solid var(--border);
}
.instructions-panel__used > span {
  color: var(--subtle);
  font-size: 0.78rem;
}
.instructions-panel__tokens {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.instructions-panel__tokens code {
  padding: 3px 5px;
  border: 1px solid color-mix(in srgb, var(--accent) 25%, var(--border));
  border-radius: 4px;
  color: var(--accent-strong);
  background: var(--accent-soft);
  font-family: var(--font-mono);
  font-size: 0.74rem;
  overflow-wrap: anywhere;
}
.instructions-panel__variable-option {
  display: grid;
  width: 100%;
  min-width: 0;
  gap: 6px;
}
.instructions-panel__variable-option > span:first-child {
  display: grid;
  gap: 3px;
}
.instructions-panel__variable-meta {
  display: flex;
  min-width: 0;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}
.instructions-panel__variable-fields {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 3px;
}
.instructions-panel__variable-fields code {
  padding: 2px 4px;
  border-radius: 4px;
  background: var(--canvas);
  font-size: 0.7rem;
}
.instructions-panel__variable-option strong,
.instructions-panel__variable-option small {
  overflow-wrap: anywhere;
}
.instructions-panel__variable-option small {
  color: var(--muted);
  font-size: 0.72rem;
}
.instructions-panel__variable-option small code {
  color: inherit;
  font-size: inherit;
}
.instructions-panel__variable-meta code {
  color: var(--accent-strong);
  font-family: var(--font-mono);
  font-size: 0.7rem;
}
.instructions-panel__variable-meta span {
  color: var(--subtle);
  font-size: 0.7rem;
}
.instructions-panel__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
  padding-top: 12px;
}
.instructions-panel__validation {
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--danger) 35%, var(--border));
  border-radius: 8px;
  color: var(--danger);
  background: var(--danger-soft);
}
.instructions-panel__validation h3 {
  font-size: 0.88rem;
}
.instructions-panel__validation ul {
  margin: 7px 0 0;
  padding-left: 20px;
}
@media (max-width: 960px) {
  .instructions-panel__workspace {
    grid-template-columns: 1fr;
  }
}
</style>
