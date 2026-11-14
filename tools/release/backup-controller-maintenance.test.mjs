import test from 'node:test';
import assert from 'node:assert/strict';
import {uid} from './fixtures/authority-catalog-synthetic.mjs';
import {backupMaintenanceFixture as setup} from './fixtures/backup-controller-maintenance-synthetic.mjs';
import {fingerprint} from './scoped-release.mjs';
import {canonicalPostgresBackupBoundarySHA256} from './backup-postgres-boundary.mjs';
import {backupControllerMaintenanceHeader,readBackupProducerInventory,requireBackupProducerBoundary,requireStoppedBackupController,planBackupControllerMaintenance,backupControllerMaintenanceStep} from './backup-controller-maintenance.mjs';


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
 const f=setup();const postgresBoundarySHA256=canonicalPostgresBackupBoundarySHA256();
 const server={kind:'StatefulSet',metadata:{name:'kodex-postgresql',uid:f.postgres.ownerUID,labels:{'app.kubernetes.io/name':'kodex-postgresql'}},serviceAccounts:[],secretRefs:['kodex-postgresql-runtime-credentials'],postgresBoundarySHA256};
 const pod={kind:'Pod',metadata:{name:f.postgres.pod,uid:f.postgres.uid,labels:{'app.kubernetes.io/name':'kodex-postgresql'},ownerReferences:[{kind:'StatefulSet',uid:f.postgres.ownerUID,controller:true}]},serviceAccounts:['default'],secretRefs:server.secretRefs,postgresBoundarySHA256};
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
 assert.equal(calls[0].includes('-l'),false);assert.ok(calls[0].includes('deployments,statefulsets,daemonsets,cronjobs,jobs,replicasets,replicationcontrollers,pods'));assert.ok(value.kinds.includes('ReplicationController'));assert.match(calls[0].at(-1),/secretKeyRef\.name/);assert.doesNotMatch(calls[0].at(-1),/\.env\}|\.data\}|\.value\}/);
 for(const bad of [row.replace(uid(300),'unknown'),row.replace('kodex-system','foreign'),row+'bad\n',row+row])assert.throws(()=>readBackupProducerInventory(()=>bad));
});
