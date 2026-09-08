// Синтетический transport для локальных тестов. Vendor effect не выполняется.
export const runtimeProofFixture = {
  projectRef: 'prj_fixture', agentRef: 'agt_fixture', environmentRef: 'renv_fixture', recipeRef: 'imgrec_fixture', revisionRef: 'mrev_fixture', artifactRef: 'imgart_fixture', buildRef: 'imgbld_fixture', manifestDigest: `sha256:${'a'.repeat(64)}`, promotedReference: `pull.fixture.invalid/kodex/roles@sha256:${'a'.repeat(64)}`, promotionReceiptSHA256: 'b'.repeat(64),
};
export function runtimeProofTransport(mode = '') {
  const f = runtimeProofFixture; const hash = 'c'.repeat(64); const catalogRevision = `mcat_${hash}`;
  const configuration = { ref: 'rcfg_fixture', agentRef: f.agentRef, version: 1, digest: hash, model: 'fixture-model', providerPolicy: { ref: 'ppol_fixture', version: 1, digest: hash, mode: 'FIXED', accountCandidates: [{ accountRef: 'pacc_fixture', providerDefinitionKey: 'openai-codex', catalogRevision, catalogDigest: hash }] } };
  if (mode === 'no-fixed-account') configuration.providerPolicy.mode = 'LEAST_USED';
  const run = { ref: 'run_fixture', projectRef: f.projectRef, sessionRef: 'ses_fixture', attempt: 1, target: { ref: f.agentRef, type: 'AGENT' }, state: 'RUNNING' };
  return (path, method = 'GET') => {
    if (method === 'POST' && path === '/api/v1/runs') return { run };
    if (path === `/api/v1/agents/${f.agentRef}`) return { ref: f.agentRef, projectRef: mode === 'foreign' ? 'prj_other' : f.projectRef, version: mode === 'stale' ? 2 : 1 };
    if (path.endsWith(`/role-image-recipes/${f.recipeRef}`)) return { recipe: { ref: f.recipeRef, version: 1, generation: 1, managedLineage: { managedBy: 'UI', revisionRef: f.revisionRef }, promotedImageReady: true, activeImageArtifactRef: f.artifactRef }, builds: [{ ref: f.buildRef, recipeRef: f.recipeRef, recipeGeneration: 1, configurationRevisionRef: f.revisionRef, stage: 'COMPLETED' }], activeArtifact: { ref: f.artifactRef, recipeRef: f.recipeRef, recipeGeneration: 1, admissionVerdict: 'ACCEPTED', manifestDigest: f.manifestDigest, promotedReference: f.promotedReference, promotionReceiptSha256: f.promotionReceiptSHA256, provenanceSha256: hash, sbomSha256: hash, vulnerabilityEvidenceSha256: hash, promotedAt: '2026-09-08T13:00:00Z' } };
    if (path.endsWith('/runtime-configuration')) return { agentVersion: mode === 'stale' ? 2 : 1, configuration, environmentBinding: { ref: 'aenv_fixture', version: 1, agentRef: f.agentRef, environmentRef: f.environmentRef, versionRef: 'renvv_fixture', digest: hash }, environment: { ref: f.environmentRef, projectRef: f.projectRef, ready: true, currentVersion: { ref: 'renvv_fixture', image: { artifactRef: f.artifactRef, reference: mode === 'wrong-image' ? 'wrong' : f.promotedReference } } } };
    if (path.includes('/effective-capabilities?')) return { agentRef: f.agentRef, agentVersion: mode === 'stale' ? 2 : 1, runtimeConfigurationRef: configuration.ref, runtimeConfigurationVersion: 1, environmentVersionRef: 'renvv_fixture', digest: hash, runtimeReady: true, items: [{ key: 'platform.artifact.manage', effective: mode !== 'no-capability' }] };
    if (path.startsWith('/api/v1/model-capabilities?')) return { catalogRevision, catalogDigest: mode === 'catalog-changed' ? 'd'.repeat(64) : hash, items: [{ id: configuration.model, providerDefinitionKey: 'openai-codex', available: true, eligibleProviderAccountRefs: ['pacc_fixture'] }] };
    if (path === '/api/v1/runs/run_fixture') return mode === 'attempt-changed' ? { ...run, attempt: 2 } : run;
    if (path === '/api/v1/runs/run_fixture/runtime-revision-diff') return { current: { ref: 'rtvr_fixture', version: 1, runRef: run.ref, sessionRef: run.sessionRef, turnRef: mode === 'missing-turn' ? undefined : 'turn_fixture', attempt: 1, revisionDigest: hash }, changes: [{ component: 'IMAGE', current: { digest: mode === 'revision-image-changed' ? 'd'.repeat(64) : f.manifestDigest } }] };
    throw new Error('SYNTHETIC_ROUTE_UNEXPECTED');
  };
}
