import {RuntimeIncidentKind} from './RuntimeIncidentKind';
interface RuntimeIncident {
  incidentId: string;
  executionId: string;
  executionFence: number;
  kind: RuntimeIncidentKind;
  evidenceSha256: string;
  workloadId: string;
  occurredAt: string;
}
export { RuntimeIncident };