import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import type {
  PersonalIntegrationCredential,
  PersonalIntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
interface SDKRequest {
  path?: { connectionRef: string };
  body?: Record<string, unknown>;
  headers?: Record<string, string>;
  signal?: AbortSignal;
}
const sdk = vi.hoisted(() => {
  const operation = () =>
    vi.fn<
      (input: SDKRequest) => Promise<{ data: unknown; response: Response }>
    >();
  return {
    listPersonalIntegrationConnections: operation(),
    createPersonalIntegrationCredential: operation(),
    configurePersonalIntegrationCredential: operation(),
    testPersonalIntegrationCredential: operation(),
    revokePersonalIntegrationCredential: operation(),
  };
});
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  personalCredentialView,
  loadPersonalConnections,
  createPersonalConnection,
  savePersonalConnection,
  testPersonalConnection,
  revokePersonalConnection,
} from "./personal-connections-api";
const connection: PersonalIntegrationConnection = {
  connectionRef: "int_fixture01",
  connectionVersion: 7,
  name: "GitLab",
  definitionKey: "gitlab",
  policy: "INITIATOR_REQUIRED",
  available: true,
  enabled: true,
};
const credential: PersonalIntegrationCredential = {
  connectionRef: connection.connectionRef,
  connectionVersion: 7,
  bindingRef: "ipc_fixture01",
  version: 3,
  policy: "INITIATOR_REQUIRED",
  state: "CHECK_REQUIRED",
  credentialConfigured: true,
  verifiedCapabilityKeys: [],
  deniedCapabilityKeys: [],
};
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"c".repeat(43)}` });
  sdk.createPersonalIntegrationCredential.mockResolvedValue({
    data: {
      ...credential,
      version: 1,
      state: "NOT_CONFIGURED",
      credentialConfigured: false,
    },
    response: new Response(),
  });
  sdk.configurePersonalIntegrationCredential.mockResolvedValue({
    data: { ...credential, version: 4 },
    response: new Response(),
  });
  sdk.testPersonalIntegrationCredential.mockResolvedValue({
    data: { ...credential, version: 4, state: "CHECKING" },
    response: new Response(),
  });
  sdk.revokePersonalIntegrationCredential.mockResolvedValue({
    data: {
      ...credential,
      version: 4,
      state: "REVOKED",
      credentialConfigured: false,
    },
    response: new Response(),
  });
});
afterEach(() => vi.unstubAllGlobals());
describe("личные подключения: HTTP boundary", () => {
  it("отправляет закрытые адреса, разные OCC, CSRF и idempotency без owner selector", async () => {
    const signal = new AbortController().signal;
    await createPersonalConnection(connection, signal);
    await savePersonalConnection(
      connection,
      credential,
      "synthetic-key",
      signal,
    );
    await testPersonalConnection(connection, credential, signal);
    await revokePersonalConnection(connection, credential, signal);
    for (const operation of [
      sdk.createPersonalIntegrationCredential,
      sdk.configurePersonalIntegrationCredential,
      sdk.testPersonalIntegrationCredential,
      sdk.revokePersonalIntegrationCredential,
    ]) {
      expect(operation).toHaveBeenCalledOnce();
      const input = operation.mock.calls[0]?.[0];
      if (!input) throw new Error("Expected SDK request is absent");
      expect(input.path).toEqual({ connectionRef: connection.connectionRef });
      expect(input.signal).toBe(signal);
      expect(input.headers?.["X-CSRF-Token"]).toBe("c".repeat(43));
      expect(input.headers?.["Idempotency-Key"]).toBeTruthy();
      if (operation !== sdk.createPersonalIntegrationCredential)
        expect(input.headers?.["If-Match"]).toBe('"3"');
    }
    expect(
      sdk.configurePersonalIntegrationCredential.mock.calls[0]?.[0].body,
    ).toEqual({ expectedConnectionVersion: 7, value: "synthetic-key" });
    expect(
      sdk.testPersonalIntegrationCredential.mock.calls[0]?.[0].body,
    ).toEqual({ expectedConnectionVersion: 7 });
    expect(
      sdk.revokePersonalIntegrationCredential.mock.calls[0]?.[0].body,
    ).toEqual({});
  });
  it("отвергает stale либо несогласованный mutation receipt", async () => {
    for (const change of [
      { version: 3 },
      { version: 5 },
      { state: "READY" },
      { credentialConfigured: false },
    ]) {
      sdk.configurePersonalIntegrationCredential.mockResolvedValueOnce({
        data: { ...credential, version: 4, ...change },
        response: new Response(),
      });
      await expect(
        savePersonalConnection(
          connection,
          credential,
          "synthetic-key",
          new AbortController().signal,
        ),
      ).rejects.toThrow();
    }
  });
  it("не повторяет mutation после transport error", async () => {
    sdk.configurePersonalIntegrationCredential.mockRejectedValueOnce(
      new TypeError("synthetic timeout"),
    );
    await expect(
      savePersonalConnection(
        connection,
        credential,
        "synthetic-key",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    expect(sdk.configurePersonalIntegrationCredential).toHaveBeenCalledOnce();
  });
  it("сохраняет только публичную проекцию и отвергает чужие pins", () => {
    const extended = {
      ...credential,
      value: "synthetic-key",
      secretRef: "foreign",
      subjectId: "other",
    };
    expect(personalCredentialView(extended, connection)).toEqual(credential);
    for (const change of [
      { connectionRef: "int_other01" },
      { connectionVersion: 8 },
      { version: 0 },
      { state: "UNKNOWN" },
      { state: "READY" },
    ])
      expect(() =>
        personalCredentialView(
          { ...credential, ...change } as PersonalIntegrationCredential,
          connection,
        ),
      ).toThrow();
  });
  it("принимает последнюю HTTP-страницу без optional курсора, включая пустой каталог", async () => {
    for (const items of [[], [connection]]) {
      const response = new Response(JSON.stringify({ items }), {
        headers: { "Content-Type": "application/json" },
      });
      sdk.listPersonalIntegrationConnections.mockResolvedValueOnce({
        data: await response.clone().json(),
        response,
      });
      await expect(
        loadPersonalConnections("", undefined, new AbortController().signal),
      ).resolves.toEqual({ items, nextPageToken: "" });
    }
  });
  it("сохраняет continuation и не скрывает malformed курсор либо отсутствующий каталог", async () => {
    sdk.listPersonalIntegrationConnections.mockResolvedValueOnce({
      data: { items: [connection], nextPageToken: "next-fixture" },
      response: new Response(),
    });
    await expect(
      loadPersonalConnections("", undefined, new AbortController().signal),
    ).resolves.toEqual({ items: [connection], nextPageToken: "next-fixture" });
    for (const data of [
      {},
      { nextPageToken: "" },
      { items: null },
      { items: [], nextPageToken: null },
      { items: [], nextPageToken: 1 },
      { items: [], nextPageToken: {} },
      { items: [], nextPageToken: "x".repeat(513) },
      { items: Array.from({ length: 51 }, () => connection) },
    ]) {
      sdk.listPersonalIntegrationConnections.mockResolvedValueOnce({
        data,
        response: new Response(),
      });
      await expect(
        loadPersonalConnections("", undefined, new AbortController().signal),
      ).rejects.toThrow();
    }
  });
  it("проверяет bounded catalog и соответствие binding подключению", async () => {
    sdk.listPersonalIntegrationConnections.mockResolvedValueOnce({
      data: { items: [{ ...connection, credential }], nextPageToken: "" },
      response: new Response(),
    });
    expect(
      (
        await loadPersonalConnections(
          "git",
          undefined,
          new AbortController().signal,
        )
      ).items,
    ).toEqual([{ ...connection, credential }]);
    sdk.listPersonalIntegrationConnections.mockResolvedValueOnce({
      data: {
        items: [
          {
            ...connection,
            credential: { ...credential, connectionRef: "int_other01" },
          },
        ],
        nextPageToken: "",
      },
      response: new Response(),
    });
    await expect(
      loadPersonalConnections("", undefined, new AbortController().signal),
    ).rejects.toThrow();
  });
});
