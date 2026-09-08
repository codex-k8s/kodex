import { afterEach, describe, expect, it, vi } from "vitest";
import { VoiceCapture } from "./voice-input";

class Recorder {
  static current: Recorder;
  static isTypeSupported() {
    return true;
  }
  state = "inactive";
  mimeType = "audio/webm";
  ondataavailable: ((event: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor() {
    Recorder.current = this;
  }
  start() {
    this.state = "recording";
  }
  stop() {
    this.state = "inactive";
    this.ondataavailable?.({ data: new Blob(["synthetic audio"]) });
    this.onstop?.();
  }
}

function setup() {
  vi.useFakeTimers();
  const track = { stop: vi.fn(), onended: null };
  const stream = { getTracks: () => [track] };
  const getUserMedia = vi.fn().mockResolvedValue(stream);
  vi.stubGlobal("navigator", { mediaDevices: { getUserMedia } });
  vi.stubGlobal("MediaRecorder", Recorder);
  const insert = vi.fn();
  const transcribe = vi
    .fn<(audio: Blob, signal: AbortSignal) => Promise<string>>()
    .mockResolvedValue("synthetic transcript");
  const available = vi.fn(() => true);
  const failed = vi.fn();
  const capture = new VoiceCapture({
    available,
    insert,
    transcribe,
    changed: vi.fn(),
    failed,
  });
  return {
    capture,
    failed,
    insert,
    transcribe,
    available,
    track,
    getUserMedia,
    stream,
  };
}
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
describe("VoiceCapture", () => {
  it("отправляет одну запись и освобождает tracks до распознавания", async () => {
    const { capture, track, insert, transcribe } = setup();
    await capture.start();
    expect(capture.state).toBe("recording");
    capture.stop();
    expect(track.stop).toHaveBeenCalled();
    await Promise.resolve();
    expect(transcribe).toHaveBeenCalledOnce();
    expect(insert).toHaveBeenCalledWith("synthetic transcript");
    expect(capture.state).toBe("idle");
  });
  it("не получает микрофон без availability", async () => {
    const { capture, available, getUserMedia } = setup();
    available.mockReturnValue(false);
    await capture.start();
    expect(getUserMedia).not.toHaveBeenCalled();
  });
  it("отмена во время запроса разрешения останавливает поздний stream", async () => {
    const { capture, getUserMedia, track, stream, transcribe } = setup();
    let resolve!: (value: unknown) => void;
    getUserMedia.mockReturnValue(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const starting = capture.start();
    capture.cancel();
    resolve(stream);
    await starting;
    expect(track.stop).toHaveBeenCalledOnce();
    expect(transcribe).not.toHaveBeenCalled();
  });
  it("не вставляет поздний transcript после cancel", async () => {
    const { capture, transcribe, insert } = setup();
    let resolve!: (value: string) => void;
    transcribe.mockReturnValue(
      new Promise<string>((r) => {
        resolve = r;
      }),
    );
    await capture.start();
    capture.stop();
    capture.cancel();
    resolve("late");
    await Promise.resolve();
    expect(insert).not.toHaveBeenCalled();
    expect(capture.state).toBe("idle");
  });
  it("ошибка провайдера не запускает автоматический повтор", async () => {
    const { capture, transcribe, failed } = setup();
    const error = {
      status: 429,
      code: "TRANSCRIPTION_RATE_LIMITED",
      retryAfterSeconds: 30,
    };
    transcribe.mockRejectedValue(error);
    await capture.start();
    capture.stop();
    await Promise.resolve();
    expect(capture.state).toBe("error");
    expect(failed).toHaveBeenCalledWith(error);
    await vi.advanceTimersByTimeAsync(120_000);
    expect(transcribe).toHaveBeenCalledOnce();
    capture.cancel();
  });
  it("отзывает запись при превышении лимита без отправки audio", async () => {
    const { capture, transcribe, track } = setup();
    await capture.start();
    Recorder.current.ondataavailable?.({
      data: new Blob([new Uint8Array(10 * 1024 * 1024 + 1)]),
    });
    expect(capture.state).toBe("error");
    expect(transcribe).not.toHaveBeenCalled();
    expect(track.stop).toHaveBeenCalled();
  });
  it("не отправляет audio при неожиданном browser stop до track ended", async () => {
    const { capture, transcribe, track, failed } = setup();
    await capture.start();
    Recorder.current.stop();
    await Promise.resolve();
    expect(capture.state).toBe("error");
    expect(failed).toHaveBeenCalledWith(
      expect.objectContaining({ code: "AUDIO_CAPTURE_INTERRUPTED" }),
    );
    expect(track.stop).toHaveBeenCalled();
    expect(transcribe).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("лимит длительности закрывает capture без billable отправки", async () => {
    const { capture, transcribe, track, failed } = setup();
    await capture.start();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(capture.state).toBe("error");
    expect(failed).toHaveBeenCalledWith(
      expect.objectContaining({ code: "AUDIO_LIMIT_EXCEEDED" }),
    );
    expect(transcribe).not.toHaveBeenCalled();
    expect(track.stop).toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });
  it.each(["NotAllowedError", "SecurityError"])(
    "browser deny %s не сохраняет исходную диагностику",
    async (name) => {
      const { capture, getUserMedia, failed, transcribe } = setup();
      getUserMedia.mockRejectedValue(
        new DOMException("Synthetic private diagnostic", name),
      );
      await capture.start();
      expect(failed).toHaveBeenCalledWith(
        expect.objectContaining({
          code: "MICROPHONE_PERMISSION_DENIED",
          message: "MICROPHONE_PERMISSION_DENIED",
        }),
      );
      expect(transcribe).not.toHaveBeenCalled();
    },
  );
  it("не запрашивает микрофон без codec", async () => {
    const { capture, getUserMedia, failed } = setup();
    const support = vi
      .spyOn(Recorder, "isTypeSupported")
      .mockReturnValue(false);
    await capture.start();
    support.mockRestore();
    expect(getUserMedia).not.toHaveBeenCalled();
    expect(failed).toHaveBeenCalledWith(
      expect.objectContaining({ code: "AUDIO_FORMAT_UNSUPPORTED" }),
    );
  });
  it("без MediaRecorder возвращает безопасную причину", async () => {
    const { capture, getUserMedia, failed } = setup();
    vi.stubGlobal("MediaRecorder", undefined);
    await capture.start();
    expect(getUserMedia).not.toHaveBeenCalled();
    expect(failed).toHaveBeenCalledWith(
      expect.objectContaining({ code: "MICROPHONE_UNAVAILABLE" }),
    );
  });
  it("отзыв track закрывает запись и устаревший callback не отменяет новую", async () => {
    const { capture, transcribe, track } = setup();
    await capture.start();
    const ended = track.onended as (() => void) | null;
    ended?.();
    expect(capture.state).toBe("error");
    expect(transcribe).not.toHaveBeenCalled();
    await capture.start();
    ended?.();
    expect(capture.state).toBe("recording");
    capture.cancel();
  });
  it("повторный stop не создаёт вторую отправку", async () => {
    const { capture, transcribe } = setup();
    await capture.start();
    capture.stop();
    capture.stop();
    await Promise.resolve();
    expect(transcribe).toHaveBeenCalledOnce();
  });
  it("отмена abort-ит request, освобождает tracks и не допускает позднюю ошибку", async () => {
    const { capture, transcribe, failed } = setup();
    let reject!: (error: Error) => void;
    transcribe.mockReturnValue(
      new Promise<string>((_, fail) => {
        reject = fail;
      }),
    );
    await capture.start();
    capture.stop();
    const signal = transcribe.mock.calls[0]?.[1];
    capture.cancel();
    reject(new Error("Synthetic late failure"));
    await Promise.resolve();
    expect(signal?.aborted).toBe(true);
    expect(failed).not.toHaveBeenCalled();
    expect(capture.state).toBe("idle");
  });
});
