import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  EmailEffectReceiptView,
  EmailReconciliationDecision,
  ReconcileEmailEffectData,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  reconcileEmailEffect:
    vi.fn<
      (
        options: Omit<ReconcileEmailEffectData, "url">,
      ) => Promise<{ data: EmailReconciliationDecision; response: Response }>
    >(),
  getEmailEffectReceipt: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", async () => {
  const { unwrap } = await import("@/shared/api/problem");
  return {
    etag: (version: number) => `"${String(version)}"`,
    mutate: (
      request: (
        headers: Record<string, string>,
      ) => Parameters<typeof unwrap>[0],
      version: number,
      key: string,
    ) =>
      unwrap(
        request({
          "If-Match": `"${String(version)}"`,
          "Idempotency-Key": key,
          "X-CSRF-Token": "synthetic-csrf",
        }),
      ),
  };
});
import {
  checkEmailView,
  decideEmailEffect,
  validReconciliationNote,
} from "./email-effects";
const view: EmailEffectReceiptView = {
  receipt: {
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
  },
};
const decision: EmailReconciliationDecision = {
  ref: "decision_synthetic",
  version: 1,
  receiptRef: view.receipt.ref,
  receiptVersion: 7,
  receiptDigest: view.receipt.externalReceiptDigest,
  invocationRef: view.receipt.invocationRef,
  outcome: "NO_EFFECT_CONFIRMED",
  actorRef: "subject_synthetic",
  createdAt: "2026-09-05T00:01:00Z",
  expiresAt: "2026-09-05T00:02:00Z",
};
describe("Email effect reconciliation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });
  it("принимает историческое решение без восстановления его срока действия", () => {
    expect(
      checkEmailView(
        { ...view, decision },
        { ref: view.receipt.connectionRef },
        view.receipt.invocationRef,
      ).decision,
    ).toEqual(decision);
  });
  it("отклоняет чужую connection, invocation и несовпадающую ревизию квитанции", () => {
    expect(() =>
      checkEmailView(view, { ref: "other" }, view.receipt.invocationRef),
    ).toThrow();
    expect(() =>
      checkEmailView(view, { ref: view.receipt.connectionRef }, "other"),
    ).toThrow();
    expect(() =>
      checkEmailView(
        { ...view, decision: { ...decision, receiptVersion: 8 } },
        { ref: view.receipt.connectionRef },
        view.receipt.invocationRef,
      ),
    ).toThrow();
  });
  it("отправляет только явное решение с версией квитанции, принимает отдельную версию решения", async () => {
    sdk.reconcileEmailEffect.mockResolvedValue({
      data: decision,
      response: new Response(null, { headers: { ETag: '"1"' } }),
    });
    expect(
      await decideEmailEffect(
        view,
        "NO_EFFECT_CONFIRMED",
        "Synthetic note",
        new AbortController().signal,
        "00000000-0000-4000-8000-000000000001",
      ),
    ).toEqual(decision);
    expect(sdk.reconcileEmailEffect).toHaveBeenCalledOnce();
    expect(sdk.reconcileEmailEffect.mock.calls[0]?.[0]).toMatchObject({
      path: { receiptRef: view.receipt.ref },
      headers: {
        "If-Match": '"7"',
        "Idempotency-Key": "00000000-0000-4000-8000-000000000001",
      },
      body: {
        expectedReceiptDigest: view.receipt.externalReceiptDigest,
        outcome: "NO_EFFECT_CONFIRMED",
        note: "Synthetic note",
      },
    });
    expect(
      Object.keys(
        sdk.reconcileEmailEffect.mock.calls[0]?.[0].body ?? {},
      ).sort(),
    ).toEqual(["expectedReceiptDigest", "note", "outcome"]);
  });
  it("не повторяет неизвестный результат команды автоматически", async () => {
    sdk.reconcileEmailEffect.mockRejectedValue(
      new TypeError("Synthetic network failure"),
    );
    await expect(
      decideEmailEffect(
        view,
        "NO_EFFECT_CONFIRMED",
        "",
        new AbortController().signal,
        "00000000-0000-4000-8000-000000000001",
      ),
    ).rejects.toThrow();
    expect(sdk.reconcileEmailEffect).toHaveBeenCalledOnce();
  });
  it("не принимает повторное решение или решение для уже определённого исхода", async () => {
    await expect(
      decideEmailEffect(
        { ...view, decision },
        "NO_EFFECT_CONFIRMED",
        "",
        new AbortController().signal,
        "00000000-0000-4000-8000-000000000001",
      ),
    ).rejects.toThrow();
    await expect(
      decideEmailEffect(
        { receipt: { ...view.receipt, outcome: "EFFECT_CONFIRMED" } },
        "NO_EFFECT_CONFIRMED",
        "",
        new AbortController().signal,
        "00000000-0000-4000-8000-000000000001",
      ),
    ).rejects.toThrow();
    expect(sdk.reconcileEmailEffect).not.toHaveBeenCalled();
  });
  it("проверяет Unicode-лимит и NUL без обрезки примечания", () => {
    expect(validReconciliationNote("😀".repeat(2000))).toBe(true);
    expect(validReconciliationNote("😀".repeat(2001))).toBe(false);
    expect(validReconciliationNote("bad\0note")).toBe(false);
  });
});
