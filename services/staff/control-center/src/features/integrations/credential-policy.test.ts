import { effectScope } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";

interface SDKRequest {
  path: { connectionRef: string };
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
}
const sdk = vi.hoisted(() => ({
  getIntegrationConnection:
    vi.fn<
      (request: SDKRequest) => Promise<{ data: unknown; response: Response }>
    >(),
  setIntegrationCredentialPolicy:
    vi.fn<
      (request: SDKRequest) => Promise<{ data: unknown; response: Response }>
    >(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  readCredentialPolicy,
  saveCredentialPolicy,
} from "./credential-policy-api";
import {
  credentialPolicySupported,
  useCredentialPolicy,
  type CredentialPolicyAPI,
} from "./credential-policy";

const definition: IntegrationDefinition = {
  key: "gitlab",
  name: "Synthetic GitLab",
  description: "Synthetic fixture",
  category: "development",
  builtIn: true,
  available: true,
  nextActions: [],
  version: 1,
  capabilities: [],
  configurationFields: [],
  schemaVersion: "integrations.kodex.io/v1",
  definitionVersion: "1.0.0",
  digest: "a".repeat(64),
  origin: "SHIPPED",
  adapter: "GITLAB",
  adapterOwner: "integration-gateway",
  executionRoute: "MANAGED_MCP",
  adapterReadiness: "READY",
  credentialSecretKey: "token",
  connectionCount: 1,
  healthyConnectionCount: 0,
};
const connection: IntegrationConnection = {
  ref: "int_policy_fixture",
  version: 4,
  definitionKey: definition.key,
  definitionVersion: definition.definitionVersion,
  definitionDigest: definition.digest,
  credentialPolicy: "PROJECT_SHARED",
  name: "Synthetic credential policy",
  state: "DISABLED",
  credentialsConfigured: false,
  credentialsHint: "Not configured",
  capabilities: [],
  grants: [],
  nextActions: ["UPDATE", "ENABLE"],
  publicConfiguration: {},
};
const selected: IntegrationConnection = {
  ...connection,
  version: 5,
  credentialPolicy: "INITIATOR_REQUIRED",
};
const scopes: ReturnType<typeof effectScope>[] = [];

function fixture() {
  const port = {
    readCredentialPolicy: vi
      .fn<CredentialPolicyAPI["readCredentialPolicy"]>()
      .mockResolvedValue(connection),
    saveCredentialPolicy: vi
      .fn<CredentialPolicyAPI["saveCredentialPolicy"]>()
      .mockResolvedValue(selected),
  };
  const changed = vi.fn();
  const scope = effectScope();
  scopes.push(scope);
  const model = scope.run(() =>
    useCredentialPolicy(() => definition, changed, port),
  );
  if (!model)
    throw new Error("Synthetic credential policy scope is unavailable");
  return { model, port, changed };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
beforeEach(() => {
  resetOwnerRequests();
  vi.resetAllMocks();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"c".repeat(43)}` });
  sdk.getIntegrationConnection.mockResolvedValue({
    data: connection,
    response: new Response(),
  });
  sdk.setIntegrationCredentialPolicy.mockResolvedValue({
    data: selected,
    response: new Response(),
  });
});
afterEach(() => {
  for (const scope of scopes.splice(0)) scope.stop();
  resetOwnerRequests();
  vi.unstubAllGlobals();
});

describe("credential policy API", () => {
  it("uses the dedicated command with exact OCC, CSRF and only the policy input", async () => {
    const signal = new AbortController().signal;
    expect(await readCredentialPolicy(connection.ref, signal)).toEqual(
      connection,
    );
    expect(
      await saveCredentialPolicy(connection, "INITIATOR_REQUIRED", signal),
    ).toEqual(selected);
    const input = sdk.setIntegrationCredentialPolicy.mock.calls[0]?.[0];
    if (!input?.headers)
      throw new Error("Synthetic policy command was not sent");
    expect(input.body).toEqual({ policy: "INITIATOR_REQUIRED" });
    expect(input.path).toEqual({ connectionRef: connection.ref });
    expect(input.headers["If-Match"]).toBe('"4"');
    expect(input.headers["X-CSRF-Token"]).toBe("c".repeat(43));
    expect(input.headers["Idempotency-Key"]).toBeTruthy();
    expect(input.signal).toBe(signal);
    expect(sdk.setIntegrationCredentialPolicy).toHaveBeenCalledTimes(1);
  });
  it.each([
    { ...selected, ref: "int_foreign_fixture" },
    { ...selected, version: 6 },
    { ...selected, credentialPolicy: "PROJECT_SHARED" },
    { ...selected, state: "CONNECTED" },
    { ...selected, definitionDigest: "b".repeat(64) },
  ])("rejects a malformed or foreign mutation receipt", async (value) => {
    sdk.setIntegrationCredentialPolicy.mockResolvedValue({
      data: value,
      response: new Response(),
    });
    await expect(
      saveCredentialPolicy(
        connection,
        "INITIATOR_REQUIRED",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    expect(sdk.setIntegrationCredentialPolicy).toHaveBeenCalledTimes(1);
  });
  it("does not retry a lost write response", async () => {
    sdk.setIntegrationCredentialPolicy.mockRejectedValue(
      new Error("Synthetic lost response"),
    );
    await expect(
      saveCredentialPolicy(
        connection,
        "INITIATOR_REQUIRED",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    expect(sdk.setIntegrationCredentialPolicy).toHaveBeenCalledTimes(1);
  });
  it("does not offer personal policy for unsupported, stale or unauthorized packages", () => {
    expect(credentialPolicySupported(connection, definition)).toBe(true);
    expect(credentialPolicySupported(connection)).toBe(false);
    expect(
      credentialPolicySupported({ ...connection, nextActions: [] }, definition),
    ).toBe(false);
    expect(
      credentialPolicySupported(connection, {
        ...definition,
        credentialSecretKey: undefined,
      }),
    ).toBe(false);
    expect(
      credentialPolicySupported(connection, {
        ...definition,
        executionRoute: "INTERACTION",
      }),
    ).toBe(false);
    expect(
      credentialPolicySupported(connection, {
        ...definition,
        digest: "b".repeat(64),
      }),
    ).toBe(false);
  });
});

describe("credential policy owner lifetime", () => {
  it("loads fresh state before enabling a mutation and applies one verified result", async () => {
    const { model, port, changed } = fixture();
    await model.open({ ...connection, state: "CONNECTED", version: 1 });
    expect(port.readCredentialPolicy).toHaveBeenCalledTimes(1);
    expect(model.connection.value?.version).toBe(4);
    expect(model.allowed.value).toBe(true);
    model.selection.value = "INITIATOR_REQUIRED";
    await model.save();
    expect(port.saveCredentialPolicy).toHaveBeenCalledTimes(1);
    expect(model.saved.value).toBe(true);
    expect(model.connection.value?.credentialPolicy).toBe("INITIATOR_REQUIRED");
    expect(changed).toHaveBeenCalledTimes(1);
    await model.save();
    expect(port.saveCredentialPolicy).toHaveBeenCalledTimes(1);
  });
  it("refuses an enabled connection even if the displayed row was disabled", async () => {
    const { model, port } = fixture();
    port.readCredentialPolicy.mockResolvedValue({
      ...connection,
      state: "CONNECTED",
    });
    await model.open(connection);
    model.selection.value = "INITIATOR_REQUIRED";
    await model.save();
    expect(model.allowed.value).toBe(false);
    expect(port.saveCredentialPolicy).not.toHaveBeenCalled();
  });
  it("requires explicit readback after a lost response rather than writing again", async () => {
    const { model, port, changed } = fixture();
    await model.open(connection);
    model.selection.value = "INITIATOR_REQUIRED";
    port.saveCredentialPolicy.mockRejectedValue(
      new Error("Synthetic lost response"),
    );
    await model.save();
    expect(model.requiresRefresh.value).toBe(true);
    await model.save();
    expect(port.saveCredentialPolicy).toHaveBeenCalledTimes(1);
    port.readCredentialPolicy.mockResolvedValue(selected);
    await model.refresh();
    expect(model.connection.value).toEqual(selected);
    expect(model.requiresRefresh.value).toBe(false);
    expect(changed).not.toHaveBeenCalled();
    await model.save();
    expect(port.saveCredentialPolicy).toHaveBeenCalledTimes(1);
  });
  it("ignores an old read response after closing and opening another connection", async () => {
    const { model, port } = fixture();
    const old = deferred<IntegrationConnection>();
    port.readCredentialPolicy.mockReturnValueOnce(old.promise);
    const opening = model.open(connection);
    model.close();
    const other = { ...connection, ref: "int_policy_other" };
    port.readCredentialPolicy.mockResolvedValueOnce(other);
    await model.open(other);
    old.resolve(connection);
    await opening;
    expect(model.connection.value?.ref).toBe(other.ref);
    expect(model.problem.value).toBeUndefined();
  });
  it.each(["ACK", "ERROR"])(
    "ignores a late %s after owner invalidation",
    async (outcome) => {
      const { model, port, changed } = fixture();
      await model.open(connection);
      const pending = deferred<IntegrationConnection>();
      port.saveCredentialPolicy.mockReturnValueOnce(pending.promise);
      model.selection.value = "INITIATOR_REQUIRED";
      const saving = model.save();
      resetOwnerRequests();
      if (outcome === "ACK") pending.resolve(selected);
      else pending.reject(new Error("Synthetic old-owner failure"));
      await saving;
      expect(model.opened.value).toBe(false);
      expect(model.connection.value).toBeUndefined();
      expect(model.problem.value).toBeUndefined();
      expect(changed).not.toHaveBeenCalled();
      await model.open(connection);
      expect(model.opened.value).toBe(false);
    },
  );
});
