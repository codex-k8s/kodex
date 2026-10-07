import { afterEach, describe, expect, it, vi } from "vitest";
import { mutateWithRetry } from "./mutation";
import { resetOwnerRequests } from "./owner-lifetime";
import { setUnauthorizedHandler, unwrap } from "./problem";
import { ingressProxyRecoveryCode } from "./proxy-session-recovery";
import { createClient } from "./generated/openapi/client";
import { getOwnerSession } from "./generated/openapi/sdk.gen";

afterEach(() => {
  setUnauthorizedHandler(null);
  vi.unstubAllGlobals();
  resetOwnerRequests();
});

function fixture() {
  const navigate = vi.fn();
  const values = new Map([["existing-unknown-receipt", "receipt-fixture"]]);
  vi.stubGlobal("document", {
    cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
  });
  vi.stubGlobal("window", {
    location: {
      origin: "https://kodex.fixture",
      pathname: "/projects/prj_fixture/agents/agt_fixture",
      search: "?tab=runtime",
      replace: navigate,
    },
    sessionStorage: {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
    },
  });
  const response = new Response(null, {
    status: 401,
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
  Object.defineProperty(response, "url", {
    value: "https://kodex.fixture/api/v1/session/authorization",
  });
  return { navigate, values, response };
}

describe("proxy and owner authority stay distinct", () => {
  it("generated SDK сохраняет точный proxy body до canonical unwrap", async () => {
    const current = fixture();
    const invalidate = vi.fn();
    setUnauthorizedHandler(invalidate);
    const reply = new Response("Unauthorized\n", {
      status: 401,
      headers: { "Content-Type": "text/plain; charset=utf-8" },
    });
    Object.defineProperty(reply, "url", {
      value: "https://kodex.fixture/api/v1/session",
    });
    const fetch = vi.fn().mockResolvedValue(reply);
    const client = createClient({
      baseUrl: "https://kodex.fixture",
      fetch,
    });
    await expect(unwrap(getOwnerSession({ client }))).rejects.toMatchObject({
      code: ingressProxyRecoveryCode,
      retryable: false,
    });
    expect(fetch).toHaveBeenCalledOnce();
    expect(invalidate).not.toHaveBeenCalled();
    expect(current.navigate).toHaveBeenCalledOnce();
  });

  it("proxy401 не отзывает BFF и не повторяет mutation", async () => {
    const current = fixture();
    const invalidate = vi.fn();
    setUnauthorizedHandler(invalidate);
    const request = vi.fn().mockResolvedValue({
      error: "Unauthorized\n",
      response: current.response,
    });
    await expect(mutateWithRetry(request, 7)).rejects.toMatchObject({
      code: ingressProxyRecoveryCode,
      retryable: false,
      kind: "unavailable",
    });
    expect(request).toHaveBeenCalledOnce();
    expect(invalidate).not.toHaveBeenCalled();
    expect(current.navigate).toHaveBeenCalledOnce();
    expect(current.values.get("existing-unknown-receipt")).toBe(
      "receipt-fixture",
    );
  });

  it("canonical owner401 по-прежнему инвалидирует, неизвестное тело не доверяется", async () => {
    const current = fixture();
    for (const error of [
      { code: "UNAUTHENTICATED", status: 401, retryable: false },
      "UNKNOWN upstream body",
    ]) {
      const invalidate = vi.fn();
      setUnauthorizedHandler(null);
      setUnauthorizedHandler(invalidate);
      await expect(
        unwrap(Promise.resolve({ error, response: current.response })),
      ).rejects.toMatchObject({
        kind: "unauthorized",
        status: 401,
      });
      expect(invalidate).toHaveBeenCalledOnce();
    }
    expect(current.navigate).not.toHaveBeenCalled();
  });

  it("устаревший proxy401 не перенаправляет новый owner context", async () => {
    const current = fixture();
    let finish!: (value: { error: string; response: Response }) => void;
    const pending = unwrap(
      new Promise<{ error: string; response: Response }>((resolve) => {
        finish = resolve;
      }),
    );
    const rejected = expect(pending).rejects.toMatchObject({
      name: "OwnerContextChangedError",
    });
    resetOwnerRequests();
    finish({ error: "Unauthorized\n", response: current.response });
    await rejected;
    expect(current.navigate).not.toHaveBeenCalled();
  });
});
