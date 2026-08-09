import {WorkspaceBackupScope} from './WorkspaceBackupScope';
import {WorkspaceBackupState} from './WorkspaceBackupState';
interface WorkspaceBackupProjection {
  scope: WorkspaceBackupScope;
  memberCount: number;
  membershipSha256: string;
  state: WorkspaceBackupState;
  attempt: number;
  generation: number;
  terminalReasonCode?: string;
  retainUntil: string;
}
export { WorkspaceBackupProjection };