import type {
  IntegrationIntent,
  IntegrationApprovalConsentSelection,
} from "@/shared/api/generated/openapi/types.gen";
import { isRecord } from "@/shared/lib/exact-object";

export type ConsentPolicy = {
  maxDurationSeconds: number;
  maxEffects: number;
  allowedLifetimes: IntegrationApprovalConsentSelection["lifetime"][];
};

// Выбор появляется только из owner preview конкретного HUMAN_SCOPED intent.
export function integrationConsentPolicy(
  intent?: IntegrationIntent,
): ConsentPolicy | undefined {
  if (intent?.effectPreview.approvalPolicy !== "HUMAN_SCOPED") return undefined;
  const scope = intent.effectPreview.approvalScope;
  if (!isRecord(scope) || !isRecord(scope.consentPolicy)) return undefined;
  const policy = scope.consentPolicy;
  if (
    !Number.isSafeInteger(policy.maxDurationSeconds) ||
    typeof policy.maxDurationSeconds !== "number" ||
    policy.maxDurationSeconds < 1 ||
    policy.maxDurationSeconds > 604800 ||
    !Number.isSafeInteger(policy.maxEffects) ||
    typeof policy.maxEffects !== "number" ||
    policy.maxEffects < 1 ||
    policy.maxEffects > 10000 ||
    !Array.isArray(policy.allowedLifetimes) ||
    !policy.allowedLifetimes.length ||
    policy.allowedLifetimes.length > 2 ||
    new Set(policy.allowedLifetimes).size !== policy.allowedLifetimes.length ||
    policy.allowedLifetimes.some(
      (v) => v !== "ROOT_RUN" && v !== "INITIATOR_RESOURCE",
    )
  )
    return undefined;
  return {
    maxDurationSeconds: policy.maxDurationSeconds,
    maxEffects: policy.maxEffects,
    allowedLifetimes:
      policy.allowedLifetimes as ConsentPolicy["allowedLifetimes"],
  };
}

export function defaultIntegrationConsent(
  policy: ConsentPolicy,
): IntegrationApprovalConsentSelection {
  return {
    durationSeconds: Math.min(86400, policy.maxDurationSeconds),
    maxEffects: Math.min(100, policy.maxEffects),
    lifetime: policy.allowedLifetimes.includes("ROOT_RUN")
      ? "ROOT_RUN"
      : policy.allowedLifetimes[0]!,
  };
}

// Пустой выбор остаётся прежним серверным default; неверный явный выбор не отправляется.
export function validIntegrationConsent(
  intent: IntegrationIntent | undefined,
  value: IntegrationApprovalConsentSelection | undefined,
): boolean {
  if (!value) return true;
  const policy = integrationConsentPolicy(intent);
  return (
    !!policy &&
    Number.isSafeInteger(value.durationSeconds) &&
    value.durationSeconds >= 1 &&
    value.durationSeconds <= policy.maxDurationSeconds &&
    Number.isSafeInteger(value.maxEffects) &&
    value.maxEffects >= 1 &&
    value.maxEffects <= policy.maxEffects &&
    policy.allowedLifetimes.includes(value.lifetime)
  );
}
