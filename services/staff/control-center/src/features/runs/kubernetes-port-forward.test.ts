import { afterEach, describe, expect, it, vi } from "vitest";
import { effectScope, ref, type EffectScope } from "vue";
import * as api from "@/shared/api/kubernetes-port-forward";
import {
  input,
  connection,
  submission,
  page,
} from "@/shared/api/kubernetes-port-forward-fixtures.test-support";
import { AppProblem } from "@/shared/api/problem";
import {
  KubernetesInputProtocolError,
  readKubernetesStreamInput,
} from "@/shared/api/kubernetes-stream-input";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import { useKubernetesPortForward } from "./kubernetes-port-forward";
let scope: EffectScope | undefined;
afterEach(() => {
  scope?.stop();
  scope = undefined;
  resetOwnerRequests();
  vi.useRealTimers();
});
async function settled() {
  for (let i = 0; i < 20; i++) await Promise.resolve();
}
function fixture() {
  vi.useFakeTimers();
  const port = {
    ...api,
    readInput: vi
      .fn<typeof readKubernetesStreamInput>()
      .mockResolvedValue({ view: input() }),
    listPortForwardConnections: vi
      .fn<typeof api.listPortForwardConnections>()
      .mockResolvedValue(page()),
    getPortForwardConnection: vi
      .fn<typeof api.getPortForwardConnection>()
      .mockResolvedValue({
        connection: connection(),
        submission: submission(),
      }),
    openPortForwardConnection: vi
      .fn<typeof api.openPortForwardConnection>()
      .mockResolvedValue({
        connection: {
          ...connection(),
          state: "PREPARED" as const,
          inputVersion: 5,
        },
        input: { ...input(), version: 5, pending: submission() },
        submission: submission(),
      }),
    closePortForwardConnection: vi
      .fn<typeof api.closePortForwardConnection>()
      .mockResolvedValue({
        connection: {
          ...connection(),
          state: "CLOSED" as const,
          inputVersion: 5,
        },
        input: { ...input(), version: 5 },
      }),
    sendPortForwardInput: vi
      .fn<typeof api.sendPortForwardInput>()
      .mockResolvedValue({
        view: { ...input(), version: 5 },
        submission: submission(),
      }),
  };
  const context = ref(input());
  scope = effectScope();
  const model = scope.run(() => useKubernetesPortForward(context, port));
  if (!model) throw new Error("PF fixture scope is inactive");
  return { port, context, model };
}
describe("PF connection control lifecycle", () => {
  it("requires explicit mapping and connection selection; OPEN is owner-assigned and not native success", async () => {
    const { model, port } = fixture();
    await settled();
    expect(model.selected.value).toBeUndefined();
    expect(model.canOpen.value).toBe(false);
    model.slot.value = 0;
    expect(model.canOpen.value).toBe(true);
    await model.open();
    expect(port.openPortForwardConnection).toHaveBeenCalledOnce();
    expect(port.openPortForwardConnection.mock.calls[0]?.[1]).toBe(0);
    expect(model.selected.value?.state).toBe("PREPARED");
    expect(model.canSend.value).toBe(false);
    expect(model.selected.value?.nativeJoined).toBe(false);
  });
  it("lost Open ACK performs one original-key Get, never another Open or native replay", async () => {
    const { model, port } = fixture();
    await settled();
    model.slot.value = 0;
    port.openPortForwardConnection.mockRejectedValueOnce(
      new AppProblem({
        status: 0,
        code: "NETWORK_ERROR",
        kind: "unavailable",
        retryable: false,
      }),
    );
    await model.open();
    expect(port.openPortForwardConnection).toHaveBeenCalledOnce();
    expect(port.getPortForwardConnection).toHaveBeenCalledOnce();
    const mutation = port.openPortForwardConnection.mock.calls[0]?.[3];
    expect(port.getPortForwardConnection.mock.calls[0]?.[2]).toEqual({
      openIdempotencyKey: mutation,
    });
    expect(model.selected.value?.connectionRef).toBe(
      connection().connectionRef,
    );
  });
  it("malformed write success blocks new Open and keeps exact metadata recovery without automatic Get", async () => {
    const { model, port } = fixture();
    await settled();
    model.slot.value = 0;
    port.openPortForwardConnection.mockRejectedValueOnce(
      new KubernetesInputProtocolError(),
    );
    await model.open();
    expect(model.uncertain.value).toBe(true);
    expect(model.canOpen.value).toBe(false);
    expect(port.getPortForwardConnection).not.toHaveBeenCalled();
    await model.refresh();
    expect(port.openPortForwardConnection).toHaveBeenCalledOnce();
    expect(port.getPortForwardConnection.mock.calls[0]?.[2]).toEqual({
      openIdempotencyKey: port.openPortForwardConnection.mock.calls[0]?.[3],
    });
  });
  it("lost Close ACK reads exact connection and never claims native pair join", async () => {
    const { model, port } = fixture();
    await settled();
    const item = model.items.value[0];
    if (!item) throw new Error("PF fixture connection is missing");
    await model.choose(item);
    port.getPortForwardConnection.mockClear();
    port.closePortForwardConnection.mockRejectedValueOnce(
      new AppProblem({
        status: 0,
        code: "NETWORK_ERROR",
        kind: "unavailable",
        retryable: false,
      }),
    );
    port.getPortForwardConnection.mockResolvedValueOnce({
      connection: { ...connection(), state: "CLOSED" },
      submission: submission(),
    });
    await model.closeConnection();
    expect(port.closePortForwardConnection).toHaveBeenCalledOnce();
    expect(port.getPortForwardConnection).toHaveBeenCalledOnce();
    expect(port.getPortForwardConnection.mock.calls[0]?.[2]).toEqual({
      connectionRef: connection().connectionRef,
    });
    expect(model.selected.value?.nativeJoined).toBe(false);
    expect(model.canSend.value).toBe(false);
  });
  it("clears cached profile and connections on current visibility denial", async () => {
    const { model, port } = fixture();
    await settled();
    const item = model.items.value[0];
    if (!item) throw new Error("PF fixture connection is missing");
    await model.choose(item);
    model.draft.value = "private bytes";
    port.listPortForwardConnections.mockRejectedValueOnce(
      new AppProblem({
        status: 403,
        code: "FORBIDDEN",
        kind: "forbidden",
        retryable: false,
      }),
    );
    await model.refresh();
    expect(model.items.value).toEqual([]);
    expect(model.selected.value).toBeUndefined();
    expect(model.profile.value).toBeUndefined();
    expect(model.draft.value).toBe("");
    expect(model.canOpen.value).toBe(false);
  });
  it("does not apply late Open/readback after a different stream is selected", async () => {
    const { model, port, context } = fixture();
    await settled();
    model.slot.value = 0;
    let release!: (
      value: Awaited<ReturnType<typeof port.openPortForwardConnection>>,
    ) => void;
    port.openPortForwardConnection.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    const pending = model.open();
    context.value = { ...input(), streamRef: "kst_foreignxx" };
    await settled();
    release({
      connection: { ...connection(), state: "PREPARED", inputVersion: 5 },
      input: { ...input(), version: 5, pending: submission() },
      submission: submission(),
    });
    await pending;
    expect(model.selected.value).toBeUndefined();
    expect(model.draft.value).toBe("");
  });
  it("does not open a WS profile or exceed disclosed active capacity", async () => {
    const { model, port } = fixture();
    await settled();
    model.slot.value = 0;
    port.listPortForwardConnections.mockResolvedValueOnce({
      ...page(),
      profile: {
        ...page().profile,
        protocol: "PORT_FORWARD_WEBSOCKET_V4",
        admissionReasonCode: "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED",
      },
    });
    await model.refresh();
    await model.open();
    expect(port.openPortForwardConnection).not.toHaveBeenCalled();
    port.listPortForwardConnections.mockResolvedValueOnce({
      ...page(),
      profile: {
        ...page().profile,
        activeConnections: 16,
        admissionReasonCode: "KUBERNETES_PORT_FORWARD_CAPACITY_EXHAUSTED",
      },
    });
    await model.refresh();
    expect(model.canOpen.value).toBe(false);
  });
});
