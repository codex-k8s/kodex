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
) {
  const eligible = accounts.filter(
    (account) => account.financialEligible === true,
  );
  const ranked = eligible
    .map((account) => {
      const summary = account.financialSummary;
      const received = summary ? Date.parse(summary.receivedAt) : Number.NaN;
      const stale =
        !live ||
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
  return {
    accounts: ranked.slice(0, 3),
    exhausted: ranked.filter((item) => item.remaining === 0).length,
    unknown: ranked.filter((item) => item.remaining === undefined).length,
    total: ranked.length,
    complete,
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
