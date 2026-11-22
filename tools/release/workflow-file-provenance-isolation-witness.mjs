import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {confirmedAPIMaintenance,confirmedAPIOpenMaintenance} from './component-api-maintenance.mjs';
import {verifyPersonalIntegrationsMigrationWitness,personalIntegrationsReadersTerminalReceipt} from './personal-integrations-migration-witness.mjs';
import {artifactGrantCatalogReceipt,artifactGrantSchemaVersion} from './artifact-retention-grant-catalog.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {validateCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';
import {readStoppedBackupMaintenance,readCanonicalBackupOwner} from './backup-controller-maintenance-cli.mjs';
import {workflowFileProvenanceMigration} from './workflow-file-provenance-migration-contract.mjs';

const rejected='WORKFLOW_FILE_PROVENANCE_ISOLATION_WITNESS_REJECTED';
const ensure=value=>{if(!value)throw Error(rejected);};
const same=(left,right)=>fingerprint(left)===fingerprint(right);
const privatePath=value=>typeof value==='string'&&value===resolve(value)&&value.length<=4096&&!/[\r\n\0]/.test(value);
const readEvents=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
const targetScope=target=>Object.fromEntries(Object.entries(target??{}).filter(([key])=>key!=='resources'));

// Исторический OPEN принадлежит прежнему049, новый CLOSE только завершает
// его serving workload. Caller отдельно доказывает непрерывную физическую
// изоляцию Service и удерживает новый backup journal lock до изменения.
export async function readWorkflowFileIsolationWitness({rotation,authority,previous,previousPlanPath,previousEvidencePath,closed,
 openPlanPath,openEvidencePath,earlyRecovery,kube}){
 const paths=rotation?.personalIntegrationsAdmission,api=rotation?.maintenance?.apiAdmission;
 ensure(rotation?.action==='rotate'&&rotation.policyTransition?.profile===personalIntegrationsAuthorityTransition&&
  paths&&Object.keys(paths).sort().join(',')==='evidencePath,migrationEvidencePath,migrationPlanPath,planPath,trafficEvidencePath,trafficPlanPath'&&
  api?.schema==='kodex.personal-integrations-api-admission/v1'&&api.profile==='personal-integrations-backup'&&
  [previousPlanPath,previousEvidencePath,openPlanPath,openEvidencePath,...Object.values(paths)].every(privatePath)&&
  previousPlanPath===api.isolationPlanPath&&previousEvidencePath===api.isolationEvidencePath&&
  paths.trafficPlanPath===previousPlanPath&&paths.trafficEvidencePath===previousEvidencePath&&
  paths.planPath===api.closedPlanPath&&paths.evidencePath===api.closedEvidencePath&&
  previous?.plan?.action==='isolate'&&!previous.plan.migrationRestore&&!previous.plan.migrationRestoreProfile&&!previous.plan.authorityTransition&&
  api.sourceSHA===rotation.revision&&previous.plan.sourceSHA===rotation.revision&&
  api.isolationPlanSHA256===fingerprint(previous.plan)&&rotation.maintenance.apiAdmissionSHA256===fingerprint(api)&&
  same(rotation.maintenance.apiTarget,previous.plan.target));
 const priorClosed={plan:readAuthorityPrivateJSON(paths.planPath),events:readEvents(paths.evidencePath)};
 ensure(priorClosed.plan.sourceSHA===rotation.revision&&api.closedPlanSHA256===fingerprint(priorClosed.plan)&&
  priorClosed.plan.maintenanceID===api.maintenanceID&&priorClosed.plan.deploymentUID===api.deploymentUID&&
  previous.plan.serviceUID===api.serviceUID&&previous.plan.closePlanSHA256===api.closedPlanSHA256&&
  previous.plan.maintenanceID===api.maintenanceID&&same(previous.plan.target,priorClosed.plan.target)&&
  same(previous.plan,readAuthorityPrivateJSON(previousPlanPath))&&same(previous.events,readEvents(previousEvidencePath)));
 const migration={plan:readAuthorityPrivateJSON(paths.migrationPlanPath),events:readEvents(paths.migrationEvidencePath)};
 verifyPersonalIntegrationsMigrationWitness(rotation,migration);
 ensure(migration.plan.profile==='personal-integrations-backup'&&same(migration.plan.apiMaintenance,api));
 const {binding,terminal}=personalIntegrationsReadersTerminalReceipt(migration.plan,migration.events);
 const artifact=artifactGrantCatalogReceipt(migration.plan,migration.events);
 ensure(artifact&&artifact.schemaVersion===artifactGrantSchemaVersion);
 const opened={plan:readAuthorityPrivateJSON(openPlanPath),events:readEvents(openEvidencePath)};
 const open=confirmedAPIOpenMaintenance(priorClosed,opened,previous);
 confirmedAPIMaintenance(closed);
 ensure(opened.plan.sourceSHA===rotation.revision&&open.openPlanSHA256===binding.openPlanSHA256&&
  open.serviceUID===api.serviceUID&&closed.plan.deploymentUID===api.deploymentUID&&
  same(targetScope(closed.plan.target),targetScope(previous.plan.target))&&
  [['apps/v1','Deployment',api.deploymentUID],['v1','Service',api.serviceUID]].every(([apiVersion,kind,uid])=>
   [closed.plan.target,previous.plan.target].every(target=>target.resources?.some(row=>row.apiVersion===apiVersion&&row.kind===kind&&
    row.name==='control-api-gateway'&&row.uid===uid)))&&same(closed.plan.beforeSpec,opened.plan.afterSpec)&&
  same(closed.plan.beforeAnnotations,opened.plan.afterAnnotations));
 validateCatalogAuthorityBaseline(authority,Date.now(),110);
 ensure(authority.rotationPlanSHA256===fingerprint(rotation)&&authority.operationID===rotation.ownerOperationID&&
  authority.operationID===binding.operationID&&authority.snapshotRevision===terminal.snapshotRevision&&
  authority.snapshotSHA256===terminal.snapshotSHA256&&authority.policySHA256===binding.policySHA256&&
  authority.registryRevision===binding.registryRevision&&authority.registrySHA256===binding.registrySHA256&&
  binding.planSHA256===fingerprint(rotation)&&
  ['context','clusterUID','namespaceUID','installationUID','installationSHA256'].every(key=>authority.scope?.[key]===previous.plan.target[key]));
 const early=earlyRecovery?.backupMaintenance;
 ensure(early&&privatePath(early.planPath)&&privatePath(early.evidencePath));
 const stop={plan:readAuthorityPrivateJSON(early.planPath),events:readEvents(early.evidencePath)};
 ensure(stop.plan.action==='stop'&&stop.plan.migrationProfile===workflowFileProvenanceMigration.profile&&
  stop.plan.planPath===early.planPath&&stop.plan.evidencePath===early.evidencePath&&
  fingerprint(stop.plan)===early.planSHA256&&stop.plan.sourceSHA===closed.plan.sourceSHA&&
  same(targetScope(stop.plan.target),targetScope(closed.plan.target))&&same(stop.plan.postgres,authority.postgres)&&
  stop.plan.postgresBackend?.systemIdentifier===authority.postgresBackend.systemIdentifier&&
  !stop.events.some(row=>['BACKUP_MIGRATION_BOUND','BACKUP_RESUME_INTENT'].includes(row.type)));
 readStoppedBackupMaintenance(kube,stop);
 const owner=await readCanonicalBackupOwner({kube,plan:stop.plan,
  readSQL:name=>readFileSync(new URL(name,import.meta.url))});
 ensure(owner.canonicalBackup===true&&owner.schemaVersion===artifactGrantSchemaVersion);
 // SQL read не превращает прежний join или входные private journals в свежий
 // proof: повторно подтверждаем те же pins после блокирующего owner read.
 ensure(same(stop.plan,readAuthorityPrivateJSON(early.planPath))&&same(stop.events,readEvents(early.evidencePath))&&
  same(migration.plan,readAuthorityPrivateJSON(paths.migrationPlanPath))&&same(migration.events,readEvents(paths.migrationEvidencePath))&&
  same(priorClosed.plan,readAuthorityPrivateJSON(paths.planPath))&&same(priorClosed.events,readEvents(paths.evidencePath))&&
  same(previous.plan,readAuthorityPrivateJSON(previousPlanPath))&&same(previous.events,readEvents(previousEvidencePath))&&
  same(opened.plan,readAuthorityPrivateJSON(openPlanPath))&&same(opened.events,readEvents(openEvidencePath)));
 readStoppedBackupMaintenance(kube,stop);
 validateCatalogAuthorityBaseline(authority,Date.now(),110);
 return Object.freeze({migrationPlanSHA256:fingerprint(migration.plan),openPlanSHA256:open.openPlanSHA256,artifactJobUID:artifact.jobUID});
}
