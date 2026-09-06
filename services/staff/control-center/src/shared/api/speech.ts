import {
  getBootstrapState,
  transcribeOrganizationSpeech,
} from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { csrfToken } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

export async function transcribeAudio(
  audio: Blob,
  signal: AbortSignal,
): Promise<string> {
  const result = await unwrap(
    transcribeOrganizationSpeech({
      body: { audio },
      headers: { "X-CSRF-Token": csrfToken(), "X-Audio-Size": audio.size },
      signal: AbortSignal.any([signal, AbortSignal.timeout(120_000)]),
    }),
  );
  if (typeof result.data.text !== "string")
    throw new Error("Invalid transcription response");
  return result.data.text;
}

export async function readSpeechAvailability(signal: AbortSignal) {
  return (await unwrap(getBootstrapState({ signal: requestSignal(signal) })))
    .data.speechTranscription;
}
