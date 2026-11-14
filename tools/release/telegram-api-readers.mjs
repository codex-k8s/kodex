import {fingerprint} from './scoped-release.mjs';
import {requireAuthorityAPIScope} from './authority-api-scope.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {componentAPIMaintenanceStep} from './component-api-maintenance.mjs';
import {requireClosedAPIAdmission,componentAPITrafficStep,sameAuthorityOutcome} from './component-api-traffic.mjs';
import {readComponentServingInventory} from './portable-component-migration.mjs';
import {telegramPolicyTransition} from './authority-telegram-transition.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const namespace='kodex-system',name='control-api-gateway';

// API readers разрешены только exact OPEN собственного maintenance journal:
// прежний положительный join и текущая пустая Service boundary обязательны.
// Счётчики Deployment и произвольные replicas3 не заменяют историю и Pod proof.
export function verifyTelegramAPIReaders({migration,authority,openPlan,openEvents,closed,traffic,deployment,resources,restored}){
 requireAuthorityAPIScope(closed.plan.target,authority.policyTransition.targetProfile);
 fail(fingerprint(closed.plan.target)===authority.maintenance?.proof?.targetSHA256,'TELEGRAM_API_READERS_SCOPE_CHANGED');
 const part=migration.componentHandoff?.components.find(c=>c.name===name);
 fail(part?.maintenance===true&&authority.policyTransition?.profile===telegramPolicyTransition&&
  authority.revision===migration.source.revision&&openPlan.sourceSHA===migration.source.revision&&openPlan.action==='open'&&
  openPlan.deploymentUID===part.uid&&same(openPlan.beforeSpec,part.stagedSpec)&&
  openPlan.previousClosePlanSHA256===authority.maintenance?.proof?.planSHA256&&
  openPlan.trafficPlanSHA256===authority.maintenance.proof.trafficPlanSHA256&&
  ['clusterUID','namespaceUID','installationUID','installationSHA256'].every(key=>
   migration.portableTarget[key]===openPlan.target[key]&&migration.portableTarget[key]===authority.policyTransition[key]),
  'TELEGRAM_API_READERS_SCOPE_CHANGED');
 fail(openEvents[0]?.type==='HEADER'&&openEvents[0].schema==='kodex.component-api-maintenance-evidence/v1'&&
  openEvents[0].planSHA256===fingerprint(openPlan)&&openEvents[0].maintenanceID===openPlan.maintenanceID,
  'TELEGRAM_API_READERS_JOURNAL_INVALID');
 const intent=openEvents.findLastIndex(e=>e.type==='INTENT'&&e.step==='apply');
 fail(intent>0&&openEvents.some((e,index)=>index>intent&&e.type==='OBSERVED'&&e.state==='OPEN'&&
  e.deploymentUID===part.uid&&e.maintenanceID===openPlan.maintenanceID),'TELEGRAM_API_READERS_OPEN_UNCONFIRMED');
 let admission;
 if(restored){
  const plan=restored.plan,rows=restored.events,intent=rows.findLastIndex(e=>e.type==='INTENT'&&e.step==='apply');
  const confirmed=rows.slice(1,intent).findLast(e=>e.type==='AUTHORITY_CONFIRMED');
  const {terminalSHA256,...terminalBody}=confirmed?.authority??{};
  fail(plan.action==='restore'&&plan.authorityTransition===telegramPolicyTransition&&plan.sourceSHA===migration.source.revision&&
   plan.previousIsolationSHA256===fingerprint(traffic.plan)&&plan.closePlanSHA256===fingerprint(closed.plan)&&
   plan.authority?.operationID===authority.ownerOperationID&&plan.authority.planSHA256===fingerprint(authority)&&
   same(plan.target,closed.plan.target)&&plan.authority.targetSHA256===fingerprint(closed.plan.target)&&
   (plan.authority.authorityTargetSHA256??plan.authority.targetSHA256)===authority.policyTransition.targetSHA256&&
   same(plan.beforeSpec,traffic.plan.afterSpec)&&same(plan.afterSpec,traffic.plan.beforeSpec)&&
   rows[0]?.type==='HEADER'&&rows[0].schema==='kodex.component-api-traffic-evidence/v1'&&rows[0].planSHA256===fingerprint(plan)&&rows[0].maintenanceID===plan.maintenanceID&&
   intent>0&&confirmed?.plannedTerminalSHA256===plan.authority.terminalSHA256&&terminalSHA256===fingerprint(terminalBody)&&
   sameAuthorityOutcome(confirmed.authority,plan.authority)&&rows.some((e,index)=>index>intent&&e.type==='OBSERVED'&&e.state==='RESTORED'&&e.serviceUID===plan.serviceUID&&e.maintenanceID===plan.maintenanceID),
   'TELEGRAM_API_RESTORE_JOURNAL_REQUIRED');
  const result=componentAPITrafficStep({phase:'status',plan,journal:{events:rows,append:()=>{}},get:()=>traffic.service,slices:()=>traffic.slices,pods:()=>traffic.pods,
   patch:()=>{throw Error('TELEGRAM_API_READERS_READ_ONLY_REQUIRED');}});
  fail(result.state==='RESTORED','TELEGRAM_API_RESTORE_READBACK_CHANGED');
  admission=authority.maintenance.proof;
 }else admission=requireClosedAPIAdmission(deployment,resources,closed,traffic);
 fail(admission.authorityTransition===telegramPolicyTransition&&same(admission,authority.maintenance.proof)&&
  admission.serviceUID===openPlan.serviceUID,'TELEGRAM_API_READERS_ISOLATION_CHANGED');
 const actual=componentAPIMaintenanceStep({phase:'status',plan:openPlan,journal:{events:openEvents,append:()=>{}},
  get:()=>deployment,resources:()=>resources,patch:()=>{throw Error('TELEGRAM_API_READERS_READ_ONLY_REQUIRED');}});
 fail(actual.state==='OPEN','TELEGRAM_API_READERS_NOT_READY');
 return {state:restored?'RESTORED_READERS_READY':'ISOLATED_READERS_READY',sourceSHA:migration.source.revision,deploymentUID:part.uid,
  openPlanSHA256:fingerprint(openPlan),trafficPlanSHA256:openPlan.trafficPlanSHA256,
  spec:structuredClone(deployment.spec),operationID:authority.ownerOperationID};
}

// Только приватные canonical inputs и свежие GET. Запуск/patch API остаётся
// отдельным штатным maintenance protocol; данный consumer не выдаёт effects.
export function readTelegramAPIReaders({migration,authority,openPlanPath,openEvidencePath,restorePlanPath,restoreEvidencePath},kube){
 const openPlan=readAuthorityPrivateJSON(openPlanPath),readEvents=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
 const maintenance=authority.maintenance;
 fail(maintenance?.trafficPlanPath&&maintenance.trafficEvidencePath,'TELEGRAM_API_READERS_ISOLATION_REQUIRED');
 const closed={plan:readAuthorityPrivateJSON(maintenance.planPath),events:readEvents(maintenance.evidencePath)};
 const trafficPlan=readAuthorityPrivateJSON(maintenance.trafficPlanPath);
 const get=(kind,resource)=>parseAuthorityPrivateJSON(kube(['-n',namespace,'get',kind,resource,'-o','json']));
 const deployment=get('deployment',name),resources=readComponentServingInventory(kube,name,deployment.metadata.uid);
 const traffic={plan:trafficPlan,events:readEvents(maintenance.trafficEvidencePath),service:get('service',name),
  slices:parseAuthorityPrivateJSON(kube(['-n',namespace,'get','endpointslices','-l',`kubernetes.io/service-name=${name}`,'-o','json'])),
  pods:parseAuthorityPrivateJSON(kube(['-n',namespace,'get','pods','-l',`kodex.dev/component-traffic-isolation=${trafficPlan.maintenanceID}`,'-o','json']))};
 fail(!restorePlanPath&&!restoreEvidencePath||restorePlanPath&&restoreEvidencePath,'TELEGRAM_API_RESTORE_JOURNAL_REQUIRED');
 const restored=restorePlanPath?{plan:readAuthorityPrivateJSON(restorePlanPath),events:readEvents(restoreEvidencePath)}:undefined;
 return verifyTelegramAPIReaders({migration,authority,openPlan,openEvents:readEvents(openEvidencePath),closed,traffic,deployment,resources,restored});
}
