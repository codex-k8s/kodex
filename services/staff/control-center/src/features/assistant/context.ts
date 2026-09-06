import type { RouteLocationNormalizedLoaded } from "vue-router";

import type {
  Agent,
  AssistantContextDescriptor,
  Project,
  Run,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";

export interface AssistantContextSources {
  projects: Readonly<Record<string, Project>>;
  agents: Readonly<Record<string, Agent>>;
  workflows: Readonly<Record<string, Workflow>>;
  runs: Readonly<Record<string, Run>>;
}

export interface ResolvedAssistantContext {
  descriptor: AssistantContextDescriptor;
  projectRef?: string;
}

export const assistantContextOperations = [
  "CREATE_PROJECT",
  "UPDATE_PROJECT",
  "CREATE_AGENT",
  "CREATE_WORKFLOW",
  "CHANGE_CAPABILITY",
  "CHANGE_INTEGRATION_GRANT",
  "CREATE_SCHEDULE",
  "LAUNCH_RUN",
  "CREATE_INTEGRATION_CONNECTION",
  "TEST_INTEGRATION_CONNECTION",
  "ARCHIVE_AGENT",
  "ARCHIVE_WORKFLOW",
] as const;

export function readableContextKind(kind: string) {
  return (
    [
      "PROJECT",
      "AGENT",
      "WORKFLOW",
      "RUN",
      "FILE",
      "ENVIRONMENT",
      "INTEGRATION_CONNECTION",
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
  const routePath = route.fullPath.slice(0, 500);
  const selectedResource =
    route.name === "runtime-environment"
      ? { kind: "ENVIRONMENT", ref: routeParameter(route, "environmentRef") }
      : route.name === "integrations"
        ? { kind: "INTEGRATION_CONNECTION", ref: route.query.connectionRef }
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
        entityName: "",
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
