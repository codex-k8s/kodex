import { afterEach, describe, expect, it, vi } from "vitest";

import { setUnauthorizedHandler, unwrap } from "./problem";

afterEach(() => setUnauthorizedHandler(null));

describe("authoritative session invalidation", () => {
  it("инвалидирует сессию при общем HTTP 401", async () => {
    const invalidate = vi.fn();
    setUnauthorizedHandler(invalidate);

    await expect(
      unwrap(
        Promise.resolve({
          error: {
            code: "UNAUTHENTICATED",
            retryable: false,
            status: 401,
          },
          response: new Response(null, { status: 401 }),
        }),
      ),
    ).rejects.toMatchObject({ kind: "unauthorized", status: 401 });
    expect(invalidate).toHaveBeenCalledOnce();
  });

  it("не инвалидирует сессию при fresh-auth HTTP 403", async () => {
    const invalidate = vi.fn();
    setUnauthorizedHandler(invalidate);

    await expect(
      unwrap(
        Promise.resolve({
          error: {
            code: "FRESH_AUTHENTICATION_REQUIRED",
            retryable: false,
            status: 403,
          },
          response: new Response(null, { status: 403 }),
        }),
      ),
    ).rejects.toMatchObject({
      code: "FRESH_AUTHENTICATION_REQUIRED",
      kind: "forbidden",
      status: 403,
    });
    expect(invalidate).not.toHaveBeenCalled();
  });
});
