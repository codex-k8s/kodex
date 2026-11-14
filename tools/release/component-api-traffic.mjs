#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {resolve} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {fingerprint,withPrivatePatchFile} from './scoped-release.mjs';
import {readPrivateJSON} from '../../infra/public-ingress/edge-plan.mjs';
import {createPrivatePrettyJSONSync} from './private-json-write.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {readComponentServingInventory,componentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {confirmedAPIMaintenance,requireClosedAPIMaintenance} from './component-api-maintenance.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)),name='control-api-gateway',key='kodex.dev/component-traffic-isolation';
const requireValue=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const header=plan=>({schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID});
const apiHeader=plan=>({schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID});
const marker=plan=>JSON.stringify({maintenanceID:plan.maintenanceID,closePlanSHA256:plan.closePlanSHA256});
const identity=(service,plan)=>service?.apiVersion==='v1'&&service.kind==='Service'&&service.metadata?.name===name&&
  service.metadata.namespace==='kodex-system'&&service.metadata.uid===plan.serviceUID&&!service.metadata.deletionTimestamp;
function validateTerminal(proof,plan,now=Date.now()){
  const digest=v=>/^[a-f0-9]{64}$/.test(v??''),uuid=v=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v??'');
  requireValue(proof&&typeof proof==='object'&&!Array.isArray(proof),'COMPONENT_API_TRAFFIC_AUTHORITY_TERMINAL_REQUIRED');
  const {terminalSHA256,...body}=proof,observed=Date.parse(proof.observedAt);
  requireValue(proof.policyRevision===106&&uuid(proof.operationID)&&proof.targetSHA256===fingerprint(plan.target)&&
    proof.trafficPlanSHA256===plan.previousIsolationSHA256&&proof.serviceUID===plan.serviceUID&&
    proof.expectedReadbackCount===16&&Number.isSafeInteger(proof.actualReadbackCount)&&proof.actualReadbackCount>=16&&proof.actualReadbackCount<=384&&
    Number.isSafeInteger(proof.registryRevision)&&proof.registryRevision>0&&Number.isSafeInteger(proof.snapshotRevision)&&proof.snapshotRevision>0&&
    ['policySHA256','registrySHA256','snapshotSHA256','planSHA256','freshPlanSHA256'].every(k=>digest(proof[k]))&&
    observed<=now+5000&&now-observed<=30000&&digest(terminalSHA256)&&terminalSHA256===fingerprint(body),
    'COMPONENT_API_TRAFFIC_AUTHORITY_TERMINAL_REQUIRED');
}
function validate(plan){
  const uuid=v=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v??'');
  requireValue(plan?.schema==='kodex.component-api-traffic-plan/v1'&&['isolate','restore'].includes(plan.action)&&
    /^[a-f0-9]{40}$/.test(plan.sourceSHA??'')&&uuid(plan.maintenanceID)&&uuid(plan.serviceUID)&&/^[a-f0-9]{64}$/.test(plan.closePlanSHA256??'')&&
    plan.target?.schema==='kodex.portable-runner-target/v1'&&plan.target.resources?.some(r=>r.apiVersion==='v1'&&r.kind==='Service'&&r.name===name&&r.uid===plan.serviceUID)&&
    /^[0-9]+$/.test(plan.beforeResourceVersion??'')&&plan.beforeSpec&&plan.afterSpec&&plan.beforeAnnotations&&plan.afterAnnotations,
    'COMPONENT_API_TRAFFIC_PLAN_INVALID');
  const isolate=plan.action==='isolate',original=isolate?plan.beforeSpec:plan.afterSpec;
  requireValue(original.type==='ClusterIP'&&original.selector?.['app.kubernetes.io/name']===name&&Object.keys(original.selector).length>0&&
    !Object.hasOwn(original.selector,key)&&!original.externalIPs?.length&&!original.externalName,
    'COMPONENT_API_TRAFFIC_SERVICE_INVALID');
  const expectedSpec=structuredClone(original);expectedSpec.selector={...original.selector,[key]:plan.maintenanceID};
  const originalAnnotations=isolate?plan.beforeAnnotations:plan.afterAnnotations;
  requireValue(!Object.hasOwn(originalAnnotations,key)&&same(expectedSpec,isolate?plan.afterSpec:plan.beforeSpec)&&
    same({...originalAnnotations,[key]:marker(plan)},isolate?plan.afterAnnotations:plan.beforeAnnotations)&&
    (isolate||/^[a-f0-9]{64}$/.test(plan.previousIsolationSHA256??'')), 'COMPONENT_API_TRAFFIC_PLAN_INVALID');
  // Старый план остаётся читаемым после TTL; fresh bound проверяется перед
  // restore intent. Исторический readback не открывает admission повторно.
  if(!isolate)validateTerminal(plan.authority,plan,Date.parse(plan.authority?.observedAt));
  else requireValue(!Object.hasOwn(plan,'authority'),'COMPONENT_API_TRAFFIC_PLAN_INVALID');
}

// Проверяется отсутствие любых адресов, включая not-ready и terminating.
// Пустой discovery не доказывает завершение прежних TCP sessions: для них
// отдельно нужен положительный join API из исторического maintenance receipt.
export function requireIsolatedAPIService(service,slices,pods,plan){
  validate(plan);requireValue(plan.action==='isolate'&&identity(service,plan)&&same(service.spec,plan.afterSpec)&&
    same(service.metadata.annotations??{},plan.afterAnnotations),'COMPONENT_API_TRAFFIC_NOT_ISOLATED');
  requireValue(Array.isArray(slices?.items)&&!slices.metadata?.continue&&slices.items.every(slice=>
    slice.apiVersion==='discovery.k8s.io/v1'&&slice.kind==='EndpointSlice'&&slice.metadata?.namespace==='kodex-system'&&
    slice.metadata.labels?.['kubernetes.io/service-name']===name&&slice.metadata.ownerReferences?.some(o=>o.kind==='Service'&&o.uid===plan.serviceUID&&o.controller===true)&&
    Array.isArray(slice.endpoints)&&slice.endpoints.length===0),'COMPONENT_API_TRAFFIC_ENDPOINTS_REMAIN');
  requireValue(Array.isArray(pods?.items)&&!pods.metadata?.continue&&pods.items.every(p=>p.kind==='Pod'&&p.metadata?.namespace==='kodex-system'&&
    !Object.entries(plan.afterSpec.selector).every(([k,v])=>p.metadata.labels?.[k]===v)), 'COMPONENT_API_TRAFFIC_SELECTOR_MATCHES_POD');
}
function confirmedIsolation(plan,events){
  validate(plan);requireValue(plan.action==='isolate'&&Array.isArray(events)&&events[0]?.type==='HEADER'&&
    Object.entries(header(plan)).every(([k,v])=>events[0][k]===v),'COMPONENT_API_TRAFFIC_JOURNAL_INVALID');
  const intent=events.findLastIndex(e=>e.type==='INTENT'&&e.step==='apply');
  requireValue(intent>0&&events.some((e,i)=>i>intent&&e.type==='OBSERVED'&&e.state==='ISOLATED'&&e.serviceUID===plan.serviceUID&&
    e.maintenanceID===plan.maintenanceID),'COMPONENT_API_TRAFFIC_UNCONFIRMED');
}
export function requireClosedAPIAdmission(deployment,resources,closed,traffic){
  if(!traffic)return requireClosedAPIMaintenance(deployment,resources,closed);
  const proof=confirmedAPIMaintenance(closed),{plan,events,service,slices,pods}=traffic;
  requireValue(plan.closePlanSHA256===proof.planSHA256&&plan.maintenanceID===proof.maintenanceID&&
    same(plan.target,closed.plan.target)&&deployment?.metadata?.uid===proof.deploymentUID&&deployment.metadata.name===name&&
    deployment.metadata.namespace==='kodex-system'&&!deployment.metadata.deletionTimestamp,'COMPONENT_API_TRAFFIC_SCOPE_INVALID');
  confirmedIsolation(plan,events);requireIsolatedAPIService(service,slices,pods,plan);
  return {...proof,trafficPlanSHA256:fingerprint(plan),serviceUID:plan.serviceUID};
}
export function planComponentAPITraffic({service,deployment,resources,target,observed,sourceSHA,action,closed,previous,terminal}){
  const scope=portableRunnerTarget(target,observed),proof=confirmedAPIMaintenance(closed);
  requireValue(scope.allows(service)&&scope.allows(deployment)&&same(target,closed.plan.target)&&/^[a-f0-9]{40}$/.test(sourceSHA??'')&&
    ['isolate','restore'].includes(action),'COMPONENT_API_TRAFFIC_SCOPE_INVALID');
  if(action==='isolate')requireClosedAPIMaintenance(deployment,resources,closed);
  else{
    requireClosedAPIAdmission(deployment,resources,closed,{...previous,service});
    requireValue(componentServingReady(deployment,
      activeDeploymentPods(componentServingInventory(resources,name,deployment.metadata.uid),deployment.metadata.uid),deployment.spec?.replicas),
      'COMPONENT_API_TRAFFIC_READY_API_REQUIRED');
  }
  const plan={schema:'kodex.component-api-traffic-plan/v1',action,sourceSHA,target,maintenanceID:proof.maintenanceID,closePlanSHA256:proof.planSHA256,
    serviceUID:service.metadata.uid,beforeResourceVersion:service.metadata.resourceVersion,beforeSpec:structuredClone(service.spec),afterSpec:structuredClone(service.spec),
    beforeAnnotations:{...service.metadata.annotations},afterAnnotations:{...service.metadata.annotations},
    ...(action==='restore'?{previousIsolationSHA256:fingerprint(previous.plan),authority:terminal}:{})};
  if(action==='isolate'){
    plan.afterSpec.selector[key]=proof.maintenanceID;plan.afterAnnotations[key]=marker(plan);
  }else{delete plan.afterSpec.selector[key];delete plan.afterAnnotations[key];}
  validate(plan);if(action==='restore')validateTerminal(terminal,plan);return plan;
}
export function componentAPITrafficStep({phase,plan,journal,get,slices,pods,gate,patch}){
  validate(plan);requireValue(['apply','status'].includes(phase),'COMPONENT_API_TRAFFIC_PHASE_INVALID');
  const service=get();requireValue(identity(service,plan),'COMPONENT_API_TRAFFIC_UID_CHANGED');
  const before=same(service.spec,plan.beforeSpec)&&same(service.metadata.annotations??{},plan.beforeAnnotations);
  const after=same(service.spec,plan.afterSpec)&&same(service.metadata.annotations??{},plan.afterAnnotations);
  requireValue(before||after,'COMPONENT_API_TRAFFIC_DRIFT');
  let state=before?'BEFORE':plan.action==='restore'?'RESTORED':'CONVERGING';
  if(after&&plan.action==='isolate'){
    try{requireIsolatedAPIService(service,slices(),pods(),plan);state='ISOLATED';}
    catch(error){if(!['COMPONENT_API_TRAFFIC_ENDPOINTS_REMAIN','COMPONENT_API_TRAFFIC_SELECTOR_MATCHES_POD'].includes(error.message))throw error;}
  }
  const observation={type:'OBSERVED',state,serviceUID:plan.serviceUID,maintenanceID:plan.maintenanceID,resourceVersion:service.metadata.resourceVersion};
  if(phase==='status'){journal.append(observation);return observation;}
  const intent=journal.events.findLastIndex(e=>e.type==='INTENT'&&e.step==='apply');
  if(after){requireValue(intent>0,'COMPONENT_API_TRAFFIC_UNOWNED_STATE');journal.append(observation);return observation;}
  if(intent>=0)requireValue(journal.events.some((e,i)=>i>intent&&e.type==='OBSERVED'&&e.state==='BEFORE'&&e.resourceVersion===service.metadata.resourceVersion),
    'COMPONENT_API_TRAFFIC_OUTCOME_UNKNOWN');
  else requireValue(service.metadata.resourceVersion===plan.beforeResourceVersion,'COMPONENT_API_TRAFFIC_VERSION_CHANGED');
  const operations=[{op:'test',path:'/metadata/uid',value:plan.serviceUID},{op:'test',path:'/metadata/resourceVersion',value:service.metadata.resourceVersion},
    {op:'test',path:'/spec',value:plan.beforeSpec},{op:'add',path:'/metadata/annotations',value:plan.afterAnnotations},{op:'replace',path:'/spec',value:plan.afterSpec}];
  patch(operations,true);const terminal=gate();
  if(plan.action==='restore'){
    validateTerminal(terminal,plan);requireValue(same(terminal,plan.authority),'COMPONENT_API_TRAFFIC_AUTHORITY_CHANGED');
  }
  journal.append({type:'INTENT',step:'apply'});
  try{patch(operations,false);}catch(error){journal.append({type:'UNKNOWN',step:'apply'});throw error;}
  return componentAPITrafficStep({phase:'status',plan,journal,get,slices,pods,gate,patch});
}

async function main(args){
  const phase=args.shift(),options={};
  while(args.length){const k=args.shift();requireValue(['--action','--target','--plan','--evidence','--closed-plan','--closed-evidence',
    '--isolation-plan','--isolation-evidence','--authority-plan','--authority-evidence','--authority-status-plan','--authority-status-evidence',
    '--approved-plan-sha','--kubeconfig','--context'].includes(k)&&!Object.hasOwn(options,k)&&args.length,
    'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');options[k]=args.shift();}
  requireValue(['plan','apply','status'].includes(phase)&&options['--plan']&&options['--closed-plan']&&options['--closed-evidence']&&
    options['--kubeconfig']?.startsWith('/')&&!/[\r\n\0]/.test(options['--kubeconfig'])&&/^[A-Za-z0-9_.:@/-]{1,160}$/.test(options['--context']??''),
    'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');
  const env=Object.fromEntries(['HOME','PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
  const run=(command,argv)=>{try{return execFileSync(command,argv,{cwd:root,env,encoding:'utf8',timeout:35000,maxBuffer:8<<20,stdio:['ignore','pipe','pipe']});}
    catch{throw Error('COMPONENT_API_TRAFFIC_OPERATION_FAILED_OR_UNKNOWN');}};
  const kube=argv=>run('kubectl',['--kubeconfig',options['--kubeconfig'],'--context',options['--context'],'--request-timeout=25s',...argv]);
  const get=(kind,n)=>JSON.parse(kube(['-n','kodex-system','get',kind,n,'-o','json']));
  const sourceSHA=run('git',['rev-parse','HEAD']).trim();requireValue(!run('git',['status','--porcelain','--untracked-files=normal']).trim(),
    'COMPONENT_API_TRAFFIC_CLEAN_SOURCE_REQUIRED');
  const readEvidence=(path,expected)=>{const j=privateJournal(path,expected,{readOnly:true});try{return structuredClone(j.events);}finally{j.close();}};
  const closePlan=await readPrivateJSON(resolve(options['--closed-plan']));
  const closed={plan:closePlan,events:readEvidence(options['--closed-evidence'],apiHeader(closePlan))};
  const service=()=>get('service',name),deployment=()=>get('deployment',name);
  const slices=()=>JSON.parse(kube(['-n','kodex-system','get','endpointslices','-l',`kubernetes.io/service-name=${name}`,'-o','json']));
  const pods=()=>JSON.parse(kube(['-n','kodex-system','get','pods','-l',`${key}=${closePlan.maintenanceID}`,'-o','json']));
  const observed={context:options['--context'],clusterUID:JSON.parse(kube(['get','namespace','kube-system','-o','json'])).metadata.uid,
    namespaceUID:JSON.parse(kube(['get','namespace','kodex-system','-o','json'])).metadata.uid,installation:get('configmap','kodex-installation-profile')};
  let previous;
  if(options['--isolation-plan']){
    requireValue(options['--isolation-evidence'],'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');
    const plan=await readPrivateJSON(resolve(options['--isolation-plan']));previous={plan,events:readEvidence(options['--isolation-evidence'],header(plan))};
  }else requireValue(!options['--isolation-evidence'],'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');
  let terminal;
  const readTerminal=async()=>{
    requireValue(['--authority-plan','--authority-evidence','--authority-status-plan','--authority-status-evidence'].every(k=>options[k]),
      'COMPONENT_API_TRAFFIC_AUTHORITY_TERMINAL_REQUIRED');
    const {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON}=await import('./authority-catalog-transition.mjs');
    const {readCatalogAuthorityTerminal}=await import('./authority-rotation-transition.mjs');
    requireValue(typeof readCatalogAuthorityTerminal==='function','COMPONENT_API_TRAFFIC_AUTHORITY_TERMINAL_REQUIRED');
    const plan=readAuthorityPrivateJSON(options['--authority-plan']),freshPlan=readAuthorityPrivateJSON(options['--authority-status-plan']);
    requireValue(plan.context===options['--context']&&plan.kubeconfig===options['--kubeconfig']&&plan.revision===sourceSHA,
      'COMPONENT_API_TRAFFIC_AUTHORITY_SOURCE_CHANGED');
    const d=deployment(),resources=readComponentServingInventory(kube,name,d.metadata.uid);
    return readCatalogAuthorityTerminal({plan,freshPlan,
      events:readAuthorityPrivateText(options['--authority-evidence']).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON),
      freshEvents:readAuthorityPrivateText(options['--authority-status-evidence']).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON),
      admissionProof:requireClosedAPIAdmission(d,resources,closed,{...previous,service:service(),slices:slices(),pods:pods()})},kube);
  };
  const build=(target,action)=>{
    const d=deployment();return planComponentAPITraffic({service:service(),deployment:d,resources:readComponentServingInventory(kube,name,d.metadata.uid),
      target,observed,sourceSHA,action,closed,previous:previous?{...previous,slices:slices(),pods:pods()}:undefined,terminal});
  };
  if(phase==='plan'){
    requireValue(options['--target']&&!options['--evidence']&&!options['--approved-plan-sha'],'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');
    if(options['--action']==='restore')terminal=await readTerminal();
    const plan=build(await readPrivateJSON(resolve(options['--target'])),options['--action']);createPrivatePrettyJSONSync(resolve(options['--plan']),plan);
    console.log(JSON.stringify({status:'PLAN_ONLY',action:plan.action,planSHA256:fingerprint(plan)}));return;
  }
  requireValue(options['--evidence']&&!options['--action']&&!options['--target'],'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');
  const plan=await readPrivateJSON(resolve(options['--plan']));validate(plan);
  requireValue(plan.sourceSHA===sourceSHA&&portableRunnerTarget(plan.target,observed).allows(service())&&
    same(plan.target,closed.plan.target)&&plan.closePlanSHA256===fingerprint(closed.plan)&&
    (phase==='apply'?options['--approved-plan-sha']===fingerprint(plan):!options['--approved-plan-sha']),'COMPONENT_API_TRAFFIC_TARGET_CHANGED');
  const journal=privateJournal(options['--evidence'],header(plan));
  try{
    if(phase==='apply'&&plan.action==='restore'&&same(service().spec,plan.beforeSpec))terminal=await readTerminal();
    const result=componentAPITrafficStep({phase,plan,journal,get:service,slices,pods,gate:()=>{
      const fresh=build(plan.target,plan.action);requireValue(same(fresh,plan),'COMPONENT_API_TRAFFIC_GATE_CHANGED');
      return terminal;
    },patch:(operations,dryRun)=>withPrivatePatchFile(operations,path=>kube(['-n','kodex-system','patch','service',name,'--type=json','--patch-file',path,
      ...(dryRun?['--dry-run=server']:[]),'-o','name']))});console.log(JSON.stringify(result));
  }finally{journal.close();}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)main(process.argv.slice(2)).catch(error=>{
  console.error(JSON.stringify({status:'FAIL_OR_UNKNOWN',code:/^[A-Z_]+$/.test(error.message??'')?error.message:'COMPONENT_API_TRAFFIC_FAILED'}));process.exitCode=1;
});
