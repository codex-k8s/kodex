import type {
  RuntimeSecret,
  RuntimeSecretCreateInput,
  RuntimeSecretDisplayHint,
  RuntimeSecretPage,
  RuntimeSecretReveal,
  RuntimeSecretRotateInput,
  RuntimeSecretValueType,
} from "@/shared/api/generated/openapi/types.gen";

export type {
  RuntimeSecret,
  RuntimeSecretCreateInput,
  RuntimeSecretDisplayHint,
  RuntimeSecretPage,
  RuntimeSecretReveal,
  RuntimeSecretRotateInput,
  RuntimeSecretValueType,
};

export type RuntimeSecretState = RuntimeSecret["state"];
export type RuntimeSecretAction = "ROTATE" | "REVOKE" | "REVEAL";

const mask = "••••••";

export function maskedSecretHint(secret: RuntimeSecret): string {
  const prefix = (secret.displayHint?.prefix ?? "").slice(0, 6);
  const suffix = (secret.displayHint?.suffix ?? "").slice(-6);
  return `${prefix}${mask}${suffix}`;
}

export function canRuntimeSecretAction(
  secret: Pick<RuntimeSecret, "nextActions" | "state">,
  action: RuntimeSecretAction,
): boolean {
  return secret.state === "ACTIVE" && secret.nextActions.includes(action);
}

export function validateSecretValue(
  valueType: RuntimeSecretValueType,
  value: string,
): "required" | "invalid-json" | undefined {
  if (!value) return "required";
  if (valueType !== "JSON") return undefined;
  try {
    JSON.parse(value);
    return undefined;
  } catch {
    return "invalid-json";
  }
}
