import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "./owner-lifetime";
interface Wire {
  path: Record<string, string>;
  body?: Record<string, unknown>;
  query?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
}
const sdk = vi.hoisted(() => {
  const operation = () =>
    vi.fn<(request: Wire) => Promise<{ data: unknown; response: Response }>>();
  return {
    listManagedKubernetesPortForwardConnections: operation(),
    getManagedKubernetesPortForwardConnection: operation(),
    openManagedKubernetesPortForwardConnection: operation(),
    closeManagedKubernetesPortForwardConnection: operation(),
    listManagedKubernetesStreamInputs: operation(),
    getManagedKubernetesStreamInput: operation(),
    submitManagedKubernetesStreamInput: operation(),
    stopManagedKubernetesStream: operation(),
  };
});
vi.mock("./generated/openapi/sdk.gen", () => sdk);
vi.mock("./client", () => ({ requestSignal: (signal: AbortSignal) => signal }));
import {
  parsePortForwardConnection,
  parsePortForwardPage,
  listPortForwardConnections,
  openPortForwardConnection,
  getPortForwardConnection,
  closePortForwardConnection,
  sendPortForwardInput,
} from "./kubernetes-port-forward";
import { submitKubernetesStreamInput } from "./kubernetes-stream-input";
import {
  encodePortForwardInput,
  validPortForwardInput,
} from "@/shared/lib/kubernetes-port-forward-frame";
import {
  input,
  connection,
  submission,
  page,
} from "./kubernetes-port-forward-fixtures.test-support";
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
describe("actual typed PF public consumer", () => {
  it("lists exact owner-approved mappings and sends only explicit slot/OCC to Open", async () => {
    sdk.listManagedKubernetesPortForwardConnections.mockResolvedValue(
      reply(page()),
    );
    const signal = new AbortController().signal;
    expect(
      (await listPortForwardConnections(input(), signal)).profile.ports,
    ).toEqual([{ port: 18080, targetPort: 8080 }]);
    sdk.openManagedKubernetesPortForwardConnection.mockResolvedValue(
      reply({
        connection: { ...connection(), state: "PREPARED", inputVersion: 5 },
        input: { ...input(), version: 5 },
        submission: submission(),
      }),
    );
    await openPortForwardConnection(input(), 0, signal, "pf-open-mutation");
    expect(
      sdk.openManagedKubernetesPortForwardConnection.mock.calls[0]?.[0],
    ).toMatchObject({
      path: { invocationRef: "inv_synthetic", streamRef: "kst_synthetic" },
      body: { slot: 0 },
      headers: { "If-Match": '"4"', "Idempotency-Key": "pf-open-mutation" },
    });
    expect(
      Object.keys(
        sdk.openManagedKubernetesPortForwardConnection.mock.calls[0]?.[0]
          .body ?? {},
      ),
    ).toEqual(["slot"]);
  });
  it("recovers Open metadata by original key without another write and keeps CLOSED separate from joined", async () => {
    sdk.getManagedKubernetesPortForwardConnection.mockResolvedValue(
      reply({
        connection: { ...connection(), state: "PREPARED" },
        submission: submission(),
      }),
    );
    await getPortForwardConnection(input(), new AbortController().signal, {
      openIdempotencyKey: "pf-open-mutation",
    });
    expect(
      sdk.getManagedKubernetesPortForwardConnection.mock.calls[0]?.[0].query,
    ).toEqual({ openIdempotencyKey: "pf-open-mutation" });
    sdk.closeManagedKubernetesPortForwardConnection.mockResolvedValue(
      reply({
        connection: { ...connection(), inputVersion: 5, state: "CLOSED" },
        input: { ...input(), version: 5 },
      }),
    );
    const closed = await closePortForwardConnection(
      input(),
      connection(),
      new AbortController().signal,
      "pf-close-mutation",
    );
    expect(closed.connection.nativeJoined).toBe(false);
    expect(closed.submission).toBeUndefined();
    expect(
      sdk.openManagedKubernetesPortForwardConnection,
    ).not.toHaveBeenCalled();
  });
  it.each([
    "foreign-parent",
    "unsafe-generation",
    "fake-join",
    "expired-version",
    "unknown-reason",
    "future-field",
  ])("rejects malformed upstream %s", (variant) => {
    const value: Record<string, unknown> = { ...connection() };
    if (variant === "foreign-parent") value.streamRef = "kst_foreignxx";
    if (variant === "unsafe-generation")
      value.generation = Number.MAX_SAFE_INTEGER + 1;
    if (variant === "fake-join") value.nativeJoined = true;
    if (variant === "expired-version") value.inputVersion = 3;
    if (variant === "unknown-reason")
      value.reasonCode = "UPSTREAM_PRIVATE_ERROR";
    if (variant === "future-field") value.targetURL = "https://foreign.invalid";
    expect(() => parsePortForwardConnection(value, input())).toThrow();
  });
  it("retains revoked metadata without granting new input", () => {
    expect(
      parsePortForwardConnection(
        {
          ...connection(),
          state: "CANCELLED",
          reasonCode: "KUBERNETES_GRANT_REVOKED",
        },
        input(),
      ).state,
    ).toBe("CANCELLED");
  });
  it.each([
    "target-override",
    "ports-empty",
    "ports-duplicate",
    "slot-outside-profile",
    "unsupported-without-reason",
    "hidden-capacity-increase",
  ])("rejects hostile profile %s", (variant) => {
    const value = page();
    if (variant === "target-override")
      Object.assign(value.profile, { targetURL: "https://foreign.invalid" });
    if (variant === "ports-empty") value.profile.ports = [];
    if (variant === "ports-duplicate")
      value.profile.ports.push({ port: 18080, targetPort: 8080 });
    if (variant === "slot-outside-profile")
      value.items = value.items.map((item) => ({ ...item, slot: 1 }));
    if (variant === "unsupported-without-reason")
      value.profile.protocol = "PORT_FORWARD_WEBSOCKET_V4";
    if (variant === "hidden-capacity-increase")
      value.profile.maximumActiveConnections = 17;
    expect(() => parsePortForwardPage(value, input())).toThrow();
  });
  it("encodes canonical DATA/half-close and rejects legacy/OPEN/CLOSE/output-ordinal before HTTP", async () => {
    sdk.submitManagedKubernetesStreamInput.mockImplementation((request) => {
      if (!request.body) throw new Error("PF fixture request body is missing");
      return Promise.resolve(
        reply({
          view: { ...input(), version: 5 },
          submission: {
            ...submission(),
            payloadDigest: request.body.payloadDigest,
            byteCount: atob(request.body.payloadBase64 as string).length,
          },
        }),
      );
    });
    const body = new TextEncoder().encode("hello");
    await sendPortForwardInput(
      input(),
      connection(),
      body,
      false,
      new AbortController().signal,
      "pf-data-mutation",
    );
    expect([...body]).toEqual([0, 0, 0, 0, 0]);
    const wire = sdk.submitManagedKubernetesStreamInput.mock.calls[0]?.[0].body;
    if (!wire) throw new Error("PF fixture wire request is missing");
    const raw = Uint8Array.from(atob(wire.payloadBase64 as string), (char) =>
      char.charCodeAt(0),
    );
    expect(validPortForwardInput(raw, false)).toBe(true);
    expect(raw[4]).toBe(2);
    expect(new DataView(raw.buffer).getBigUint64(6)).toBe(3n);
    expect(wire.endOfInput).toBe(false);
    const half = encodePortForwardInput(connection(), new Uint8Array(), true);
    expect(half.length).toBe(51);
    expect(validPortForwardInput(half, false)).toBe(true);
    for (const forbidden of [
      new Uint8Array([0, 1]),
      new Uint8Array(),
      Uint8Array.from(raw, (value, index) => (index === 4 ? 1 : value)),
      Uint8Array.from(raw, (value, index) => (index === 50 ? 1 : value)),
    ]) {
      await expect(
        submitKubernetesStreamInput(
          input(),
          "PORT_FORWARD",
          forbidden,
          false,
          new AbortController().signal,
          "pf-forged-mutation",
        ),
      ).rejects.toThrow();
    }
    expect(sdk.submitManagedKubernetesStreamInput).toHaveBeenCalledOnce();
  });
});
