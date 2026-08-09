import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  commandWorkspaceBackup,
  commandWorkspaceRestore,
  fetchWorkspaceBackups,
  fetchWorkspaceRestores,
} from "@/shared/api/adapters/owner-control";
import type {
  Resource,
  WorkspaceBackupScope,
  WorkspaceRestoreView,
} from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { invalidate, remoteState, resetRemoteState } from "@/shared/lib/remote";

export const useWorkspaceRecoveryStore = defineStore(
  "workspace-recovery",
  () => {
    const workspaceBackups = reactive(remoteState<Resource[]>([]));
    const workspaceRestores = reactive(remoteState<WorkspaceRestoreView[]>([]));
    const runtime = createFeatureRuntime();
    const loadWorkspaceRecovery = () =>
      Promise.all([
        runtime.loadInto(
          workspaceBackups,
          async () => (await fetchWorkspaceBackups()).resources,
          (items) => items.length === 0,
        ),
        runtime.loadInto(
          workspaceRestores,
          async () => (await fetchWorkspaceRestores()).restores,
          (items) => items.length === 0,
        ),
      ]).then(() => undefined);
    const createBackup = (
      name: string,
      scope: WorkspaceBackupScope,
      retainUntil: string,
    ) =>
      runtime.mutate(
        () =>
          commandWorkspaceBackup({
            action: "CREATE",
            name,
            scope,
            retainUntil,
          }),
        loadWorkspaceRecovery,
        workspaceBackups,
      );
    const executeBackup = (
      backup: Resource,
      action: "CANCEL" | "RETRY",
      terminalReasonCode?: string,
    ) => {
      if (action === "CANCEL" && !terminalReasonCode)
        return Promise.resolve(false);
      return runtime.mutate(
        () =>
          commandWorkspaceBackup(
            action === "CANCEL"
              ? {
                  action,
                  backupRef: backup.id,
                  terminalReasonCode,
                }
              : { action, backupRef: backup.id },
            backup.version,
          ),
        loadWorkspaceRecovery,
        workspaceBackups,
      );
    };
    const createRestore = (backup: Resource, name: string) => {
      const projection = backup.spec.workspaceBackup;
      if (!projection) return Promise.resolve(false);
      return runtime.mutate(
        () =>
          commandWorkspaceRestore({
            action: "CREATE",
            backupRef: backup.id,
            backupVersion: backup.version,
            membershipSha256: projection.membershipSha256,
            name,
          }),
        loadWorkspaceRecovery,
        workspaceRestores,
      );
    };
    const executeRestore = (
      restore: WorkspaceRestoreView,
      action: "CANCEL" | "RETRY",
      terminalReasonCode?: string,
    ) => {
      if (action === "CANCEL" && !terminalReasonCode)
        return Promise.resolve(false);
      return runtime.mutate(
        () =>
          commandWorkspaceRestore(
            action === "CANCEL"
              ? {
                  action,
                  restoreRef: restore.restoreRef,
                  terminalReasonCode,
                }
              : { action, restoreRef: restore.restoreRef },
            restore.version,
          ),
        loadWorkspaceRecovery,
        workspaceRestores,
      );
    };
    function replaceBackups(items: Resource[]): void {
      invalidate(workspaceBackups);
      workspaceBackups.data = items.filter(
        (item) => item.kind === "WORKSPACE_BACKUP",
      );
      workspaceBackups.phase = workspaceBackups.data.length ? "ready" : "empty";
    }
    function reset(): void {
      runtime.invalidate();
      resetRemoteState(workspaceBackups, []);
      resetRemoteState(workspaceRestores, []);
    }
    return {
      workspaceBackups,
      workspaceRestores,
      mutationProblem: runtime.mutationProblem,
      mutating: runtime.mutating,
      loadWorkspaceRecovery,
      createBackup,
      executeBackup,
      createRestore,
      executeRestore,
      replaceBackups,
      reset,
    };
  },
);
