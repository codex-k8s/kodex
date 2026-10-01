import { onBeforeUnmount, onMounted, provide, ref, watch } from "vue";
import { usePlatformStore } from "@/features/platform/store";
import { useSessionStore } from "@/features/session/store";
import { readSpeechAvailability, transcribeAudio } from "@/shared/api/speech";
import { voiceContextKey } from "@/shared/ui/voice-input";
import { SpeechAvailabilityLease } from "./availability";

export function useSpeechInput(): void {
  const platform = usePlatformStore();
  const session = useSessionStore();
  const available = ref(false);
  const lease = new SpeechAvailabilityLease({
    read: readSpeechAvailability,
    changed: (value) => {
      available.value = value;
    },
  });
  let mounted = false;
  provide(voiceContextKey, {
    available,
    transcribe(audio, signal) {
      if (!lease.available)
        throw new Error("Speech transcription eligibility expired");
      return transcribeAudio(audio, signal);
    },
  });
  function synchronize(): void {
    if (mounted && session.phase === "authenticated")
      lease.start(platform.bootstrap?.speechTranscription, false);
    else lease.stop();
  }
  watch(() => session.phase, synchronize, { flush: "sync" });
  watch(
    () => platform.bootstrap?.speechTranscription,
    (value) => lease.synchronize(value),
    { flush: "sync" },
  );
  onMounted(() => {
    mounted = true;
    synchronize();
  });
  onBeforeUnmount(() => {
    mounted = false;
    lease.stop();
  });
}
