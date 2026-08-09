import {IntegrationApprovalStatus} from './IntegrationApprovalStatus';
import {IntegrationApprovalRedactedPreview} from './IntegrationApprovalRedactedPreview';
interface IntegrationApproval {
  approvalRef: string;
  invocationRef: string;
  version: number;
  reservedStatus: IntegrationApprovalStatus;
  requestHash: string;
  redactedPreview: IntegrationApprovalRedactedPreview;
  expiresAt: string;
  decidedAt?: string;
  reasonCode?: string;
}
export { IntegrationApproval };