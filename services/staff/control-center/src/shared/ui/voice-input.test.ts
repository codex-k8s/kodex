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
  const transcribe = vi.fn().mockResolvedValue("synthetic transcript");
  const available = vi.fn(() => true);
  const capture = new VoiceCapture({
    available,
    insert,
    transcribe,
    changed: vi.fn(),
  });
  return {
    capture,
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
    const { capture, transcribe } = setup();
    transcribe.mockRejectedValue(new Error("provider error"));
    await capture.start();
    capture.stop();
    await Promise.resolve();
    expect(capture.state).toBe("error");
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
});
