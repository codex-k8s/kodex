import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import type { KubernetesIdentitySetupView } from "@/shared/api/generated/openapi/types.gen";
type Request = {
  path: Record<string, string>;
  body?: unknown;
  query?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
};
const sdk = vi.hoisted(() => {
  const operation = () =>
    vi.fn<(input: Request) => Promise<{ data: unknown; response: Response }>>();
  return {
    beginManagedKubernetesIdentitySetup: operation(),
    getManagedKubernetesIdentitySetup: operation(),
    cancelManagedKubernetesIdentitySetup: operation(),
  };
});
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  beginKubernetesSetup,
  getKubernetesSetup,
  cancelKubernetesSetup,
  type KubernetesSetupContext,
} from "./kubernetes-identity-setup-api";
const context: KubernetesSetupContext = {
  connectionRef: "int_fixture01",
  connectionVersion: 7,
  expectedVersion: 0,
  policy: "INITIATOR_REQUIRED",
  available: true,
  enabled: true,
  requirements: {
    targetRef: "ktg_fixture01",
    targetRevision: 2,
    canonicalContext: "operator-context",
    connectionVersion: 7,
    credentialPolicy: "INITIATOR_REQUIRED",
  },
};
const view: KubernetesIdentitySetupView = {
  setupRef: "ksu_fixture01",
  connectionRef: context.connectionRef,
  connectionVersion: 7,
  bindingGeneration: 1,
  bindingVersion: 1,
  identityRef: "kid_fixture01",
  identityRevision: 1,
  canonicalContext: "operator-context",
  targetRef: "ktg_fixture01",
  state: "PREPARED",
  expiresAt: "2099-11-15T12:00:00Z",
  reasonCode: "",
  version: 3,
  credentialPolicy: "INITIATOR_REQUIRED",
};
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"c".repeat(43)}` });
});
afterEach(() => {
  resetOwnerRequests();
  vi.unstubAllGlobals();
});
describe("Kubernetes setup generated HTTP boundary", () => {
  it("Begin carries explicit0+Connection OCC, hash/size only, no caller identity or secret", async () => {
    sdk.beginManagedKubernetesIdentitySetup.mockResolvedValue({
      data: { setup: view, reconcileOnly: true },
      response: new Response(null, { status: 201 }),
    });
    await beginKubernetesSetup(
      context,
      "a".repeat(64),
      128,
      "begin-fixture01",
      new AbortController().signal,
    );
    expect(sdk.beginManagedKubernetesIdentitySetup).toHaveBeenCalledOnce();
    const request = sdk.beginManagedKubernetesIdentitySetup.mock.calls[0]?.[0];
    expect(request?.body).toEqual({
      expectedVersion: 0,
      expectedConnectionVersion: 7,
      contentSHA256: "a".repeat(64),
      contentBytes: 128,
    });
    expect(request?.headers).toEqual({
      "Idempotency-Key": "begin-fixture01",
      "X-CSRF-Token": "c".repeat(43),
    });
  });
  it("Get uses one selector and requires ETag matching addressed setup", async () => {
    sdk.getManagedKubernetesIdentitySetup.mockResolvedValue({
      data: view,
      response: new Response(null, { headers: { ETag: '"3"' } }),
    });
    await expect(
      getKubernetesSetup(
        context,
        { setupRef: view.setupRef },
        new AbortController().signal,
      ),
    ).resolves.toEqual(view);
    await getKubernetesSetup(
      context,
      { beginIdempotencyKey: "begin-fixture01" },
      new AbortController().signal,
    );
    expect(
      sdk.getManagedKubernetesIdentitySetup.mock.calls.map(
        (call) => call[0].query,
      ),
    ).toEqual([
      { setupRef: view.setupRef },
      { beginIdempotencyKey: "begin-fixture01" },
    ]);
    sdk.getManagedKubernetesIdentitySetup.mockResolvedValue({
      data: view,
      response: new Response(null, { headers: { ETag: '"2"' } }),
    });
    await expect(
      getKubernetesSetup(
        context,
        { setupRef: view.setupRef },
        new AbortController().signal,
      ),
    ).rejects.toThrow();
  });
  it("Cancel uses exact setup ETag, immutable identity and one mutation only", async () => {
    const result = { ...view, state: "CANCELLED", version: 4 };
    sdk.cancelManagedKubernetesIdentitySetup.mockResolvedValue({
      data: result,
      response: new Response(null, { headers: { ETag: '"4"' } }),
    });
    await cancelKubernetesSetup(
      context,
      view,
      "cancel-fixture01",
      new AbortController().signal,
    );
    const request = sdk.cancelManagedKubernetesIdentitySetup.mock.calls[0]?.[0];
    expect(request?.path).toEqual({
      connectionRef: context.connectionRef,
      setupRef: view.setupRef,
    });
    expect(request?.body).toEqual({});
    expect(request?.headers?.["If-Match"]).toBe('"3"');
    sdk.cancelManagedKubernetesIdentitySetup.mockResolvedValue({
      data: { ...result, identityRef: "kid_foreign01" },
      response: new Response(null, { headers: { ETag: '"4"' } }),
    });
    await expect(
      cancelKubernetesSetup(
        context,
        view,
        "cancel-fixture02",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
  });
  it("bad hash/size/selector fails before network; missingversion cannot mean absent binding", async () => {
    for (const [digest, bytes] of [
      ["bad", 128],
      ["a".repeat(64), 0],
      ["a".repeat(64), 1048577],
    ] as const)
      await expect(
        beginKubernetesSetup(
          context,
          digest,
          bytes,
          "begin-fixture01",
          new AbortController().signal,
        ),
      ).rejects.toThrow();
    await expect(
      beginKubernetesSetup(
        { ...context, expectedVersion: NaN },
        "a".repeat(64),
        128,
        "begin-fixture01",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    await expect(
      getKubernetesSetup(
        context,
        {
          setupRef: "ksu_fixture01",
          beginIdempotencyKey: "begin-fixture01",
        } as unknown as { setupRef: string },
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    expect(sdk.beginManagedKubernetesIdentitySetup).not.toHaveBeenCalled();
    expect(sdk.getManagedKubernetesIdentitySetup).not.toHaveBeenCalled();
  });
});
