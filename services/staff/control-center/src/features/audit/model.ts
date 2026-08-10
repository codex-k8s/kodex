import type {
  AuditEvent,
  ResourceKind,
} from "@/shared/api/generated/openapi/types.gen";

export type AuditResourceKindModel = ResourceKind;

export interface AuditEventModel {
  id: string;
  action: string;
  resourceKind: string;
  resourceVersion: number;
  outcome: string;
  policyRevision: number;
  occurredAt: string;
}

export const toAuditEventModel = (value: AuditEvent): AuditEventModel => ({
  id: value.id,
  action: value.action,
  resourceKind: value.resourceKind,
  resourceVersion: value.resourceVersion,
  outcome: value.outcome,
  policyRevision: value.policyRevision,
  occurredAt: value.occurredAt,
});
