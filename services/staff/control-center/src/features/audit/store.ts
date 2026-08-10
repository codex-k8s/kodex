import { defineStore } from "pinia";
import { reactive } from "vue";

import { fetchAudit } from "@/shared/api/adapters/operations";
import { downloadAudit } from "@/shared/api/adapters/owner-control";
import {
  toAuditEventModel,
  type AuditEventModel,
  type AuditResourceKindModel,
} from "@/features/audit/model";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { remoteState, resetRemoteState } from "@/shared/lib/remote";

export const useAuditStore = defineStore("audit", () => {
  const audit = reactive(remoteState<AuditEventModel[]>([]));
  const runtime = createFeatureRuntime();
  const load = () =>
    runtime.loadInto(
      audit,
      async () => (await fetchAudit()).events.map(toAuditEventModel),
      (items) => items.length === 0,
    );
  const exportFile = (filters?: {
    resourceKind?: AuditResourceKindModel;
    resourceRef?: string;
    action?: string;
  }) => downloadAudit(filters);
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(audit, []);
  }
  return { audit, load, exportFile, reset };
});
