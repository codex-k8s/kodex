import { describe, expect, it, vi } from "vitest";
const sdk = vi.hoisted(() => ({ transcribe: vi.fn(), bootstrap: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  transcribeOrganizationSpeech: sdk.transcribe,
  getBootstrapState: sdk.bootstrap,
}));
vi.mock("@/shared/api/mutation", () => ({ csrfToken: () => "synthetic-csrf" }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import { readSpeechAvailability, transcribeAudio } from "./speech";
describe("speech API", () => {
  it("отправляет audio ровно один раз без projectRef, model или credential", async () => {
    const audio = new Blob(["synthetic"], { type: "audio/webm" });
    sdk.transcribe.mockResolvedValue({
      data: { text: "Тест" },
      response: new Response(null, { status: 200 }),
    });
    await expect(
      transcribeAudio(audio, new AbortController().signal),
    ).resolves.toBe("Тест");
    expect(sdk.transcribe).toHaveBeenCalledOnce();
    const request = sdk.transcribe.mock.calls[0]?.[0] as {
      body: unknown;
      headers: unknown;
      signal: AbortSignal;
    };
    expect(request.signal).toBeInstanceOf(AbortSignal);
    expect(Object.keys(request).sort()).toEqual(["body", "headers", "signal"]);
    expect(request.body).toEqual({ audio });
    expect(request.headers).toEqual({
      "X-CSRF-Token": "synthetic-csrf",
      "X-Audio-Size": audio.size,
    });
  });
  it("читает только серверную доступность и не повторяет неоднозначную транскрипцию", async () => {
    sdk.bootstrap.mockResolvedValue({
      data: {
        speechTranscription: { available: false, reason: "STT_DISABLED" },
      },
      response: new Response(null, { status: 200 }),
    });
    await expect(
      readSpeechAvailability(new AbortController().signal),
    ).resolves.toEqual({ available: false, reason: "STT_DISABLED" });
    sdk.transcribe.mockRejectedValue(new Error("Ambiguous provider outcome"));
    await expect(
      transcribeAudio(new Blob(["synthetic"]), new AbortController().signal),
    ).rejects.toThrow();
    expect(sdk.transcribe).toHaveBeenCalledOnce();
  });
});
