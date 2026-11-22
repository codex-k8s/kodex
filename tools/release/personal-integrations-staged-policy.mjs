import {resolvePersonalIntegrationsExecutorSource} from './personal-integrations-executor-source.mjs';
import {artifactGrantCatalogReceipt,artifactGrantCatalogRequired} from './artifact-retention-grant-catalog.mjs';
import {inspectSource} from './application-source.mjs';
import {personalMigrationAttempt} from './personal-integrations-migration-recovery.mjs';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {existsSync} from 'node:fs';
import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {componentMigrationNames} from './portable-component-migration.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const scopeKeys=['clusterUID','namespaceUID','installationUID','installationSHA256'];
const events=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
export const personalIntegrationsStagedPolicyProtocol='STAGED_POLICY_110_V1';

// Полный owner inspect доказывает049 и остановку всех девяти прежних writers,
// включая уже установленный ART. Hash authority target остаётся отдельным;
// здесь не создаётся подмена admission proof и не возникает READY/DELIVER.
export function personalIntegrationsCohort049Proof(migration,rows,inspection,{source,revision,context,kubeconfig,transition,target},phase='STAGED'){
 fail(['STOPPED','STAGED'].includes(phase),'PERSONAL_INTEGRATIONS_STAGE_PURPOSE_REJECTED');
 const common=transition??target,expectedStage=phase==='STAGED'?'stagedSpec':'stoppedSpec';
 const selected=personalMigrationAttempt(migration,rows);
 const parts=migration.componentHandoff?.components;
 fail(migration.version===2&&['personal-integrations','personal-integrations-backup'].includes(migration.profile)&&
  migration.migrationVersion==='20261114170000'&&Array.isArray(parts)&&
  same(parts.map(part=>part.name),componentMigrationNames(migration.profile))&&parts.some(part=>part.name==='artifact-retention')&&
  migration.source.path===source&&migration.source.revision===revision&&migration.context===context&&migration.kubeconfig===kubeconfig&&
  common&&(!transition||transition.profile===personalIntegrationsAuthorityTransition)&&
  (phase!=='STAGED'||transition?.profile===personalIntegrationsAuthorityTransition)&&scopeKeys.every(key=>migration.portableTarget[key]===common[key])&&
  parts.every(part=>part.stagedSpec.replicas===0),'PERSONAL_INTEGRATIONS_STAGE_SCOPE_REJECTED');
 fail(rows[0]?.type==='HEADER'&&rows[0].kind==='CONTROL_PLANE_MIGRATION'&&rows[0].planSHA256===fingerprint(migration)&&
  rows.some(row=>row.type==='STOP_ACK')&&(phase!=='STOPPED'||!rows.some(row=>['PAIR_HANDOFF_INTENT','PAIR_HANDOFF_ACK'].includes(row.type)))&&(phase==='STOPPED'||rows.some(row=>row.type==='PAIR_HANDOFF_ACK'&&row.publicationSHA256===migration.componentHandoff.publicationSHA256))&&
  !rows.some(row=>['RESUME_INTENT','PAIR_RESUME_INTENT','PAIR_ACTIVATE_INTENT'].includes(row.type))&&
  rows.some(row=>row.type===selected.readbackType&&row.status==='PASS'&&row.schemaPostcondition==='PASS'&&
   row.jobUID===inspection.jobUID&&(selected.attempt===1||selected.jobUID===inspection.jobUID&&
    row.jobName===selected.job.metadata.name&&inspection.jobName===selected.job.metadata.name&&inspection.attempt===2)&&row.sourceRevision===revision&&row.migrationVersion==='20261114170000'),
  'PERSONAL_INTEGRATIONS_STAGE_JOURNAL_REJECTED');
 fail(inspection.status==='PASS'&&inspection.migrationVersion==='20261114170000'&&inspection.sourceRevision===revision&&
  inspection.schemaPostcondition==='PASS'&&inspection.cpStage===phase&&inspection.podsGone===true&&
  inspection.cpReady===false&&inspection.pairReady===false&&inspection.componentPair?.length===parts.length&&
  parts.every(part=>inspection.componentPair.filter(value=>value.name===part.name&&value.stage===expectedStage&&value.joined===true&&value.ready===false).length===1),
  'PERSONAL_INTEGRATIONS_STAGE_JOIN_REQUIRED');
 const art=phase==='STAGED'&&artifactGrantCatalogRequired(migration)?artifactGrantCatalogReceipt(migration,rows):undefined;
 if(phase==='STAGED'&&artifactGrantCatalogRequired(migration))fail(art&&inspection.servingSchema?.version===art.schemaVersion&&
  inspection.servingSchema.schemaProofSHA256===art.sqlSHA256&&inspection.servingSchema.jobUID===art.jobUID,
  'PERSONAL_ARTIFACT_GRANT_CATALOG_RECEIPT_REQUIRED');
 return {schema:'kodex.personal-integrations-cohort049-proof/v1',phase,profile:migration.profile,
  protocol:phase==='STAGED'?personalIntegrationsStagedPolicyProtocol:'STOPPED_COHORT_049_V1',migrationPlanSHA256:fingerprint(migration),migrationID:migration.id,
  sourceSHA:revision,migrationTargetSHA256:fingerprint(migration.portableTarget),
  components:parts.map(part=>({name:part.name,uid:part.uid,beforeSpecSHA256:fingerprint(part.beforeSpec),
   stoppedSpecSHA256:fingerprint(part.stoppedSpec),stagedSpecSHA256:fingerprint(part.stagedSpec),observedSpecSHA256:fingerprint(part[expectedStage])})),
  publicationSHA256:migration.componentHandoff.publicationSHA256,postconditionSQLSHA256:migration.postconditionSQLSHA256,jobUID:inspection.jobUID,
  ...(art?{artifactGrantCatalog:{jobUID:art.jobUID,schemaVersion:art.schemaVersion,planSHA256:art.planSHA256,sqlSHA256:art.sqlSHA256}}:{})};
}

// Выполняется только существующий inspect с fixed argv. Он заново проверяет
// exact Job/Pod writer join, PostgreSQL049 и текущие Deployment descendants.
// Saved rows либо caller inspection boolean не являются production input.
export function readPersonalIntegrationsCohort049(paths,scope,{phase='STAGED',inspect}={}){
 fail(paths?.planPath&&paths.evidencePath,'PERSONAL_INTEGRATIONS_STAGE_INPUTS_REQUIRED');
 fail(!existsSync(resolve(paths.evidencePath)+'.lock'),'PERSONAL_INTEGRATIONS_STAGE_WRITER_ACTIVE');
 const migration=readAuthorityPrivateJSON(paths.planPath);
 const inspection=(inspect??(()=>{
  const executorSource=fileURLToPath(new URL('../../',import.meta.url)).replace(/\/$/,'');
  const executorRevision=inspectSource(executorSource).revision;
  const executor=executorRevision===migration.source.revision?undefined:resolvePersonalIntegrationsExecutorSource(executorSource,{source:migration.source.path,revision:migration.source.revision});
  return parseAuthorityPrivateJSON(execFileSync(process.execPath,
  [fileURLToPath(new URL('./control-plane-migration.mjs',import.meta.url)),'inspect','--context',scope.context,'--kubeconfig',scope.kubeconfig,
   '--profile',migration.profile,'--plan',paths.planPath,'--evidence',paths.evidencePath,'--evidence-read-only','true',
   ...(executor?['--personal-executor-sha',executor.executorRevision]:[])],
  {encoding:'utf8',timeout:180000,maxBuffer:8<<20,stdio:['ignore','pipe','pipe']}));}))();
 return {...paths,proof:personalIntegrationsCohort049Proof(migration,events(paths.evidencePath),inspection,scope,phase)};
}

// Rotation требует именно staged, в отличие от asset prestage на stopped.
export function personalIntegrationsStagedPolicyProof(migration,rows,inspection,scope){
 return personalIntegrationsCohort049Proof(migration,rows,inspection,scope,'STAGED');
}
export function readPersonalIntegrationsStagedPolicyMigration(paths,scope,io={}){
 return readPersonalIntegrationsCohort049(paths,scope,{...io,phase:'STAGED'});
}
