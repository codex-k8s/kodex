import {fingerprint} from './scoped-release.mjs';
import {requireAuthorityAPIScope} from './authority-api-scope.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {telegramAuthorityReadbackContract} from './authority-telegram-transition.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const sha=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const revision=value=>typeof value==='string'&&/^[a-f0-9]{40}$/.test(value);
const keys=['context','clusterUID','namespaceUID','installationUID','installationSHA256'];

// Проверяется типизированный результат fixed fresh PRE_ROTATION factory.
// Функция не читает policy CM вместо owner SQL и сама не выдаёт grant. API,
// migration и authority target hashes сохраняются раздельно без retargeting.
export function verifyPersonalIntegrationsAuthorityAPI(admission,transition){
 fail(transition?.profile===personalIntegrationsAuthorityTransition&&admission?.schema==='kodex.personal-integrations-staged-admission/v1'&&
  revision(admission.sourceSHA)&&admission.cohort049?.phase==='STAGED'&&admission.cohort049.sourceSHA===admission.sourceSHA&&
  sha(admission.cohort049.migrationPlanSHA256)&&sha(admission.cohort049.migrationTargetSHA256)&&
  keys.every(key=>admission.target?.[key]===(key==='context'?transition.targetProfile?.context:transition[key])),
  'PERSONAL_INTEGRATIONS_POLICY_ADMISSION_PROFILE_REQUIRED');
 requireAuthorityAPIScope(admission.apiTarget,transition.targetProfile);
 const api=transition.targetProfile.resources.find(r=>r.kind==='Deployment'&&r.name==='control-api-gateway');
 const service=transition.targetProfile.resources.find(r=>r.kind==='Service'&&r.name==='control-api-gateway');
 fail(admission.apiAdmission?.schema==='kodex.personal-integrations-api-admission/v1'&&
  ['personal-integrations','personal-integrations-backup'].includes(admission.apiAdmission.profile)&&
  admission.apiAdmission.sourceSHA===admission.sourceSHA&&admission.apiAdmission.apiTargetSHA256===fingerprint(admission.apiTarget)&&
  admission.apiAdmission.deploymentUID===api.uid&&admission.apiAdmission.serviceUID===service.uid&&
  admission.apiAdmissionSHA256===fingerprint(admission.apiAdmission)&&sha(admission.apiAdmission.closedPlanSHA256)&&
  sha(admission.apiAdmission.isolationPlanSHA256), 'PERSONAL_INTEGRATIONS_POLICY_API_SCOPE_CHANGED');
 const old=telegramAuthorityReadbackContract(),owner=admission.owner107;
 fail(owner?.schema==='kodex.personal-integrations-authority-predecessor107/v1'&&owner.policyRevision===107&&owner.sourceSHA===admission.sourceSHA&&
  owner.targetSHA256===transition.targetSHA256&&owner.ownerScope?.targetSHA256===transition.targetSHA256&&
  owner.ownerScope.sourceRevision===admission.sourceSHA&&Object.entries(old).every(([key,value])=>owner[key]===value)&&
  admission.issuerMigrationReady?.schema==='kodex.artifact-retention-issuer-migration-ready/v1'&&
  admission.issuerMigrationReady.migrationVersion===20261115000200&&admission.materialReady?.schema==='kodex.artifact-retention-issuer-ready/v1',
  'PERSONAL_INTEGRATIONS_POLICY_OWNER_PREREQUISITES_REQUIRED');
 return admission;
}
