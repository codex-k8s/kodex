import { describe, expect, it } from "vitest";

import { runBoundedPlatformReload } from "./platform-reload";

function deferred(): { promise: Promise<void>; resolve: () => void } {
  let resolve!: () => void;
  const promise = new Promise<void>((ready) => {
    resolve = ready;
  });
  return { promise, resolve };
}

describe("bounded platform reload", () => {
  it("не запускает больше четырёх authoritative reads одновременно", async () => {
    const gates = Array.from({ length: 9 }, () => deferred());
    let active = 0;
    let maximum = 0;
    let completed = 0;
    const pending = runBoundedPlatformReload(
      gates.map((gate) => ({
        run: async () => {
          active += 1;
          maximum = Math.max(maximum, active);
          await gate.promise;
          active -= 1;
          completed += 1;
        },
      })),
      4,
    );

    await Promise.resolve();
    expect(active).toBe(4);
    for (const gate of gates) {
      gate.resolve();
      await Promise.resolve();
    }
    await pending;

    expect(maximum).toBe(4);
    expect(completed).toBe(9);
  });

  it("завершает остальные reads и затем возвращает первую ошибку", async () => {
    const completed: number[] = [];
    await expect(
      runBoundedPlatformReload(
        [0, 1, 2].map((index) => ({
          run: () => {
            completed.push(index);
            if (index === 1) return Promise.reject(new Error("read failed"));
            return Promise.resolve();
          },
        })),
        2,
      ),
    ).rejects.toThrow("read failed");
    expect(completed).toEqual([0, 1, 2]);
  });
});
