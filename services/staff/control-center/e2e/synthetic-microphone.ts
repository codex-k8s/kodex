import type { BrowserContext, Page } from "@playwright/test";

// Chromium использует fake device, Firefox — WebAudio с настоящим recorder.
// WebKit получает отдельную модель recorder; hardware permission не проверяется.
export async function prepareSyntheticMicrophone(
  page: Page,
  context: BrowserContext,
  browserName: string,
  origin = "http://127.0.0.1:43122",
  recorderMode: "native" | "fixture" = "native",
): Promise<void> {
  if (recorderMode === "fixture") {
    // Явная модель lifecycle для UI; Blob не является закодированным аудио.
    await page.addInitScript(() => {
      class FixtureRecorder extends EventTarget {
        static isTypeSupported(type: string): boolean {
          return type === "audio/webm;codecs=opus";
        }
        readonly mimeType: string;
        state: RecordingState = "inactive";
        ondataavailable?: (event: BlobEvent) => void;
        onstop?: (event: Event) => void;
        constructor(_stream: MediaStream, options?: MediaRecorderOptions) {
          super();
          this.mimeType = options?.mimeType ?? "audio/webm;codecs=opus";
        }
        start(): void {
          this.state = "recording";
        }
        stop(): void {
          if (this.state === "inactive") return;
          this.state = "inactive";
          setTimeout(() => {
            const event = new Event("dataavailable");
            Object.defineProperty(event, "data", {
              value: new Blob(["synthetic-ui-capture"], {
                type: this.mimeType,
              }),
            });
            this.ondataavailable?.(event as BlobEvent);
            this.onstop?.(new Event("stop"));
          }, 0);
        }
      }
      window.MediaRecorder = FixtureRecorder as unknown as typeof MediaRecorder;
      // WebKit может пересоздать wrapper mediaDevices между обращениями.
      Object.defineProperty(MediaDevices.prototype, "getUserMedia", {
        configurable: true,
        value: () => Promise.resolve(new MediaStream()),
      });
    });
    return;
  }
  if (browserName === "chromium") {
    await context.grantPermissions(["microphone"], {
      origin,
    });
    return;
  }
  await page.addInitScript(() => {
    navigator.mediaDevices.getUserMedia = async () => {
      const audio = new AudioContext();
      const oscillator = audio.createOscillator();
      const destination = audio.createMediaStreamDestination();
      oscillator.connect(destination);
      oscillator.start();
      await audio.resume();
      let stopped = false;
      for (const track of destination.stream.getTracks()) {
        const stop = track.stop.bind(track);
        track.stop = () => {
          stop();
          if (!stopped) {
            stopped = true;
            oscillator.stop();
            void audio.close();
          }
        };
      }
      return destination.stream;
    };
  });
}
