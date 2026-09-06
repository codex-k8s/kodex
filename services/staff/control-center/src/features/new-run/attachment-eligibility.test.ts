import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RunAttachmentEligibility } from "@/shared/api/generated/openapi/types.gen";
const calls = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  getRunAttachmentEligibility: calls.get,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
import { loadAttachmentEligibility } from "./attachment-eligibility";

const scope = {
  projectRef: "project_one",
  targetType: "WORKFLOW" as const,
  targetRef: "workflow_one",
  runRef: "run_one",
};
const result: RunAttachmentEligibility = {
  ...scope,
  runVersion: 7,
  eligible: true,
  reason: "AVAILABLE",
  digest: "a".repeat(64),
  evaluatedAt: "2026-09-05T12:00:00Z",
};
function respond(value: unknown) {
  calls.get.mockResolvedValue({
    data: value,
    response: new Response(null, { status: 200 }),
  });
}
beforeEach(() => calls.get.mockReset());
describe("доступность вложений выбранной цели", () => {
  it("передаёт exact target и continuation Run одним owner query", async () => {
    respond(result);
    expect(
      await loadAttachmentEligibility(scope, new AbortController().signal),
    ).toEqual(result);
    expect(calls.get).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        path: { projectRef: scope.projectRef },
        query: {
          targetType: scope.targetType,
          targetRef: scope.targetRef,
          runRef: scope.runRef,
        },
      }),
    );
  });
  it("сохраняет aggregate refusal для частично неготового Процесса", async () => {
    respond({ ...result, eligible: false, reason: "RUNTIME_NOT_READY" });
    expect(
      await loadAttachmentEligibility(scope, new AbortController().signal),
    ).toMatchObject({ eligible: false, reason: "RUNTIME_NOT_READY" });
  });
  it.each([
    { projectRef: "foreign" },
    { targetRef: "another" },
    { targetType: "AGENT" },
    { runRef: "another" },
    { runRef: undefined },
    { eligible: false },
    { reason: "UNKNOWN" },
    { digest: "" },
    { runVersion: -1 },
  ])(
    "закрыто отклоняет чужой scope или некорректный ответ %j",
    async (change) => {
      respond({ ...result, ...change });
      await expect(
        loadAttachmentEligibility(scope, new AbortController().signal),
      ).rejects.toThrow("Invalid attachment eligibility");
    },
  );
});
