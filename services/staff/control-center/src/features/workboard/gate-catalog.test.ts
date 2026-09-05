import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  listOwnerGates:
    vi.fn<(request: { query: Record<string, unknown> }) => Promise<unknown>>(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import { useGateCatalog } from "./gate-catalog";
const scope = {
  query: "текст %_",
  view: "HISTORY" as const,
  projectRef: "project_one",
};
const gate = (ref: string, state: OwnerGate["state"] = "APPROVED") =>
  ({ ref, state, projectRef: "project_one" }) as OwnerGate;
const response = (items: OwnerGate[], total = 91, nextPageToken = "") => ({
  data: { items, total, nextPageToken },
  response: new Response(null, { status: 200 }),
});
describe("серверные страницы решений", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.useRealTimers());
  it("объединяет realtime invalidation и отменяет refresh прежнего scope", async () => {
    vi.useFakeTimers();
    const catalog = useGateCatalog();
    sdk.listOwnerGates.mockResolvedValue(response([gate("one")]));
    await catalog.load(scope);
    catalog.invalidate(scope);
    catalog.invalidate(scope);
    await vi.advanceTimersByTimeAsync(250);
    expect(sdk.listOwnerGates).toHaveBeenCalledTimes(2);
    catalog.invalidate(scope);
    catalog.reset();
    await vi.advanceTimersByTimeAsync(500);
    expect(sdk.listOwnerGates).toHaveBeenCalledTimes(2);
    expect(catalog.items.value).toEqual([]);
  });
  it("передаёт history набор, literal query и cursor; сохраняет total и порядок", async () => {
    const catalog = useGateCatalog();
    sdk.listOwnerGates.mockResolvedValueOnce(
      response([gate("second")], 91, "next"),
    );
    await catalog.load(scope);
    expect(sdk.listOwnerGates).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        query: {
          projectRef: "project_one",
          query: "текст %_",
          states: [
            "APPROVED",
            "REJECTED",
            "CHANGES_REQUESTED",
            "CANCELLED",
            "EXPIRED",
          ],
          pageSize: 30,
          pageToken: undefined,
        },
      }),
    );
    sdk.listOwnerGates.mockResolvedValueOnce(
      response([gate("first", "EXPIRED")]),
    );
    await catalog.load(scope, true);
    expect(sdk.listOwnerGates.mock.calls[1]?.[0].query.pageToken).toBe("next");
    expect(catalog.items.value.map((item) => item.ref)).toEqual([
      "second",
      "first",
    ]);
    expect(catalog.total.value).toBe(91);
  });
  it("не принимает ответ прежнего фильтра после нового запроса", async () => {
    const catalog = useGateCatalog();
    let finish!: (value: ReturnType<typeof response>) => void;
    sdk.listOwnerGates.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const previous = catalog.load(scope);
    sdk.listOwnerGates.mockResolvedValueOnce(
      response([gate("new", "OPEN")], 1),
    );
    await catalog.load({ query: "new", view: "PENDING" });
    finish(response([gate("old")]));
    await previous;
    expect(catalog.items.value.map((item) => item.ref)).toEqual(["new"]);
    expect(catalog.total.value).toBe(1);
  });
  it("не использует cursor прежнего query и сбрасывает данные при dispose", async () => {
    const catalog = useGateCatalog();
    sdk.listOwnerGates.mockResolvedValueOnce(
      response([gate("one")], 91, "next"),
    );
    await catalog.load(scope);
    await catalog.load({ ...scope, query: "another" }, true);
    expect(sdk.listOwnerGates).toHaveBeenCalledTimes(1);
    catalog.reset();
    expect(catalog.items.value).toEqual([]);
    expect(catalog.total.value).toBeUndefined();
  });
  it.each([
    response([gate("one")], -1),
    response([gate("one", "OPEN")]),
    response([{ ...gate("one"), projectRef: "foreign" }]),
    response([gate("one")], Number.MAX_SAFE_INTEGER + 1),
  ])("отклоняет неверный total или чужую страницу", async (page) => {
    const catalog = useGateCatalog();
    sdk.listOwnerGates.mockResolvedValueOnce(page);
    await catalog.load(scope);
    expect(catalog.problem.value).toBeDefined();
    expect(catalog.items.value).toEqual([]);
    expect(catalog.total.value).toBeUndefined();
  });
  it("останавливает повтор cursor без добавления дублей", async () => {
    const catalog = useGateCatalog();
    sdk.listOwnerGates.mockResolvedValueOnce(
      response([gate("one")], 91, "next"),
    );
    await catalog.load(scope);
    sdk.listOwnerGates.mockResolvedValueOnce(
      response([gate("two")], 91, "next"),
    );
    await catalog.load(scope, true);
    expect(catalog.problem.value).toBeDefined();
    expect(catalog.items.value.map((item) => item.ref)).toEqual(["one"]);
  });
  it("удаляет прежние строки после отказа authority на следующей странице", async () => {
    const catalog = useGateCatalog();
    sdk.listOwnerGates.mockResolvedValueOnce(
      response([gate("one")], 91, "next"),
    );
    await catalog.load(scope);
    sdk.listOwnerGates.mockResolvedValueOnce({
      error: { status: 403, code: "FORBIDDEN" },
      response: new Response(null, { status: 403 }),
    });
    await catalog.load(scope, true);
    expect(catalog.problem.value?.status).toBe(403);
    expect(catalog.items.value).toEqual([]);
    expect(catalog.total.value).toBeUndefined();
    expect(catalog.pageToken.value).toBeUndefined();
  });
});
