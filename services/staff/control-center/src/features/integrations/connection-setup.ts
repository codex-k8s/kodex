import type {
  IntegrationConnection,
  IntegrationConnectionInput,
  IntegrationConfigurationField,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";

export interface PendingCredentialSetup {
  connectionRef: string;
  version: number;
  idempotencyKey: string;
}

export type ConnectionSetupOutcome =
  | {
      status: "COMPLETE";
      connection: IntegrationConnection;
    }
  | {
      status: "CREDENTIAL_FAILED";
      pending: PendingCredentialSetup;
      error: unknown;
    };

interface ConnectionSetupDependencies {
  create: (input: IntegrationConnectionInput) => Promise<IntegrationConnection>;
  configure: (
    target: Pick<PendingCredentialSetup, "connectionRef" | "version">,
    credentialValue: string,
    idempotencyKey: string,
  ) => Promise<IntegrationConnection>;
  createIdempotencyKey: () => string;
}

export type ConnectionConfigurationProblem =
  | "REQUIRED"
  | "INVALID_HTTPS_URL"
  | "INVALID_VALUE";

export interface PreparedConnectionConfiguration {
  value: Record<string, unknown>;
  problems: Partial<Record<string, ConnectionConfigurationProblem>>;
}

function fieldValue(
  field: IntegrationConfigurationField,
  rawValue: string,
): string | string[] {
  const normalized = rawValue.trim();
  if (field.valueType !== "STRING_LIST") return normalized;
  return normalized
    .split(",")
    .map((item) => item.trim())
    .filter((item, index, values) => item && values.indexOf(item) === index);
}

function validHttpsUrl(value: string, originOnly: boolean): boolean {
  try {
    const parsed = new URL(value);
    return (
      parsed.protocol === "https:" &&
      !parsed.username &&
      !parsed.password &&
      (!originOnly ||
        ((!parsed.pathname || parsed.pathname === "/") &&
          !parsed.search &&
          !parsed.hash))
    );
  } catch {
    return false;
  }
}

export function prepareConnectionConfiguration(
  fields: readonly IntegrationConfigurationField[],
  values: Readonly<Record<string, string>>,
): PreparedConnectionConfiguration {
  const value: Record<string, unknown> = {};
  const problems: PreparedConnectionConfiguration["problems"] = {};

  for (const field of fields) {
    const prepared = fieldValue(field, values[field.key] ?? "");
    const empty = Array.isArray(prepared)
      ? prepared.length === 0
      : prepared.length === 0;
    if (empty) {
      if (field.required) problems[field.key] = "REQUIRED";
      continue;
    }
    if (field.valueType === "INTEGER") {
      const number =
        typeof prepared === "string" && /^-?\d+$/.test(prepared)
          ? Number(prepared)
          : NaN;
      if (
        !Number.isSafeInteger(number) ||
        (field.minimum !== undefined && number < field.minimum) ||
        (field.maximum !== undefined && number > field.maximum)
      )
        problems[field.key] = "INVALID_VALUE";
      else value[field.key] = number;
      continue;
    }
    if (field.valueType === "BOOLEAN") {
      if (prepared !== "true" && prepared !== "false")
        problems[field.key] = "INVALID_VALUE";
      else value[field.key] = prepared === "true";
      continue;
    }
    const strings = Array.isArray(prepared) ? prepared : [prepared];
    if (
      strings.some(
        (item) =>
          (field.maximumLength !== undefined &&
            item.length > field.maximumLength) ||
          (field.allowedValues?.length && !field.allowedValues.includes(item)),
      )
    ) {
      problems[field.key] = "INVALID_VALUE";
      continue;
    }
    if (
      field.valueType === "URL" &&
      typeof prepared === "string" &&
      !validHttpsUrl(prepared, field.format === "HTTPS_ORIGIN")
    ) {
      problems[field.key] = "INVALID_HTTPS_URL";
      continue;
    }
    value[field.key] = prepared;
  }

  return { value, problems };
}

export function definitionRequiresCredential(
  definition: IntegrationDefinition | undefined,
): boolean {
  return Boolean(definition?.credentialSecretKey?.trim());
}

export function canConfigureCredential(
  definition: IntegrationDefinition | undefined,
  connection: IntegrationConnection,
): boolean {
  const nextActions = connection.nextActions as readonly string[];
  return (
    definition?.available === true &&
    definitionRequiresCredential(definition) &&
    !connection.credentialsConfigured &&
    nextActions.includes("CONFIGURE_CREDENTIAL")
  );
}

export async function executeConnectionSetup(
  input: {
    connection: IntegrationConnectionInput;
    credentialValue: string;
    requiresCredential: boolean;
    pending?: PendingCredentialSetup;
  },
  dependencies: ConnectionSetupDependencies,
): Promise<ConnectionSetupOutcome> {
  let pending = input.pending;
  if (!pending) {
    const connection = await dependencies.create(input.connection);
    if (!input.requiresCredential) return { status: "COMPLETE", connection };
    pending = {
      connectionRef: connection.ref,
      version: connection.version,
      idempotencyKey: dependencies.createIdempotencyKey(),
    };
  }

  try {
    const connection = await dependencies.configure(
      {
        connectionRef: pending.connectionRef,
        version: pending.version,
      },
      input.credentialValue,
      pending.idempotencyKey,
    );
    return { status: "COMPLETE", connection };
  } catch (error) {
    return { status: "CREDENTIAL_FAILED", pending, error };
  }
}
