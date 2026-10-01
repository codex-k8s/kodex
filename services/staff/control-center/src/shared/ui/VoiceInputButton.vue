<script setup lang="ts">
import { Check, LoaderCircle, Mic, RotateCcw, Square, X } from "@lucide/vue";
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { routeLocationKey } from "vue-router";
import { asProblem, type AppProblem } from "@/shared/api/problem";
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
const problem = ref<AppProblem>();
const problemMessage = computed(() => {
  const messages: Record<string, string> = {
    MICROPHONE_PERMISSION_DENIED: "voice.permissionDenied",
    MICROPHONE_UNAVAILABLE: "voice.microphoneUnavailable",
    AUDIO_CAPTURE_INTERRUPTED: "voice.interrupted",
    AUDIO_FORMAT_UNSUPPORTED: "voice.unsupportedFormat",
    UNSUPPORTED_MEDIA_TYPE: "voice.unsupportedFormat",
    AUDIO_LIMIT_EXCEEDED: "voice.limitExceeded",
    PAYLOAD_TOO_LARGE: "voice.limitExceeded",
    AUDIO_EMPTY: "voice.empty",
    FORBIDDEN: "voice.forbidden",
    STT_NOT_CONFIGURED: "voice.notConfigured",
    STT_CREDENTIAL_UNAVAILABLE: "voice.credentialUnavailable",
    STT_MODEL_UNAVAILABLE: "voice.modelUnavailable",
    TRANSCRIPTION_TIMEOUT: "voice.timeout",
    TRANSCRIPTION_PROVIDER_UNAVAILABLE: "voice.providerUnavailable",
  };
  return messages[problem.value?.code ?? ""] ?? "voice.error";
});
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
    if (value === "idle") problem.value = undefined;
  },
  failed: (error) => {
    problem.value = asProblem(error);
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
  { flush: "sync" },
);
const pageHidden = () => capture.cancel();
onMounted(() => window.addEventListener("pagehide", pageHidden));
onBeforeUnmount(() => {
  window.removeEventListener("pagehide", pageHidden);
  observer?.disconnect();
  capture.cancel();
});
</script>
<template>
  <span ref="root" class="voice-input" :data-state="state">
    <template v-if="visible">
      <span v-if="state === 'error'" class="voice-input__error" role="alert">
        {{
          problem?.code === "TRANSCRIPTION_RATE_LIMITED"
            ? problem.retryAfterSeconds
              ? $t("voice.rateLimitedWait", {
                  seconds: problem.retryAfterSeconds,
                })
              : $t("voice.rateLimited")
            : $t(problemMessage)
        }}
      </span>
      <span
        v-if="state === 'recording'"
        class="voice-input__recording-indicator"
        role="status"
        :aria-label="$t('voice.recordingActive')"
      >
        <i></i><i></i><i></i>
      </span>
      <button
        v-if="state === 'idle' || state === 'error'"
        class="voice-input__button"
        type="button"
        :title="$t(`voice.${state}`)"
        :aria-label="$t(`voice.${state}`)"
        @mousedown.prevent
        @click="capture.start()"
      >
        <RotateCcw v-if="state === 'error'" :size="19" />
        <Mic v-else :size="19" />
      </button>
      <button
        v-if="state === 'requesting' || state === 'transcribing'"
        class="voice-input__button voice-input__button--progress"
        type="button"
        :title="$t(`voice.${state}`)"
        :aria-label="$t(`voice.${state}`)"
        disabled
      >
        <LoaderCircle :size="19" />
      </button>
      <button
        v-if="state === 'requesting' || state === 'transcribing'"
        class="voice-input__button voice-input__button--cancel"
        type="button"
        :title="$t('voice.cancelRecording')"
        :aria-label="$t('voice.cancelRecording')"
        @mousedown.prevent
        @click="capture.cancel()"
      >
        <X :size="19" />
      </button>
      <button
        v-if="state === 'recording'"
        class="voice-input__button voice-input__button--cancel"
        type="button"
        :title="$t('voice.cancelRecording')"
        :aria-label="$t('voice.cancelRecording')"
        @mousedown.prevent
        @click="capture.cancel()"
      >
        <Square :size="17" fill="currentColor" />
      </button>
      <button
        v-if="state === 'recording'"
        class="voice-input__button voice-input__button--accept"
        type="button"
        :title="$t('voice.acceptRecording')"
        :aria-label="$t('voice.acceptRecording')"
        @mousedown.prevent
        @click="capture.stop()"
      >
        <Check :size="21" stroke-width="2.5" />
      </button>
    </template>
  </span>
</template>
<style scoped>
.voice-input {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.voice-input:empty {
  display: none;
}
.voice-input__error {
  max-width: 240px;
  color: var(--danger);
  font-size: 12px;
}
.voice-input__button {
  display: grid;
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  padding: 0;
  place-items: center;
  border: 1px solid transparent;
  border-radius: 50%;
  color: var(--accent-strong);
  background: var(--accent-soft);
  cursor: pointer;
}
.voice-input__button:disabled {
  cursor: default;
}
.voice-input__button--progress svg {
  animation: voice-input-spin 0.8s linear infinite;
}
.voice-input__button--cancel {
  color: var(--danger);
  border-color: color-mix(in srgb, var(--danger) 24%, transparent);
  background: var(--danger-soft);
}
.voice-input__button--accept {
  color: #fff;
  background: var(--accent);
}
.voice-input__recording-indicator {
  display: inline-flex;
  height: 40px;
  align-items: center;
  gap: 3px;
  padding: 0 4px;
  color: var(--danger);
}
.voice-input__recording-indicator::before {
  width: 8px;
  height: 8px;
  margin-right: 2px;
  border-radius: 50%;
  background: currentColor;
  box-shadow: 0 0 0 4px color-mix(in srgb, currentColor 14%, transparent);
  content: "";
  animation: voice-input-pulse 1.2s ease-in-out infinite;
}
.voice-input__recording-indicator i {
  display: block;
  width: 3px;
  height: 10px;
  border-radius: 2px;
  background: currentColor;
  animation: voice-input-level 0.8s ease-in-out infinite alternate;
}
.voice-input__recording-indicator i:nth-child(2) {
  height: 18px;
  animation-delay: -0.35s;
}
.voice-input__recording-indicator i:nth-child(3) {
  height: 13px;
  animation-delay: -0.6s;
}
@keyframes voice-input-spin {
  to {
    transform: rotate(360deg);
  }
}
@keyframes voice-input-pulse {
  50% {
    opacity: 0.45;
  }
}
@keyframes voice-input-level {
  to {
    transform: scaleY(0.45);
  }
}
@media (prefers-reduced-motion: reduce) {
  .voice-input__button--progress svg,
  .voice-input__recording-indicator::before,
  .voice-input__recording-indicator i {
    animation: none;
  }
}
</style>
