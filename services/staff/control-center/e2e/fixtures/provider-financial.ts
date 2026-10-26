import type {
  ProviderAccount,
  ProviderFinancialOverview,
} from "../../src/shared/api/generated/openapi/types.gen";

// Эти fixtures не получают наблюдения провайдера: отсутствие лимита остаётся unknown.
export function syntheticProviderFinancialOverview(
  accounts: readonly ProviderAccount[],
  now = Date.now(),
): ProviderFinancialOverview {
  const visible = accounts
    .filter((account) => account.state !== "DELETED")
    .map(({ ref, name }) => ({ ref, name }))
    .sort((left, right) =>
      left.ref < right.ref ? -1 : left.ref > right.ref ? 1 : 0,
    );
  return {
    accounts: visible.slice(0, 3),
    total: visible.length,
    exhausted: 0,
    unknown: visible.length,
    observedAt: new Date(now).toISOString(),
    validUntil: new Date(now + 300_000).toISOString(),
  };
}
