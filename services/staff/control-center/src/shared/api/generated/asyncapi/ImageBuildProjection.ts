import {ImageBuildStage} from './ImageBuildStage';
interface ImageBuildProjection {
  recipeId: string;
  recipeVersion: number;
  recipeGeneration: number;
  specSha256: string;
  attempt: number;
  stage: ImageBuildStage;
  progressPercent: number;
  stagingReference?: string;
  manifestDigest?: string;
  provenanceSha256?: string;
  immutableBuildSha256: string;
  errorCode?: string;
  diagnosticCode?: string;
  diagnosticSummary?: string;
  availableAt: string;
  maximumAttempts: number;
}
export { ImageBuildProjection };