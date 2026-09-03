import { defineStore } from "pinia";
import { reactive, ref } from "vue";

import * as api from "@/features/access/api";
import type {
  AccessBinding,
  AccessBindingChangeInput,
  AccessBindingInput,
  AccessRole,
  AccessRoleInput,
  AccessRoleVersion,
  AccessSubject,
  AccessSubjectKind,
  Agent,
  EffectiveAccessPage,
  EffectiveAccessQuery,
  ExplainAccessInput,
  ExplainAccessResult,
  IntegrationConnection,
  Membership,
  OidcGroup,
  PermissionDefinition,
  Project,
  SimulateAccessInput,
  SimulateAccessResult,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";

export type AccessLoadKey =
  | "permissions"
  | "subjects"
  | "groups"
  | "roles"
  | "roleVersions"
  | "bindings"
  | "projects"
  | "agents"
  | "workflows"
  | "integrations"
  | "platformMemberships"
  | "projectMemberships"
  | "effective"
  | "explanation"
  | "simulation";

function appendUnique<T>(
  target: T[],
  values: T[],
  key: (value: T) => string,
): T[] {
  const result = new Map(target.map((value) => [key(value), value]));
  for (const value of values) result.set(key(value), value);
  return [...result.values()];
}

export const useAccessStore = defineStore("access", () => {
  const permissions = ref<PermissionDefinition[]>([]);
  const subjects = ref<AccessSubject[]>([]);
  const groups = ref<OidcGroup[]>([]);
  const roles = ref<AccessRole[]>([]);
  const bindings = ref<AccessBinding[]>([]);
  const projects = ref<Project[]>([]);
  const agents = reactive<Record<string, Agent[]>>({});
  const workflows = reactive<Record<string, Workflow[]>>({});
  const integrations = ref<IntegrationConnection[]>([]);
  const platformMemberships = ref<Membership[]>([]);
  const projectMemberships = ref<Membership[]>([]);
  const roleVersions = reactive<Record<string, AccessRoleVersion[]>>({});
  const effective = ref<EffectiveAccessPage>();
  const explanation = ref<ExplainAccessResult>();
  const simulation = ref<SimulateAccessResult>();
  const subjectNextPageToken = ref<string>();
  const groupNextPageToken = ref<string>();
  const roleNextPageToken = ref<string>();
  const bindingNextPageToken = ref<string>();
  const loading = reactive<Record<AccessLoadKey, boolean>>({
    permissions: false,
    subjects: false,
    groups: false,
    roles: false,
    roleVersions: false,
    bindings: false,
    projects: false,
    agents: false,
    workflows: false,
    integrations: false,
    platformMemberships: false,
    projectMemberships: false,
    effective: false,
    explanation: false,
    simulation: false,
  });
  const problems = reactive<Partial<Record<AccessLoadKey, AppProblem>>>({});
  const sequence = reactive<Record<AccessLoadKey, number>>({
    permissions: 0,
    subjects: 0,
    groups: 0,
    roles: 0,
    roleVersions: 0,
    bindings: 0,
    projects: 0,
    agents: 0,
    workflows: 0,
    integrations: 0,
    platformMemberships: 0,
    projectMemberships: 0,
    effective: 0,
    explanation: 0,
    simulation: 0,
  });

  async function query<T>(
    key: AccessLoadKey,
    request: () => Promise<T>,
    apply: (value: T) => void,
  ): Promise<void> {
    const current = ++sequence[key];
    loading[key] = true;
    problems[key] = undefined;
    try {
      const value = await request();
      if (sequence[key] === current) apply(value);
    } catch (error) {
      if (sequence[key] === current) problems[key] = asProblem(error);
    } finally {
      if (sequence[key] === current) loading[key] = false;
    }
  }

  async function loadPermissions(): Promise<void> {
    await query("permissions", api.fetchPermissionRegistry, (page) => {
      permissions.value = page.items;
    });
  }

  async function loadSubjects(
    queryText = "",
    kind?: AccessSubjectKind,
    append = false,
  ): Promise<void> {
    const pageToken = append ? subjectNextPageToken.value : undefined;
    await query(
      "subjects",
      () => api.fetchAccessSubjects({ query: queryText, kind, pageToken }),
      (page) => {
        subjects.value = append
          ? appendUnique(subjects.value, page.items, (item) => item.ref)
          : page.items;
        subjectNextPageToken.value = page.nextPageToken;
      },
    );
  }

  async function loadGroups(queryText = "", append = false): Promise<void> {
    const pageToken = append ? groupNextPageToken.value : undefined;
    await query(
      "groups",
      () => api.fetchOidcGroups({ query: queryText, pageToken }),
      (page) => {
        groups.value = append
          ? appendUnique(groups.value, page.items, (item) => item.ref)
          : page.items;
        groupNextPageToken.value = page.nextPageToken;
      },
    );
  }

  async function loadRoles(
    includeArchived = false,
    append = false,
  ): Promise<void> {
    const pageToken = append ? roleNextPageToken.value : undefined;
    await query(
      "roles",
      () => api.fetchAccessRoles({ includeArchived, pageToken }),
      (page) => {
        roles.value = append
          ? appendUnique(roles.value, page.items, (item) => item.ref)
          : page.items;
        roleNextPageToken.value = page.nextPageToken;
      },
    );
  }

  async function loadRoleVersions(roleRef: string): Promise<void> {
    await query(
      "roleVersions",
      () => api.fetchAccessRoleVersions(roleRef),
      (page) => {
        roleVersions[roleRef] = page.items;
      },
    );
  }

  async function loadBindings(
    options: Parameters<typeof api.fetchAccessBindings>[0] = {},
    append = false,
  ): Promise<void> {
    const pageToken = append ? bindingNextPageToken.value : undefined;
    await query(
      "bindings",
      () => api.fetchAccessBindings({ ...options, pageToken }),
      (page) => {
        bindings.value = append
          ? appendUnique(bindings.value, page.items, (item) => item.ref)
          : page.items;
        bindingNextPageToken.value = page.nextPageToken;
      },
    );
  }

  async function loadProjects(queryText = ""): Promise<void> {
    await query(
      "projects",
      () => api.fetchProjects(queryText),
      (page) => {
        projects.value = page.items;
      },
    );
  }

  async function loadAgents(projectRef: string, queryText = ""): Promise<void> {
    if (!projectRef) return;
    await query(
      "agents",
      () => api.fetchAgents(projectRef, queryText),
      (page) => {
        agents[projectRef] = page.items;
      },
    );
  }

  async function loadWorkflows(
    projectRef: string,
    queryText = "",
  ): Promise<void> {
    if (!projectRef) return;
    await query(
      "workflows",
      () => api.fetchWorkflows(projectRef, queryText),
      (page) => {
        workflows[projectRef] = page.items;
      },
    );
  }

  async function loadIntegrations(): Promise<void> {
    await query("integrations", api.fetchIntegrationConnections, (items) => {
      integrations.value = items;
    });
  }

  async function loadMembershipPresentation(projectRef = ""): Promise<void> {
    await query(
      "platformMemberships",
      api.fetchPlatformMemberships,
      (items) => {
        platformMemberships.value = items;
      },
    );
    if (!projectRef) {
      projectMemberships.value = [];
      delete problems.projectMemberships;
      return;
    }
    await query(
      "projectMemberships",
      () => api.fetchProjectMemberships(projectRef),
      (items) => {
        projectMemberships.value = items;
      },
    );
  }

  async function saveRole(
    input: AccessRoleInput,
    current?: AccessRole,
  ): Promise<AccessRole> {
    const role = current
      ? await api.addAccessRoleVersion(current, input)
      : await api.addAccessRole(input);
    await loadRoles();
    roles.value = appendUnique(roles.value, [role], (item) => item.ref);
    await loadRoleVersions(role.ref);
    return role;
  }

  async function archiveRole(role: AccessRole): Promise<void> {
    await api.archiveRole(role);
    await loadRoles(true);
  }

  async function saveBinding(
    input: AccessBindingInput | AccessBindingChangeInput,
    current?: AccessBinding,
  ): Promise<AccessBinding> {
    const binding = current
      ? await api.updateAccessBinding(
          current,
          input as AccessBindingChangeInput,
        )
      : await api.addAccessBinding(input as AccessBindingInput);
    await loadBindings();
    return binding;
  }

  async function revokeBinding(binding: AccessBinding): Promise<void> {
    await api.removeAccessBinding(binding);
    await loadBindings();
  }

  async function queryEffective(input: EffectiveAccessQuery): Promise<void> {
    await query(
      "effective",
      () => api.fetchEffectiveAccess(input),
      (value) => {
        effective.value = value;
      },
    );
  }

  async function explain(input: ExplainAccessInput): Promise<void> {
    await query(
      "explanation",
      () => api.fetchAccessExplanation(input),
      (value) => {
        explanation.value = value;
      },
    );
  }

  async function simulate(input: SimulateAccessInput): Promise<void> {
    await query(
      "simulation",
      () => api.fetchAccessSimulation(input),
      (value) => {
        simulation.value = value;
      },
    );
  }

  function clearDecision(): void {
    effective.value = undefined;
    explanation.value = undefined;
    simulation.value = undefined;
    delete problems.effective;
    delete problems.explanation;
    delete problems.simulation;
  }

  return {
    permissions,
    subjects,
    groups,
    roles,
    bindings,
    projects,
    agents,
    workflows,
    integrations,
    platformMemberships,
    projectMemberships,
    roleVersions,
    effective,
    explanation,
    simulation,
    subjectNextPageToken,
    groupNextPageToken,
    roleNextPageToken,
    bindingNextPageToken,
    loading,
    problems,
    loadPermissions,
    loadSubjects,
    loadGroups,
    loadRoles,
    loadRoleVersions,
    loadBindings,
    loadProjects,
    loadAgents,
    loadWorkflows,
    loadIntegrations,
    loadMembershipPresentation,
    saveRole,
    archiveRole,
    saveBinding,
    revokeBinding,
    queryEffective,
    explain,
    simulate,
    clearDecision,
  };
});
