import { afterEach, describe, expect, it, vi } from "vitest";

import { mutate, type MutationHeaders } from "@/shared/api/mutation";

afterEach(() => {
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
});
