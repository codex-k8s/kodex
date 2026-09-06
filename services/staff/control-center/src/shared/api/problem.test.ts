import { afterEach, describe, expect, it, vi } from "vitest";

import { normalizeProblem, setUnauthorizedHandler, unwrap } from "./problem";
import { resetOwnerRequests } from "./owner-lifetime";

afterEach(() => setUnauthorizedHandler(null));

describe("authoritative session invalidation", () => {
  it("не применяет старый HTTP 401 к новой owner сессии", async () => {
    const invalidate = vi.fn();
    setUnauthorizedHandler(invalidate);
    let complete!: (value: {
      error: { code: string; status: number };
      response: Response;
    }) => void;
    const pending = unwrap(
      new Promise<{
        error: { code: string; status: number };
        response: Response;
      }>((resolve) => {
        complete = resolve;
      }),
    );
    const rejection = expect(pending).rejects.toMatchObject({
      name: "OwnerContextChangedError",
    });
    resetOwnerRequests();
    complete({
      error: { code: "UNAUTHENTICATED", status: 401 },
      response: new Response(null, { status: 401 }),
    });
    await rejection;
    expect(invalidate).not.toHaveBeenCalled();
  });
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

describe("strict Problem normalization", () => {
  it.each([
    [404, "not-found", false],
    [500, "unavailable", true],
    [503, "unavailable", true],
  ] as const)(
    "нормализует malformed HTTP %d без зависимости от тела",
    (status, kind, retryable) => {
      const problem = normalizeProblem(
        "upstream HTML",
        new Response(null, { status }),
      );
      expect(problem).toMatchObject({
        status,
        code: "UNKNOWN",
        kind,
        retryable,
      });
    },
  );

  it("сохраняет только typed локализованный Problem", () => {
    const problem = normalizeProblem({
      type: "urn:kodex:problem:not_found",
      title: "Запрошенный объект не найден",
      status: 404,
      code: "NOT_FOUND",
      correlationId: "00000000-0000-4000-8000-000000000001",
      retryable: false,
    });
    expect(problem.title).toBe("Запрошенный объект не найден");
    expect(problem.kind).toBe("not-found");
  });

  it.each([undefined, "0", "301", "1, 2", "invalid", "999999999", "-1"])(
    "не выдумывает STT ожидание из некорректной подсказки %s",
    (hint) => {
      const problem = normalizeProblem(
        { status: 429, code: "TRANSCRIPTION_RATE_LIMITED", retryable: true },
        new Response(null, {
          status: 429,
          headers: hint === undefined ? {} : { "Retry-After": hint },
        }),
      );
      expect(problem.code).toBe("TRANSCRIPTION_RATE_LIMITED");
      expect(problem.retryAfterSeconds).toBeUndefined();
      expect(problem.retryable).toBe(false);
    },
  );
  it.each(["1", "30", "300"])(
    "сохраняет точный bounded STT Retry-After %s",
    (hint) => {
      const problem = normalizeProblem(
        { status: 429, code: "TRANSCRIPTION_RATE_LIMITED", retryable: true },
        new Response(null, { status: 429, headers: { "Retry-After": hint } }),
      );
      expect(problem.retryAfterSeconds).toBe(Number(hint));
      expect(problem.retryable).toBe(true);
    },
  );
  it("не превращает generic rate limit в STT outcome", () => {
    const problem = normalizeProblem(
      { status: 429, code: "RATE_LIMITED", retryable: true },
      new Response(null, { status: 429, headers: { "Retry-After": "30" } }),
    );
    expect(problem.code).toBe("RATE_LIMITED");
    expect(problem.retryAfterSeconds).toBeUndefined();
  });

  it("не показывает raw i18n key из невалидного Problem", () => {
    const problem = normalizeProblem(
      {
        status: 503,
        code: "app.searchKind.SEARCH_RESULT_KIND_RUN",
        retryable: true,
      },
      new Response(null, { status: 503 }),
    );
    expect(problem).toMatchObject({
      status: 503,
      code: "UNKNOWN",
      kind: "unavailable",
      retryable: true,
    });
  });
});
