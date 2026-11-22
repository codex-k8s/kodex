import type { KubernetesFileInputView } from "../api/generated/openapi/types.gen";
// Синтетическая metadata без private source bytes, native credentials и production locators.
export function fileInputFixture(): KubernetesFileInputView {
  return {
    sourceRef: "kfs_synthetic",
    invocationRef: "inv_synthetic",
    planRef: "kpl_synthetic",
    childRef: "kch_synthetic",
    streamRef: "kst_synthetic",
    state: "READY",
    version: 2,
    artifactRef: "art_synthetic",
    artifactRevisionRef: "arv_synthetic",
    artifactSHA256: "a".repeat(64),
    artifactSizeBytes: 4,
    transferFormat: "FILE",
    normalizedSHA256: "b".repeat(64),
    normalizedSizeBytes: 2048,
    normalizationRevision: 1,
    stagedBytes: 1024,
    acknowledgedBytes: 0,
    eofAcknowledged: false,
    reason: "NONE",
    createdAt: "2026-11-17T00:00:00Z",
    cleanup: null,
    hardDeadline: "2026-11-17T00:10:00Z",
  };
}
