import type { Page } from "@playwright/test";
import { describe, expect, test, vi } from "vitest";

import {
  readJsonWithNetworkRetry,
  retryReadOnlyBrowserAction,
} from "./helpers";

describe("read-only JSON readback в браузере", () => {
  test("повторяет только временный сетевой сбой", async () => {
    const evaluate = vi
      .fn()
      .mockRejectedValueOnce(
        new Error("page.evaluate: TypeError: Failed to fetch"),
      )
      .mockResolvedValueOnce({ body: { ready: true }, status: 200 });
    const waitForTimeout = vi.fn().mockResolvedValue(undefined);
    const page = { evaluate, waitForTimeout } as unknown as Page;

    await expect(
      readJsonWithNetworkRetry<{ ready: boolean }>(page, "/api/v1/readiness"),
    ).resolves.toEqual({ body: { ready: true }, status: 200 });
    expect(evaluate).toHaveBeenCalledTimes(2);
    expect(waitForTimeout).toHaveBeenCalledWith(200);
  });

  test("не повторяет HTTP-ответ", async () => {
    const evaluate = vi
      .fn()
      .mockResolvedValue({ body: { code: "UNAVAILABLE" }, status: 503 });
    const waitForTimeout = vi.fn().mockResolvedValue(undefined);
    const page = { evaluate, waitForTimeout } as unknown as Page;

    await expect(
      readJsonWithNetworkRetry<{ code: string }>(page, "/api/v1/readiness"),
    ).resolves.toEqual({ body: { code: "UNAVAILABLE" }, status: 503 });
    expect(evaluate).toHaveBeenCalledTimes(1);
    expect(waitForTimeout).not.toHaveBeenCalled();
  });

  test("закрыто отклоняет путь вне API", async () => {
    const page = {} as Page;

    await expect(
      readJsonWithNetworkRetry(page, "https://example.test/secret"),
    ).rejects.toThrow("must start with /api/");
  });

  test("повторяет произвольное безопасное read-only действие", async () => {
    const action = vi
      .fn()
      .mockRejectedValueOnce(
        new Error("page.evaluate: TypeError: Failed to fetch"),
      )
      .mockResolvedValueOnce({ ready: true });
    const waitForTimeout = vi.fn().mockResolvedValue(undefined);
    const page = { waitForTimeout } as unknown as Page;

    await expect(retryReadOnlyBrowserAction(page, action)).resolves.toEqual({
      ready: true,
    });
    expect(action).toHaveBeenCalledTimes(2);
    expect(waitForTimeout).toHaveBeenCalledWith(200);
  });
});
