import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RuntimeRevisionDiff } from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({ getRuntimeRevisionDiff: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import { loadRuntimeRevisionDiff } from "./runtime-revision-diff";
const current = {
  ref: "revision_current",
  version: 2,
  runRef: "run_current",
  sessionRef: "session_one",
  attempt: 2,
  revisionDigest: "a".repeat(64),
  createdAt: "2026-09-05T00:00:00Z",
};
function respond(data: unknown): void {
  sdk.getRuntimeRevisionDiff.mockResolvedValue({
    data,
    response: new Response(null, { status: 200 }),
  });
}
beforeEach(() => vi.clearAllMocks());
describe("runtime revision diff boundary", () => {
  it("разрешает previous другого запуска той же сессии и сохраняет exact metadata", async () => {
    const value: RuntimeRevisionDiff = {
      current,
      previous: {
        ...current,
        ref: "revision_previous",
        runRef: "run_previous",
        attempt: 1,
      },
      changes: [
        {
          component: "MODEL",
          previous: { ref: "previous-model" },
          current: { ref: "current-model" },
        },
      ],
    };
    respond(value);
    const signal = new AbortController().signal;
    await expect(
      loadRuntimeRevisionDiff("run_current", "session_one", signal),
    ).resolves.toEqual(value);
    expect(sdk.getRuntimeRevisionDiff).toHaveBeenCalledWith({
      path: { runRef: "run_current" },
      signal,
    });
  });
  it("различает первую ревизию без previous", async () => {
    respond({ current, changes: [] });
    await expect(
      loadRuntimeRevisionDiff(
        "run_current",
        "session_one",
        new AbortController().signal,
      ),
    ).resolves.toEqual({ current, changes: [] });
  });
  it.each([
    { current: { ...current, runRef: "foreign" }, changes: [] },
    {
      current,
      previous: { ...current, ref: "other", sessionRef: "foreign" },
      changes: [],
    },
    { current, changes: [{ component: "UNKNOWN", current: {} }] },
    {
      current,
      changes: [
        { component: "MODEL", current: {} },
        { component: "MODEL", current: {} },
      ],
    },
    { current: { ...current, revisionDigest: "invalid" }, changes: [] },
  ])("отклоняет несовместимое происхождение или компонент", async (value) => {
    respond(value);
    await expect(
      loadRuntimeRevisionDiff(
        "run_current",
        "session_one",
        new AbortController().signal,
      ),
    ).rejects.toThrow("boundary");
  });
});
