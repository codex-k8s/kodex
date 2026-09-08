import { describe, expect, it, vi } from "vitest";

import {
  sessionRenewalLeaseKey,
  sessionRenewalLeaseMs,
  SessionRenewalBus,
  SessionRenewalCoordinator,
  type SessionRevisionChannel,
} from "@/features/session/renewal-coordinator";

class MemoryStorage {
  readonly values = new Map<string, string>();
  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }
  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }
  removeItem(key: string): void {
    this.values.delete(key);
  }
}

class MemoryChannel implements SessionRevisionChannel {
  peer?: MemoryChannel;
  private readonly listeners = new Set<(event: MessageEvent) => void>();
  postMessage(message: {
    type: "session-renewed";
    revision: number;
    completedAt: number;
    nextRenewalAt: number;
  }): void {
    for (const listener of this.peer?.listeners ?? [])
      listener({ data: message } as MessageEvent);
  }
  addEventListener(
    _type: "message",
    listener: (event: MessageEvent) => void,
  ): void {
    this.listeners.add(listener);
  }
  removeEventListener(
    _type: "message",
    listener: (event: MessageEvent) => void,
  ): void {
    this.listeners.delete(listener);
  }
  close(): void {
    this.listeners.clear();
  }
  inject(data: unknown): void {
    for (const listener of this.listeners) listener({ data } as MessageEvent);
  }
}

describe("session renewal coordinator", () => {
  it("удерживает общий Web Lock до завершения refresh даже после lease timeout", async () => {
    let held = false;
    let complete!: () => void;
    const pending = new Promise<void>((resolve) => {
      complete = resolve;
    });
    vi.stubGlobal("navigator", {
      locks: {
        request: async (
          _name: string,
          _options: unknown,
          callback: (lock: object | null) => Promise<boolean>,
        ) => {
          if (held) return await callback(null);
          held = true;
          try {
            return await callback({});
          } finally {
            held = false;
          }
        },
      },
    });
    try {
      const storage = new MemoryStorage();
      const first = new SessionRenewalCoordinator(storage, "first");
      const second = new SessionRenewalCoordinator(storage, "second");
      const operation = vi.fn(() => pending);
      const one = first.runExclusive(operation);
      expect(await second.runExclusive(operation)).toBe(false);
      expect(operation).toHaveBeenCalledOnce();
      complete();
      expect(await one).toBe(true);
      expect(await second.runExclusive(async () => {})).toBe(true);
    } finally {
      vi.unstubAllGlobals();
    }
  });
  it("передаёт результат с дробной сетевой задержкой и не допускает второй refresh", () => {
    const storage = new MemoryStorage();
    let now = 100;
    const first = new SessionRenewalCoordinator(
      storage,
      "first",
      () => now,
      () => "token-1",
    );
    const second = new SessionRenewalCoordinator(
      storage,
      "second",
      () => now,
      () => "token-2",
    );
    const firstChannel = new MemoryChannel();
    const secondChannel = new MemoryChannel();
    firstChannel.peer = secondChannel;
    const publisher = new SessionRenewalBus(firstChannel, 3);
    const subscriber = new SessionRenewalBus(secondChannel, 3);
    const receipts: number[] = [];
    subscriber.subscribe((receipt) => receipts.push(receipt.nextRenewalAt));

    expect(first.acquire().acquired).toBe(true);
    expect(second.acquire().retryAfterMs).toBe(sessionRenewalLeaseMs);
    const nextRenewalAt = first.complete(300_000.75);
    publisher.publish({ revision: 3, completedAt: now, nextRenewalAt });
    first.release();

    expect(nextRenewalAt).toBe(300_100);
    expect(receipts).toEqual([300_100]);
    now += sessionRenewalLeaseMs + 25;
    expect(second.acquire()).toEqual({
      acquired: false,
      retryAfterMs: nextRenewalAt - now,
    });
    now = nextRenewalAt;
    expect(second.acquire().acquired).toBe(true);
    publisher.close();
    subscriber.close();
  });

  it.each([NaN, Infinity, -Infinity, 0, -1, Number.MAX_SAFE_INTEGER])(
    "не публикует недопустимый срок следующего обновления: %s",
    (delay) => {
      const coordinator = new SessionRenewalCoordinator(
        new MemoryStorage(),
        "first",
        () => 100,
      );
      expect(() => coordinator.complete(delay)).toThrow(
        "Session renewal deadline is invalid",
      );
    },
  );

  it("выдаёт lease ровно одной вкладке и предотвращает storm", () => {
    const storage = new MemoryStorage();
    const first = new SessionRenewalCoordinator(
      storage,
      "first",
      () => 100,
      () => "token-1",
    );
    const second = new SessionRenewalCoordinator(
      storage,
      "second",
      () => 100,
      () => "token-2",
    );
    expect(first.acquire()).toEqual({ acquired: true, retryAfterMs: 0 });
    expect(second.acquire()).toEqual({
      acquired: false,
      retryAfterMs: sessionRenewalLeaseMs,
    });
    expect(first.complete(300_000)).toBe(300_100);
    expect(second.acquire()).toEqual({
      acquired: false,
      retryAfterMs: 300_000,
    });
  });

  it("перехватывает stale leader только после expiry", () => {
    const storage = new MemoryStorage();
    let now = 1_000;
    const first = new SessionRenewalCoordinator(
      storage,
      "first",
      () => now,
      () => "token-1",
    );
    const second = new SessionRenewalCoordinator(
      storage,
      "second",
      () => now,
      () => "token-2",
    );
    first.acquire();
    now += sessionRenewalLeaseMs - 1;
    expect(second.acquire().acquired).toBe(false);
    now += 1;
    expect(second.acquire()).toEqual({ acquired: true, retryAfterMs: 0 });
  });

  it("после failure release позволяет другой вкладке восстановить renewal", () => {
    const storage = new MemoryStorage();
    const first = new SessionRenewalCoordinator(
      storage,
      "first",
      () => 100,
      () => "token-1",
    );
    const second = new SessionRenewalCoordinator(
      storage,
      "second",
      () => 101,
      () => "token-2",
    );
    first.acquire();
    first.release();
    expect(storage.getItem(sessionRenewalLeaseKey)).toBeNull();
    expect(second.acquire()).toEqual({ acquired: true, retryAfterMs: 0 });
  });

  it("BroadcastChannel передаёт renewal и отклоняет stale leader", () => {
    const firstChannel = new MemoryChannel();
    const secondChannel = new MemoryChannel();
    firstChannel.peer = secondChannel;
    secondChannel.peer = firstChannel;
    const first = new SessionRenewalBus(firstChannel, 3);
    const second = new SessionRenewalBus(secondChannel, 3);
    const receipts: number[] = [];
    second.subscribe((receipt) => receipts.push(receipt.completedAt));

    first.publish({ revision: 3, completedAt: 100, nextRenewalAt: 300_100 });
    first.publish({ revision: 2, completedAt: 101, nextRenewalAt: 300_101 });
    secondChannel.inject({
      type: "session-renewed",
      revision: 2,
      completedAt: 101,
      nextRenewalAt: 300_101,
    });
    secondChannel.inject({
      type: "session-renewed",
      revision: 3,
      completedAt: 99,
      nextRenewalAt: 300_099,
    });
    secondChannel.inject({
      type: "session-renewed",
      revision: "5",
      completedAt: 102,
      nextRenewalAt: 300_102,
    });

    expect(receipts).toEqual([100]);
    first.close();
    second.close();
  });

  it("отклоняет Broadcast revision ниже подтверждённой bootstrap revision", () => {
    const channel = new MemoryChannel();
    const bus = new SessionRenewalBus(channel, 0);
    const receipts: number[] = [];
    bus.subscribe((receipt) => receipts.push(receipt.revision));
    bus.observeRevision(7);

    channel.inject({
      type: "session-renewed",
      revision: 6,
      completedAt: 100,
      nextRenewalAt: 300_100,
    });
    channel.inject({
      type: "session-renewed",
      revision: 7,
      completedAt: 101,
      nextRenewalAt: 300_101,
    });

    expect(receipts).toEqual([7]);
    bus.close();
  });
});
