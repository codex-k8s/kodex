import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {readComponentServingInventory} from './portable-component-migration.mjs';
import {requireClosedAPIAdmission} from './component-api-traffic.mjs';
import {requireClosedAPIMaintenance} from './component-api-maintenance.mjs';
import {isPersonalIntegrationsMigrationProfile} from './personal-integrations-migration-contract.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const events=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
export const personalIntegrationsAPIAdmissionSchema='kodex.personal-integrations-api-admission/v1';

// Generic close/isolate доказывает только физическую изоляцию API. Здесь нет
// утверждения baseline106/107/110, подмены target hash или schema049 READY.
// Exact049 подтверждает отдельный owner inspect;107/110 — отдельный owner read.
export function readPersonalIntegrationsAPIAdmission({profile,isolationPlanPath,isolationEvidencePath,closedPlanPath,closedEvidencePath},kube,{stopped=false}={}){
 fail(isPersonalIntegrationsMigrationProfile(profile)&&[isolationPlanPath,isolationEvidencePath,closedPlanPath,closedEvidencePath]
  .every(value=>typeof value==='string'&&value===resolve(value)&&!/[\r\n\0]/.test(value)),
  'PERSONAL_INTEGRATIONS_API_INPUT_REQUIRED');
 const isolation={plan:readAuthorityPrivateJSON(isolationPlanPath),events:events(isolationEvidencePath)};
 const closed={plan:readAuthorityPrivateJSON(closedPlanPath),events:events(closedEvidencePath)};
 fail(isolation.plan.action==='isolate'&&!Object.hasOwn(isolation.plan,'migrationRestore')&&
  !Object.hasOwn(isolation.plan,'migrationRestoreProfile')&&!Object.hasOwn(isolation.plan,'authorityTransition')&&
  isolation.plan.sourceSHA===closed.plan.sourceSHA&&/^[a-f0-9]{40}$/.test(closed.plan.sourceSHA??''),
  'PERSONAL_INTEGRATIONS_API_ISOLATION_PROFILE_REQUIRED');
 const get=(kind,name)=>parseAuthorityPrivateJSON(kube(['-n','kodex-system','get',kind,name,'-o','json']));
 const deployment=get('deployment','control-api-gateway');
 const resources=readComponentServingInventory(kube,'control-api-gateway',deployment.metadata.uid);
 isolation.service=get('service','control-api-gateway');
 const marker=isolation.service.spec?.selector?.['kodex.dev/component-traffic-isolation'];
 fail(typeof marker==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(marker),
  'PERSONAL_INTEGRATIONS_API_ISOLATION_CHANGED');
 isolation.slices=parseAuthorityPrivateJSON(kube(['-n','kodex-system','get','endpointslices','-l','kubernetes.io/service-name=control-api-gateway','-o','json']));
 isolation.pods=parseAuthorityPrivateJSON(kube(['-n','kodex-system','get','pods','-l',`kodex.dev/component-traffic-isolation=${marker}`,'-o','json']));
 const proof=requireClosedAPIAdmission(deployment,resources,closed,isolation);
 if(stopped)requireClosedAPIMaintenance(deployment,resources,closed);
 return {closed,isolation,deployment,resources,proof,
  admission:{schema:personalIntegrationsAPIAdmissionSchema,profile,sourceSHA:closed.plan.sourceSHA,
   isolationPlanPath,isolationEvidencePath,closedPlanPath,closedEvidencePath,
   isolationPlanSHA256:fingerprint(isolation.plan),closedPlanSHA256:fingerprint(closed.plan),
   maintenanceID:proof.maintenanceID,deploymentUID:proof.deploymentUID,serviceUID:proof.serviceUID,
   apiTargetSHA256:proof.targetSHA256}};
}
