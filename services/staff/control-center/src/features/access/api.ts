import { requestSignal } from "@/shared/api/client";
import {
  archiveAccessRole,
  changeAccessBinding,
  createAccessBinding,
  createAccessRole,
  createAccessRoleVersion,
  explainAccess,
  listAccessBindings,
  listAccessRoles,
  listAccessRoleVersions,
  listAccessSubjects,
  listAgents,
  listOidcGroups,
  listPermissionRegistry,
  listProjects,
  queryEffectiveAccess,
  revokeAccessBinding,
  simulateAccess,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AccessBinding,
  AccessBindingChangeInput,
  AccessBindingInput,
  AccessBindingPage,
  AccessRole,
  AccessRoleInput,
  AccessRolePage,
  AccessRoleVersionPage,
  AccessSubjectKind,
  AccessSubjectPage,
  AgentPage,
  EffectiveAccessPage,
  EffectiveAccessQuery,
  ExplainAccessInput,
  ExplainAccessResult,
  OidcGroupPage,
  PermissionDefinitionPage,
  ProjectPage,
  SimulateAccessInput,
  SimulateAccessResult,
} from "@/shared/api/generated/openapi/types.gen";
import { csrfToken, mutate, type MutationHeaders } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

function mutationHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "X-CSRF-Token": string;
} {
  return {
    "Idempotency-Key": headers["Idempotency-Key"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

function versionedHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "X-CSRF-Token": string;
  "If-Match": string;
} {
  if (!headers["If-Match"]) throw new Error("Version header is unavailable");
  return { ...mutationHeaders(headers), "If-Match": headers["If-Match"] };
}

export async function fetchPermissionRegistry(): Promise<PermissionDefinitionPage> {
  return (await unwrap(listPermissionRegistry({ signal: requestSignal() })))
    .data;
}

export async function fetchAccessSubjects(options: {
  query?: string;
  kind?: AccessSubjectKind;
  pageToken?: string;
}): Promise<AccessSubjectPage> {
  return (
    await unwrap(
      listAccessSubjects({
        query: {
          ...(options.query ? { query: options.query } : {}),
          ...(options.kind ? { kind: options.kind } : {}),
          ...(options.pageToken ? { pageToken: options.pageToken } : {}),
          pageSize: 50,
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchOidcGroups(options: {
  query?: string;
  pageToken?: string;
}): Promise<OidcGroupPage> {
  return (
    await unwrap(
      listOidcGroups({
        query: {
          ...(options.query ? { query: options.query } : {}),
          ...(options.pageToken ? { pageToken: options.pageToken } : {}),
          pageSize: 50,
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchAccessRoles(options: {
  pageToken?: string;
  includeArchived?: boolean;
}): Promise<AccessRolePage> {
  return (
    await unwrap(
      listAccessRoles({
        query: {
          ...(options.pageToken ? { pageToken: options.pageToken } : {}),
          pageSize: 50,
          includeArchived: options.includeArchived ?? false,
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchAccessRoleVersions(
  roleRef: string,
): Promise<AccessRoleVersionPage> {
  return (
    await unwrap(
      listAccessRoleVersions({
        path: { roleRef },
        query: { pageSize: 100 },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchAccessBindings(options: {
  pageToken?: string;
  subjectKind?: AccessSubjectKind;
  subjectRef?: string;
  roleRef?: string;
  projectRef?: string;
  includeRevoked?: boolean;
}): Promise<AccessBindingPage> {
  return (
    await unwrap(
      listAccessBindings({
        query: {
          ...(options.pageToken ? { pageToken: options.pageToken } : {}),
          ...(options.subjectKind ? { subjectKind: options.subjectKind } : {}),
          ...(options.subjectRef ? { subjectRef: options.subjectRef } : {}),
          ...(options.roleRef ? { roleRef: options.roleRef } : {}),
          ...(options.projectRef ? { projectRef: options.projectRef } : {}),
          pageSize: 50,
          includeRevoked: options.includeRevoked ?? false,
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchProjects(query = ""): Promise<ProjectPage> {
  return (
    await unwrap(
      listProjects({
        query: { ...(query ? { query } : {}), pageSize: 100 },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchAgents(
  projectRef: string,
  query = "",
): Promise<AgentPage> {
  return (
    await unwrap(
      listAgents({
        path: { projectRef },
        query: { ...(query ? { query } : {}), pageSize: 100 },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function addAccessRole(
  input: AccessRoleInput,
): Promise<AccessRole> {
  return (
    await mutate((headers) =>
      createAccessRole({
        body: input,
        headers: mutationHeaders(headers),
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function addAccessRoleVersion(
  role: AccessRole,
  input: AccessRoleInput,
): Promise<AccessRole> {
  return (
    await mutate(
      (headers) =>
        createAccessRoleVersion({
          path: { roleRef: role.ref },
          body: input,
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      role.version,
    )
  ).data;
}

export async function archiveRole(role: AccessRole): Promise<AccessRole> {
  return (
    await mutate(
      (headers) =>
        archiveAccessRole({
          path: { roleRef: role.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      role.version,
    )
  ).data;
}

export async function addAccessBinding(
  input: AccessBindingInput,
): Promise<AccessBinding> {
  return (
    await mutate((headers) =>
      createAccessBinding({
        body: input,
        headers: mutationHeaders(headers),
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function updateAccessBinding(
  binding: AccessBinding,
  input: AccessBindingChangeInput,
): Promise<AccessBinding> {
  return (
    await mutate(
      (headers) =>
        changeAccessBinding({
          path: { bindingRef: binding.ref },
          body: input,
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      binding.version,
    )
  ).data;
}

export async function removeAccessBinding(
  binding: AccessBinding,
): Promise<AccessBinding> {
  return (
    await mutate(
      (headers) =>
        revokeAccessBinding({
          path: { bindingRef: binding.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      binding.version,
    )
  ).data;
}

export async function fetchEffectiveAccess(
  input: EffectiveAccessQuery,
): Promise<EffectiveAccessPage> {
  return (
    await unwrap(
      queryEffectiveAccess({
        body: input,
        headers: { "X-CSRF-Token": csrfToken() },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchAccessExplanation(
  input: ExplainAccessInput,
): Promise<ExplainAccessResult> {
  return (
    await unwrap(
      explainAccess({
        body: input,
        headers: { "X-CSRF-Token": csrfToken() },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchAccessSimulation(
  input: SimulateAccessInput,
): Promise<SimulateAccessResult> {
  return (
    await unwrap(
      simulateAccess({
        body: input,
        headers: { "X-CSRF-Token": csrfToken() },
        signal: requestSignal(),
      }),
    )
  ).data;
}
