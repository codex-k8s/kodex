<script setup lang="ts">
import { StreamLanguage } from "@codemirror/language";
import { dockerFile } from "@codemirror/legacy-modes/mode/dockerfile";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { FileCode2, LockKeyhole, ShieldAlert } from "@lucide/vue";
import { basicSetup } from "codemirror";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

const props = withDefaults(
  defineProps<{
    modelValue: string;
    label: string;
    readonly?: boolean;
    validationMessages?: readonly string[];
  }>(),
  {
    readonly: false,
    validationMessages: () => [],
  },
);

const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const editorRoot = ref<HTMLElement>();
const lineCount = computed(
  () => props.modelValue.replace(/\r\n?/g, "\n").split("\n").length,
);
const editorConfiguration = new Compartment();
let view: EditorView | undefined;

function editableConfiguration() {
  return [
    EditorState.readOnly.of(props.readonly),
    EditorView.editable.of(!props.readonly),
    EditorView.contentAttributes.of({
      "aria-label": props.label,
      "aria-invalid": props.validationMessages.length ? "true" : "false",
      spellcheck: "false",
    }),
  ];
}

const theme = EditorView.theme({
  "&": {
    minHeight: "440px",
    backgroundColor: "transparent",
    color: "var(--text)",
    fontSize: "12.5px",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": {
    minHeight: "440px",
    fontFamily: "var(--font-mono)",
    lineHeight: "1.6",
  },
  ".cm-content": { padding: "15px 0", caretColor: "var(--text)" },
  ".cm-gutters": {
    backgroundColor: "var(--surface)",
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
    doc: props.modelValue,
    extensions: [
      basicSetup,
      StreamLanguage.define(dockerFile),
      EditorState.tabSize.of(2),
      EditorView.lineWrapping,
      theme,
      editorConfiguration.of(editableConfiguration()),
      EditorView.updateListener.of((update) => {
        if (!update.docChanged) return;
        emit("update:modelValue", update.state.doc.toString());
      }),
    ],
  });
});

watch(
  () => props.modelValue,
  (value) => {
    if (!view || value === view.state.doc.toString()) return;
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: value },
    });
  },
);

watch(
  () => [props.readonly, props.label, props.validationMessages.length],
  () => {
    view?.dispatch({
      effects: editorConfiguration.reconfigure(editableConfiguration()),
    });
  },
);

onBeforeUnmount(() => {
  view?.destroy();
  view = undefined;
});
</script>

<template>
  <div class="dockerfile-editor" :class="{ 'is-readonly': readonly }">
    <header>
      <FileCode2 :size="17" aria-hidden="true" />
      <strong>{{ label }}</strong>
      <code>Dockerfile</code>
      <span />
      <LockKeyhole v-if="readonly" :size="15" aria-hidden="true" />
    </header>
    <div ref="editorRoot" class="dockerfile-editor__viewport" />
    <footer aria-live="polite">
      <span class="mono">{{ lineCount }} · {{ modelValue.length }}</span>
      <span v-if="validationMessages.length" class="editor-errors">
        <ShieldAlert :size="14" aria-hidden="true" />
        {{ validationMessages.join(" · ") }}
      </span>
    </footer>
  </div>
</template>

<style scoped>
.dockerfile-editor {
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  background: var(--panel);
}
.dockerfile-editor > header,
.dockerfile-editor > footer {
  display: flex;
  min-height: 38px;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  color: var(--muted);
  font-size: 0.8rem;
}
.dockerfile-editor > header {
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.dockerfile-editor > header strong {
  color: var(--text);
}
.dockerfile-editor > header code {
  color: var(--accent-strong);
}
.dockerfile-editor > header > span {
  flex: 1;
}
.dockerfile-editor__viewport {
  min-height: 440px;
  background: color-mix(in srgb, var(--panel) 84%, var(--canvas));
}
.dockerfile-editor > footer {
  justify-content: space-between;
  border-top: 1px solid var(--border);
  background: var(--surface);
}
.editor-errors {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--danger);
}
.is-readonly .dockerfile-editor__viewport {
  opacity: 0.92;
}
@media (max-width: 640px) {
  .dockerfile-editor__viewport {
    min-height: 340px;
  }
}
</style>
