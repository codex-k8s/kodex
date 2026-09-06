import type { RuntimeSecretReveal } from "./model";

export interface RuntimeSecretRevealSessionBoundary {
  beginRuntimeSecretRevealReauth(input: {
    projectRef: string;
    secretRef: string;
  }): Promise<void>;
  consumePendingRuntimeSecretReveal(
    projectRef: string,
    secretRef: string,
  ): boolean;
  refreshMetadata(): Promise<void>;
}

export type RuntimeSecretRevealFlowResult =
  | { readonly kind: "reauthentication-started" }
  | { readonly kind: "revealed"; readonly value: RuntimeSecretReveal };

export async function executeRuntimeSecretReveal(input: {
  readonly projectRef: string;
  readonly secretRef: string;
  readonly session: RuntimeSecretRevealSessionBoundary;
  reveal(secretRef: string): Promise<RuntimeSecretReveal>;
}): Promise<RuntimeSecretRevealFlowResult> {
  if (
    !input.session.consumePendingRuntimeSecretReveal(
      input.projectRef,
      input.secretRef,
    )
  ) {
    await input.session.beginRuntimeSecretRevealReauth({
      projectRef: input.projectRef,
      secretRef: input.secretRef,
    });
    return { kind: "reauthentication-started" };
  }
  try {
    return { kind: "revealed", value: await input.reveal(input.secretRef) };
  } finally {
    await input.session.refreshMetadata();
  }
}
