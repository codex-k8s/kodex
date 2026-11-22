import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {readPersonalIntegrationsAPIAdmission} from './personal-integrations-api-admission.mjs';
import {verifyMigrationAPIAdmission} from './component-migration-api-admission.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {readPersonalIntegrationsCohort049} from './personal-integrations-staged-policy.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const targetKeys=['context','clusterUID','namespaceUID','installationUID','installationSHA256'];
const pathsKeys=['planPath','evidencePath','trafficPlanPath','trafficEvidencePath','migrationPlanPath','migrationEvidencePath'];
export const personalIntegrationsStoppedAdmissionSchema='kodex.personal-integrations-stopped-admission/v1';

// Фабрика объединяет два независимых owner read: физическую изоляцию API и
// committed049 с positive join всех девяти прежних writers. Она не доказывает
// policy107/110, LOGIN либо готовность нового ART. Хэши разных target не заменяются.
// io.inspect — только локальная оснастка fixed migration inspect, не CLI option.
function readCohortAdmission({sourceSHA,target,admission},kube,io,phase){
 fail(/^[a-f0-9]{40}$/.test(sourceSHA??'')&&target&&targetKeys.every(key=>typeof target[key]==='string'&&target[key])&&
  admission&&Object.keys(admission).sort().join(',')===[...pathsKeys].sort().join(',')&&
  pathsKeys.every(key=>typeof admission[key]==='string'&&admission[key]===resolve(admission[key])&&!/[\r\n\0]/.test(admission[key])),
  'PERSONAL_INTEGRATIONS_STOPPED_ADMISSION_INPUT_REJECTED');
 const migration=readAuthorityPrivateJSON(admission.migrationPlanPath);
 fail(migration.source?.revision===sourceSHA&&targetKeys.every(key=>migration.portableTarget?.[key]===target[key])&&
  migration.context===target.context&&typeof migration.kubeconfig==='string'&&migration.kubeconfig===resolve(migration.kubeconfig),
  'PERSONAL_INTEGRATIONS_STOPPED_ADMISSION_SCOPE_CHANGED');
 const input={profile:migration.profile,closedPlanPath:admission.planPath,closedEvidencePath:admission.evidencePath,
  isolationPlanPath:admission.trafficPlanPath,isolationEvidencePath:admission.trafficEvidencePath};
 const physical=()=>verifyMigrationAPIAdmission(migration,readPersonalIntegrationsAPIAdmission(input,kube,{stopped:true}));
 const before=physical();
 const {proof:cohort049}=readPersonalIntegrationsCohort049({planPath:admission.migrationPlanPath,evidencePath:admission.migrationEvidencePath},
  {source:migration.source.path,revision:sourceSHA,context:target.context,kubeconfig:migration.kubeconfig,
   ...(phase==='STAGED'?{transition:{...target,profile:personalIntegrationsAuthorityTransition}}:{target})},
  {phase,...(io.inspect?{inspect:io.inspect}:{})});
 // Блокирующий inspect не делает прежнюю Service isolation свежей. Повторное
 // чтение проверяет физическую границу после Job/SQL readback; ephemeral counters
 // и observedAt не включаются в стабильные fingerprints.
 const after=physical();
 fail(fingerprint(before.admission)===fingerprint(after.admission),'PERSONAL_INTEGRATIONS_STOPPED_API_CHANGED');
 return {schema:phase==='STOPPED'?personalIntegrationsStoppedAdmissionSchema:'kodex.personal-integrations-staged-cohort-admission/v1',sourceSHA,
  target:Object.fromEntries(targetKeys.map(key=>[key,target[key]])),
  apiTarget:after.closed.plan.target,apiAdmission:after.admission,apiAdmissionSHA256:fingerprint(after.admission),cohort049};
}

// Asset prestage допускается только до handoff.
export function readPersonalIntegrationsStoppedAdmission(input,kube,io={}){
 return readCohortAdmission(input,kube,io,'STOPPED');
}

// Этот читатель фиксирует только committed049, API isolation и полный staged
// join. Issuer resources/material/image custody добавляет отдельная фабрика;
// сам cohort result не является authority110 либо готовностью issuer.
export function readPersonalIntegrationsStagedCohortAdmission(input,kube,io={}){
 return readCohortAdmission(input,kube,io,'STAGED');
}

// Сравнивается только закрытый immutable результат свежей фабрики. Читатель
// должен вызываться заново перед effect; сохранённый JSON не заменяет fresh read.
export function verifyPersonalIntegrationsStoppedAdmission(current,pinned){
 fail(current?.schema===personalIntegrationsStoppedAdmissionSchema&&current.cohort049?.phase==='STOPPED'&&
  fingerprint(current)===fingerprint(pinned),'PERSONAL_INTEGRATIONS_STOPPED_ADMISSION_CHANGED');
 return current;
}
