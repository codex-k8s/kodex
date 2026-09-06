import { describe, expect, it } from "vitest";
import type {
  ProviderAccount,
  ProviderAccountUsageContext,
} from "@/shared/api/generated/openapi/types.gen";
import { providerUsageFixture } from "@/test-utils/provider-usage-fixture";
import { hasCurrentUsage, usageContextKey, usageQuery } from "./usage";

const context: ProviderAccountUsageContext = {
  purpose: "CONFIGURE",
  agentRef: "agent-one",
  runtimeProfileRef: "profile-one",
  providerDefinitionKey: "openai-codex",
  model: "model-one",
  reasoningEffort: "low",
};
function account(): ProviderAccount {
  return {
    ref: "account-one",
    version: 1,
    name: "Account",
    definitionKey: "openai-codex",
    enabled: true,
    ready: true,
    state: "AUTHORIZED",
    externalAccountMasked: "",
    nextActions: [],
    createdAt: "2026-09-05T00:00:00Z",
    updatedAt: "2026-09-05T00:00:00Z",
    usage: providerUsageFixture(context),
  };
}
describe("контекстная доступность provider account", () => {
  it("не подменяет допуск заполненной конфигурации ёмкостью или неизвестной health", () => {
    const item = account();
    expect(hasCurrentUsage(item, context)).toBe(true);
    expect(item.usage?.capacity.state).toBe("BLOCKED");
    expect(item.usage?.providerHealth.state).toBe("UNKNOWN");
    expect(item.usage?.allowedToSubmit).toBe(true);
  });
  it("различает выбор аккаунта до модели и отправку полной формы", () => {
    const selected = {
      ...context,
      model: undefined,
      reasoningEffort: undefined,
    };
    const usage = providerUsageFixture(selected);
    expect(usage.eligibleForSelection).toBe(true);
    expect(usage.allowedToSubmit).toBe(false);
    expect(usage.modelCompatibility.state).toBe("NOT_EVALUATED");
    expect(hasCurrentUsage({ ...account(), usage }, selected)).toBe(true);
  });
  it.each([
    { agentRef: "another-agent" },
    { runtimeProfileRef: "another-profile" },
    { model: "another-model" },
    { reasoningEffort: "high" },
    { providerDefinitionKey: "another-provider" },
    { purpose: "LAUNCH" as const },
  ])("не принимает ответ предыдущего контекста %j", (changed) => {
    expect(hasCurrentUsage(account(), { ...context, ...changed })).toBe(false);
    expect(usageContextKey(context)).not.toBe(
      usageContextKey({ ...context, ...changed }),
    );
  });
  it("отклоняет истёкшую/чужую версию и отсутствие usage, независимо от legacy ready", () => {
    expect(hasCurrentUsage({ ...account(), version: 2 }, context)).toBe(false);
    expect(hasCurrentUsage({ ...account(), usage: undefined }, context)).toBe(
      false,
    );
    expect(
      hasCurrentUsage(
        {
          ...account(),
          usage: providerUsageFixture(context, {
            expiresAt: "2020-01-01T00:00:00Z",
          }),
        },
        context,
      ),
    ).toBe(false);
  });
  it("административное чтение не получает candidate context или ложную actor readiness", () => {
    expect(usageQuery()).toEqual({});
    const usage = providerUsageFixture();
    expect(usage.actorEligibility.state).toBe("NOT_EVALUATED");
    expect(hasCurrentUsage({ ...account(), usage }, undefined)).toBe(true);
    expect(hasCurrentUsage({ ...account(), usage }, context)).toBe(false);
    expect(usageQuery(context)).toMatchObject({
      usagePurpose: "CONFIGURE",
      usageAgentRef: "agent-one",
      usageRuntimeProfileRef: "profile-one",
    });
  });
});
