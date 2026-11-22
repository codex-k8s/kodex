import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {componentCohortFixture} from './fixtures/component-cohort-synthetic.mjs';
import {planComponentMigrationHandoff} from './portable-component-migration.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {fingerprint} from './scoped-release.mjs';
import {personalIntegrationsCohort049Proof,readPersonalIntegrationsCohort049} from './personal-integrations-staged-policy.mjs';
import {artifactGrantSchemaJobName,artifactGrantSchemaReceiptType,artifactGrantSchemaVersion} from './artifact-retention-grant-catalog.mjs';
import {personalStagedInputsSchema} from './artifact-retention-issuer-inputs.mjs';

// Заменяется только результат actual inspect. Фактические UID/DDL/signatures
// этим тестом не квалифицируются и не используются как live admission.
function fixture(phase='STAGED',profile='personal-integrations'){
 const f=componentCohortFixture(profile),source={path:'/synthetic/source',revision:'a'.repeat(40)};
 const migration={version:2,kind:'CONTROL_PLANE_MIGRATION',id:'synthetic-id',profile,migrationVersion:'20261114170000',
  source,context:f.value.context,kubeconfig:'/synthetic/kubeconfig',portableTarget:f.target,
  imageEvidence:f.value,postconditionSQLSHA256:'b'.repeat(64),
  componentHandoff:planComponentMigrationHandoff(f.value,f.deployments,source)};
 const rows=[{type:'HEADER',kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(migration)},
  {type:'STOP_ACK'},{type:'READBACK',status:'PASS',schemaPostcondition:'PASS',sourceRevision:source.revision,migrationVersion:'20261114170000',jobUID:'synthetic-job'}];
 if(phase==='STAGED')rows.push({type:'PAIR_HANDOFF_ACK',publicationSHA256:migration.componentHandoff.publicationSHA256});
 const scope={source:source.path,revision:source.revision,context:migration.context,kubeconfig:migration.kubeconfig,
  transition:{...Object.fromEntries(['clusterUID','namespaceUID','installationUID','installationSHA256'].map(key=>[key,f.target[key]])),
   profile:personalIntegrationsAuthorityTransition,targetSHA256:'c'.repeat(64)}};
 const inspection={status:'PASS',migrationVersion:'20261114170000',sourceRevision:source.revision,
  schemaPostcondition:'PASS',cpStage:phase,podsGone:true,cpReady:false,pairReady:false,jobUID:'synthetic-job',
  componentPair:migration.componentHandoff.components.map(part=>({name:part.name,stage:phase==='STAGED'?'stagedSpec':'stoppedSpec',joined:true,ready:false}))};
 return {migration,rows,scope,inspection,read:()=>personalIntegrationsCohort049Proof(migration,rows,inspection,scope,phase)};
}

test('STOPPED/STAGED049 разделяют asset prestage и policy stage, оба требуют девять UID и общий join',()=>{
 for(const phase of ['STOPPED','STAGED'])for(const profile of ['personal-integrations','personal-integrations-backup']){
  const f=fixture(phase,profile),proof=f.read();
  assert.equal(proof.components.length,9);assert.equal(proof.phase,phase);assert.equal(proof.profile,profile);
  assert.equal(proof.migrationTargetSHA256,fingerprint(f.migration.portableTarget));
  assert.equal(Object.hasOwn(proof,'authorityTargetSHA256'),false);
  assert.equal(proof.components.find(part=>part.name==='artifact-retention').observedSpecSHA256,
   fingerprint(f.migration.componentHandoff.components.find(part=>part.name==='artifact-retention')[phase==='STAGED'?'stagedSpec':'stoppedSpec']));
 }
});

test('живой ART, incomplete/foreign owner result, early resume и другой namespace не дают stage proof',()=>{
 for(const phase of ['STOPPED','STAGED'])for(const mutate of [f=>f.inspection.componentPair.find(part=>part.name==='artifact-retention').joined=false,
  f=>f.inspection.componentPair.pop(),f=>f.inspection.componentPair[0].ready=true,f=>f.inspection.schemaPostcondition='FAIL',
  f=>f.inspection.migrationVersion='20261114160000',f=>f.inspection.sourceRevision='d'.repeat(40),
  f=>f.inspection.podsGone=false,f=>f.rows.push({type:'RESUME_INTENT'}),f=>f.rows.push({type:'PAIR_RESUME_INTENT'}),
  f=>f.rows[2].jobUID='foreign',f=>f.scope.transition.namespaceUID='foreign',f=>f.migration.kubeconfig='/foreign']){
  const f=fixture(phase);mutate(f);assert.throws(f.read);
 }
 const f=fixture('STOPPED');f.rows.push({type:'PAIR_HANDOFF_INTENT'});assert.throws(f.read,/JOURNAL/);
});

test('STAGED новой поставки связывает исходный 049 и проверенный ART050 до rotation',()=>{
 const f=fixture('STAGED','personal-integrations-backup');
 f.migration.personalIntegrationsInputs={schema:personalStagedInputsSchema};
 const migrationPlanSHA256=fingerprint(f.migration);
 f.rows[0].planSHA256=migrationPlanSHA256;
 assert.throws(f.read,/ARTIFACT_GRANT_CATALOG_RECEIPT_REQUIRED/);
 const jobUID='11111111-1111-4111-8111-111111111111',planSHA256='e'.repeat(64),jobSpecSHA256='f'.repeat(64),sqlSHA256='a'.repeat(64);
 f.rows.push({type:'PERSONAL_ARTIFACT_GRANT_CATALOG_CREATE_INTENT',admission:'STAGED_COHORT',sourceSHA:f.migration.source.revision,planSHA256,jobSpecSHA256},
  {type:'PERSONAL_ARTIFACT_GRANT_CATALOG_CREATE_ACK',jobUID,planSHA256,jobSpecSHA256},
  {type:artifactGrantSchemaReceiptType,jobUID,planSHA256,jobSpecSHA256,migrationPlanSHA256,sqlSHA256,
   schemaVersion:artifactGrantSchemaVersion,jobName:artifactGrantSchemaJobName,schemaCatalogSHA256:'b'.repeat(64)});
 assert.throws(f.read,/ARTIFACT_GRANT_CATALOG_RECEIPT_REQUIRED/);
 f.inspection.servingSchema={version:artifactGrantSchemaVersion,schemaProofSHA256:sqlSHA256,jobUID};
 assert.deepEqual(f.read().artifactGrantCatalog,{jobUID,schemaVersion:artifactGrantSchemaVersion,planSHA256,sqlSHA256});
 f.inspection.servingSchema.schemaProofSHA256='c'.repeat(64);
 assert.throws(f.read,/ARTIFACT_GRANT_CATALOG_RECEIPT_REQUIRED/);
});

test('production reader uses fixed inspect and refuses active writer lock before transport',t=>{
 const f=fixture(),directory=mkdtempSync(join(tmpdir(),'kodex-personal-stage-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const paths={planPath:join(directory,'migration.json'),evidencePath:join(directory,'migration.jsonl')};
 writeFileSync(paths.planPath,JSON.stringify(f.migration),{mode:0o600});
 writeFileSync(paths.evidencePath,f.rows.map(row=>JSON.stringify(row)+'\n').join(''),{mode:0o600});
 let reads=0;const inspect=()=>{reads++;return f.inspection;};
 assert.equal(readPersonalIntegrationsCohort049(paths,f.scope,{inspect}).proof.phase,'STAGED');assert.equal(reads,1);
 writeFileSync(paths.evidencePath+'.lock','synthetic',{mode:0o600});
 assert.throws(()=>readPersonalIntegrationsCohort049(paths,f.scope,{inspect}),/WRITER_ACTIVE/);assert.equal(reads,1);
});
