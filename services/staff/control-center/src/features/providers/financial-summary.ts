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
  const authoritativeFresh =
    !authoritative ||
    (live &&
      Date.parse(authoritative.validUntil) > now &&
      Date.parse(authoritative.observedAt) <= now + 30_000);
  const eligible = accounts.filter(
    (account) => account.financialEligible === true,
  );
  const ranked = eligible
    .map((account) => {
      const summary = account.financialSummary;
      const received = summary ? Date.parse(summary.receivedAt) : Number.NaN;
      const stale =
        !live ||
        !authoritativeFresh ||
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
    exhausted: authoritative
      ? fresh
        ? authoritative.exhausted
        : 0
      : ranked.filter((item) => item.remaining === 0).length,
    unknown: authoritative
      ? fresh
        ? authoritative.unknown
        : authoritative.total
      : ranked.filter((item) => item.remaining === undefined).length,
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
export function validateFinancialOverview(
  value: unknown,
): ProviderFinancialOverview {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error(invalidFinancialOverview);
  const row = value as Record<string, unknown>;
  const fields = [
    "accounts",
    "total",
    "exhausted",
    "unknown",
    "observedAt",
    "validUntil",
  ];
  if (
    Object.keys(row).some((key) => !fields.includes(key)) ||
    fields.some((key) => !(key in row))
  )
    throw new Error(invalidFinancialOverview);
  const total = row.total as number,
    exhausted = row.exhausted as number,
    unknown = row.unknown as number;
  if (
    ![total, exhausted, unknown].every(
      (item) => Number.isSafeInteger(item) && item >= 0,
    ) ||
    exhausted + unknown > total ||
    !Array.isArray(row.accounts) ||
    row.accounts.length > 3 ||
    row.accounts.length !== Math.min(total, 3)
  )
    throw new Error(invalidFinancialOverview);
  const at = Date.parse(String(row.observedAt)),
    until = Date.parse(String(row.validUntil));
  if (
    !Number.isFinite(at) ||
    !Number.isFinite(until) ||
    until < at ||
    until - at > 300_000
  )
    throw new Error(invalidFinancialOverview);
  const refs = new Set<string>();
  for (const item of row.accounts) {
    if (
      !item ||
      typeof item !== "object" ||
      Array.isArray(item) ||
      Object.keys(item).some(
        (key) => !["ref", "name", "financialSummary"].includes(key),
      ) ||
      typeof item.ref !== "string" ||
      item.ref.length > 96 ||
      typeof item.name !== "string" ||
      item.name.length > 160 ||
      refs.has(item.ref)
    )
      throw new Error(invalidFinancialOverview);
    if (item.financialSummary !== undefined)
      validateFinancialMeasurement(item.financialSummary);
    refs.add(item.ref);
  }
  return value as ProviderFinancialOverview;
}

function validateFinancialMeasurement(value: unknown): void {
  const fail = (): never => {
    throw new Error(invalidFinancialOverview);
  };
  const object = (
    value: unknown,
    fields: string[],
  ): Record<string, unknown> => {
    if (
      !value ||
      typeof value !== "object" ||
      Array.isArray(value) ||
      Object.keys(value).some((key) => !fields.includes(key))
    )
      return fail();
    return value as Record<string, unknown>;
  };
  const integer = (value: unknown, min: number, max: number) =>
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= min &&
    value <= max;
  const summary = object(value, ["observation", "receivedAt", "version"]);
  if (
    !integer(summary.version, 1, Number.MAX_SAFE_INTEGER) ||
    typeof summary.receivedAt !== "string" ||
    !Number.isFinite(Date.parse(summary.receivedAt))
  )
    fail();
  const observation = object(summary.observation, [
    "sequence",
    "state",
    "windows",
    "credits",
    "ordinaryUsageAllowed",
    "spendControlReached",
    "restriction",
  ]);
  if (
    !integer(observation.sequence, 1, 64) ||
    !["AVAILABLE", "UNAVAILABLE"].includes(String(observation.state))
  )
    fail();
  for (const field of ["ordinaryUsageAllowed", "spendControlReached"])
    if (field in observation && typeof observation[field] !== "boolean") fail();
  if (observation.windows !== undefined) {
    if (!Array.isArray(observation.windows) || observation.windows.length > 2)
      return fail();
    for (const item of observation.windows) {
      const window = object(item, [
        "usedPercent",
        "durationMinutes",
        "resetsAt",
      ]);
      if (
        !integer(window.usedPercent, 0, 100) ||
        ("durationMinutes" in window &&
          !integer(window.durationMinutes, 1, 5256000)) ||
        ("resetsAt" in window && !integer(window.resetsAt, 0, 253402300799))
      )
        fail();
    }
  }
  if (observation.credits !== undefined) {
    const credits = object(observation.credits, [
      "hasCredits",
      "unlimited",
      "balance",
    ]);
    for (const field of ["hasCredits", "unlimited"])
      if (field in credits && typeof credits[field] !== "boolean") fail();
    if (
      "balance" in credits &&
      (typeof credits.balance !== "string" ||
        !/^[0-9]{1,64}(\.[0-9]{1,64})?$/.test(credits.balance))
    )
      fail();
  }
  if (
    observation.restriction !== undefined &&
    ![
      "",
      "rate_limit_reached",
      "workspace_owner_credits_depleted",
      "workspace_member_credits_depleted",
      "workspace_owner_usage_limit_reached",
      "workspace_member_usage_limit_reached",
    ].includes(String(observation.restriction))
  )
    fail();
  if (
    observation.state === "UNAVAILABLE" &&
    ((Array.isArray(observation.windows) && observation.windows.length > 0) ||
      observation.credits !== undefined ||
      observation.ordinaryUsageAllowed !== undefined ||
      observation.spendControlReached !== undefined ||
      observation.restriction !== undefined)
  )
    fail();
}
