import type {
  IntegrationDefinition,
  ManagedConfiguration,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";

interface SourceIdentity {
  name: string;
  ref: string;
  revision: string;
  version: number;
  digest?: string;
  origin: "SHIPPED" | "UI" | "GIT";
}
export type ConfigurationCopySource = SourceIdentity &
  (
    | {
        kind: "ROLE_IMAGE";
        projectRef: string;
        recipeRef: string;
        configurationRef?: never;
      }
    | {
        kind: "ROLE_IMAGE";
        projectRef: string;
        configurationRef: string;
        recipeRef?: never;
      }
    | {
        kind: "INTEGRATION_DEFINITION";
        shipped: { key: string; definitionVersion: string; digest: string };
        configurationRef?: never;
      }
    | {
        kind: "INTEGRATION_DEFINITION";
        configurationRef: string;
        shipped?: never;
      }
  );

export function managedCopySource(
  configuration: ManagedConfiguration,
): ConfigurationCopySource | undefined {
  if (!configuration.nextActions.includes("COPY")) return;
  const identity: SourceIdentity = {
    name: configuration.name,
    ref: configuration.ref,
    revision: configuration.currentRevision?.ref ?? "",
    version: configuration.version,
    digest: configuration.currentRevision?.digest,
    origin: configuration.managedBy,
  };
  if (configuration.kind === "ROLE_IMAGE" && configuration.projectRef)
    return {
      ...identity,
      kind: "ROLE_IMAGE",
      configurationRef: configuration.ref,
      projectRef: configuration.projectRef,
    };
  if (configuration.kind === "INTEGRATION_DEFINITION")
    return {
      ...identity,
      kind: "INTEGRATION_DEFINITION",
      configurationRef: configuration.ref,
    };
}

export function recipeCopySource(
  recipe: RoleImageRecipe,
): ConfigurationCopySource | undefined {
  if (!recipe.nextActions.includes("COPY") || !recipe.sourceAvailable) return;
  const lineage = recipe.managedLineage;
  // Managed recipe copy выбирается из авторитетной карточки configuration,
  // чтобы If-Match относился к set, а не к наблюдаемой версии recipe.
  if (lineage?.managedBy !== "SHIPPED") return;
  return {
    kind: "ROLE_IMAGE",
    projectRef: recipe.projectRef,
    recipeRef: recipe.ref,
    name: recipe.name,
    ref: recipe.ref,
    revision: String(recipe.generation),
    version: recipe.version,
    origin: "SHIPPED",
  };
}

export function integrationCopySource(
  definition: IntegrationDefinition,
): ConfigurationCopySource | undefined {
  if (!definition.nextActions.includes("COPY")) return;
  return {
    kind: "INTEGRATION_DEFINITION",
    name: definition.name,
    ref: definition.key,
    revision: definition.definitionVersion,
    version: definition.version,
    digest: definition.digest,
    origin: "SHIPPED",
    shipped: {
      key: definition.key,
      definitionVersion: definition.definitionVersion,
      digest: definition.digest,
    },
  };
}

export function canArchiveConfiguration(
  configuration: ManagedConfiguration,
): boolean {
  return (
    !configuration.archived &&
    configuration.managedBy === "UI" &&
    configuration.nextActions.includes("ARCHIVE") &&
    (configuration.kind === "INTEGRATION_DEFINITION" ||
      configuration.kind === "ROLE_IMAGE")
  );
}
