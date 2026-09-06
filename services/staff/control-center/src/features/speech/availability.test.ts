import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SpeechTranscriptionAvailability } from "@/shared/api/generated/openapi/types.gen";
import { SpeechAvailabilityLease } from "./availability";

function ready(): SpeechTranscriptionAvailability {
  return {
    available: true,
    reason: "READY",
    validUntil: new Date(Date.now() + 30_000).toISOString(),
  };
}
describe("SpeechAvailabilityLease", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-05T00:00:00Z"));
  });
  afterEach(() => {
    vi.useRealTimers();
  });
  it("продлевает допуск без выключения микрофона на протяжении 120-секундной записи", async () => {
    const changed = vi.fn();
    const read = vi.fn(() => Promise.resolve(ready()));
    const lease = new SpeechAvailabilityLease({ read, changed });
    lease.start();
    await vi.advanceTimersByTimeAsync(0);
    changed.mockClear();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(lease.available).toBe(true);
    expect(read).toHaveBeenCalledTimes(9);
    expect(changed).not.toHaveBeenCalledWith(false);
    lease.stop();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("скрывает микрофон по expiry при неуспешном refresh, не продлевая прежний допуск", async () => {
    const changed = vi.fn();
    const read = vi
      .fn<() => Promise<SpeechTranscriptionAvailability>>()
      .mockResolvedValueOnce(ready())
      .mockRejectedValue(new Error("Unavailable"));
    const lease = new SpeechAvailabilityLease({ read, changed });
    lease.start();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(29_999);
    expect(lease.available).toBe(true);
    await vi.advanceTimersByTimeAsync(1);
    expect(lease.available).toBe(false);
    expect(changed).toHaveBeenLastCalledWith(false);
    lease.stop();
  });
  it("отзывает доступ немедленно по false и не принимает отсутствующий/некорректный validUntil", async () => {
    const lease = new SpeechAvailabilityLease({
      read: () => Promise.resolve(ready()),
      changed: vi.fn(),
    });
    lease.start();
    await vi.advanceTimersByTimeAsync(0);
    lease.accept({ available: false, reason: "STT_PERMISSION_DENIED" });
    expect(lease.available).toBe(false);
    for (const validUntil of [
      undefined,
      "invalid",
      new Date(Date.now()).toISOString(),
    ]) {
      lease.accept({ available: true, reason: "READY", validUntil });
      expect(lease.available).toBe(false);
    }
    lease.stop();
  });
  it("отменяет прежний запрос при navigation refresh и игнорирует ответ после logout", async () => {
    const pending: {
      signal: AbortSignal;
      resolve(value: SpeechTranscriptionAvailability): void;
    }[] = [];
    const lease = new SpeechAvailabilityLease({
      read: (signal) =>
        new Promise((resolve) => {
          pending.push({ signal, resolve });
        }),
      changed: vi.fn(),
    });
    lease.start();
    const next = lease.refresh();
    expect(pending[0]?.signal.aborted).toBe(true);
    pending[0]?.resolve(ready());
    await vi.advanceTimersByTimeAsync(0);
    expect(lease.available).toBe(false);
    lease.stop();
    expect(pending[1]?.signal.aborted).toBe(true);
    pending[1]?.resolve(ready());
    await next;
    expect(lease.available).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
  });
  it("не возвращает допуск из запоздавшего общего bootstrap после отзыва", async () => {
    const pending: {
      signal: AbortSignal;
      resolve(value: SpeechTranscriptionAvailability): void;
    }[] = [];
    const lease = new SpeechAvailabilityLease({
      read: (signal) =>
        new Promise((resolve) => pending.push({ signal, resolve })),
      changed: vi.fn(),
    });
    lease.start(ready());
    lease.synchronize({ available: false, reason: "STT_PERMISSION_DENIED" });
    expect(pending[0]?.signal.aborted).toBe(true);
    expect(lease.available).toBe(false);
    lease.synchronize(ready());
    expect(pending[1]?.signal.aborted).toBe(true);
    expect(lease.available).toBe(false);
    pending[0]?.resolve(ready());
    pending[1]?.resolve(ready());
    await vi.advanceTimersByTimeAsync(0);
    expect(lease.available).toBe(false);
    pending[2]?.resolve(ready());
    await vi.advanceTimersByTimeAsync(0);
    expect(lease.available).toBe(true);
    lease.stop();
    expect(vi.getTimerCount()).toBe(0);
  });
});
