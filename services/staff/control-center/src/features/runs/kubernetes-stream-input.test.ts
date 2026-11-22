import { afterEach, describe, expect, it, vi } from "vitest";
import { effectScope, ref, type EffectScope } from "vue";
import { AppProblem } from "@/shared/api/problem";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import {
  kubernetesInputDigest,
  KubernetesInputProtocolError,
} from "@/shared/api/kubernetes-stream-input";
import type {
  KubernetesStreamInputView,
  KubernetesStreamInputReadback,
} from "@/shared/api/generated/openapi/types.gen";
import {
  useKubernetesStreamInput,
  type KubernetesInputAPI,
} from "./kubernetes-stream-input";
let scope: EffectScope | undefined;
afterEach(() => {
  scope?.stop();
  scope = undefined;
  resetOwnerRequests();
  vi.useRealTimers();
});
function fixture(): KubernetesStreamInputView {
  return {
    invocationRef: "inv_synthetic",
    streamRef: "kst_synthetic",
    runRef: "run_synthetic",
    nodeRef: "nod_synthetic",
    generation: 7,
    operation: "kubernetes.exec",
    namespace: "example",
    podName: "web",
    container: "app",
    state: "ACTIVE",
    version: 4,
    inputChannels: ["STDIN", "TERMINAL_RESIZE"],
    closedInputChannels: [],
    maximumFrameBytes: 32768,
    maximumTotalBytes: 65536,
    maximumStagedBytes: 1073741824,
    maximumStagedObjects: 32768,
    acknowledgedSequence: 0,
    hardDeadline: "2099-11-15T12:00:00Z",
    reasonCode: "",
    targetRef: "target_review",
    targetRevision: 2,
    connectionRef: "int_synthetic",
    resource: {
      group: "",
      version: "v1",
      resource: "pods",
      namespace: "example",
      name: "web",
      uid: "30000000-0000-4000-8000-000000000001",
      resourceVersion: "18",
      subresource: "",
      namespaced: true,
      allNamespaces: false,
    },
  };
}
function harness(view = fixture()) {
  const context = ref({ runRef: view.runRef, nodeRef: view.nodeRef });
  const port = {
    listKubernetesStreamInputs: vi
      .fn<KubernetesInputAPI["listKubernetesStreamInputs"]>()
      .mockResolvedValue({ items: [view], nextPageToken: "" }),
    readKubernetesStreamInput: vi
      .fn<KubernetesInputAPI["readKubernetesStreamInput"]>()
      .mockResolvedValue({ view }),
    submitKubernetesStreamInput:
      vi.fn<KubernetesInputAPI["submitKubernetesStreamInput"]>(),
    stopKubernetesStream: vi.fn<KubernetesInputAPI["stopKubernetesStream"]>(),
    kubernetesInputDigest,
  };
  scope = effectScope();
  const model = scope.run(() => useKubernetesStreamInput(context, port));
  if (!model) throw new Error("Fixture scope is unavailable");
  return { model, port, context, view };
}
async function ready(h: ReturnType<typeof harness>) {
  await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
  const item = h.model.items.value[0];
  if (!item) throw new Error("Fixture stream is missing");
  await h.model.choose(item);
}
describe("Kubernetes stream input owner lifecycle", () => {
  it("discovers before terminal result and requires explicit selection", async () => {
    const h = harness();
    await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
    expect(h.model.selected.value).toBeUndefined();
    expect(h.model.canSend.value).toBe(false);
    expect(h.port.listKubernetesStreamInputs.mock.calls[0]?.[0]).toEqual({
      runRef: h.view.runRef,
      nodeRef: h.view.nodeRef,
    });
    const item = h.model.items.value[0];
    if (!item) throw new Error("Fixture stream is missing");
    await h.model.choose(item);
    expect(h.model.canSend.value).toBe(true);
  });
  it("lost Submit ACK reads one exact key, never repeats bytes, and labels only local acknowledgement", async () => {
    const h = harness();
    await ready(h);
    let owned: Uint8Array | undefined;
    let key = "";
    h.port.submitKubernetesStreamInput.mockImplementation(
      (_view, channel, bytes, eof, _signal, mutationKey) => {
        owned = bytes;
        key = mutationKey;
        expect(channel).toBe("STDIN");
        expect(eof).toBe(false);
        return Promise.reject(new Error("lost ACK"));
      },
    );
    h.port.readKubernetesStreamInput.mockImplementation(
      async (_view, _signal, lookup) => {
        expect(lookup).toBe(key);
        return {
          view: { ...h.view, version: 5 },
          submission: {
            ref: "kif_synthetic",
            invocationRef: h.view.invocationRef,
            streamRef: h.view.streamRef,
            generation: 7,
            sequence: 1,
            channel: "STDIN",
            payloadDigest: await kubernetesInputDigest(
              new TextEncoder().encode("hello"),
            ),
            byteCount: 5,
            endOfInput: false,
            state: "ACKNOWLEDGED",
          },
        };
      },
    );
    h.model.draft.value = "hello";
    await h.model.send();
    expect(h.port.submitKubernetesStreamInput).toHaveBeenCalledOnce();
    expect(h.port.readKubernetesStreamInput).toHaveBeenCalledTimes(2);
    expect(h.model.uncertain.value).toBe(false);
    expect(h.model.selected.value?.version).toBe(5);
    expect(h.model.draft.value).toBe("");
    expect(owned?.every((b) => b === 0)).toBe(true);
  });
  it("unknown lost-ACK read leaves writes closed without automatic retransmission", async () => {
    const h = harness();
    await ready(h);
    h.port.submitKubernetesStreamInput.mockRejectedValue(new Error("lost ACK"));
    h.port.readKubernetesStreamInput.mockRejectedValue(new Error("unknown"));
    h.model.draft.value = "private";
    await h.model.send();
    expect(h.model.uncertain.value).toBe(true);
    expect(h.model.canSend.value).toBe(false);
    expect(h.model.canStop.value).toBe(true);
    h.model.draft.value = "again";
    await h.model.send();
    expect(h.port.submitKubernetesStreamInput).toHaveBeenCalledOnce();
    expect(h.port.readKubernetesStreamInput).toHaveBeenCalledTimes(2);
  });
  it("malformed positive response does not trigger a reconciliation read", async () => {
    const h = harness();
    await ready(h);
    h.port.submitKubernetesStreamInput.mockRejectedValue(
      new KubernetesInputProtocolError(),
    );
    h.model.draft.value = "private";
    await h.model.send();
    expect(h.model.uncertain.value).toBe(true);
    expect(h.port.readKubernetesStreamInput).toHaveBeenCalledOnce();
  });
  it("Stop closes one stream, lost ACK gets metadata once and never invokes Run cancellation", async () => {
    const h = harness();
    await ready(h);
    h.port.stopKubernetesStream.mockRejectedValue(new Error("lost ACK"));
    h.port.readKubernetesStreamInput.mockResolvedValue({
      view: { ...h.view, state: "CANCELED", version: 5 },
    });
    await h.model.stop();
    expect(h.port.stopKubernetesStream).toHaveBeenCalledOnce();
    expect(h.port.readKubernetesStreamInput).toHaveBeenCalledTimes(2);
    expect(h.port.readKubernetesStreamInput.mock.calls[1]?.[2]).toBeUndefined();
    expect(h.model.selected.value?.state).toBe("CANCELED");
    expect(h.model.canStop.value).toBe(false);
  });
  it("keeps EOF separate from Stop and emits the existing typed resize shape", async () => {
    const h = harness();
    await ready(h);
    h.port.submitKubernetesStreamInput.mockImplementation(
      async (_view, channel, bytes, eof) => {
        const result: KubernetesStreamInputReadback = {
          view: { ...h.view, version: 5 },
          submission: {
            ref: "kif_synthetic",
            invocationRef: h.view.invocationRef,
            streamRef: h.view.streamRef,
            generation: 7,
            sequence: 1,
            channel,
            payloadDigest: await kubernetesInputDigest(bytes),
            byteCount: bytes.length,
            endOfInput: eof,
            state: "ACKNOWLEDGED",
          },
        };
        if (channel === "TERMINAL_RESIZE")
          expect(new TextDecoder().decode(bytes)).toBe(
            '{"columns":80,"rows":24}',
          );
        else expect(bytes.length).toBe(0);
        return result;
      },
    );
    await h.model.eof();
    await h.model.resize();
    expect(
      h.port.submitKubernetesStreamInput.mock.calls.map((c) => [c[1], c[3]]),
    ).toEqual([
      ["STDIN", true],
      ["TERMINAL_RESIZE", false],
    ]);
    expect(h.port.stopKubernetesStream).not.toHaveBeenCalled();
  });
  it("owner invalidation aborts delivery and clears draft without accepting stale responses", async () => {
    const h = harness();
    await ready(h);
    let resolve: ((value: KubernetesStreamInputReadback) => void) | undefined;
    let signal: AbortSignal | undefined;
    let bytes: Uint8Array | undefined;
    h.port.submitKubernetesStreamInput.mockImplementation(
      (_view, _channel, payload, _eof, ownerSignal) => {
        signal = ownerSignal;
        bytes = payload;
        return new Promise((accept) => {
          resolve = accept;
        });
      },
    );
    h.model.draft.value = "private";
    const sending = h.model.send();
    await vi.waitFor(() => expect(resolve).toBeDefined());
    resetOwnerRequests();
    expect(signal?.aborted).toBe(true);
    expect(h.model.draft.value).toBe("");
    if (!resolve) throw new Error("Fixture delivery has not started");
    resolve({ view: { ...h.view, version: 99 } });
    await sending;
    expect(h.model.selected.value).toBeUndefined();
    expect(bytes?.every((b) => b === 0)).toBe(true);
    expect(h.port.readKubernetesStreamInput).toHaveBeenCalledOnce();
  });
  it("input-free watch still allows owner Stop but never sends user data", async () => {
    const view = fixture();
    view.operation = "kubernetes.watch";
    view.inputChannels = [];
    view.podName = "";
    view.container = "";
    view.resource.name = "";
    view.resource.uid = "";
    view.resource.resourceVersion = "";
    const h = harness(view);
    await ready(h);
    expect(h.model.canSend.value).toBe(false);
    expect(h.model.canResize.value).toBe(false);
    expect(h.model.canStop.value).toBe(true);
    h.model.draft.value = "private";
    await h.model.send();
    expect(h.port.submitKubernetesStreamInput).not.toHaveBeenCalled();
  });
  it("fresh root visibility denial clears cached metadata and disables new actions", async () => {
    const h = harness();
    await ready(h);
    h.model.draft.value = "private";
    h.port.readKubernetesStreamInput.mockRejectedValue(
      new AppProblem({
        status: 403,
        code: "FORBIDDEN",
        retryable: false,
        kind: "forbidden",
      }),
    );
    await h.model.refresh();
    expect(h.model.selected.value).toBeUndefined();
    expect(h.model.items.value).toEqual([]);
    expect(h.model.draft.value).toBe("");
    expect(h.model.canSend.value).toBe(false);
    expect(h.model.canStop.value).toBe(false);
  });
  it.each([401, 403, 404])(
    "list denial %i removes cached streams and their page cursor",
    async (status) => {
      const h = harness();
      await vi.waitFor(() => expect(h.model.items.value).toHaveLength(1));
      h.model.nextPageToken.value = "previous-page";
      h.model.draft.value = "private";
      h.port.listKubernetesStreamInputs.mockRejectedValue(
        new AppProblem({
          status,
          code: "FORBIDDEN",
          retryable: false,
          kind: "forbidden",
        }),
      );
      await h.model.discover("previous-page");
      expect(h.model.items.value).toEqual([]);
      expect(h.model.nextPageToken.value).toBe("");
      expect(h.model.draft.value).toBe("");
      expect(h.model.selected.value).toBeUndefined();
      expect(h.model.canSend.value).toBe(false);
      expect(h.model.canStop.value).toBe(false);
    },
  );
  it("input execution denial closes further writes while preserving owner Stop", async () => {
    const h = harness();
    await ready(h);
    h.port.submitKubernetesStreamInput.mockRejectedValue(
      new AppProblem({
        status: 403,
        code: "FORBIDDEN",
        retryable: false,
        kind: "forbidden",
      }),
    );
    h.model.draft.value = "private";
    await h.model.send();
    expect(h.model.canSend.value).toBe(false);
    expect(h.model.canStop.value).toBe(true);
    expect(h.port.readKubernetesStreamInput).toHaveBeenCalledOnce();
  });
});
