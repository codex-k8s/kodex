import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {fixture,uid} from './fixtures/authority-catalog-synthetic.mjs';
import {canonicalPostgresBackupSpec} from './backup-postgres-boundary.mjs';
import {authorityOwnerDatabaseArgs} from './migration-owner-database.mjs';
import {deriveCanonicalRegistryAdvance} from './authority-rotation-transition.mjs';
import {authorityReadbackContract} from './authority-readback-targets.mjs';
import {catalogAuthorityBaselineSQL,readCatalogAuthorityBaseline,validateCatalogAuthorityBaseline,sameCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';

// Только Kubernetes/SQL transport заменены оснасткой; исполняются настоящие
// pinned source/template, owner-shape, namespace, canonical registry и DB readers.
function baselineFixture(){
 const f=fixture(),source=fileURLToPath(new URL('../../',import.meta.url)).replace(/\/$/,'');
 const canonical=readFileSync(`${source}/deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml`,'utf8');
 const advance=deriveCanonicalRegistryAdvance(f.registry.data['key-delivery-targets.yaml'],canonical);
 f.registry.data['key-delivery-targets.yaml']=advance.raw;f.registry.data['authority-policy.json']=f.desired.raw;
 const intentID=uid(60),operationID=advance.identity.ownerOperationID;
 f.registry.metadata.annotations={'kodex.dev/rotation-intent':intentID,'kodex.dev/rotation-operation':operationID};
 f.publisher.metadata.generation=1;f.publisher.spec.replicas=1;
 f.publisher.spec.template.metadata.annotations={'kodex.dev/authority-rotation-operation':operationID};
 f.publisher.status={observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1};
 const transition=f.target.transition;
 const plan={action:'rotate',intentID,ownerOperationID:operationID,source,revision:'a'.repeat(40),context:f.profile.context,
  policyTransition:transition,registry:{uid:f.registry.metadata.uid,desiredData:structuredClone(f.registry.data),desiredDataSHA256:fingerprint(f.registry.data),
   sourceRevision:advance.identity.sourceRevision,sourceDigestSHA256:advance.identity.sourceDigestSHA256},
  publisher:{uid:f.publisher.metadata.uid,desiredSpecSHA256:fingerprint(f.publisher.spec)}};
 const now=Date.now(),row=(status,extra={})=>({status,intentID,ownerOperationID:operationID,at:new Date(now).toISOString(),...extra});
 const events=[row('INTENT',{action:'rotate',planSHA256:fingerprint(plan)}),row('APPLIED',{operation:'REGISTRY_CAS'}),
  row('APPLIED',{operation:'PUBLISHER_RESTART'}),row('SUCCEEDED',{rotationStatus:'RETIRED',jobUID:uid(61)})];
 const readers=authorityReadbackContract(advance.raw);
 const owner={observedAt:new Date(now).toISOString(),operationId:operationID,operationStatus:'RETIRED',status:'RETIRED',phase:'RETIRE',
  registryRevision:advance.identity.sourceRevision,registrySourceDigestSHA256:advance.identity.sourceDigestSHA256,
  baseRevision:1,sourceRevision:4,sourceDigestSHA256:'d'.repeat(64),intentId:uid(62),protocolVersion:2,
  expectedReadbackCount:readers.expectedReadbackCount,actualReadbackCount:readers.expectedReadbackCount,
  switchNotBefore:new Date(now-90000).toISOString(),previousNotAfter:new Date(now-5000).toISOString(),completedAt:new Date(now-1000).toISOString()};
 const postgresSpec=canonicalPostgresBackupSpec();
 const set={apiVersion:'apps/v1',kind:'StatefulSet',metadata:{name:'kodex-postgresql',namespace:'kodex-system',uid:uid(70)},spec:{template:{spec:postgresSpec}}};
 const pg={apiVersion:'v1',kind:'Pod',metadata:{name:'kodex-postgresql-0',namespace:'kodex-system',uid:uid(71),labels:{'app.kubernetes.io/name':'kodex-postgresql'},
  ownerReferences:[{apiVersion:'apps/v1',kind:'StatefulSet',name:'kodex-postgresql',uid:uid(70),controller:true}]},
  spec:{...structuredClone(postgresSpec),volumes:[...postgresSpec.volumes,{name:'data',persistentVolumeClaim:{claimName:'data-kodex-postgresql-0'}}]},
  status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:'postgresql',ready:true,state:{running:{}},imageID:postgresSpec.containers[0].image}]}};
 const rs={kind:'ReplicaSet',metadata:{name:'publisher-rs',namespace:'kodex-system',uid:uid(72),ownerReferences:[{kind:'Deployment',uid:f.publisher.metadata.uid,controller:true}]}};
 const publisherPod={kind:'Pod',metadata:{name:'publisher-0',namespace:'kodex-system',uid:uid(73),labels:{'app.kubernetes.io/name':'internal-rpc-authority-publisher'},ownerReferences:[{kind:'ReplicaSet',uid:rs.metadata.uid,controller:true}]},
  spec:structuredClone(f.publisher.spec.template.spec),status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:'publisher',ready:true,state:{running:{}},imageID:f.publisher.spec.template.spec.containers[0].image}]}};
 f.resources.set('pod/kodex-postgresql-0',pg);f.resources.set('statefulset/kodex-postgresql',set);f.resources.set('pod/publisher-0',publisherPod);
 const backend={systemIdentifier:'18446744073709551615',databaseOID:'20000',database:'internal_rpc_authority',port:'5432',serverAddress:null,serverPort:null};
 const calls=[],faults={};
 const kube=(args,sql)=>{
  calls.push({args,sql});
  if(args.includes('exec')){
   assert.deepEqual(args,authorityOwnerDatabaseArgs);
   if(String(sql).includes('pg_catalog.pg_control_system()'))return JSON.stringify(backend);
   assert.ok(Buffer.from(sql).equals(catalogAuthorityBaselineSQL()));
   faults.afterOwner?.();return JSON.stringify(owner);
  }
  const index=args.indexOf('get'),kind=args[index+1],name=args[index+2];
  if(kind==='replicasets,pods,jobs')return [rs,publisherPod].map(r=>[r.kind,r.metadata.name,r.metadata.uid,r.metadata.labels?.['app.kubernetes.io/name']??'',r.metadata.namespace,JSON.stringify(r.metadata.ownerReferences)].join('\t')).join('\n')+'\n';
  const value=f.resources.get(`${kind}/${name}`);assert.ok(value,`${kind}/${name}`);return JSON.stringify(value);
 };
 const read=expected=>readCatalogAuthorityBaseline({plan,events,expected,now:()=>now},kube,{inspectSource:()=>({revision:plan.revision})});
 return {...f,plan,events,now,owner,pg,set,publisherPod,backend,calls,faults,kube,read};
}

test('routine baseline reads exact retired owner106 without old isolation or any write',()=>{
 const f=baselineFixture(),proof=f.read();
 assert.equal(proof.policyRevision,106);assert.equal(proof.postgresBackend.database,'internal_rpc_authority');
 assert.equal(proof.operationID,f.plan.ownerOperationID);assert.equal(proof.expectedReadbackCount,14);
 assert.equal(f.calls.filter(call=>call.args.includes('exec')).length,4);
 assert.ok(f.calls.every(call=>!call.args.some(arg=>['create','patch','replace','delete'].includes(arg))));
 const fresh=f.read(proof);assert.ok(sameCatalogAuthorityBaseline(proof,fresh));
 f.owner.actualReadbackCount++;assert.ok(sameCatalogAuthorityBaseline(proof,f.read(proof)));
});

test('baseline rejects unknown owner fields, wrong operation/snapshot, stale and incomplete required members',()=>{
 for(const patch of [{operationId:uid(99)},{sourceRevision:5},{sourceRevision:'4'},{baseRevision:null},{status:'WAITING_RETIRE'},
  {phase:'SWITCH'},{protocolVersion:1},{actualReadbackCount:13},{expectedReadbackCount:16},{extra:true},
  {observedAt:new Date(Date.now()-60000).toISOString()},{observedAt:new Date(Date.now()+60000).toISOString()}]){
  const f=baselineFixture();Object.assign(f.owner,patch);assert.throws(()=>f.read());
 }
});

test('timeout watcher RUNNING remains immutable while fresh owner RETIRED completes baseline proof',()=>{
 const f=baselineFixture();
 f.events.at(-1).status='RUNNING';delete f.events.at(-1).rotationStatus;
 const before=JSON.stringify(f.events),proof=f.read();
 assert.equal(proof.operationID,f.plan.ownerOperationID);assert.equal(JSON.stringify(f.events),before);
 f.owner.status='WAITING_RETIRE';f.owner.operationStatus='WAITING_RETIRE';
 assert.throws(()=>f.read(),/OWNER_REJECTED/);assert.equal(JSON.stringify(f.events),before);
});

test('baseline binds actual PostgreSQL Pod/STS, local backend and complete selected container spec',()=>{
 for(const mutate of [f=>f.pg.metadata.ownerReferences[0].uid=uid(99),f=>f.set.metadata.uid=uid(99),
  f=>f.pg.spec.ephemeralContainers=[{name:'debug'}],f=>f.pg.spec.containers[0].command=['foreign'],
  f=>f.pg.status.containerStatuses[0].ready=false,f=>f.pg.status.containerStatuses[0].state={waiting:{}},
  f=>f.backend.database='control_plane',f=>f.backend.databaseOID=20000,
  f=>f.faults.afterOwner=()=>{f.pg.metadata.uid=uid(99);}]){
  const f=baselineFixture();mutate(f);assert.throws(()=>f.read());
 }
 const f=baselineFixture(),proof=f.read();f.backend.databaseOID='20001';assert.throws(()=>f.read(proof),/INSTANCE_CHANGED/);
});

test('baseline preserves immutable registry/publisher pins and rejects valid-looking substitutes',()=>{
 for(const mutate of [f=>f.registry.metadata.uid=uid(99),f=>f.registry.data.unrelated='changed',
  f=>f.publisher.metadata.uid=uid(99),f=>f.publisher.spec.template.spec.containers[0].image+='foreign',
  f=>f.publisherPod.spec.containers.push({name:'debug',image:'foreign'}),
  f=>f.events.pop(),f=>f.plan.policyTransition.targetProfile.clusterUID=uid(99)]){
  const f=baselineFixture();mutate(f);assert.throws(()=>f.read());
 }
 const f=baselineFixture(),proof=f.read();
 for(const patch of [{observedAt:new Date(f.now-30001).toISOString()},{policyRevision:107},{sqlSHA256:'f'.repeat(64)}]){
  const changed={...proof,...patch};delete changed.baselineSHA256;changed.baselineSHA256=fingerprint(changed);
  assert.throws(()=>validateCatalogAuthorityBaseline(changed,f.now));
 }
});
