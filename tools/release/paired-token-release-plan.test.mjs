import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {readPairedTokenReleaseOwnerFromDatabase} from './paired-token-release-owner.mjs';
import {pairedTokenOwnerQueries} from './paired-token-owner-database.mjs';
import {pairedTokenOwnerFixture} from './fixtures/paired-token-owner-synthetic.mjs';
import {migrationOwnerDatabaseArgs,authorityOwnerDatabaseArgs} from './migration-owner-database.mjs';
import {pairedTokenReleaseNames,validatePairedTokenReleaseManifest,planPairedTokenRelease,verifyPairedTokenReleasePlan} from './paired-token-release-plan.mjs';
import {pairedTokenReleaseFixture as fixture,pairedTokenReleaseFixtureUID as uid,
 pairedTokenReleaseFixtureSource as source,pairedTokenReleaseFixtureDigest as digest} from './fixtures/paired-token-release-synthetic.mjs';

test('адресный same-schema план сохраняет historical installation seal, обе exact images и sidecars; apply не заявлен',()=>{
 const f=fixture(),before=structuredClone(f.observed),plan=planPairedTokenRelease(f.manifest,f.current());
 assert.equal(plan.state,'PLAN_ONLY');assert.equal(plan.releaseReadiness,'BLOCKED_LIFECYCLE_NOT_IMPLEMENTED');
 assert.equal(plan.patch,undefined);assert.equal(plan.migrationVersion,undefined);assert.equal(plan.jobUID,undefined);
 assert.deepEqual(plan.components.map(c=>c.name),pairedTokenReleaseNames);assert.deepEqual(f.observed,before);
 assert.notEqual(JSON.parse(f.observed.installation.data['schema-heads.json'])['control-plane'],plan.schemaHeads['control-plane']);
 for(const part of plan.components){
  const expected=structuredClone(part.beforeSpec);
  expected.template.spec.containers.find(c=>c.name===part.name).image=part.imageRef;
  expected.template.metadata.annotations['kodex.dev/component-source-sha']=source;
  assert.deepEqual(part.afterSpec,expected);
 }
 assert.deepEqual(verifyPairedTokenReleasePlan(plan,f.manifest,f.current()).components,plan.components);
});

test('прежний более широкий CAG scope сохраняется целиком и не превращается в новый paired target',()=>{
 const f=fixture({broaderAdmission:true}),plan=planPairedTokenRelease(f.manifest,f.current());
 assert.equal(plan.apiAdmission.targetSHA256,fingerprint(f.api.closed.plan.target));
 assert.notEqual(plan.apiAdmission.targetSHA256,fingerprint(plan.target));
 assert.equal(plan.target.resources.length,4);assert.equal(f.api.closed.plan.target.resources.length,5);
 f.api.closed.plan.target.installationUID='10000000-0000-4000-8000-000000000099';
 assert.throws(()=>planPairedTokenRelease(f.manifest,f.current()),/API_SCOPE_CHANGED/);
});

test('закрытый manifest отвергает изменение schema, чужой workload, повтор target, raw patch и отдельный старый gateway',()=>{
 for(const alter of [v=>v.schemaHeadsAfter['control-plane']++,v=>v.schemaHeadsBefore['control-plane']=20261112000100,
  v=>v.schemaHeadsAfter['internal-rpc-authority']++,v=>v.profile='personal-integrations',v=>v.schema='kodex.portable-component-migration/v2',
  v=>v.components.pop(),v=>v.components.reverse(),v=>v.components.push(v.components[0]),v=>v.components[0].name='secret-broker',
  v=>v.target.resources.push(v.target.resources[0]),v=>v.target.resources[0].uid=uid(98),v=>v.components[0].patch=[],
  v=>v.migrationCommand='up',v=>v.sourceSHA='0'.repeat(40),v=>v.components[0].beforeImage='example.invalid/cp:latest']){
  const f=fixture();alter(f.manifest);assert.throws(()=>validatePairedTokenReleaseManifest(f.manifest));
 }
});

test('actual publisher receipt обязателен для обоих: source/target/archive/hash/digest/phase drift закрыт',()=>{
 for(const alter of [p=>p.publicationEvidence.pop(),p=>p.publicationEvidence[0].planSHA256='a'.repeat(64),
  p=>p.publicationEvidence[1].phase='BUILD',p=>p.publicationPlan.namespaceUID=uid(99),
  p=>p.publicationPlan.receipt.sourceSHA='b'.repeat(40),p=>p.publicationPlan.image.digest='sha256:'+'3'.repeat(64),
  p=>p.publicationPlan.receipt.dockerfile='services/jobs/artifact-retention/Dockerfile']){
  const f=fixture();alter(f.manifest.components[1].publication);assert.throws(()=>validatePairedTokenReleaseManifest(f.manifest));
 }
 const f=fixture(),p=f.manifest.components[1].publication;
 p.publicationPlan.receipt.contextSHA256='b'.repeat(64);p.publicationEvidence[0].planSHA256=fingerprint(p.publicationPlan);
 assert.throws(()=>validatePairedTokenReleaseManifest(f.manifest),/BUILD_SOURCE_CHANGED/);
 const separate=fixture(),p2=separate.manifest.components[1].publication;
 p2.publicationPlan.receipt.archiveSHA256='c'.repeat(64);p2.publicationPlan.archiveSHA256='c'.repeat(64);
 p2.publicationEvidence[0].planSHA256=fingerprint(p2.publicationPlan);
 assert.equal(validatePairedTokenReleaseManifest(separate.manifest),separate.manifest);
});

test('CP051 catalogue, same native backend, authority110 и первый ceiling обязательны до plan',()=>{
 for(const alter of [f=>f.controlPlane.proof.gooseVersion--,f=>f.controlPlane.proof.artifactPredecessorApplied=false,
  f=>f.controlPlane.proof.schemaMatches=false,f=>f.controlPlane.proof.schemaCatalog.catalogDigest=digest,
  f=>f.controlPlane.backend.systemIdentifier='123',f=>f.controlPlane.backend.databaseOID=f.authority.postgresBackend.databaseOID,
  f=>f.head.gooseVersion--,f=>f.head.readOnly='off',f=>f.head.backend={...f.head.backend,databaseOID:'99'},
  f=>{f.authority.policyRevision=107;},f=>{f.authority.observedAt=new Date(f.now-30001).toISOString();},
  f=>{f.authority.scope.installationUID=uid(99);},f=>{f.authority.postgres={...f.authority.postgres,uid:uid(99)};}]){
  const f=fixture();alter(f);delete f.authority.baselineSHA256;f.authority.baselineSHA256=fingerprint(f.authority);
  assert.throws(()=>f.current());
 }
 const f=fixture();let call=0;f.ports.now=()=>f.now+(call++?30001:0);
 assert.throws(()=>f.current(),/OWNER_EXPIRED/);
});

test('сохранённый JSON/мутация proof/expiry не заменяют fresh private owner result',()=>{
 const f=fixture(),current=f.current();
 assert.throws(()=>planPairedTokenRelease(f.manifest,{...current,owner:structuredClone(current.owner)}),/OWNER_PROOF_REQUIRED/);
 current.owner.authority.policyRevision=107;
 assert.throws(()=>planPairedTokenRelease(f.manifest,current),/OWNER_PROOF_REQUIRED/);
 const fresh=f.current();assert.throws(()=>planPairedTokenRelease(f.manifest,{...fresh,now:f.now+30001}),/OWNER_PROOF_REQUIRED/);
 assert.throws(()=>planPairedTokenRelease(f.manifest,{...fresh,source:{revision:'f'.repeat(40)}}),/SOURCE_CHANGED/);
});

test('current API isolate и positive close join нельзя заменить историческим CLOSED или пустым selector',()=>{
 for(const alter of [f=>f.api.closed.events.pop(),f=>f.api.traffic.events.pop(),f=>f.api.service.metadata.uid=uid(99),
  f=>f.api.service.spec.selector={},f=>f.api.slices.items.push({kind:'EndpointSlice'}),f=>f.api.resources.items.push(
   {kind:'ReplicaSet',metadata:{uid:uid(98),ownerReferences:[{kind:'Deployment',uid:f.api.closed.plan.deploymentUID,controller:true}]}},
   {kind:'Pod',metadata:{uid:uid(99),namespace:'kodex-system',labels:{},ownerReferences:[{kind:'ReplicaSet',uid:uid(98),controller:true}]},status:{phase:'Running'}}),
  f=>f.observed.deployments.find(d=>d.metadata.name==='control-api-gateway').spec.replicas=1,
  f=>f.api.traffic.plan.target={...f.api.traffic.plan.target,installationUID:uid(99)}]){
  const f=fixture();alter(f);assert.throws(()=>planPairedTokenRelease(f.manifest,f.current()),undefined,alter.toString());
 }
});

test('serving/source/spec/sidecar/CRI drift закрыты; label removal не скрывает старый owned Pod',()=>{
 for(const alter of [f=>f.observed.deployments[0].spec.template.spec.containers[1].env[0].value='changed',
  f=>f.observed.deployments[0].metadata.uid=uid(99),f=>f.observed.installation.data['source.sha']='f'.repeat(40),
  f=>f.observed.serving['control-plane'].items[1].status.containerStatuses[1].ready=false,
  f=>f.observed.serving['control-plane'].items[1].spec.ephemeralContainers=[{name:'debug'}],
  f=>f.nodeReadback.images.pop(),f=>f.nodeReadback.images[1].nodes[0].repoDigestVerified=false,
  f=>f.nodeReadback.images[0].nodes[0].uid=uid(99),f=>f.nodeReadback.binding.push(f.nodeReadback.binding[0])]){
  const f=fixture();alter(f);assert.throws(()=>planPairedTokenRelease(f.manifest,f.current()));
 }
 const f=fixture();f.observed.serving['control-plane'].items[1].metadata.labels={};
 assert.equal(planPairedTokenRelease(f.manifest,f.current()).components[0].servingPodUIDs.length,1);
});

test('перечитывание не принимает подменённый saved plan, APPLIED или другой paired preimage',()=>{
 for(const alter of [p=>p.components[0].imageRef=p.components[0].beforeImage,p=>p.state='APPLIED',
  p=>p.releaseReadiness='READY',p=>p.components[0].beforeResourceVersion='99',p=>p.target.installationUID=uid(99),
  p=>p.nodeReadback.images.reverse(),p=>p.apiAdmission.serviceUID=uid(99),p=>p.sourceSHA='f'.repeat(40),
  p=>p.schemaHeads['control-plane']--,p=>p.extra='unknown']){
  const f=fixture(),plan=structuredClone(planPairedTokenRelease(f.manifest,f.current()));alter(plan);
  assert.throws(()=>verifyPairedTokenReleasePlan(plan,f.manifest,f.current()),/PLAN_CHANGED/);
 }
});

test('реальный owner factory сохраняет оба schema negatives в двух fixed same-session SQL без effects',t=>{
 for(const bad of ['control-plane','internal-rpc-authority']){
  const f=pairedTokenOwnerFixture(t),queries=pairedTokenOwnerQueries();
  f.faults.response=(value,authority)=>{
   if(bad==='control-plane'&&!authority)value.proof.schemaMatches=false;
   if(bad==='internal-rpc-authority'&&authority)value.head.gooseVersion=20261115000100;
  };
  assert.throws(()=>readPairedTokenReleaseOwnerFromDatabase(f.manifest,f,f.kube,{now:()=>f.now}),/SCHEMA_REJECTED/);
  const calls=f.calls.filter(c=>c.args.includes('exec'));
  assert.equal(String(calls[0].input),queries.controlPlane.sql);
  assert.deepEqual(calls[0].args,migrationOwnerDatabaseArgs);
  assert.equal(calls.length,bad==='internal-rpc-authority'?2:1);
  if(calls.length===2){assert.equal(String(calls[1].input),queries.authority.sql);assert.deepEqual(calls[1].args,authorityOwnerDatabaseArgs);}
  assert.ok(f.calls.every(c=>!c.args.some(v=>['create','patch','replace','delete','apply'].includes(v))));
 }
});
