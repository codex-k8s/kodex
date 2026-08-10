
interface RuntimeRevisionProjection {
  manifestSha256: string;
  imageReference: string;
  roleImageRecipeId: string;
  roleImageRecipeVersion: number;
  roleImageSpecSha256: string;
  imageBuildId: string;
  imageBuildVersion: number;
  imageBuildAttempt: number;
  imageArtifactId: string;
  imageArtifactVersion: number;
  imageManifestDigest: string;
  imageAdmissionRevision: number;
  imageAdmissionReceiptSha256: string;
  imageAdmissionReceiptOciManifestDigest: string;
  imagePolicyRevision: number;
  imagePolicySha256: string;
  imageSignatureSha256: string;
  imagePromotionReadbackSha256: string;
  roleRuntimeContractRevision: number;
  roleRuntimeContractSha256: string;
  promptProfileId: string;
  promptRevision: number;
  sessionId: string;
  roleId: string;
  chatId?: string;
  effectiveRuntimeSha256: string;
  authorityPolicyRevision: number;
}
export { RuntimeRevisionProjection };