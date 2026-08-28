import type {
  IntegrationConnection,
  IntegrationConnectionInput,
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
