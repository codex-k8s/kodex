import {ArtifactScanStatus} from './ArtifactScanStatus';
interface ArtifactProjection {
  artifactKind: string;
  direction: string;
  sizeBytes: number;
  mediaType: string;
  sha256: string;
  scanStatus: ArtifactScanStatus;
  scanPolicyRevision: number;
  scanEvidenceSha256?: string;
  scannedAt?: string;
}
export { ArtifactProjection };