import type {
  Membership,
  NextAction,
  PlatformMembershipCreateInput,
  ProjectMembershipCreateInput,
} from "@/shared/api/generated/openapi/types.gen";

export type AccessSection =
  | "MEMBERS"
  | "GROUPS"
  | "ROLES"
  | "EFFECTIVE"
  | "AGENT_SCOPE";

export type PlatformRole = PlatformMembershipCreateInput["platformRole"];
export type ProjectPermission =
  ProjectMembershipCreateInput["permissions"][number];

export interface SystemRolePresentation {
  role: PlatformRole;
  memberCount: number;
  activeMemberCount: number;
}

export const platformRoleOrder: readonly PlatformRole[] = [
  "OWNER",
  "ADMINISTRATOR",
  "OPERATOR",
  "MEMBER",
  "AUDITOR",
];

export const projectPermissionOrder: readonly ProjectPermission[] = [
  "VIEW",
  "LAUNCH_RUNS",
  "CANCEL_RUNS",
  "RESOLVE_GATES",
  "VIEW_AUDIT",
  "MANAGE_ARTIFACTS",
  "MANAGE_AGENTS",
  "MANAGE_WORKFLOWS",
  "MANAGE_SCHEDULES",
  "MANAGE_INTEGRATIONS",
  "MANAGE_MEMBERS",
  "MANAGE",
];

export function buildSystemRoles(
  memberships: readonly Membership[],
): SystemRolePresentation[] {
  return platformRoleOrder.map((role) => {
    const roleMemberships = memberships.filter(
      (membership) => membership.platformRole === role,
    );
    return {
      role,
      memberCount: roleMemberships.length,
      activeMemberCount: roleMemberships.filter(
        (membership) => membership.active,
      ).length,
    };
  });
}

export function filterMemberships(
  memberships: readonly Membership[],
  search: string,
): Membership[] {
  const normalized = search.trim().toLocaleLowerCase();
  if (!normalized) return [...memberships];
  return memberships.filter((membership) =>
    [
      membership.user.displayName,
      membership.user.emailHint ?? "",
      membership.platformRole,
      ...membership.permissions,
    ].some((value) => value.toLocaleLowerCase().includes(normalized)),
  );
}

export function orderedProjectPermissions(
  membership: Membership,
): ProjectPermission[] {
  const assigned = new Set(membership.permissions);
  return projectPermissionOrder.filter((permission) =>
    assigned.has(permission),
  );
}

export function membershipAllows(
  membership: Membership,
  action: NextAction,
): boolean {
  return membership.nextActions.includes(action);
}
