import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import type { KubernetesResourceMaterialView } from "@/shared/api/generated/openapi/types.gen";
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
    beginManagedKubernetesResourceMaterial: operation(),
    getManagedKubernetesResourceMaterial: operation(),
    cancelManagedKubernetesResourceMaterial: operation(),
  };
});
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  beginKubernetesMaterial,
  getKubernetesMaterial,
  cancelKubernetesMaterial,
  type KubernetesMaterialContext,
} from "./kubernetes-resource-material-api";
const context: KubernetesMaterialContext = {
  connectionRef: "int_fixture01",
  connectionVersion: 7,
  expectedVersion: 0,
  policy: "INITIATOR_REQUIRED",
  available: true,
  enabled: true,
};
const view: KubernetesResourceMaterialView = {
  materialRef: "krs_fixture01",
  connectionRef: context.connectionRef,
  connectionVersion: 7,
  namespace: "team",
  name: "application",
  generateName: "",
  operation: "kubernetes.create",
  secretType: "Opaque",
  patchType: "",
  bindingVersion: 1,
  identityRef: "kid_fixture01",
  identityRevision: 1,
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
    sdk.beginManagedKubernetesResourceMaterial.mockResolvedValue({
      data: { material: view, reconcileOnly: true },
      response: new Response(null, { status: 201 }),
    });
    await beginKubernetesMaterial(
      context,
      {
        operation: "kubernetes.create",
        namespace: "team",
        name: "application",
        generateName: "",
        secretType: "Opaque",
        patchType: "",
        contentSHA256: "a".repeat(64),
        contentBytes: 128,
      },
      "begin-fixture01",
      new AbortController().signal,
    );
    expect(sdk.beginManagedKubernetesResourceMaterial).toHaveBeenCalledOnce();
    const request =
      sdk.beginManagedKubernetesResourceMaterial.mock.calls[0]?.[0];
    expect(request?.body).toEqual({
      expectedVersion: 0,
      expectedConnectionVersion: 7,
      operation: "kubernetes.create",
      namespace: "team",
      name: "application",
      generateName: "",
      secretType: "Opaque",
      patchType: "",
      contentSHA256: "a".repeat(64),
      contentBytes: 128,
    });
    expect(request?.headers).toEqual({
      "Idempotency-Key": "begin-fixture01",
      "X-CSRF-Token": "c".repeat(43),
    });
  });
  it("Get uses one selector and requires ETag matching addressed setup", async () => {
    sdk.getManagedKubernetesResourceMaterial.mockResolvedValue({
      data: view,
      response: new Response(null, { headers: { ETag: '"3"' } }),
    });
    await expect(
      getKubernetesMaterial(
        context,
        { materialRef: view.materialRef },
        new AbortController().signal,
      ),
    ).resolves.toEqual(view);
    await getKubernetesMaterial(
      context,
      { beginIdempotencyKey: "begin-fixture01" },
      new AbortController().signal,
    );
    expect(
      sdk.getManagedKubernetesResourceMaterial.mock.calls.map(
        (call) => call[0].query,
      ),
    ).toEqual([
      { materialRef: view.materialRef },
      { beginIdempotencyKey: "begin-fixture01" },
    ]);
    sdk.getManagedKubernetesResourceMaterial.mockResolvedValue({
      data: view,
      response: new Response(null, { headers: { ETag: '"2"' } }),
    });
    await expect(
      getKubernetesMaterial(
        context,
        { materialRef: view.materialRef },
        new AbortController().signal,
      ),
    ).rejects.toThrow();
  });
  it("Cancel uses exact setup ETag, immutable identity and one mutation only", async () => {
    const result = { ...view, state: "CANCELLED", version: 4 };
    sdk.cancelManagedKubernetesResourceMaterial.mockResolvedValue({
      data: result,
      response: new Response(null, { headers: { ETag: '"4"' } }),
    });
    await cancelKubernetesMaterial(
      context,
      view,
      "cancel-fixture01",
      new AbortController().signal,
    );
    const request =
      sdk.cancelManagedKubernetesResourceMaterial.mock.calls[0]?.[0];
    expect(request?.path).toEqual({
      connectionRef: context.connectionRef,
      materialRef: view.materialRef,
    });
    expect(request?.body).toEqual({});
    expect(request?.headers?.["If-Match"]).toBe('"3"');
    sdk.cancelManagedKubernetesResourceMaterial.mockResolvedValue({
      data: { ...result, identityRef: "kid_foreign01" },
      response: new Response(null, { headers: { ETag: '"4"' } }),
    });
    await expect(
      cancelKubernetesMaterial(
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
        beginKubernetesMaterial(
          context,
          {
            operation: "kubernetes.create",
            namespace: "team",
            name: "application",
            generateName: "",
            secretType: "Opaque",
            patchType: "",
            contentSHA256: digest,
            contentBytes: bytes,
          },
          "begin-fixture01",
          new AbortController().signal,
        ),
      ).rejects.toThrow();
    await expect(
      beginKubernetesMaterial(
        { ...context, expectedVersion: NaN },
        {
          operation: "kubernetes.create",
          namespace: "team",
          name: "application",
          generateName: "",
          secretType: "Opaque",
          patchType: "",
          contentSHA256: "a".repeat(64),
          contentBytes: 128,
        },
        "begin-fixture01",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    await expect(
      getKubernetesMaterial(
        context,
        {
          materialRef: "krs_fixture01",
          beginIdempotencyKey: "begin-fixture01",
        } as unknown as { materialRef: string },
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    expect(sdk.beginManagedKubernetesResourceMaterial).not.toHaveBeenCalled();
    expect(sdk.getManagedKubernetesResourceMaterial).not.toHaveBeenCalled();
  });
});
