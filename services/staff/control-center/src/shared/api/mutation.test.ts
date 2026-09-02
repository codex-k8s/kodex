import { afterEach, describe, expect, it, vi } from "vitest";

import {
  mutate,
  mutateWithRetry,
  type MutationHeaders,
} from "@/shared/api/mutation";
import { AppProblem } from "@/shared/api/problem";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("mutate HTTP boundary", () => {
  it("отправляет команду с CSRF, idempotency и ожидаемой версией", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    const response = new Response(null, { status: 204 });
    const request = vi
      .fn<(headers: MutationHeaders) => Promise<{ response: Response }>>()
      .mockResolvedValue({ response });

    await expect(mutate(request, 7)).resolves.toMatchObject({
      data: undefined,
    });
    expect(request).toHaveBeenCalledOnce();
    const headers = request.mock.calls[0]?.[0];
    expect(headers).toBeDefined();
    if (!headers) return;
    expect(headers).toMatchObject({
      "X-CSRF-Token": "a".repeat(43),
      "If-Match": '"7"',
    });
    expect(headers["Idempotency-Key"]).toMatch(/^[0-9a-f-]{36}$/);
  });

  it("сохраняет выданный вызывающей стороной idempotency key", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    const request = vi
      .fn<(headers: MutationHeaders) => Promise<{ response: Response }>>()
      .mockResolvedValue({ response: new Response(null, { status: 204 }) });

    await mutate(request, undefined, "stable-upload-idempotency-key");

    expect(request.mock.calls[0]?.[0]?.["Idempotency-Key"]).toBe(
      "stable-upload-idempotency-key",
    );
  });

  it("повторяет неопределённую мутацию с теми же key и If-Match", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    const uncertain = new AppProblem({
      status: 503,
      code: "UNAVAILABLE",
      retryable: true,
      kind: "unavailable",
    });
    const request = vi
      .fn<(headers: MutationHeaders) => Promise<{ response: Response }>>()
      .mockRejectedValueOnce(uncertain)
      .mockResolvedValueOnce({
        response: new Response(null, { status: 204 }),
      });

    const result = mutateWithRetry(request, 11, "stable-mutation-key");
    await vi.runAllTimersAsync();
    await expect(result).resolves.toMatchObject({ data: undefined });

    expect(request).toHaveBeenCalledTimes(2);
    expect(request.mock.calls[0]?.[0]).toEqual(request.mock.calls[1]?.[0]);
    expect(request.mock.calls[1]?.[0]).toEqual({
      "Idempotency-Key": "stable-mutation-key",
      "X-CSRF-Token": "a".repeat(43),
      "If-Match": '"11"',
    });
  });

  it("не повторяет non-retryable мутацию", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    const conflict = new AppProblem({
      status: 412,
      code: "VERSION_CONFLICT",
      retryable: false,
      kind: "conflict",
    });
    const request = vi
      .fn<(headers: MutationHeaders) => Promise<{ response: Response }>>()
      .mockRejectedValue(conflict);

    await expect(
      mutateWithRetry(request, 11, "stable-mutation-key"),
    ).rejects.toBe(conflict);
    expect(request).toHaveBeenCalledOnce();
  });
});
