import type { AgentRuntimeConfigurationInput } from "@/shared/api/generated/openapi/types.gen";
import type { ProviderAccountCandidate } from "@/features/providers/model";
import {
  accountSnapshotAvailable,
  type ModelSelection,
} from "@/features/providers/model-catalog";

export type RuntimeForm = Omit<
  AgentRuntimeConfigurationInput,
  "providerAccounts"
> & { providerAccounts: ProviderAccountCandidate[] };
export function runtimeCandidates(
  values: readonly ProviderAccountCandidate[],
): ProviderAccountCandidate[] {
  return values.map(({ accountRef, weight }) => ({ accountRef, weight }));
}
export function pinnedRuntimeInput(
  form: RuntimeForm,
  selection: ModelSelection | undefined,
  provider: string,
): AgentRuntimeConfigurationInput {
  if (
    !selection ||
    selection.model !== form.model ||
    selection.providerDefinitionKey !== provider ||
    selection.accounts.length !== form.providerAccounts.length ||
    new Set(form.providerAccounts.map((item) => item.accountRef)).size !==
      form.providerAccounts.length
  )
    throw new Error("Runtime model selection is not pinned");
  return {
    runtimeProfileRef: form.runtimeProfileRef,
    model: form.model,
    providerPolicyMode: form.providerPolicyMode,
    providerAccounts: form.providerAccounts.map((item) => {
      const snapshot = selection.accounts.find(
        (value) => value.accountRef === item.accountRef,
      );
      if (
        !snapshot ||
        snapshot.providerDefinitionKey !== provider ||
        snapshot.model?.id !== form.model ||
        !accountSnapshotAvailable(snapshot)
      )
        throw new Error("Runtime account catalog is unavailable");
      return {
        accountRef: item.accountRef,
        weight: item.weight,
        catalogRevision: snapshot.catalogRevision,
        catalogDigest: snapshot.catalogDigest,
        providerDefinitionKey: snapshot.providerDefinitionKey,
      };
    }),
  };
}
