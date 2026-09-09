import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  documentFetch,
  documentRequestSignal,
  documentRequestsResumed,
  installDocumentRequestLifetime,
  retainRequestSignalParents,
} from "./document-lifetime";
import { ownerRequestSignal, resetOwnerRequests } from "./owner-lifetime";
import { readWithRetry } from "./read-retry";
import { createClient } from "./generated/openapi/client/client.gen";
import { runBoundedPlatformReload } from "@/features/platform/platform-reload";

let target: EventTarget;
let cleanup: () => void;
beforeEach(() => {
  target = new EventTarget();
  cleanup = installDocumentRequestLifetime(target as Window);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("document request lifetime", () => {
  it("закрывает старый owner signal, не разрешает fetch после interceptor и не меняет владельца при resume", async () => {
    const native = vi.fn<typeof fetch>().mockResolvedValue(new Response("ok"));
    vi.stubGlobal("fetch", native);
    const old = ownerRequestSignal();
    target.dispatchEvent(new Event("beforeunload"));
    expect(old.aborted).toBe(true);
    await expect(
      documentFetch(new Request("https://kodex.example/api/v1/projects")),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(native).not.toHaveBeenCalled();
    resetOwnerRequests();
    expect(ownerRequestSignal().aborted).toBe(true);
    let resumed = 0;
    target.addEventListener(documentRequestsResumed, () => resumed++);
    target.dispatchEvent(new Event("pointerdown"));
    expect(documentRequestSignal().aborted).toBe(true);
    target.dispatchEvent(new Event("pageshow"));
    target.dispatchEvent(new Event("pageshow"));
    expect(resumed).toBe(1);
    expect(old.aborted).toBe(true);
    await documentFetch(new Request("https://kodex.example/api/v1/projects"));
    expect(native).toHaveBeenCalledOnce();
  });
  it("generated interceptor, завершённый после ухода, не достигает native fetch", async () => {
    const native = vi.fn<typeof fetch>();
    vi.stubGlobal("fetch", native);
    const client = createClient({
      baseUrl: "https://kodex.example",
      fetch: documentFetch,
    });
    let release!: () => void;
    const pendingInterceptor = new Promise<void>((resolve) => {
      release = resolve;
    });
    let entered = false;
    client.interceptors.request.use(async (request) => {
      entered = true;
      await pendingInterceptor;
      return request;
    });
    const pending = client.get({ url: "/api/v1/projects" });
    for (let index = 0; index < 10; index++) await Promise.resolve();
    expect(entered).toBe(true);
    target.dispatchEvent(new Event("beforeunload"));
    release();
    const result = await pending;
    expect(result.error).toMatchObject({ name: "AbortError" });
    expect(native).not.toHaveBeenCalled();
  });

  it("не повторяет старый read после pause/resume и не начинает оставшуюся очередь", async () => {
    vi.useFakeTimers();
    const read = vi
      .fn<() => Promise<string>>()
      .mockRejectedValue(new TypeError("Network unavailable"));
    const pending = readWithRetry(read, [0, 200]);
    const rejected = expect(pending).rejects.toMatchObject({
      name: "OwnerContextChangedError",
    });
    await vi.advanceTimersByTimeAsync(0);
    target.dispatchEvent(new Event("pagehide"));
    target.dispatchEvent(new Event("pageshow"));
    await vi.runAllTimersAsync();
    await rejected;
    expect(read).toHaveBeenCalledOnce();
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const first = vi.fn(() => gate),
      next = vi.fn(() => Promise.resolve());
    const queue = runBoundedPlatformReload([{ run: first }, { run: next }], 1);
    const cancelled = expect(queue).rejects.toMatchObject({
      name: "OwnerContextChangedError",
    });
    target.dispatchEvent(new Event("beforeunload"));
    target.dispatchEvent(new Event("pageshow"));
    release();
    await cancelled;
    expect(next).not.toHaveBeenCalled();
  });
  it("отменяет активный native request, сохраняет headers/body/method/credentials и внешнюю причину отказа", async () => {
    const native = vi.fn<typeof fetch>((_input, init) => {
      return new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener(
          "abort",
          () => reject(new DOMException("Fixture aborted", "AbortError")),
          { once: true },
        );
      });
    });
    vi.stubGlobal("fetch", native);
    const request = new Request("https://kodex.example/api/v1/projects", {
      method: "POST",
      headers: { "X-Fixture": "preserved" },
      body: "fixture",
      credentials: "include",
    });
    const pending = documentFetch(request);
    await vi.waitFor(() => expect(native).toHaveBeenCalledOnce());
    const transmitted = native.mock.calls[0]?.[0];
    const init = native.mock.calls[0]?.[1];
    expect(transmitted).toBeInstanceOf(Request);
    if (!(transmitted instanceof Request))
      throw new Error("Missing fixture request");
    expect(transmitted.method).toBe("POST");
    expect(transmitted.headers.get("X-Fixture")).toBe("preserved");
    expect(transmitted.credentials).toBe("include");
    expect(await transmitted.text()).toBe("fixture");
    expect(init?.signal?.aborted).toBe(false);
    target.dispatchEvent(new Event("beforeunload"));
    expect(init?.signal?.aborted).toBe(true);
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    target.dispatchEvent(new Event("pageshow"));
    const failure = new TypeError("Network unavailable");
    native.mockRejectedValueOnce(failure);
    await expect(
      documentFetch(new Request("https://kodex.example/api/v1/projects")),
    ).rejects.toBe(failure);
  });
  it("передаёт отмену parent signal напрямую в native fetch", async () => {
    const native = vi.fn<typeof fetch>((_input, init) => {
      return new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener(
          "abort",
          () => reject(new DOMException("Fixture aborted", "AbortError")),
          { once: true },
        );
      });
    });
    vi.stubGlobal("fetch", native);
    const parent = new AbortController();
    const source = new Request("https://kodex.example/api/v1/projects", {
      signal: parent.signal,
    });
    const pending = documentFetch(
      retainRequestSignalParents(
        new Request(source, { headers: { "X-Fixture": "preserved" } }),
        source,
      ),
    );
    await vi.waitFor(() => expect(native).toHaveBeenCalledOnce());
    const init = native.mock.calls[0]?.[1];
    expect(init?.signal?.aborted).toBe(false);
    parent.abort();
    expect(init?.signal?.aborted).toBe(true);
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
  });
  it("регистрация идемпотентна, cleanup удаляет слушатели", () => {
    expect(installDocumentRequestLifetime(target as Window)).toBe(cleanup);
    cleanup();
    const detached = documentRequestSignal();
    target.dispatchEvent(new Event("pageshow"));
    expect(documentRequestSignal()).toBe(detached);
    expect(detached.aborted).toBe(true);
    cleanup = installDocumentRequestLifetime(target as Window);
    expect(documentRequestSignal().aborted).toBe(false);
  });
});
