<script setup lang="ts">
import { StreamLanguage } from "@codemirror/language";
import { json } from "@codemirror/legacy-modes/mode/javascript";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { basicSetup } from "codemirror";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

import { validateAssistantObjectJSON } from "@/features/assistant/code-editor";
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
const editorRoot = ref<HTMLElement>();
const editableConfiguration = new Compartment();
const languageConfiguration = new Compartment();
let view: EditorView | undefined;

watch(
  () => props.modelValue,
  (value) => {
    draft.value = value;
    if (!view || value === view.state.doc.toString()) return;
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: value },
    });
  },
);

const valid = computed(
  () => !props.objectRequired || validateAssistantObjectJSON(draft.value),
);

function editableExtension() {
  return [
    EditorState.readOnly.of(props.busy),
    EditorView.editable.of(!props.busy),
    EditorView.contentAttributes.of({
      "aria-label": props.title,
      spellcheck: "false",
    }),
  ];
}

function languageExtension() {
  return props.language === "json" ? StreamLanguage.define(json) : [];
}

function save(): void {
  if (valid.value && !props.busy) emit("save", draft.value);
}

const theme = EditorView.theme({
  "&": {
    height: "min(68vh, 720px)",
    minHeight: "420px",
    backgroundColor: "var(--surface)",
    color: "var(--text)",
    fontSize: "13px",
  },
  "&.cm-focused": {
    outline: "2px solid color-mix(in srgb, var(--accent) 42%, transparent)",
    outlineOffset: "-2px",
  },
  ".cm-scroller": {
    fontFamily: "var(--font-mono)",
    lineHeight: "1.6",
  },
  ".cm-content": { padding: "14px 0", caretColor: "var(--text)" },
  ".cm-gutters": {
    backgroundColor: "var(--panel)",
    borderRight: "1px solid var(--border)",
    color: "var(--subtle)",
  },
  ".cm-activeLine, .cm-activeLineGutter": {
    backgroundColor: "color-mix(in srgb, var(--accent-soft) 46%, transparent)",
  },
  ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
    backgroundColor: "color-mix(in srgb, var(--accent) 24%, transparent)",
  },
});

onMounted(() => {
  if (!editorRoot.value) return;
  view = new EditorView({
    parent: editorRoot.value,
    doc: draft.value,
    extensions: [
      basicSetup,
      EditorState.tabSize.of(2),
      EditorView.lineWrapping,
      theme,
      editableConfiguration.of(editableExtension()),
      languageConfiguration.of(languageExtension()),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) draft.value = update.state.doc.toString();
      }),
    ],
  });
});

watch(
  () => [props.busy, props.title],
  () =>
    view?.dispatch({
      effects: editableConfiguration.reconfigure(editableExtension()),
    }),
);
watch(
  () => props.language,
  () =>
    view?.dispatch({
      effects: languageConfiguration.reconfigure(languageExtension()),
    }),
);
onBeforeUnmount(() => {
  view?.destroy();
  view = undefined;
});
</script>

<template>
  <Teleport to="body">
    <div class="assistant-code-editor-layer">
      <ModalDialog :title="title" size="xl" :busy="busy" @close="emit('close')">
        <div
          ref="editorRoot"
          class="assistant-code-editor"
          :class="{ 'assistant-code-editor--invalid': !valid }"
        />
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
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-radius: 8px;
}
.assistant-code-editor--invalid {
  border-color: var(--danger);
}
.field-error {
  margin: 10px 0 0;
}
@media (max-width: 640px) {
  .assistant-code-editor :deep(.cm-editor) {
    min-height: 56vh;
  }
}
</style>
