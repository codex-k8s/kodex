import {WorkspaceRestoreState} from './WorkspaceRestoreState';
interface WorkspaceRestoreProjection {
  backupRef: string;
  backupVersion: number;
  membershipSha256: string;
  memberCount: number;
  state: WorkspaceRestoreState;
  attempt: number;
  generation: number;
  partial: boolean;
  terminalReasonCode?: string;
}
export { WorkspaceRestoreProjection };