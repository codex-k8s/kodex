<script setup lang="ts">
import { Eye, FilePenLine, Save, ShieldCheck } from "@lucide/vue";
import { computed, ref, shallowRef } from "vue";
import { useI18n } from "vue-i18n";

import { createTemplateVariableLoader } from "@/features/agents/detail/api";
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
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
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
}>();
const emit = defineEmits<{
  "update:modelValue": [value: string];
  save: [];
  validate: [];
  publish: [];
}>();
const { locale, t } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
const mode = ref<"edit" | "preview">("edit");
const editor = shallowRef<{
  insertAtCursor(value: string): void;
}>();
const usedVariables = computed(() =>
  extractTemplateVariables(props.modelValue),
);
const loadVariables = createTemplateVariableLoader(props.projectRef);
const variableLabels = computed(() => ({
  label: copy.value.instructions.variables,
  searchPlaceholder: copy.value.instructions.variableSearch,
  loading: t("common.loading"),
  loadingMore: copy.value.environment.loadingMore,
  empty: t("common.empty"),
  error: t("common.error"),
  retry: t("common.retry"),
}));

function insertVariable(item: TemplateVariablePickerItem): void {
  editor.value?.insertAtCursor(templateVariableInsertion(item.variable));
}

const completeVariables: CodeEditorCompletionProvider = async (
  query,
  signal,
): Promise<CodeEditorCompletionItem[]> => {
  const page = await loadVariables({ cursor: undefined, query, signal });
  return page.items.map((item) => ({
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
        @click="mode = 'edit'"
      >
        <FilePenLine :size="15" aria-hidden="true" />{{
          copy.instructions.editor
        }}
      </button>
      <button
        class="instructions-panel__mode"
        type="button"
        :aria-pressed="mode === 'preview'"
        @click="mode = 'preview'"
      >
        <Eye :size="15" aria-hidden="true" />{{ copy.instructions.preview }}
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
        <section v-else class="instructions-panel__preview" aria-live="polite">
          <div class="instructions-panel__preview-bar">
            <Eye :size="15" aria-hidden="true" />{{ copy.instructions.preview }}
          </div>
          <SafeMarkdown :content="modelValue" />
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
        <AsyncEntityPicker
          :load-items="loadVariables"
          :labels="variableLabels"
          :disabled="!canEdit || busy || mode !== 'edit'"
          :debounce-ms="250"
          @select="insertVariable"
        >
          <template #option="{ item }">
            <span class="instructions-panel__variable-option">
              <span>
                <strong>{{ item.variable.name }}</strong>
                <small>{{ item.variable.description }}</small>
                <small v-if="item.variable.example">
                  {{ copy.instructions.variableExample }}:
                  <code>{{ item.variable.example }}</code>
                </small>
                <small v-if="item.variable.collection">
                  {{ copy.instructions.collection }} ·
                  {{ item.variable.itemValueType ?? item.variable.valueType }}
                </small>
                <span
                  v-if="item.variable.itemFields.length"
                  class="instructions-panel__variable-fields"
                >
                  <code
                    v-for="field in item.variable.itemFields"
                    :key="field.name"
                    :title="field.description"
                  >
                    {{ field.name }}: {{ field.valueType }}
                  </code>
                </span>
              </span>
              <span class="instructions-panel__variable-meta">
                <code>{{ item.scope }}</code>
                <span>{{ item.variable.valueType }}</span>
              </span>
            </span>
          </template>
        </AsyncEntityPicker>
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
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 7px;
}
.instructions-panel__mode {
  display: inline-flex;
  min-height: 32px;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  border: 0;
  border-right: 1px solid var(--border);
  color: var(--muted);
  background: var(--surface);
  cursor: pointer;
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
  grid-template-columns: minmax(0, 1fr) 300px;
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
  min-height: 36px;
  align-items: center;
  gap: 6px;
  padding: 7px 11px;
  border-bottom: 1px solid var(--border);
  color: var(--muted);
  font-size: 0.78rem;
}
.instructions-panel__preview :deep(.safe-markdown) {
  padding: 16px;
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
