import { effectScope, ref, type EffectScope } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import type {
  KubernetesIdentitySetupAdmission,
  KubernetesIdentitySetupView,
} from "@/shared/api/generated/openapi/types.gen";
import {
  useKubernetesIdentitySetup,
  type KubernetesSetupAPI,
} from "./kubernetes-identity-setup";
import {
  isPersonalKubernetesSetup,
  kubernetesSetupAdmission,
  kubernetesSetupView,
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
  version: 1,
  credentialPolicy: "INITIATOR_REQUIRED",
};
const admission: KubernetesIdentitySetupAdmission = {
  setup: view,
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
    beginKubernetesSetup: vi
      .fn<KubernetesSetupAPI["beginKubernetesSetup"]>()
      .mockResolvedValue(structuredClone(admission)),
    getKubernetesSetup: vi
      .fn<KubernetesSetupAPI["getKubernetesSetup"]>()
      .mockResolvedValue(structuredClone(view)),
    cancelKubernetesSetup: vi
      .fn<KubernetesSetupAPI["cancelKubernetesSetup"]>()
      .mockResolvedValue({ ...view, state: "CANCELLED", version: 2 }),
  };
  const scope = effectScope();
  scopes.push(scope);
  const current = ref({ ...context }),
    disabled = ref(false);
  const model = scope.run(() =>
    useKubernetesIdentitySetup(current, disabled, port),
  );
  if (!model) throw new Error("Fixture scope is absent");
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
    expect(port.beginKubernetesSetup).toHaveBeenCalledWith(
      context,
      "a".repeat(64),
      128,
      expect.any(String),
      expect.any(AbortSignal),
    );
    expect(model.setup.value).toEqual(view);
    expect(model.hasAdmission.value).toBe(true);
    expect(model.canBegin.value).toBe(false);
    expect(JSON.stringify(model.setup.value)).not.toContain(
      admission.uploadPermit,
    );
    expect(Object.keys(model)).not.toContain("admission");
  });
  it("lost Begin ACK requires Get by original key and never repeats mutation or recovers permit", async () => {
    const { model, port } = fixture();
    port.beginKubernetesSetup.mockRejectedValue(
      new Error("Synthetic lost response"),
    );
    await model.begin();
    const key = model.beginKey.value;
    expect(model.uncertain.value).toBe(true);
    await model.begin();
    expect(port.beginKubernetesSetup).toHaveBeenCalledOnce();
    model.selectorKind.value = "SETUP";
    model.selector.value = "ksu_different01";
    await model.refresh();
    expect(port.getKubernetesSetup).not.toHaveBeenCalled();
    model.selectorKind.value = "BEGIN_KEY";
    model.selector.value = key;
    await model.refresh();
    expect(port.getKubernetesSetup).toHaveBeenCalledWith(
      context,
      { beginIdempotencyKey: key },
      expect.any(AbortSignal),
    );
    expect(model.setup.value).toEqual(view);
    expect(model.hasAdmission.value).toBe(false);
    expect(model.canBegin.value).toBe(false);
  });
  it("Cancel uses current setup, reconciles lost response before another write and preserves immutable identity", async () => {
    const { model, port } = fixture();
    await model.begin();
    port.cancelKubernetesSetup.mockRejectedValueOnce(
      new Error("Synthetic unknown cancel"),
    );
    await model.cancel();
    await model.cancel();
    expect(port.cancelKubernetesSetup).toHaveBeenCalledOnce();
    expect(model.canCancel.value).toBe(false);
    port.getKubernetesSetup.mockResolvedValue({
      ...view,
      state: "CANCELLED",
      version: 2,
    });
    await model.refresh();
    expect(model.setup.value?.state).toBe("CANCELLED");
    expect(model.requiresConnectionRefresh.value).toBe(true);
    expect(model.canBegin.value).toBe(false);
  });
  it("COMPLETED means TEST_REQUIRED and requires current connection metadata, never READY", async () => {
    const { model, port } = fixture();
    await model.begin();
    port.getKubernetesSetup.mockResolvedValue({
      ...view,
      state: "COMPLETED",
      version: 2,
    });
    await model.refresh();
    expect(model.setup.value?.state).toBe("COMPLETED");
    expect(model.requiresConnectionRefresh.value).toBe(true);
    expect(model.hasAdmission.value).toBe(false);
    expect(model.canBegin.value).toBe(false);
  });
  it("refresh rejects a different identity or reversed version under the same setup ref", async () => {
    for (const change of [
      { identityRef: "kid_foreign01" },
      { identityRevision: 2 },
      { targetRef: "ktg_foreign01" },
      { bindingGeneration: 2 },
    ]) {
      const { model, port } = fixture();
      await model.begin();
      port.getKubernetesSetup.mockResolvedValue({ ...view, ...change });
      await model.refresh();
      expect(model.problem.value).toBeDefined();
      expect(model.setup.value).toEqual(view);
    }
  });
  it("connection version change ignores a late Begin ACK and clears permit and recovery context", async () => {
    const { model, port, current } = fixture();
    let resolve!: (value: KubernetesIdentitySetupAdmission) => void;
    port.beginKubernetesSetup.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const operation = model.begin();
    current.value = { ...context, connectionVersion: 8 };
    resolve(admission);
    await operation;
    expect(model.setup.value).toBeUndefined();
    expect(model.hasAdmission.value).toBe(false);
    expect(model.beginKey.value).toBe("");
  });
  it("logout fences all late callbacks; pagehide clears downloadable permit without claiming remote cancel", async () => {
    const { model, port } = fixture();
    await model.begin();
    model.suspend();
    expect(model.hasAdmission.value).toBe(false);
    expect(model.uncertain.value).toBe(true);
    expect(port.cancelKubernetesSetup).not.toHaveBeenCalled();
    resetOwnerRequests();
    await model.refresh();
    expect(model.inactive.value).toBe(true);
    expect(model.setup.value).toBeUndefined();
    expect(port.getKubernetesSetup).not.toHaveBeenCalled();
  });
  it("disabled/unavailable metadata cannot issue Begin", async () => {
    for (const field of ["available", "enabled"] as const) {
      const { model, port, current } = fixture();
      current.value[field] = false;
      await model.begin();
      expect(port.beginKubernetesSetup).not.toHaveBeenCalled();
    }
  });
});

describe("Kubernetes public DTO and purpose boundary", () => {
  it("TOKEN/SSH/unknown kinds and shared policy never mount personal KUBE setup", () => {
    const connection = {
      connectionRef: context.connectionRef,
      connectionVersion: 7,
      name: "Kubernetes",
      definitionKey: "kubernetes",
      policy: "INITIATOR_REQUIRED" as const,
      available: true,
      enabled: true,
    };
    expect(
      isPersonalKubernetesSetup({
        ...connection,
        credentialDelivery: {
          kind: "BROKER_IDENTITY",
          brokerPurpose: "KUBERNETES_TARGET",
        },
      }),
    ).toBe(true);
    for (const delivery of [
      undefined,
      { kind: "TOKEN" as const },
      { kind: "NONE" as const },
      {
        kind: "BROKER_IDENTITY" as const,
        brokerPurpose: "SSH_TARGET" as const,
      },
    ])
      expect(
        isPersonalKubernetesSetup({
          ...connection,
          credentialDelivery: delivery,
        }),
      ).toBe(false);
    expect(
      isPersonalKubernetesSetup({
        ...connection,
        policy: "PROJECT_SHARED",
        credentialDelivery: {
          kind: "BROKER_IDENTITY",
          brokerPurpose: "KUBERNETES_TARGET",
        },
      }),
    ).toBe(false);
  });
  it("shared context is reusable only with exact Connection OCC and zero binding metadata", () => {
    const shared = {
      ...context,
      policy: "PROJECT_SHARED" as const,
      requirements: {
        targetRef: "ktg_fixture01",
        targetRevision: 2,
        canonicalContext: "operator-context",
        connectionVersion: 7,
        credentialPolicy: "PROJECT_SHARED" as const,
      },
      expectedVersion: 7,
    };
    const value = {
      ...view,
      credentialPolicy: "PROJECT_SHARED" as const,
      bindingGeneration: 0,
      bindingVersion: 0,
    };
    expect(kubernetesSetupView(value, shared)).toEqual(value);
    expect(() =>
      kubernetesSetupView(value, { ...shared, expectedVersion: 0 }),
    ).toThrow();
  });
  it("unknown/null/foreign/mixed payload is closed; Get cannot return a permit", () => {
    for (const change of [
      { state: "READY" },
      { reasonCode: "provider secret text" },
      { version: null },
      { connectionRef: "int_foreign01" },
      { uploadPermit: admission.uploadPermit },
    ])
      expect(() =>
        kubernetesSetupView({ ...view, ...change }, context),
      ).toThrow();
    for (const change of [
      { uploadPermit: "invalid" },
      { reconcileOnly: true },
      { endpoint: null },
      { secret: "forbidden" },
    ])
      expect(() =>
        kubernetesSetupAdmission({ ...admission, ...change }, context),
      ).toThrow();
    expect(
      kubernetesSetupAdmission({ setup: view, reconcileOnly: true }, context),
    ).toEqual({ setup: view, reconcileOnly: true });
  });
});

describe("Kubernetes explicit export requirements", () => {
  it("missing requirements block a new Begin but preserve addressed recovery and cancellation", async () => {
    const { model, port, current } = fixture();
    current.value = { ...context, requirements: undefined };
    model.contentSHA256.value = "a".repeat(64);
    model.contentBytes.value = "128";
    expect(model.exportCommand.value).toBe("");
    await model.begin();
    expect(port.beginKubernetesSetup).not.toHaveBeenCalled();
    model.selectorKind.value = "SETUP";
    model.selector.value = view.setupRef;
    await model.refresh();
    expect(model.setup.value).toEqual(view);
    await model.cancel();
    expect(port.cancelKubernetesSetup).toHaveBeenCalledOnce();
  });
  it("context changes reset hashes; opaque context is one shell-quoted literal", async () => {
    const { model, current, port } = fixture();
    current.value = {
      ...context,
      requirements: {
        targetRef: "ktg_fixture01",
        targetRevision: 3,
        canonicalContext: "team'$(synthetic)",
        connectionVersion: 7,
        credentialPolicy: "INITIATOR_REQUIRED",
      },
    };
    expect(model.contentSHA256.value).toBe("");
    expect(model.contentBytes.value).toBe("");
    expect(model.exportCommand.value).toContain(
      "--target-context 'team'\"'\"'$(synthetic)'",
    );
    expect(model.exportCommand.value).toContain(
      "--context <selected-local-context>",
    );
    await model.begin();
    expect(port.beginKubernetesSetup).not.toHaveBeenCalled();
  });
  it("invalid or stale requirements never admit a new setup", async () => {
    for (const patch of [
      { targetRevision: 0 },
      { connectionVersion: 6 },
      { credentialPolicy: "PROJECT_SHARED" },
      { canonicalContext: "" },
      { canonicalContext: "bad\ncontext" },
      { canonicalContext: "я".repeat(129) },
      { canonicalContext: "\ud800" },
      { targetRef: "wrong" },
      { extra: true },
    ]) {
      const { model, current, port } = fixture();
      current.value = {
        ...context,
        requirements: { ...context.requirements, ...patch } as NonNullable<
          KubernetesSetupContext["requirements"]
        >,
      };
      model.contentSHA256.value = "a".repeat(64);
      model.contentBytes.value = "128";
      await model.begin();
      expect(model.canBegin.value).toBe(false);
      expect(port.beginKubernetesSetup).not.toHaveBeenCalled();
    }
  });
  it("changed target or canonicalContext in Begin acknowledgement cannot yield a downloadable permit", async () => {
    for (const patch of [
      { targetRef: "ktg_foreign01" },
      { canonicalContext: "another-context" },
    ]) {
      const { model, port } = fixture();
      port.beginKubernetesSetup.mockResolvedValue({
        ...admission,
        setup: { ...view, ...patch },
      });
      await model.begin();
      expect(model.hasAdmission.value).toBe(false);
      expect(model.uncertain.value).toBe(true);
      expect(model.problem.value).toBeDefined();
    }
  });
});
