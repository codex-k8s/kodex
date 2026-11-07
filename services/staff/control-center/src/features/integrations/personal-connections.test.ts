import { effectScope, type EffectScope } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import type {
  PersonalIntegrationConnection,
  PersonalIntegrationCredential,
} from "@/shared/api/generated/openapi/types.gen";
import {
  personalConnectionAllows,
  usePersonalConnections,
  type PersonalConnectionsAPI,
} from "./personal-connections";

const connection: PersonalIntegrationConnection = {
  connectionRef: "int_fixture01",
  connectionVersion: 7,
  name: "Личный GitLab",
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
function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Fixture value is absent");
  return value;
}
const scopes: EffectScope[] = [];
function fixture(entries: PersonalIntegrationConnection[] = [connection]) {
  const port = {
    loadPersonalConnections: vi
      .fn<PersonalConnectionsAPI["loadPersonalConnections"]>()
      .mockResolvedValue({
        items: structuredClone(entries),
        nextPageToken: "",
      }),
    createPersonalConnection: vi
      .fn<PersonalConnectionsAPI["createPersonalConnection"]>()
      .mockResolvedValue({
        ...credential,
        version: 1,
        state: "NOT_CONFIGURED",
        credentialConfigured: false,
      }),
    savePersonalConnection: vi
      .fn<PersonalConnectionsAPI["savePersonalConnection"]>()
      .mockResolvedValue(credential),
    testPersonalConnection: vi
      .fn<PersonalConnectionsAPI["testPersonalConnection"]>()
      .mockResolvedValue({ ...credential, version: 4, state: "CHECKING" }),
    revokePersonalConnection: vi
      .fn<PersonalConnectionsAPI["revokePersonalConnection"]>()
      .mockResolvedValue({
        ...credential,
        version: 4,
        state: "REVOKED",
        credentialConfigured: false,
      }),
  };
  const scope = effectScope();
  scopes.push(scope);
  const model = scope.run(() => usePersonalConnections(port));
  if (!model) throw new Error("Fixture scope is inactive");
  return { port, scope, model };
}
afterEach(() => {
  for (const scope of scopes.splice(0)) scope.stop();
  resetOwnerRequests();
});

describe("личные подключения: жизненный цикл UI", () => {
  it("создаёт собственную metadata, сохраняет ключ один раз и очищает поле до ответа", async () => {
    const { port, model } = fixture();
    await model.load();
    model.edit(required(model.items.value[0]));
    model.credentialValue.value = "synthetic-private-key";
    const pending = model.execute(connection.connectionRef, "SAVE");
    expect(model.credentialValue.value).toBe("");
    expect(model.editingRef.value).toBe("");
    await pending;
    expect(port.createPersonalConnection).toHaveBeenCalledOnce();
    expect(port.savePersonalConnection).toHaveBeenCalledOnce();
    expect(port.savePersonalConnection.mock.calls[0]?.slice(0, 3)).toEqual([
      connection,
      {
        ...credential,
        version: 1,
        state: "NOT_CONFIGURED",
        credentialConfigured: false,
      },
      "synthetic-private-key",
    ]);
    expect(JSON.stringify(model.items.value)).not.toContain(
      "synthetic-private-key",
    );
    expect(model.success.value).toBe("SAVE");
  });
  it("после потерянного ответа не повторяет write и требует readback", async () => {
    const { port, model } = fixture([{ ...connection, credential }]);
    await model.load();
    model.edit(required(model.items.value[0]));
    model.credentialValue.value = "synthetic-private-key";
    port.savePersonalConnection.mockRejectedValueOnce(
      new Error("lost response"),
    );
    await model.execute(connection.connectionRef, "SAVE");
    expect(model.requiresRefresh.value).toBe(true);
    expect(model.credentialValue.value).toBe("");
    await model.execute(connection.connectionRef, "TEST");
    expect(port.testPersonalConnection).not.toHaveBeenCalled();
    expect(port.savePersonalConnection).toHaveBeenCalledOnce();
    await model.load();
    expect(model.requiresRefresh.value).toBe(false);
  });
  it("не разрешает вторую проверку CHECKING и сохраняет отзыв при отключённом adapter", async () => {
    const { port, model } = fixture([{ ...connection, credential }]);
    await model.load();
    await model.execute(connection.connectionRef, "TEST");
    await model.execute(connection.connectionRef, "TEST");
    expect(port.testPersonalConnection).toHaveBeenCalledOnce();
    const disabled = {
      ...required(model.items.value[0]),
      available: false,
      policy: "PROJECT_SHARED" as const,
    };
    model.items.value = [disabled];
    expect(personalConnectionAllows(disabled, "SAVE")).toBe(false);
    expect(personalConnectionAllows(disabled, "TEST")).toBe(false);
    await model.execute(connection.connectionRef, "REVOKE");
    expect(port.revokePersonalConnection).toHaveBeenCalledOnce();
    expect(model.items.value[0]?.credential?.state).toBe("REVOKED");
  });
  it("очищает поле при отмене, смене подключения и остановке scope", async () => {
    const second = { ...connection, connectionRef: "int_fixture02" };
    const { model, scope } = fixture([connection, second]);
    await model.load();
    model.edit(required(model.items.value[0]));
    model.credentialValue.value = "synthetic-private-key";
    model.edit(required(model.items.value[1]));
    expect(model.credentialValue.value).toBe("");
    model.credentialValue.value = "another-synthetic-key";
    model.closeEditor();
    expect(model.credentialValue.value).toBe("");
    model.edit(required(model.items.value[0]));
    model.credentialValue.value = "final-synthetic-key";
    scope.stop();
    expect(model.items.value).toEqual([]);
    expect(model.credentialValue.value).toBe("");
  });
  it("отклоняет устаревший ответ чтения после смены owner и не изменяет новую сессию", async () => {
    const old = fixture();
    let resolve!: (page: {
      items: PersonalIntegrationConnection[];
      nextPageToken: string;
    }) => void;
    old.port.loadPersonalConnections.mockReturnValueOnce(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const pending = old.model.load();
    resetOwnerRequests();
    const fresh = fixture([{ ...connection, name: "Новая сессия" }]);
    await fresh.model.load();
    resolve({ items: [connection], nextPageToken: "" });
    await pending;
    expect(old.model.items.value).toEqual([]);
    expect(old.model.unavailable.value).toBe(true);
    expect(fresh.model.items.value[0]?.name).toBe("Новая сессия");
  });
  it("после pagehide не применяет старый ACK и возобновляет только чтение", async () => {
    const { port, model } = fixture([{ ...connection, credential }]);
    await model.load();
    let resolve!: (value: PersonalIntegrationCredential) => void;
    port.testPersonalConnection.mockReturnValueOnce(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const pending = model.execute(connection.connectionRef, "TEST");
    model.suspend();
    resolve({ ...credential, version: 4, state: "CHECKING" });
    await pending;
    expect(model.requiresRefresh.value).toBe(true);
    expect(model.items.value[0]?.credential?.version).toBe(3);
    expect(model.success.value).toBeUndefined();
    await model.load();
    expect(model.requiresRefresh.value).toBe(false);
  });
  it("проверяет UTF-8 лимит и не создаёт пустую привязку при неверном ключе", async () => {
    const { port, model } = fixture();
    await model.load();
    model.edit(required(model.items.value[0]));
    model.credentialValue.value = "я".repeat(8193);
    await model.execute(connection.connectionRef, "SAVE");
    expect(port.createPersonalConnection).not.toHaveBeenCalled();
  });
  it("не принимает повторяющиеся строки и зацикленный cursor", async () => {
    const { port, model } = fixture();
    port.loadPersonalConnections.mockResolvedValueOnce({
      items: [connection],
      nextPageToken: "page2",
    });
    await model.load();
    port.loadPersonalConnections.mockResolvedValueOnce({
      items: [{ ...connection, connectionRef: "int_fixture02" }],
      nextPageToken: "page2",
    });
    await model.load(true);
    expect(model.problem.value).toBeDefined();
    expect(model.items.value).toHaveLength(1);
    port.loadPersonalConnections.mockResolvedValueOnce({
      items: [connection, connection],
      nextPageToken: "",
    });
    await model.load();
    expect(model.problem.value).toBeDefined();
    expect(model.items.value).toHaveLength(1);
  });
});
