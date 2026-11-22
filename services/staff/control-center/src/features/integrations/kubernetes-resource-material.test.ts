import { effectScope, ref, type EffectScope } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import type {
  KubernetesResourceMaterialAdmission,
  KubernetesResourceMaterialView,
} from "@/shared/api/generated/openapi/types.gen";
import {
  useKubernetesResourceMaterial,
  type KubernetesMaterialAPI,
} from "./kubernetes-resource-material";
import { type KubernetesMaterialContext } from "./kubernetes-resource-material-api";

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
  version: 1,
  credentialPolicy: "INITIATOR_REQUIRED",
};
const admission: KubernetesResourceMaterialAdmission = {
  material: view,
  reconcileOnly: false,
  uploadPermit: btoa("p".repeat(32)),
  endpoint: {
    serverOrigin: "https://broker.example.invalid:9443",
    tlsServerName: "broker.example.invalid",
    certificateAuthorityPem: btoa("synthetic-ca"),
    certificateAuthoritySHA256: "c".repeat(64),
    addressIPs: ["192.0.2.10"],
  },
};
const scopes: EffectScope[] = [];
function fixture() {
  const port = {
    beginKubernetesMaterial: vi
      .fn<KubernetesMaterialAPI["beginKubernetesMaterial"]>()
      .mockResolvedValue(structuredClone(admission)),
    getKubernetesMaterial: vi
      .fn<KubernetesMaterialAPI["getKubernetesMaterial"]>()
      .mockResolvedValue(structuredClone(view)),
    cancelKubernetesMaterial: vi
      .fn<KubernetesMaterialAPI["cancelKubernetesMaterial"]>()
      .mockResolvedValue({ ...view, state: "CANCELLED", version: 2 }),
  };
  const scope = effectScope();
  scopes.push(scope);
  const current = ref({ ...context }),
    disabled = ref(false);
  const model = scope.run(() =>
    useKubernetesResourceMaterial(current, disabled, port),
  );
  if (!model) throw new Error("Fixture scope is absent");
  Object.assign(model.destination, { namespace: "team", name: "application" });
  model.contentSHA256.value = "a".repeat(64);
  model.contentBytes.value = "128";
  return { model, port, current, scope, disabled };
}
afterEach(() => {
  for (const scope of scopes.splice(0)) scope.stop();
  resetOwnerRequests();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Kubernetes setup UI: owner lifecycle", () => {
  it("Begin sends only hash/size and explicit absent-binding version0; no material field", async () => {
    const { model, port } = fixture();
    await model.begin();
    expect(port.beginKubernetesMaterial).toHaveBeenCalledWith(
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
      expect.any(String),
      expect.any(AbortSignal),
    );
    expect(model.material.value).toEqual(view);
    expect(model.hasAdmission.value).toBe(true);
    expect(model.canBegin.value).toBe(false);
    expect(JSON.stringify(model.material.value)).not.toContain(
      admission.uploadPermit,
    );
    expect(Object.keys(model)).not.toContain("admission");
  });
  it("lost Begin ACK requires Get by original key and never repeats mutation or recovers permit", async () => {
    const { model, port } = fixture();
    port.beginKubernetesMaterial.mockRejectedValue(
      new Error("Synthetic lost response"),
    );
    await model.begin();
    const key = model.beginKey.value;
    expect(model.uncertain.value).toBe(true);
    await model.begin();
    expect(port.beginKubernetesMaterial).toHaveBeenCalledOnce();
    model.selectorKind.value = "MATERIAL";
    model.selector.value = "krs_different01";
    await model.refresh();
    expect(port.getKubernetesMaterial).not.toHaveBeenCalled();
    model.selectorKind.value = "BEGIN_KEY";
    model.selector.value = key;
    await model.refresh();
    expect(port.getKubernetesMaterial).toHaveBeenCalledWith(
      context,
      { beginIdempotencyKey: key },
      expect.any(AbortSignal),
    );
    expect(model.material.value).toEqual(view);
    expect(model.hasAdmission.value).toBe(false);
    expect(model.canBegin.value).toBe(false);
  });
  it("Cancel uses current setup, reconciles lost response before another write and preserves immutable identity", async () => {
    const { model, port } = fixture();
    await model.begin();
    port.cancelKubernetesMaterial.mockRejectedValueOnce(
      new Error("Synthetic unknown cancel"),
    );
    await model.cancel();
    await model.cancel();
    expect(port.cancelKubernetesMaterial).toHaveBeenCalledOnce();
    expect(model.canCancel.value).toBe(false);
    port.getKubernetesMaterial.mockResolvedValue({
      ...view,
      state: "CANCELLED",
      version: 2,
    });
    await model.refresh();
    expect(model.material.value?.state).toBe("CANCELLED");
    expect(model.canBegin.value).toBe(true);
  });
  it("AVAILABLE changes upload to use deadline without target READY or another upload permit", async () => {
    const { model, port } = fixture();
    await model.begin();
    port.getKubernetesMaterial.mockResolvedValue({
      ...view,
      state: "AVAILABLE",
      expiresAt: "2099-11-15T12:15:00Z",
      version: 2,
    });
    await model.refresh();
    expect(model.material.value?.state).toBe("AVAILABLE");
    expect(model.hasAdmission.value).toBe(false);
    expect(model.canBegin.value).toBe(false);
  });
  it("refresh rejects a different identity or reversed version under the same setup ref", async () => {
    for (const change of [
      { identityRef: "kid_foreign01" },
      { identityRevision: 2 },
      { targetRef: "ktg_foreign01" },
      { namespace: "foreign" },
    ]) {
      const { model, port } = fixture();
      await model.begin();
      port.getKubernetesMaterial.mockResolvedValue({ ...view, ...change });
      await model.refresh();
      expect(model.problem.value).toBeDefined();
      expect(model.material.value).toEqual(view);
    }
  });
  it("connection version change ignores a late Begin ACK and clears permit and recovery context", async () => {
    const { model, port, current } = fixture();
    let resolve!: (value: KubernetesResourceMaterialAdmission) => void;
    port.beginKubernetesMaterial.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const operation = model.begin();
    current.value = { ...context, connectionVersion: 8 };
    resolve(admission);
    await operation;
    expect(model.material.value).toBeUndefined();
    expect(model.hasAdmission.value).toBe(false);
    expect(model.beginKey.value).toBe("");
  });
  it("logout fences all late callbacks; pagehide clears downloadable permit without claiming remote cancel", async () => {
    const { model, port } = fixture();
    await model.begin();
    model.suspend();
    expect(model.hasAdmission.value).toBe(false);
    expect(model.uncertain.value).toBe(true);
    expect(port.cancelKubernetesMaterial).not.toHaveBeenCalled();
    resetOwnerRequests();
    await model.refresh();
    expect(model.inactive.value).toBe(true);
    expect(model.material.value).toBeUndefined();
    expect(port.getKubernetesMaterial).not.toHaveBeenCalled();
  });
  it("disabled/unavailable metadata cannot issue Begin", async () => {
    for (const field of ["available", "enabled"] as const) {
      const { model, port, current } = fixture();
      current.value[field] = false;
      await model.begin();
      expect(port.beginKubernetesMaterial).not.toHaveBeenCalled();
    }
  });
});
