#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
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
import {requireClosedAPIAdmission} from './component-api-traffic.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)),key='kodex.dev/component-maintenance';
const requireValue=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const header=plan=>({schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID});
const stopped=(deployment,resources)=>deployment.spec?.replicas===0&&deployment.status?.observedGeneration>=deployment.metadata.generation&&
  ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k=>(deployment.status[k]??0)===0)&&
  activeDeploymentPods(componentServingInventory(resources,'control-api-gateway',deployment.metadata.uid),deployment.metadata.uid).length===0;

function validatePlan(plan){
  const uuid=v=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v??'');
  requireValue(plan?.schema==='kodex.component-api-maintenance-plan/v1'&&['close','open'].includes(plan.action)&&
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
export function planComponentAPIMaintenance(deployment,target,observed,sourceSHA,action,previous,traffic){
  const scope=portableRunnerTarget(target,observed);
  requireValue(scope.allows(deployment)&&/^[a-f0-9]{40}$/.test(sourceSHA??'')&&['close','open'].includes(action),
    'COMPONENT_API_MAINTENANCE_SCOPE_INVALID');
  const maintenanceID=action==='close'?randomUUID():previous?.plan?.maintenanceID;
  if(action==='open'){
    requireClosedAPIMaintenance(deployment,observed.resources,previous);
    requireValue(traffic,'COMPONENT_API_MAINTENANCE_TRAFFIC_REQUIRED');
    requireClosedAPIAdmission(deployment,observed.resources,previous,traffic);
    requireValue(same(target,previous.plan.target),'COMPONENT_API_MAINTENANCE_SCOPE_INVALID');
  }
  const maintenanceSHA256=fingerprint({maintenanceID,targetSHA256:scope.digest});
  const change=planGatewayMaintenance(deployment,maintenanceSHA256,action==='open',scope,key);
  requireValue(deployment.status?.observedGeneration>=deployment.metadata.generation&&/^[0-9]+$/.test(deployment.metadata.resourceVersion),
    'COMPONENT_API_MAINTENANCE_OBSERVATION_REQUIRED');
  const plan={schema:'kodex.component-api-maintenance-plan/v1',action,sourceSHA,target,maintenanceID,maintenanceSHA256,
    deploymentUID:deployment.metadata.uid,beforeResourceVersion:deployment.metadata.resourceVersion,
    beforeSpec:structuredClone(deployment.spec),afterSpec:change.spec,beforeAnnotations:{...deployment.metadata.annotations},afterAnnotations:change.annotations,
    ...(action==='open'?{previousClosePlanSHA256:fingerprint(previous.plan),trafficPlanSHA256:fingerprint(traffic.plan),serviceUID:traffic.plan.serviceUID}:{})};
  validatePlan(plan);return plan;
}
export function componentAPIMaintenanceStep({phase,plan,journal,get,resources,patch,gate}){
  validatePlan(plan);
  requireValue(['apply','status'].includes(phase),'COMPONENT_API_MAINTENANCE_PHASE_INVALID');
  const actual=get();requireValue(actual.metadata?.uid===plan.deploymentUID,'COMPONENT_API_MAINTENANCE_UID_CHANGED');
  const before=same(actual.spec,plan.beforeSpec)&&same(actual.metadata.annotations??{},plan.beforeAnnotations);
  const after=same(actual.spec,plan.afterSpec)&&same(actual.metadata.annotations??{},plan.afterAnnotations);
  const closed=plan.action==='close'&&actual.metadata.annotations?.[key]===plan.afterAnnotations[key]&&stopped(actual,resources());
  const opened=plan.action==='open'&&after&&componentServingReady(actual,
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
  if(plan.action==='open'){
    requireValue(stopped(actual,resources()),'COMPONENT_API_MAINTENANCE_PODS_REMAIN');
    requireValue(typeof gate==='function','COMPONENT_API_MAINTENANCE_TRAFFIC_REQUIRED');
  }
  const operations=[{op:'test',path:'/metadata/uid',value:plan.deploymentUID},{op:'test',path:'/metadata/resourceVersion',value:actual.metadata.resourceVersion},
    {op:'test',path:'/spec',value:plan.beforeSpec},{op:'add',path:'/metadata/annotations',value:plan.afterAnnotations},{op:'replace',path:'/spec',value:plan.afterSpec}];
  patch(operations,true);
  if(plan.action==='open'){
    const proof=gate();requireValue(proof.trafficPlanSHA256===plan.trafficPlanSHA256&&proof.serviceUID===plan.serviceUID&&
      proof.planSHA256===plan.previousClosePlanSHA256&&proof.targetSHA256===fingerprint(plan.target),'COMPONENT_API_MAINTENANCE_TRAFFIC_CHANGED');
  }
  journal.append({type:'INTENT',step:'apply',beforeResourceVersion:actual.metadata.resourceVersion});
  try{patch(operations,false);}catch(error){journal.append({type:'UNKNOWN',step:'apply'});throw error;}
  return componentAPIMaintenanceStep({phase:'status',plan,journal,get,resources,patch,gate});
}

async function main(args){
  const phase=args.shift(),options={};
  while(args.length){const name=args.shift();requireValue(['--action','--kubeconfig','--context','--target','--plan','--evidence','--closed-plan',
    '--closed-evidence','--isolation-plan','--isolation-evidence','--approved-plan-sha'].includes(name)&&!Object.hasOwn(options,name)&&args.length,'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');options[name]=args.shift();}
  requireValue(['plan','apply','status'].includes(phase)&&options['--plan']&&options['--kubeconfig']?.startsWith('/')&&
    !/[\r\n\0]/.test(options['--kubeconfig'])&&/^[A-Za-z0-9_.:@/-]{1,160}$/.test(options['--context']??''),'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
  const env=Object.fromEntries(['HOME','PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
  const run=(cmd,argv)=>{try{return execFileSync(cmd,argv,{cwd:root,env,encoding:'utf8',timeout:35000,maxBuffer:4<<20,stdio:['ignore','pipe','pipe']});}
    catch{throw Error('COMPONENT_API_MAINTENANCE_OPERATION_FAILED_OR_UNKNOWN');}};
  const kube=argv=>run('kubectl',['--kubeconfig',options['--kubeconfig'],'--context',options['--context'],'--request-timeout=25s',...argv]);
  const get=(kind,name)=>JSON.parse(kube(['-n','kodex-system','get',kind,name,'-o','json']));
  const sourceSHA=run('git',['rev-parse','HEAD']).trim();requireValue(run('git',['status','--porcelain','--untracked-files=normal']).trim()==='',
    'COMPONENT_API_MAINTENANCE_CLEAN_SOURCE_REQUIRED');
  const cluster=JSON.parse(kube(['get','namespace','kube-system','-o','json'])),namespace=JSON.parse(kube(['get','namespace','kodex-system','-o','json']));
  const installation=get('configmap','kodex-installation-profile'),deployment=get('deployment','control-api-gateway');
  const observed={context:options['--context'],clusterUID:cluster.metadata.uid,namespaceUID:namespace.metadata.uid,installation};
  const inventory=()=>readComponentServingInventory(kube,'control-api-gateway',deployment.metadata.uid);
  const readEvents=(path,expected)=>{const journal=privateJournal(path,expected,{readOnly:true});try{return structuredClone(journal.events);}finally{journal.close();}};
  const openingProof=async()=>{
    requireValue(options['--closed-plan']&&options['--closed-evidence']&&options['--isolation-plan']&&options['--isolation-evidence'],
      'COMPONENT_API_MAINTENANCE_TRAFFIC_REQUIRED');
    const p=await readPrivateJSON(resolve(options['--closed-plan'])),t=await readPrivateJSON(resolve(options['--isolation-plan']));
    return {previous:{plan:p,events:readEvents(options['--closed-evidence'],header(p))},traffic:{plan:t,
      events:readEvents(options['--isolation-evidence'],{schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(t),maintenanceID:t.maintenanceID}),
      service:get('service','control-api-gateway'),
      slices:JSON.parse(kube(['-n','kodex-system','get','endpointslices','-l','kubernetes.io/service-name=control-api-gateway','-o','json'])),
      pods:JSON.parse(kube(['-n','kodex-system','get','pods','-l',`kodex.dev/component-traffic-isolation=${p.maintenanceID}`,'-o','json']))}};
  };
  if(phase==='plan'){
    requireValue(options['--target']&&!options['--evidence']&&!options['--approved-plan-sha'],'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
    let previous,traffic;
    if(options['--action']==='open'){
      ({previous,traffic}=await openingProof());
    }else requireValue(!options['--closed-plan']&&!options['--closed-evidence']&&!options['--isolation-plan']&&!options['--isolation-evidence'],
      'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
    const target=await readPrivateJSON(resolve(options['--target']));
    const plan=planComponentAPIMaintenance(deployment,target,{...observed,resources:inventory()},sourceSHA,options['--action'],previous,traffic);
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
  const journal=privateJournal(options['--evidence'],header(plan));
  try{
    const proof=plan.action==='open'?await openingProof():undefined;
    if(plan.action==='close')requireValue(!options['--closed-plan']&&!options['--closed-evidence']&&!options['--isolation-plan']&&!options['--isolation-evidence'],
      'COMPONENT_API_MAINTENANCE_ARGUMENTS_INVALID');
    const result=componentAPIMaintenanceStep({phase,plan,journal,get:()=>get('deployment','control-api-gateway'),resources:inventory,
      gate:()=>requireClosedAPIAdmission(get('deployment','control-api-gateway'),inventory(),proof.previous,{...proof.traffic,
        service:get('service','control-api-gateway'),slices:JSON.parse(kube(['-n','kodex-system','get','endpointslices','-l','kubernetes.io/service-name=control-api-gateway','-o','json'])),
        pods:JSON.parse(kube(['-n','kodex-system','get','pods','-l',`kodex.dev/component-traffic-isolation=${proof.previous.plan.maintenanceID}`,'-o','json']))}),
      patch:(operations,dryRun)=>withPrivatePatchFile(operations,path=>kube(['-n','kodex-system','patch','deployment','control-api-gateway','--type=json',
        '--patch-file',path,...(dryRun?['--dry-run=server']:[]),'-o','name']))});
    console.log(JSON.stringify(result));
  }finally{journal.close();}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)main(process.argv.slice(2)).catch(error=>{
  console.error(JSON.stringify({status:'FAIL_OR_UNKNOWN',code:/^[A-Z_]+$/.test(error.message??'')?error.message:'COMPONENT_API_MAINTENANCE_FAILED'}));process.exitCode=1;
});
