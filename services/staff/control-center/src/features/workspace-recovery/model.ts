import type {
  Resource,
  WorkspaceRestoreView,
} from "@/shared/api/generated/openapi/types.gen";

export interface WorkspaceBackupModel {
  id: string;
  name: string;
  version: number;
  state: string;
  scope: string;
  memberCount: number;
  membershipSha256: string;
  retainUntil: string;
}

export interface WorkspaceRestoreModel {
  restoreRef: string;
  displayName: string;
  version: number;
  state: string;
  attempt: number;
  memberCount: number;
  updatedAt: string;
  nextActions: Array<"CANCEL" | "RETRY">;
}

export const toWorkspaceBackupModel = (
  value: Resource,
): WorkspaceBackupModel => ({
  id: value.id,
  name: value.name,
  version: value.version,
  state: value.spec.workspaceBackup?.state ?? value.state,
  scope: value.spec.workspaceBackup?.scope ?? "WORKSPACE",
  memberCount: value.spec.workspaceBackup?.memberCount ?? 0,
  membershipSha256: value.spec.workspaceBackup?.membershipSha256 ?? "",
  retainUntil: value.spec.workspaceBackup?.retainUntil ?? "",
});

export const toWorkspaceRestoreModel = (
  value: WorkspaceRestoreView,
): WorkspaceRestoreModel => ({
  restoreRef: value.restoreRef,
  displayName: value.displayName,
  version: value.version,
  state: value.state,
  attempt: value.attempt,
  memberCount: value.memberCount,
  updatedAt: value.updatedAt,
  nextActions: [...value.nextActions],
});
