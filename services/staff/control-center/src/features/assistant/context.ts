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
    context: Pick<AssistantContextDescriptor, "entityKind" | "entityRef">;
  },
  context: AssistantContextDescriptor,
): boolean {
  return (
    (conversation.context.entityKind ?? "") === context.entityKind &&
    (conversation.context.entityRef ?? "") === context.entityRef
  );
}
