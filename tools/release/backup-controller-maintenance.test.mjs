import test from 'node:test';
import assert from 'node:assert/strict';
import {fixture,metadata,uid} from './fixtures/authority-catalog-synthetic.mjs';
import {fingerprint} from './scoped-release.mjs';
import {backupControllerMaintenanceHeader,readBackupProducerInventory,requireBackupProducerBoundary,requireStoppedBackupController,planBackupControllerMaintenance,backupControllerMaintenanceStep} from './backup-controller-maintenance.mjs';

function setup(){
 const base=fixture(),sourceSHA='a'.repeat(40),deployment=structuredClone(base.api);
 deployment.metadata={...metadata('backup-controller',300),generation:1,labels:{...deployment.metadata.labels,'app.kubernetes.io/name':'backup-controller'}};
 deployment.spec.replicas=1;deployment.spec.selector.matchLabels={'app.kubernetes.io/name':'backup-controller'};
 deployment.spec.template.spec.containers[0].name='backup-controller';deployment.spec.template.spec.serviceAccountName='backup-controller';
 deployment.spec.template.spec.volumes=[{name:'credentials',secret:{secretName:'backup-controller-credentials'}}];
 deployment.status={observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1};
 const target={...base.profile,resources:[{apiVersion:'apps/v1',kind:'Deployment',name:'backup-controller',uid:deployment.metadata.uid}]};
 const postgres={pod:'kodex-postgresql-0',uid:uid(310),ownerUID:uid(311)};
 const observed={context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,installation:base.installation,postgres,resources:{items:[]}};
 const rs={kind:'ReplicaSet',metadata:{...metadata('backup-controller-rs',301),generation:1,ownerReferences:[{kind:'Deployment',uid:deployment.metadata.uid,controller:true}]},spec:{replicas:1},status:{replicas:1,observedGeneration:1}};
 let serial=320;
 const pod=()=>({kind:'Pod',metadata:{...metadata('backup-controller-pod',serial++),ownerReferences:[{kind:'ReplicaSet',uid:rs.metadata.uid,controller:true}]},
  spec:{...structuredClone(deployment.spec.template.spec),nodeName:'synthetic-node',enableServiceLinks:true},status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:deployment.spec.template.spec.containers.map(c=>({name:c.name,ready:true,imageID:c.image,state:{running:{}}}))}});
 const state={pods:[pod()],extra:[],effects:0,loseACK:false,gateValid:true};
 const projected=item=>({kind:item.kind,metadata:structuredClone(item.metadata),serviceAccounts:item.kind==='Deployment'||item.kind==='Pod'?['backup-controller']:[],secretRefs:[],replicas:item.spec?.replicas,statusReplicas:item.status?.replicas,observedGeneration:item.status?.observedGeneration});
 const inventory=()=>({schema:'kodex.backup-producer-inventory/v1',namespace:'kodex-system',kinds:['Deployment','StatefulSet','DaemonSet','CronJob','Job','ReplicaSet','Pod'],items:[deployment,rs,...state.pods].map(projected).concat(state.extra)});
 const resources=()=>({items:[rs,...state.pods]});
 const stop=planBackupControllerMaintenance(deployment,target,observed,sourceSHA,'stop',undefined,inventory());
 const journal=plan=>({events:[{type:'HEADER',...backupControllerMaintenanceHeader(plan)}],append(value){this.events.push(value);}});
 const patch=(operations,dryRun)=>{
  for(const op of operations.filter(v=>v.op==='test')){let current=deployment;for(const key of op.path.slice(1).split('/'))current=current[key];assert.deepEqual(current,op.value);}
  if(dryRun)return;
  for(const op of operations.filter(v=>v.op==='add'||v.op==='replace')){const keys=op.path.slice(1).split('/');let current=deployment;for(const key of keys.slice(0,-1))current=current[key];current[keys.at(-1)]=structuredClone(op.value);}
  deployment.metadata.resourceVersion=String(Number(deployment.metadata.resourceVersion)+1);deployment.metadata.generation++;state.effects++;
  if(state.loseACK)throw Error('LOST_ACK');
 };
 const gate=plan=>({targetSHA256:fingerprint(target),deploymentUID:deployment.metadata.uid,sourceSHA,planSHA256:plan.stopPlanSHA256,canonicalBackup:state.gateValid,cleanupTerminal:state.gateValid});
 const step=(phase,plan,journal,extra={})=>backupControllerMaintenanceStep({phase,plan,journal,get:()=>structuredClone(deployment),resources,producerInventory:inventory,patch,gate:async()=>gate(plan),...extra});
 const converge=()=>{
  const n=deployment.spec.replicas;deployment.status={observedGeneration:deployment.metadata.generation,replicas:n,updatedReplicas:n,readyReplicas:n,availableReplicas:n};
  rs.spec.replicas=n;rs.status.replicas=n;state.pods=n?[pod()]:[];observed.resources=resources();
 };
 return {deployment,target,postgres,observed,sourceSHA,rs,state,inventory,resources,stop,journal,step,converge};
}

test('stop/resume сохраняет template, scoped journal и точный positive join',async()=>{
 const f=setup(),before=structuredClone(f.deployment.spec),journal=f.journal(f.stop);
 assert.equal((await f.step('apply',f.stop,journal)).state,'CONVERGING');assert.equal(f.state.effects,1);
 assert.throws(()=>requireStoppedBackupController(f.deployment,f.resources(),{plan:f.stop,events:journal.events},f.inventory()),/NOT_STOPPED|STOP_REQUIRED/);
 f.converge();assert.equal((await f.step('status',f.stop,journal)).state,'STOPPED');
 const proof=requireStoppedBackupController(f.deployment,f.resources(),{plan:f.stop,events:journal.events},f.inventory());assert.equal(proof.planSHA256,fingerprint(f.stop));
 const resume=planBackupControllerMaintenance(f.deployment,f.target,f.observed,f.sourceSHA,'resume',{plan:f.stop,events:journal.events},f.inventory()),resumeJournal=f.journal(resume);
 f.state.gateValid=false;await assert.rejects(f.step('apply',resume,resumeJournal),/GATE_CHANGED/);assert.equal(f.state.effects,1);
 f.state.gateValid=true;assert.equal((await f.step('apply',resume,resumeJournal)).state,'CONVERGING');f.converge();
 assert.equal((await f.step('status',resume,resumeJournal)).state,'RESUMED');assert.deepEqual(f.deployment.spec,before);assert.equal(f.state.effects,2);
});

test('потерянный ACK разрешается readback без второго scale и без переписывания истории',async()=>{
 const f=setup(),journal=f.journal(f.stop);f.state.loseACK=true;
 await assert.rejects(f.step('apply',f.stop,journal),/LOST_ACK/);assert.equal(f.state.effects,1);f.converge();
 const count=journal.events.length;assert.equal((await f.step('status',f.stop,journal)).state,'STOPPED');
 assert.equal((await f.step('apply',f.stop,journal)).state,'STOPPED');assert.equal(f.state.effects,1);assert.equal(journal.events[count-1].type,'UNKNOWN');
});

test('Evicted без statuses и оставшийся init не являются подтверждённой остановкой',async()=>{
 const f=setup(),journal=f.journal(f.stop);await f.step('apply',f.stop,journal);const old=f.state.pods[0];f.converge();
 old.status={phase:'Failed',reason:'Evicted'};f.state.pods=[old];assert.equal((await f.step('status',f.stop,journal)).state,'CONVERGING');
 old.status.containerStatuses=old.spec.containers.map(c=>({name:c.name,state:{terminated:{exitCode:137}}}));old.spec.initContainers=[{name:'pending'}];
 assert.equal((await f.step('status',f.stop,journal)).state,'CONVERGING');
 old.status.initContainerStatuses=[{name:'pending',state:{terminated:{exitCode:0}}}];assert.equal((await f.step('status',f.stop,journal)).state,'STOPPED');
});

test('labels не скрывают owner Pod, а ReplicaSet должен прекратить производство Pod',async()=>{
 const f=setup(),journal=f.journal(f.stop);await f.step('apply',f.stop,journal);const pod=f.state.pods[0];f.converge();f.state.pods=[pod];pod.metadata.labels={};
 assert.equal((await f.step('status',f.stop,journal)).state,'CONVERGING');f.state.pods=[];f.rs.spec.replicas=1;
 await assert.rejects(f.step('status',f.stop,journal),/PRODUCER_NOT_STOPPED/);
});

test('чужой producer по Secret или service account запрещён, в том числе после gate',async()=>{
 const f=setup(),journal=f.journal(f.stop);
 const other={kind:'CronJob',metadata:{name:'foreign',uid:uid(400),namespace:'kodex-system'},secretRefs:['backup-controller-credentials'],serviceAccounts:[]};
 f.state.extra=[other];await assert.rejects(f.step('apply',f.stop,journal),/OTHER_PRODUCER/);assert.equal(f.state.effects,0);
 f.state.extra=[];await assert.rejects(f.step('apply',f.stop,journal,{gate:async()=>{f.state.extra=[{...other,secretRefs:[],serviceAccounts:['backup-controller']}];return {targetSHA256:fingerprint(f.target),deploymentUID:f.deployment.metadata.uid,sourceSHA:f.sourceSHA};}}),/OTHER_PRODUCER/);
 assert.equal(f.state.effects,0);assert.equal(journal.events.length,1);
});

test('исключение credential initializer относится только к точным PostgreSQL UID и lineage',()=>{
 const f=setup();const server={kind:'StatefulSet',metadata:{name:'kodex-postgresql',uid:f.postgres.ownerUID,labels:{'app.kubernetes.io/name':'kodex-postgresql'}},serviceAccounts:[],secretRefs:['kodex-postgresql-runtime-credentials']};
 const pod={kind:'Pod',metadata:{name:f.postgres.pod,uid:f.postgres.uid,labels:{'app.kubernetes.io/name':'kodex-postgresql'},ownerReferences:[{kind:'StatefulSet',uid:f.postgres.ownerUID,controller:true}]},serviceAccounts:['default'],secretRefs:server.secretRefs};
 f.state.extra=[server,pod];assert.doesNotThrow(()=>requireBackupProducerBoundary(f.inventory(),f.deployment.metadata.uid,false,f.postgres));
 pod.metadata.uid=uid(999);assert.throws(()=>requireBackupProducerBoundary(f.inventory(),f.deployment.metadata.uid,false,f.postgres),/OTHER_PRODUCER/);
});

test('foreign UID, изменённый template или чужой journal не допускают scale',async()=>{
 for(const mutate of [f=>f.deployment.metadata.uid=uid(999),f=>f.deployment.spec.template.spec.containers[0].args=['foreign']]){
  const f=setup(),journal=f.journal(f.stop);mutate(f);await assert.rejects(f.step('apply',f.stop,journal));assert.equal(f.state.effects,0);
 }
 const f=setup(),journal=f.journal(f.stop);journal.events[0].planSHA256='0'.repeat(64);await assert.rejects(f.step('apply',f.stop,journal),/JOURNAL_INVALID/);assert.equal(f.state.effects,0);
});

test('namespace discovery читает только metadata/refs и закрыто отвергает повреждённую projection',()=>{
 const row=['Deployment','backup-controller',uid(300),'kodex-system','backup-controller','[]','backup-controller','backup-controller-credentials','','','','1','1','1','1'].join('\t')+'\n';
 const calls=[];const value=readBackupProducerInventory(args=>{calls.push(args);return row;});assert.equal(value.items.length,1);
 assert.equal(calls[0].includes('-l'),false);assert.match(calls[0].at(-1),/secretKeyRef\.name/);assert.doesNotMatch(calls[0].at(-1),/\.env\}|\.data\}|\.value\}/);
 for(const bad of [row.replace(uid(300),'unknown'),row.replace('kodex-system','foreign'),row+'bad\n',row+row])assert.throws(()=>readBackupProducerInventory(()=>bad));
});
