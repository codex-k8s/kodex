import {
  getProject,
  listOrganizationAgents,
  listOrganizationWorkflows,
  listOrganizationSchedules,
  listOrganizationRuntimeEnvironmentSets,
  listOrganizationRuntimeSecrets,
} from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
export type CatalogKind =
  | "agents"
  | "workflows"
  | "automations"
  | "environments"
  | "secrets";
export function catalogInvalidated(
  kind: CatalogKind,
  resource: string,
): boolean {
  if (["PROJECT", "MEMBERSHIP", "PLATFORM_MEMBERSHIP"].includes(resource))
    return true;
  return (
    (kind === "agents" && ["AGENT", "INSTRUCTIONS"].includes(resource)) ||
    (kind === "workflows" && resource === "WORKFLOW") ||
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
    case "agents": {
      const page = (await unwrap(listOrganizationAgents(options))).data;
      return {
        ...page,
        items: page.items.map((item) => ({
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
          ref: item.ref,
          projectRef: item.projectRef,
          title: item.name,
          description: item.purpose,
          state: item.state,
          version: item.version,
          path: `${prefix(item.projectRef)}/workflows/${encodeURIComponent(item.ref)}`,
          meta: item.steps.map((step) => step.name),
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
