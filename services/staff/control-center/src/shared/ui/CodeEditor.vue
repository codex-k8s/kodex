<script setup lang="ts">
import { StreamLanguage } from "@codemirror/language";
import { json } from "@codemirror/lang-json";
import { linter } from "@codemirror/lint";
import { jsonSyntaxIssue } from "@/shared/ui/json-diagnostic";
import { yaml } from "@codemirror/legacy-modes/mode/yaml";
import { toml } from "@codemirror/legacy-modes/mode/toml";
import { dockerFile } from "@codemirror/legacy-modes/mode/dockerfile";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { basicSetup } from "codemirror";
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  codeEditorKeymap,
  insertVoiceText,
} from "@/shared/ui/code-editor-keymap";
import VoiceInputButton from "@/shared/ui/VoiceInputButton.vue";

const props = defineProps<{
  modelValue: string;
  label: string;
  language?: "json" | "yaml" | "toml" | "dockerfile";
  readonly?: boolean;
  disabled?: boolean;
  sensitive?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [text: string] }>();
const { t } = useI18n();
const root = ref<HTMLElement>();
const configuration = new Compartment();
let view: EditorView | undefined;
function extensions() {
  const parser =
    props.language && props.language !== "json"
      ? { yaml, toml, dockerfile: dockerFile }[props.language]
      : undefined;
  return [
    EditorState.readOnly.of(props.readonly || props.disabled),
    EditorView.editable.of(!props.readonly && !props.disabled),
    EditorView.contentAttributes.of({
      "aria-label": props.label,
      "aria-description": t("app.editorKeyboard"),
      spellcheck: "false",
      autocomplete: "off",
      autocorrect: "off",
    }),
    ...(parser ? [StreamLanguage.define(parser)] : []),
    ...(props.language === "json"
      ? [
          json(),
          linter((editor) => {
            const issue = jsonSyntaxIssue(editor.state.doc.toString());
            return issue
              ? [
                  {
                    from: issue.from,
                    to: issue.to,
                    severity: "error" as const,
                    message: t("common.invalidJsonAt", issue),
                  },
                ]
              : [];
          }),
        ]
      : []),
  ];
}
onMounted(() => {
  if (!root.value) return;
  view = new EditorView({
    parent: root.value,
    doc: props.modelValue,
    extensions: [
      basicSetup,
      codeEditorKeymap,
      EditorState.tabSize.of(2),
      EditorView.lineWrapping,
      configuration.of(extensions()),
      EditorView.theme({
        "&": { height: "280px", fontSize: "13px" },
        ".cm-scroller": { fontFamily: "var(--font-mono)" },
      }),
      EditorView.updateListener.of((update) => {
        if (update.docChanged)
          emit("update:modelValue", update.state.doc.toString());
      }),
    ],
  });
});
watch(
  () => props.modelValue,
  (value) => {
    if (view && value !== view.state.doc.toString())
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: value },
      });
  },
);
watch(
  () => [props.readonly, props.disabled, props.language, props.label],
  () => view?.dispatch({ effects: configuration.reconfigure(extensions()) }),
);
onBeforeUnmount(() => {
  view?.destroy();
  view = undefined;
});
</script>
<template>
  <div
    class="code-editor-voice shared-code-editor"
    :title="$t('app.editorKeyboard')"
  >
    <div ref="root" />
    <VoiceInputButton
      :sensitive="sensitive"
      :readonly="readonly"
      :disabled="disabled"
      @transcript="insertVoiceText(view, $event)"
    />
  </div>
</template>
<style scoped>
.shared-code-editor {
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--surface);
}
</style>
