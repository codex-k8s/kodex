import { effectScope, type EffectScope } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { usePersonalEmailConnections } from "./personal-email-connections";
import { mailboxSetupAttempts } from "./personal-email-recovery";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import { AppProblem } from "@/shared/api/problem";
import type {
  MyEmailMailbox,
  PersonalEmailMailboxMutationReceipt,
} from "@/shared/api/generated/openapi/types.gen";

const scopes: EffectScope[] = [];
afterEach(() => {
  for (const scope of scopes) scope.stop();
  scopes.length = 0;
  resetOwnerRequests();
});
function storage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    },
  };
}
function mailbox(): MyEmailMailbox {
  return {
    name: "Личный ящик",
    enabled: true,
    available: false,
    ownership: {
      connectionRef: "int_mailbox_owner",
      connectionVersion: 1,
      deliveryKind: "MAILBOX_CONFIGURATION",
      policy: "INITIATOR_REQUIRED",
      state: "ACTIVE",
      generation: 1,
      ownerEligible: true,
    },
  };
}
function fixture() {
  const cache = storage(),
    port = {
      loadMyMailboxes: vi
        .fn()
        .mockResolvedValue({ items: [], nextPageToken: "" }),
      writeMailboxSetup: vi.fn(),
      recoverMailboxSetup: vi.fn(),
    };
  const scope = effectScope();
  scopes.push(scope);
  const model = scope.run(() => usePersonalEmailConnections(port, cache));
  if (!model) throw new Error("Fixture scope unavailable");
  return { model, port, cache };
}
describe("личная почта: UI lifecycle", () => {
  it("считает Unicode codepoints перед записью:161кириллический запрещён,160emoji разрешены", async () => {
    const { model, port } = fixture();
    await model.load();
    model.sender.value = "owner@example.test";
    model.name.value = "я".repeat(161);
    await model.create();
    expect(port.writeMailboxSetup).not.toHaveBeenCalled();
    model.name.value = "😀".repeat(160);
    port.writeMailboxSetup.mockResolvedValue({
      operation: "CREATE",
      mailbox: { ...mailbox(), name: model.name.value },
    });
    await model.create();
    expect(port.writeMailboxSetup).toHaveBeenCalledOnce();
  });
  it("lost ACK не повторяет POST; явный повтор сохраняет key/body, затем читает текущий catalog", async () => {
    const { model, port, cache } = fixture();
    await model.load();
    model.name.value = "Личный ящик";
    model.sender.value = "owner@example.test";
    port.writeMailboxSetup.mockRejectedValueOnce(new TypeError("Lost ACK"));
    await model.create();
    const attempt = model.pending.value;
    expect(attempt).toBeDefined();
    expect(port.writeMailboxSetup).toHaveBeenCalledOnce();
    expect(mailboxSetupAttempts(cache)).toEqual([attempt]);
    await model.create();
    expect(port.writeMailboxSetup).toHaveBeenCalledOnce();
    await model.load();
    port.writeMailboxSetup.mockResolvedValue({
      operation: "CREATE",
      mailbox: mailbox(),
    });
    port.loadMyMailboxes.mockResolvedValue({
      items: [
        {
          ...mailbox(),
          ownership: { ...mailbox().ownership, ownerEligible: false },
        },
      ],
      nextPageToken: "",
    });
    await model.create();
    expect(port.writeMailboxSetup.mock.calls[1]?.[0]).toEqual(attempt);
    expect(port.writeMailboxSetup.mock.calls[1]?.[1]).toEqual(
      port.writeMailboxSetup.mock.calls[0]?.[1],
    );
    expect(model.pending.value).toBeUndefined();
    expect(mailboxSetupAttempts(cache)).toEqual([]);
    expect(model.items.value[0]?.ownership.ownerEligible).toBe(false);
  });
  it("квитанция после reopen не делает новую запись и не заменяет fresh read исторической availability", async () => {
    const first = fixture();
    await first.model.load();
    first.model.name.value = "Личный ящик";
    first.model.sender.value = "owner@example.test";
    first.port.writeMailboxSetup.mockRejectedValue(new TypeError("Lost ACK"));
    await first.model.create();
    scopes[scopes.length - 1]?.stop();
    const scope = effectScope();
    scopes.push(scope);
    const port = {
      loadMyMailboxes: vi
        .fn()
        .mockResolvedValue({ items: [], nextPageToken: "" }),
      writeMailboxSetup: vi.fn(),
      recoverMailboxSetup: vi
        .fn()
        .mockResolvedValue({ operation: "CREATE", mailbox: mailbox() }),
    };
    const reopened = scope.run(() =>
      usePersonalEmailConnections(port, first.cache),
    );
    if (!reopened) throw new Error("Fixture scope unavailable");
    expect(reopened.sender.value).toBe("");
    expect(reopened.pending.value).toBeDefined();
    await reopened.recover();
    expect(port.writeMailboxSetup).not.toHaveBeenCalled();
    expect(port.recoverMailboxSetup).toHaveBeenCalledOnce();
    expect(port.loadMyMailboxes).toHaveBeenCalledOnce();
    expect(reopened.items.value).toEqual([]);
  });
  it("отказ новой попытке закрывает её, но отказ после прежнего lost ACK не удаляет uncertainty", async () => {
    const { model, port } = fixture();
    await model.load();
    model.name.value = "Личный ящик";
    model.sender.value = "owner@example.test";
    const denied = new AppProblem({
      status: 403,
      code: "FORBIDDEN",
      kind: "forbidden",
      retryable: false,
    });
    port.writeMailboxSetup.mockRejectedValueOnce(denied);
    await model.create();
    expect(model.pending.value).toBeUndefined();
    expect(model.problem.value?.status).toBe(403);
    await model.load();
    port.writeMailboxSetup.mockRejectedValueOnce(new TypeError("Lost ACK"));
    await model.create();
    const attempt = model.pending.value;
    await model.load();
    port.writeMailboxSetup.mockRejectedValueOnce(denied);
    await model.create();
    expect(model.pending.value).toEqual(attempt);
    expect(port.recoverMailboxSetup).not.toHaveBeenCalled();
  });
  it("старый ACK после смены owner context не попадает в новую сессию", async () => {
    const { model, port } = fixture();
    await model.load();
    model.name.value = "Личный ящик";
    model.sender.value = "owner@example.test";
    let complete!: (value: PersonalEmailMailboxMutationReceipt) => void;
    port.writeMailboxSetup.mockImplementation(
      () =>
        new Promise<PersonalEmailMailboxMutationReceipt>((resolve) => {
          complete = resolve;
        }),
    );
    const writing = model.create();
    await vi.waitFor(() =>
      expect(port.writeMailboxSetup).toHaveBeenCalledOnce(),
    );
    resetOwnerRequests();
    complete({ operation: "CREATE", mailbox: mailbox() });
    await writing;
    expect(model.createdRef.value).toBe("");
    expect(model.items.value).toEqual([]);
    expect(model.sender.value).toBe("");
    expect(model.pending.value).toBeUndefined();
  });
  it("повторяющийся cursor не добавляет чужой снимок к прежней странице", async () => {
    const { model, port } = fixture();
    port.loadMyMailboxes.mockResolvedValueOnce({
      items: [mailbox()],
      nextPageToken: "same-cursor",
    });
    await model.load();
    port.loadMyMailboxes.mockResolvedValueOnce({
      items: [
        {
          ...mailbox(),
          ownership: {
            ...mailbox().ownership,
            connectionRef: "int_mailbox_second",
          },
        },
      ],
      nextPageToken: "same-cursor",
    });
    await model.load(true);
    expect(model.problem.value).toBeDefined();
    expect(model.items.value).toHaveLength(1);
  });
});
