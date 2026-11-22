import {resolve} from 'node:path';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {componentAPIMaintenanceStep} from './component-api-maintenance.mjs';
import {verifyMigrationAPIAdmission} from './component-migration-api-admission.mjs';
import {readPersonalIntegrationsAPIAdmission} from './personal-integrations-api-admission.mjs';
import {componentPodMatchesTemplate,componentServingInventory} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {fingerprint} from './scoped-release.mjs';

const name='control-api-gateway';
const same=(left,right)=>fingerprint(left)===fingerprint(right);
const fail=(ok,code)=>{if(!ok)throw Error(code);};

// Подтверждает exact staged API после внутреннего OPEN. Допуск CONVERGING
// нужен для запуска зависимых readers, но Service при этом изолирован.
export function verifyPersonalIntegrationsAPIReaders(migration,current,openPlan,openEvents,{allowPending=false}={}){
 verifyMigrationAPIAdmission(migration,current);
 const {closed,isolation,deployment,resources,proof}=current;
 const part=migration.componentHandoff?.components.find(value=>value.name===name);
 fail(part?.maintenance===true&&openPlan.action==='open'&&openPlan.sourceSHA===migration.source.revision&&
  openPlan.deploymentUID===part.uid&&openPlan.maintenanceID===proof.maintenanceID&&same(openPlan.target,closed.plan.target)&&
  openPlan.previousClosePlanSHA256===proof.planSHA256&&openPlan.trafficPlanSHA256===proof.trafficPlanSHA256&&
  openPlan.serviceUID===isolation.plan.serviceUID&&same(openPlan.beforeSpec,part.stagedSpec)&&
  openPlan.afterSpec?.replicas===closed.plan.beforeSpec.replicas,
  'PERSONAL_INTEGRATIONS_API_READERS_SCOPE_CHANGED');
 fail(Array.isArray(openEvents)&&openEvents[0]?.type==='HEADER'&&
  openEvents[0].schema==='kodex.component-api-maintenance-evidence/v1'&&openEvents[0].maintenanceID===proof.maintenanceID&&
  openEvents[0].planSHA256===fingerprint(openPlan),'PERSONAL_INTEGRATIONS_API_READERS_JOURNAL_INVALID');
 const intent=openEvents.findLastIndex(row=>row.type==='INTENT'&&row.step==='apply');
 fail(intent>0&&openEvents.some((row,index)=>index>intent&&row.type==='OBSERVED'&&
  (row.state==='OPEN'||allowPending&&row.state==='CONVERGING')&&row.deploymentUID===part.uid&&row.maintenanceID===proof.maintenanceID),
  'PERSONAL_INTEGRATIONS_API_READERS_OPEN_UNCONFIRMED');
 const status=componentAPIMaintenanceStep({phase:'status',plan:openPlan,journal:{events:openEvents,append:()=>{}},
  get:()=>deployment,resources:()=>resources,patch:()=>{throw Error('PERSONAL_INTEGRATIONS_API_READERS_READ_ONLY');}});
 const pending=allowPending&&status.state==='CONVERGING';
 fail(status.state==='OPEN'||pending,'PERSONAL_INTEGRATIONS_API_READERS_NOT_READY');
 if(pending){
  const descendants=activeDeploymentPods(componentServingInventory(resources,name,part.uid),part.uid);
  fail(descendants.every(pod=>componentPodMatchesTemplate(deployment,pod)),'PERSONAL_INTEGRATIONS_API_READERS_POD_CHANGED');
 }
 return {state:pending?'ISOLATED_READERS_STARTING':'ISOLATED_READERS_READY',sourceSHA:migration.source.revision,
  deploymentUID:part.uid,spec:structuredClone(deployment.spec),trafficPlanSHA256:proof.trafficPlanSHA256,
  openPlanSHA256:fingerprint(openPlan),serviceUID:proof.serviceUID};
}

// Читает исходный CLOSE и ISOLATE из migration plan. OPEN предъявляется
// отдельным журналом существующего maintenance оператора.
export function readPersonalIntegrationsAPIReaders({migration,openPlanPath,openEvidencePath,allowPending=false,onVerified},kube){
 fail([openPlanPath,openEvidencePath].every(path=>typeof path==='string'&&path===resolve(path)&&!/[\r\n\0]/.test(path)),
  'PERSONAL_INTEGRATIONS_API_READERS_INPUT_REQUIRED');
 const current=readPersonalIntegrationsAPIAdmission(migration.apiMaintenance,kube);
 const openPlan=readAuthorityPrivateJSON(openPlanPath);
 const openEvents=readAuthorityPrivateText(openEvidencePath).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
 const proof=verifyPersonalIntegrationsAPIReaders(migration,current,openPlan,openEvents,{allowPending});
 onVerified?.(current);
 return proof;
}
