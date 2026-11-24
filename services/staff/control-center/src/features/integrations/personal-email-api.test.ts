import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  EmailMailboxOwnership,
  MyEmailMailbox,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  listMyEmailMailboxes: vi.fn(),
  createPersonalEmailMailboxConnection: vi.fn(),
  getEmailMailboxOwnership: vi.fn(),
  configureEmailMailboxOwnership: vi.fn(),
  revokeEmailMailboxOwnership: vi.fn(),
  getPersonalEmailMailboxMutationReceipt: vi.fn(),
  commandIntegrationConnection: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  checkedMailboxOwnership,
  checkedMyMailbox,
  loadMyMailboxes,
  writeMailboxSetup,
  recoverMailboxSetup,
} from "./personal-email-api";
const signal = new AbortController().signal,
  key = "12345678-1234-1234-1234-123456789abc";
const owner: EmailMailboxOwnership = {
  connectionRef: "int_mailbox_owner",
  connectionVersion: 1,
  deliveryKind: "MAILBOX_CONFIGURATION",
  policy: "INITIATOR_REQUIRED",
  state: "ACTIVE",
  generation: 1,
  ownerEligible: true,
};
const mailbox: MyEmailMailbox = {
  ownership: owner,
  name: "Моя почта",
  enabled: true,
  available: false,
};
const reply = (data: unknown) => ({
  data,
  response: new Response(null, { status: 200 }),
});
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"c".repeat(43)}` });
});
afterEach(() => vi.unstubAllGlobals());
describe("отдельная mailbox API boundary", () => {
  it("успешный Create принимает реальный enabled+unconfigured contract и160Unicode name", async () => {
    const name = "я".repeat(160),
      attempt = {
        operation: "CREATE" as const,
        key,
        connectionRef: "",
        connectionVersion: 0,
      };
    sdk.createPersonalEmailMailboxConnection.mockResolvedValue(
      reply({ ...mailbox, name }),
    );
    expect(
      (
        await writeMailboxSetup(
          attempt,
          { name, sender: "owner@example.test" },
          signal,
        )
      ).mailbox?.enabled,
    ).toBe(true);
    sdk.createPersonalEmailMailboxConnection.mockResolvedValue(
      reply({ ...mailbox, name, enabled: false }),
    );
    await expect(
      writeMailboxSetup(
        attempt,
        { name, sender: "owner@example.test" },
        signal,
      ),
    ).rejects.toThrow();
  });
  it.each([
    { ownerEligible: undefined },
    { deliveryKind: "TOKEN" },
    { state: "REVOKED", ownerEligible: true },
    { generation: 0 },
    { connectionVersion: 2 ** 53 },
    { serviceSubjectRef: "subject_foreign" },
    { secretRef: "private" },
  ])("отклоняет неизвестную/несогласованную owner metadata %j", (change) => {
    expect(() => checkedMailboxOwnership({ ...owner, ...change })).toThrow();
  });
  it("ACTIVE с отключённым subject остаётся metadata без доступности", () => {
    expect(
      checkedMailboxOwnership({ ...owner, ownerEligible: false }).ownerEligible,
    ).toBe(false);
    expect(() =>
      checkedMyMailbox({
        ...mailbox,
        enabled: true,
        available: true,
        ownership: { ...owner, ownerEligible: false },
      }),
    ).toThrow();
    expect(() => checkedMailboxOwnership(owner, "int_foreign_owner")).toThrow();
  });
  it("catalog отвергает дубликаты/cursor без элементов, не достраивает отсутствующее поле", async () => {
    for (const value of [
      { items: [mailbox, mailbox] },
      { items: [], nextPageToken: "cursor" },
      { nextPageToken: "" },
    ]) {
      sdk.listMyEmailMailboxes.mockResolvedValue(reply(value));
      await expect(loadMyMailboxes("", undefined, signal)).rejects.toThrow();
    }
  });
  it("создаёт fixed personal setup без project/owner selector и не повторяет потерянный ACK", async () => {
    const attempt = {
        operation: "CREATE" as const,
        key,
        connectionRef: "",
        connectionVersion: 0,
      },
      body = { name: mailbox.name, sender: "owner@example.test" };
    sdk.createPersonalEmailMailboxConnection.mockRejectedValueOnce(
      new TypeError("Lost ACK"),
    );
    await expect(writeMailboxSetup(attempt, body, signal)).rejects.toThrow();
    expect(sdk.createPersonalEmailMailboxConnection).toHaveBeenCalledOnce();
    expect(sdk.createPersonalEmailMailboxConnection.mock.calls[0]?.[0]).toEqual(
      {
        body,
        signal,
        headers: { "Idempotency-Key": key, "X-CSRF-Token": "c".repeat(43) },
      },
    );
    sdk.getPersonalEmailMailboxMutationReceipt.mockResolvedValue(
      reply({ operation: "CREATE", mailbox }),
    );
    expect(await recoverMailboxSetup(attempt, signal)).toEqual({
      operation: "CREATE",
      mailbox,
    });
    expect(sdk.createPersonalEmailMailboxConnection).toHaveBeenCalledOnce();
    expect(sdk.getPersonalEmailMailboxMutationReceipt).toHaveBeenCalledWith(
      expect.objectContaining({
        query: { operation: "CREATE", idempotencyKey: key },
      }),
    );
  });
  it("REVOKE receipt связывает exact connection/version и не превращает исторический ACTIVE в текущую власть", async () => {
    const attempt = {
      operation: "REVOKE" as const,
      key,
      connectionRef: owner.connectionRef,
      connectionVersion: 1,
    };
    sdk.revokeEmailMailboxOwnership.mockResolvedValue(
      reply({
        ...owner,
        connectionVersion: 2,
        state: "REVOKED",
        generation: 2,
        ownerEligible: false,
      }),
    );
    const result = await writeMailboxSetup(attempt, undefined, signal);
    expect(result.ownership?.ownerEligible).toBe(false);
    sdk.getPersonalEmailMailboxMutationReceipt.mockResolvedValue(
      reply({ operation: "CONFIGURE", ownership: owner }),
    );
    await expect(recoverMailboxSetup(attempt, signal)).rejects.toThrow();
  });
});
