import type {
  ProviderAccountUsage,
  ProviderAccountUsageContext,
} from "../shared/api/generated/openapi/types.gen";
import { catalogStatusFixture } from "./runtime-catalog-fixture";

export function providerUsageFixture(
  context?: ProviderAccountUsageContext,
  overrides: Partial<ProviderAccountUsage> = {},
): ProviderAccountUsage {
  const ready = {
    state: "READY",
    reason: "AVAILABLE",
    remediation: "NONE",
  } as const;
  const untested = {
    state: "NOT_EVALUATED",
    reason: "CONTEXT_REQUIRED",
    remediation: "SELECT_CONTEXT",
  } as const;
  return {
    context,
    accountVersion: 1,
    agentVersion: context ? 3 : 0,
    lifecycle: ready,
    credential: {
      state: "READY",
      reason: "CREDENTIAL_READY",
      remediation: "NONE",
    },
    providerHealth: {
      state: "UNKNOWN",
      reason: "PROVIDER_HEALTH_UNOBSERVED",
      remediation: "WAIT_FOR_OBSERVATION",
    },
    modelCompatibility: context?.model
      ? ready
      : context
        ? {
            state: "NOT_EVALUATED",
            reason: "MODEL_REQUIRED",
            remediation: "SELECT_MODEL",
          }
        : untested,
    capacity: {
      state: "BLOCKED",
      reason: "CAPACITY_EXHAUSTED",
      remediation: "WAIT_FOR_CAPACITY",
    },
    actorEligibility: context ? ready : untested,
    allowedToSubmit: !!context?.model,
    eligibleForSelection: !!context,
    operationalState: "UNKNOWN",
    maximumConcurrentExecutions: 1,
    activeExecutions: 1,
    catalogStatus: catalogStatusFixture,
    catalogRevision: `mcat_${"a".repeat(64)}`,
    catalogDigest: "a".repeat(64),
    contextDigest: "b".repeat(64),
    observedAt: "2026-09-05T00:00:00Z",
    expiresAt: "2099-01-01T00:00:00Z",
    providerHealthScope: "CREDENTIALED_CATALOG_REACHABILITY",
    ...overrides,
  };
}
