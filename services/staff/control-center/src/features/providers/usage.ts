import type {
  ProviderAccount,
  ProviderAccountUsageContext,
} from "@/shared/api/generated/openapi/types.gen";

export function usageContextKey(context?: ProviderAccountUsageContext): string {
  return JSON.stringify([
    context?.purpose ?? "",
    context?.agentRef ?? "",
    context?.runtimeProfileRef ?? "",
    context?.providerDefinitionKey ?? "",
    context?.model ?? "",
    context?.reasoningEffort ?? "",
  ]);
}

export function usageQuery(context?: ProviderAccountUsageContext) {
  return context
    ? {
        usagePurpose: context.purpose,
        usageAgentRef: context.agentRef,
        usageRuntimeProfileRef: context.runtimeProfileRef,
        usageProviderDefinitionKey: context.providerDefinitionKey,
        usageModel: context.model,
        usageReasoningEffort: context.reasoningEffort,
      }
    : {};
}

export function hasCurrentUsage(
  account: ProviderAccount,
  context: ProviderAccountUsageContext | undefined,
  now = Date.now(),
): boolean {
  const usage = account.usage;
  return !!(
    usage &&
    usage.accountVersion === account.version &&
    usageContextKey(usage.context) === usageContextKey(context) &&
    Date.parse(usage.expiresAt) > now &&
    Number.isFinite(Date.parse(usage.observedAt)) &&
    /^[a-f0-9]{64}$/.test(usage.contextDigest)
  );
}

export function providerBlockerMessage(code: string): string {
  switch (code) {
    case "PROVIDER_DISABLED":
      return "providerUsage.reasons.PROVIDER_DISABLED";
    case "PROVIDER_ACCOUNT_DISABLED":
      return "providerUsage.reasons.ACCOUNT_DISABLED";
    case "PROVIDER_ACCOUNT_REVOKED":
      return "providerUsage.reasons.ACCOUNT_REVOKED";
    case "PROVIDER_ACCOUNT_REAUTHORIZATION_REQUIRED":
    case "PROVIDER_ACCOUNT_AUTHORIZATION_PENDING":
      return "providerUsage.reasons.AUTHORIZATION_REQUIRED";
    case "PROVIDER_ACCOUNT_CREDENTIAL_MISSING":
      return "providerUsage.reasons.CREDENTIAL_MISSING";
    case "MODEL_CATALOG_PENDING":
      return "providerUsage.reasons.CATALOG_PENDING";
    case "MODEL_CATALOG_EXPIRED":
      return "providerUsage.reasons.CATALOG_EXPIRED";
    case "MODEL_CATALOG_UNAVAILABLE":
      return "providerUsage.reasons.CATALOG_UNAVAILABLE";
    case "MODEL_CATALOG_UNVERIFIED_SOURCE":
      return "providerUsage.reasons.CATALOG_UNVERIFIED_SOURCE";
    case "MODEL_CATALOG_AUTHORIZATION_REJECTED":
      return "providerUsage.reasons.CATALOG_AUTHORIZATION_REJECTED";
    default:
      return "providers.modelUnavailable";
  }
}
