import type {
  RuntimeSecret,
  RuntimeSecretCreateInput,
  RuntimeSecretDisplayHint,
  RuntimeSecretPage,
  RuntimeSecretReveal,
  RuntimeSecretRotateInput,
  RuntimeSecretValueType,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";

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
): "required" | "invalid-json" | "invalid-base64" | undefined {
  if (!value) return "required";
  if (valueType === "BINARY") {
    try {
      if (
        !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(
          value,
        ) ||
        btoa(atob(value)) !== value
      )
        return "invalid-base64";
      return undefined;
    } catch {
      return "invalid-base64";
    }
  }
  if (valueType !== "JSON") return undefined;
  try {
    JSON.parse(value);
    return undefined;
  } catch {
    return "invalid-json";
  }
}

export function normalizeSecretPage(value: unknown): RuntimeSecretPage {
  const invalid = () =>
    new AppProblem({
      status: 502,
      code: "INVALID_SECRET_PAGE",
      retryable: false,
      kind: "unknown",
    });
  if (!value || typeof value !== "object") throw invalid();
  const page = value as Partial<RuntimeSecretPage>;
  if (
    !Array.isArray(page.items) ||
    (page.nextPageToken != null && typeof page.nextPageToken !== "string")
  )
    throw invalid();
  const refs = new Set<string>();
  for (const item of page.items) {
    if (
      !isObject(item) ||
      typeof item.ref !== "string" ||
      !item.ref ||
      refs.has(item.ref) ||
      !Number.isSafeInteger(item.version) ||
      item.version < 1 ||
      !Number.isSafeInteger(item.currentRevision) ||
      item.currentRevision < 1 ||
      typeof item.projectRef !== "string" ||
      !item.projectRef ||
      typeof item.name !== "string" ||
      typeof item.description !== "string" ||
      !["STRING", "JSON", "BINARY"].includes(item.valueType) ||
      !["ACTIVE", "REVOKED"].includes(item.state) ||
      !Array.isArray(item.nextActions) ||
      !item.nextActions.every((action) => typeof action === "string") ||
      typeof item.createdAt !== "string" ||
      typeof item.updatedAt !== "string" ||
      !Number.isFinite(Date.parse(item.createdAt)) ||
      !Number.isFinite(Date.parse(item.updatedAt))
    )
      throw invalid();
    if (
      item.displayHint != null &&
      (!isObject(item.displayHint) ||
        typeof item.displayHint.prefix !== "string" ||
        typeof item.displayHint.suffix !== "string")
    )
      throw invalid();
    refs.add(item.ref);
  }
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      version: item.version,
      projectRef: item.projectRef,
      name: item.name,
      description: item.description,
      valueType: item.valueType,
      state: item.state,
      currentRevision: item.currentRevision,
      nextActions: [...item.nextActions],
      createdAt: item.createdAt,
      updatedAt: item.updatedAt,
      ...(item.displayHint
        ? {
            displayHint: {
              prefix: item.displayHint.prefix,
              suffix: item.displayHint.suffix,
            },
          }
        : {}),
    })),
    nextPageToken: page.nextPageToken ?? "",
  };
}
function isObject(value: unknown): boolean {
  return value !== null && typeof value === "object";
}
