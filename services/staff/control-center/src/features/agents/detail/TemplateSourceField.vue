<script setup lang="ts">
import { computed } from "vue";
import CodeEditorSurface from "./CodeEditorSurface.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import {
  createPromptVariableLoader,
  type PromptTarget,
} from "./prompt-context";
import { templateVariableInsertion } from "./model";
import type { CodeEditorCompletionProvider } from "./code-editor";
const props = defineProps<{
  modelValue: string;
  label: string;
  disabled?: boolean;
  target?: PromptTarget;
}>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const loader = computed(
  () => props.target && createPromptVariableLoader(props.target),
);
const enabled = () => !props.disabled;
const complete: CodeEditorCompletionProvider = async (query, signal) => {
  const selected = loader.value;
  if (!selected || !enabled()) return [];
  const page = await selected({ query, signal });
  if (signal.aborted || selected !== loader.value || !enabled()) return [];
  return page.items
    .filter((item) => !item.disabled)
    .map((item) => ({
      label: item.variable.name,
      apply: templateVariableInsertion(item.variable),
      detail: [
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
  <div class="shared-code-editor">
    <CodeEditorSurface
      :model-value="modelValue"
      :label="label"
      language="markdown"
      :readonly="disabled"
      :completion-provider="complete"
      :min-lines="5"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <details>
      <summary>{{ $t("promptDetails.markdownPreview") }}</summary>
      <SafeMarkdown :content="modelValue" />
    </details>
  </div>
</template>
