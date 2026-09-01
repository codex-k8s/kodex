<script setup lang="ts">
import type { CompletionSource } from "@codemirror/autocomplete";
import { StreamLanguage } from "@codemirror/language";
import { toml } from "@codemirror/legacy-modes/mode/toml";
import { lintGutter, setDiagnostics } from "@codemirror/lint";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, placeholder as editorPlaceholder } from "@codemirror/view";
import { FileCode2, LockKeyhole, ShieldAlert } from "@lucide/vue";
import { basicSetup } from "codemirror";
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  codeEditorContentAttributes,
  codeEditorDiagnostics,
  codeMirrorPhrases,
  markdownStreamParser,
  templateCompletionQuery,
  type CodeEditorCompletionProvider,
} from "@/features/agents/detail/code-editor";

const props = withDefaults(
  defineProps<{
    modelValue: string;
    language: "markdown" | "toml";
    label: string;
    description?: string;
    placeholder?: string;
    readonly?: boolean;
    validationMessages?: readonly string[];
    minLines?: number;
    completionProvider?: CodeEditorCompletionProvider;
  }>(),
  {
    description: "",
    minLines: 12,
    placeholder: "",
    readonly: false,
    validationMessages: () => [],
    completionProvider: undefined,
  },
);

const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const { locale } = useI18n();
const editorId = useId();
const helpId = `${editorId}-help`;
const validationId = `${editorId}-validation`;
const editorRoot = ref<HTMLElement>();
const lineCount = computed(
  () => props.modelValue.replace(/\r\n?/g, "\n").split("\n").length,
);
const editorStyle = computed<Record<string, string>>(() => ({
  "--editor-lines": String(Math.max(props.minLines, lineCount.value)),
}));
const editableConfiguration = new Compartment();
const languageConfiguration = new Compartment();
const completionConfiguration = new Compartment();
const placeholderConfiguration = new Compartment();
const phrasesConfiguration = new Compartment();
let view: EditorView | undefined;

function editableExtension() {
  return [
    EditorState.readOnly.of(props.readonly),
    EditorView.editable.of(!props.readonly),
    EditorView.contentAttributes.of(
      codeEditorContentAttributes({
        label: props.label,
        readonly: props.readonly,
        invalid: props.validationMessages.length > 0,
        describedBy: props.validationMessages.length ? validationId : helpId,
        errorMessageId: validationId,
      }),
    ),
  ];
}

function languageExtension() {
  return StreamLanguage.define(
    props.language === "toml" ? toml : markdownStreamParser,
  );
}

const completionSource: CompletionSource = async (context) => {
  const provider = props.completionProvider;
  if (!provider) return null;
  const match = templateCompletionQuery(
    context.state.doc.toString(),
    context.pos,
    context.explicit,
  );
  if (!match) return null;

  const controller = new AbortController();
  context.addEventListener("abort", () => controller.abort());
  try {
    const items = await provider(match.query, controller.signal);
    if (controller.signal.aborted) return null;
    return {
      from: match.from,
      options: items.map((item) => ({
        label: item.label,
        apply: item.apply ?? item.label,
        ...(item.detail ? { detail: item.detail } : {}),
        ...(item.type ? { type: item.type } : {}),
      })),
    };
  } catch {
    return null;
  }
};

function completionExtension() {
  return props.completionProvider
    ? EditorState.languageData.of(() => [{ autocomplete: completionSource }])
    : [];
}

function placeholderExtension() {
  return props.placeholder ? editorPlaceholder(props.placeholder) : [];
}

function applyDiagnostics(): void {
  if (!view) return;
  view.dispatch(
    setDiagnostics(
      view.state,
      codeEditorDiagnostics(props.validationMessages, view.state.doc.length),
    ),
  );
}

const theme = EditorView.theme({
  "&": {
    height: "clamp(240px, calc(var(--editor-lines) * 1.55em + 28px), 440px)",
    minHeight: "240px",
    backgroundColor: "transparent",
    color: "var(--text)",
    fontSize: "12.5px",
  },
  "&.cm-focused": {
    outline: "2px solid color-mix(in srgb, var(--accent) 42%, transparent)",
    outlineOffset: "-2px",
  },
  ".cm-scroller": {
    fontFamily: "var(--font-mono)",
    lineHeight: "1.55",
  },
  ".cm-content": { padding: "14px 0", caretColor: "var(--text)" },
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
  ".cm-placeholder": { color: "var(--subtle)" },
  ".cm-diagnostic-error": { borderLeftColor: "var(--danger)" },
});

function insertAtCursor(value: string): void {
  if (!view || props.readonly) return;
  const selection = view.state.selection.main;
  view.dispatch({
    changes: { from: selection.from, to: selection.to, insert: value },
    selection: { anchor: selection.from + value.length },
    scrollIntoView: true,
  });
  view.focus();
}

function focus(): void {
  view?.focus();
}

onMounted(() => {
  if (!editorRoot.value) return;
  view = new EditorView({
    parent: editorRoot.value,
    doc: props.modelValue,
    extensions: [
      basicSetup,
      lintGutter(),
      EditorState.tabSize.of(2),
      EditorView.lineWrapping,
      theme,
      editableConfiguration.of(editableExtension()),
      languageConfiguration.of(languageExtension()),
      completionConfiguration.of(completionExtension()),
      placeholderConfiguration.of(placeholderExtension()),
      phrasesConfiguration.of(
        EditorState.phrases.of(codeMirrorPhrases(locale.value)),
      ),
      EditorView.updateListener.of((update) => {
        if (update.docChanged)
          emit("update:modelValue", update.state.doc.toString());
      }),
    ],
  });
  applyDiagnostics();
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
      effects: editableConfiguration.reconfigure(editableExtension()),
    });
  },
);
watch(
  () => props.language,
  () =>
    view?.dispatch({
      effects: languageConfiguration.reconfigure(languageExtension()),
    }),
);
watch(
  () => props.completionProvider,
  () =>
    view?.dispatch({
      effects: completionConfiguration.reconfigure(completionExtension()),
    }),
);
watch(
  () => props.placeholder,
  () =>
    view?.dispatch({
      effects: placeholderConfiguration.reconfigure(placeholderExtension()),
    }),
);
watch(locale, (value) =>
  view?.dispatch({
    effects: phrasesConfiguration.reconfigure(
      EditorState.phrases.of(codeMirrorPhrases(value)),
    ),
  }),
);
watch(
  () => props.validationMessages,
  () => applyDiagnostics(),
  { deep: true },
);

onBeforeUnmount(() => {
  view?.destroy();
  view = undefined;
});

defineExpose({ focus, insertAtCursor });
</script>

<template>
  <div
    class="code-editor"
    :class="{ 'code-editor--readonly': readonly }"
    :style="editorStyle"
  >
    <div class="code-editor__bar">
      <FileCode2 :size="16" aria-hidden="true" />
      <strong>{{ label }}</strong>
      <code :id="helpId">
        {{ description || (language === "toml" ? "TOML" : "Markdown") }}
      </code>
      <span class="code-editor__spacer" />
      <LockKeyhole v-if="readonly" :size="15" aria-hidden="true" />
    </div>
    <div ref="editorRoot" class="code-editor__viewport" />
    <div class="code-editor__foot" aria-live="polite">
      <span class="mono">{{ lineCount }} · {{ modelValue.length }}</span>
      <span
        v-if="validationMessages.length"
        :id="validationId"
        class="code-editor__validation"
      >
        <ShieldAlert :size="14" aria-hidden="true" />
        {{ validationMessages.join(" · ") }}
      </span>
      <span v-else class="code-editor__spacer" />
    </div>
  </div>
</template>

<style scoped>
.code-editor {
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  background: var(--panel);
}
.code-editor__bar,
.code-editor__foot {
  display: flex;
  min-height: 36px;
  align-items: center;
  gap: 8px;
  padding: 7px 11px;
  color: var(--muted);
  font-size: 0.78rem;
}
.code-editor__bar {
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.code-editor__bar strong {
  color: var(--text);
}
.code-editor__bar code {
  color: var(--accent-strong);
  font-family: var(--font-mono);
}
.code-editor__spacer {
  flex: 1;
}
.code-editor__viewport {
  min-height: 240px;
  background: color-mix(in srgb, var(--panel) 84%, var(--canvas));
}
.code-editor__viewport :deep(.cm-panels) {
  border-color: var(--border);
  color: var(--text);
  background: var(--surface);
}
.code-editor__viewport :deep(.cm-textfield),
.code-editor__viewport :deep(.cm-button) {
  color: var(--text);
  background: var(--panel);
}
.code-editor__viewport :deep(.cm-tooltip) {
  border-color: var(--border-strong);
  color: var(--text);
  background: var(--panel);
}
.code-editor__foot {
  min-height: 34px;
  border-top: 1px solid var(--border);
  background: var(--surface);
}
.code-editor__validation {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 5px;
  color: var(--danger);
  overflow-wrap: anywhere;
}
.code-editor--readonly .code-editor__viewport {
  opacity: 0.92;
}
@media (max-width: 640px) {
  .code-editor__bar,
  .code-editor__foot {
    padding-inline: 8px;
  }
}
</style>
