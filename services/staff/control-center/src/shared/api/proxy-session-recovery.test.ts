import { afterEach, describe, expect, it, vi } from "vitest";
import {
  clearIngressProxyRecovery,
  ingressProxyRecoveryKey,
  isIngressProxyUnauthorized,
  recoverIngressProxySession,
} from "./proxy-session-recovery";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function response(
  url = "https://kodex.fixture/api/v1/session",
  type = "text/plain; charset=utf-8",
  status = 401,
): Response {
  const result = new Response(null, {
    status,
    headers: { "Content-Type": type },
  });
  Object.defineProperty(result, "url", { value: url });
  return result;
}

function context(pathname = "/projects/prj_fixture/agents/agt_fixture") {
  const values = new Map<string, string>();
  return {
    location: {
      origin: "https://kodex.fixture",
      pathname,
      search:
        "?tab=runtime&draftRef=renvd_fixture&code=private-code&state=private-state",
    },
    storage: {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key),
    },
    navigate: vi.fn<(path: string) => void>(),
  };
}

describe("closed ingress proxy recovery", () => {
  it("очистка в SSR не требует window", () => {
    vi.stubGlobal("window", undefined);
    expect(() => clearIngressProxyRecovery()).not.toThrow();
    const removeItem = vi.fn();
    clearIngressProxyRecovery({ removeItem });
    expect(removeItem).toHaveBeenCalledExactlyOnceWith(ingressProxyRecoveryKey);
  });

  it("недоступный storage getter не ломает очистку и не логируется", () => {
    const error = vi.spyOn(console, "error");
    const warn = vi.spyOn(console, "warn");
    const getter = vi.fn(() => {
      throw new DOMException("Private fixture", "SecurityError");
    });
    vi.stubGlobal(
      "window",
      Object.defineProperty({}, "sessionStorage", {
        get: getter,
      }),
    );
    expect(() => clearIngressProxyRecovery()).not.toThrow();
    expect(getter).toHaveBeenCalledOnce();
    expect(error).not.toHaveBeenCalled();
    expect(warn).not.toHaveBeenCalled();
  });

  it("ошибка removeItem не ломает очистку и не логируется", () => {
    const error = vi.spyOn(console, "error");
    const warn = vi.spyOn(console, "warn");
    const removeItem = vi.fn(() => {
      throw new DOMException("Private fixture", "SecurityError");
    });
    vi.stubGlobal("window", { sessionStorage: { removeItem } });
    expect(() => clearIngressProxyRecovery()).not.toThrow();
    expect(removeItem).toHaveBeenCalledExactlyOnceWith(ingressProxyRecoveryKey);
    expect(error).not.toHaveBeenCalled();
    expect(warn).not.toHaveBeenCalled();
  });

  it("storage failure не превращает proxy response в retry/owner invalidation", () => {
    const current = context();
    current.storage.getItem = () => {
      throw new Error("private storage failure");
    };
    expect(
      recoverIngressProxySession("Unauthorized\n", response(), current),
    ).toBe(true);
    expect(current.navigate).not.toHaveBeenCalled();
  });
  it("сохраняет UI route/draft/tab и делает один top-level переход", () => {
    const current = context();
    expect(
      recoverIngressProxySession("Unauthorized\n", response(), current),
    ).toBe(true);
    expect(current.navigate).toHaveBeenCalledOnce();
    const destination = current.navigate.mock.calls[0]?.[0];
    if (!destination) throw new Error("Missing proxy recovery navigation");
    const url = new URL(destination, current.location.origin);
    expect(url.origin).toBe(current.location.origin);
    expect(url.pathname).toBe("/oauth2/sign_in");
    expect(url.searchParams.get("rd")).toBe(
      "/projects/prj_fixture/agents/agt_fixture?draftRef=renvd_fixture&tab=runtime",
    );
    expect(url.href).not.toContain("private-");
    recoverIngressProxySession("Unauthorized\n", response(), current);
    expect(current.navigate).toHaveBeenCalledOnce();
    expect(current.storage.getItem(ingressProxyRecoveryKey)).toBe("1");
    clearIngressProxyRecovery(current.storage);
    recoverIngressProxySession("Unauthorized\n", response(), current);
    expect(current.navigate).toHaveBeenCalledOnce();
    const nextDocument = {
      ...current,
      location: { ...current.location },
    };
    recoverIngressProxySession("Unauthorized\n", response(), nextDocument);
    expect(current.navigate).toHaveBeenCalledTimes(2);
  });

  it.each([
    [{ code: "UNAUTHENTICATED", status: 401 }, response()],
    ["Unauthorized\n", response(undefined, "application/problem+json")],
    ["Unauthorized", response()],
    ["private arbitrary body", response()],
    ["Unauthorized\n", response("https://foreign.fixture/api/v1/session")],
    [
      "Unauthorized\n",
      response("https://user:secret@kodex.fixture/api/v1/session"),
    ],
    ["Unauthorized\n", response("https://kodex.fixture/oauth2/auth")],
    ["Unauthorized\n", response(undefined, undefined, 403)],
  ])("не подменяет owner/foreign/unknown отказ recovery", (body, reply) => {
    const current = context();
    expect(recoverIngressProxySession(body, reply, current)).toBe(false);
    expect(current.navigate).not.toHaveBeenCalled();
  });

  it.each([
    "//foreign.fixture",
    "/oauth2/callback",
    "/auth/callback",
    "/api/v1/session",
    "/projects/%2f%2fforeign",
    "/unregistered",
  ])("не использует unknown/private return target %s", (pathname) => {
    const current = context(pathname);
    recoverIngressProxySession("Unauthorized\n", response(), current);
    const destination = current.navigate.mock.calls[0]?.[0];
    if (!destination) throw new Error("Missing proxy recovery navigation");
    const url = new URL(destination, current.location.origin);
    expect(url.searchParams.get("rd")).toBe("/");
  });

  it("не доверяет redirected ответу и не делает plaintext recovery", () => {
    const reply = response();
    Object.defineProperty(reply, "redirected", { value: true });
    expect(
      isIngressProxyUnauthorized(
        "Unauthorized\n",
        reply,
        "https://kodex.fixture",
      ),
    ).toBe(false);
    expect(
      isIngressProxyUnauthorized(
        "Unauthorized\n",
        response(),
        "http://kodex.fixture",
      ),
    ).toBe(false);
  });
});
