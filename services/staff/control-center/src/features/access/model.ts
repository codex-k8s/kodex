import type {
  AccessBindingInput,
  AccessConditions,
  AccessResourceKind,
  AccessRoleInput,
  AccessScope,
  AccessScopeKind,
  AccessSubject,
  AccessSubjectKind,
  PermissionDefinition,
} from "@/shared/api/generated/openapi/types.gen";

export type AccessSection =
  | "participants"
  | "groups"
  | "roles"
  | "bindings"
  | "effective";

export const accessSections: AccessSection[] = [
  "participants",
  "groups",
  "roles",
  "bindings",
  "effective",
];

export const accessScopeKinds: AccessScopeKind[] = [
  "ORGANIZATION",
  "PROJECT",
  "RESOURCE_KIND",
  "RESOURCE_INSTANCE",
];

export const accessResourceKinds: AccessResourceKind[] = [
  "ORGANIZATION",
  "PROJECT",
  "AGENT",
  "WORKFLOW",
  "RUN",
  "OWNER_GATE",
  "ARTIFACT",
  "SCHEDULE",
  "INTEGRATION",
];

export interface ScopeDraft {
  kind: AccessScopeKind;
  projectRef: string;
  resourceKind: AccessResourceKind;
  resourceRef: string;
}

export interface BindingDraft {
  subjectKind: AccessSubjectKind;
  subjectRef: string;
  roleVersionRef: string;
  scope: ScopeDraft;
  validFrom: string;
  validUntil: string;
  requireOwner: boolean;
}

export function emptyScopeDraft(projectRef = ""): ScopeDraft {
  return {
    kind: projectRef ? "PROJECT" : "ORGANIZATION",
    projectRef,
    resourceKind: projectRef ? "PROJECT" : "ORGANIZATION",
    resourceRef: "",
  };
}

export function emptyBindingDraft(projectRef = ""): BindingDraft {
  return {
    subjectKind: "USER",
    subjectRef: "",
    roleVersionRef: "",
    scope: emptyScopeDraft(projectRef),
    validFrom: "",
    validUntil: "",
    requireOwner: false,
  };
}

export function toAccessScope(draft: ScopeDraft): AccessScope {
  if (draft.kind === "ORGANIZATION") return { kind: "ORGANIZATION" };
  if (draft.kind === "PROJECT") {
    return { kind: "PROJECT", projectRef: draft.projectRef };
  }
  if (draft.kind === "RESOURCE_KIND") {
    return {
      kind: "RESOURCE_KIND",
      projectRef: draft.projectRef,
      resourceKind: draft.resourceKind,
    };
  }
  return {
    kind: "RESOURCE_INSTANCE",
    projectRef: draft.projectRef,
    resourceKind: draft.resourceKind,
    resourceRef: draft.resourceRef,
  };
}

export function scopeToDraft(scope: AccessScope): ScopeDraft {
  return {
    kind: scope.kind,
    projectRef: scope.projectRef ?? "",
    resourceKind: scope.resourceKind ?? "PROJECT",
    resourceRef: scope.resourceRef ?? "",
  };
}

export function toAccessConditions(draft: BindingDraft): AccessConditions {
  return {
    ...(draft.validFrom
      ? { validFrom: new Date(draft.validFrom).toISOString() }
      : {}),
    ...(draft.validUntil
      ? { validUntil: new Date(draft.validUntil).toISOString() }
      : {}),
    requireOwner: draft.requireOwner,
  };
}

export function toBindingInput(draft: BindingDraft): AccessBindingInput {
  return {
    subjectKind: draft.subjectKind,
    subjectRef: draft.subjectRef,
    roleVersionRef: draft.roleVersionRef,
    scope: toAccessScope(draft.scope),
    conditions: toAccessConditions(draft),
  };
}

export function validScope(draft: ScopeDraft): boolean {
  if (draft.kind === "ORGANIZATION") return true;
  if (!draft.projectRef) return false;
  if (draft.kind === "PROJECT") return true;
  return draft.kind !== "RESOURCE_INSTANCE" || Boolean(draft.resourceRef);
}

export function isExactAgentScope(scope: AccessScope): boolean {
  return (
    scope.kind === "RESOURCE_INSTANCE" &&
    scope.resourceKind === "AGENT" &&
    Boolean(scope.projectRef) &&
    Boolean(scope.resourceRef)
  );
}

export function roleInput(
  name: string,
  description: string,
  permissionKeys: string[],
  allowedScopes: AccessScopeKind[],
  changeComment: string,
): AccessRoleInput {
  return {
    name: name.trim(),
    description: description.trim(),
    permissionKeys: [...new Set(permissionKeys)].sort(),
    allowedScopes: accessScopeKinds.filter((scope) =>
      allowedScopes.includes(scope),
    ),
    changeComment: changeComment.trim(),
  };
}

export function permissionI18nKey(
  permission: PermissionDefinition,
  field: "name" | "description",
): string {
  const source =
    field === "name" ? permission.nameKey : permission.descriptionKey;
  return source.startsWith("i18n:") ? `access.registry.${source.slice(5)}` : "";
}

export function subjectsOfKind(
  subjects: AccessSubject[],
  kind: AccessSubjectKind,
): AccessSubject[] {
  return subjects.filter((subject) => subject.kind === kind);
}
