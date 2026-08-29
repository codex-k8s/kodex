<script setup lang="ts">
import { computed, ref, watch } from "vue";

import {
  tokenizeAssistantCodeLine,
  validateAssistantObjectJSON,
} from "@/features/assistant/code-editor";
import ModalDialog from "@/shared/ui/ModalDialog.vue";

const props = withDefaults(
  defineProps<{
    title: string;
    modelValue: string;
    language?: "json" | "text";
    objectRequired?: boolean;
    busy?: boolean;
  }>(),
  { language: "text", objectRequired: false, busy: false },
);
const emit = defineEmits<{ close: []; save: [value: string] }>();
const draft = ref(props.modelValue);
const editor = ref<HTMLTextAreaElement>();
const highlight = ref<HTMLElement>();
const gutter = ref<HTMLElement>();

watch(
  () => props.modelValue,
  (value) => (draft.value = value),
);

const lines = computed(() => draft.value.replace(/\r\n?/g, "\n").split("\n"));
const highlightedLines = computed(() =>
  lines.value.map((line) => tokenizeAssistantCodeLine(line, props.language)),
);
const valid = computed(
  () => !props.objectRequired || validateAssistantObjectJSON(draft.value),
);

function syncScroll(): void {
  if (!editor.value) return;
  if (highlight.value) {
    highlight.value.scrollLeft = editor.value.scrollLeft;
    highlight.value.scrollTop = editor.value.scrollTop;
  }
  if (gutter.value) gutter.value.scrollTop = editor.value.scrollTop;
}

function save(): void {
  if (valid.value && !props.busy) emit("save", draft.value);
}
</script>

<template>
  <Teleport to="body">
    <div class="assistant-code-editor-layer">
      <ModalDialog :title="title" size="xl" :busy="busy" @close="emit('close')">
        <div class="assistant-code-editor">
          <pre
            ref="gutter"
            class="assistant-code-editor__gutter"
            aria-hidden="true"
          ><span
        v-for="(_, index) in lines"
        :key="index"
      >{{ index + 1 }}</span></pre>
          <div class="assistant-code-editor__stack">
            <pre
              ref="highlight"
              class="assistant-code-editor__highlight"
              aria-hidden="true"
            ><code><span
          v-for="(line, lineIndex) in highlightedLines"
          :key="lineIndex"
          class="assistant-code-editor__line"
        ><span
          v-for="(token, tokenIndex) in line"
          :key="tokenIndex"
          :class="`assistant-code-editor__token--${token.tone}`"
        >{{ token.text }}</span>{{ lineIndex < highlightedLines.length - 1 ? "\n" : "" }}</span></code></pre>
            <textarea
              ref="editor"
              v-model="draft"
              class="assistant-code-editor__input"
              :aria-label="title"
              :aria-invalid="!valid || undefined"
              spellcheck="false"
              @scroll="syncScroll"
            />
          </div>
        </div>
        <p v-if="!valid" class="field-error" role="alert">
          {{ $t("assistant.planEditor.jsonError") }}
        </p>
        <template #actions>
          <button
            class="button"
            type="button"
            :disabled="busy"
            @click="emit('close')"
          >
            {{ $t("common.cancel") }}
          </button>
          <button
            class="button button--primary"
            type="button"
            :disabled="busy || !valid"
            @click="save"
          >
            {{ $t("common.save") }}
          </button>
        </template>
      </ModalDialog>
    </div>
  </Teleport>
</template>

<style scoped>
.assistant-code-editor-layer {
  position: fixed;
  z-index: 90;
  inset: 0;
  pointer-events: none;
}
.assistant-code-editor-layer :deep(.modal-backdrop) {
  pointer-events: auto;
}
.assistant-code-editor {
  display: grid;
  min-height: min(64vh, 680px);
  grid-template-columns: 52px minmax(0, 1fr);
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-code-editor__gutter,
.assistant-code-editor__highlight,
.assistant-code-editor__input {
  padding-block: 16px;
  margin: 0;
  border: 0;
  border-radius: 0;
  font-family: var(--font-mono);
  font-size: 13px;
  line-height: 1.6;
  white-space: pre;
}
.assistant-code-editor__gutter {
  display: flex;
  overflow: hidden;
  flex-direction: column;
  padding-inline: 10px;
  border-right: 1px solid var(--border);
  color: var(--subtle);
  text-align: right;
  user-select: none;
}
.assistant-code-editor__stack {
  position: relative;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.assistant-code-editor__highlight,
.assistant-code-editor__input {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  padding-inline: 16px;
  overflow: auto;
}
.assistant-code-editor__highlight {
  pointer-events: none;
  color: var(--text);
}
.assistant-code-editor__input {
  z-index: 1;
  resize: none;
  outline: 0;
  background: transparent;
  caret-color: var(--text);
  color: transparent;
  -webkit-text-fill-color: transparent;
}
.assistant-code-editor__input::selection {
  background: color-mix(in srgb, var(--accent) 28%, transparent);
}
.assistant-code-editor__input:focus-visible {
  box-shadow: inset 0 0 0 2px color-mix(in srgb, var(--accent) 42%, transparent);
}
.assistant-code-editor__token--key {
  color: var(--accent-strong);
  font-weight: 600;
}
.assistant-code-editor__token--string {
  color: var(--success);
}
.assistant-code-editor__token--number {
  color: var(--warning);
}
.assistant-code-editor__token--keyword {
  color: var(--danger);
}
.field-error {
  margin: 10px 0 0;
}
@media (max-width: 640px) {
  .assistant-code-editor {
    min-height: 62vh;
    grid-template-columns: 38px minmax(0, 1fr);
  }
}
</style>
