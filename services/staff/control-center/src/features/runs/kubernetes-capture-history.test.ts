import { afterEach, describe, expect, it, vi } from "vitest";
import { effectScope, ref, type EffectScope } from "vue";
import { useKubernetesCaptureHistory } from "./kubernetes-capture-history";
import type { KubernetesCapturePage } from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
let scope: EffectScope | undefined;
afterEach(() => {
  scope?.stop();
  scope = undefined;
  resetOwnerRequests();
});
function fixture(): KubernetesCapturePage {
  return {
    items: [
      {
        summary: {
          captureRef: "koc_synthetic",
          streamRef: "kst_synthetic",
          captureGeneration: 1,
          version: 5,
          state: "WAITING_OWNER",
          reasonCode: "CAPTURE_RETRY_EXHAUSTED",
        },
        invocationRef: "inv_synthetic",
        runRef: "run_synthetic",
        nodeRef: "nod_synthetic",
        operation: "kubernetes.exec",
        kind: "NONE",
        attemptCount: 3,
        repairGeneration: 0,
        sealedAt: "2026-11-16T02:00:00Z",
        artifacts: [],
      },
    ],
    nextPageToken: "",
  };
}
describe("original-root capture history lifecycle", () => {
  it("removes captures and child repair contexts after current visibility is denied", async () => {
    const context = ref({ runRef: "run_synthetic", nodeRef: "nod_synthetic" });
    const port = {
      listKubernetesCaptures: vi.fn().mockResolvedValue(fixture()),
    };
    scope = effectScope();
    const model = scope.run(() => useKubernetesCaptureHistory(context, port));
    if (!model) throw new Error("History scope unavailable");
    await vi.waitFor(() => expect(model.items.value).toHaveLength(1));
    port.listKubernetesCaptures.mockRejectedValue(
      new AppProblem({
        status: 403,
        code: "FORBIDDEN",
        kind: "forbidden",
        retryable: false,
      }),
    );
    await model.refresh();
    expect(model.items.value).toEqual([]);
    expect(model.nextPageToken.value).toBe("");
    port.listKubernetesCaptures.mockResolvedValue(fixture());
    await model.refresh();
    expect(model.items.value).toHaveLength(1);
    resetOwnerRequests();
    expect(model.items.value).toEqual([]);
    expect(model.busy.value).toBe(false);
  });
  it("ignores a late page from a previous node and aborts its request", async () => {
    let resolve: ((value: KubernetesCapturePage) => void) | undefined;
    const pending = new Promise<KubernetesCapturePage>((r) => {
      resolve = r;
    });
    const context = ref({ runRef: "run_synthetic", nodeRef: "nod_synthetic" });
    const port = {
      listKubernetesCaptures: vi
        .fn()
        .mockReturnValueOnce(pending)
        .mockResolvedValue({ items: [], nextPageToken: "" }),
    };
    scope = effectScope();
    const model = scope.run(() => useKubernetesCaptureHistory(context, port));
    if (!model) throw new Error("History scope unavailable");
    const firstSignal = port.listKubernetesCaptures.mock.calls[0]?.[1] as
      | AbortSignal
      | undefined;
    context.value = { runRef: "run_synthetic", nodeRef: "nod_successor" };
    await vi.waitFor(() =>
      expect(port.listKubernetesCaptures).toHaveBeenCalledTimes(2),
    );
    resolve?.(fixture());
    await Promise.resolve();
    expect(model.items.value).toEqual([]);
    expect(firstSignal?.aborted).toBe(true);
  });
});
