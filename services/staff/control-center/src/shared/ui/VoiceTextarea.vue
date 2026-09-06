<script setup lang="ts">
import { computed, inject, ref, useAttrs } from "vue";
import VoiceInputButton from "@/shared/ui/VoiceInputButton.vue";
import { voiceContextKey } from "@/shared/ui/voice-input";
defineOptions({ inheritAttrs: false });
const props = defineProps<{
  modelValue?: string | number;
  sensitive?: boolean;
  disabled?: boolean;
  readonly?: boolean;
  modelModifiers?: { trim?: boolean };
}>();
const emit = defineEmits<{ "update:modelValue": [text: string] }>();
const field = ref<HTMLTextAreaElement>();
const attrs = useAttrs();
const value = computed(
  () =>
    props.modelValue ??
    (typeof attrs.value === "string" || typeof attrs.value === "number"
      ? attrs.value
      : ""),
);
const context = inject(voiceContextKey, undefined);
const enabled = computed(
  () =>
    Boolean(context?.available.value) &&
    !props.disabled &&
    !props.readonly &&
    !props.sensitive,
);
function update(event: Event): void {
  const value = (event.target as HTMLTextAreaElement).value;
  emit("update:modelValue", props.modelModifiers?.trim ? value.trim() : value);
}
function insert(text: string): void {
  const target = field.value;
  if (!target || !enabled.value || target.matches(":disabled")) return;
  const scrollTop = target.scrollTop;
  target.focus({ preventScroll: true });
  // insertText сохраняет нативную историю undo; setRangeText остаётся fallback для браузеров без этой команды.
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- Стандартной замены с сохранением native undo для textarea пока нет (MDN execCommand).
  if (!document.execCommand("insertText", false, text)) {
    target.setRangeText(
      text,
      target.selectionStart,
      target.selectionEnd,
      "end",
    );
    target.dispatchEvent(new Event("input", { bubbles: true }));
  }
  target.scrollTop = scrollTop;
}
defineExpose({ focus: () => field.value?.focus() });
</script>
<template>
  <span class="voice-textarea" :class="{ 'voice-textarea--enabled': enabled }">
    <textarea
      ref="field"
      v-bind="attrs"
      :value="value"
      :disabled="disabled"
      :readonly="readonly"
      @input="update"
    />
    <VoiceInputButton
      class="voice-textarea__action"
      :sensitive="sensitive"
      :disabled="disabled"
      :readonly="readonly"
      @transcript="insert"
    />
  </span>
</template>
<style scoped>
.voice-textarea {
  display: block;
  position: relative;
  min-width: 0;
  width: 100%;
}
.voice-textarea textarea {
  display: block;
  width: 100%;
}
.voice-textarea:has(.voice-input > button) textarea {
  padding-bottom: 50px;
}
.voice-textarea__action {
  position: absolute;
  bottom: 9px;
  right: 18px;
}
</style>
