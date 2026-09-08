import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
type Request = {
  signal: AbortSignal;
  query: { states: Run["state"][]; [key: string]: unknown };
};
const sdk = vi.hoisted(() => ({
  listRuns: vi.fn<(request: Request) => Promise<unknown>>(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import { useRunBoardStore } from "./run-board";
function response(state: Run["state"], suffix = "first", cursor?: string) {
  return {
    data: {
      items: [
        {
          ref: `${state}_${suffix}`,
          state,
          projectRef: "project_one",
          version: 1,
        },
      ],
      nextPageToken: cursor,
    },
    response: new Response(null, { status: 200 }),
  };
}
beforeEach(() => {
  setActivePinia(createPinia());
  sdk.listRuns
    .mockReset()
    .mockImplementation((request: { query: { states: Run["state"][] } }) =>
      Promise.resolve(
        response(
          request.query.states[0] ?? "QUEUED",
          "first",
          `cursor_${String(request.query.states[0])}`,
        ),
      ),
    );
});
describe("независимые серверные колонки Kanban", () => {
  it("читает четыре фильтра и догружает только cursor выбранной колонки", async () => {
    const store = useRunBoardStore();
    const scope = {
      projectRef: "project_one",
      query: " задача ",
      filter: "ALL" as const,
    };
    await store.load(scope);
    expect(sdk.listRuns).toHaveBeenCalledTimes(4);
    expect(store.items).toHaveLength(4);
    expect(store.ready).toBe(true);
    sdk.listRuns.mockResolvedValueOnce(response("RUNNING", "second"));
    await store.load(scope, true, "RUNNING");
    expect(sdk.listRuns).toHaveBeenCalledTimes(5);
    expect(sdk.listRuns.mock.lastCall?.[0].query).toMatchObject({
      states: ["RUNNING", "CANCELLING"],
      pageToken: "cursor_RUNNING",
      pageSize: 40,
      query: "задача",
      projectRef: "project_one",
    });
    expect(store.columns.RUNNING.items).toHaveLength(2);
    expect(store.columns.QUEUED.pageToken).toBe("cursor_QUEUED");
  });
  it("изолирует ошибку колонки и не дублирует её медленный запрос", async () => {
    const store = useRunBoardStore();
    const scope = { query: "", filter: "ALL" as const };
    await store.load(scope);
    let finish!: (value: ReturnType<typeof response>) => void;
    sdk.listRuns.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const pending = store.load(scope, true, "QUEUED");
    await store.load(scope, true, "QUEUED");
    expect(sdk.listRuns).toHaveBeenCalledTimes(5);
    finish(response("RUNNING", "wrong_lane"));
    await pending;
    expect(store.columns.QUEUED.problem).toBeDefined();
    expect(store.columns.QUEUED.items[0]?.ref).toBe("QUEUED_first");
    expect(store.columns.RUNNING.problem).toBeUndefined();
  });
  it("смена проекта отменяет все старые запросы и закрыто отклоняет поздние ответы", async () => {
    const store = useRunBoardStore();
    const pending: {
      finish: (value: ReturnType<typeof response>) => void;
      signal: AbortSignal;
      state: Run["state"];
    }[] = [];
    sdk.listRuns.mockImplementation(
      (request: { signal: AbortSignal; query: { states: Run["state"][] } }) =>
        new Promise((finish) => {
          pending.push({
            finish,
            signal: request.signal,
            state: request.query.states[0] ?? "QUEUED",
          });
        }),
    );
    const old = store.load({
      projectRef: "project_one",
      query: "",
      filter: "ALL",
    });
    store.reset();
    sdk.listRuns.mockResolvedValue({
      data: { items: [] },
      response: new Response(null, { status: 200 }),
    });
    await store.load({
      projectRef: "project_two",
      query: "",
      filter: "TERMINAL",
    });
    for (const request of pending) {
      expect(request.signal.aborted).toBe(true);
      request.finish(response(request.state));
    }
    await old;
    expect(store.items).toEqual([]);
    expect(store.pageToken).toBeUndefined();
    expect(sdk.listRuns).toHaveBeenCalledTimes(5);
    expect(sdk.listRuns.mock.lastCall?.[0].query.states).toEqual([
      "SUCCEEDED",
      "FAILED",
      "CANCELLED",
    ]);
  });
});

it("переход между независимо прочитанными колонками показывает только новейшую версию", async () => {
  const store = useRunBoardStore();
  await store.load({ query: "", filter: "ALL" });
  store.columns.QUEUED.items = [
    { ref: "moving", state: "QUEUED", version: 1 } as Run,
  ];
  store.columns.RUNNING.items = [
    { ref: "moving", state: "RUNNING", version: 2 } as Run,
  ];
  expect(store.items.filter((run) => run.ref === "moving")).toEqual([
    { ref: "moving", state: "RUNNING", version: 2 },
  ]);
});
