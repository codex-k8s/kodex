import { afterEach, describe, expect, it, vi } from "vitest";

import { AppProblem } from "@/shared/api/problem";

import { readWithRetry } from "./read-retry";

afterEach(() => vi.useRealTimers());

describe("readWithRetry", () => {
  it("повторяет безопасное чтение после временного сетевого сбоя", async () => {
    vi.useFakeTimers();
    const request = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValue("ready");

    const result = readWithRetry(request, [0, 200]);
    await vi.runAllTimersAsync();

    await expect(result).resolves.toBe("ready");
    expect(request).toHaveBeenCalledTimes(2);
  });

  it.each([
    ["unauthorized", 401],
    ["forbidden", 403],
  ] as const)("не повторяет HTTP %s", async (kind, status) => {
    const problem = new AppProblem({
      status,
      code: "ACCESS_DENIED",
      retryable: true,
      kind,
    });
    const request = vi.fn<() => Promise<never>>().mockRejectedValue(problem);

    await expect(readWithRetry(request, [0, 1])).rejects.toBe(problem);
    expect(request).toHaveBeenCalledOnce();
  });

  it("возвращает последнюю ошибку после исчерпания bounded retry", async () => {
    vi.useFakeTimers();
    const failure = new TypeError("Failed to fetch");
    const request = vi.fn<() => Promise<never>>().mockRejectedValue(failure);

    const result = readWithRetry(request, [0, 200, 600]);
    const rejection = expect(result).rejects.toMatchObject({
      kind: "unavailable",
      retryable: true,
      status: 0,
    });
    await vi.runAllTimersAsync();

    await rejection;
    expect(request).toHaveBeenCalledTimes(3);
  });
});
