<script setup lang="ts">
import { Mic, Square, X, LoaderCircle, RotateCcw } from "@lucide/vue";
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { routeLocationKey } from "vue-router";
import {
  VoiceCapture,
  voiceContextKey,
  type VoiceState,
} from "@/shared/ui/voice-input";
const props = defineProps<{
  disabled?: boolean;
  readonly?: boolean;
  sensitive?: boolean;
}>();
const emit = defineEmits<{ transcript: [text: string] }>();
const context = inject(voiceContextKey, undefined);
const route = inject(routeLocationKey, undefined);
const root = ref<HTMLElement>();
const inheritedDisabled = ref(false);
let observer: MutationObserver | undefined;
onMounted(() => {
  const fieldsets: HTMLFieldSetElement[] = [];
  for (
    let element = root.value?.parentElement;
    element;
    element = element.parentElement
  ) {
    if (element instanceof HTMLFieldSetElement) fieldsets.push(element);
  }
  const update = () => {
    inheritedDisabled.value = fieldsets.some((fieldset) => fieldset.disabled);
  };
  observer = new MutationObserver(update);
  for (const fieldset of fieldsets)
    observer.observe(fieldset, {
      attributes: true,
      attributeFilter: ["disabled"],
    });
  update();
});
const state = ref<VoiceState>("idle");
const visible = computed(
  () =>
    Boolean(context?.available.value) &&
    !inheritedDisabled.value &&
    !props.disabled &&
    !props.readonly &&
    !props.sensitive,
);
const capture = new VoiceCapture({
  available: () => visible.value,
  transcribe: (audio, signal) => {
    if (!context) throw new Error("Speech context is unavailable");
    return context.transcribe(audio, signal);
  },
  insert: (text) => emit("transcript", text),
  changed: (value) => {
    state.value = value;
  },
});
watch(
  visible,
  (value) => {
    if (!value) capture.cancel();
  },
  { flush: "sync" },
);
watch(
  () => route?.fullPath,
  () => capture.cancel(),
);
onBeforeUnmount(() => {
  observer?.disconnect();
  capture.cancel();
});
</script>
<template>
  <span ref="root" class="voice-input" :data-state="state">
    <template v-if="visible">
      <span v-if="state === 'error'" class="voice-input__error" role="alert">{{
        $t("voice.error")
      }}</span>
      <button
        class="icon-button"
        type="button"
        :title="$t(`voice.${state}`)"
        :aria-label="$t(`voice.${state}`)"
        :disabled="state === 'requesting' || state === 'transcribing'"
        @mousedown.prevent
        @click="state === 'recording' ? capture.stop() : capture.start()"
      >
        <Square v-if="state === 'recording'" :size="17" /><LoaderCircle
          v-else-if="state === 'requesting' || state === 'transcribing'"
          :size="17"
        /><RotateCcw v-else-if="state === 'error'" :size="17" /><Mic
          v-else
          :size="17"
        />
      </button>
      <button
        v-if="state !== 'idle'"
        class="icon-button"
        type="button"
        :title="$t('common.cancel')"
        :aria-label="$t('common.cancel')"
        @mousedown.prevent
        @click="capture.cancel()"
      >
        <X :size="17" />
      </button>
    </template>
  </span>
</template>
<style scoped>
.voice-input {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.voice-input:empty {
  display: none;
}
.voice-input__error {
  max-width: 240px;
  color: var(--danger);
  font-size: 12px;
}
.voice-input[data-state="recording"] {
  color: var(--danger);
}
</style>
