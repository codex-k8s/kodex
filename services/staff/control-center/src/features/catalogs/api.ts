import {
  getProject,
  listOrganizationAgents,
  listOrganizationWorkflows,
  listOrganizationSchedules,
  listOrganizationRuntimeEnvironmentSets,
  listOrganizationRuntimeSecrets,
  listOrganizationProjectMemberships,
} from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import type { Agent, Workflow } from "@/shared/api/generated/openapi/types.gen";
export type CatalogKind =
  | "agents"
  | "workflows"
  | "automations"
  | "environments"
  | "secrets"
  | "members";
export function catalogInvalidated(
  kind: CatalogKind | "projects",
  resource: string,
): boolean {
  if (["PROJECT", "MEMBERSHIP", "PLATFORM_MEMBERSHIP"].includes(resource))
    return true;
  return (
    (kind === "agents" && ["AGENT", "INSTRUCTIONS"].includes(resource)) ||
    (kind === "workflows" &&
      ["WORKFLOW", "AGENT", "INSTRUCTIONS"].includes(resource)) ||
    (["agents", "workflows"].includes(kind) &&
      ["RUN", "INTEGRATION_CONNECTION", "INTEGRATION_GRANT"].includes(
        resource,
      )) ||
    (kind === "projects" &&
      [
        "AGENT",
        "WORKFLOW",
        "RUN",
        "INTEGRATION_CONNECTION",
        "INTEGRATION_GRANT",
      ].includes(resource)) ||
    (kind === "automations" && resource === "SCHEDULE")
  );
}
export interface CatalogEntry {
  ref: string;
  projectRef: string;
  title: string;
  description: string;
  state: string;
  version: number;
  path: string;
  meta: string[];
  role?: string;
  agent?: Agent;
  workflow?: Workflow;
}
export interface CatalogPage {
  items: CatalogEntry[];
  nextPageToken?: string;
}
export async function loadCatalog(
  kind: CatalogKind,
  query: string,
  signal: AbortSignal,
  pageToken?: string,
  projectRef?: string,
): Promise<CatalogPage> {
  const options = {
    query: { query, pageToken, projectRef, pageSize: 30 },
    signal: AbortSignal.any([signal, requestSignal()]),
  };
  const prefix = (project: string) =>
    `/projects/${encodeURIComponent(project)}`;
  switch (kind) {
    case "members": {
      const page = (await unwrap(listOrganizationProjectMemberships(options)))
        .data;
      return {
        ...page,
        items: page.items.map((item) => {
          if (
            !item.projectRef ||
            (projectRef && item.projectRef !== projectRef)
          )
            throw new Error("Invalid project membership catalog scope");
          return {
            ref: item.ref,
            projectRef: item.projectRef,
            title: item.user.displayName,
            description: "",
            state: item.active ? "ACTIVE" : "DISABLED",
            version: item.version,
            path: `${prefix(item.projectRef)}/members`,
            meta: [],
            role: item.platformRole,
          };
        }),
      };
    }
    case "agents": {
      const page = (await unwrap(listOrganizationAgents(options))).data;
      return {
        ...page,
        items: page.items.map((item) => ({
          agent: item,
          ref: item.ref,
          projectRef: item.projectRef,
          title: item.name,
          description: item.purpose,
          state: item.state,
          version: item.version,
          path: `${prefix(item.projectRef)}/agents/${encodeURIComponent(item.ref)}`,
          meta: [
            item.runtimeProvider ?? "",
            item.runtimeModel ?? item.runtimeName,
            item.roleDefinitionName ?? "",
          ],
        })),
      };
    }
    case "workflows": {
      const page = (await unwrap(listOrganizationWorkflows(options))).data;
      return {
        ...page,
        items: page.items.map((item) => ({
          workflow: item,
          ref: item.ref,
          projectRef: item.projectRef,
          title: item.name,
          description: item.purpose,
          state: item.state,
          version: item.version,
          path: `${prefix(item.projectRef)}/workflows/${encodeURIComponent(item.ref)}`,
          meta: [],
        })),
      };
    }
    case "automations": {
      const page = (await unwrap(listOrganizationSchedules(options))).data;
      return {
        ...page,
        items: page.items.map((item) => ({
          ref: item.ref,
          projectRef: item.projectRef,
          title: item.name,
          description: item.automationText,
          state: item.state,
          version: item.version,
          path: `${prefix(item.projectRef)}/automations?scheduleRef=${encodeURIComponent(item.ref)}`,
          meta: [item.target.displayName, item.cronExpression, item.timezone],
        })),
      };
    }
    case "environments": {
      const page = (
        await unwrap(listOrganizationRuntimeEnvironmentSets(options))
      ).data;
      return {
        ...page,
        items: page.items.map((item) => ({
          ref: item.ref,
          projectRef: item.projectRef,
          title: item.name,
          description: item.description,
          state: item.ready ? item.state : "UNAVAILABLE",
          version: item.version,
          path: `${prefix(item.projectRef)}/environments/${encodeURIComponent(item.ref)}`,
          meta: [],
        })),
      };
    }
    case "secrets": {
      const page = (await unwrap(listOrganizationRuntimeSecrets(options))).data;
      return {
        ...page,
        items: page.items.map((item) => ({
          ref: item.ref,
          projectRef: item.projectRef,
          title: item.name,
          description: item.description,
          state: item.state,
          version: item.version,
          path: `${prefix(item.projectRef)}/secrets?secretRef=${encodeURIComponent(item.ref)}`,
          meta: [item.valueType],
        })),
      };
    }
  }
}
export async function loadCatalogProject(ref: string, signal: AbortSignal) {
  return (
    await unwrap(
      getProject({
        path: { projectRef: ref },
        signal: AbortSignal.any([signal, requestSignal()]),
      }),
    )
  ).data;
}
