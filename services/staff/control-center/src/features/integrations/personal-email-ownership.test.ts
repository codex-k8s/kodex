import { effectScope, ref, type EffectScope } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useEmailMailboxOwnership } from "./personal-email-ownership";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import type {
  EmailMailboxOwnership,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";

const scopes: EffectScope[] = [];
afterEach(() => {
  for (const scope of scopes) scope.stop();
  scopes.length = 0;
  resetOwnerRequests();
});
const connection: IntegrationConnection = {
  ref: "int_mailbox_owner",
  version: 3,
  definitionKey: "email",
  credentialPolicy: "INITIATOR_REQUIRED",
  name: "Мой ящик",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "",
  capabilities: [],
  grants: [],
  nextActions: [],
  definitionVersion: "1.5.0",
  definitionDigest: "a".repeat(64),
  publicConfiguration: {},
};
const owner: EmailMailboxOwnership = {
  connectionRef: connection.ref,
  connectionVersion: 3,
  deliveryKind: "MAILBOX_CONFIGURATION",
  policy: "INITIATOR_REQUIRED",
  state: "ACTIVE",
  generation: 1,
  ownerEligible: true,
};
function cache() {
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
describe("mailbox owner UI: свежая eligibility", () => {
  it("ACTIVE без eligibility не открывает health и не подменяется generic token action", async () => {
    const selected = ref({ ...connection }),
      observed = vi.fn(),
      port = {
        readMailboxOwnership: vi
          .fn()
          .mockResolvedValue({ ...owner, ownerEligible: false }),
        writeMailboxSetup: vi.fn(),
        recoverMailboxSetup: vi.fn(),
        checkMailboxHealth: vi.fn(),
      };
    const scope = effectScope();
    scopes.push(scope);
    const model = scope.run(() =>
      useEmailMailboxOwnership(
        () => selected.value,
        observed,
        vi.fn(),
        port,
        cache(),
      ),
    );
    if (!model) throw new Error("Fixture scope unavailable");
    await vi.waitFor(() => expect(model.loading.value).toBe(false));
    expect(model.ownership.value?.state).toBe("ACTIVE");
    expect(model.canTest.value).toBe(false);
    await model.test();
    expect(port.checkMailboxHealth).not.toHaveBeenCalled();
    expect(model.canConfigure.value).toBe(false);
    expect(observed.mock.lastCall?.[0].ownerEligible).toBe(false);
  });
  it("поздний owner read старой version не восстанавливает редактор текущей версии", async () => {
    const selected = ref({ ...connection }),
      observed = vi.fn();
    let oldRead!: (value: EmailMailboxOwnership) => void;
    const port = {
      readMailboxOwnership: vi
        .fn()
        .mockImplementationOnce(
          () =>
            new Promise<EmailMailboxOwnership>((resolve) => {
              oldRead = resolve;
            }),
        )
        .mockResolvedValue({
          ...owner,
          connectionVersion: 4,
          ownerEligible: false,
        }),
      writeMailboxSetup: vi.fn(),
      recoverMailboxSetup: vi.fn(),
      checkMailboxHealth: vi.fn(),
    };
    const scope = effectScope();
    scopes.push(scope);
    const model = scope.run(() =>
      useEmailMailboxOwnership(
        () => selected.value,
        observed,
        vi.fn(),
        port,
        cache(),
      ),
    );
    if (!model) throw new Error("Fixture scope unavailable");
    selected.value = { ...selected.value, version: 4 };
    await vi.waitFor(() => expect(model.loading.value).toBe(false));
    oldRead(owner);
    await Promise.resolve();
    expect(model.ownership.value?.connectionVersion).toBe(4);
    expect(model.ownership.value?.ownerEligible).toBe(false);
    expect(model.canTest.value).toBe(false);
    expect(
      observed.mock.calls.filter(([value]) => value?.ownerEligible === true),
    ).toEqual([]);
  });
});
