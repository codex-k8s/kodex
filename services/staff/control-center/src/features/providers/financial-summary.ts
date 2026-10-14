import type { ProviderFinancialOverview } from "@/shared/api/generated/openapi/types.gen";
import type { ProviderAccount } from "./model";

export type FinancialAccount = Pick<
  ProviderAccount,
  "ref" | "name" | "financialEligible" | "financialSummary"
>;

// Источник — только полный WS snapshot после owner eligibility; unknown не ноль.
export function financialSummary(
  accounts: readonly FinancialAccount[],
  live: boolean,
  complete: boolean,
  now = Date.now(),
  authoritative?: ProviderFinancialOverview,
) {
  const authoritativeFresh = !authoritative || (live && Date.parse(authoritative.validUntil) > now && Date.parse(authoritative.observedAt) <= now + 30_000);
  const eligible = accounts.filter(
    (account) => account.financialEligible === true,
  );
  const ranked = eligible
    .map((account) => {
      const summary = account.financialSummary;
      const received = summary ? Date.parse(summary.receivedAt) : Number.NaN;
      const stale =
        !live || !authoritativeFresh ||
        !Number.isFinite(received) ||
        now - received > 300_000 ||
        received > now + 30_000;
      const observation = summary?.observation;
      const available = observation?.state === "AVAILABLE" && !stale;
      const windows = available
        ? (observation.windows ?? []).filter(
            (window) =>
              Number.isInteger(window.usedPercent) &&
              window.usedPercent >= 0 &&
              window.usedPercent <= 100,
          )
        : [];
      const remaining = windows.length
        ? Math.min(...windows.map((window) => 100 - window.usedPercent))
        : undefined;
      return { account, observation, stale, available, remaining };
    })
    .sort(
      (left, right) =>
        (left.remaining ?? Infinity) - (right.remaining ?? Infinity) ||
        left.account.ref.localeCompare(right.account.ref),
    );
  const fresh = authoritativeFresh;
  return {
    accounts: ranked.slice(0, 3),
    exhausted: authoritative ? (fresh ? authoritative.exhausted : 0) : ranked.filter((item) => item.remaining === 0).length,
    unknown: authoritative ? (fresh ? authoritative.unknown : authoritative.total) : ranked.filter((item) => item.remaining === undefined).length,
    total: authoritative?.total ?? ranked.length,
    complete: complete && fresh,
  };
}

export function financialCredits(
  account: FinancialAccount,
  available: boolean,
): string {
  const credits = available
    ? account.financialSummary?.observation.credits
    : undefined;
  if (!credits) return "Кредиты: недоступно";
  if (credits.unlimited) return "Кредиты: без ограничения";
  if (credits.balance !== undefined) return `Кредиты: ${credits.balance}`;
  if (credits.hasCredits === undefined) return "Кредиты: недоступно";
  return credits.hasCredits
    ? "Кредиты: есть; баланс недоступен"
    : "Кредиты: нет; баланс недоступен";
}

export function financialReset(
  seconds: number | undefined,
): string | undefined {
  if (
    seconds === undefined ||
    !Number.isSafeInteger(seconds) ||
    seconds < 0 ||
    seconds > 253402300799
  )
    return undefined;
  return new Date(seconds * 1000).toLocaleString("ru-RU");
}

const invalidFinancialOverview = "provider financial overview is invalid";
export function validateFinancialOverview(value: unknown): ProviderFinancialOverview {
 if (!value || typeof value!=="object" || Array.isArray(value)) throw new Error(invalidFinancialOverview);
 const row=value as Record<string,unknown>;
 const fields=["accounts","total","exhausted","unknown","observedAt","validUntil"];
 if (Object.keys(row).some(key=>!fields.includes(key)) || fields.some(key=>!(key in row))) throw new Error(invalidFinancialOverview);
 const total=row.total as number, exhausted=row.exhausted as number, unknown=row.unknown as number;
 if (![total,exhausted,unknown].every(item=>Number.isSafeInteger(item)&&item>=0) || exhausted+unknown>total || !Array.isArray(row.accounts) || row.accounts.length>3 || row.accounts.length!==Math.min(total,3)) throw new Error(invalidFinancialOverview);
 const at=Date.parse(String(row.observedAt)),until=Date.parse(String(row.validUntil));
 if (!Number.isFinite(at)||!Number.isFinite(until)||until<at||until-at>300_000) throw new Error(invalidFinancialOverview);
 const refs=new Set<string>();
 for (const item of row.accounts) {
  if (!item || typeof item!=="object" || Array.isArray(item) || Object.keys(item).some(key=>!["ref","name","financialSummary"].includes(key)) || typeof item.ref!=="string" || item.ref.length>96 || typeof item.name!=="string" || item.name.length>160 || refs.has(item.ref)) throw new Error(invalidFinancialOverview);
  refs.add(item.ref);
 }
 return value as ProviderFinancialOverview;
}
