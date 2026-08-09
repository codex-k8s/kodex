import {RuntimeIncidentKind} from './RuntimeIncidentKind';
import {IncidentState} from './IncidentState';
import {IncidentSeverity} from './IncidentSeverity';
import {OwnerDisplayValue} from './OwnerDisplayValue';
import {IncidentNextAction} from './IncidentNextAction';
interface IncidentProjection {
  incidentRef: string;
  version: number;
  kind: RuntimeIncidentKind;
  state: IncidentState;
  severity: IncidentSeverity;
  impact: string;
  workspace: OwnerDisplayValue;
  run: OwnerDisplayValue;
  safeCorrelation: string;
  diagnosticSummary: string;
  runbookUrl: string;
  occurredAt: string;
  updatedAt: string;
  nextActions: IncidentNextAction[];
  executionFence: number;
}
export { IncidentProjection };