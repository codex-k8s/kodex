import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { effectScope, nextTick, ref } from "vue";
import type {
  PersonalIntegrationConnection,
  PersonalIntegrationCredential,
} from "@/shared/api/generated/openapi/types.gen";
interface SDKRequest {
  headers?: Record<string, string>;
  body?: Record<string, unknown>;
  signal?: AbortSignal;
  path?: { connectionRef: string };
}
const sdk = vi.hoisted(() => {
  const operation = () =>
    vi.fn<
      (input: SDKRequest) => Promise<{ data: unknown; response: Response }>
    >();
  return {
    getIntegrationConnection: operation(),
    getPersonalIntegrationCredential: operation(),
    testPersonalIntegrationCredential: operation(),
    updateIntegrationConnection: operation(),
  };
});
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import { useGitHubRepositoryScope } from "./github-repository-scope";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Synthetic fixture value is absent");
  return value;
}

const credential: PersonalIntegrationCredential = {
  connectionRef: "int_fixture01",
  connectionVersion: 7,
  bindingRef: "ipc_fixture01",
  version: 3,
  policy: "INITIATOR_REQUIRED",
  state: "READY",
  credentialConfigured: true,
  verifiedCapabilityKeys: ["github.identity.read"],
  deniedCapabilityKeys: [],
};
const connection = (): PersonalIntegrationConnection => ({
  connectionRef: credential.connectionRef,
  connectionVersion: 7,
  name: "GitHub",
  definitionKey: "github",
  policy: "INITIATOR_REQUIRED",
  available: true,
  enabled: true,
  credential: { ...credential },
});
const discovered = {
  identityId: 7,
  login: "synthetic",
  page: 1,
  nextPage: 0,
  items: [
    {
      id: 17,
      fullName: "team/repo",
      ownerKind: "Organization" as const,
      private: true,
      archived: false,
      canRead: true,
      canPush: false,
    },
  ],
};
const response = (data: unknown) => ({ data, response: new Response() });
beforeEach(() => {
  vi.resetAllMocks();
  vi.useFakeTimers();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"c".repeat(43)}` });
  resetOwnerRequests();
  sdk.testPersonalIntegrationCredential.mockResolvedValue(
    response({ ...credential, version: 4, state: "CHECKING" }),
  );
  sdk.getPersonalIntegrationCredential.mockResolvedValue(
    response({ ...credential, version: 5, githubDiscovery: discovered }),
  );
});
afterEach(() => {
  resetOwnerRequests();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
describe("GitHub scope: пользовательский lifecycle", () => {
  it("одним Test получает собственную страницу, сохраняет observed ID/name с OCC и очищает старое наблюдение", async () => {
    const scope = effectScope();
    const changed = vi.fn();
    const current = ref(connection());
    const model = required(
      scope.run(() => useGitHubRepositoryScope(current, changed)),
    );
    const pending = model.discover();
    await vi.advanceTimersByTimeAsync(1000);
    await pending;
    expect(sdk.testPersonalIntegrationCredential).toHaveBeenCalledTimes(1);
    expect(model.page.value).toEqual(discovered);
    expect(changed).toHaveBeenCalledOnce();
    model.kind.value = "SELECTED";
    model.toggle(required(required(model.page.value).items[0]));
    sdk.getIntegrationConnection.mockResolvedValue(
      response({
        ref: current.value.connectionRef,
        version: 7,
        name: "GitHub",
        nextActions: ["UPDATE"],
        publicConfiguration: {
          scope_kind: "ALL_ACCESSIBLE",
          service_identity: "explicit-service",
        },
      }),
    );
    sdk.updateIntegrationConnection.mockResolvedValue(
      response({ ref: current.value.connectionRef, version: 8 }),
    );
    await model.save();
    const request = required(
      required(sdk.updateIntegrationConnection.mock.calls[0])[0],
    );
    expect(request.headers?.["If-Match"]).toBe('"7"');
    expect(request.body?.publicConfiguration).toEqual({
      scope_kind: "SELECTED",
      service_identity: "explicit-service",
      repositories: '[{"id":17,"full_name":"team/repo"}]',
    });
    expect(model.page.value).toBeUndefined();
    expect(model.saved.value).toBe(true);
    scope.stop();
  });
  it("смена owner отзывает pending polling и запрещает применение late response", async () => {
    const scope = effectScope();
    const changed = vi.fn();
    const model = required(
      scope.run(() => useGitHubRepositoryScope(ref(connection()), changed)),
    );
    const pending = model.discover();
    await Promise.resolve();
    resetOwnerRequests();
    await pending;
    expect(model.page.value).toBeUndefined();
    expect(changed).not.toHaveBeenCalled();
    expect(sdk.getPersonalIntegrationCredential).not.toHaveBeenCalled();
    scope.stop();
  });
  it("не сохраняет выбор после изменения server version", async () => {
    const scope = effectScope();
    const current = ref(connection());
    const model = required(
      scope.run(() => useGitHubRepositoryScope(current, vi.fn())),
    );
    model.kind.value = "SELECTED";
    model.toggle(required(discovered.items[0]));
    sdk.getIntegrationConnection.mockResolvedValue(
      response({
        ref: current.value.connectionRef,
        version: 8,
        name: "GitHub",
        nextActions: ["UPDATE"],
        publicConfiguration: { scope_kind: "ALL_ACCESSIBLE" },
      }),
    );
    await model.save();
    expect(sdk.updateIntegrationConnection).not.toHaveBeenCalled();
    expect(model.problem.value).toBeDefined();
    current.value = { ...current.value, connectionVersion: 8 };
    await nextTick();
    expect(model.selected.value).toEqual([]);
    scope.stop();
  });
});
