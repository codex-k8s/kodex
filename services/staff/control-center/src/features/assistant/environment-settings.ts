import {
  editableRuntimeEnvironmentPolicy,
  editableSecretBindings,
} from "@/features/runtime/environment-form";
import {
  assertRuntimeResourceScope,
  type RuntimeResourceScope,
} from "@/features/runtime/resource-scope";
import type {
  RuntimeEnvironmentInput,
  RuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/types.gen";

export function editableAssistantEnvironment(
  environment: RuntimeEnvironmentSet,
  scope: RuntimeResourceScope,
): RuntimeEnvironmentInput {
  assertRuntimeResourceScope(scope, environment.projectRef);
  const current = environment.currentVersion;
  return {
    name: environment.name,
    description: environment.description,
    imageArtifactRef: current.image.artifactRef,
    tools: current.tools.map((tool) => ({ ...tool })),
    values: current.values.map((value) => ({ ...value })),
    secretBindings: editableSecretBindings(current.secretDescriptors),
    policy: editableRuntimeEnvironmentPolicy(current.policy),
  };
}
