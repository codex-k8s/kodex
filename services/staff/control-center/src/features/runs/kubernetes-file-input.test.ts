import { computed, effectScope, ref, type EffectScope } from "vue";
import { afterEach, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import { AppProblem } from "@/shared/api/problem";
import { fileInputFixture } from "@/shared/lib/kubernetes-file-input.test-support";
import { useKubernetesFileInputs } from "./kubernetes-file-input";
let scope: EffectScope | undefined;
afterEach(() => {
  scope?.stop();
  resetOwnerRequests();
  vi.useRealTimers();
});
function harness() {
  const value = fileInputFixture(),
    context = ref({
      invocationRef: value.invocationRef,
      runRef: "run_synthetic",
      nodeRef: "nod_synthetic",
      active: true,
    });
  const port = {
    listKubernetesFileCleanup: vi
      .fn()
      .mockRejectedValue(new Error("Unavailable")),
    listKubernetesFileInputs: vi.fn().mockResolvedValue([value]),
  };
  scope = effectScope();
  const model = scope.run(() =>
    useKubernetesFileInputs(
      computed(() => context.value),
      port,
    ),
  );
  if (!model) throw Error("Synthetic FILE scope is missing");
  return { model, context, port, value };
}
it("discovers metadata before terminal and polls without a preparation mutation", async () => {
  vi.useFakeTimers();
  const h = harness();
  await vi.advanceTimersByTimeAsync(0);
  expect(h.model.items.value).toHaveLength(1);
  expect(h.port.listKubernetesFileInputs).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(2000);
  expect(h.port.listKubernetesFileInputs).toHaveBeenCalledTimes(2);
  h.context.value.active = false;
  await vi.advanceTimersByTimeAsync(8000);
  expect(h.port.listKubernetesFileInputs).toHaveBeenCalledTimes(2);
});
for (const status of [401, 403, 404])
  it(`clears cached metadata on denied refresh ${String(status)} and permits explicit fresh recovery`, async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
    h.port.listKubernetesFileInputs.mockRejectedValueOnce(
      new AppProblem({
        status,
        title: "Unavailable",
        code: "FORBIDDEN",
        kind: "forbidden",
        retryable: false,
      }),
    );
    await h.model.refresh();
    expect(h.model.items.value).toEqual([]);
    expect(h.model.problem.value).toBeDefined();
    await h.model.refresh();
    expect(h.model.items.value).toHaveLength(1);
  });
it("discards a late response after invocation change and aborts on owner invalidation", async () => {
  const h = harness();
  await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
  let release: (
    value: ReturnType<typeof fileInputFixture>[],
  ) => void = () => {};
  h.port.listKubernetesFileInputs.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  const old = h.model.refresh();
  h.context.value.invocationRef = "inv_foreign00";
  h.port.listKubernetesFileInputs.mockResolvedValue([]);
  await Promise.resolve();
  release([h.value]);
  await old;
  expect(h.model.items.value).toEqual([]);
  resetOwnerRequests();
  expect(h.model.items.value).toEqual([]);
  expect(h.model.inactive.value).toBe(true);
});

it("source denial can discover only cleanup metadata and never restores artifact provenance", async () => {
  const h = harness();
  await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
  const minimal = {
    sourceRef: h.value.sourceRef,
    invocationRef: h.value.invocationRef,
    cleanup: {
      version: 1,
      repairGeneration: 0,
      outstandingCount: 1,
      outstandingBytes: 2048,
      exhaustedCount: 1,
      eligibleExhaustedCount: 1,
    },
  };
  h.port.listKubernetesFileInputs.mockRejectedValueOnce(
    new AppProblem({
      status: 403,
      code: "FORBIDDEN",
      kind: "forbidden",
      retryable: false,
    }),
  );
  h.port.listKubernetesFileCleanup.mockResolvedValueOnce([minimal]);
  await h.model.refresh();
  expect(h.model.items.value).toEqual([]);
  expect(h.model.cleanupItems.value).toEqual([minimal]);
  expect(h.model.problem.value).toBeUndefined();
});
