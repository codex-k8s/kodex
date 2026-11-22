import { afterEach, describe, expect, it, vi } from "vitest";
import { effectScope, ref, type EffectScope } from "vue";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import { parseKubernetesTool } from "@/shared/api/kubernetes-tool";
import type { KubernetesToolView } from "@/shared/api/generated/openapi/types.gen";
import { useKubernetesTools, type KubernetesToolAPI } from "./kubernetes-tool";
let scope: EffectScope | undefined;
afterEach(() => {
  scope?.stop();
  scope = undefined;
  resetOwnerRequests();
  vi.useRealTimers();
});
function fixture(): KubernetesToolView {
  return {
    invocationRef: "inv_synthetic",
    runRef: "run_synthetic",
    nodeRef: "nod_synthetic",
    connectionRef: "int_synthetic",
    targetRef: "target_synthetic",
    targetRevision: 1,
    operation: "kubernetes.exec",
    state: "RUNNING",
    version: 3,
    waitingForGate: true,
    safeReasonCode: "NONE",
    captureStatuses: [],
  };
}
function harness() {
  const view = fixture(),
    context = ref({ runRef: view.runRef, nodeRef: view.nodeRef });
  const port = {
    listKubernetesTools: vi
      .fn<KubernetesToolAPI["listKubernetesTools"]>()
      .mockResolvedValue({ items: [view], nextPageToken: "" }),
    readKubernetesTool: vi.fn<KubernetesToolAPI["readKubernetesTool"]>(),
    stopKubernetesTool: vi.fn<KubernetesToolAPI["stopKubernetesTool"]>(),
  };
  scope = effectScope();
  const model = scope.run(() => useKubernetesTools(context, port));
  if (!model) throw new Error("Missing test scope");
  return { view, context, port, model };
}
describe("Kubernetes pre-stream tool closure", () => {
  it("shows a pending Gate before any stream exists", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
    expect(h.model.items.value[0]?.waitingForGate).toBe(true);
    expect(h.port.stopKubernetesTool).not.toHaveBeenCalled();
  });
  it("performs one Stop and only a metadata Get after lost ACK", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
    h.port.stopKubernetesTool.mockRejectedValue(new Error("lost reply"));
    h.port.readKubernetesTool.mockResolvedValue({
      ...h.view,
      state: "CANCELLED",
      version: 4,
      waitingForGate: false,
    });
    await h.model.stop(h.view);
    expect(h.port.stopKubernetesTool).toHaveBeenCalledTimes(1);
    expect(h.port.readKubernetesTool).toHaveBeenCalledTimes(1);
    expect(h.model.items.value[0]?.state).toBe("CANCELLED");
    expect(h.model.uncertain.value).toBe(false);
  });
  it("preserves uncertainty when both Stop and readback fail", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
    h.port.stopKubernetesTool.mockRejectedValue(new Error("lost reply"));
    h.port.readKubernetesTool.mockRejectedValue(new Error("lost read"));
    await h.model.stop(h.view);
    expect(h.port.stopKubernetesTool).toHaveBeenCalledTimes(1);
    expect(h.model.uncertain.value).toBe(true);
    await h.model.stop(h.view);
    expect(h.port.stopKubernetesTool).toHaveBeenCalledTimes(1);
    expect(h.model.items.value[0]?.state).toBe("RUNNING");
  });
  it("rejects a late Stop response after switching nodes", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
    let finish!: (v: KubernetesToolView) => void;
    h.port.stopKubernetesTool.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const pending = h.model.stop(h.view);
    h.port.listKubernetesTools.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    h.context.value = { runRef: "run_foreignxx", nodeRef: "nod_foreignxx" };
    await vi.waitFor(() => expect(h.model.items.value).toHaveLength(0));
    finish({
      ...h.view,
      state: "CANCELLED",
      version: 4,
      waitingForGate: false,
    });
    await pending;
    expect(h.model.items.value).toHaveLength(0);
    expect(h.model.problem.value).toBeUndefined();
  });
  it("discards metadata after owner invalidation", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
    let finish!: (v: KubernetesToolView) => void;
    h.port.stopKubernetesTool.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const pending = h.model.stop(h.view);
    resetOwnerRequests();
    finish({
      ...h.view,
      state: "CANCELLED",
      version: 4,
      waitingForGate: false,
    });
    await pending;
    expect(h.model.items.value).toHaveLength(0);
    expect(h.model.writing.value).toBe(false);
  });
  it("reads the exact persisted profile rejection without any stream", () => {
    const view = {
      ...fixture(),
      operation: "kubernetes.port-forward",
      state: "FAILED",
      waitingForGate: false,
      safeReasonCode: "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED",
    } as const;
    expect(parseKubernetesTool(view, view)).toEqual(view);
  });
  it("rejects foreign metadata, unknown fields, invalid phases and inexact versions", () => {
    const view = fixture();
    expect(parseKubernetesTool(view, view)).toEqual(view);
    for (const invalid of [
      { ...view, nodeRef: "nod_foreignxx" },
      { ...view, credential: "private" },
      { ...view, state: "FUTURE" },
      { ...view, state: "CANCELLED" },
      { ...view, version: 2 ** 53 },
      { ...view, operation: "shell.exec" },
      { ...view, safeReasonCode: "private upstream diagnostic" },
      { ...view, safeReasonCode: undefined },
      {
        ...view,
        safeReasonCode: "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED",
      },
    ])
      expect(() => parseKubernetesTool(invalid, view)).toThrow();
  });
});
