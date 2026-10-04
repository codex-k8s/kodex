import type { RuntimeSecretReveal } from "./model";
import type { RuntimeResourceAddress } from "@/features/runtime/resource-scope";

export interface RuntimeSecretRevealSessionBoundary {
  beginRuntimeSecretRevealReauth(input: {
    projectRef: RuntimeResourceAddress;
    organizationRef: string;
    secretRef: string;
  }): Promise<void>;
  consumePendingRuntimeSecretReveal(
    projectRef: RuntimeResourceAddress,
    secretRef: string,
    organizationRef: string,
  ): boolean;
  refreshMetadata(): Promise<void>;
}

export type RuntimeSecretRevealFlowResult =
  | { readonly kind: "reauthentication-started" }
  | { readonly kind: "revealed"; readonly value: RuntimeSecretReveal };

export async function executeRuntimeSecretReveal(input: {
  readonly projectRef: RuntimeResourceAddress;
  readonly organizationRef: string;
  readonly secretRef: string;
  readonly session: RuntimeSecretRevealSessionBoundary;
  reveal(secretRef: string): Promise<RuntimeSecretReveal>;
}): Promise<RuntimeSecretRevealFlowResult> {
  if (
    !input.session.consumePendingRuntimeSecretReveal(
      input.projectRef,
      input.secretRef,
      input.organizationRef,
    )
  ) {
    await input.session.beginRuntimeSecretRevealReauth({
      projectRef: input.projectRef,
      secretRef: input.secretRef,
      organizationRef: input.organizationRef,
    });
    return { kind: "reauthentication-started" };
  }
  try {
    return { kind: "revealed", value: await input.reveal(input.secretRef) };
  } finally {
    await input.session.refreshMetadata();
  }
}
