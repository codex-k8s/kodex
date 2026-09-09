import { createHash } from 'node:crypto';
export const sha = v => createHash('sha256').update(typeof v === 'string' || Buffer.isBuffer(v) ? v : JSON.stringify(v)).digest('hex');
export const localRepository = 'registry.local.kodex/kodex/agent-runner';
export const trustedRepository = 'kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/agent-runner';
export const a = 'a'.repeat(64), b = 'b'.repeat(64), origin = 'https://fixture.invalid';
export const fixture = { projectRef: 'project', agentRef: 'agent', environmentRef: 'environment', recipeRef: 'recipe', revisionRef: 'revision1', artifactRef: 'artifact1', buildRef: 'build1', manifestDigest: `sha256:${a}`, promotedReference: `pull.fixture.invalid/roles@sha256:${a}`, promotionReceiptSHA256: a };
export function initialState() {
  const content = JSON.stringify({ name: 'Образ fixture', roleImage: { roleDefinitionRef: 'role', environment: { environmentKey: 'standard', packageKeys: [], toolKeys: [], dockerfile: `FROM ${trustedRepository}@sha256:${a}\n` } } });
  return { generation: 1, configVersion: 1, published: 'revision1', promoted: true, bound: 1, mode: '', calls: [], revisions: [{ ref: 'revision1', revision: 1, content, digest: sha(content), sourceAvailable: true, state: 'PUBLISHED' }] };
}
export function upgradeTransportFixture(state = initialState()) {
  const config = () => ({ ref: 'configuration', name: 'Образ fixture', kind: 'ROLE_IMAGE', managedBy: 'UI', projectRef: 'project', version: state.configVersion, currentRevision: state.revisions.find(r => r.ref === state.published) });
  const artifact = n => ({ ref: `artifact${n}`, recipeRef: 'recipe', recipeGeneration: n, manifestDigest: `sha256:${n === 1 ? a : b}`, provenanceSha256: b, sbomSha256: b, vulnerabilityEvidenceSha256: b, admissionVerdict: 'ACCEPTED', promotedReference: `pull.fixture.invalid/roles@sha256:${n === 1 ? a : b}`, promotionReceiptSha256: n === 1 ? a : b, promotedAt: '2026-09-09T00:00:00Z' });
  const get = async path => {
    const u = new URL(path, origin);
    if (u.pathname === '/api/v1/role-environments') return { items: [{ key: 'standard', available: true, dockerfileTemplate: `FROM ${state.catalogRepository ?? trustedRepository}@sha256:${state.mode === 'base-drift' ? a : (state.catalogDigest ?? b)}\n` }] };
    if (u.pathname === '/api/v1/agents/agent') return { ref: 'agent', projectRef: state.mode === 'foreign' ? 'foreign' : 'project', roleDefinitionRef: 'role', enabled: true, state: 'READY' };
    if (u.pathname.endsWith('/runtime-configuration')) return { environment: { ref: 'environment', currentVersion: { ref: `envv${state.bound}`, image: { reference: artifact(state.bound).promotedReference, artifactRef: `artifact${state.bound}` } } }, environmentBinding: { ref: 'binding', version: state.bound, agentRef: 'agent', environmentRef: 'environment', versionRef: `envv${state.bound}`, digest: state.bound === 1 ? a : b } };
    if (u.pathname === '/api/v1/managed-configurations/configuration/revisions') return { configuration: config(), items: state.revisions };
    if (u.pathname === '/api/v1/projects/project/role-image-recipes/recipe') return { recipe: { ref: 'recipe', roleDefinitionRef: 'role', version: state.generation, generation: state.generation, managedLineage: { managedBy: 'UI', configurationRef: 'configuration', revisionRef: state.published }, promotedImageReady: state.promoted, activeImageArtifactRef: state.promoted ? `artifact${state.generation}` : 'artifact1' }, builds: [{ ref: `build${state.generation}`, recipeRef: 'recipe', recipeGeneration: state.generation, configurationRevisionRef: state.published, stage: state.mode === 'build-failed' ? 'FAILED' : 'COMPLETED' }], promotionCandidate: artifact(state.generation), activeArtifact: artifact(state.promoted ? state.generation : 1) };
    if (u.pathname === '/api/v1/role-image-impact-plans/impact') return { items: ['agent-item', 'environment-item'].map(ref => ({ ref, projectRef: 'project', environmentRef: 'environment', outcome: state.bound === 2 ? 'APPLIED' : 'PENDING' })) };
    throw new Error('FIXTURE_ROUTE_INVALID');
  };
  const request = async (path, options = {}) => {
    const method = options.method ?? 'GET';
    if (method === 'GET') return new Response(JSON.stringify(await get(path)), { status: 200 });
    state.calls.push({ method, path, key: new Headers(options.headers).get('Idempotency-Key') });
    const body = options.body ? JSON.parse(options.body) : undefined;
    let value, status = 200;
    if (path.endsWith('/drafts')) { state.configVersion++; state.revisions.push({ ref: 'revision2', revision: 2, state: 'DRAFT', parentRevisionRef: 'revision1', content: body.content, digest: sha(body.content), sourceAvailable: true }); value = { configuration: config(), revision: state.revisions.at(-1) }; status = 201; }
    else if (path.endsWith('/validation')) { state.configVersion++; state.revisions.at(-1).state = 'VALID'; value = { configuration: config(), revision: state.revisions.at(-1) }; }
    else if (path.endsWith('/publication')) { state.configVersion++; state.revisions[0].state = 'SUPERSEDED'; state.revisions.at(-1).state = 'PUBLISHED'; state.published = 'revision2'; state.generation = 2; state.promoted = false; value = { configuration: config(), revision: state.revisions.at(-1) }; }
    else if (path.endsWith('/promotions')) { state.promoted = true; value = { ref: 'promotion2', recipeRef: 'recipe', imageArtifactRef: 'artifact2', state: 'QUEUED' }; status = 202; }
    else if (path.endsWith('/impact-plans')) { value = { ref: 'impact', digest: b, configurationRef: 'configuration', revisionRef: 'revision2', artifactRef: 'artifact2', total: 2 }; status = 201; }
    else if (path.endsWith('/consumer-bindings')) { if (state.mode !== 'missing-rebind') state.bound = 2; value = { plan: { ref: 'impact', state: 'APPLIED' } }; }
    else throw new Error('FIXTURE_MUTATION_INVALID');
    if (state.mode === 'lost-ack' && path.endsWith('/publication')) throw new Error('Synthetic response loss');
    return new Response(JSON.stringify(value), { status });
  };
  return { state, get, request };
}
