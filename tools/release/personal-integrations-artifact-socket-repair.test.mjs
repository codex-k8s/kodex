import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {fingerprint} from './scoped-release.mjs';
import {personalIntegrationsArtifactAPISpec} from './personal-integrations-artifact-spec.mjs';
import {repairPersonalArtifactSocket,personalArtifactSocketRepairSpec} from './personal-integrations-artifact-socket-repair.mjs';

function fixture() {
 const name='artifact-retention', init='internal-rpc-authority-socket-init', mount='internal-rpc-authority-sockets';
 const authorityImage='registry.example.invalid/authority@sha256:'+'a'.repeat(64);
 const resumedSpec={replicas:1,template:{spec:{serviceAccountName:name,automountServiceAccountToken:false,
  securityContext:{fsGroup:10001,fsGroupChangePolicy:'OnRootMismatch'},
  containers:[{name,image:'registry.example.invalid/artifact-retention@sha256:'+'b'.repeat(64)},
   {name:'internal-rpc-authority-issuer',image:authorityImage},{name:'platform-worker-grant-agent',image:authorityImage}],
  initContainers:[{name:init,image:authorityImage,command:['/usr/local/bin/'+init],securityContext:{runAsGroup:29000,runAsUser:29000},
   volumeMounts:[{name:mount,mountPath:'/run/kodex'}]}],
  volumes:[{name:mount,emptyDir:{}}]}}};
 const part={name,uid:'deployment-uid',replicas:1,beforeSpec:{template:{spec:{containers:[{name}]}}},resumedSpec};
 const migration={profile:'personal-integrations-backup',componentHandoff:{components:[part]},personalIntegrationsInputs:{
  authority:{imageRef:authorityImage},issuerInputs:{deployment:{spec:{template:{spec:{securityContext:{fsGroup:29000,fsGroupChangePolicy:'OnRootMismatch'}}}}}}}};
 const binding={type:'PERSONAL_INTEGRATIONS_READERS_BOUND',migrationPlanSHA256:fingerprint(migration),
  operationID:'rotation',openPlanSHA256:'open',bindingSHA256:'binding'};
 const events=[binding,{type:'PAIR_RESUME_INTENT',name}];
 const spec=personalIntegrationsArtifactAPISpec({profile:migration.profile,name,beforeSpec:part.beforeSpec,expectedSpec:resumedSpec,authorityImage});
 const deployment={apiVersion:'apps/v1',kind:'Deployment',metadata:{name,namespace:'kodex-system',uid:part.uid,
  resourceVersion:'9',annotations:{original:'kept'}},spec};
 const pod={metadata:{uid:'pod-uid'},spec:structuredClone(spec.template.spec),status:{phase:'Pending',
  initContainerStatuses:[{name:init,state:{terminated:{exitCode:1}}}],
  containerStatuses:spec.template.spec.containers.map(c=>({name:c.name,state:{waiting:{reason:'PodInitializing'}},restartCount:0}))}};
 const effects={dryRuns:0,writes:0,lostAck:false,advanceResourceVersion:false};
 const run=approvedSHA256=>repairPersonalArtifactSocket({migration,journal:{events,append:value=>events.push(value)},approvedSHA256,
  readPods:()=>[pod],readAPI:()=>({openPlanSHA256:binding.openPlanSHA256}),checkMigration:()=>{},bindExecutor:()=>{},
  authority:()=>{
   if(effects.advanceResourceVersion)deployment.metadata.resourceVersion='19';
   return {operationID:binding.operationID,bindingSHA256:binding.bindingSHA256,state:'PENDING_AUTHORITY',
    policyRevision:110,observedAt:new Date().toISOString()};
  },
  kube:args=>{
   if(args.includes('get'))return JSON.stringify(deployment);
   const path=args.find(arg=>arg.startsWith('--patch-file=')).slice('--patch-file='.length);
   const [uid,version,oldSpec,group,marker]=JSON.parse(readFileSync(path));
   assert.deepEqual([uid.value,version.value,oldSpec.value],[deployment.metadata.uid,deployment.metadata.resourceVersion,deployment.spec]);
   const next=structuredClone(deployment);
   next.spec.template.spec.securityContext.fsGroup=group.value;
   next.metadata.annotations=marker.value;
   if(args.includes('--dry-run=server'))effects.dryRuns++;
   else{
    effects.writes++;
    next.metadata.resourceVersion=String(Number(deployment.metadata.resourceVersion)+1);
    Object.assign(deployment,next);
    if(effects.lostAck)throw Error('synthetic lost reply');
   }
   return JSON.stringify(next);
  }});
 return {run,events,pod,migration,deployment,effects};
}

test('планирование и один CAS сохраняют прежний spec и журнал',async()=>{
 const f=fixture(),before=structuredClone(f.deployment.spec);
 const plan=await f.run();
 assert.equal(plan.status,'PLAN_ONLY');
 assert.equal(f.effects.writes,0);
 assert.equal(f.events.length,2);
 assert.equal((await f.run(plan.repairSHA256)).status,'ARTIFACT_SOCKET_REPAIRED');
 assert.equal(f.effects.writes,1);
 assert.equal(f.deployment.metadata.annotations.original,'kept');
 before.template.spec.securityContext.fsGroup=29000;
 assert.deepEqual(f.deployment.spec,before);
 assert.deepEqual(personalArtifactSocketRepairSpec(f.migration,f.events,f.deployment),before);
 await f.run(plan.repairSHA256);
 assert.equal(f.effects.writes,1);
});

test('потерянный ответ восстанавливается только по exact marker и spec',async()=>{
 const f=fixture(),plan=await f.run();
 f.effects.lostAck=true;
 await assert.rejects(f.run(plan.repairSHA256),/PATCH_UNKNOWN/);
 assert.throws(()=>personalArtifactSocketRepairSpec(f.migration,f.events,f.deployment),/REPAIR_REJECTED/);
 const saved=structuredClone(f.deployment.metadata.annotations);
 delete f.deployment.metadata.annotations['kodex.dev/artifact-socket-repair'];
 await assert.rejects(f.run(plan.repairSHA256),/REPAIR_REJECTED/);
 f.deployment.metadata.annotations=saved;
 await f.run(plan.repairSHA256);
 assert.equal(f.effects.writes,1);
});

test('запускавшийся writer, неверный hash и drift закрывают изменение',async()=>{
 const f=fixture(),plan=await f.run();
 await assert.rejects(f.run('f'.repeat(64)),/REPAIR_REJECTED/);
 f.pod.status.containerStatuses[0].containerID='containerd://synthetic';
 await assert.rejects(f.run(plan.repairSHA256),/REPAIR_REJECTED/);
 f.pod.status.containerStatuses[0].containerID=undefined;
 f.deployment.spec.template.spec.automountServiceAccountToken=true;
 await assert.rejects(f.run(plan.repairSHA256),/REPAIR_REJECTED/);
 assert.equal(f.effects.writes,0);
 assert.equal(f.events.length,2);
});

test('CAS использует версию после owner readback при неизменном spec',async()=>{
 const f=fixture(),plan=await f.run();
 f.effects.advanceResourceVersion=true;
 await f.run(plan.repairSHA256);
 assert.equal(f.deployment.metadata.resourceVersion,'20');
 assert.equal(f.effects.writes,1);
});
