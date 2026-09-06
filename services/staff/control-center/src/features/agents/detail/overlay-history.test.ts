import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const api = vi.hoisted(() => ({
  listConfigOverlayRevisions: vi.fn(),
  getConfigOverlayRevision: vi.fn(),
  rollbackConfigOverlay: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) =>
    signal ?? new AbortController().signal,
}));
vi.mock("@/shared/api/problem", () => ({
  unwrap: (value: unknown) => Promise.resolve(value),
}));
import {
  loadOverlayHistory,
  readOverlayRevision,
  restoreOverlayRevision,
} from "./overlay-history";
const revision = {
  ref: "overlay-one",
  revision: 4,
  version: 1,
  state: "SUPERSEDED",
  digest: "a".repeat(64),
  content: 'personality = "friendly"',
  validationMessages: [],
  createdAt: "2026-09-05T00:00:00Z",
};
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"a".repeat(43)}` });
});
afterEach(() => vi.unstubAllGlobals());
describe("защищённая история overlay", () => {
  it("передаёт actor-scoped agent, поиск/cursor и сохраняет server total", async () => {
    api.listConfigOverlayRevisions.mockResolvedValue({
      data: { items: [revision], total: 43, nextPageToken: "next" },
    });
    const signal = new AbortController().signal;
    expect(
      (await loadOverlayHistory("agent-one", "friendly", "cursor", signal))
        .total,
    ).toBe(43);
    expect(api.listConfigOverlayRevisions).toHaveBeenCalledWith({
      path: { agentRef: "agent-one" },
      query: { query: "friendly", pageToken: "cursor", pageSize: 30 },
      signal,
    });
  });
  it.each(["DRAFT", "VALID", "INVALID"])(
    "не выдаёт %s за published history",
    async (state) => {
      api.getConfigOverlayRevision.mockResolvedValue({
        data: { ...revision, state },
      });
      await expect(
        readOverlayRevision(
          "agent-one",
          revision.ref,
          new AbortController().signal,
        ),
      ).rejects.toThrow("Invalid published");
    },
  );
  it("отклоняет чужой exact ref и поздний ответ после закрытия", async () => {
    api.getConfigOverlayRevision.mockResolvedValue({
      data: { ...revision, ref: "other" },
    });
    await expect(
      readOverlayRevision(
        "agent-one",
        revision.ref,
        new AbortController().signal,
      ),
    ).rejects.toThrow("mismatch");
    const controller = new AbortController();
    api.getConfigOverlayRevision.mockImplementation(() => {
      controller.abort();
      return { data: revision };
    });
    await expect(
      readOverlayRevision("agent-one", revision.ref, controller.signal),
    ).rejects.toThrow();
  });
  it("rollback передаёт только выбранный published ref и текущий agent OCC", async () => {
    api.rollbackConfigOverlay.mockResolvedValue({
      data: { agentVersion: 8 },
      response: new Response(null, { status: 200 }),
    });
    await restoreOverlayRevision("agent-one", revision.ref, 7);
    const call = api.rollbackConfigOverlay.mock.calls[0]?.[0] as {
      path: unknown;
      body: unknown;
      headers: Record<string, string>;
    };
    expect(call.path).toEqual({ agentRef: "agent-one" });
    expect(call.body).toEqual({ publishedOverlayRef: revision.ref });
    expect(call.headers["If-Match"]).toBe('"7"');
  });
});
