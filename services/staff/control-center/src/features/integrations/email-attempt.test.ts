import { describe, expect, it } from "vitest";
import type { EmailEffectReceipt } from "@/shared/api/generated/openapi/types.gen";
import {
  emailAttemptStorageKey,
  EmailAttemptMismatch,
  forgetEmailAttempt,
  prepareEmailAttempt,
} from "./email-attempt";
const receipt: EmailEffectReceipt = {
  ref: "receipt_synthetic",
  version: 7,
  invocationRef: "invocation_synthetic",
  externalReceiptDigest: "a".repeat(64),
  semanticInputDigest: "b".repeat(64),
  outcome: "UNKNOWN_OUTCOME",
  mailboxRef: "mailbox_synthetic",
  configurationRevision: 4,
  connectionRef: "connection_synthetic",
  projectRef: "project_synthetic",
  createdAt: "2026-09-05T00:00:00Z",
  updatedAt: "2026-09-05T00:00:00Z",
};
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
describe("Email reconciliation attempt", () => {
  it("сохраняет прежний idempotency key после нового SSO без хранения примечания", async () => {
    const store = storage();
    const first = await prepareEmailAttempt(
      receipt,
      "NO_EFFECT_CONFIRMED",
      "Synthetic private note",
      store,
    );
    expect(
      await prepareEmailAttempt(
        receipt,
        "NO_EFFECT_CONFIRMED",
        "Synthetic private note",
        store,
      ),
    ).toBe(first);
    expect(store.getItem(emailAttemptStorageKey)).not.toContain(
      "Synthetic private note",
    );
    await expect(
      prepareEmailAttempt(
        receipt,
        "EFFECT_CONFIRMED",
        "Synthetic private note",
        store,
      ),
    ).rejects.toBeInstanceOf(EmailAttemptMismatch);
    await expect(
      prepareEmailAttempt(receipt, "NO_EFFECT_CONFIRMED", "changed", store),
    ).rejects.toBeInstanceOf(EmailAttemptMismatch);
    await expect(
      prepareEmailAttempt(
        { ...receipt, version: 8 },
        "NO_EFFECT_CONFIRMED",
        "Synthetic private note",
        store,
      ),
    ).rejects.toBeInstanceOf(EmailAttemptMismatch);
    forgetEmailAttempt(receipt.ref, store);
    expect(store.getItem(emailAttemptStorageKey)).toBeNull();
  });
  it("закрыто отклоняет повреждённые durable metadata", async () => {
    const store = storage();
    store.setItem(emailAttemptStorageKey, '[{"note":"forbidden"}]');
    await expect(
      prepareEmailAttempt(receipt, "NO_EFFECT_CONFIRMED", "", store),
    ).rejects.toThrow();
  });
});
