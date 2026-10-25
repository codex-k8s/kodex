import type { Page } from "@playwright/test";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mutateLocalApi } from "./local-api";

function browserPage() {
  const evaluate = vi.fn(
    async (callback: (input: unknown) => Promise<unknown>, input: unknown) =>
      callback(input),
  );
  return { page: { evaluate } as unknown as Page, evaluate };
}

const browserFetch = vi.fn<typeof fetch>();

beforeEach(() => {
  browserFetch.mockReset();
  vi.stubGlobal("fetch", browserFetch);
  vi.stubGlobal("document", {
    cookie: "other=ignored; __Host-kodex-csrf=synthetic%20csrf",
  });
});

afterEach(() => vi.unstubAllGlobals());

describe("однократная локальная E2E mutation", () => {
  it("передаёт POST, CSRF и один новый ключ без необязательного body/версии", async () => {
    const { page, evaluate } = browserPage();
    browserFetch.mockResolvedValue(
      new Response(JSON.stringify({ ref: "created" }), { status: 201 }),
    );

    await expect(mutateLocalApi(page, "/api/v1/fixture")).resolves.toEqual({
      ref: "created",
    });
    expect(evaluate).toHaveBeenCalledTimes(1);
    expect(browserFetch).toHaveBeenCalledTimes(1);
    const [path, request] = browserFetch.mock.calls[0] ?? [];
    expect(path).toBe("/api/v1/fixture");
    expect(request?.method).toBe("POST");
    expect(request?.body).toBeUndefined();
    const headers = new Headers(request?.headers);
    expect(headers.get("Idempotency-Key")).toMatch(/^[a-f0-9-]{36}$/);
    expect(headers.get("Accept")).toBe("application/json");
    expect(headers.get("Content-Type")).toBe("application/json");
    expect(headers.get("X-CSRF-Token")).toBe("synthetic csrf");
    expect(headers.has("If-Match")).toBe(false);
  });

  it.each([0, 7])("сохраняет PUT/body/If-Match версии %s", async (version) => {
    const { page } = browserPage();
    browserFetch.mockResolvedValue(
      new Response(JSON.stringify({ saved: true })),
    );

    await mutateLocalApi(
      page,
      "/api/v1/fixture",
      { marker: "input" },
      version,
      "PUT",
    );

    expect(browserFetch).toHaveBeenCalledTimes(1);
    const request = browserFetch.mock.calls[0]?.[1];
    expect(request?.method).toBe("PUT");
    expect(request?.body).toBe('{"marker":"input"}');
    expect(request?.headers).toMatchObject({
      "If-Match": `"${String(version)}"`,
    });
  });

  it("сохраняет явный null body", async () => {
    const { page } = browserPage();
    browserFetch.mockResolvedValue(
      new Response(JSON.stringify({ saved: true })),
    );

    await mutateLocalApi(page, "/api/v1/fixture", null);

    expect(browserFetch.mock.calls[0]?.[1]?.body).toBe("null");
  });

  it("создаёт разные ключи для двух явно вызванных операций", async () => {
    const { page } = browserPage();
    browserFetch.mockImplementation(() =>
      Promise.resolve(new Response(JSON.stringify({ saved: true }))),
    );

    await mutateLocalApi(page, "/api/v1/fixture");
    await mutateLocalApi(page, "/api/v1/fixture");

    const keys = browserFetch.mock.calls.map(([, request]) =>
      new Headers(request?.headers).get("Idempotency-Key"),
    );
    expect(keys).toHaveLength(2);
    expect(new Set(keys).size).toBe(2);
  });

  it("не отправляет запрос без CSRF", async () => {
    const { page, evaluate } = browserPage();
    vi.stubGlobal("document", { cookie: "other=ignored" });

    await expect(mutateLocalApi(page, "/api/v1/fixture")).rejects.toMatchObject(
      {
        message: "API mutation failed: 0 CSRF_UNAVAILABLE",
      },
    );
    expect(evaluate).toHaveBeenCalledTimes(1);
    expect(browserFetch).not.toHaveBeenCalled();
  });

  it.each([
    [{ code: "CONFLICT", detail: "synthetic-private-detail" }, "CONFLICT"],
    [{ code: 7, detail: "synthetic-private-detail" }, "UNKNOWN"],
  ])("ограничивает диагностику HTTP-ошибки status/code", async (body, code) => {
    const { page, evaluate } = browserPage();
    browserFetch.mockResolvedValue(
      new Response(JSON.stringify(body), { status: 409 }),
    );

    await expect(mutateLocalApi(page, "/api/v1/fixture")).rejects.toMatchObject(
      {
        message: `API mutation failed: 409 ${code}`,
      },
    );
    expect(evaluate).toHaveBeenCalledTimes(1);
    expect(browserFetch).toHaveBeenCalledTimes(1);
  });

  it("не повторяет неизвестный транспортный исход", async () => {
    const { page, evaluate } = browserPage();
    const failure = new Error("synthetic uncertain transport");
    browserFetch.mockRejectedValue(failure);

    await expect(mutateLocalApi(page, "/api/v1/fixture")).rejects.toBe(failure);
    expect(evaluate).toHaveBeenCalledTimes(1);
    expect(browserFetch).toHaveBeenCalledTimes(1);
  });

  it("не повторяет mutation после невалидного JSON ответа", async () => {
    const { page, evaluate } = browserPage();
    browserFetch.mockResolvedValue(new Response("invalid-json"));

    await expect(
      mutateLocalApi(page, "/api/v1/fixture"),
    ).rejects.toBeInstanceOf(SyntaxError);
    expect(evaluate).toHaveBeenCalledTimes(1);
    expect(browserFetch).toHaveBeenCalledTimes(1);
  });

  it("не повторяет отказ самого evaluate", async () => {
    const failure = new Error("synthetic closed browser");
    const evaluate = vi.fn().mockRejectedValue(failure);
    const page = { evaluate } as unknown as Page;

    await expect(mutateLocalApi(page, "/api/v1/fixture")).rejects.toBe(failure);
    expect(evaluate).toHaveBeenCalledTimes(1);
    expect(browserFetch).not.toHaveBeenCalled();
  });
});
