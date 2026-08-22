import {IncidentSeverity} from './IncidentSeverity';
import {IncidentState} from './IncidentState';
interface Incident {
  ref: string;
  projectRef?: string;
  runRef?: string;
  category: string;
  severity: IncidentSeverity;
  state: IncidentState;
  safeSummary: string;
  safeNextStep: string;
  coreAffected: boolean;
  createdAt: string;
}
export { Incident };