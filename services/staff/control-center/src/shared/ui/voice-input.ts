import type { InjectionKey, Ref } from "vue";

export type VoiceState =
  | "idle"
  | "requesting"
  | "recording"
  | "transcribing"
  | "error";
export interface VoiceContext {
  available: Readonly<Ref<boolean>>;
  transcribe(audio: Blob, signal: AbortSignal): Promise<string>;
}
export const voiceContextKey: InjectionKey<VoiceContext> =
  Symbol("voice-input");

let cancelActiveCapture: (() => void) | undefined;

export class VoiceCapture {
  private readonly cancelCapture = () => this.cancel();
  private generation = 0;
  private recorder?: MediaRecorder;
  private stream?: MediaStream;
  private controller?: AbortController;
  private chunks: Blob[] = [];
  private bytes = 0;
  private timer?: ReturnType<typeof setTimeout>;
  state: VoiceState = "idle";

  constructor(
    private readonly options: {
      available(): boolean;
      transcribe(audio: Blob, signal: AbortSignal): Promise<string>;
      insert(text: string): void;
      changed(state: VoiceState): void;
      maxBytes?: number;
      maxDurationMs?: number;
    },
  ) {}

  private setState(state: VoiceState): void {
    this.state = state;
    this.options.changed(state);
  }

  async start(): Promise<void> {
    if (!this.options.available() || !["idle", "error"].includes(this.state))
      return;
    this.cancel();
    cancelActiveCapture?.();
    cancelActiveCapture = this.cancelCapture;
    const generation = this.generation;
    this.setState("requesting");
    try {
      const mediaDevices = navigator.mediaDevices as MediaDevices | undefined;
      if (typeof MediaRecorder === "undefined" || !mediaDevices)
        throw new Error("Audio capture is unavailable");
      const mimeType = [
        "audio/webm;codecs=opus",
        "audio/ogg;codecs=opus",
        "audio/mp4",
      ].find((type) => MediaRecorder.isTypeSupported(type));
      if (!mimeType) throw new Error("Supported audio codec is unavailable");
      const stream = await mediaDevices.getUserMedia({
        audio: true,
        video: false,
      });
      if (generation !== this.generation || !this.options.available()) {
        stream.getTracks().forEach((track) => track.stop());
        if (generation === this.generation) this.cancel();
        return;
      }
      this.stream = stream;
      const recorder = new MediaRecorder(stream, { mimeType });
      this.recorder = recorder;
      recorder.ondataavailable = (event) => {
        if (generation !== this.generation) return;
        this.bytes += event.data.size;
        if (this.bytes > (this.options.maxBytes ?? 10 * 1024 * 1024)) {
          this.fail();
          return;
        }
        this.chunks.push(event.data);
      };
      recorder.onerror = () => {
        if (generation === this.generation) this.fail();
      };
      recorder.onstop = () => {
        void this.finish(generation, recorder.mimeType);
      };
      for (const track of stream.getTracks())
        track.onended = () => {
          if (this.state === "recording") this.fail();
        };
      recorder.start(250);
      this.setState("recording");
      this.timer = setTimeout(
        () => this.stop(),
        this.options.maxDurationMs ?? 120_000,
      );
    } catch {
      if (generation === this.generation) this.fail();
    }
  }

  stop(): void {
    if (this.state !== "recording") return;
    this.setState("transcribing");
    try {
      this.recorder?.stop();
    } catch {
      this.fail();
    } finally {
      this.releaseStream();
    }
  }

  private async finish(generation: number, mimeType: string): Promise<void> {
    if (generation !== this.generation) return;
    this.releaseStream();
    const blob = new Blob(this.chunks, { type: mimeType });
    this.chunks = [];
    if (
      !blob.size ||
      blob.size > (this.options.maxBytes ?? 10 * 1024 * 1024) ||
      !this.options.available()
    ) {
      this.fail();
      return;
    }
    const controller = new AbortController();
    this.controller = controller;
    try {
      const text = await this.options.transcribe(blob, controller.signal);
      if (
        generation !== this.generation ||
        controller.signal.aborted ||
        !this.options.available()
      )
        return;
      this.options.insert(text);
      this.cancel();
    } catch {
      if (generation === this.generation) this.fail();
    }
  }

  private releaseStream(): void {
    if (this.timer !== undefined) clearTimeout(this.timer);
    this.timer = undefined;
    this.stream?.getTracks().forEach((track) => {
      track.onended = null;
      track.stop();
    });
    this.stream = undefined;
  }

  cancel(): void {
    if (cancelActiveCapture === this.cancelCapture)
      cancelActiveCapture = undefined;
    this.generation += 1;
    this.controller?.abort();
    this.controller = undefined;
    if (this.recorder) {
      this.recorder.onstop = null;
      this.recorder.ondataavailable = null;
      this.recorder.onerror = null;
      try {
        if (this.recorder.state !== "inactive") this.recorder.stop();
      } catch {
        /* Уже остановленный browser recorder не удерживает поток и данные. */
      }
    }
    this.recorder = undefined;
    this.releaseStream();
    this.chunks = [];
    this.bytes = 0;
    this.setState("idle");
  }

  private fail(): void {
    this.cancel();
    this.setState("error");
  }
}
