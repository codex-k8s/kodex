import { computed, effectScope, ref, type EffectScope } from "vue";
import { afterEach, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import { useKubernetesFileCleanup } from "./kubernetes-file-cleanup";
let scope: EffectScope | undefined;
afterEach(() => {
  scope?.stop();
  resetOwnerRequests();
});
function harness(repairAvailable = true) {
  const context = ref({
    invocationRef: "inv_synthetic",
    sourceRef: "kfs_synthetic",
    runRef: "run_synthetic",
    nodeRef: "nod_synthetic",
    active: false,
  });
  const readback = {
    projection: "CLEANUP_ONLY" as const,
    cleanupView: {
      sourceRef: context.value.sourceRef,
      invocationRef: context.value.invocationRef,
      ...(repairAvailable ? { repairAvailable } : {}),
      cleanup: {
        version: 3,
        repairGeneration: 0,
        outstandingCount: 1,
        outstandingBytes: 2048,
        exhaustedCount: 1,
        eligibleExhaustedCount: 1,
      },
    },
  };
  const port = {
    readKubernetesFileCleanup: vi.fn().mockResolvedValue(readback),
    repairKubernetesFileCleanup: vi.fn(),
  };
  scope = effectScope();
  const model = scope.run(() =>
    useKubernetesFileCleanup(
      computed(() => context.value),
      port,
    ),
  );
  if (!model) throw Error("Synthetic FILE cleanup scope missing");
  return { model, context, readback, port };
}
it("the legacy readback preserves cleanup status and never enables a repair POST", async () => {
  const h = harness(false);
  await vi.waitFor(() => expect(h.model.current.value).toEqual(h.readback));
  expect(h.model.canRepair.value).toBe(false);
  await h.model.repair();
  expect(h.port.repairKubernetesFileCleanup).not.toHaveBeenCalled();
});
it("lost repair ACK causes one original-key GET and blocks a second POST without receipt", async () => {
  const h = harness();
  await vi.waitFor(() => expect(h.model.canRepair.value).toBe(true));
  h.port.repairKubernetesFileCleanup.mockRejectedValue(new Error("Lost ACK"));
  await h.model.repair();
  expect(h.port.repairKubernetesFileCleanup).toHaveBeenCalledTimes(1);
  const key: unknown = h.port.repairKubernetesFileCleanup.mock.calls[0]?.[3];
  expect(key).toBeTruthy();
  expect(h.port.readKubernetesFileCleanup).toHaveBeenLastCalledWith(
    "inv_synthetic",
    "kfs_synthetic",
    expect.any(AbortSignal),
    key,
  );
  expect(h.model.uncertain.value).toBe(true);
  expect(h.model.canRepair.value).toBe(false);
  await h.model.repair();
  expect(h.port.repairKubernetesFileCleanup).toHaveBeenCalledTimes(1);
});
it("failed readback hides cached counters and explicit fresh GET recovers without POST", async () => {
  const h = harness();
  await vi.waitFor(() => expect(h.model.current.value).toBeDefined());
  h.port.readKubernetesFileCleanup.mockRejectedValueOnce(
    new Error("Forbidden"),
  );
  await h.model.refresh();
  expect(h.model.current.value).toBeUndefined();
  expect(h.model.canRepair.value).toBe(false);
  await h.model.refresh();
  expect(h.model.canRepair.value).toBe(true);
  expect(h.port.repairKubernetesFileCleanup).not.toHaveBeenCalled();
});
it("context change discards a late cleanup mutation and owner invalidation clears counters", async () => {
  const h = harness();
  await vi.waitFor(() => expect(h.model.canRepair.value).toBe(true));
  let release: (v: unknown) => void = () => {};
  h.port.repairKubernetesFileCleanup.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  const pending = h.model.repair();
  h.port.readKubernetesFileCleanup.mockRejectedValue(new Error("Not found"));
  h.context.value.sourceRef = "kfs_foreign00";
  await Promise.resolve();
  release(h.readback);
  await pending;
  expect(h.model.current.value).toBeUndefined();
  resetOwnerRequests();
  expect(h.model.inactive.value).toBe(true);
  expect(h.model.canRepair.value).toBe(false);
});
