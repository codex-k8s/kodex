import type { SpeechTranscriptionAvailability } from "@/shared/api/generated/openapi/types.gen";

const refreshIntervalMs = 15_000;
const maximumGrantMs = 30_000;

export class SpeechAvailabilityLease {
  private active = false;
  private generation = 0;
  private expiresAt = 0;
  private refreshTimer?: ReturnType<typeof setTimeout>;
  private expiryTimer?: ReturnType<typeof setTimeout>;
  private controller?: AbortController;

  constructor(
    private readonly options: {
      read(signal: AbortSignal): Promise<SpeechTranscriptionAvailability>;
      changed(available: boolean): void;
    },
  ) {}

  get available(): boolean {
    return this.active && this.expiresAt > Date.now();
  }

  start(initial?: SpeechTranscriptionAvailability): void {
    this.stop();
    this.active = true;
    if (initial) this.accept(initial);
    void this.refresh();
  }

  synchronize(value: SpeechTranscriptionAvailability | undefined): void {
    if (!this.active) return;
    // Общий bootstrap может завершиться позже отдельной проверки допуска.
    if (!value?.available) this.accept(value);
    void this.refresh();
  }

  accept(value: SpeechTranscriptionAvailability | undefined): void {
    if (!this.active) return;
    if (this.expiryTimer) clearTimeout(this.expiryTimer);
    const deadline =
      value?.available && value.validUntil ? Date.parse(value.validUntil) : 0;
    this.expiresAt = Number.isFinite(deadline)
      ? Math.min(deadline, Date.now() + maximumGrantMs)
      : 0;
    this.options.changed(this.available);
    if (this.available)
      this.expiryTimer = setTimeout(() => {
        this.expiresAt = 0;
        this.options.changed(false);
      }, this.expiresAt - Date.now());
  }

  async refresh(): Promise<void> {
    if (!this.active) return;
    if (this.refreshTimer) clearTimeout(this.refreshTimer);
    this.controller?.abort();
    const controller = new AbortController();
    this.controller = controller;
    const generation = ++this.generation;
    try {
      const value = await this.options.read(controller.signal);
      if (generation === this.generation && !controller.signal.aborted)
        this.accept(value);
    } catch {
      // Сбой refresh не продлевает прежний допуск: отдельный таймер закрывает его по expiry.
    } finally {
      if (generation === this.generation) {
        this.controller = undefined;
        const remaining = this.expiresAt - Date.now();
        const delay =
          remaining > 0
            ? Math.min(
                refreshIntervalMs,
                Math.max(1000, Math.floor(remaining / 2)),
              )
            : refreshIntervalMs;
        this.refreshTimer = setTimeout(() => {
          void this.refresh();
        }, delay);
      }
    }
  }

  stop(): void {
    this.active = false;
    this.generation += 1;
    this.expiresAt = 0;
    this.controller?.abort();
    this.controller = undefined;
    if (this.refreshTimer) clearTimeout(this.refreshTimer);
    if (this.expiryTimer) clearTimeout(this.expiryTimer);
    this.refreshTimer = undefined;
    this.expiryTimer = undefined;
    this.options.changed(false);
  }
}
