import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { requireIdle } from './runner-policy-model.mjs';
const query = readFileSync(new URL('./runner-policy-readback.sql', import.meta.url), 'utf8');
const diagnosticQuery = readFileSync(new URL('./runner-pending-promotions-readback.sql', import.meta.url), 'utf8');
const adapter = new URL('../../services/internal/control-plane/internal/repository/postgres/platform/sql/', import.meta.url);
test('all canonical recipe writers are monotonic; native request/claim cannot resurrect predecessor', () => {
  const names = ['role_images_update_recipe.sql','role_images_change_recipe_state.sql','role_images_refresh_recipe_policy.sql','role_images_activate_artifact.sql','runtime_configuration__activate_system_image.sql','runtime_configuration__materialize_system_image.sql'];
  const writers = readdirSync(adapter).filter(name => /UPDATE\s+control_plane\.role_image_recipes\b/.test(readFileSync(new URL(name, adapter), 'utf8')) || name === 'runtime_configuration__materialize_system_image.sql');
  assert.deepEqual(writers.sort(), names.sort());
  for (const name of names) {
    const sql = readFileSync(new URL(name, adapter), 'utf8');
    assert.match(sql, /(?:recipe\.|control_plane\.role_image_recipes\.)?version \+ 1/, name);
    const assignments = /\bSET\b([\s\S]*?)(?:\bWHERE\b|\bFROM\b)/.exec(sql)?.[1];assert.ok(assignments,name);
    assert.doesNotMatch(assignments, /(?:version|generation)\s*=\s*(?:EXCLUDED|@|\$\d)|(?:version|generation)\s*-\s*1/i, name);
    if (name === 'role_images_update_recipe.sql') assert.match(assignments,/generation = generation \+ 1/);
    else if (name === 'role_images_change_recipe_state.sql') assert.match(assignments,/generation = CASE WHEN \$3 = 'ACTIVE' THEN generation \+ 1 ELSE generation END/);
    else if (name === 'runtime_configuration__materialize_system_image.sql') assert.match(assignments,/generation = control_plane\.role_image_recipes\.generation \+ 1/);
    else assert.doesNotMatch(assignments,/\bgeneration\s*=/);
  }
  const claim = readFileSync(new URL('role_images_claim_promotion_candidate.sql', adapter), 'utf8');
  for (const pin of ['version', 'generation']) assert.ok(claim.includes(`recipe.${pin} = artifact.recipe_${pin}`));
  const request = readFileSync(new URL('../../services/internal/control-plane/internal/repository/postgres/platform/role_images_promotions.go', import.meta.url), 'utf8');
  assert.match(request, /artifact\.Artifact\.RecipeVersion == recipe\.Recipe\.Version/);
  assert.match(request, /artifact\.Artifact\.RecipeGeneration == recipe\.Recipe\.Generation/);
});
test('counter remains read-only, pin-complete and failclosed for effect-bearing/unknown promotion', () => {
  const body = query.replace(/--[^\n]*/g, '');
  assert.match(body, /BEGIN TRANSACTION READ ONLY/);assert.match(body, /statement_timeout = '10s'/);
  assert.doesNotMatch(body, /\b(?:UPDATE|INSERT|DELETE|ALTER|CREATE|DROP|GRANT|REVOKE)\b/i);
  assert.match(body, /COALESCE\([\s\S]*false\) AS superseded_unrequested/);
  assert.match(body, /recipe\.version > artifact\.recipe_version AND recipe\.generation > artifact\.recipe_generation/);
  assert.match(body, /artifact\.admission_state = 'ACCEPTED' AND artifact\.promotion_state = 'PENDING'/);
  assert.match(body, /artifact\.promotion_state IN \('CLAIMED', 'AUTHORIZED'\)/);
  assert.match(body, /artifact\.admission_state IS NULL OR artifact\.admission_state NOT IN \('PENDING', 'CLAIMED', 'ACCEPTED', 'REJECTED', 'FAILED'\)/);
  assert.match(body, /artifact\.promotion_state IS NULL OR artifact\.promotion_state NOT IN \('PENDING', 'CLAIMED', 'AUTHORIZED', 'PROMOTED', 'REJECTED'\)/);
  assert.match(body, /'supersededUnrequestedPromotions', \(SELECT count\(\*\) FROM promotion_candidates WHERE superseded_unrequested\)/);
});
const image = 'docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7';
test('canonical admission terminal mappings distinguish technical failure from promotion effect', () => {
  const source = name => readFileSync(new URL(name, adapter), 'utf8');
  const baseline = readFileSync(new URL('../../services/internal/control-plane/cmd/cli/migrations/20260822000100_web_first_baseline.sql', import.meta.url), 'utf8');
  const technical = readFileSync(new URL('../../services/internal/control-plane/cmd/cli/migrations/20261005000100_image_admission_technical_failure.sql', import.meta.url), 'utf8');
  assert.match(baseline,/promotion_state text NOT NULL DEFAULT 'PENDING'/);
  assert.match(technical,/admission_state IN \('PENDING', 'CLAIMED', 'ACCEPTED', 'REJECTED', 'FAILED'\)/);
  for (const name of ['role_images_fail_admission.sql','role_images_expire_admissions.sql']) {
    assert.match(source(name),/admission_state = 'FAILED'/);assert.doesNotMatch(source(name),/promotion_state\s*=/);
  }
  assert.match(source('role_images_record_admission.sql'),/promotion_state = CASE WHEN \$4 = 'ACCEPTED' THEN 'PENDING' ELSE 'REJECTED' END/);
  assert.match(source('role_images_reject_stale_admission_candidates.sql'),/promotion_state = 'REJECTED'/);
  assert.match(source('role_images_risk_restart_admission.sql'),/promotion_state='REJECTED'/);
});
test('actual disposable PostgreSQL counter excludes only strict superseded unrequested tuple without modifying history', {skip:process.env.KODEX_TEST_RUNNER_POLICY_POSTGRES !== '1',timeout:90000}, async () => {
  const directory = mkdtempSync(join(tmpdir(), 'runner-policy-idle-pg.'));let container;
  const env = {PATH:process.env.PATH,DOCKER_CONFIG:directory};
  if (process.env.DOCKER_HOST) {assert.match(process.env.DOCKER_HOST,/^unix:\/\//);env.DOCKER_HOST=process.env.DOCKER_HOST;}
  const docker = (...args) => execFileSync('docker', args, {env,encoding:'utf8',timeout:45000,maxBuffer:1<<20,stdio:['pipe','pipe','pipe']});
  const sql = input => execFileSync('docker', ['exec','-i',container,'psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','postgres','-d','postgres'], {env,input,encoding:'utf8',timeout:15000,maxBuffer:1<<20,stdio:['pipe','pipe','pipe']});
  try {
    assert.match(docker('context','inspect','--format','{{.Endpoints.docker.Host}}'),/^unix:\/\//);
    assert.equal(docker('info','--format','{{.OSType}}').trim(),'linux');docker('image','inspect',image);
    writeFileSync(join(directory,'password'),randomUUID(),{mode:0o600});
    container=docker('run','--pull=never','--rm','-d','--network','none','--memory','256m','--cpus','1','--name','kodex-runner-idle-'+randomUUID(),'--label','kodex.dev/disposable-test=runner-policy-readback','--mount',`type=bind,src=${directory},dst=/run/test-fixture,readonly`,'--tmpfs','/var/lib/postgresql:rw,size=268435456','-e','POSTGRES_PASSWORD_FILE=/run/test-fixture/password',image).trim();
    assert.match(container,/^[a-f0-9]{64}$/);
    for (let attempt=0;;attempt++) {try {docker('exec',container,'pg_isready','-h','127.0.0.1','-U','postgres');break;}catch {assert.ok(attempt<20,'Disposable PostgreSQL startup exhausted');await new Promise(resolve=>setTimeout(resolve,250));}}
    sql(`CREATE SCHEMA control_plane;
CREATE TABLE control_plane.organizations(id text PRIMARY KEY);
CREATE TABLE control_plane.projects(id text PRIMARY KEY,organization_id text);
CREATE TABLE control_plane.role_image_recipes(id text PRIMARY KEY,organization_id text DEFAULT 'org',project_id text,scope_kind text DEFAULT 'ORGANIZATION',state text DEFAULT 'ACTIVE',version bigint DEFAULT 5,generation bigint DEFAULT 4,spec_sha256 text DEFAULT repeat('a',64),policy_revision bigint DEFAULT 1,policy_sha256 text DEFAULT repeat('a',64),role_runtime_contract_revision bigint DEFAULT 1,role_runtime_contract_sha256 text DEFAULT repeat('a',64));
CREATE TABLE control_plane.image_builds(id text PRIMARY KEY,organization_id text DEFAULT 'org',project_id text,scope_kind text DEFAULT 'ORGANIZATION',recipe_id text DEFAULT 'recipe',version bigint DEFAULT 11,recipe_version bigint DEFAULT 3,recipe_generation bigint DEFAULT 3,spec_sha256 text DEFAULT repeat('a',64),attempt integer DEFAULT 1,stage text DEFAULT 'COMPLETED');
CREATE TABLE control_plane.image_artifacts(id text PRIMARY KEY,organization_id text DEFAULT 'org',project_id text,scope_kind text DEFAULT 'ORGANIZATION',recipe_id text DEFAULT 'recipe',build_id text DEFAULT 'build',version bigint DEFAULT 2,recipe_version bigint DEFAULT 3,recipe_generation bigint DEFAULT 3,build_version bigint DEFAULT 11,build_attempt integer DEFAULT 1,spec_sha256 text DEFAULT repeat('a',64),policy_revision bigint DEFAULT 1,policy_sha256 text DEFAULT repeat('a',64),role_runtime_contract_revision bigint DEFAULT 1,role_runtime_contract_sha256 text DEFAULT repeat('a',64),admission_state text DEFAULT 'ACCEPTED',admission_verdict text DEFAULT 'ACCEPTED',promotion_state text DEFAULT 'PENDING',promotion_request_id text,promoted_reference text DEFAULT '',promotion_fence bigint DEFAULT 0,promotion_authority_generation bigint DEFAULT 0,promotion_claimant_workload text,promotion_claim_token_sha256 text,promotion_claim_expires_at timestamptz,promotion_authorization_token_sha256 text,promotion_authorization_expires_at timestamptz,manifest_digest text DEFAULT 'sha256:'||repeat('a',64),provenance_sha256 text DEFAULT repeat('a',64),immutable_build_sha256 text DEFAULT repeat('a',64),sbom_sha256 text DEFAULT repeat('a',64),vulnerability_evidence_sha256 text DEFAULT repeat('a',64),signature_identity text DEFAULT 'synthetic-signature',signature_sha256 text DEFAULT repeat('a',64),admission_revision bigint DEFAULT 1,admission_receipt_sha256 text DEFAULT repeat('a',64),admission_receipt_oci_manifest_digest text DEFAULT 'sha256:'||repeat('a',64));
CREATE TABLE control_plane.role_image_promotion_requests(id text PRIMARY KEY,image_artifact_id text,state text);
CREATE TABLE control_plane.runs(state text);CREATE TABLE control_plane.runtime_leases(state text);
ALTER TABLE control_plane.organizations ADD COLUMN ref text DEFAULT 'org-ref';
ALTER TABLE control_plane.projects ADD COLUMN ref text DEFAULT 'project-ref';
ALTER TABLE control_plane.role_image_recipes ADD COLUMN ref text DEFAULT 'recipe-ref';
ALTER TABLE control_plane.image_artifacts ADD COLUMN ref text DEFAULT 'artifact-ref';
ALTER TABLE control_plane.image_builds ADD COLUMN ref text DEFAULT 'build-ref',ADD COLUMN created_at timestamptz DEFAULT now(),ADD COLUMN updated_at timestamptz DEFAULT now();
ALTER TABLE control_plane.role_image_promotion_requests ADD COLUMN ref text DEFAULT 'request-ref',ADD COLUMN organization_id text DEFAULT 'org',ADD COLUMN project_id text,ADD COLUMN recipe_id text DEFAULT 'recipe',ADD COLUMN expected_provenance_sha256 text DEFAULT repeat('a',64),ADD COLUMN manifest_digest text DEFAULT 'sha256:'||repeat('a',64),ADD COLUMN receipt_sha256 text DEFAULT repeat('a',64);
INSERT INTO control_plane.organizations VALUES('org');INSERT INTO control_plane.projects VALUES('project','org');
INSERT INTO control_plane.role_image_recipes(id) VALUES('recipe');INSERT INTO control_plane.image_builds(id) VALUES('build');INSERT INTO control_plane.image_artifacts(id) VALUES('artifact');
INSERT INTO control_plane.image_artifacts(id,promotion_state,promoted_reference) SELECT 'promoted-'||n,'PROMOTED','synthetic-pull-'||n FROM generate_series(1,2) n;
CREATE TABLE baseline_recipes AS TABLE control_plane.role_image_recipes;CREATE TABLE baseline_builds AS TABLE control_plane.image_builds;CREATE TABLE baseline_artifacts AS TABLE control_plane.image_artifacts;`);
    const baseline=JSON.parse(sql(query));assert.equal(baseline.promotedArtifactCount,2);
    const scenarios = {
      superseded:['',0],current:["UPDATE control_plane.role_image_recipes SET version=3,generation=3",1],
      'version-only':["UPDATE control_plane.role_image_recipes SET generation=3",1], 'generation-only':["UPDATE control_plane.role_image_recipes SET version=3",1],
      rollback:["UPDATE control_plane.role_image_recipes SET version=2,generation=2",1],
      requested:["UPDATE control_plane.image_artifacts SET promotion_request_id='request'",1],
      'orphan-request':["INSERT INTO control_plane.role_image_promotion_requests(id,image_artifact_id,state) VALUES('request','artifact','QUEUED')",1],
      claimed:["UPDATE control_plane.image_artifacts SET promotion_state='CLAIMED'",1],authorized:["UPDATE control_plane.image_artifacts SET promotion_state='AUTHORIZED'",1],
      'unknown-state':["UPDATE control_plane.image_artifacts SET promotion_state='UNKNOWN'",1], 'null-state':["UPDATE control_plane.image_artifacts SET promotion_state=NULL",1],
      'unknown-admission':["UPDATE control_plane.image_artifacts SET admission_state='UNKNOWN'",1], 'null-admission':["UPDATE control_plane.image_artifacts SET admission_state=NULL",1],
      'rejected-admission-initial-pending':["UPDATE control_plane.image_artifacts SET admission_state='REJECTED',admission_verdict='REJECTED'",0],
      'failed-admission-initial-pending':["UPDATE control_plane.image_artifacts SET admission_state='FAILED',admission_verdict=''",0],
      'pending-admission-initial-pending':["UPDATE control_plane.image_artifacts SET admission_state='PENDING',admission_verdict=''",0],
      'claimed-admission-initial-pending':["UPDATE control_plane.image_artifacts SET admission_state='CLAIMED',admission_verdict=''",0],
      'rejected-admission-current-pending':["UPDATE control_plane.image_artifacts SET admission_state='REJECTED';UPDATE control_plane.role_image_recipes SET version=3,generation=3",0],
      'failed-admission-current-pending':["UPDATE control_plane.image_artifacts SET admission_state='FAILED';UPDATE control_plane.role_image_recipes SET version=3,generation=3",0],
      'rejected-admission-promotion-claimed':["UPDATE control_plane.image_artifacts SET admission_state='REJECTED',promotion_state='CLAIMED'",1],
      'failed-admission-promotion-authorized':["UPDATE control_plane.image_artifacts SET admission_state='FAILED',promotion_state='AUTHORIZED'",1],
      'rejected-admission-requested':["UPDATE control_plane.image_artifacts SET admission_state='REJECTED',promotion_request_id='missing-request'",1],
      'failed-admission-open-request':["UPDATE control_plane.image_artifacts SET admission_state='FAILED';INSERT INTO control_plane.role_image_promotion_requests(id,image_artifact_id,state) VALUES('request','artifact','QUEUED')",1],
      'rejected-admission-unknown-request':["UPDATE control_plane.image_artifacts SET admission_state='REJECTED';INSERT INTO control_plane.role_image_promotion_requests(id,image_artifact_id,state) VALUES('request','artifact','UNKNOWN')",1],
      'failed-admission-claim-token':["UPDATE control_plane.image_artifacts SET admission_state='FAILED',promotion_claim_token_sha256=repeat('a',64)",1],
      'rejected-admission-authorization-token':["UPDATE control_plane.image_artifacts SET admission_state='REJECTED',promotion_authorization_token_sha256=repeat('a',64)",1],
      'pin-null':["UPDATE control_plane.image_artifacts SET spec_sha256=NULL",1], 'pin-corrupt':["UPDATE control_plane.role_image_recipes SET policy_sha256='unknown'",1],
      'recipe-pin-null':["UPDATE control_plane.role_image_recipes SET version=NULL",1], 'artifact-generation-invalid':["UPDATE control_plane.image_artifacts SET recipe_generation=0",1],
      'owner-foreign':["UPDATE control_plane.role_image_recipes SET organization_id='foreign'",1], 'build-foreign':["UPDATE control_plane.image_builds SET organization_id='foreign'",1],
      'scope-foreign':["UPDATE control_plane.image_builds SET scope_kind='PROJECT'",1], 'scope-unknown':["UPDATE control_plane.image_artifacts SET scope_kind='UNKNOWN'",1],
      'project-mismatch':["UPDATE control_plane.image_artifacts SET project_id='project'",1],
      'build-pin':["UPDATE control_plane.image_builds SET recipe_generation=2",1], 'build-version':["UPDATE control_plane.image_builds SET version=12",1], 'build-running':["UPDATE control_plane.image_builds SET stage='BUILDING'",1],
      'promoted-ref':["UPDATE control_plane.image_artifacts SET promoted_reference='synthetic-promoted-reference'",1],
      'claim-leftover':["UPDATE control_plane.image_artifacts SET promotion_fence=1",1], 'authorization-leftover':["UPDATE control_plane.image_artifacts SET promotion_authorization_token_sha256=repeat('a',64)",1],
      'evidence-missing':["UPDATE control_plane.image_artifacts SET sbom_sha256=''",1], 'verdict-unknown':["UPDATE control_plane.image_artifacts SET admission_verdict='PENDING'",1],
      'recipe-archived':["UPDATE control_plane.role_image_recipes SET state='ARCHIVED'",1],
      promoted:["UPDATE control_plane.image_artifacts SET promotion_state='PROMOTED',promoted_reference='synthetic-promoted-reference'",0], rejected:["UPDATE control_plane.image_artifacts SET promotion_state='REJECTED'",0],
      'promoted-open-request':["UPDATE control_plane.image_artifacts SET promotion_state='PROMOTED';INSERT INTO control_plane.role_image_promotion_requests(id,image_artifact_id,state) VALUES('request','artifact','QUEUED')",1],
      'rejected-open-request':["UPDATE control_plane.image_artifacts SET promotion_state='REJECTED';INSERT INTO control_plane.role_image_promotion_requests(id,image_artifact_id,state) VALUES('request','artifact','PROMOTING')",1],
      'terminal-unknown-request':["UPDATE control_plane.image_artifacts SET promotion_state='REJECTED';INSERT INTO control_plane.role_image_promotion_requests(id,image_artifact_id,state) VALUES('request','artifact','UNKNOWN')",1],
      'project-superseded':["UPDATE control_plane.role_image_recipes SET scope_kind='PROJECT',project_id='project';UPDATE control_plane.image_builds SET scope_kind='PROJECT',project_id='project';UPDATE control_plane.image_artifacts SET scope_kind='PROJECT',project_id='project'",0],
    };
    for (const [name,[change,pending]] of Object.entries(scenarios)) {
      const exactChange=change.split(';').map(statement=>statement.startsWith('UPDATE control_plane.image_artifacts ')?statement+" WHERE id='artifact'":statement).join(';');
      sql(`TRUNCATE control_plane.role_image_recipes,control_plane.image_builds,control_plane.image_artifacts,control_plane.role_image_promotion_requests;INSERT INTO control_plane.role_image_recipes SELECT * FROM baseline_recipes;INSERT INTO control_plane.image_builds SELECT * FROM baseline_builds;INSERT INTO control_plane.image_artifacts SELECT * FROM baseline_artifacts;${exactChange};`);
      const digest = () => sql("SELECT md5((SELECT jsonb_agg(to_jsonb(t))::text FROM control_plane.role_image_recipes t)||(SELECT jsonb_agg(to_jsonb(t))::text FROM control_plane.image_builds t)||(SELECT jsonb_agg(to_jsonb(t))::text FROM control_plane.image_artifacts t)||COALESCE((SELECT jsonb_agg(to_jsonb(t))::text FROM control_plane.role_image_promotion_requests t),'[]'))").trim();
      const before=digest(),state=JSON.parse(sql(query));assert.equal(state.pendingPromotions,pending,name);assert.equal(digest(),before,`${name}: readback mutated history`);
      const newlyPromoted=['promoted','promoted-open-request'].includes(name);assert.equal(state.promotedArtifactCount,newlyPromoted?3:2,name);if(!newlyPromoted) assert.equal(state.promotedPinsSHA256,baseline.promotedPinsSHA256,name);
      assert.equal(state.supersededUnrequestedPromotions,['superseded','project-superseded'].includes(name)?1:0,name);
      const diagnostic=JSON.parse(sql(diagnosticQuery));assert.equal(diagnostic.version,2,name);
      assert.equal(diagnostic.status,'OBSERVED',name);assert.equal(diagnostic.complete,true,name);
      assert.equal(diagnostic.total,pending+state.supersededUnrequestedPromotions,name);
      assert.equal(diagnostic.items.length,diagnostic.total,name);assert.equal(digest(),before,`${name}: diagnostic mutated history`);
      const admissionPending=['pending-admission-initial-pending','claimed-admission-initial-pending'].includes(name);
      assert.equal(state.pendingAdmissions,admissionPending?1:0,name);
      if(pending||admissionPending) assert.throws(()=>requireIdle(state),/FRESH_IDLE_OWNER_STATE_REQUIRED/,name);else assert.doesNotThrow(()=>requireIdle(state),name);
    }
  } finally {
    if (container && /^[a-f0-9]{64}$/.test(container)) docker('stop','--time','5',container);
    rmSync(directory,{recursive:true,force:true});
  }
});
