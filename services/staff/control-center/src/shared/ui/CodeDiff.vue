<script setup lang="ts">
import { unifiedMergeView } from "@codemirror/merge";
import { EditorState } from "@codemirror/state";
import { EditorView, lineNumbers } from "@codemirror/view";
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
const props = defineProps<{
  original: string;
  modified: string;
  label: string;
}>();
const root = ref<HTMLElement>();
let view: EditorView | undefined;
function render(): void {
  view?.destroy();
  view = undefined;
  if (!root.value) return;
  view = new EditorView({
    parent: root.value,
    doc: props.modified,
    extensions: [
      lineNumbers(),
      EditorState.readOnly.of(true),
      EditorView.editable.of(false),
      EditorView.lineWrapping,
      EditorView.contentAttributes.of({
        "aria-label": props.label,
        tabindex: "0",
      }),
      unifiedMergeView({
        original: props.original,
        mergeControls: false,
        syntaxHighlightDeletions: false,
        diffConfig: { scanLimit: 500, timeout: 50 },
        collapseUnchanged: { margin: 3, minSize: 8 },
      }),
      EditorView.theme({
        "&": { maxHeight: "420px", fontSize: "13px" },
        ".cm-scroller": { overflow: "auto", fontFamily: "var(--font-mono)" },
      }),
    ],
  });
}
onMounted(render);
watch(() => [props.original, props.modified, props.label], render);
onBeforeUnmount(() => {
  view?.destroy();
  view = undefined;
});
</script>
<template><div ref="root" class="code-diff" /></template>
<style scoped>
.code-diff {
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--surface);
}
</style>
