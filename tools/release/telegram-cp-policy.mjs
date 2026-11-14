import {readTelegramPortRepair,telegramPortRepairCodes} from './telegram-port-repair.mjs';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON,validateCatalogProjectionEvents,validateAuthorityPolicy} from './authority-catalog-transition.mjs';
import {telegramPolicyTransition} from './authority-telegram-transition.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const events=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
export const telegramStagedPolicyProtocol='STAGED_POLICY_107_V1';
const scopeKeys=['clusterUID','namespaceUID','installationUID','installationSHA256'];

// Staged не означает readiness: все пять writers положительно joined, новый CP
// остаётся replicas0 до собственной immutable projection107 и owner DELIVER.
export function telegramStagedPolicyProof(migration,rows,inspection,{source,revision,context,kubeconfig,transition}){
 const cp=migration.componentHandoff?.components.find(c=>c.name==='control-plane');
 fail(migration.version===2&&['telegram-notifications','telegram-notifications-backup'].includes(migration.profile)&&
  migration.migrationVersion==='20261114140000'&&migration.componentHandoff.introducedReadiness===telegramStagedPolicyProtocol&&
  migration.source.path===source&&migration.source.revision===revision&&migration.context===context&&migration.kubeconfig===kubeconfig&&
  transition.profile===telegramPolicyTransition&&scopeKeys.every(key=>migration.portableTarget[key]===transition[key])&&
  cp?.stagedSpec.replicas===0,'TELEGRAM_CP_STAGE_SCOPE_REJECTED');
 fail(rows[0]?.type==='HEADER'&&rows[0].kind==='CONTROL_PLANE_MIGRATION'&&rows[0].planSHA256===fingerprint(migration)&&
  rows.some(e=>e.type==='STOP_ACK')&&rows.some(e=>e.type==='PAIR_HANDOFF_ACK'&&e.publicationSHA256===migration.componentHandoff.publicationSHA256)&&
  !rows.some(e=>e.type==='RESUME_INTENT')&&rows.some(e=>e.type==='TERMINAL_READBACK'&&e.jobUID===inspection.jobUID),
  'TELEGRAM_CP_STAGE_JOURNAL_REJECTED');
 fail(inspection.status==='PASS'&&inspection.schemaPostcondition==='PASS'&&inspection.cpStage==='STAGED'&&inspection.podsGone===true&&
  inspection.cpReady===false&&inspection.pairReady===false&&inspection.componentPair?.length===5&&
  migration.componentHandoff.components.every(part=>inspection.componentPair.filter(value=>value.name===part.name&&value.stage==='stagedSpec'&&value.joined===true&&value.ready===false).length===1),
  'TELEGRAM_CP_STAGE_JOIN_REQUIRED');
 return {protocol:telegramStagedPolicyProtocol,migrationPlanSHA256:fingerprint(migration),migrationID:migration.id,
  sourceSHA:revision,introducedReceiverSHA256:fingerprint(migration.imageEvidence.introducedReceiver),deploymentUID:cp.uid,stagedSpecSHA256:fingerprint(cp.stagedSpec),
  publicationSHA256:migration.componentHandoff.publicationSHA256,postconditionSQLSHA256:migration.postconditionSQLSHA256,jobUID:inspection.jobUID,...(inspection.telegramPortRepairSHA256?{telegramPortRepairSHA256:inspection.telegramPortRepairSHA256}:{})};
}

// Public owner inspect исполняет полный Job/Pod и PostgreSQL postcondition;
// приватный receipt сам по себе не является текущим допуском к pointer CAS.
export function readTelegramStagedPolicyMigration(paths,scope,{inspect}={}){
 fail(paths?.planPath&&paths.evidencePath,'TELEGRAM_CP_STAGE_INPUTS_REQUIRED');
 const migration=readAuthorityPrivateJSON(paths.planPath);
 const inspection=(inspect??(()=>parseAuthorityPrivateJSON(execFileSync(process.execPath,
  [fileURLToPath(new URL('./control-plane-migration.mjs',import.meta.url)),'inspect','--context',scope.context,'--kubeconfig',scope.kubeconfig,
   '--profile',migration.profile,'--plan',paths.planPath,'--evidence',paths.evidencePath,...(paths.repairPlanPath?['--telegram-port-repair-plan',paths.repairPlanPath,...(paths.repairExecutorRevision?['--telegram-port-repair-executor-sha',paths.repairExecutorRevision]:[])]:[])],
  {encoding:'utf8',timeout:180000,maxBuffer:8<<20,stdio:['ignore','pipe','pipe']}))))();
 const repair=paths.repairPlanPath?readTelegramPortRepair(paths.repairPlanPath,{migration,executorRevision:paths.repairExecutorRevision}):undefined;
 fail(inspection.telegramPortRepairSHA256===(repair?.sha256),telegramPortRepairCodes.pinRequired);
 return {...paths,proof:telegramStagedPolicyProof(migration,events(paths.evidencePath),inspection,scope)};
}

export function telegramProjectedCPSpec(before,projection){
 const after=structuredClone(before),app=after.template.spec.containers.find(c=>c.name==='control-plane');
 fail(app&&projection.desired,'TELEGRAM_CP_PROJECTION_SPEC_REJECTED');
 after.template.spec.volumes=structuredClone(projection.desired.volumes);app.env=structuredClone(projection.desired.env);
 app.volumeMounts=structuredClone(projection.desired.volumeMounts);return after;
}

// Только ACK собственного projection journal и текущий API object разрешают
// новый CP spec. Реплики меняет исключительно исходный migration lifecycle.
export function readTelegramCPProjection({migration,planPath,evidencePath,repairPlanPath,repairExecutorRevision},kube){
 fail(planPath&&evidencePath,'TELEGRAM_CP_PROJECTION_REQUIRED');
 const projection=readAuthorityPrivateJSON(planPath),rows=events(evidencePath),cp=migration.componentHandoff.components.find(c=>c.name==='control-plane');
 const proof=projection.telegramStage?.proof;
 const repair=repairPlanPath?readTelegramPortRepair(repairPlanPath,{migration,executorRevision:repairExecutorRevision}):undefined;
 fail(proof?.telegramPortRepairSHA256===repair?.sha256&&(!repair||projection.sourceCompatibility?.telegramPortRepair?.planSHA256===repair.sha256),telegramPortRepairCodes.pinRequired);
 fail(migration.componentHandoff.introducedReadiness===telegramStagedPolicyProtocol&&projection.version===2&&projection.kind==='CP_CATALOG_POLICY_PROJECTION'&&
  projection.policyTransition?.profile===telegramPolicyTransition&&projection.source===migration.source.path&&projection.revision===migration.source.revision&&
  projection.context===migration.context&&projection.kubeconfig===migration.kubeconfig&&scopeKeys.every(key=>migration.portableTarget[key]===projection.policyTransition[key])&&
  proof?.protocol===telegramStagedPolicyProtocol&&proof.migrationPlanSHA256===fingerprint(migration)&&proof.migrationID===migration.id&&
  proof.sourceSHA===migration.source.revision&&proof.introducedReceiverSHA256===fingerprint(migration.imageEvidence.introducedReceiver)&&proof.deploymentUID===cp.uid&&proof.stagedSpecSHA256===fingerprint(cp.stagedSpec)&&
  proof.publicationSHA256===migration.componentHandoff.publicationSHA256&&proof.postconditionSQLSHA256===migration.postconditionSQLSHA256&&
  projection.deploymentUID===cp.uid&&projection.beforeSpecSHA256===fingerprint(cp.stagedSpec),'TELEGRAM_CP_PROJECTION_SCOPE_CHANGED');
 const acknowledged=validateCatalogProjectionEvents(rows,projection);
 const get=(kind,name)=>parseAuthorityPrivateJSON(kube(['-n','kodex-system','get',kind,name,'-o','json']));
 const cm=get('configmap',projection.projection.metadata.name),registry=get('configmap','internal-rpc-authority-publisher-target-registry');
 fail(acknowledged.get('CP_POINTER_CAS')===cp.uid&&acknowledged.get('CREATE_PROJECTION')===cm.metadata?.uid&&
  cm.apiVersion==='v1'&&cm.kind==='ConfigMap'&&cm.immutable===true&&!cm.metadata.deletionTimestamp&&!cm.binaryData&&
  same(cm.data,projection.projection.data)&&Object.entries(projection.projection.metadata.labels).every(([key,value])=>cm.metadata.labels?.[key]===value)&&
  registry.metadata.uid===projection.registry.uid&&registry.data?.['authority-policy.json']===cm.data['policy.json'],
  'TELEGRAM_CP_PROJECTION_READBACK_CHANGED');
 const policy=validateAuthorityPolicy(cm.data['policy.json'],'desired',projection.policyTransition);
 fail(policy.revision===107&&policy.sha256===projection.renderedPolicySHA256&&fingerprint(registry.data)===projection.registry.dataSHA256,
  'TELEGRAM_CP_PROJECTION_POLICY_CHANGED');
 const volumeName='control-plane-environment-policy',mountPath='/var/run/config/kodex/control-plane/environment-policy',policyEnv='CONTROL_PLANE_AUTHORITY_POLICY_FILE';
 const app=cp.stagedSpec.template.spec.containers.find(c=>c.name==='control-plane');
 const desired={volumes:[...(cp.stagedSpec.template.spec.volumes??[]).filter(value=>value.name!==volumeName),
   {name:volumeName,configMap:{name:cm.metadata.name,defaultMode:292}}],
  env:[...(app.env??[]).filter(value=>value.name!==policyEnv),{name:policyEnv,value:`${mountPath}/policy.json`}],
  volumeMounts:[...(app.volumeMounts??[]).filter(value=>value.name!==volumeName),{name:volumeName,mountPath,readOnly:true}]};
 fail(same(desired,projection.desired)&&cm.metadata.name===`control-plane-policy-projection-${policy.sha256.slice(0,16)}`,
  'TELEGRAM_CP_PROJECTION_SPEC_REJECTED');
 const stagedSpec=telegramProjectedCPSpec(cp.stagedSpec,projection),resumedSpec=telegramProjectedCPSpec(cp.resumedSpec,projection);
 fail(fingerprint(stagedSpec)===projection.afterSpecSHA256&&stagedSpec.replicas===0&&resumedSpec.replicas===3,
  'TELEGRAM_CP_PROJECTION_SPEC_REJECTED');
 return {stagedSpec,resumedSpec,projectionPlanSHA256:fingerprint(projection),migrationPlanSHA256:fingerprint(migration)};
}
