import type {
  EmailEffectReceipt,
  EmailReconciliationOutcome,
} from "@/shared/api/generated/openapi/types.gen";
export const emailAttemptStorageKey = "kodex.email.reconciliation-attempts";
interface Attempt {
  receiptRef: string;
  receiptVersion: number;
  receiptDigest: string;
  inputDigest: string;
  key: string;
  outcome: EmailReconciliationOutcome;
}
function entries(storage: Pick<Storage, "getItem">): Attempt[] {
  const raw = storage.getItem(emailAttemptStorageKey);
  if (!raw) return [];
  const parsed: unknown = JSON.parse(raw);
  if (
    !Array.isArray(parsed) ||
    parsed.length > 20 ||
    parsed.some((item: unknown) => {
      if (!item || typeof item !== "object") return true;
      const value = item as Partial<Attempt>;
      return (
        Object.keys(value).sort().join(",") !==
          "inputDigest,key,outcome,receiptDigest,receiptRef,receiptVersion" ||
        typeof value.receiptRef !== "string" ||
        !/^[A-Za-z0-9_-]{8,128}$/.test(value.receiptRef) ||
        typeof value.receiptVersion !== "number" ||
        !Number.isSafeInteger(value.receiptVersion) ||
        value.receiptVersion < 1 ||
        typeof value.receiptDigest !== "string" ||
        !/^[a-f0-9]{64}$/.test(value.receiptDigest) ||
        typeof value.inputDigest !== "string" ||
        !/^[a-f0-9]{64}$/.test(value.inputDigest) ||
        typeof value.key !== "string" ||
        !/^[0-9a-f-]{36}$/.test(value.key) ||
        !["EFFECT_CONFIRMED", "NO_EFFECT_CONFIRMED"].includes(
          value.outcome ?? "",
        )
      );
    })
  )
    throw new Error("Invalid email reconciliation attempt storage");
  const result = parsed as Attempt[];
  if (new Set(result.map((item) => item.receiptRef)).size !== result.length)
    throw new Error("Duplicate email reconciliation attempt");
  return result;
}
export class EmailAttemptMismatch extends Error {
  constructor() {
    super("Email reconciliation input differs from the pending attempt");
  }
}
export async function prepareEmailAttempt(
  receipt: EmailEffectReceipt,
  outcome: EmailReconciliationOutcome,
  note: string,
  storage: Pick<Storage, "getItem" | "setItem">,
): Promise<string> {
  const bytes = new TextEncoder().encode(
    JSON.stringify({
      expectedReceiptDigest: receipt.externalReceiptDigest,
      outcome,
      ...(note ? { note } : {}),
    }),
  );
  const inputDigest = Array.from(
    new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)),
    (byte) => byte.toString(16).padStart(2, "0"),
  ).join("");
  const values = entries(storage);
  const old = values.find((item) => item.receiptRef === receipt.ref);
  if (old) {
    if (
      old.receiptVersion !== receipt.version ||
      old.receiptDigest !== receipt.externalReceiptDigest ||
      old.inputDigest !== inputDigest ||
      old.outcome !== outcome
    )
      throw new EmailAttemptMismatch();
    return old.key;
  }
  if (values.length >= 20)
    throw new Error("Email reconciliation attempt limit reached");
  const key = crypto.randomUUID();
  storage.setItem(
    emailAttemptStorageKey,
    JSON.stringify([
      ...values,
      {
        receiptRef: receipt.ref,
        receiptVersion: receipt.version,
        receiptDigest: receipt.externalReceiptDigest,
        inputDigest,
        key,
        outcome,
      } satisfies Attempt,
    ]),
  );
  return key;
}
export function forgetEmailAttempt(
  receiptRef: string,
  storage: Pick<Storage, "getItem" | "setItem" | "removeItem">,
): void {
  const values = entries(storage).filter(
    (item) => item.receiptRef !== receiptRef,
  );
  if (values.length)
    storage.setItem(emailAttemptStorageKey, JSON.stringify(values));
  else storage.removeItem(emailAttemptStorageKey);
}
