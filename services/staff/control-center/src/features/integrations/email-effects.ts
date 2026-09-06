import { requestSignal } from "@/shared/api/client";
import {
  getEmailEffectReceipt,
  reconcileEmailEffect,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  EmailEffectReceipt,
  EmailEffectReceiptView,
  EmailReconciliationDecision,
  EmailReconciliationOutcome,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

const digestPattern = /^[a-f0-9]{64}$/;
const positiveVersion = (value: number) =>
  Number.isSafeInteger(value) && value > 0;
export function validReconciliationNote(note: string): boolean {
  return Array.from(note).length <= 2000 && !note.includes("\0");
}
export function checkEmailDecision(
  value: EmailReconciliationDecision | null | undefined,
  receipt: EmailEffectReceipt,
): EmailReconciliationDecision {
  if (
    !value ||
    !value.ref ||
    !positiveVersion(value.version) ||
    value.receiptRef !== receipt.ref ||
    value.receiptVersion !== receipt.version ||
    value.receiptDigest !== receipt.externalReceiptDigest ||
    value.invocationRef !== receipt.invocationRef ||
    !["EFFECT_CONFIRMED", "NO_EFFECT_CONFIRMED"].includes(value.outcome) ||
    !value.actorRef ||
    !Number.isFinite(Date.parse(value.createdAt)) ||
    !Number.isFinite(Date.parse(value.expiresAt))
  )
    throw new Error("Invalid email reconciliation decision");
  return value;
}
export function checkEmailView(
  value: EmailEffectReceiptView | null | undefined,
  connection: Pick<IntegrationConnection, "ref">,
  invocationRef: string,
): EmailEffectReceiptView {
  const receipt = value?.receipt;
  if (
    !receipt ||
    !receipt.ref ||
    !positiveVersion(receipt.version) ||
    receipt.invocationRef !== invocationRef ||
    receipt.connectionRef !== connection.ref ||
    !receipt.projectRef ||
    !positiveVersion(receipt.configurationRevision) ||
    !digestPattern.test(receipt.externalReceiptDigest) ||
    !digestPattern.test(receipt.semanticInputDigest) ||
    !["UNKNOWN_OUTCOME", "EFFECT_CONFIRMED", "NO_EFFECT_CONFIRMED"].includes(
      receipt.outcome,
    ) ||
    !Number.isFinite(Date.parse(receipt.createdAt)) ||
    !Number.isFinite(Date.parse(receipt.updatedAt))
  )
    throw new Error("Invalid email effect receipt");
  if (value.decision) checkEmailDecision(value.decision, receipt);
  return value;
}
export async function readEmailEffect(
  connection: IntegrationConnection,
  invocationRef: string,
  signal: AbortSignal,
): Promise<EmailEffectReceiptView> {
  const result = await unwrap(
    getEmailEffectReceipt({
      path: { invocationRef },
      signal: requestSignal(signal),
    }),
  );
  const view = checkEmailView(result.data, connection, invocationRef);
  if (result.etag !== etag(view.receipt.version))
    throw new Error("Email receipt version mismatch");
  return view;
}
export async function decideEmailEffect(
  view: EmailEffectReceiptView,
  outcome: EmailReconciliationOutcome,
  note: string,
  signal: AbortSignal,
  requestIdempotencyKey: string,
): Promise<EmailReconciliationDecision> {
  if (
    view.receipt.outcome !== "UNKNOWN_OUTCOME" ||
    view.decision ||
    !["EFFECT_CONFIRMED", "NO_EFFECT_CONFIRMED"].includes(outcome) ||
    !validReconciliationNote(note)
  )
    throw new Error("Email reconciliation is unavailable");
  const result = await mutate(
    (headers) =>
      reconcileEmailEffect({
        path: { receiptRef: view.receipt.ref },
        headers: { ...headers, "If-Match": etag(view.receipt.version) },
        body: {
          expectedReceiptDigest: view.receipt.externalReceiptDigest,
          outcome,
          ...(note ? { note } : {}),
        },
        signal: requestSignal(signal),
      }),
    view.receipt.version,
    requestIdempotencyKey,
  );
  const decision = checkEmailDecision(result.data, view.receipt);
  if (decision.outcome !== outcome || result.etag !== etag(decision.version))
    throw new Error("Email reconciliation readback mismatch");
  return decision;
}
