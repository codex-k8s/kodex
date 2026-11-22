import {personalIntegrationsReleaseReferences} from './personal-integrations-deployment-inputs.mjs';
import {personalIntegrationsAuthorityPredecessorPins} from './personal-integrations-authority-predecessor.mjs';
import {issuerAssetsResourceScope,issuerAssetsResourcesSchema} from './artifact-retention-issuer-assets.mjs';
import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {pathToFileURL} from 'node:url';
import {syntheticPersonalExecutor} from './fixtures/personal-integrations-executor-synthetic.mjs';
import {baselineFixture} from './fixtures/catalog-authority-baseline-synthetic.mjs';
import {telegramPolicySource,telegramRegistrySource,telegramAuthorityReadbackContract} from './authority-telegram-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';
import {deriveRegistryRotationIdentity} from './authority-rotation-transition.mjs';
import {readCatalogTarget} from './authority-catalog-transition.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {readPersonalIntegrationsAuthorityPredecessor} from './personal-integrations-authority-predecessor.mjs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {planComponentMigrationHandoff} from './portable-component-migration.mjs';
import {replaceComponentLock} from './fixtures/component-cohort-synthetic.mjs';
import {personalIntegrationsStagedInputsFixture} from './fixtures/personal-integrations-staged-inputs-synthetic.mjs';
import {readPersonalIntegrationsStagedAdmission} from './personal-integrations-staged-admission.mjs';

// Это composition test: fixed owner/SQL/CRI/resource ports заменены локальными
// результатами их отдельных контуров. Actual049/signature/TLS/live NOT RUN.
function stagedAdmissionFixture(t,withHistoricalReadiness=false,{sourceSHA='a'.repeat(40),sourcePath='/synthetic/source',ownerValue,authorityTarget,kube,readAdmission=readPersonalIntegrationsStagedAdmission}={}){
 const f=personalIntegrationsStagedInputsFixture();
 if(sourceSHA!=='a'.repeat(40)){
  f.inputs=JSON.parse(JSON.stringify(f.inputs),(_key,value)=>value==='a'.repeat(40)?sourceSHA:value);
  for(const part of f.value.components){const lock=JSON.parse(Buffer.from(part.componentLockRawBase64,'base64'));lock.source_sha=sourceSHA;replaceComponentLock(part,lock);part.nodeProof.sourceSHA=sourceSHA;}
 }
 if(authorityTarget){for(const key of ['context','clusterUID','namespaceUID','installationUID','installationSHA256'])f.target[key]=authorityTarget[key];}
 const directory=mkdtempSync(join(tmpdir(),'kodex-staged-admission-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const target=Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(key=>[key,f.target[key]]));
 const admission=Object.fromEntries(['planPath','evidencePath','trafficPlanPath','trafficEvidencePath','migrationPlanPath','migrationEvidencePath'].map(key=>[key,join(directory,key)]));
 const migration={profile:'personal-integrations',context:f.target.context,source:{path:sourcePath,revision:sourceSHA},kubeconfig:'/synthetic/kubeconfig',imageEvidence:f.value,
  personalIntegrationsInputs:f.inputs,componentHandoff:planComponentMigrationHandoff(f.value,f.deployments,{revision:sourceSHA},undefined,f.inputs)};
 const cohort={schema:'kodex.personal-integrations-staged-cohort-admission/v1',sourceSHA,target,apiAdmissionSHA256:'e'.repeat(64),
  cohort049:{phase:'STAGED',migrationPlanSHA256:fingerprint(migration)}};
 const art=migration.componentHandoff.components.find(part=>part.name==='artifact-retention');
 const assetsPlan={source:migration.source,target,admission,apiAdmissionSHA256:cohort.apiAdmissionSHA256,
  workload:{uid:art.uid,specSHA256:fingerprint(art.stoppedSpec)},stoppedAdmission:{cohort049:{migrationPlanSHA256:cohort.cohort049.migrationPlanSHA256}}};
 const assets={planPath:join(directory,'assets-plan'),evidencePath:join(directory,'assets-events')};
 const material={planPath:join(directory,'material-plan'),evidencePath:join(directory,'material-events'),sourceSHA:'b'.repeat(40)};
 const issuerMigration={planPath:join(directory,'role-plan'),evidencePath:join(directory,'role-events'),sourceSHA:'c'.repeat(40)};
 const uuid=n=>`20000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
 const resources={schema:issuerAssetsResourcesSchema,purpose:'PRE_ROTATION',sourceSHA,planSHA256:fingerprint(assetsPlan),
  resources:issuerAssetsResourceScope.map((scope,i)=>({...scope,action:i<9?'create':'patch',uid:uuid(i+1),bodySHA256:'d'.repeat(64)})),
  certificate:{uid:uuid(8),ready:true,tlsSecretUID:uuid(100),tlsMetadataSHA256:'f'.repeat(64)}};
 const role={schema:'kodex.artifact-retention-issuer-migration-ready/v1',sourceSHA:issuerMigration.sourceSHA,target,migrationVersion:20261115000200};
 const issuerMigrationReadiness=withHistoricalReadiness?{contextPath:join(directory,'role-context'),contextSHA256:'3'.repeat(64)}:undefined;
 const history=issuerMigrationReadiness&&{schema:'kodex.artifact-retention-issuer-historical-readiness/v1',
  purpose:'HISTORICAL_ROLE_ONLY',contextSHA256:issuerMigrationReadiness.contextSHA256,ready:structuredClone(role)};
 const ready={schema:'kodex.artifact-retention-issuer-ready/v1',sourceSHA:material.sourceSHA,target};
 const owner=ownerValue??{schema:'kodex.personal-integrations-authority-predecessor107/v1',sourceSHA,policyRevision:107,operationID:uuid(200),observedAt:'2026-11-21T00:00:00Z',actualReadbackCount:15,
  targetSHA256:f.inputs.cpPolicy.targetSHA256};
 const cpProjection={name:f.inputs.cpPolicy.projection.metadata.name,policySHA256:f.inputs.cpPolicy.policySHA256,
  uid:uuid(201),projectionSHA256:'2'.repeat(64)};
 const calls=[];
 const io={readCohort(){calls.push('cohort');return structuredClone(cohort);},
  readOwner(args){calls.push('owner');assert.equal(args.sourceSHA,sourceSHA);return structuredClone(owner);},
  readRole(args){
   calls.push('role');assert.equal(args.issuerMigration.sourceSHA,issuerMigration.sourceSHA);
   assert.deepEqual(args.issuerMigrationReadiness,issuerMigrationReadiness);
   return {ready:structuredClone(role),...(history?{historicalReadiness:structuredClone(history)}:{})};
  },
  readMaterialSource(args){calls.push('material-source');assert.equal(args.materialSourceSHA,material.sourceSHA);return {sourceSHA,materialSourceSHA:material.sourceSHA};},
  readMaterial(args){calls.push('material');assert.equal(args.sourceSHA,material.sourceSHA);return structuredClone(ready);},
  readImage(args){calls.push('image');assert.equal(args.sourceSHA,sourceSHA);assert.deepEqual(args.target,target);return structuredClone(f.inputs.authority);},
  readInputs(){calls.push('source-inputs');return structuredClone(f.inputs.issuerInputs);},
  readPolicyInput(){calls.push('policy-source');return structuredClone(f.inputs.cpPolicy);},
  readPolicyProjection(){calls.push('policy-projection');return structuredClone(cpProjection);},
  readResources(){calls.push('resources');return structuredClone(resources);}};
 const write=()=>{
  writeFileSync(admission.migrationPlanPath,JSON.stringify(migration),{mode:0o600});
  writeFileSync(admission.migrationEvidencePath,'{}\n',{mode:0o600});
  writeFileSync(assets.planPath,JSON.stringify(assetsPlan),{mode:0o600});writeFileSync(assets.evidencePath,'{}\n',{mode:0o600});
 };
 const input={sourceSHA,target,authorityTarget:authorityTarget??f.target,admission,assets,material,issuerMigration,issuerMigrationReadiness,
  authorityImage:{publicationPlanPath:'/synthetic/image',publicationEvidencePath:'/synthetic/image-events',nodeProofPath:'/synthetic/nodes'}};
 migration.personalIntegrationsRelease=personalIntegrationsReleaseReferences({authorityTarget:input.authorityTarget,material,issuerMigration,issuerMigrationReadiness,authorityImage:input.authorityImage,assets});
 migration.personalIntegrationsPrerequisites={schema:'kodex.personal-integrations-deployment-prerequisites/v1',sourceSHA,
  owner107:personalIntegrationsAuthorityPredecessorPins(owner),issuerMigrationReady:structuredClone(role),
  ...(history?{issuerMigrationHistoricalReadiness:structuredClone(history)}:{}),
  materialSource:{sourceSHA,materialSourceSHA:material.sourceSHA},materialReady:structuredClone(ready)};
 cohort.cohort049.migrationPlanSHA256=fingerprint(migration);
 assetsPlan.stoppedAdmission.cohort049.migrationPlanSHA256=cohort.cohort049.migrationPlanSHA256;
 resources.planSHA256=fingerprint(assetsPlan);write();
 return {input,migration,cohort,assetsPlan,resources,role,ready,owner,history,io,calls,write,
  read:()=>readAdmission(input,kube??(()=>assert.fail('unexpected live transport')),io)};
}

test('STAGED соединяет разные source/targets без remint и повторяет join/custody/owner после блокирующих reads',async t=>{
 const f=stagedAdmissionFixture(t),proof=await f.read();assert.equal(proof.schema,'kodex.personal-integrations-staged-admission/v1');
 assert.equal(proof.materialReady.sourceSHA,'b'.repeat(40));assert.equal(proof.issuerMigrationReady.sourceSHA,'c'.repeat(40));
 assert.equal(proof.sourceSHA,'a'.repeat(40));assert.equal(proof.artifactRetentionStage.uid,f.assetsPlan.workload.uid);
 assert.equal(proof.controlPlanePolicy.uid,'20000000-0000-4000-8000-000000000201');
 assert.equal(f.calls.filter(c=>c==='cohort').length,2);assert.equal(f.calls.at(-1),'owner');
 f.owner.observedAt='2026-11-21T01:00:00Z';f.owner.actualReadbackCount++;
 assert.deepEqual(await f.read(),proof);
});

test('foreign source/namespace, image-only handoff, old schema и missing TLS/resource custody закрыты',async t=>{
 for(const mutate of [f=>f.input.authorityTarget.namespaceUID='foreign',f=>f.cohort.cohort049.phase='STOPPED',
  f=>f.role.migrationVersion=20261115000100,f=>f.role.target={...f.role.target,namespaceUID:'foreign'},f=>f.ready.sourceSHA='d'.repeat(40),
  f=>{delete f.migration.personalIntegrationsInputs;f.write();},
  f=>{f.migration.componentHandoff.components.find(p=>p.name==='artifact-retention').stagedSpec.template.spec.containers.pop();f.write();},
  f=>{f.assetsPlan.workload.uid='foreign';f.write();},f=>f.resources.purpose='POST_ROTATION',f=>delete f.resources.certificate.tlsSecretUID,
  f=>f.resources.resources.pop(),f=>f.resources.schema='kodex.artifact-retention-issuer-assets-resources/v1',
  f=>f.resources.resources[13].apiVersion='foreign.invalid/v1',f=>f.resources.resources[13].name='foreign-policy',
  f=>f.resources.resources[13].namespace='foreign',f=>f.resources.resources[13].action='upsert',f=>f.resources.planSHA256='e'.repeat(64),
  f=>{delete f.migration.personalIntegrationsPrerequisites;f.write();},
  f=>{delete f.migration.personalIntegrationsRelease;f.write();},
  f=>f.input.material.planPath='/foreign/material-plan',f=>f.input.authorityImage.nodeProofPath='/foreign/nodes',
  f=>f.ready.planSHA256='e'.repeat(64),f=>f.role.evidenceSHA256='e'.repeat(64)]){
  const f=stagedAdmissionFixture(t);mutate(f);await assert.rejects(f.read);
 }
});

test('late cohort/material/authority drift не выдаёт прежний staged proof',async t=>{
 for(const kind of ['cohort','material','owner']){
  const f=stagedAdmissionFixture(t),name={cohort:'readCohort',material:'readMaterial',owner:'readOwner'}[kind],read=f.io[name];let count=0;
  f.io[name]=(...args)=>{const value=read(...args);if(++count===2){
   if(kind==='cohort')value.cohort049.migrationPlanSHA256='f'.repeat(64);
   if(kind==='material')value.planSHA256='f'.repeat(64);
   if(kind==='owner')value.operationID='foreign';
  }return value;};
  await assert.rejects(f.read);
 }
});

test('STAGED повторно читает bound историческую роль и отклоняет drift или другой контекст',async t=>{
 const f=stagedAdmissionFixture(t,true);
 assert.deepEqual((await f.read()).issuerMigrationHistoricalReadiness,f.history);
 assert.equal(f.calls.filter(c=>c==='role').length,2);
 const read=f.io.readRole;
 let count=0;
 f.io.readRole=(...args)=>{
  const value=read(...args);
  if(++count===2)value.historicalReadiness.contextSHA256='4'.repeat(64);
  return value;
 };
 await assert.rejects(f.read,/STAGED_HISTORICAL_ROLE_CHANGED/);
 f.input.issuerMigrationReadiness={...f.input.issuerMigrationReadiness,contextSHA256:'4'.repeat(64)};
 await assert.rejects(f.read,/STAGED_ORIGINAL_REFERENCES_CHANGED/);
});

// Исполняется actual original107 module и default STAGED reader нового executor.
// Kubernetes/SQL transport synthetic; ни current live107, ни crypto не заявлены.
test('исходный owner107 scope сохраняется после смены STAGED и rotate executor',async t=>{
 const {source,revision,executor,executorRevision}=syntheticPersonalExecutor(t);
 const fromExecutor=name=>import(pathToFileURL(join(executor,'tools/release',name+'.mjs')).href);
 const [predecessor,deployment,originalOwner,staged,authority,binding]=await Promise.all([
  fromExecutor('personal-integrations-authority-predecessor'),fromExecutor('personal-integrations-deployment-inputs'),
  fromExecutor('personal-integrations-original-owner'),fromExecutor('personal-integrations-staged-admission'),
  fromExecutor('personal-integrations-authority-owner'),fromExecutor('personal-integrations-authority-binding'),
 ]);
 const b=baselineFixture(t);b.registry.data['key-delivery-targets.yaml']=telegramRegistrySource('desired').raw;
 b.registry.data['authority-policy.json']=renderAuthorityPolicyEnvironment(telegramPolicySource('desired').raw,b.target.transition.environment).raw;
 const identity=deriveRegistryRotationIdentity(b.registry.data['key-delivery-targets.yaml']),readers=telegramAuthorityReadbackContract();
 Object.assign(b.owner,{operationId:identity.ownerOperationID,registryRevision:identity.sourceRevision,registrySourceDigestSHA256:identity.sourceDigestSHA256,
  expectedReadbackCount:readers.expectedReadbackCount,actualReadbackCount:readers.expectedReadbackCount});
 const kube=(args,sql)=>{if(args.includes('exec')&&!String(sql).includes('pg_catalog.pg_control_system()'))b.owner.observedAt=new Date().toISOString();return b.kube(args,sql);};
 const input={source,sourceSHA:revision,context:b.profile.context,kubeconfig:'/synthetic/kubeconfig',targetProfile:b.profile};
 const owner=readPersonalIntegrationsAuthorityPredecessor(input,kube);
 assert.equal(owner.ownerScope.executorSource,source);assert.equal(owner.ownerScope.executorRevision,revision);
 assert.throws(()=>predecessor.readPersonalIntegrationsAuthorityPredecessor({...input,expected:owner},kube),/OWNER_EXECUTOR_CHANGED/,'causal old scope cannot run in current module');
 const f=stagedAdmissionFixture(t,false,{sourceSHA:revision,sourcePath:source,ownerValue:owner,authorityTarget:b.profile,kube,readAdmission:staged.readPersonalIntegrationsStagedAdmission});
 const before=JSON.stringify(f.migration.personalIntegrationsPrerequisites);delete f.io.readOwner;
 const handoffOwner=await deployment.readPersonalIntegrationsDeploymentOwner107({...input,originalOwnerMigration:f.migration},kube);
 assert.deepEqual(personalIntegrationsAuthorityPredecessorPins(handoffOwner),personalIntegrationsAuthorityPredecessorPins(owner));
 assert.throws(()=>deployment.readPersonalIntegrationsDeploymentOwner107({...input,expected:owner},kube),/OWNER_EXECUTOR_CHANGED/);

 const planBytes=readFileSync(f.input.admission.migrationPlanPath),proof=await f.read();
 assert.deepEqual(proof.owner107,personalIntegrationsAuthorityPredecessorPins(owner));
 assert.equal(JSON.stringify(f.migration.personalIntegrationsPrerequisites),before);assert.deepEqual(readFileSync(f.input.admission.migrationPlanPath),planBytes);
 const transition=readCatalogTarget(b.profile,b.profile.context,b.get,personalIntegrationsAuthorityTransition).transition;
 const rotate={source,revision,context:b.profile.context,kubeconfig:input.kubeconfig,policyTransition:transition,maintenance:{owner107:proof.owner107}};
 rotate.ownerScope=authority.capturePersonalIntegrationsOwnerScope(rotate,kube);
 assert.equal(rotate.ownerScope.executorSource,executor);assert.equal(rotate.ownerScope.executorRevision,executorRevision);
 assert.deepEqual(binding.verifyPersonalIntegrationsOwnerDatabase(rotate,rotate.ownerScope).postgres,owner.ownerScope.postgres);
 for(const change of [o=>o.ownerScope.executorRevision='f'.repeat(40),o=>o.ownerScope.executorSource=executor,o=>o.ownerScope.postgresBackend.databaseOID='20001']){
  const saved=structuredClone(f.migration.personalIntegrationsPrerequisites.owner107),changed=structuredClone(saved);change(changed);f.migration.personalIntegrationsPrerequisites.owner107=changed;
  await assert.rejects(()=>originalOwner.readPersonalIntegrationsOriginalOwner107({...input,migration:f.migration},kube));
  f.migration.personalIntegrationsPrerequisites.owner107=saved;
 }
 assert.ok(b.calls.every(call=>!call.args.some(arg=>['create','patch','replace','delete'].includes(arg))));
});
