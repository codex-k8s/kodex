import { afterEach, describe, expect, it, vi } from "vitest";
import { effectScope, ref, type EffectScope } from "vue";
import {
  useKubernetesCapture,
  type KubernetesCaptureAPI,
} from "./kubernetes-capture";
import { AppProblem } from "@/shared/api/problem";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import type { KubernetesCaptureReadback } from "@/shared/api/generated/openapi/types.gen";
let scope: EffectScope | undefined;
afterEach(() => {
  scope?.stop();
  scope = undefined;
  resetOwnerRequests();
});
function fixture(): KubernetesCaptureReadback {
  return {
    view: {
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
  };
}
function harness() {
  const v = fixture(),
    context = ref({ ...v.view, captureRef: v.view.summary.captureRef });
  const port = {
    repairKubernetesStagingCleanup:
      vi.fn<KubernetesCaptureAPI["repairKubernetesStagingCleanup"]>(),
    readKubernetesCapture: vi
      .fn<KubernetesCaptureAPI["readKubernetesCapture"]>()
      .mockResolvedValueOnce(v),
    repairKubernetesCapture:
      vi.fn<KubernetesCaptureAPI["repairKubernetesCapture"]>(),
  };
  scope = effectScope();
  const model = scope.run(() => useKubernetesCapture(context, port));
  if (!model) throw new Error("Capture model scope unavailable");
  return { v, context, port, model };
}
describe("capture recovery from the Tool context", () => {
  for (const status of [401, 403, 404])
    it(`clears denied current capture after refresh ${String(status)}`, async () => {
      const h = harness();
      await vi.waitFor(() => expect(h.model.canRepair.value).toBe(true));
      h.port.readKubernetesCapture.mockRejectedValue(
        new AppProblem({
          status,
          code:
            status === 401
              ? "UNAUTHORIZED"
              : status === 403
                ? "FORBIDDEN"
                : "NOT_FOUND",
          kind:
            status === 401
              ? "unauthorized"
              : status === 403
                ? "forbidden"
                : "not-found",
          retryable: false,
        }),
      );
      await h.model.refresh();
      expect(h.model.current.value).toBeUndefined();
      expect(h.model.canRepair.value).toBe(false);
      expect(h.model.hidden.value).toBe(true);
      h.port.readKubernetesCapture.mockResolvedValue(h.v);
      await h.model.refresh();
      expect(h.model.hidden.value).toBe(false);
      expect(h.model.canRepair.value).toBe(true);
    });

  it("uses one POST and one original-key metadata read after a lost acknowledgement", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.canRepair.value).toBe(true));
    const accepted = structuredClone(h.v);
    Object.assign(accepted.view, { repairGeneration: 1 });
    Object.assign(accepted.view.summary, {
      state: "READY",
      version: 6,
      reasonCode: "",
    });
    accepted.repairReceipt = {
      captureRef: h.v.view.summary.captureRef,
      repairGeneration: 1,
      version: 6,
      attemptCount: 3,
      acceptedAt: "2026-11-16T02:01:00Z",
    };
    h.port.readKubernetesCapture.mockResolvedValue(accepted);
    h.port.repairKubernetesCapture.mockRejectedValue(
      new Error("lost acknowledgement"),
    );
    await h.model.repair();
    expect(h.port.readKubernetesCapture).toHaveBeenCalledTimes(2);
    expect(h.port.repairKubernetesCapture).toHaveBeenCalledTimes(1);
    expect(h.port.readKubernetesCapture.mock.calls[1]?.[3]).toBe(
      h.port.repairKubernetesCapture.mock.calls[0]?.[3],
    );
    expect(h.model.current.value?.view.attemptCount).toBe(3);
    expect(h.model.uncertain.value).toBe(false);
  });
  it("retains uncertainty without a second POST when metadata readback is unavailable", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.canRepair.value).toBe(true));
    h.port.readKubernetesCapture.mockRejectedValue(
      new Error("read unavailable"),
    );
    h.port.repairKubernetesCapture.mockRejectedValue(
      new Error("lost acknowledgement"),
    );
    await h.model.repair();
    await h.model.repair();
    expect(h.port.readKubernetesCapture).toHaveBeenCalledTimes(2);
    expect(h.port.repairKubernetesCapture).toHaveBeenCalledTimes(1);
    expect(h.model.uncertain.value).toBe(true);
    expect(h.model.canRepair.value).toBe(false);
  });
  it("hides cached metadata when lost-ACK readback is denied", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.canRepair.value).toBe(true));
    h.port.repairKubernetesCapture.mockRejectedValue(new Error("lost reply"));
    h.port.readKubernetesCapture.mockRejectedValue(
      new AppProblem({
        status: 403,
        code: "FORBIDDEN",
        kind: "forbidden",
        retryable: false,
      }),
    );
    await h.model.repair();
    expect(h.model.current.value).toBeUndefined();
    expect(h.model.hidden.value).toBe(true);
    expect(h.model.canRepair.value).toBe(false);
    expect(h.model.uncertain.value).toBe(true);
    expect(h.port.repairKubernetesCapture).toHaveBeenCalledTimes(1);
  });
  it("discards capture metadata on owner invalidation", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.canRepair.value).toBe(true));
    resetOwnerRequests();
    expect(h.model.current.value).toBeUndefined();
    expect(h.model.inactive.value).toBe(true);
    expect(h.model.canRepair.value).toBe(false);
  });
  it("discards a late reply after switching node", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.canRepair.value).toBe(true));
    let finish!: (v: KubernetesCaptureReadback) => void;
    h.port.repairKubernetesCapture.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const pending = h.model.repair();
    h.port.readKubernetesCapture.mockRejectedValue(new Error("foreign scope"));
    h.context.value = { ...h.context.value, nodeRef: "nod_foreignxx" };
    await vi.waitFor(() => expect(h.model.current.value).toBeUndefined());
    finish(h.v);
    await pending;
    expect(h.model.current.value).toBeUndefined();
  });
});

it("recovers one lost cleanup ACK by its original key without publication/native retry", async () => {
  const h = harness();
  h.v.view.cleanup = {
    version: 1,
    repairGeneration: 0,
    outstandingCount: 2,
    outstandingBytes: 30,
    exhaustedCount: 1,
    eligibleExhaustedCount: 1,
  };
  await vi.waitFor(() => expect(h.model.canRepairCleanup.value).toBe(true));
  const recovered: KubernetesCaptureReadback = {
    view: {
      ...h.v.view,
      cleanup: {
        ...h.v.view.cleanup,
        version: 2,
        repairGeneration: 1,
        exhaustedCount: 0,
        eligibleExhaustedCount: 0,
      },
    },
    cleanupReceipt: {
      captureRef: h.v.view.summary.captureRef,
      version: 2,
      repairGeneration: 1,
      repairedCount: 1,
      remainingExhaustedCount: 0,
      acceptedAt: "2026-11-16T02:00:01Z",
    },
  };
  h.port.repairKubernetesStagingCleanup.mockRejectedValue(
    new Error("lost cleanup ACK"),
  );
  h.port.readKubernetesCapture.mockResolvedValue(recovered);
  await h.model.repairCleanup();
  expect(h.port.repairKubernetesStagingCleanup).toHaveBeenCalledTimes(1);
  expect(h.port.repairKubernetesCapture).not.toHaveBeenCalled();
  const key = h.port.repairKubernetesStagingCleanup.mock.calls[0]?.[3];
  expect(h.port.readKubernetesCapture.mock.calls.at(-1)?.slice(3)).toEqual([
    "",
    key,
  ]);
  expect(h.model.uncertain.value).toBe(false);
  expect(h.model.current.value?.view.cleanup?.outstandingBytes).toBe(30);
  expect(h.model.canRepairCleanup.value).toBe(false);
});

it("hides cached cleanup counters and repair after current-view denial", async () => {
  const h = harness();
  h.v.view.cleanup = {
    version: 1,
    repairGeneration: 0,
    outstandingCount: 1,
    outstandingBytes: 10,
    exhaustedCount: 1,
    eligibleExhaustedCount: 1,
  };
  await vi.waitFor(() => expect(h.model.canRepairCleanup.value).toBe(true));
  h.port.readKubernetesCapture.mockRejectedValue(
    new AppProblem({
      status: 403,
      code: "FORBIDDEN",
      kind: "forbidden",
      retryable: false,
    }),
  );
  await h.model.refresh();
  expect(h.model.current.value).toBeUndefined();
  expect(h.model.hidden.value).toBe(true);
  expect(h.model.canRepairCleanup.value).toBe(false);
});
