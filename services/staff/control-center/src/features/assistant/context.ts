import type { RouteLocationNormalizedLoaded } from "vue-router";

import type {
  Agent,
  AssistantPlanOperation,
  AssistantContextDescriptor,
  Project,
  RoleImageRecipe,
  Run,
  RuntimeEnvironmentSet,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";

export interface AssistantContextSources {
  projects: Readonly<Record<string, Project>>;
  agents: Readonly<Record<string, Agent>>;
  workflows: Readonly<Record<string, Workflow>>;
  runs: Readonly<Record<string, Run>>;
  roleImages: Readonly<Record<string, RoleImageRecipe>>;
  environments?: Readonly<Record<string, RuntimeEnvironmentSet>>;
  organizationRef?: string;
  environmentReadBlocked?: boolean;
}

export interface ResolvedAssistantContext {
  descriptor: AssistantContextDescriptor;
  projectRef?: string;
}

export const assistantContextOperations = [
  "CREATE_PROJECT",
  "CREATE_PROJECT_FILE",
  "UPDATE_PROJECT",
  "UPDATE_AGENT",
  "CREATE_AGENT",
  "CREATE_PROJECT_ASSISTANT",
  "CREATE_INSTRUCTION_DRAFT",
  "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS",
  "CREATE_RUNTIME_ENVIRONMENT_DRAFT",
  "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
  "BIND_AGENT_RUNTIME_ENVIRONMENT",
  "CREATE_ROLE_IMAGE_RECIPE",
  "UPDATE_ROLE_IMAGE_RECIPE",
  "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE",
  "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE",
  "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION",
  "CREATE_WORKFLOW",
  "UPDATE_WORKFLOW",
  "CHANGE_CAPABILITY",
  "CHANGE_INTEGRATION_GRANT",
  "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
  "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT",
  "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION",
  "CREATE_SCHEDULE",
  "UPDATE_SCHEDULE",
  "LAUNCH_RUN",
  "CREATE_INTEGRATION_CONNECTION",
  "PUBLISH_INTEGRATION_DEFINITION",
  "UPDATE_INTEGRATION_CONNECTION",
  "TEST_INTEGRATION_CONNECTION",
  "ARCHIVE_AGENT",
  "ARCHIVE_WORKFLOW",
] as const satisfies readonly AssistantPlanOperation["type"][];

export function readableContextKind(kind: string) {
  return (
    [
      "PROJECT",
      "AGENT",
      "WORKFLOW",
      "RUN",
      "FILE",
      "ENVIRONMENT",
      "ROLE_IMAGE_RECIPE",
      "INTEGRATION_CONNECTION",
      "SCHEDULE",
    ] as const
  ).find((known) => known === kind);
}

export function readableContextOperations(operations: readonly string[]) {
  if (
    operations.some(
      (operation) =>
        !assistantContextOperations.some((known) => known === operation),
    )
  )
    return undefined;
  return assistantContextOperations.filter((operation) =>
    operations.includes(operation),
  );
}

export function assistantContextIdentity(
  context: AssistantContextDescriptor,
  projectRef?: string,
): string {
  return [
    projectRef ?? "",
    context.route,
    context.entityKind,
    context.entityRef,
  ].join(":");
}

export function assistantContextTitle(
  routeContext: AssistantContextDescriptor,
  conversationContext?: AssistantContextDescriptor,
  routeLabel?: string,
): string {
  return (
    routeContext.entityName ||
    conversationContext?.entityName ||
    routeLabel ||
    routeContext.route ||
    "Kodex"
  );
}

// Подпись экрана не меняет маршрут, привязки сущностей и серверные полномочия.
export function assistantContextRouteLabelKey(
  routeName: RouteLocationNormalizedLoaded["name"],
): string | undefined {
  switch (routeName) {
    case "runtime-environment":
      return "nav.environment";
    case "system-assistant-environment":
      return "assistant.resources.environment";
    case "system-role-images":
      return "assistant.contextRoutes.images";
    case "system-role-image-new":
      return "assistant.contextRoutes.newImage";
    case "system-role-image":
      return "assistant.contextRoutes.image";
    case "system-runtime-secrets":
      return "assistant.contextRoutes.secrets";
    default:
      return undefined;
  }
}

function routeParameter(
  route: RouteLocationNormalizedLoaded,
  name: string,
): string | undefined {
  const value = route.params[name];
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

export function resolveAssistantContext(
  route: RouteLocationNormalizedLoaded,
  sources: AssistantContextSources,
): ResolvedAssistantContext {
  const projectRef = routeParameter(route, "projectRef");
  const agentRef = routeParameter(route, "agentRef");
  const workflowRef = routeParameter(route, "workflowRef");
  const runRef = routeParameter(route, "runRef");
  const recipeRef = routeParameter(route, "recipeRef");
  const environmentRef = routeParameter(route, "environmentRef");
  const environment = environmentRef
    ? sources.environments?.[environmentRef]
    : undefined;
  // Имя берётся только из уже загруженного окружения exact owner/project scope.
  // Подпись не назначает version, операции или полномочия контекста.
  const environmentName =
    !sources.environmentReadBlocked &&
    projectRef &&
    sources.organizationRef &&
    environment &&
    environment.ref === environmentRef &&
    environment.projectRef === projectRef &&
    environment.organizationRef === sources.organizationRef &&
    environment.scopeKind === "PROJECT" &&
    environment.state !== "DELETED"
      ? environment.name
      : undefined;
  const routePath = route.fullPath.slice(0, 500);
  const selectedResource =
    route.name === "role-image"
      ? {
          kind: "ROLE_IMAGE_RECIPE",
          ref: recipeRef,
          name: recipeRef ? sources.roleImages[recipeRef]?.name : undefined,
          version: recipeRef
            ? sources.roleImages[recipeRef]?.version
            : undefined,
        }
      : route.name === "runtime-environment"
        ? { kind: "ENVIRONMENT", ref: environmentRef, name: environmentName }
        : route.name === "integrations"
          ? { kind: "INTEGRATION_CONNECTION", ref: route.query.connectionRef }
          : route.name === "automations"
            ? { kind: "SCHEDULE", ref: route.query.scheduleRef }
            : ["files", "files-trash", "organization-files"].includes(
                  String(route.name),
                )
              ? { kind: "FILE", ref: route.query.artifactRef }
              : undefined;

  if (typeof selectedResource?.ref === "string" && selectedResource.ref) {
    return {
      descriptor: {
        route: routePath,
        entityKind: selectedResource.kind,
        entityRef: selectedResource.ref,
        entityName: selectedResource.name ?? "",
        ...(selectedResource.version
          ? { entityVersion: selectedResource.version }
          : {}),
        allowedOperations: [],
      },
      ...(projectRef ? { projectRef } : {}),
    };
  }

  if (agentRef) {
    const agent = sources.agents[agentRef];
    return {
      descriptor: {
        route: routePath,
        entityKind: "AGENT",
        entityRef: agentRef,
        entityName: agent?.name ?? "",
        ...(agent?.version ? { entityVersion: agent.version } : {}),
        allowedOperations: [],
      },
      ...(projectRef ? { projectRef } : {}),
    };
  }
  if (workflowRef) {
    const workflow = sources.workflows[workflowRef];
    return {
      descriptor: {
        route: routePath,
        entityKind: "WORKFLOW",
        entityRef: workflowRef,
        entityName: workflow?.name ?? "",
        ...(workflow?.version ? { entityVersion: workflow.version } : {}),
        allowedOperations: [],
      },
      ...(projectRef ? { projectRef } : {}),
    };
  }
  if (runRef) {
    const run = sources.runs[runRef];
    return {
      descriptor: {
        route: routePath,
        entityKind: "RUN",
        entityRef: runRef,
        entityName: run?.title ?? "",
        ...(run?.version ? { entityVersion: run.version } : {}),
        allowedOperations: [],
      },
      ...(run?.projectRef ? { projectRef: run.projectRef } : {}),
    };
  }
  if (projectRef) {
    const project = sources.projects[projectRef];
    return {
      descriptor: {
        route: routePath,
        entityKind: "PROJECT",
        entityRef: projectRef,
        entityName: project?.name ?? "",
        ...(project?.version ? { entityVersion: project.version } : {}),
        allowedOperations: [],
      },
      projectRef,
    };
  }
  return {
    descriptor: {
      route: routePath,
      entityKind: "",
      entityRef: "",
      entityName: "",
      allowedOperations: [],
    },
  };
}

export function conversationMatchesContext(
  conversation: {
    context: Partial<
      Pick<AssistantContextDescriptor, "entityKind" | "entityRef">
    >;
  },
  context: AssistantContextDescriptor,
): boolean {
  return (
    (conversation.context.entityKind ?? "") === context.entityKind &&
    (conversation.context.entityRef ?? "") === context.entityRef
  );
}
