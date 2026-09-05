import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  listRuns:
    vi.fn<
      (request: { query?: unknown; signal: AbortSignal }) => Promise<unknown>
    >(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import { useRunCatalogStore } from "./run-catalog";

function run(
  ref: string,
  state: Run["state"] = "QUEUED",
  projectRef = "project_one",
): Run {
  return { ref, state, projectRef, version: 1 } as Run;
}
function response(items: Run[], nextPageToken = "") {
  return {
    data: { items, nextPageToken },
    response: new Response(null, { status: 200 }),
  };
}
describe("серверный каталог запусков", () => {
  afterEach(() => vi.useRealTimers());
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
  });
  it("объединяет realtime invalidation и перечитывает фильтр после текущего чтения", async () => {
    vi.useFakeTimers();
    const store = useRunCatalogStore();
    const scope = { query: "", filter: "ACTIVE" as const };
    let finish!: (value: ReturnType<typeof response>) => void;
    sdk.listRuns.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const loading = store.load(scope);
    store.invalidate(scope);
    store.invalidate(scope);
    sdk.listRuns.mockResolvedValueOnce(response([run("new_match")]));
    finish(response([]));
    await loading;
    await vi.advanceTimersByTimeAsync(250);
    expect(sdk.listRuns).toHaveBeenCalledTimes(2);
    expect(store.items.map((item) => item.ref)).toEqual(["new_match"]);
  });
  it("не выполняет отложенный refresh прежнего фильтра", async () => {
    vi.useFakeTimers();
    const store = useRunCatalogStore();
    const scope = { query: "", filter: "ACTIVE" as const };
    sdk.listRuns.mockResolvedValueOnce(response([]));
    await store.load(scope);
    store.invalidate(scope);
    sdk.listRuns.mockResolvedValueOnce(response([run("failed", "FAILED")]));
    await store.load({ query: "", filter: "TERMINAL" });
    await vi.advanceTimersByTimeAsync(1000);
    expect(sdk.listRuns).toHaveBeenCalledTimes(2);
    expect(store.items.map((item) => item.ref)).toEqual(["failed"]);
  });
  it("сохраняет текущие строки при ошибке realtime refresh", async () => {
    vi.useFakeTimers();
    const store = useRunCatalogStore();
    const scope = { query: "", filter: "ACTIVE" as const };
    sdk.listRuns.mockResolvedValueOnce(response([run("retained")]));
    await store.load(scope);
    sdk.listRuns.mockRejectedValueOnce(new Error("Read unavailable"));
    store.invalidate(scope);
    await vi.advanceTimersByTimeAsync(250);
    expect(store.problem).toBeDefined();
    expect(store.items.map((item) => item.ref)).toEqual(["retained"]);
  });
  it("передаёт фильтр и сбрасывает cursor при переходе к другому состоянию", async () => {
    const store = useRunCatalogStore();
    sdk.listRuns.mockResolvedValueOnce(response([run("run_one")], "next"));
    await store.load({
      projectRef: "project_one",
      query: "  задача  ",
      filter: "ACTIVE",
    });
    expect(sdk.listRuns.mock.lastCall?.[0].query).toMatchObject({
      states: ["QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"],
      query: "задача",
      pageSize: 40,
    });
    sdk.listRuns.mockResolvedValueOnce(response([run("run_two", "FAILED")]));
    await store.load({
      projectRef: "project_one",
      query: "",
      filter: "TERMINAL",
    });
    expect(sdk.listRuns.mock.lastCall?.[0].query).toMatchObject({
      states: ["SUCCEEDED", "FAILED", "CANCELLED"],
      pageToken: undefined,
    });
    expect(store.items.map((item) => item.ref)).toEqual(["run_two"]);
  });
  it("отклоняет чужой scope, неподходящее состояние и повтор cursor без потери первой страницы", async () => {
    const store = useRunCatalogStore();
    const scope = {
      projectRef: "project_one",
      query: "",
      filter: "ACTIVE" as const,
    };
    for (const invalid of [
      run("foreign", "QUEUED", "project_other"),
      run("terminal", "FAILED"),
    ]) {
      sdk.listRuns.mockResolvedValueOnce(response([invalid]));
      await store.load(scope);
      expect(store.problem).toBeDefined();
      expect(store.items).toEqual([]);
    }
    sdk.listRuns.mockResolvedValueOnce(response([run("run_one")], "next"));
    await store.load(scope);
    sdk.listRuns.mockResolvedValueOnce(response([run("run_two")], "next"));
    await store.load(scope, true);
    expect(store.problem).toBeDefined();
    expect(store.items.map((item) => item.ref)).toEqual(["run_one"]);
  });
  it("отменяет старое чтение и не принимает его ответ после смены фильтра", async () => {
    let complete!: (value: ReturnType<typeof response>) => void;
    const old = new Promise<ReturnType<typeof response>>((resolve) => {
      complete = resolve;
    });
    sdk.listRuns
      .mockReturnValueOnce(old)
      .mockResolvedValueOnce(response([run("new", "FAILED")]));
    const store = useRunCatalogStore();
    const first = store.load({ query: "", filter: "ACTIVE" });
    const signal = sdk.listRuns.mock.lastCall?.[0].signal as AbortSignal;
    await store.load({ query: "", filter: "TERMINAL" });
    expect(signal.aborted).toBe(true);
    complete(response([run("old")]));
    await first;
    expect(store.items.map((item) => item.ref)).toEqual(["new"]);
    store.reset();
    expect(store.items).toEqual([]);
    expect(store.pageToken).toBeUndefined();
  });
});
