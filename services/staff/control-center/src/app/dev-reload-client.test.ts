import { runInNewContext } from "node:vm";
import { afterEach, expect, test, vi } from "vitest";
import {
  remoteReloadClientSource,
  controlCenterReloadTimeoutMs,
} from "../../vite.config";
const revision = (n: number) =>
  `00000000-0000-0000-0000-000000000000:${String(n)}`;
const response = (n: number) => ({
  ok: true,
  url: "https://kodex.test/__kodex_dev_revision",
  text: () => Promise.resolve(revision(n)),
});
function fixture(fetch: ReturnType<typeof vi.fn>) {
  const events = new Map<
    string,
    Set<(event?: { isTrusted: boolean }) => void>
  >();
  const reload = vi.fn();
  const window = {
    location: { origin: "https://kodex.test", reload },
    dispatchEvent: vi.fn<(event: Event) => boolean>(() => true),
    setTimeout,
    clearTimeout,
    addEventListener: (
      name: string,
      handler: (event?: { isTrusted: boolean }) => void,
    ) => {
      const set = events.get(name) ?? new Set();
      set.add(handler);
      events.set(name, set);
    },
    removeEventListener: (
      name: string,
      handler: (event?: { isTrusted: boolean }) => void,
    ) => events.get(name)?.delete(handler),
  };
  const context = {
    window,
    fetch,
    AbortController,
    URL,
    Promise,
    Symbol,
    Event,
  };
  const install = () =>
    runInNewContext(remoteReloadClientSource(), context) as unknown;
  const event = (name: string, payload?: { isTrusted: boolean }) =>
    events.get(name)?.forEach((handler) => handler(payload));
  install();
  return {
    reload,
    dispatchEvent: window.dispatchEvent,
    event,
    install,
    events,
    dispose: () => {
      (window as unknown as Record<symbol, { dispose(): void }>)[
        Symbol.for("kodex.dev.reload")
      ]?.dispose();
    },
  };
}
afterEach(() => vi.useRealTimers());
test("timeout отменяет единственный запрос и dispose завершает цикл", async () => {
  vi.useFakeTimers();
  let live = 0,
    maximum = 0;
  const fetch = vi.fn(
    (_path: string, init: { signal: AbortSignal }) =>
      new Promise((_resolve, reject) => {
        live++;
        maximum = Math.max(maximum, live);
        init.signal.addEventListener("abort", () => {
          live--;
          reject(new DOMException("fixture", "AbortError"));
        });
      }),
  );
  const f = fixture(fetch);
  await vi.advanceTimersByTimeAsync(controlCenterReloadTimeoutMs - 1);
  expect(fetch).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(1001);
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(maximum).toBe(1);
  f.dispose();
  await vi.advanceTimersByTimeAsync(20000);
  expect(live).toBe(0);
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(vi.getTimerCount()).toBe(0);
});
test("pagehide прекращает чтение, pageshow возобновляет один цикл, поздний body не reload", async () => {
  vi.useFakeTimers();
  let complete: (value: string) => void = () => undefined;
  const body = new Promise<string>((resolve) => {
    complete = resolve;
  });
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(response(1))
    .mockResolvedValueOnce({ ...response(2), text: () => body })
    .mockResolvedValue(response(1));
  const f = fixture(fetch);
  await vi.advanceTimersByTimeAsync(1000);
  f.event("pagehide");
  f.event("pagehide");
  await vi.advanceTimersByTimeAsync(5000);
  expect(fetch).toHaveBeenCalledTimes(2);
  f.event("pageshow");
  f.event("pageshow");
  complete(revision(2));
  await vi.advanceTimersByTimeAsync(1001);
  expect(fetch).toHaveBeenCalledTimes(3);
  expect(f.reload).not.toHaveBeenCalled();
  f.dispose();
  await vi.advanceTimersByTimeAsync(10000);
  expect(vi.getTimerCount()).toBe(0);
});
test("повторная установка заменяет цикл, новая revision вызывает ровно один reload", async () => {
  vi.useFakeTimers();
  const fetch = vi.fn().mockResolvedValue(response(1));
  const f = fixture(fetch);
  await vi.advanceTimersByTimeAsync(1);
  f.install();
  await vi.advanceTimersByTimeAsync(1);
  expect(f.events.get("pagehide")?.size).toBe(1);
  fetch.mockResolvedValue(response(2));
  await vi.advanceTimersByTimeAsync(1000);
  expect(f.reload).toHaveBeenCalledTimes(1);
  const count = fetch.mock.calls.length;
  await vi.advanceTimersByTimeAsync(20000);
  expect(fetch).toHaveBeenCalledTimes(count);
  expect(f.reload).toHaveBeenCalledTimes(1);
  f.dispose();
});
test("outage, foreign redirect, malformed body и HTTP failure не меняют baseline", async () => {
  vi.useFakeTimers();
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(response(1))
    .mockRejectedValueOnce(new Error("fixture outage"))
    .mockResolvedValueOnce({
      ...response(2),
      url: "https://identity.invalid/login",
    })
    .mockResolvedValueOnce({
      ...response(2),
      text: () => Promise.resolve("<html>fixture</html>"),
    })
    .mockResolvedValueOnce({ ...response(2), ok: false })
    .mockResolvedValue(response(1));
  const f = fixture(fetch);
  await vi.advanceTimersByTimeAsync(6000);
  expect(f.reload).not.toHaveBeenCalled();
  fetch.mockResolvedValue(response(2));
  await vi.advanceTimersByTimeAsync(1000);
  expect(f.reload).toHaveBeenCalledTimes(1);
  f.dispose();
});

test("beforeunload закрывает provisional load; только trusted interaction восстанавливает отменённый уход", async () => {
  vi.useFakeTimers();
  const fetch = vi.fn().mockResolvedValue(response(1));
  const f = fixture(fetch);
  await vi.advanceTimersByTimeAsync(1);
  f.event("beforeunload");
  await vi.advanceTimersByTimeAsync(5000);
  expect(fetch).toHaveBeenCalledTimes(1);
  f.event("pointerdown", { isTrusted: false });
  await vi.advanceTimersByTimeAsync(5000);
  expect(fetch).toHaveBeenCalledTimes(1);
  f.event("keydown", { isTrusted: true });
  f.event("pointerdown", { isTrusted: true });
  await vi.advanceTimersByTimeAsync(1000);
  expect(fetch).toHaveBeenCalledTimes(2);
  f.install();
  expect(f.events.get("beforeunload")?.size).toBe(1);
  expect(f.events.get("pointerdown")?.size).toBe(1);
  expect(f.events.get("keydown")?.size).toBe(1);
  f.dispose();
  expect(f.events.get("beforeunload")?.size).toBe(0);
  expect(f.events.get("pointerdown")?.size).toBe(0);
  expect(f.events.get("keydown")?.size).toBe(0);
  await vi.advanceTimersByTimeAsync(5000);
  expect(vi.getTimerCount()).toBe(0);
});

test.each([
  { ok: false, type: "opaqueredirect", status: 0 },
  { ok: false, type: "basic", status: 401 },
  { ok: false, type: "basic", status: 403 },
])(
  "auth boundary $status останавливает poll и запрашивает только проверку сессии",
  async (boundary) => {
    vi.useFakeTimers();
    const fetch = vi.fn().mockResolvedValue(boundary);
    const f = fixture(fetch);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0]?.[1]).toMatchObject({
      redirect: "manual",
      credentials: "same-origin",
    });
    expect((fetch.mock.calls[0]?.[1] as RequestInit).signal?.aborted).toBe(
      false,
    );
    expect(f.dispatchEvent).toHaveBeenCalledTimes(1);
    expect(f.dispatchEvent.mock.calls[0]?.[0].type).toBe(
      "kodex:session-probe-requested",
    );
    expect(f.reload).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
    f.event("pageshow");
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(f.dispatchEvent).toHaveBeenCalledTimes(2);
    f.dispose();
  },
);
