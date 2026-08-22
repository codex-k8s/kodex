import {ArtifactScanState} from './ArtifactScanState';
import {NextAction} from './NextAction';
interface Artifact {
  ref: string;
  version: number;
  projectRef: string;
  runRef?: string;
  fileName: string;
  mediaType: string;
  sizeBytes: number;
  scanState: ArtifactScanState;
  source: string;
  revision: number;
  agentBindings: string[];
  previewAvailable: boolean;
  createdAt: string;
  nextActions: NextAction[];
}
export { Artifact };