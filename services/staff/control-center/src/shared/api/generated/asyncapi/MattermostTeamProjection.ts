import {MattermostTeamStatus} from './MattermostTeamStatus';
interface MattermostTeamProjection {
  selector: string;
  displayName: string;
  slug: string;
  reservedStatus: MattermostTeamStatus;
  providerSnapshotSha256: string;
  createdAt: string;
  updatedAt: string;
  observedAt: string;
}
export { MattermostTeamProjection };