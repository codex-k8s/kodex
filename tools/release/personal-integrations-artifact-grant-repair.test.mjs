import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {fingerprint} from './scoped-release.mjs';
import {personalIntegrationsArtifactAPISpec} from './personal-integrations-artifact-spec.mjs';
import {repairPersonalArtifactGrant,personalArtifactGrantRepairSpec} from './personal-integrations-artifact-grant-repair.mjs';

function fixture(){
 const name='artifact-retention', worker='platform-worker-grant-agent', init='internal-rpc-authority-socket-init';
 const imagePrefix='registry.local.kodex/kodex/internal-rpc-authority@sha256:';
 const authorityImage=imagePrefix+'a'.repeat(64),newImage=imagePrefix+'b'.repeat(64);
 const scope={fsGroup:10001,fsGroupChangePolicy:'OnRootMismatch'};
 const volume={name:'internal-rpc-authority-sockets',emptyDir:{}};
 const resumedSpec={replicas:2,template:{spec:{serviceAccountName:name,automountServiceAccountToken:false,securityContext:scope,
  containers:[{name,image:'registry.example.invalid/art@sha256:'+'c'.repeat(64)},
   {name:'internal-rpc-authority-issuer',image:authorityImage},{name:worker,image:authorityImage}],
  initContainers:[{name:init,image:authorityImage,command:['/usr/local/bin/'+init],
   securityContext:{runAsUser:29000,runAsGroup:29000},volumeMounts:[{name:volume.name,mountPath:'/run/kodex'}]}],
  volumes:[volume]}}};
 const part={name,uid:'deployment-uid',replicas:2,beforeSpec:{template:{spec:{containers:[{name}]}}},resumedSpec};
 const migration={profile:'personal-integrations-backup',componentHandoff:{components:[part]},personalIntegrationsInputs:{
  authority:{imageRef:authorityImage},
  issuerInputs:{deployment:{spec:{template:{spec:{securityContext:{...scope,fsGroup:29000}}}}}}}};
 const old=personalIntegrationsArtifactAPISpec({profile:migration.profile,name,beforeSpec:part.beforeSpec,expectedSpec:resumedSpec,authorityImage});
 const current=structuredClone(old);current.template.spec.securityContext.fsGroup=29000;
 const binding={type:'PERSONAL_INTEGRATIONS_READERS_BOUND',migrationPlanSHA256:fingerprint(migration),operationID:'rotation',bindingSHA256:'binding',openPlanSHA256:'api'};
 const socketPins={migrationPlanSHA256:fingerprint(migration),deploymentUID:part.uid,operationID:binding.operationID,
  beforeSHA256:fingerprint(old),afterSHA256:fingerprint(current)};
 const socketHash=fingerprint(socketPins);
 const events=[binding,{type:'PAIR_RESUME_INTENT',name},
  {type:'PERSONAL_ARTIFACT_SOCKET_REPAIR_INTENT',...socketPins,repairSHA256:socketHash,resourceVersion:'1'},
  {type:'PERSONAL_ARTIFACT_SOCKET_REPAIR_ACK',...socketPins,repairSHA256:socketHash,resourceVersion:'2'}];
 const deployment={apiVersion:'apps/v1',kind:'Deployment',metadata:{name,namespace:'kodex-system',uid:part.uid,resourceVersion:'9',
  generation:1,annotations:{'kodex.dev/artifact-socket-repair':socketHash}},spec:current,status:{replicas:2,observedGeneration:1}};
 const fixture={deployment,events,migration,newImage,writes:[],lostAck:'',notJoined:false,ownerState:'RETIRED'};
 fixture.run=approvedSHA256=>repairPersonalArtifactGrant({migration,journal:{events,append:value=>events.push(value)},approvedSHA256,
  readAPI:()=>({openPlanSHA256:binding.openPlanSHA256}),checkMigration:()=>{},bindExecutor:()=>{},
  authority:()=>({operationID:binding.operationID,bindingSHA256:binding.bindingSHA256,state:fixture.ownerState,
   policyRevision:110,observedAt:new Date().toISOString()}),
  loadCandidate:()=>({schema:'kodex.personal-integrations-authority-image/v1',imageRef:newImage}),
  readPods:()=>fixture.notJoined?[{}]:[],
  wait:async(read,accept,error)=>{if(!accept(read()))throw Error(error);},
  kube:args=>{
   if(args.includes('get'))return JSON.stringify(deployment);
   const path=args.find(arg=>arg.startsWith('--patch-file=')).slice('--patch-file='.length);
   const [uid,version,oldSpec,spec,marker]=JSON.parse(readFileSync(path));
   assert.deepEqual([uid.value,version.value,oldSpec.value],[deployment.metadata.uid,deployment.metadata.resourceVersion,deployment.spec]);
   const next=structuredClone(deployment),step=spec.value.replicas?'resume':'stop';
   next.spec=spec.value;
   next.metadata.annotations=marker.value;
   if(!args.includes('--dry-run=server')){
    fixture.writes.push(step);
    next.metadata.resourceVersion=String(Number(deployment.metadata.resourceVersion)+1);
    next.metadata.generation++;
    next.status={replicas:next.spec.replicas,observedGeneration:next.metadata.generation};
    Object.assign(deployment,next);
    if(fixture.lostAck===step)throw Error('synthetic lost reply');
   }
   return JSON.stringify(next);
  }});
 return fixture;
}

test('план перед join, две связанные записи и одна замена image',async()=>{
 const f=fixture(),before=structuredClone(f.deployment.spec);
 const plan=await f.run();
 assert.equal(plan.status,'PLAN_ONLY');assert.equal(f.writes.length,0);
 const result=await f.run(plan.repairSHA256);
 assert.equal(result.status,'ARTIFACT_GRANT_REPAIRED');
 assert.deepEqual(f.writes,['stop','resume']);
 before.template.spec.containers.find(c=>c.name==='platform-worker-grant-agent').image=f.newImage;
 assert.deepEqual(f.deployment.spec,before);
 assert.deepEqual(personalArtifactGrantRepairSpec(f.migration,f.events,f.deployment),before);
 await f.run(plan.repairSHA256);
 assert.deepEqual(f.writes,['stop','resume']);
});

test('без RETIRED нет остановки, а без join нет запуска',async()=>{
 const f=fixture(),plan=await f.run();
 f.ownerState='PENDING_AUTHORITY';
 await assert.rejects(f.run(plan.repairSHA256),/REJECTED/);
 assert.deepEqual(f.writes,[]);
 f.ownerState='RETIRED';f.notJoined=true;
 await assert.rejects(f.run(plan.repairSHA256),/STOP_NOT_JOINED/);
 assert.deepEqual(f.writes,['stop']);
 f.notJoined=false;
 await f.run(plan.repairSHA256);
 assert.deepEqual(f.writes,['stop','resume']);
});

test('потерянный ответ stop или resume не повторяет PATCH',async()=>{
 for(const step of ['stop','resume']){
  const f=fixture(),plan=await f.run();f.lostAck=step;
  await assert.rejects(f.run(plan.repairSHA256),/PATCH_UNKNOWN/);
  assert.throws(()=>personalArtifactGrantRepairSpec(f.migration,f.events,f.deployment),/REJECTED/);
  f.lostAck='';
  await f.run(plan.repairSHA256);
  assert.deepEqual(f.writes,['stop','resume']);
 }
});

test('image delta без прежнего socket receipt или с посторонним spec отклоняется',async()=>{
 const f=fixture(),plan=await f.run();
 f.deployment.spec.template.spec.hostNetwork=true;
 await assert.rejects(f.run(plan.repairSHA256),/REJECTED/);
 f.deployment.spec.template.spec.hostNetwork=undefined;
 f.events.pop();
 await assert.rejects(f.run(plan.repairSHA256),/REJECTED/);
 assert.deepEqual(f.writes,[]);
});
