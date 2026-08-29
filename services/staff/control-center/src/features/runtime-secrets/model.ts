export type RuntimeSecretValueType = "STRING" | "BINARY" | "JSON";
export type RuntimeSecretState = "ACTIVE" | "REVOKED";

export interface RuntimeSecretDisplayHint {
  prefix: string;
  suffix: string;
}

export interface RuntimeSecret {
  ref: string;
  version: number;
  projectRef: string;
  name: string;
  description: string;
  valueType: RuntimeSecretValueType;
  state: RuntimeSecretState;
  currentRevision: number;
  displayHint?: RuntimeSecretDisplayHint;
  createdAt: string;
  updatedAt: string;
}

export interface RuntimeSecretPage {
  items: RuntimeSecret[];
  nextPageToken: string;
}

export interface RuntimeSecretCreateInput {
  name: string;
  description: string;
  valueType: RuntimeSecretValueType;
  value: string;
}

export interface RuntimeSecretRotateInput {
  valueType: RuntimeSecretValueType;
  value: string;
}

export interface RuntimeSecretReveal {
  value: string;
  valueType: RuntimeSecretValueType;
}

const mask = "••••••";

export function maskedSecretHint(secret: RuntimeSecret): string {
  const prefix = (secret.displayHint?.prefix ?? "").slice(0, 6);
  const suffix = (secret.displayHint?.suffix ?? "").slice(-6);
  return `${prefix}${mask}${suffix}`;
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
