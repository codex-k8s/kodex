import {fingerprint} from './scoped-release.mjs';

export const namespace='kodex-system';
export const controllerName='image-admission-controller';
export const pauseEnvironment='IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS';
export const phases=['stop','terminal','cleanup-reader','cleanup','ttl-zero'];
export const maximumQuiesceSeconds=4800;

const uid=/^[a-f0-9][a-f0-9-]{7,127}$/;
const sha=/^[a-f0-9]{64}$/;
const requireValue=(value,code)=>{if(!value)throw new Error(code);};
const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&
 Object.keys(value).sort().join('\0')===[...keys].sort().join('\0');

function controllerApplication(deployment,{healthy=true}={}) {
 requireValue(deployment?.apiVersion==='apps/v1'&&deployment.kind==='Deployment'&&deployment.metadata?.namespace===namespace&&
  deployment.metadata.name===controllerName&&uid.test(deployment.metadata.uid??'')&&/^\d+$/.test(deployment.metadata.resourceVersion??'')&&
  !deployment.metadata.deletionTimestamp&&deployment.spec?.strategy?.type==='Recreate'&&deployment.spec.paused!==true,
 'EXACT_QUIESCE_CONTROLLER_REQUIRED');
 if(healthy)requireValue(deployment.spec.replicas===1&&deployment.status?.observedGeneration>=deployment.metadata.generation&&
  ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(key=>deployment.status?.[key]===1),'EXACT_HEALTHY_QUIESCE_CONTROLLER_REQUIRED');
 const containers=deployment.spec.template?.spec?.containers??[],matches=containers.filter(item=>item.name===controllerName);
 requireValue(matches.length===1&&fingerprint(matches[0].command)===fingerprint(['/usr/local/bin/image-admission-controller'])&&
  (!matches[0].args||matches[0].args.length===0)&&/@sha256:[a-f0-9]{64}$/.test(matches[0].image??''),'EXACT_QUIESCE_CONTROLLER_REQUIRED');
 return matches[0];
}

function pauseValue(container) {
 const entries=(container.env??[]).filter(item=>item.name===pauseEnvironment);
 requireValue(entries.length<=1&&(!entries.length||typeof entries[0].value==='string'&&!entries[0].valueFrom&&['true','false'].includes(entries[0].value)),
  'EXACT_QUIESCE_PAUSE_REQUIRED');
 return entries[0]?.value??'false';
}

function pausedSpec(initial) {
 const result=structuredClone(initial),app=result.template.spec.containers.find(item=>item.name===controllerName),entries=app.env??=[];
 const current=entries.filter(item=>item.name===pauseEnvironment);
 requireValue(current.length<=1&&(current.length===0||current[0].value==='false'&&!current[0].valueFrom),'EXACT_QUIESCE_PAUSE_REQUIRED');
 if(current.length)current[0].value='true';else {app.env??=[];app.env.push({name:pauseEnvironment,value:'true'});}
 result.replicas=1;return result;
}

function terminalCondition(job) {
 return (job.status?.conditions??[]).find(item=>['Complete','Failed'].includes(item.type)&&item.status==='True')??null;
}

export function classifyNoWorkLog(phase,output) {
 const operation=phase==='claim'?'claim':phase==='promote'?'claim-promotion':null;
 requireValue(operation&&typeof output==='string'&&output.length<=8192,'QUIESCE_NO_WORK_PROOF_REJECTED');
 const attempts=[1,...Array.from({length:10},(_,index)=>(index+1)*12)];
 const expected=[...attempts.map(attempt=>`image admission bridge claim retry: operation=${operation} attempt=${attempt}/120 class=no-work`),
  `image admission failed: owner ${phase==='claim'?'admission':'promotion'} work is unavailable`];
 requireValue(fingerprint(output.trim().split('\n'))===fingerprint(expected),'QUIESCE_NO_WORK_PROOF_REJECTED');
 return 'NO_WORK';
}

function jobState(job) {
 requireValue(job?.apiVersion==='batch/v1'&&job.kind==='Job'&&job.metadata?.namespace===namespace&&uid.test(job.metadata.uid??'')&&
  !job.metadata.deletionTimestamp&&job.metadata.labels?.['kodex.dev/image-admission-orchestrated']==='true','EXACT_QUIESCE_JOB_REQUIRED');
 const phase=job.metadata.labels?.['kodex.dev/image-admission-phase'],operationID=job.metadata.labels?.['kodex.dev/image-admission-id'];
 requireValue(['claim','scan','sign','admit','promote'].includes(phase)&&/^[a-f0-9]{32}$/.test(operationID??'')&&
  job.metadata.name===`mc-admit-${operationID}-${phase}`&&job.spec?.backoffLimit===0&&job.spec?.activeDeadlineSeconds===720&&
  job.spec?.ttlSecondsAfterFinished===3600,'EXACT_QUIESCE_JOB_REQUIRED');
 const condition=terminalCondition(job),terminal=Boolean(condition||job.status?.succeeded>0||job.status?.failed>0);
 let terminalAt=null,expiryAt=null,outcome='ACTIVE';
 if(terminal) {
  terminalAt=job.status?.completionTime??condition?.lastTransitionTime??null;
  requireValue(typeof terminalAt==='string'&&Number.isFinite(Date.parse(terminalAt)),'EXACT_QUIESCE_TERMINAL_TIME_REQUIRED');
  expiryAt=new Date(Date.parse(terminalAt)+job.spec.ttlSecondsAfterFinished*1000).toISOString();
  outcome=job.status?.succeeded>0||condition?.type==='Complete'?'SUCCEEDED':'FAILED';
 }
 const terminalClass=outcome==='FAILED'?job.quiesceTerminalClass??null:null;
 requireValue(outcome!=='FAILED'||terminalClass==='NO_WORK','QUIESCE_NO_WORK_PROOF_REQUIRED');
 return {name:job.metadata.name,uid:job.metadata.uid,specSHA256:fingerprint(job.spec),phase,operationID,terminal,outcome,terminalClass,terminalAt,expiryAt};
}

function pvcState(pvc) {
 requireValue(pvc?.apiVersion==='v1'&&pvc.kind==='PersistentVolumeClaim'&&pvc.metadata?.namespace===namespace&&uid.test(pvc.metadata.uid??'')&&
  !pvc.metadata.deletionTimestamp&&pvc.metadata.labels?.['kodex.dev/image-admission-orchestrated']==='true','EXACT_QUIESCE_PVC_REQUIRED');
 const operationID=pvc.metadata.labels?.['kodex.dev/image-admission-id'];
 requireValue(/^[a-f0-9]{32}$/.test(operationID??'')&&pvc.metadata.name===`mc-admit-${operationID}`,'EXACT_QUIESCE_PVC_REQUIRED');
 return {name:pvc.metadata.name,uid:pvc.metadata.uid,specSHA256:fingerprint(pvc.spec),operationID};
}

export function workState(snapshot) {
 return {jobs:(snapshot.jobs??[]).map(jobState).sort((a,b)=>a.name.localeCompare(b.name)),
  pvcs:(snapshot.pvcs??[]).map(pvcState).sort((a,b)=>a.name.localeCompare(b.name))};
}

function owner(snapshot) {
 const value=snapshot.ownerState;
 requireValue(value&&['openBuilds','pendingAdmissions','pendingPromotions','activeRuntimeRuns','claimedRuntimeLeases'].every(key=>value[key]===0)&&
  Number.isSafeInteger(value.promotedArtifactCount)&&value.promotedArtifactCount>=0&&sha.test(value.promotedPinsSHA256??''),'FRESH_IDLE_OWNER_STATE_REQUIRED');
 return {promotedArtifactCount:value.promotedArtifactCount,promotedPinsSHA256:value.promotedPinsSHA256};
}

function exactOwner(plan,snapshot) {
 const current=owner(snapshot);requireValue(fingerprint(current)===fingerprint(plan.owner),'QUIESCE_OWNER_OR_PINS_CHANGED');
}

function resourcePin(resource,field='spec') {
 requireValue(resource&&uid.test(resource.metadata?.uid??'')&&/^\d+$/.test(resource.metadata?.resourceVersion??''),'QUIESCE_OPEN_TARGET_REQUIRED');
 return {uid:resource.metadata.uid,resourceVersion:resource.metadata.resourceVersion,digest:fingerprint(resource[field])};
}
const validResourcePin=value=>exact(value,['uid','resourceVersion','digest'])&&uid.test(value.uid??'')&&/^\d+$/.test(value.resourceVersion??'')&&sha.test(value.digest??'');

function exactOpenTargets(completion,snapshot) {
 const resources=completion.resources;requireValue(exact(resources,['jobsPolicy','releasePolicy','releaseBinding','jobsBinding','parameters','policyConfig'])&&
  fingerprint(resourcePin(snapshot.jobsPolicy))===fingerprint(resources.jobsPolicy)&&fingerprint(resourcePin(snapshot.releasePolicy))===fingerprint(resources.releasePolicy)&&
  fingerprint(resourcePin(snapshot.releaseBinding))===fingerprint(resources.releaseBinding)&&fingerprint(resourcePin(snapshot.jobsBinding))===fingerprint(resources.jobsBinding)&&
  fingerprint(resourcePin(snapshot.parameters))===fingerprint(resources.parameters)&&fingerprint(resourcePin(snapshot.policyConfig,'data'))===fingerprint(resources.policyConfig)&&
  snapshot.controllerExecutableSHA256===completion.controllerExecutableSHA256,'QUIESCE_OPEN_TARGET_CHANGED');
}

function exactControllerPods(snapshot,count) {
 const pods=snapshot.controllerPods??[];requireValue(pods.length===count&&pods.every(pod=>!pod.metadata?.deletionTimestamp),
  count?'EXACT_QUIESCE_CONTROLLER_POD_REQUIRED':'QUIESCE_CONTROLLER_NOT_STOPPED');
}

export function buildQuiescePlan(snapshot,{context,k3sSudo,intent,now=new Date()}) {
 const app=controllerApplication(snapshot.controller);requireValue(pauseValue(app)==='false','QUIESCE_ALREADY_STARTED');exactControllerPods(snapshot,1);
 requireValue(typeof context==='string'&&context&&!/prod/i.test(context)&&k3sSudo===true&&uid.test(intent??'')&&
  uid.test(snapshot.clusterUID??'')&&uid.test(snapshot.namespaceUID??'')&&snapshot.namespace?.metadata?.labels?.['kodex.dev/environment']==='staging',
 'INVALID_QUIESCE_INPUT');
 const initial=structuredClone(snapshot.controller.spec),stopped=structuredClone(initial);stopped.replicas=0;
 const paused=pausedSpec(initial),work=workState(snapshot);requireValue(work.jobs.every(item=>item.outcome!=='SUCCEEDED'),'QUIESCE_WORK_SUCCEEDED');
 const startedAt=new Date(now).toISOString(),deadlineAt=new Date(new Date(now).getTime()+maximumQuiesceSeconds*1000).toISOString();
 return {version:1,intent,context,k3sSudo,clusterUID:snapshot.clusterUID,namespaceUID:snapshot.namespaceUID,startedAt,deadlineAt,
  controllerUID:snapshot.controller.metadata.uid,controllerInitialResourceVersion:snapshot.controller.metadata.resourceVersion,
  controllerSpecs:{initial,stopped,paused},controllerImage:app.image,owner:owner(snapshot),initialWork:work,
  targets:{stop:{before:initial,after:stopped},'cleanup-reader':{before:stopped,after:paused}}};
}

export function validateQuiescePlan(plan,context,k3sSudo) {
 requireValue(plan?.version===1&&uid.test(plan.intent??'')&&plan.context===context&&plan.k3sSudo===k3sSudo&&uid.test(plan.clusterUID??'')&&
  uid.test(plan.namespaceUID??'')&&uid.test(plan.controllerUID??'')&&/^\d+$/.test(plan.controllerInitialResourceVersion??'')&&
  Number.isFinite(Date.parse(plan.startedAt))&&Date.parse(plan.deadlineAt)-Date.parse(plan.startedAt)===maximumQuiesceSeconds*1000&&
  exact(plan.controllerSpecs,['initial','stopped','paused'])&&exact(plan.targets,['stop','cleanup-reader'])&&
  fingerprint(plan.targets.stop.before)===fingerprint(plan.controllerSpecs.initial)&&fingerprint(plan.targets.stop.after)===fingerprint(plan.controllerSpecs.stopped)&&
  fingerprint(plan.targets['cleanup-reader'].before)===fingerprint(plan.controllerSpecs.stopped)&&fingerprint(plan.targets['cleanup-reader'].after)===fingerprint(plan.controllerSpecs.paused)&&
  plan.controllerSpecs.initial.replicas===1&&plan.controllerSpecs.stopped.replicas===0&&plan.controllerSpecs.paused.replicas===1&&
  pauseValue(controllerApplication({apiVersion:'apps/v1',kind:'Deployment',metadata:{name:controllerName,namespace,uid:plan.controllerUID,resourceVersion:'1'},spec:plan.controllerSpecs.paused},{healthy:false}))==='true',
 'QUIESCE_PLAN_INVALID');
 return plan;
}

function knownTerminal(rows) {
 const result=new Map();for(const row of rows??[])for(const item of row.terminalJobs??[])if(item.terminal)result.set(item.uid,item);return result;
}

function inventory(plan,snapshot,rows,{allowPVCDeletion=false,allowJobDeletion=false,now=new Date()}={}) {
 const actual=workState(snapshot),expectedJobs=new Map(plan.initialWork.jobs.map(item=>[item.uid,item])),expectedPVCs=new Map(plan.initialWork.pvcs.map(item=>[item.uid,item])),known=knownTerminal(rows);
 for(const job of actual.jobs) {const before=expectedJobs.get(job.uid);requireValue(before&&before.name===job.name&&before.specSHA256===job.specSHA256,'QUIESCE_JOB_ADDED_OR_REPLACED');}
 for(const pvc of actual.pvcs) {const before=expectedPVCs.get(pvc.uid);requireValue(before&&before.name===pvc.name&&before.specSHA256===pvc.specSHA256,'QUIESCE_PVC_ADDED_OR_REPLACED');}
 for(const before of plan.initialWork.jobs)if(!actual.jobs.some(item=>item.uid===before.uid)) {
  const proof=before.terminal?before:known.get(before.uid),expired=proof?.expiryAt&&new Date(now).getTime()>=Date.parse(proof.expiryAt);
  requireValue(allowJobDeletion&&(before.phase!=='promote'||expired)&&proof?.terminal,'QUIESCE_JOB_DISAPPEARED_WITHOUT_PROOF');
 }
 for(const before of plan.initialWork.pvcs)if(!actual.pvcs.some(item=>item.uid===before.uid))
  requireValue(allowPVCDeletion,'QUIESCE_PVC_DISAPPEARED_WITHOUT_CLEANUP');
 return actual;
}

function controllerState(plan,snapshot) {
 requireValue(snapshot.controller?.metadata?.uid===plan.controllerUID,'QUIESCE_CONTROLLER_IDENTITY_CHANGED');
 const digest=fingerprint(snapshot.controller.spec),entry=Object.entries(plan.controllerSpecs).find(([,spec])=>fingerprint(spec)===digest);
 requireValue(entry,'QUIESCE_CONTROLLER_SPEC_DRIFT');return entry[0];
}

export function inspectQuiescePhase(plan,snapshot,phase,rows=[],now=new Date()) {
 validateQuiescePlan(plan,plan.context,plan.k3sSudo);requireValue(phases.includes(phase)&&snapshot.clusterUID===plan.clusterUID&&snapshot.namespaceUID===plan.namespaceUID,'QUIESCE_IDENTITY_CHANGED');
 requireValue(new Date(now).getTime()<=Date.parse(plan.deadlineAt),'QUIESCE_DEADLINE_EXCEEDED');exactOwner(plan,snapshot);
 const state=controllerState(plan,snapshot);let work,target='AFTER',ready=true;
 if(phase==='stop') {work=inventory(plan,snapshot,rows);target=state==='initial'?'BEFORE':state==='stopped'?'AFTER':'DRIFT';exactControllerPods(snapshot,target==='AFTER'?0:1);}
 else if(phase==='terminal') {requireValue(state==='stopped','QUIESCE_PHASE_ORDER_REJECTED');exactControllerPods(snapshot,0);work=inventory(plan,snapshot,rows,{allowJobDeletion:true,now});
  const activeInitial=plan.initialWork.jobs.filter(item=>!item.terminal),actualByUID=new Map(work.jobs.map(item=>[item.uid,item])),known=knownTerminal(rows);
  ready=activeInitial.every(item=>{const current=actualByUID.get(item.uid)??known.get(item.uid);return current?.terminal&&current.outcome==='FAILED'&&current.terminalClass==='NO_WORK';});
  const completedUnexpectedly=plan.initialWork.jobs.some(item=>(actualByUID.get(item.uid)??known.get(item.uid)??item)?.outcome==='SUCCEEDED');
  requireValue(!completedUnexpectedly,'QUIESCE_WORK_SUCCEEDED');}
 else if(phase==='cleanup-reader') {target=state==='stopped'?'BEFORE':state==='paused'?'AFTER':'DRIFT';
  exactControllerPods(snapshot,target==='AFTER'?1:0);
  const known=knownTerminal(rows);requireValue(plan.initialWork.jobs.every(item=>item.terminal||known.get(item.uid)?.outcome==='FAILED'),'QUIESCE_TERMINAL_RECEIPT_REQUIRED');
  work=inventory(plan,snapshot,rows,{allowJobDeletion:true,allowPVCDeletion:target==='AFTER',now});}
 else {requireValue(state==='paused','QUIESCE_PHASE_ORDER_REJECTED');exactControllerPods(snapshot,1);work=inventory(plan,snapshot,rows,{allowPVCDeletion:true,allowJobDeletion:true,now});
  if(phase==='cleanup')ready=work.pvcs.length===0&&work.jobs.every(item=>item.phase==='promote');
  if(phase==='ttl-zero')ready=work.pvcs.length===0&&work.jobs.length===0;
 }
 requireValue(target!=='DRIFT','QUIESCE_CONTROLLER_SPEC_DRIFT');return {phase,target,controller:state,ready,work,
  terminalJobs:work.jobs.filter(item=>item.terminal)};
}

export function quiesceMutation(plan,snapshot,phase,rows=[],now=new Date()) {
 requireValue(['stop','cleanup-reader'].includes(phase),'QUIESCE_PHASE_HAS_NO_MUTATION');const inspected=inspectQuiescePhase(plan,snapshot,phase,rows,now);
 requireValue(inspected.ready,'QUIESCE_TERMINAL_RECEIPT_REQUIRED');
 requireValue(inspected.target==='BEFORE','QUIESCE_PHASE_ALREADY_APPLIED');const target=plan.targets[phase],resource=snapshot.controller;
 return {kind:'Deployment',name:controllerName,patch:[{op:'test',path:'/metadata/uid',value:resource.metadata.uid},
  {op:'test',path:'/metadata/resourceVersion',value:resource.metadata.resourceVersion},{op:'test',path:'/spec',value:target.before},
  {op:'replace',path:'/spec',value:target.after}]};
}

export function validateQuiesceReceipt(plan,rows) {
 validateQuiescePlan(plan,plan.context,plan.k3sSudo);requireValue(Array.isArray(rows)&&phases.every(phase=>rows.some(row=>row.phase===phase&&row.status==='PASS'))&&
  rows.every(row=>row.intent===plan.intent&&row.planSHA256===fingerprint(plan)),'QUIESCE_RECEIPT_REQUIRED');
 return {version:1,intent:plan.intent,planSHA256:fingerprint(plan),controllerUID:plan.controllerUID,pausedSpecSHA256:fingerprint(plan.controllerSpecs.paused),
  owner:plan.owner,completedAt:rows.findLast(row=>row.phase==='ttl-zero'&&row.status==='PASS').at};
}

export function inspectQuiesceRecovery(plan,snapshot) {
 validateQuiescePlan(plan,plan.context,plan.k3sSudo);requireValue(snapshot.clusterUID===plan.clusterUID&&snapshot.namespaceUID===plan.namespaceUID,
  'QUIESCE_IDENTITY_CHANGED');const state=controllerState(plan,snapshot);
 requireValue(['initial','stopped','paused'].includes(state),'QUIESCE_RECOVERY_STATE_REJECTED');
 exactControllerPods(snapshot,state==='stopped'?0:1);return {target:state==='initial'?'AFTER':'BEFORE',controller:state};
}

export function quiesceRecoveryMutation(plan,snapshot) {
 const state=inspectQuiesceRecovery(plan,snapshot);requireValue(state.target==='BEFORE','QUIESCE_RECOVERY_ALREADY_APPLIED');
 return {kind:'Deployment',name:controllerName,patch:[{op:'test',path:'/metadata/uid',value:snapshot.controller.metadata.uid},
  {op:'test',path:'/metadata/resourceVersion',value:snapshot.controller.metadata.resourceVersion},{op:'test',path:'/spec',value:snapshot.controller.spec},
  {op:'replace',path:'/spec',value:plan.controllerSpecs.initial}]};
}

export function buildQuiesceOpenPlan(snapshot,quiescePlan,receipt,completion,{intent}) {
 validateQuiescePlan(quiescePlan,quiescePlan.context,quiescePlan.k3sSudo);requireValue(receipt?.intent===quiescePlan.intent&&receipt.planSHA256===fingerprint(quiescePlan)&&
  exact(completion,['deliveryIntent','deliveryPlanSHA256','deliveryEvidenceSHA256','controllerSpecSHA256','controllerExecutableSHA256','controllerReader','resources'])&&
  uid.test(completion.deliveryIntent??'')&&['deliveryPlanSHA256','deliveryEvidenceSHA256','controllerSpecSHA256','controllerExecutableSHA256'].every(key=>sha.test(completion[key]??''))&&
  fingerprint(snapshot.controller?.spec)===completion.controllerSpecSHA256,
 'QUIESCE_OPEN_COMPLETION_REQUIRED');
 exactOpenTargets(completion,snapshot);
 const app=controllerApplication(snapshot.controller);requireValue(pauseValue(app)==='true'&&workState(snapshot).jobs.length===0&&workState(snapshot).pvcs.length===0,
  'QUIESCE_OPEN_PAUSED_EMPTY_REQUIRED');exactOwner(quiescePlan,snapshot);exactControllerPods(snapshot,1);
 const before=structuredClone(snapshot.controller.spec),after=structuredClone(before),afterApp=after.template.spec.containers.find(item=>item.name===controllerName);
 const entry=afterApp.env.find(item=>item.name===pauseEnvironment);requireValue(entry?.value==='true'&&!entry.valueFrom,'QUIESCE_OPEN_PAUSED_EMPTY_REQUIRED');entry.value='false';
 return {version:1,intent,context:quiescePlan.context,k3sSudo:quiescePlan.k3sSudo,clusterUID:quiescePlan.clusterUID,namespaceUID:quiescePlan.namespaceUID,
  quiesceIntent:quiescePlan.intent,quiescePlanSHA256:fingerprint(quiescePlan),receiptSHA256:fingerprint(receipt),completion,controllerUID:snapshot.controller.metadata.uid,
  controllerResourceVersion:snapshot.controller.metadata.resourceVersion,owner:quiescePlan.owner,before,after};
}

export function validateQuiesceOpenPlan(plan,context,k3sSudo) {
 requireValue(plan?.version===1&&uid.test(plan.intent??'')&&plan.context===context&&plan.k3sSudo===k3sSudo&&uid.test(plan.clusterUID??'')&&uid.test(plan.namespaceUID??'')&&
  uid.test(plan.quiesceIntent??'')&&sha.test(plan.quiescePlanSHA256??'')&&sha.test(plan.receiptSHA256??'')&&uid.test(plan.controllerUID??'')&&
  /^\d+$/.test(plan.controllerResourceVersion??'')&&exact(plan.completion,['deliveryIntent','deliveryPlanSHA256','deliveryEvidenceSHA256','controllerSpecSHA256','controllerExecutableSHA256','controllerReader','resources'])&&
  uid.test(plan.completion.deliveryIntent??'')&&['deliveryPlanSHA256','deliveryEvidenceSHA256','controllerSpecSHA256','controllerExecutableSHA256'].every(key=>sha.test(plan.completion[key]??''))&&
  exact(plan.completion.controllerReader,['name','uid','containerID','imageID','restarts'])&&uid.test(plan.completion.controllerReader.uid??'')&&
  /^containerd:\/\/[a-f0-9]{64}$/.test(plan.completion.controllerReader.containerID??'')&&Number.isSafeInteger(plan.completion.controllerReader.restarts)&&
  exact(plan.completion.resources,['jobsPolicy','releasePolicy','releaseBinding','jobsBinding','parameters','policyConfig'])&&Object.values(plan.completion.resources).every(validResourcePin)&&
  plan.before?.replicas===1&&plan.after?.replicas===1,'QUIESCE_OPEN_PLAN_INVALID');
 const expected=structuredClone(plan.before),app=expected.template?.spec?.containers?.find(item=>item.name===controllerName),entry=app?.env?.find(item=>item.name===pauseEnvironment);
 requireValue(entry?.value==='true'&&!entry.valueFrom,'QUIESCE_OPEN_PLAN_INVALID');entry.value='false';
 requireValue(fingerprint(expected)===fingerprint(plan.after),'QUIESCE_OPEN_PLAN_INVALID');return plan;
}

export function inspectQuiesceOpen(plan,snapshot) {
 validateQuiesceOpenPlan(plan,plan.context,plan.k3sSudo);requireValue(snapshot.clusterUID===plan.clusterUID&&snapshot.namespaceUID===plan.namespaceUID&&
  snapshot.controller?.metadata?.uid===plan.controllerUID,'QUIESCE_OPEN_IDENTITY_CHANGED');
 const digest=fingerprint(snapshot.controller.spec),target=digest===fingerprint(plan.after)?'AFTER':digest===fingerprint(plan.before)?'BEFORE':'DRIFT';
 requireValue(target!=='DRIFT','QUIESCE_OPEN_CONTROLLER_DRIFT');controllerApplication(snapshot.controller);exactControllerPods(snapshot,1);exactOpenTargets(plan.completion,snapshot);
 if(target==='BEFORE'){const current=owner(snapshot);requireValue(fingerprint(current)===fingerprint(plan.owner),'QUIESCE_OWNER_OR_PINS_CHANGED');
  const work=workState(snapshot);requireValue(work.jobs.length===0&&work.pvcs.length===0&&fingerprint(plan.completion.controllerReader)===fingerprint(snapshot.controllerReader),
   'QUIESCE_OPEN_WORK_OR_READER_CHANGED');}
 return {target};
}

export function quiesceOpenMutation(plan,snapshot) {
 requireValue(inspectQuiesceOpen(plan,snapshot).target==='BEFORE','QUIESCE_OPEN_ALREADY_APPLIED');return {kind:'Deployment',name:controllerName,
  patch:[{op:'test',path:'/metadata/uid',value:snapshot.controller.metadata.uid},{op:'test',path:'/metadata/resourceVersion',value:snapshot.controller.metadata.resourceVersion},
   {op:'test',path:'/spec',value:plan.before},{op:'replace',path:'/spec',value:plan.after}]};
}
