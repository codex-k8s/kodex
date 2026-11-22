import { webcrypto } from "node:crypto";
import { effectScope, type EffectScope } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import type { SshIdentitySetup } from "@/shared/api/generated/openapi/types.gen";
import {
  checkedSSHSetup,
  checkedSSHUploadCapability,
  uploadSSHMaterial,
  type SSHSetupContext,
} from "./ssh-identity-setup-api";
import { useSSHIdentitySetup, type SSHSetupPort } from "./ssh-identity-setup";

const context: SSHSetupContext = {
  connectionRef: "int_fixture_123",
  connectionVersion: 7,
  bindingVersion: 0,
  policy: "INITIATOR_REQUIRED",
};
const setup: SshIdentitySetup = {
  ref: "ssu_12345678901234567890123456789012",
  connectionRef: context.connectionRef,
  identityRef: "bki_fixture_123",
  targetRef: "target_fixture_123",
  reasonCode: "PREPARED",
  version: 1,
  connectionVersion: 7,
  bindingVersion: 1,
  bindingGeneration: 1,
  identityRevision: 1,
  policy: "INITIATOR_REQUIRED",
  state: "PREPARED",
  expiresAt: "2026-11-15T12:00:00Z",
};
const capability = {
  origin: "https://ssh-upload.example.invalid",
  tlsServerName: "ssh-upload.example.invalid",
  permit: "cHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHA",
};
const scopes: EffectScope[] = [];
function fixture(getContext = () => context) {
  vi.stubGlobal("crypto", webcrypto);
  const port = {
    beginSSHSetup: vi.fn<SSHSetupPort["beginSSHSetup"]>().mockResolvedValue({
      setup: { ...setup },
      reconcileOnly: false,
      upload: { ...capability },
    }),
    readSSHSetup: vi.fn<SSHSetupPort["readSSHSetup"]>().mockResolvedValue({
      ...setup,
      state: "COMPLETED",
      reasonCode: "COMPLETED",
      version: 3,
    }),
    recoverSSHSetup: vi
      .fn<SSHSetupPort["recoverSSHSetup"]>()
      .mockResolvedValue({
        ...setup,
        state: "UNKNOWN",
        reasonCode: "MATERIAL_UNKNOWN",
        version: 3,
      }),
    cancelSSHSetup: vi.fn<SSHSetupPort["cancelSSHSetup"]>().mockResolvedValue({
      ...setup,
      state: "CANCELLED",
      reasonCode: "CANCELLED",
      version: 4,
    }),
    uploadSSHMaterial: vi
      .fn<SSHSetupPort["uploadSSHMaterial"]>()
      .mockResolvedValue(undefined),
  };
  const scope = effectScope();
  scopes.push(scope);
  const model = scope.run(() => useSSHIdentitySetup(getContext, port));
  if (!model) throw new Error("Synthetic SSH setup scope is absent");
  return { model, port };
}
afterEach(() => {
  for (const scope of scopes.splice(0)) scope.stop();
  resetOwnerRequests();
  vi.unstubAllGlobals();
});

describe("SSH identity owner setup flow", () => {
  it("clears local key and owned bytes, sends only metadata to Begin, and publishes only owner readback", async () => {
    const { model, port } = fixture();
    model.privateKey.value = "synthetic-private-key-not-a-real-credential";
    await model.submit();
    expect(model.privateKey.value).toBe("");
    expect(port.beginSSHSetup).toHaveBeenCalledTimes(1);
    expect(JSON.stringify(port.beginSSHSetup.mock.calls)).not.toContain(
      "synthetic-private-key",
    );
    expect(port.beginSSHSetup.mock.calls[0]?.[1]).toMatch(/^[a-f0-9]{64}$/);
    expect(port.uploadSSHMaterial).toHaveBeenCalledTimes(1);
    const bytes = port.uploadSSHMaterial.mock.calls[0]?.[2];
    expect(bytes?.every((byte) => byte === 0)).toBe(true);
    expect(model.setup.value?.state).toBe("COMPLETED");
    expect(port.readSSHSetup).toHaveBeenCalledTimes(1);
    expect(model.canObserve()).toBe(false);
  });
  it("does not repeat an unknown material write; explicit Observe performs a fresh owner read and no material body", async () => {
    const { model, port } = fixture();
    port.uploadSSHMaterial.mockRejectedValueOnce(
      new Error("Synthetic response lost"),
    );
    port.readSSHSetup.mockResolvedValue({
      ...setup,
      state: "UNKNOWN",
      reasonCode: "MATERIAL_UNKNOWN",
      version: 3,
    });
    model.privateKey.value = "synthetic-key";
    await model.submit();
    expect(port.uploadSSHMaterial).toHaveBeenCalledTimes(1);
    expect(model.setup.value?.state).toBe("UNKNOWN");
    expect(model.canObserve()).toBe(true);
    await model.observe();
    expect(port.beginSSHSetup).toHaveBeenCalledTimes(1);
    expect(port.uploadSSHMaterial).toHaveBeenCalledTimes(2);
    expect(port.uploadSSHMaterial.mock.calls[1]?.[2]).toBeUndefined();
    expect(port.readSSHSetup).toHaveBeenCalledTimes(3);
  });
  it("recovers the original Begin receipt without recreating a permit or repeating Begin", async () => {
    const { model, port } = fixture();
    port.beginSSHSetup.mockRejectedValueOnce(
      new Error("Synthetic Begin response lost"),
    );
    model.privateKey.value = "synthetic-key";
    await model.submit();
    const key = model.pendingBeginKey.value;
    expect(key).not.toBe("");
    expect(port.uploadSSHMaterial).not.toHaveBeenCalled();
    await model.refresh();
    expect(port.recoverSSHSetup).toHaveBeenCalledWith(
      context.connectionRef,
      key,
      expect.any(AbortSignal),
    );
    expect(port.beginSSHSetup).toHaveBeenCalledTimes(1);
    expect(model.canObserve()).toBe(false);
  });
  it("keeps rejected acknowledgements closed and does not infer successful attachment from an upload receipt", async () => {
    const { model, port } = fixture();
    port.readSSHSetup.mockResolvedValue({
      ...setup,
      state: "ACK_REJECTED",
      reasonCode: "ACK_REJECTED",
      version: 3,
    });
    model.privateKey.value = "synthetic-key";
    await model.submit();
    expect(model.setup.value?.state).toBe("ACK_REJECTED");
    expect(model.canObserve()).toBe(false);
    await model.observe();
    expect(port.uploadSSHMaterial).toHaveBeenCalledTimes(1);
  });
  it("refuses a late owner read after logout without restoring key, setup or errors", async () => {
    const { model, port } = fixture();
    let release: ((value: SshIdentitySetup) => void) | undefined;
    port.readSSHSetup.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    model.privateKey.value = "synthetic-key";
    const pending = model.submit();
    await vi.waitFor(() => expect(port.readSSHSetup).toHaveBeenCalledTimes(1));
    resetOwnerRequests();
    release?.({ ...setup, state: "COMPLETED", reasonCode: "COMPLETED" });
    await pending;
    expect(model.setup.value).toBeUndefined();
    expect(model.privateKey.value).toBe("");
    expect(model.problem.value).toBeUndefined();
    expect(model.unavailable.value).toBe(true);
  });
  it("never clears a newer Connection result when an old read resolves after context replacement", async () => {
    let current = { ...context };
    const { model, port } = fixture(() => current);
    let release: ((value: SshIdentitySetup) => void) | undefined;
    port.readSSHSetup.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    model.privateKey.value = "old-synthetic-key";
    const old = model.submit();
    await vi.waitFor(() => expect(port.readSSHSetup).toHaveBeenCalledTimes(1));
    current = {
      ...context,
      connectionRef: "int_replacement_123",
      connectionVersion: 8,
    };
    model.resetContext();
    const replacement = {
      ...setup,
      connectionRef: current.connectionRef,
      connectionVersion: 8,
      ref: "ssu_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    };
    port.beginSSHSetup.mockResolvedValueOnce({
      setup: replacement,
      reconcileOnly: false,
      upload: capability,
    });
    port.readSSHSetup.mockResolvedValueOnce({
      ...replacement,
      state: "COMPLETED",
      reasonCode: "COMPLETED",
    });
    model.privateKey.value = "new-synthetic-key";
    await model.submit();
    release?.(setup);
    await old;
    expect(model.setup.value?.connectionRef).toBe(current.connectionRef);
    expect(model.setup.value?.state).toBe("COMPLETED");
  });
});

describe("SSH browser metadata and direct upload boundary", () => {
  it("rejects foreign or private metadata and mixed personal/shared versions", () => {
    expect(checkedSSHSetup(setup, context.connectionRef)).toBe(setup);
    for (const changed of [
      { ...setup, connectionRef: "int_foreign_123" },
      { ...setup, bindingVersion: 0 },
      { ...setup, state: "FUTURE" },
      { ...setup, privateKey: "synthetic-forbidden" },
      { ...setup, version: 9007199254740992 },
    ]) {
      expect(() =>
        checkedSSHSetup(changed as SshIdentitySetup, context.connectionRef),
      ).toThrow();
    }
    expect(() =>
      checkedSSHUploadCapability({
        ...capability,
        origin: "http://ssh-upload.example.invalid",
      }),
    ).toThrow();
    expect(() =>
      checkedSSHUploadCapability({
        ...capability,
        tlsServerName: "foreign.example.invalid",
      }),
    ).toThrow();
    expect(() =>
      checkedSSHUploadCapability({
        ...capability,
        origin: "https://ssh-upload.example.invalid:",
      }),
    ).toThrow();
  });
  it("uses a single credential-free HTTPS browser request and clears its private copy without changing the caller buffer", async () => {
    let sent: Uint8Array | undefined;
    const transport = vi.fn<typeof fetch>().mockImplementation((_url, init) => {
      sent = init?.body as Uint8Array;
      expect(new TextDecoder().decode(sent)).toBe("synthetic-key");
      return Promise.resolve(
        new Response(
          '{"outcome":"EVIDENCE_RECORDED","reason":"OWNER_READ_REQUIRED"}',
          { status: 200 },
        ),
      );
    });
    vi.stubGlobal("fetch", transport);
    const input = new TextEncoder().encode("synthetic-key");
    await uploadSSHMaterial(
      capability,
      setup.ref,
      input,
      new AbortController().signal,
    );
    expect(transport).toHaveBeenCalledTimes(1);
    expect(transport.mock.calls[0]?.[1]).toMatchObject({
      method: "POST",
      credentials: "omit",
      mode: "cors",
      redirect: "error",
      cache: "no-store",
      referrerPolicy: "no-referrer",
    });
    expect(sent?.every((byte) => byte === 0)).toBe(true);
    expect(new TextDecoder().decode(input)).toBe("synthetic-key");
  });
  it("never retries a failed POST or follows a redirect, leaving reconciliation to the owner", async () => {
    const transport = vi
      .fn<typeof fetch>()
      .mockRejectedValue(new Error("Synthetic network outcome unknown"));
    vi.stubGlobal("fetch", transport);
    await expect(
      uploadSSHMaterial(
        capability,
        setup.ref,
        new TextEncoder().encode("synthetic-key"),
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    expect(transport).toHaveBeenCalledTimes(1);
  });
});
