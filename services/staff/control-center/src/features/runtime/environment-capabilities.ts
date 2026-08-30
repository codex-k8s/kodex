import type {
  RuntimeEnvironmentInput,
  RuntimeEnvironmentSet,
  RuntimeSecretDescriptor,
} from "@/shared/api/generated/openapi/types.gen";

import {
  hasEffectivePolicyDigests,
  validateEnvironmentInput,
  type EnvironmentFormProblem,
} from "@/features/runtime/environment-form";

export type EnvironmentCapabilityState = "AVAILABLE" | "UNAVAILABLE";

export interface EnvironmentCapability {
  key:
    | "versioning"
    | "search"
    | "values"
    | "secretReferences"
    | "imageBinding"
    | "verifiedTools"
    | "resources"
    | "networkPolicy"
    | "kubernetesRbac"
    | "effectivePolicy"
    | "secretLifecycle"
    | "secretReveal"
    | "serverReadiness";
  state: EnvironmentCapabilityState;
}

/**
 * Возможности определяются только фактическим generated API. Компоненты не
 * должны хранить неподдержанные поля в локальных черновиках или показывать
 * ложный успешный результат.
 */
export const runtimeEnvironmentCapabilities: readonly EnvironmentCapability[] =
  [
    { key: "versioning", state: "AVAILABLE" },
    { key: "search", state: "AVAILABLE" },
    { key: "values", state: "AVAILABLE" },
    { key: "secretReferences", state: "AVAILABLE" },
    { key: "imageBinding", state: "AVAILABLE" },
    { key: "verifiedTools", state: "AVAILABLE" },
    { key: "resources", state: "AVAILABLE" },
    { key: "networkPolicy", state: "AVAILABLE" },
    { key: "kubernetesRbac", state: "AVAILABLE" },
    { key: "effectivePolicy", state: "AVAILABLE" },
    { key: "secretLifecycle", state: "UNAVAILABLE" },
    { key: "secretReveal", state: "UNAVAILABLE" },
    { key: "serverReadiness", state: "UNAVAILABLE" },
  ];

export interface EnvironmentReadinessCheck {
  key:
    | "FORM"
    | "IMAGE"
    | "TOOLS"
    | "POLICY"
    | "REVISION"
    | "EFFECTIVE_POLICY"
    | "SECRET_REFS"
    | "SERVER_READINESS";
  state: "READY" | "NEEDS_ATTENTION" | "UNAVAILABLE";
  problems: EnvironmentFormProblem[];
}

export function environmentReadiness(
  input: RuntimeEnvironmentInput,
  environment?: RuntimeEnvironmentSet,
): EnvironmentReadinessCheck[] {
  const problems = validateEnvironmentInput(input);
  const secretProblems = problems.filter((problem) =>
    problem.field.startsWith("secretBindings."),
  );
  const imageProblems = problems.filter(
    (problem) => problem.field === "imageArtifactRef",
  );
  const toolProblems = problems.filter((problem) =>
    problem.field.startsWith("tools."),
  );
  const policyProblems = problems.filter((problem) =>
    problem.field.startsWith("policy."),
  );
  const formProblems = problems.filter(
    (problem) =>
      !problem.field.startsWith("secretBindings.") &&
      problem.field !== "imageArtifactRef" &&
      !problem.field.startsWith("tools.") &&
      !problem.field.startsWith("policy."),
  );
  const revisionReady =
    environment !== undefined &&
    environment.currentVersion.digest.length === 64 &&
    environment.currentVersion.revision > 0;
  const effectivePolicyReady =
    environment !== undefined &&
    hasEffectivePolicyDigests(environment.currentVersion.policy);

  return [
    {
      key: "FORM",
      state: formProblems.length ? "NEEDS_ATTENTION" : "READY",
      problems: formProblems,
    },
    {
      key: "SECRET_REFS",
      state: secretProblems.length ? "NEEDS_ATTENTION" : "READY",
      problems: secretProblems,
    },
    {
      key: "IMAGE",
      state: imageProblems.length ? "NEEDS_ATTENTION" : "READY",
      problems: imageProblems,
    },
    {
      key: "TOOLS",
      state: toolProblems.length ? "NEEDS_ATTENTION" : "READY",
      problems: toolProblems,
    },
    {
      key: "POLICY",
      state: policyProblems.length ? "NEEDS_ATTENTION" : "READY",
      problems: policyProblems,
    },
    {
      key: "REVISION",
      state: revisionReady ? "READY" : "NEEDS_ATTENTION",
      problems: [],
    },
    {
      key: "EFFECTIVE_POLICY",
      state: effectivePolicyReady ? "READY" : "NEEDS_ATTENTION",
      problems: [],
    },
    {
      key: "SERVER_READINESS",
      state: "UNAVAILABLE",
      problems: [],
    },
  ];
}

export interface SafeSecretReference {
  name: string;
  target: string;
  revision: string;
  uidHint: string;
  digestHint: string;
}

export function safeSecretReference(
  descriptor: RuntimeSecretDescriptor,
): SafeSecretReference {
  return {
    name: descriptor.name,
    target: [descriptor.secretName, descriptor.secretKey]
      .filter(Boolean)
      .join(" / "),
    revision: descriptor.secretResourceVersion,
    uidHint: compactIdentifier(descriptor.secretUid),
    digestHint: compactIdentifier(descriptor.contentSha256),
  };
}

export function compactIdentifier(value: string): string {
  const normalized = value.trim();
  if (normalized.length <= 18) return normalized || "—";
  return `${normalized.slice(0, 8)}…${normalized.slice(-8)}`;
}
