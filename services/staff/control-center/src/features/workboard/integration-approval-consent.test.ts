import { describe, it, expect } from "vitest";
import {
  integrationConsentPolicy,
  defaultIntegrationConsent,
  validIntegrationConsent,
} from "./integration-approval-consent";
import type { IntegrationIntent } from "@/shared/api/generated/openapi/types.gen";
const intent = (policy: unknown, approvalPolicy = "HUMAN_SCOPED") =>
  ({
    effectPreview: { approvalPolicy, approvalScope: { consentPolicy: policy } },
  }) as unknown as IntegrationIntent;
const bounded = {
  maxDurationSeconds: 604800,
  maxEffects: 10000,
  allowedLifetimes: ["ROOT_RUN", "INITIATOR_RESOURCE"],
};
describe("выбор ограниченного подтверждения", () => {
  it("оставляет default в исходном запуске и допускает явные сутки для объекта", () => {
    const i = intent(bounded);
    const p = integrationConsentPolicy(i)!;
    expect(defaultIntegrationConsent(p)).toEqual({
      durationSeconds: 86400,
      maxEffects: 100,
      lifetime: "ROOT_RUN",
    });
    expect(
      validIntegrationConsent(i, {
        durationSeconds: 86400,
        maxEffects: 20,
        lifetime: "INITIATOR_RESOURCE",
      }),
    ).toBe(true);
  });
  it("сужает default до policy bounds", () =>
    expect(
      defaultIntegrationConsent({
        maxDurationSeconds: 600,
        maxEffects: 3,
        allowedLifetimes: ["ROOT_RUN"],
      }),
    ).toEqual({ durationSeconds: 600, maxEffects: 3, lifetime: "ROOT_RUN" }));
  it.each(["NONE", "HUMAN_EACH_EFFECT"])("не выдаёт выбор для %s", (p) => {
    expect(integrationConsentPolicy(intent(bounded, p))).toBeUndefined();
    expect(
      validIntegrationConsent(intent(bounded, p), {
        durationSeconds: 1,
        maxEffects: 1,
        lifetime: "ROOT_RUN",
      }),
    ).toBe(false);
  });
  it.each([
    { ...bounded, maxEffects: 0 },
    { ...bounded, maxDurationSeconds: 604801 },
    { ...bounded, allowedLifetimes: ["ALL_USERS"] },
    { ...bounded, allowedLifetimes: ["ROOT_RUN", "ROOT_RUN"] },
  ])("отвергает неверную owner metadata", (p) =>
    expect(integrationConsentPolicy(intent(p))).toBeUndefined(),
  );
  it.each([
    { durationSeconds: 0, maxEffects: 1, lifetime: "ROOT_RUN" },
    { durationSeconds: 1.5, maxEffects: 1, lifetime: "ROOT_RUN" },
    { durationSeconds: 604801, maxEffects: 1, lifetime: "ROOT_RUN" },
    { durationSeconds: 1, maxEffects: 10001, lifetime: "ROOT_RUN" },
  ])("не отправляет неверный явный выбор", (v) =>
    expect(validIntegrationConsent(intent(bounded), v as never)).toBe(false),
  );
});
