import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "./owner-lifetime";
import type { KubernetesStreamInputView } from "./generated/openapi/types.gen";
interface WireRequest {
  path: Record<string, string>;
  body?: Record<string, unknown>;
  query?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
}
const sdk = vi.hoisted(() => {
  const operation = () =>
    vi.fn<
      (request: WireRequest) => Promise<{ data: unknown; response: Response }>
    >();
  return {
    listManagedKubernetesStreamInputs: operation(),
    getManagedKubernetesStreamInput: operation(),
    submitManagedKubernetesStreamInput: operation(),
    stopManagedKubernetesStream: operation(),
  };
});
vi.mock("./generated/openapi/sdk.gen", () => sdk);
vi.mock("./client", () => ({ requestSignal: (signal: AbortSignal) => signal }));
import {
  parseKubernetesInputView,
  listKubernetesStreamInputs,
  readKubernetesStreamInput,
  submitKubernetesStreamInput,
  stopKubernetesStream,
  kubernetesInputDigest,
} from "./kubernetes-stream-input";
const context = { runRef: "run_synthetic", nodeRef: "nod_synthetic" };
function fixture(): KubernetesStreamInputView {
  return {
    invocationRef: "inv_synthetic",
    streamRef: "kst_synthetic",
    ...context,
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
function reply(data: unknown) {
  return { data, response: new Response(null, { status: 200 }) };
}
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"c".repeat(43)}` });
});
afterEach(() => {
  resetOwnerRequests();
  vi.unstubAllGlobals();
});
describe("protected Kubernetes input HTTP consumer", () => {
  it("requires explicit physical storage limits and canonical quota reasons", () => {
    for (const invalid of [
      { ...fixture(), maximumStagedBytes: 0 },
      { ...fixture(), maximumStagedBytes: 1073741825 },
      { ...fixture(), maximumStagedObjects: 0 },
      { ...fixture(), maximumStagedObjects: 32769 },
      { ...fixture(), reasonCode: "KUBERNETES_PORT_FORWARD_CAPACITY" },
      Object.fromEntries(
        Object.entries(fixture()).filter(
          ([key]) => key !== "maximumStagedObjects",
        ),
      ),
    ]) {
      expect(() => parseKubernetesInputView(invalid, context)).toThrow();
    }
    for (const reasonCode of [
      "KUBERNETES_STAGING_QUOTA_EXCEEDED",
      "KUBERNETES_PORT_FORWARD_CAPACITY_EXHAUSTED",
    ]) {
      expect(
        parseKubernetesInputView({ ...fixture(), reasonCode }, context)
          .reasonCode,
      ).toBe(reasonCode);
    }
  });
  it("lists exact run/node and retains explicit stream selection and owner target", async () => {
    const view = fixture();
    sdk.listManagedKubernetesStreamInputs.mockResolvedValue(
      reply({ items: [view], nextPageToken: "" }),
    );
    const result = await listKubernetesStreamInputs(
      context,
      new AbortController().signal,
    );
    expect(result.items[0]).toEqual(view);
    expect(sdk.listManagedKubernetesStreamInputs).toHaveBeenCalledOnce();
    expect(
      sdk.listManagedKubernetesStreamInputs.mock.calls[0]?.[0],
    ).toMatchObject({ path: context, query: { pageSize: 20 } });
  });
  it.each([4097, 2 ** 20, Number.MAX_SAFE_INTEGER])(
    "retains representable stream sequence %d",
    (sequence) => {
      const view = fixture();
      view.acknowledgedSequence = sequence - 1;
      view.pending = {
        ref: "kif_synthetic",
        invocationRef: view.invocationRef,
        streamRef: view.streamRef,
        generation: view.generation,
        sequence,
        channel: "STDIN",
        payloadDigest: "a".repeat(64),
        byteCount: 1,
        endOfInput: false,
        state: "CLAIMED",
      };
      expect(parseKubernetesInputView(view, context)).toEqual(view);
      expect(() =>
        parseKubernetesInputView(
          { ...view, acknowledgedSequence: Number.MAX_SAFE_INTEGER + 1 },
          context,
        ),
      ).toThrow();
    },
  );
  it("retains unknown logs selection without inventing a privileged container", () => {
    const view = fixture();
    view.operation = "kubernetes.logs";
    view.container = "";
    view.inputChannels = [];
    expect(parseKubernetesInputView(view, context)).toEqual(view);
    expect(() =>
      parseKubernetesInputView(
        { ...view, operation: "kubernetes.exec" },
        context,
      ),
    ).toThrow();
  });
  it.each(["namespace", "cluster", "all"])(
    "accepts input-free watch %s without fake Pod or incarnation",
    (scope) => {
      const view = fixture();
      view.operation = "kubernetes.watch";
      view.podName = "";
      view.container = "";
      view.inputChannels = [];
      view.resource.name = "";
      view.resource.uid = "";
      view.resource.resourceVersion = "";
      if (scope === "cluster") {
        view.namespace = "@cluster";
        view.resource.namespace = "";
        view.resource.namespaced = false;
      }
      if (scope === "all") {
        view.operation = "kubernetes.watch.all-namespaces";
        view.namespace = "@all-namespaces";
        view.resource.namespace = "";
        view.resource.allNamespaces = true;
      }
      expect(parseKubernetesInputView(view, context)).toEqual(view);
      expect(() =>
        parseKubernetesInputView(
          { ...view, inputChannels: ["STDIN"] },
          context,
        ),
      ).toThrow();
    },
  );
  it.each([
    "actor",
    "foreign node",
    "unknown channel",
    "future state",
    "resource",
    "target",
    "duplicate",
    "null pending",
  ])("rejects %s before consumption", (kind) => {
    const view: Record<string, unknown> = { ...fixture() };
    if (kind === "actor") view.actorRef = "user_other";
    if (kind === "foreign node") view.nodeRef = "nod_foreign01";
    if (kind === "unknown channel") view.inputChannels = ["RAW"];
    if (kind === "future state") view.state = "READY";
    if (kind === "resource") view.resource = { ...fixture().resource, uid: "" };
    if (kind === "target") view.targetRef = "bad target";
    if (kind === "duplicate") view.inputChannels = ["STDIN", "STDIN"];
    if (kind === "null pending") view.pending = null;
    expect(() => parseKubernetesInputView(view, context)).toThrow();
  });
  it("submits one bounded owned binary frame with OCC/hash and clears bytes", async () => {
    const view = fixture();
    const payload = new TextEncoder().encode("typed-input");
    const hash = await kubernetesInputDigest(payload);
    sdk.submitManagedKubernetesStreamInput.mockImplementation((request) =>
      Promise.resolve(
        reply({
          view: { ...view, version: 5 },
          submission: {
            ref: "kif_synthetic",
            invocationRef: view.invocationRef,
            streamRef: view.streamRef,
            generation: 7,
            sequence: 1,
            channel: request.body?.channel,
            payloadDigest: request.body?.payloadDigest,
            byteCount: 11,
            endOfInput: false,
            state: "QUEUED",
          },
        }),
      ),
    );
    const result = await submitKubernetesStreamInput(
      view,
      "STDIN",
      payload,
      false,
      new AbortController().signal,
      "submit-fixture01",
    );
    expect(result.submission?.sequence).toBe(1);
    expect(payload.every((entry) => entry === 0)).toBe(true);
    const request = sdk.submitManagedKubernetesStreamInput.mock.calls[0]?.[0];
    if (!request) throw new Error("Fixture Submit request is missing");
    expect(request.body).toEqual({
      channel: "STDIN",
      payloadBase64: btoa("typed-input"),
      payloadDigest: hash,
      endOfInput: false,
    });
    expect(request.headers).toMatchObject({
      "If-Match": '"4"',
      "Idempotency-Key": "submit-fixture01",
    });
    expect(sdk.submitManagedKubernetesStreamInput).toHaveBeenCalledOnce();
  });
  it("never retransmits uncertain writes or silently turns absence into EOF", async () => {
    const payload = new TextEncoder().encode("private");
    sdk.submitManagedKubernetesStreamInput.mockRejectedValue(
      new Error("lost ACK"),
    );
    await expect(
      submitKubernetesStreamInput(
        fixture(),
        "STDIN",
        payload,
        false,
        new AbortController().signal,
        "submit-fixture01",
      ),
    ).rejects.toThrow();
    expect(sdk.submitManagedKubernetesStreamInput).toHaveBeenCalledOnce();
    expect(sdk.getManagedKubernetesStreamInput).not.toHaveBeenCalled();
    expect(payload.every((entry) => entry === 0)).toBe(true);
    const empty = new Uint8Array();
    await expect(
      submitKubernetesStreamInput(
        fixture(),
        "STDIN",
        empty,
        false,
        new AbortController().signal,
        "submit-fixture01",
      ),
    ).rejects.toThrow();
    expect(sdk.submitManagedKubernetesStreamInput).toHaveBeenCalledOnce();
  });
  it("reads only original lost-ACK key and rejects stale or mixed stream metadata", async () => {
    const view = fixture();
    sdk.getManagedKubernetesStreamInput.mockResolvedValue(
      reply({ view: { ...view, version: 3 } }),
    );
    await expect(
      readKubernetesStreamInput(
        view,
        new AbortController().signal,
        "submit-fixture01",
      ),
    ).rejects.toThrow();
    expect(
      sdk.getManagedKubernetesStreamInput.mock.calls[0]?.[0],
    ).toMatchObject({
      path: { invocationRef: view.invocationRef, streamRef: view.streamRef },
      query: { submissionIdempotencyKey: "submit-fixture01" },
    });
  });
  it("Stop is exact tool closure and rejects active owner acknowledgements", async () => {
    const view = fixture();
    sdk.stopManagedKubernetesStream.mockResolvedValue(
      reply({ ...view, state: "CANCELED", version: 5 }),
    );
    expect(
      (
        await stopKubernetesStream(
          view,
          new AbortController().signal,
          "stop-fixture01",
        )
      ).state,
    ).toBe("CANCELED");
    const request = sdk.stopManagedKubernetesStream.mock.calls[0]?.[0];
    if (!request) throw new Error("Fixture Stop request is missing");
    expect(request.body).toEqual({});
    expect(request.path).toEqual({
      invocationRef: view.invocationRef,
      streamRef: view.streamRef,
    });
    sdk.stopManagedKubernetesStream.mockResolvedValue(
      reply({ ...view, version: 5 }),
    );
    await expect(
      stopKubernetesStream(
        view,
        new AbortController().signal,
        "stop-fixture02",
      ),
    ).rejects.toThrow();
  });
});
