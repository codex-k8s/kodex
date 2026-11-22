import test from 'node:test';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {copyFileSync,mkdtempSync,readdirSync,readFileSync,rmSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {join,resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {personalIntegrationsCatalog} from './personal-integrations-migration-contract.mjs';
import {artifactGrantCatalogPostconditionSQL,artifactGrantCatalogReceipt,hasPersonalBackupArtifactMigrationChain,artifactGrantSchemaVersion,artifactGrantSchemaCommand,
 artifactGrantSchemaJobName,verifyArtifactGrantCatalogProof} from './artifact-retention-grant-catalog.mjs';
import {readStagedArtifactGrantCatalogCandidate,releaseArtifactGrantCatalog} from './artifact-retention-grant-catalog-release.mjs';
import {personalStagedInputsSchema} from './artifact-retention-issuer-inputs.mjs';
import {inspectSource} from './application-source.mjs';
import {immutableMigrationProfile} from './control-plane-immutable-migration.mjs';

const original=readFileSync(new URL('./personal-integrations-backup-migration-postcondition.sql',import.meta.url));
const originalSHA=createHash('sha256').update(original).digest('hex');
const qualified={profile:'PERSONAL_INTEGRATIONS_BACKUP_V1',database:'control_plane',readOnly:'on',
 gooseVersion:artifactGrantSchemaVersion,serverVersionNum:180003,historyMatches:true,schemaMatches:true,
 artifactGrantCatalogMatches:true,publicSequenceSafe:true,backupQualification:'CANONICAL_BACKUP',
 schemaCatalog:personalIntegrationsCatalog.backup.after};

test('исходник штатного 049 сверяет все SQL с полным manifest и закреплённым хвостом ART050 и 051',t=>{
 const personal=immutableMigrationProfile('personal-integrations-backup').file;
 const artifact='20261121000100_control_plane_artifact_retention_grant.sql';
 const workflow='20261121000200_workflow_file_provenance.sql';
 const source=fileURLToPath(new URL('../../services/internal/control-plane/cmd/cli/migrations/',import.meta.url));
 const directory=mkdtempSync(join(tmpdir(),'kodex-art-catalog-chain-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const copy=name=>copyFileSync(join(source,name),join(directory,name));
 const check=options=>hasPersonalBackupArtifactMigrationChain(directory,personal,options);
 for(const name of readdirSync(source).filter(name=>name.endsWith('.sql')&&name<=personal))copy(name);
 assert.equal(check(),false);
 assert.equal(check({allowOriginalHead:true}),true);
 copy(workflow);
 assert.equal(check(),false);
 rmSync(join(directory,workflow));
 copy(artifact);
 assert.equal(check(),true);
 copy(workflow);
 assert.equal(check(),true);
 for(const foreign of ['020261114155000_unknown.sql','20261114165000_unknown.sql','20261121000300_unknown.sql']){
  writeFileSync(join(directory,foreign),'',{mode:0o600});
  assert.equal(check(),false);
  assert.equal(check({allowOriginalHead:true}),false);
  rmSync(join(directory,foreign));
 }
 for(const altered of [artifact,workflow]){
  writeFileSync(join(directory,altered),'',{mode:0o600});
  assert.equal(check(),false);
  copy(altered);
 }
});

test('штатный кандидат читает настоящий clean source и закреплённый SQL050 из того же образа',()=>{
 const {migration}=fixture(true);
 migration.source.path=resolve(migration.source.path);
 const revision=inspectSource(migration.source.path).revision;
 migration.source.revision=revision;
 const imageRef=migration.job.spec.template.spec.containers[0].image;
 const nodeProof={imageRef,sourceSHA:revision};
 migration.imageEvidence={components:[{component:'control-plane',nodeProof}]};
 const candidate=readStagedArtifactGrantCatalogCandidate(migration,imageRef);
 assert.equal(candidate.sourceSHA,revision);
 assert.equal(candidate.imageRef,imageRef);
 assert.equal(candidate.nodeProofSHA256,fingerprint(nodeProof));
 assert.match(candidate.migrationSQLSHA256,/^[0-9a-f]{64}$/);
 nodeProof.sourceSHA='0'.repeat(40);
 assert.throws(()=>readStagedArtifactGrantCatalogCandidate(migration,imageRef),/REJECTED/);
});

test('readback включает ровно два CHECK, запись ART и полную новую Goose историю',()=>{
 const sql=artifactGrantCatalogPostconditionSQL(original,originalSHA);
 assert.match(sql,/BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY/);
 assert.match(sql,/ROLLBACK;\s*$/);
 assert.match(sql,/\[20261121000100,true\]\]\$history\$/);
 assert.match(sql,/workload_id='artifact-retention' AND credential_generation=1 AND enabled/);
 assert.equal((sql.match(/THEN 'CHECK .*?' ELSE/g)??[]).length,1);
 assert.throws(()=>artifactGrantCatalogPostconditionSQL(Buffer.concat([original,Buffer.from(' ')]),originalSHA),/REJECTED/);
 assert.equal(verifyArtifactGrantCatalogProof(qualified,true).schemaVersion,artifactGrantSchemaVersion);
 for(const bad of [{artifactGrantCatalogMatches:false},{schemaMatches:false},{historyMatches:false},{gooseVersion:20261114170000},
  {backupQualification:'NONE'},{schemaCatalog:{...qualified.schemaCatalog,tableCount:0}}])
  assert.throws(()=>verifyArtifactGrantCatalogProof({...qualified,...bad},true),/REJECTED/);
});

function fixture(staged=false){
 const migration={profile:'personal-integrations-backup',source:{path:fileURLToPath(new URL('../../',import.meta.url))},
  postconditionSQLSHA256:originalSHA,job:{apiVersion:'batch/v1',kind:'Job',metadata:{name:'old',namespace:'kodex-system',
   labels:{'app.kubernetes.io/component':'migration'}},spec:{backoffLimit:0,activeDeadlineSeconds:300,template:{metadata:{},spec:{
    serviceAccountName:'control-plane-migrator',automountServiceAccountToken:false,restartPolicy:'Never',
    containers:[{name:'migrate',command:['/usr/local/bin/control-plane-cli'],args:['old'],image:'old'}]}}}}};
 const binding={type:'PERSONAL_INTEGRATIONS_READERS_BOUND',operationID:'rotation-op',bindingSHA256:'binding',openPlanSHA256:'api'};
 const events=staged?[]:[binding,{type:'PERSONAL_ARTIFACT_GRANT_REPAIR_RESUME_ACK'}];
 const imageRef='registry.local.kodex/kodex/control-plane@sha256:'+'c'.repeat(64),jobUID='11111111-1111-4111-8111-111111111111';
 if(staged){
  migration.personalIntegrationsInputs={schema:personalStagedInputsSchema};
  migration.source.revision='a'.repeat(40);
  migration.job.spec.template.spec.containers[0].image=imageRef;
 }
 let actual;
 const f={migration,events,created:0,lostAck:false,status:'RETIRED',alteredPod:false,stagedReads:0,authorizations:0,
  joined:true,owner107:true,foreignCandidate:false,
  replaceJob:()=>{actual.metadata.uid='22222222-2222-4222-8222-222222222222';}};
 f.run=approvedSHA256=>releaseArtifactGrantCatalog({migration,journal:{events,append:value=>events.push(value)},approvedSHA256,
  normalizePodSpec:pod=>pod,readAPI:()=>{assert.equal(staged,false);return {openPlanSHA256:'api'};},
  checkMigration:()=>{},bindExecutor:()=>assert.equal(staged,false),
  readAuthority:()=>{assert.equal(staged,false);return {state:f.status,policyRevision:110,operationID:'rotation-op',bindingSHA256:'binding',observedAt:new Date().toISOString()};},
  readStaged:()=>{f.stagedReads++;assert.equal(f.joined,true,'cohort not joined');},
  authorizeStaged:()=>{f.authorizations++;assert.equal(f.owner107,true,'prior authority changed');},
  loadCandidate:()=>({imageRef,sourceSHA:(f.foreignCandidate?'d':'a').repeat(40),migrationSQLSHA256:'b'.repeat(64)}),
  readOwnerSQL:()=>qualified,successReceipt:'success receipt',
  readJobPods:uid=>{assert.equal(uid,jobUID);return [{spec:actual.spec.template.spec,status:{phase:'Succeeded',
   containerStatuses:[{name:'migrate',imageID:f.alteredPod?'foreign':imageRef,state:{terminated:{exitCode:0}}}]}}];},
  wait:async(read,accept,error)=>{const value=read();if(!accept(value))throw Error(error);return value;},
  kube:(args,body)=>{
   if(args.includes('get'))return actual?JSON.stringify(actual):'';
   if(args.includes('logs'))return 'success receipt\n';
   const job=JSON.parse(body);
   assert.equal(job.metadata.name,artifactGrantSchemaJobName);
   assert.deepEqual(job.spec.template.spec.containers[0].args,[artifactGrantSchemaCommand]);
   assert.equal(job.spec.template.spec.containers[0].image,imageRef);
   if(!args.includes('--dry-run=server')){
    f.created++;actual=job;actual.metadata.uid=jobUID;
    actual.status={succeeded:1,conditions:[{type:'Complete',status:'True'}]};
    if(f.lostAck)throw Error('synthetic lost reply');
   }
   return JSON.stringify(job);
  }});
 return f;
}

test('только один Job и readback при потере ответа create',async()=>{
 const f=fixture(),plan=await f.run();
 assert.equal(f.created,0);
 f.lostAck=true;
 await assert.rejects(f.run(plan.planSHA256),/CREATE_UNKNOWN/);
 assert.equal(artifactGrantCatalogReceipt(f.migration,f.events),undefined);
 f.alteredPod=true;
 await assert.rejects(f.run(plan.planSHA256),/REJECTED/);
 f.alteredPod=false;
 const result=await f.run(plan.planSHA256);
 assert.equal(result.status,'ARTIFACT_GRANT_CATALOG_APPLIED');
 assert.equal(f.created,1);
 assert.equal(artifactGrantCatalogReceipt(f.migration,f.events).schemaVersion,artifactGrantSchemaVersion);
 await f.run(plan.planSHA256);
 assert.equal(f.created,1);
});

test('неверный план или незавершённая ротация не создают Job',async()=>{
 const f=fixture(),plan=await f.run();
 await assert.rejects(f.run('a'.repeat(64)),/REJECTED/);
 f.status='RETIRING';
 await assert.rejects(f.run(plan.planSHA256),/REJECTED/);
 assert.equal(f.created,0);
});

test('после durable UID readback другой Job с тем же планом отклоняется',async()=>{
 const f=fixture(),plan=await f.run();
 f.alteredPod=true;
 await assert.rejects(f.run(plan.planSHA256),/REJECTED/);
 f.alteredPod=false;
 f.replaceJob();
 await assert.rejects(f.run(plan.planSHA256),/REJECTED/);
 assert.equal(f.created,1);
 assert.equal(artifactGrantCatalogReceipt(f.migration,f.events),undefined);
});

test('штатный ART050 использует STAGED и исходный образ; потерянный ACK подтверждается без повторного create',async()=>{
 const f=fixture(true),plan=await f.run();
 assert.equal(f.created,0);
 assert.equal(f.authorizations,0);
 assert.equal(f.stagedReads,1);
 f.lostAck=true;
 await assert.rejects(f.run(plan.planSHA256),/CREATE_UNKNOWN/);
 assert.equal(f.events[0].admission,'STAGED_COHORT');
 assert.equal(f.events[0].sourceSHA,f.migration.source.revision);
 assert.equal(f.authorizations,1);
 assert.equal(artifactGrantCatalogReceipt(f.migration,f.events),undefined);
 const result=await f.run(plan.planSHA256);
 assert.equal(result.schemaVersion,artifactGrantSchemaVersion);
 assert.equal(f.authorizations,1);
 assert.equal(f.created,1);
 const tampered=structuredClone(f.events);
 delete tampered[0].admission;
 assert.throws(()=>artifactGrantCatalogReceipt(f.migration,tampered),/REJECTED/);
});

test('штатный ART050 закрывает доступ при drift, другом образе, runtime или repair journal',async()=>{
 const f=fixture(true),plan=await f.run();
 f.owner107=false;
 await assert.rejects(f.run(plan.planSHA256),/prior authority changed/);
 f.owner107=true;
 f.joined=false;
 await assert.rejects(f.run(plan.planSHA256),/cohort not joined/);
 f.joined=true;
 f.foreignCandidate=true;
 await assert.rejects(f.run(plan.planSHA256),/REJECTED/);
 f.foreignCandidate=false;
 for(const type of ['PERSONAL_INTEGRATIONS_READERS_BOUND','PAIR_RESUME_INTENT','PERSONAL_ARTIFACT_GRANT_REPAIR_RESUME_ACK','BACKUP_RESUME_INTENT']){
  f.events.push({type});
  await assert.rejects(f.run(plan.planSHA256),/REJECTED/);
  f.events.pop();
 }
 assert.equal(f.created,0);
});

test('CLI отделяет штатные аргументы ART050 от позднего repair до обращения к Kubernetes',()=>{
 const cli=fileURLToPath(new URL('./control-plane-migration.mjs',import.meta.url));
 const run=(...args)=>spawnSync(process.execPath,[cli,'plan-artifact-grant-catalog','--profile','personal-integrations-backup',
  '--plan','/synthetic/plan','--evidence','/synthetic/evidence',...args],{encoding:'utf8',timeout:10000}).stderr;
 assert.match(run(),/STAGING_CONTEXT_REQUIRED/);
 assert.match(run('--artifact-grant-catalog-inputs','/synthetic/candidate'),/PHASE_ARGUMENT_INVALID/);
 assert.match(run('--personal-executor-sha','a'.repeat(40)),/PHASE_ARGUMENT_INVALID/);
});
