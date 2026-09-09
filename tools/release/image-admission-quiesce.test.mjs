import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,rmSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {fingerprint} from './scoped-release.mjs';
import {executeQuiescePhase,executeQuiesceRecovery} from './image-admission-quiesce.mjs';
import {buildQuiesceOpenPlan,buildQuiescePlan,classifyNoWorkLog,inspectQuiesceOpen,inspectQuiescePhase,inspectQuiesceRecovery,phases,quiesceMutation,quiesceOpenMutation,quiesceRecoveryMutation,validateQuiescePlan,validateQuiesceReceipt} from './image-admission-quiesce-model.mjs';

const context='k3s-staging',intent='11111111-1111-4111-8111-111111111111',image=`registry.local.kodex/kodex/image-admission@sha256:${'a'.repeat(64)}`;
let sequence=10;const uid=()=>`${String(sequence++).padStart(8,'0')}-1111-4111-8111-111111111111`;
function controller(){return {apiVersion:'apps/v1',kind:'Deployment',metadata:{name:'image-admission-controller',namespace:'kodex-system',uid:uid(),resourceVersion:'10',generation:1},
 spec:{replicas:1,strategy:{type:'Recreate'},selector:{matchLabels:{app:'controller'}},template:{spec:{containers:[{name:'image-admission-controller',image,command:['/usr/local/bin/image-admission-controller'],env:[]}]}}},
 status:{observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1}};}
function pod(owner){return {apiVersion:'v1',kind:'Pod',metadata:{name:'controller-pod',namespace:'kodex-system',uid:uid(),ownerReferences:[{uid:owner.metadata.uid,controller:true}]},status:{phase:'Running'}};}
function job(phase,{terminal=false,at='2026-09-09T10:00:00.000Z'}={}){const operationID=(phase.charCodeAt(0).toString(16)).repeat(32).slice(0,32),condition=terminal?[{type:'Failed',status:'True',lastTransitionTime:at}]:[];
 return {apiVersion:'batch/v1',kind:'Job',metadata:{name:`mc-admit-${operationID}-${phase}`,namespace:'kodex-system',uid:uid(),labels:{'kodex.dev/image-admission-orchestrated':'true','kodex.dev/image-admission-phase':phase,'kodex.dev/image-admission-id':operationID}},
  spec:{backoffLimit:0,activeDeadlineSeconds:720,ttlSecondsAfterFinished:3600,template:{spec:{containers:[]}}},status:{failed:terminal?1:0,conditions:condition},...(terminal?{quiesceTerminalClass:'NO_WORK'}:{})};}
function pvc(operationID){return {apiVersion:'v1',kind:'PersistentVolumeClaim',metadata:{name:`mc-admit-${operationID}`,namespace:'kodex-system',uid:uid(),labels:{'kodex.dev/image-admission-orchestrated':'true','kodex.dev/image-admission-id':operationID}},spec:{accessModes:['ReadWriteOnce']}};}
function snapshot(){const deployment=controller(),claim=job('claim'),promote=job('promote',{terminal:true});return {clusterUID:uid(),namespaceUID:uid(),namespace:{metadata:{labels:{'kodex.dev/environment':'staging'}}},controller:deployment,controllerPods:[pod(deployment)],jobs:[claim,promote],pvcs:[pvc(claim.metadata.labels['kodex.dev/image-admission-id'])],ownerState:{openBuilds:0,pendingAdmissions:0,pendingPromotions:0,activeRuntimeRuns:0,claimedRuntimeLeases:0,promotedArtifactCount:13,promotedPinsSHA256:'b'.repeat(64)}};}
function setSpec(state,spec,pods){state.controller.spec=structuredClone(spec);state.controller.metadata.resourceVersion=String(Number(state.controller.metadata.resourceVersion)+1);state.controller.metadata.generation++;state.controller.status.observedGeneration=state.controller.metadata.generation;state.controller.status.replicas=pods;state.controller.status.updatedReplicas=pods;state.controller.status.readyReplicas=pods;state.controller.status.availableReplicas=pods;state.controllerPods=pods?[pod(state.controller)]:[];}
function row(plan,phase,status,extra={}){return {at:'2026-09-09T10:01:00.000Z',intent:plan.intent,planSHA256:fingerprint(plan),phase,status,...extra};}
function noWorkLog(phase){const operation=phase==='claim'?'claim':'claim-promotion';return [...[1,12,24,36,48,60,72,84,96,108,120].map(attempt=>`image admission bridge claim retry: operation=${operation} attempt=${attempt}/120 class=no-work`),`image admission failed: owner ${phase==='claim'?'admission':'promotion'} work is unavailable`].join('\n')+'\n';}

test('terminal classifier accepts only the complete closed no-work diagnostic',()=>{assert.equal(classifyNoWorkLog('claim',noWorkLog('claim')),'NO_WORK');assert.equal(classifyNoWorkLog('promote',noWorkLog('promote')),'NO_WORK');
 assert.throws(()=>classifyNoWorkLog('promote',noWorkLog('promote').replace('class=no-work','class=dependency-unavailable')),/QUIESCE_NO_WORK_PROOF_REJECTED/);
 assert.throws(()=>classifyNoWorkLog('scan',noWorkLog('claim')),/QUIESCE_NO_WORK_PROOF_REJECTED/);});

test('quiesce stops exact controller before accepting no-work terminal lifecycle',()=>{const state=snapshot(),plan=buildQuiescePlan(state,{context,k3sSudo:true,intent,now:new Date('2026-09-09T10:00:00Z')});
 validateQuiescePlan(plan,context,true);assert.deepEqual(phases,['stop','terminal','cleanup-reader','cleanup','ttl-zero']);
 assert.deepEqual(quiesceMutation(plan,state,'stop',[],new Date('2026-09-09T10:00:01Z')).patch.slice(0,3).map(item=>item.path),['/metadata/uid','/metadata/resourceVersion','/spec']);
 setSpec(state,plan.controllerSpecs.stopped,0);assert.equal(inspectQuiescePhase(plan,state,'stop',[],new Date('2026-09-09T10:00:02Z')).target,'AFTER');
 state.jobs[0].status={failed:1,conditions:[{type:'Failed',status:'True',lastTransitionTime:'2026-09-09T10:02:00Z'}]};state.jobs[0].quiesceTerminalClass='NO_WORK';
 const terminal=inspectQuiescePhase(plan,state,'terminal',[],new Date('2026-09-09T10:02:01Z'));assert.equal(terminal.terminalJobs.length,2);
 const history=[row(plan,'stop','PASS'),row(plan,'terminal','PASS',{terminalJobs:terminal.terminalJobs})];
 assert.equal(quiesceMutation(plan,state,'cleanup-reader',history,new Date('2026-09-09T10:02:02Z')).kind,'Deployment');
 setSpec(state,plan.controllerSpecs.paused,1);state.jobs=state.jobs.filter(item=>item.metadata.labels['kodex.dev/image-admission-phase']==='promote');state.pvcs=[];
 assert.equal(inspectQuiescePhase(plan,state,'cleanup-reader',history,new Date('2026-09-09T10:02:03Z')).target,'AFTER');
 assert.equal(inspectQuiescePhase(plan,state,'cleanup',history,new Date('2026-09-09T10:02:03Z')).work.jobs.length,1);
 state.jobs=[];assert.equal(inspectQuiescePhase(plan,state,'ttl-zero',history,new Date('2026-09-09T11:00:01Z')).work.jobs.length,0);
 const complete=[...history,row(plan,'cleanup-reader','PASS'),row(plan,'cleanup','PASS'),row(plan,'ttl-zero','PASS')];assert.equal(validateQuiesceReceipt(plan,complete).controllerUID,plan.controllerUID);
});

test('quiesce rejects disappearance without durable terminal and expiry proof',()=>{const state=snapshot(),plan=buildQuiescePlan(state,{context,k3sSudo:true,intent,now:new Date('2026-09-09T10:00:00Z')});setSpec(state,plan.controllerSpecs.stopped,0);
 state.jobs=state.jobs.filter(item=>item.metadata.labels['kodex.dev/image-admission-phase']==='promote');
 assert.throws(()=>inspectQuiescePhase(plan,state,'terminal',[],new Date('2026-09-09T10:05:00Z')),/QUIESCE_JOB_DISAPPEARED_WITHOUT_PROOF/);
});

test('quiesce rejects successful new work, owner drift, replacement and expired handle',()=>{for(const mutate of [
 state=>{state.jobs[0].status={succeeded:1,completionTime:'2026-09-09T10:02:00Z',conditions:[{type:'Complete',status:'True',lastTransitionTime:'2026-09-09T10:02:00Z'}]};},
 state=>{state.jobs[0].status={failed:1,conditions:[{type:'Failed',status:'True',lastTransitionTime:'2026-09-09T10:02:00Z'}]};},
 state=>{state.ownerState.pendingAdmissions=1;state.jobs[0].status={failed:1,conditions:[{type:'Failed',status:'True',lastTransitionTime:'2026-09-09T10:02:00Z'}]};},
 state=>{state.jobs[0].spec.template.spec.containers.push({name:'changed'});},
 ]){const state=snapshot(),plan=buildQuiescePlan(state,{context,k3sSudo:true,intent,now:new Date('2026-09-09T10:00:00Z')});setSpec(state,plan.controllerSpecs.stopped,0);mutate(state);assert.throws(()=>inspectQuiescePhase(plan,state,'terminal',[],new Date('2026-09-09T10:02:01Z')));}
 const state=snapshot(),plan=buildQuiescePlan(state,{context,k3sSudo:true,intent,now:new Date('2026-09-09T10:00:00Z')});setSpec(state,plan.controllerSpecs.stopped,0);
 assert.throws(()=>inspectQuiescePhase(plan,state,'terminal',[],new Date('2026-09-09T12:00:00Z')),/QUIESCE_DEADLINE_EXCEEDED/);
 const succeeded=snapshot();succeeded.jobs[1].status={succeeded:1,completionTime:'2026-09-09T09:59:00Z',conditions:[{type:'Complete',status:'True',lastTransitionTime:'2026-09-09T09:59:00Z'}]};delete succeeded.jobs[1].quiesceTerminalClass;
 assert.throws(()=>buildQuiescePlan(succeeded,{context,k3sSudo:true,intent,now:new Date('2026-09-09T10:00:00Z')}),/QUIESCE_WORK_SUCCEEDED/);
});

test('bounded recovery restores exact initial controller after stop, cleanup or deadline',()=>{for(const target of ['stopped','paused']){const state=snapshot(),plan=buildQuiescePlan(state,{context,k3sSudo:true,intent,now:new Date('2026-09-09T10:00:00Z')});setSpec(state,plan.controllerSpecs[target],target==='stopped'?0:1);state.ownerState.pendingAdmissions=1;
  const mutation=quiesceRecoveryMutation(plan,state);assert.equal(mutation.patch[3].value.replicas,1);setSpec(state,plan.controllerSpecs.initial,1);assert.equal(inspectQuiesceRecovery(plan,state).target,'AFTER');}
});

test('open is a separate exact CAS after delivery and may create new work only after mutation',()=>{const state=snapshot(),plan=buildQuiescePlan(state,{context,k3sSudo:true,intent,now:new Date('2026-09-09T10:00:00Z')});setSpec(state,plan.controllerSpecs.paused,1);state.jobs=[];state.pvcs=[];
 state.jobsPolicy={spec:{policy:'jobs'}};state.releasePolicy={spec:{policy:'release'}};state.releaseBinding={spec:{binding:'release'}};
 const rows=phases.map(phase=>row(plan,phase,'PASS')),receipt=validateQuiesceReceipt(plan,rows),completion={deliveryIntent:'22222222-2222-4222-8222-222222222222',deliveryPlanSHA256:'c'.repeat(64),deliveryEvidenceSHA256:'d'.repeat(64),
  controllerSpecSHA256:fingerprint(state.controller.spec),jobsPolicySpecSHA256:fingerprint(state.jobsPolicy.spec),releasePolicySpecSHA256:fingerprint(state.releasePolicy.spec),releaseBindingSpecSHA256:fingerprint(state.releaseBinding.spec)};
 const open=buildQuiesceOpenPlan(state,plan,receipt,completion,{intent:'33333333-3333-4333-8333-333333333333'});assert.equal(quiesceOpenMutation(open,state).patch[3].value.template.spec.containers[0].env.find(item=>item.name==='IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS').value,'false');
 const weakened=structuredClone(open);weakened.after.template.spec.containers[0].env.push({name:'UNPLANNED',value:'true'});assert.throws(()=>quiesceOpenMutation(weakened,state),/QUIESCE_OPEN_PLAN_INVALID/);
 state.jobsPolicy.spec.policy='foreign';assert.throws(()=>buildQuiesceOpenPlan(state,plan,receipt,completion,{intent:'44444444-4444-4444-8444-444444444444'}),/QUIESCE_OPEN_COMPLETION_REQUIRED/);state.jobsPolicy.spec.policy='jobs';
 setSpec(state,open.after,1);state.jobs=[job('claim')];assert.equal(inspectQuiesceOpen(open,state).target,'AFTER');
});

test('unknown mutation requires read-only resume and recovery does not need owner or Job logs',async()=>{const state=snapshot(),plan=buildQuiescePlan(state,{context,k3sSudo:true,intent,now:new Date()}),directory=mkdtempSync(join(tmpdir(),'quiesce-unknown-')),evidence=join(directory,'evidence.jsonl');let patches=0;
 const rt={get:(kind,name)=>kind==='deployment'?state.controller:kind==='namespace'&&name==='kube-system'?{metadata:{uid:state.clusterUID}}:{metadata:{uid:state.namespaceUID}},
  list:kind=>kind==='replicasets,pods'?state.controllerPods:kind==='jobs'?state.jobs:kind==='persistentvolumeclaims'?state.pvcs:[],owner:()=>state.ownerState,
  logs:(_name,phase)=>noWorkLog(phase),patch:()=>{patches++;},rollout:()=>{}};
 try{assert.deepEqual(await executeQuiescePhase(plan,'stop',evidence,'apply',rt),{status:'UNKNOWN',code:'QUIESCE_MUTATION_OUTCOME_UNCONFIRMED'});assert.equal(patches,1);
  await assert.rejects(()=>executeQuiescePhase(plan,'stop',evidence,'apply',rt),/QUIESCE_PHASE_REQUIRES_RESUME/);assert.equal(patches,1);
  assert.equal((await executeQuiescePhase(plan,'stop',evidence,'resume',rt)).status,'UNKNOWN');assert.equal(patches,1);
  setSpec(state,plan.controllerSpecs.stopped,0);rt.owner=()=>{throw new Error('OWNER_UNAVAILABLE');};rt.logs=()=>{throw new Error('LOGS_UNAVAILABLE');};
  assert.equal((await executeQuiesceRecovery(plan,evidence,'apply',rt)).status,'UNKNOWN');assert.equal(patches,2);
  await assert.rejects(()=>executeQuiesceRecovery(plan,evidence,'apply',rt),/QUIESCE_RECOVERY_REQUIRES_RESUME/);assert.equal(patches,2);
  assert.equal((await executeQuiesceRecovery(plan,evidence,'resume',rt)).status,'UNKNOWN');assert.equal(patches,2);
 }finally{rmSync(directory,{recursive:true,force:true});}
});
