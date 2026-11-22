#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {migrationOwnerDatabase} from './migration-owner-database.mjs';
import {backupPostgresIdentity,readBackupMigrationJobPods} from './backup-controller-maintenance-cli.mjs';
import {workflowFileCancelledCloseAnnotation,workflowFileProvenanceMigration,validWorkflowFileProvenancePredecessorCatalog} from './workflow-file-provenance-migration-contract.mjs';
import {randomUUID} from 'node:crypto';
import {resolve} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {fingerprint,withPrivatePatchFile} from './scoped-release.mjs';
import {readPrivateJSON} from '../../infra/public-ingress/edge-plan.mjs';
import {createPrivatePrettyJSONSync} from './private-json-write.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {planGatewayMaintenance} from './runner-policy-model.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {componentServingInventory,readComponentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {requireClosedAPIAdmission,componentAPIIsolationNonce} from './component-api-traffic.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)),key='kodex.dev/component-maintenance';
const requireValue=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const header=plan=>({schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID});
const stopped=(deployment,resources)=>deployment.spec?.replicas===0&&deployment.status?.observedGeneration>=deployment.metadata.generation&&
  ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k=>(deployment.status[k]??0)===0)&&
  activeDeploymentPods(componentServingInventory(resources,'control-api-gateway',deployment.metadata.uid),deployment.metadata.uid).length===0;

function validatePlan(plan){
  const uuid=v=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v??'');
  requireValue(plan?.schema==='kodex.component-api-maintenance-plan/v1'&&['close','open','cancel'].includes(plan.action)&&
    /^[a-f0-9]{40}$/.test(plan.sourceSHA??'')&&uuid(plan.maintenanceID)&&uuid(plan.deploymentUID)&&
    plan.target?.schema==='kodex.portable-runner-target/v1'&&plan.target.resources?.some(r=>r.kind==='Deployment'&&
      r.name==='control-api-gateway'&&r.uid===plan.deploymentUID)&&/^[0-9]+$/.test(plan.beforeResourceVersion??'')&&
    plan.beforeSpec&&plan.afterSpec&&plan.beforeAnnotations&&plan.afterAnnotations&&
    plan.maintenanceSHA256===fingerprint({maintenanceID:plan.maintenanceID,targetSHA256:fingerprint(plan.target)}),
    'COMPONENT_API_MAINTENANCE_PLAN_INVALID');
  const close=plan.action==='close',replicas=close?plan.beforeSpec.replicas:plan.afterSpec.replicas;
  const marker=JSON.stringify({bundleSHA256:plan.maintenanceSHA256,replicas});
  const expectedSpec=structuredClone(plan.beforeSpec),expectedAnnotations={...plan.beforeAnnotations};
  expectedSpec.replicas=close?0:replicas;
  if(close){
    requireValue(!plan.beforeAnnotations[key]&&!plan.beforeAnnotations['kodex.dev/runner-policy-maintenance'],
      'COMPONENT_API_MAINTENANCE_PLAN_INVALID');expectedAnnotations[key]=marker;
  }else{
    requireValue(plan.beforeSpec.replicas===0&&plan.beforeAnnotations[key]===marker&&
      /^[a-f0-9]{64}$/.test(plan.previousClosePlanSHA256??'')&&/^[a-f0-9]{64}$/.test(plan.trafficPlanSHA256??'')&&uuid(plan.serviceUID),
      'COMPONENT_API_MAINTENANCE_PLAN_INVALID');delete expectedAnnotations[key];
  }
  if(plan.action==='cancel'||plan.recoveryService!==undefined){
    const service=plan.recoveryService;
    requireValue(plan.action!=='open'&&service&&uuid(service.uid)&&
      /^[a-f0-9]{64}$/.test(service.specSHA256??'')&&service.annotations&&
      typeof service.annotations==='object'&&!Array.isArray(service.annotations)&&
      Object.values(service.annotations).every(value=>typeof value==='string')&&
      !Object.hasOwn(service.annotations,'kodex.dev/component-traffic-isolation')&&
      plan.target.resources.some(resource=>resource.apiVersion==='v1'&&resource.kind==='Service'&&
        resource.name==='control-api-gateway'&&resource.uid===service.uid)&&
      (close||plan.serviceUID===service.uid&&plan.trafficPlanSHA256===fingerprint(service)),
      'COMPONENT_API_MAINTENANCE_RECOVERY_SERVICE_INVALID');
  }
  requireValue(Number.isSafeInteger(replicas)&&replicas>0&&replicas<=8&&same(expectedSpec,plan.afterSpec)&&
    same(expectedAnnotations,plan.afterAnnotations),'COMPONENT_API_MAINTENANCE_PLAN_INVALID');
}

// Gate допускает только принадлежащую конкретному журналу закрытую API boundary.
// Изменение image/template под maintenance допустимо; открытие не возвращает их назад.
export function requireClosedAPIMaintenance(deployment,resources,{plan,events}){
  validatePlan(plan);
  requireValue(plan?.schema==='kodex.component-api-maintenance-plan/v1'&&plan.action==='close'&&
    deployment?.apiVersion==='apps/v1'&&deployment.kind==='Deployment'&&!deployment.metadata?.deletionTimestamp&&
    deployment.metadata?.uid===plan.deploymentUID&&deployment.metadata.name==='control-api-gateway'&&deployment.metadata.namespace==='kodex-system'&&
    deployment.metadata.annotations?.[key]===plan.afterAnnotations[key]&&stopped(deployment,resources),
    'COMPONENT_API_MAINTENANCE_REQUIRED');
  return confirmedAPIMaintenance({plan,events});
}
// Исторический join используется только вместе со свежим доказательством
// изоляции Service; сам по себе он не означает, что API сейчас закрыт.
export function confirmedAPIMaintenance({plan,events}){
  validatePlan(plan);requireValue(plan.action==='close','COMPONENT_API_MAINTENANCE_PLAN_INVALID');
  requireValue(Array.isArray(events)&&events[0]?.type==='HEADER'&&Object.entries(header(plan)).every(([k,v])=>events[0][k]===v),
    'COMPONENT_API_MAINTENANCE_JOURNAL_INVALID');
  const intent=events.findLastIndex(e=>e.type==='INTENT'&&e.step==='apply');
  requireValue(intent>0&&events.some((e,i)=>i>intent&&e.type==='OBSERVED'&&e.state==='CLOSED'&&e.deploymentUID===plan.deploymentUID&&
    e.maintenanceID===plan.maintenanceID),'COMPONENT_API_MAINTENANCE_UNCONFIRMED');
  return {planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID,deploymentUID:plan.deploymentUID,targetSHA256:fingerprint(plan.target)};
}
// Открытое состояние выводится из exact нового open plan и его собственного
// INTENT/OPEN, а не из числа replicas либо исторического close receipt.
export function confirmedAPIOpenMaintenance(closed,opened,traffic){
  const proof=confirmedAPIMaintenance(closed),{plan,events}=opened;
  validatePlan(plan);
  requireValue(plan.action==='open'&&plan.maintenanceID===proof.maintenanceID&&plan.deploymentUID===proof.deploymentUID&&
    same(plan.target,closed.plan.target)&&plan.previousClosePlanSHA256===proof.planSHA256&&
    plan.trafficPlanSHA256===fingerprint(traffic.plan)&&plan.serviceUID===traffic.plan.serviceUID&&
    plan.afterSpec.replicas===closed.plan.beforeSpec.replicas&&Array.isArray(events)&&events[0]?.type==='HEADER'&&
    Object.entries(header(plan)).every(([key,value])=>events[0][key]===value),
    'COMPONENT_API_MAINTENANCE_OPEN_RECEIPT_REQUIRED');
  const intent=events.findLastIndex(row=>row.type==='INTENT'&&row.step==='apply');
  requireValue(intent>0&&events.some((row,index)=>index>intent&&row.type==='OBSERVED'&&row.state==='OPEN'&&
    row.deploymentUID===proof.deploymentUID&&row.maintenanceID===proof.maintenanceID),
    'COMPONENT_API_MAINTENANCE_OPEN_UNCONFIRMED');
  return {...proof,openPlanSHA256:fingerprint(plan),openEvidenceSHA256:fingerprint(events),trafficPlanSHA256:plan.trafficPlanSHA256,
    serviceUID:plan.serviceUID,replicas:plan.afterSpec.replicas,specSHA256:fingerprint(plan.afterSpec)};
}
// Исторический OPEN receipt не заменяет свежую Service isolation и готовность
// текущего Deployment: прежний публичный predicate сохраняет обе проверки.
export function requireOpenedAPIMaintenance(deployment,resources,closed,opened,traffic){
  const proof=confirmedAPIOpenMaintenance(closed,opened,traffic),{plan}=opened;
  requireClosedAPIAdmission(deployment,resources,closed,traffic);
  requireValue(deployment.apiVersion==='apps/v1'&&deployment.kind==='Deployment'&&deployment.metadata.uid===proof.deploymentUID&&
    !deployment.metadata.deletionTimestamp&&same(deployment.spec,plan.afterSpec)&&same(deployment.metadata.annotations??{},plan.afterAnnotations)&&
    componentServingReady(deployment,activeDeploymentPods(componentServingInventory(resources,'control-api-gateway',proof.deploymentUID),proof.deploymentUID),plan.afterSpec.replicas),
    'COMPONENT_API_MAINTENANCE_OPEN_NOT_READY');
  return proof;
}
// Service preimage закрепляется в плане close до первой остановки.
function pinRecoveryService(scope,service){
  requireValue(scope.allows(service)&&service?.apiVersion==='v1'&&service.kind==='Service'&&
    service.metadata.name==='control-api-gateway'&&!service.metadata.deletionTimestamp&&
    service.spec?.type==='ClusterIP'&&service.spec.selector?.['app.kubernetes.io/name']==='control-api-gateway'&&
    !Object.hasOwn(service.spec.selector,'kodex.dev/component-traffic-isolation')&&
    !Object.hasOwn(service.metadata.annotations??{},'kodex.dev/component-traffic-isolation'),
    'COMPONENT_API_MAINTENANCE_SERVICE_ALREADY_ISOLATED');
  return {uid:service.metadata.uid,specSHA256:fingerprint(service.spec),annotations:{...service.metadata.annotations}};
}

export function recoveryServiceState(service,ref,maintenanceID){
  requireValue(service?.kind==='Service'&&service.apiVersion==='v1'&&service.metadata?.uid===ref.uid&&
    service.metadata.name==='control-api-gateway'&&service.metadata.namespace==='kodex-system'&&
    !service.metadata.deletionTimestamp&&/^[0-9]+$/.test(service.metadata.resourceVersion??'')&&
    fingerprint(service.spec)===ref.specSHA256,'COMPONENT_API_MAINTENANCE_RECOVERY_SERVICE_CHANGED');
  const before=ref.annotations,after={...before,[workflowFileCancelledCloseAnnotation]:maintenanceID};
  const current=service.metadata.annotations??{};
  requireValue(same(current,before)||same(current,after),'COMPONENT_API_MAINTENANCE_RECOVERY_SERVICE_CHANGED');
  return {fenced:same(current,after),after};
}

export function planComponentAPIMaintenance(deployment,target,observed,sourceSHA,action,previous,traffic,recoveryService){
  const scope=portableRunnerTarget(target,observed);
  requireValue(scope.allows(deployment)&&/^[a-f0-9]{40}$/.test(sourceSHA??'')&&['close','open','cancel'].includes(action),
    'COMPONENT_API_MAINTENANCE_SCOPE_INVALID');
  const maintenanceID=action==='close'?randomUUID():previous?.plan?.maintenanceID;
  let recovery;
  if(action!=='close'){
    requireClosedAPIMaintenance(deployment,observed.resources,previous);
    requireValue(same(target,previous.plan.target),'COMPONENT_API_MAINTENANCE_SCOPE_INVALID');
    if(action==='open'){
      requireValue(traffic,'COMPONENT_API_MAINTENANCE_TRAFFIC_REQUIRED');
      requireClosedAPIAdmission(deployment,observed.resources,previous,traffic);
    }else{
      recovery=previous.plan.recoveryService;
      requireValue(recovery&&sourceSHA===previous.plan.sourceSHA&&same(deployment.spec,previous.plan.afterSpec),
        'COMPONENT_API_MAINTENANCE_RECOVERY_SCOPE_CHANGED');
      recoveryServiceState(recoveryService,recovery,maintenanceID);
    }
  }else if(recoveryService)recovery=pinRecoveryService(scope,recoveryService);
  const maintenanceSHA256=fingerprint({maintenanceID,targetSHA256:scope.digest});
  const change=planGatewayMaintenance(deployment,maintenanceSHA256,action!=='close',scope,key);
  requireValue(deployment.status?.observedGeneration>=deployment.metadata.generation&&/^[0-9]+$/.test(deployment.metadata.resourceVersion),
    'COMPONENT_API_MAINTENANCE_OBSERVATION_REQUIRED');
  const plan={schema:'kodex.component-api-maintenance-plan/v1',action,sourceSHA,target,maintenanceID,maintenanceSHA256,
    deploymentUID:deployment.metadata.uid,beforeResourceVersion:deployment.metadata.resourceVersion,
    beforeSpec:structuredClone(deployment.spec),afterSpec:change.spec,beforeAnnotations:{...deployment.metadata.annotations},afterAnnotations:change.annotations,
    ...(action==='open'?{previousClosePlanSHA256:fingerprint(previous.plan),trafficPlanSHA256:fingerprint(traffic.plan),serviceUID:traffic.plan.serviceUID}:{}),
    ...(action==='cancel'?{previousClosePlanSHA256:fingerprint(previous.plan),trafficPlanSHA256:fingerprint(recovery),serviceUID:recovery.uid}:{}),
    ...(recovery?{recoveryService:recovery}:{})};
  validatePlan(plan);return plan;
}
export function componentAPIMaintenanceStep({phase,plan,journal,get,resources,patch,gate}){
  validatePlan(plan);
  requireValue(['apply','status'].includes(phase),'COMPONENT_API_MAINTENANCE_PHASE_INVALID');
  const actual=get();requireValue(actual.metadata?.uid===plan.deploymentUID,'COMPONENT_API_MAINTENANCE_UID_CHANGED');
  const before=same(actual.spec,plan.beforeSpec)&&same(actual.metadata.annotations??{},plan.beforeAnnotations);
  const after=same(actual.spec,plan.afterSpec)&&same(actual.metadata.annotations??{},plan.afterAnnotations);
  const closed=plan.action==='close'&&actual.metadata.annotations?.[key]===plan.afterAnnotations[key]&&stopped(actual,resources());
  const opened=plan.action!=='close'&&after&&componentServingReady(actual,
    activeDeploymentPods(componentServingInventory(resources(),'control-api-gateway',plan.deploymentUID),plan.deploymentUID),plan.afterSpec.replicas);
  const state=closed?'CLOSED':opened?'OPEN':after?'CONVERGING':before?'BEFORE':'DRIFT';
  requireValue(state!=='DRIFT','COMPONENT_API_MAINTENANCE_SPEC_CHANGED');
  const observation={type:'OBSERVED',state,deploymentUID:plan.deploymentUID,maintenanceID:plan.maintenanceID,resourceVersion:actual.metadata.resourceVersion};
  if(phase==='status'){journal.append(observation);return observation;}
  const intent=journal.events.findLastIndex(e=>e.type==='INTENT'&&e.step==='apply');
  if(!before){requireValue(intent>0,'COMPONENT_API_MAINTENANCE_UNOWNED_STATE');journal.append(observation);return observation;}
  if(intent>=0)requireValue(journal.events.some((e,i)=>i>intent&&e.type==='OBSERVED'&&e.state==='BEFORE'&&
    e.resourceVersion===actual.metadata.resourceVersion),'COMPONENT_API_MAINTENANCE_OUTCOME_UNKNOWN');
  else requireValue(actual.metadata.resourceVersion===plan.beforeResourceVersion,'COMPONENT_API_MAINTENANCE_VERSION_CHANGED');
  if(plan.action!=='close'){
    requireValue(stopped(actual,resources()),'COMPONENT_API_MAINTENANCE_PODS_REMAIN');
    requireValue(typeof gate==='function','COMPONENT_API_MAINTENANCE_TRAFFIC_REQUIRED');
  }
  const operations=[{op:'test',path:'/metadata/uid',value:plan.deploymentUID},{op:'test',path:'/metadata/resourceVersion',value:actual.metadata.resourceVersion},
    {op:'test',path:'/spec',value:plan.beforeSpec},{op:'add',path:'/metadata/annotations',value:plan.afterAnnotations},{op:'replace',path:'/spec',value:plan.afterSpec}];
  patch(operations,true);
  if(plan.action!=='close'){
    const proof=gate();requireValue(proof.trafficPlanSHA256===plan.trafficPlanSHA256&&proof.serviceUID===plan.serviceUID&&
      proof.planSHA256===plan.previousClosePlanSHA256&&proof.targetSHA256===fingerprint(plan.target),'COMPONENT_API_MAINTENANCE_TRAFFIC_CHANGED');
  }
  journal.append({type:'INTENT',step:'apply',beforeResourceVersion:actual.metadata.resourceVersion});
  try{patch(operations,false);}catch(error){journal.append({type:'UNKNOWN',step:'apply'});throw error;}
  return componentAPIMaintenanceStep({phase:'status',plan,journal,get,resources,patch,gate});
}

// Проверка exact ART 050 отделена от durable Service fence. CHECK Job может
// только читать; любая DDL Job или связанный Pod запрещают раннюю отмену.
function requireUnstartedWorkflowFileMigration(kube){
  const name=`control-plane-migrate-${workflowFileProvenanceMigration.after}`;
  const absent=()=>!kube(['-n','kodex-system','get','job',name,'--ignore-not-found=true','-o','json']).trim()&&
    readBackupMigrationJobPods(kube,name,undefined).length===0;
  requireValue(absent(),'COMPONENT_API_MAINTENANCE_RECOVERY_WRITER_FOUND');
  const identity=()=>backupPostgresIdentity(kube),pod=identity(),db=migrationOwnerDatabase(kube,identity);
  const sql=readFileSync(new URL(workflowFileProvenanceMigration.proofFile,import.meta.url));
  const proof=db.read(sql,pod,db.identity(pod));
  requireValue(validWorkflowFileProvenancePredecessorCatalog(proof)&&absent(),
    'COMPONENT_API_MAINTENANCE_RECOVERY_OWNER_CHANGED');
}

export function fenceCancelledAPIClose({plan,previous,getService,closeJournal,journal,patchService,verifyOwner}){
  const proof=confirmedAPIMaintenance(previous),ref=plan.recoveryService;
  requireValue(plan.previousClosePlanSHA256===proof.planSHA256&&plan.serviceUID===ref.uid&&
    same(ref,previous.plan.recoveryService)&&same(plan.afterSpec,previous.plan.beforeSpec),
    'COMPONENT_API_MAINTENANCE_RECOVERY_SCOPE_CHANGED');
  const events=closeJournal.events.filter(row=>row.type==='CLOSE_CANCEL_INTENT');
  const planSHA256=fingerprint(plan);
  requireValue(events.length<=1&&events.every(row=>row.cancelPlanSHA256===planSHA256),
    'COMPONENT_API_MAINTENANCE_RECOVERY_SCOPE_CHANGED');
  verifyOwner();
  const service=getService(),state=recoveryServiceState(service,ref,plan.maintenanceID);
  const prior=journal.events.filter(row=>row.type==='SERVICE_FENCE_INTENT');
  if(state.fenced)requireValue(events.length===1&&prior.some(row=>row.serviceUID===ref.uid),
    'COMPONENT_API_MAINTENANCE_UNOWNED_STATE');
  if(!state.fenced){
    const patch=[{op:'test',path:'/metadata/uid',value:ref.uid},
      {op:'test',path:'/metadata/resourceVersion',value:service.metadata.resourceVersion},
      {op:'test',path:'/spec',value:service.spec},
      {op:'add',path:'/metadata/annotations',value:state.after}];
    patchService(patch,true);
    if(!events.length)closeJournal.append({type:'CLOSE_CANCEL_INTENT',cancelPlanSHA256:planSHA256});
    journal.append({type:'SERVICE_FENCE_INTENT',serviceUID:ref.uid,resourceVersion:service.metadata.resourceVersion});
    patchService(patch,false);
  }
  requireValue(recoveryServiceState(getService(),ref,plan.maintenanceID).fenced,
    'COMPONENT_API_MAINTENANCE_SERVICE_FENCE_UNCONFIRMED');
  return {...proof,trafficPlanSHA256:fingerprint(ref),serviceUID:ref.uid};
}

async function main(args){
  const phase=args.shift(),options={};
  while(args.length){const name=args.shift();requireValue(['--action','--kubeconfig','--context','--target','--plan','--evidence','--closed-plan','--workflow-file-provenance',
    '--closed-evidence','--isolation-plan','--isolation-evidence','--approved-plan-sha'].includes(name)&&!Object.hasOwn(options,name)&&args.length,'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');options[name]=args.shift();}
  requireValue(['plan','apply','status'].includes(phase)&&options['--plan']&&options['--kubeconfig']?.startsWith('/')&&
    !/[\r\n\0]/.test(options['--kubeconfig'])&&/^[A-Za-z0-9_.:@/-]{1,160}$/.test(options['--context']??''),'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
  const workflowProvenance=options['--workflow-file-provenance'];
  requireValue(workflowProvenance===undefined||workflowProvenance==='true'&&phase==='plan'&&options['--action']==='close',
    'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
  const env=Object.fromEntries(['HOME','PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
  const run=(cmd,argv,input)=>{try{return execFileSync(cmd,argv,{cwd:root,env,input,encoding:'utf8',timeout:35000,maxBuffer:4<<20,stdio:[input===undefined?'ignore':'pipe','pipe','pipe']});}
    catch{throw Error('COMPONENT_API_MAINTENANCE_OPERATION_FAILED_OR_UNKNOWN');}};
  const kube=(argv,input)=>run('kubectl',['--kubeconfig',options['--kubeconfig'],'--context',options['--context'],'--request-timeout=25s',...argv],input);
  const get=(kind,name)=>JSON.parse(kube(['-n','kodex-system','get',kind,name,'-o','json']));
  const sourceSHA=run('git',['rev-parse','HEAD']).trim();requireValue(run('git',['status','--porcelain','--untracked-files=normal']).trim()==='',
    'COMPONENT_API_MAINTENANCE_CLEAN_SOURCE_REQUIRED');
  const cluster=JSON.parse(kube(['get','namespace','kube-system','-o','json'])),namespace=JSON.parse(kube(['get','namespace','kodex-system','-o','json']));
  const installation=get('configmap','kodex-installation-profile'),deployment=get('deployment','control-api-gateway');
  const observed={context:options['--context'],clusterUID:cluster.metadata.uid,namespaceUID:namespace.metadata.uid,installation};
  const inventory=()=>readComponentServingInventory(kube,'control-api-gateway',deployment.metadata.uid);
  const readEvents=(path,expected)=>{const journal=privateJournal(path,expected,{readOnly:true});try{return structuredClone(journal.events);}finally{journal.close();}};
  const cancellationClose=async()=>{
    requireValue(options['--closed-plan']&&options['--closed-evidence']&&!options['--isolation-plan']&&!options['--isolation-evidence'],
      'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
    const plan=await readPrivateJSON(resolve(options['--closed-plan']));
    return {plan,events:readEvents(options['--closed-evidence'],header(plan))};
  };
  const openingProof=async()=>{
    requireValue(options['--closed-plan']&&options['--closed-evidence']&&options['--isolation-plan']&&options['--isolation-evidence'],
      'COMPONENT_API_MAINTENANCE_TRAFFIC_REQUIRED');
    const p=await readPrivateJSON(resolve(options['--closed-plan'])),t=await readPrivateJSON(resolve(options['--isolation-plan']));
    return {previous:{plan:p,events:readEvents(options['--closed-evidence'],header(p))},traffic:{plan:t,
      events:readEvents(options['--isolation-evidence'],{schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(t),maintenanceID:t.maintenanceID}),
      service:get('service','control-api-gateway'),
      slices:JSON.parse(kube(['-n','kodex-system','get','endpointslices','-l','kubernetes.io/service-name=control-api-gateway','-o','json'])),
      pods:JSON.parse(kube(['-n','kodex-system','get','pods','-l',`kodex.dev/component-traffic-isolation=${componentAPIIsolationNonce(t)}`,'-o','json']))}};
  };
  if(phase==='plan'){
    requireValue(options['--target']&&!options['--evidence']&&!options['--approved-plan-sha'],'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
    let previous,traffic,recovery;
    if(options['--action']==='open'){
      ({previous,traffic}=await openingProof());
    }else if(options['--action']==='cancel'){
      previous=await cancellationClose();
      requireValue(!previous.events.some(row=>row.type==='CLOSE_CANCEL_INTENT'),'COMPONENT_API_MAINTENANCE_RECOVERY_SCOPE_CHANGED');
      recovery=get('service','control-api-gateway');
    }else requireValue(!options['--closed-plan']&&!options['--closed-evidence']&&!options['--isolation-plan']&&!options['--isolation-evidence'],
      'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
    const target=await readPrivateJSON(resolve(options['--target']));
    if(workflowProvenance)recovery=get('service','control-api-gateway');
    const plan=planComponentAPIMaintenance(deployment,target,{...observed,resources:inventory()},sourceSHA,options['--action'],previous,traffic,recovery);
    if(plan.recoveryService)requireUnstartedWorkflowFileMigration(kube);
    createPrivatePrettyJSONSync(resolve(options['--plan']),plan);
    console.log(JSON.stringify({status:'PLAN_ONLY',action:plan.action,planSHA256:fingerprint(plan)}));return;
  }
  requireValue(options['--evidence']&&!options['--action']&&!options['--target'],
    'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
  const plan=await readPrivateJSON(resolve(options['--plan']));
  requireValue(plan.schema==='kodex.component-api-maintenance-plan/v1'&&plan.sourceSHA===sourceSHA&&
    portableRunnerTarget(plan.target,observed).allows(deployment),'COMPONENT_API_MAINTENANCE_TARGET_CHANGED');
  requireValue(phase==='apply'?options['--approved-plan-sha']===fingerprint(plan):!options['--approved-plan-sha'],
    'COMPONENT_API_MAINTENANCE_EXACT_PLAN_REQUIRED');
  let closeJournal;
  if(plan.action==='cancel'&&phase==='apply')requireValue(resolve(options['--closed-evidence']??'')!==resolve(options['--evidence']),
    'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
  const journal=privateJournal(options['--evidence'],header(plan));
  try{
    const proof=plan.action==='open'?await openingProof():undefined;
    const previous=plan.action==='cancel'?await cancellationClose():undefined;
    if(previous&&phase==='apply'){
      closeJournal=privateJournal(options['--closed-evidence'],header(previous.plan));
      previous.events=closeJournal.events;
    }
    if(plan.action==='close')requireValue(!options['--closed-plan']&&!options['--closed-evidence']&&!options['--isolation-plan']&&!options['--isolation-evidence'],
      'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
    if(plan.action==='close'&&phase==='apply'&&plan.recoveryService){
      requireValue(!journal.events.some(row=>row.type==='CLOSE_CANCEL_INTENT')&&
        !recoveryServiceState(get('service','control-api-gateway'),plan.recoveryService,plan.maintenanceID).fenced,
        'COMPONENT_API_MAINTENANCE_CLOSE_CANCELLED');
      requireUnstartedWorkflowFileMigration(kube);
    }
    const cancelGate=()=>{
      requireClosedAPIMaintenance(get('deployment','control-api-gateway'),inventory(),previous);
      return fenceCancelledAPIClose({plan,previous,closeJournal,journal,getService:()=>get('service','control-api-gateway'),
        verifyOwner:()=>requireUnstartedWorkflowFileMigration(kube),
        patchService:(operations,dryRun)=>withPrivatePatchFile(operations,path=>kube(['-n','kodex-system','patch',
          'service','control-api-gateway','--type=json','--patch-file',path,...(dryRun?['--dry-run=server']:[]),'-o','name']))});
    };
    const result=componentAPIMaintenanceStep({phase,plan,journal,get:()=>get('deployment','control-api-gateway'),resources:inventory,
      gate:plan.action==='cancel'?cancelGate:()=>requireClosedAPIAdmission(get('deployment','control-api-gateway'),inventory(),proof.previous,{...proof.traffic,
        service:get('service','control-api-gateway'),slices:JSON.parse(kube(['-n','kodex-system','get','endpointslices','-l','kubernetes.io/service-name=control-api-gateway','-o','json'])),
        pods:JSON.parse(kube(['-n','kodex-system','get','pods','-l',`kodex.dev/component-traffic-isolation=${componentAPIIsolationNonce(proof.traffic.plan)}`,'-o','json']))}),
      patch:(operations,dryRun)=>withPrivatePatchFile(operations,path=>kube(['-n','kodex-system','patch','deployment','control-api-gateway','--type=json',
        '--patch-file',path,...(dryRun?['--dry-run=server']:[]),'-o','name']))});
    console.log(JSON.stringify(result));
  }finally{closeJournal?.close();journal.close();}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)main(process.argv.slice(2)).catch(error=>{
  console.error(JSON.stringify({status:'FAIL_OR_UNKNOWN',code:/^[A-Z_]+$/.test(error.message??'')?error.message:'COMPONENT_API_MAINTENANCE_FAILED'}));process.exitCode=1;
});
