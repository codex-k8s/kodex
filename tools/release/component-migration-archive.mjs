import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {parseAuthorityPrivateJSON, readAuthorityPrivateText} from './authority-catalog-transition.mjs';
import {authorityPostgresIdentity} from './catalog-authority-baseline.mjs';
import {validateMigrationAPITerminal, sameMigrationAPITerminal,
  componentMigrationAPITerminalSchema} from './component-migration-api-terminal.mjs';
import {migrationReadback} from './control-plane-migration.mjs';
import {migrationOwnerDatabase} from './migration-owner-database.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {fingerprint} from './scoped-release.mjs';
import {validWorkflowFileProvenanceComposedPostcondition,
  workflowFileProvenanceMigration} from './workflow-file-provenance-migration-contract.mjs';

const ensure = (value, code) => { if (!value) throw Error(code); };
const same = (left, right) => fingerprint(left) === fingerprint(right);
const digest = raw => createHash('sha256').update(raw).digest('hex');
const scopeFields = ['context', 'clusterUID', 'namespaceUID', 'installationUID', 'installationSHA256'];
const archiveError = 'MIGRATION_ARCHIVE_RESTORATION_REJECTED';

// Сроки проверяются на момент принятия архивной квитанции. Результат хранит
// доказательство прежнего исполнения и используется для read-only сверки.
export function verifyArchivedMigrationRestoration(migration, history, restore, events) {
  const profile = workflowFileProvenanceMigration.profile;
  const migrationSHA256 = fingerprint(migration);
  ensure(migration?.kind === 'CONTROL_PLANE_MIGRATION' && migration.version === 2 &&
    migration.profile === profile && restore?.action === 'restore' &&
    restore.schema === 'kodex.component-api-traffic-plan/v1' && restore.migrationRestoreProfile === profile &&
    restore.migrationSourceSHA === migration.source?.revision &&
    scopeFields.every(key => restore.target?.[key] && restore.target[key] === migration.portableTarget?.[key]) &&
    ['context', 'clusterUID', 'namespaceUID'].every(key => migration[key] === restore.target[key]), archiveError);
  const [header] = events;
  const times = events.map(row => Date.parse(row.at));
  ensure(header?.type === 'HEADER' && header.schema === 'kodex.component-api-traffic-evidence/v1' &&
    header.planSHA256 === fingerprint(restore) && header.maintenanceID === restore.maintenanceID &&
    times.every((value, i) => Number.isFinite(value) && (i === 0 || value >= times[i-1])), archiveError);
  const confirmations = events.filter(row => row.type === 'MIGRATION_CONFIRMED');
  const intents = events.filter(row => row.type === 'INTENT');
  const [confirmation] = confirmations, [intent] = intents;
  const observation = events.at(-1);
  ensure(confirmations.length === 1 && confirmation === events[1] && intents.length === 1 && intent === events[2] &&
    intent.step === 'apply' && events.length > 3 &&
    events.slice(3).every(row =>
      row.type === 'OBSERVED' && row.serviceUID === restore.serviceUID && row.maintenanceID === restore.maintenanceID ||
      row.type === 'UNKNOWN' && row.step === 'apply') &&
    observation.type === 'OBSERVED' && observation.state === 'RESTORED', archiveError);
  const planned = validateMigrationAPITerminal(restore.authority, restore, times[0]);
  const applied = validateMigrationAPITerminal(confirmation.authority, restore, times[1]);
  ensure(applied.schema === componentMigrationAPITerminalSchema && sameMigrationAPITerminal(planned, applied) &&
    confirmation.plannedTerminalSHA256 === planned.terminalSHA256 &&
    applied.migrationPlanSHA256 === migrationSHA256 && applied.migrationEvidenceSHA256 === fingerprint(history) &&
    applied.migrationVersion === Number(migration.migrationVersion) &&
    applied.jobName === migration.job?.metadata?.name && applied.sourceCompatibility.postconditionSQLSHA256 === migration.postconditionSQLSHA256 &&
    applied.postcondition.postgresSHA256 === fingerprint(migration.postgres) &&
    applied.postcondition.backendSHA256 === fingerprint(migration.postgresBackend) &&
    applied.migrationTargetSHA256 === fingerprint(migration.portableTarget) &&
    applied.publicationSHA256 === fingerprint(migration.imageEvidence) &&
    applied.handoffSHA256 === fingerprint(migration.componentHandoff), archiveError);
  const migrationHeader = history[0];
  const receiptTypes = ['CREATE_ACK','READBACK','TERMINAL_READBACK'];
  const created = history.filter(row => row.type === 'CREATE_INTENT');
  const createIndex = history.indexOf(created[0]);
  const receipts = history.slice(createIndex + 1).filter(row => receiptTypes.includes(row.type));
  const readback = receipts.findLast(row => row.type === 'READBACK');
  ensure(migrationHeader?.type === 'HEADER' && migrationHeader.kind === 'CONTROL_PLANE_MIGRATION' &&
    migrationHeader.version === 1 && migrationHeader.planSHA256 === migrationSHA256 &&
    created.length === 1 && created[0].jobName === applied.jobName &&
    history.slice(0, createIndex).every(row => !receiptTypes.includes(row.type)) &&
    receipts.every(row => row.jobUID === applied.jobUID) &&
    receipts.some(row => row.type === 'TERMINAL_READBACK') &&
    readback?.status === 'PASS' && readback.schemaPostcondition === 'PASS' &&
    readback.jobName === applied.jobName && readback.sourceRevision === migration.source.revision &&
    readback.migrationVersion === migration.migrationVersion, archiveError);
  return applied;
}

// Сверка текущего backend выдерживает замену Pod внутри того же StatefulSet.
// SQL привязан к историческому backend и точному UID нового Pod на всё чтение.
export function readMigrationArchiveState(migration, restore, terminal, context, kube) {
  const get = (...args) => JSON.parse(kube(['get', ...args, '-o', 'json']));
  const namespaced = (...args) => get('-n','kodex-system',...args);
  const observe = () => {
    const clusterUID = get('namespace','kube-system').metadata.uid;
    const namespaceUID = get('namespace','kodex-system').metadata.uid;
    const installation = namespaced('configmap','kodex-installation-profile');
    const scope = portableRunnerTarget(restore.target,{context,clusterUID,namespaceUID,installation});
    const service = namespaced('service','control-api-gateway');
    const deployment = namespaced('deployment','control-api-gateway');
    const job = namespaced('job',terminal.jobName);
    ensure(scope.allows(service) && scope.allows(deployment) &&
      service.metadata.uid === restore.serviceUID && same(service.spec,restore.afterSpec) &&
      same(service.metadata.annotations ?? {},restore.afterAnnotations) &&
      deployment.metadata.uid === terminal.deploymentUID && fingerprint(deployment.spec) === terminal.apiSpecSHA256 &&
      job.apiVersion === 'batch/v1' && job.kind === 'Job' &&
      job.metadata.uid === terminal.jobUID && !job.metadata.deletionTimestamp &&
      job.status?.succeeded === 1 && !job.status.active && !job.status.failed && !job.status.terminating &&
      job.status.conditions?.filter(row => row.type === 'Complete' && row.status === 'True').length === 1 &&
      migrationReadback(job,migration,'',[]).status === 'READBACK_UNCONFIRMED', 'MIGRATION_ARCHIVE_LIVE_SCOPE_CHANGED');
    return {clusterUID,namespaceUID,installationUID:installation.metadata.uid,
      installationSHA256:restore.target.installationSHA256,jobUID:job.metadata.uid,
      serviceUID:service.metadata.uid,deploymentUID:deployment.metadata.uid,
      versions:[installation,job,service,deployment].map(resource => resource.metadata.resourceVersion)};
  };
  const before = observe();
  const sql = readFileSync(new URL('./workflow-file-provenance-backup-migration.sql',import.meta.url));
  ensure(digest(sql) === migration.postconditionSQLSHA256, 'MIGRATION_ARCHIVE_SQL_CHANGED');
  const identity = () => authorityPostgresIdentity(kube);
  const pod = identity();
  ensure(pod.ownerUID === migration.postgres.ownerUID, 'MIGRATION_ARCHIVE_POSTGRES_OWNER_CHANGED');
  const proof = migrationOwnerDatabase(kube,identity).read(sql,pod,migration.postgresBackend);
  const schemaCatalogSHA256 = fingerprint(proof.schemaCatalog);
  ensure(validWorkflowFileProvenanceComposedPostcondition(proof) &&
    schemaCatalogSHA256 === terminal.postcondition.schemaCatalogSHA256, 'MIGRATION_ARCHIVE_DATABASE_CHANGED');
  ensure(same(observe(),before) && same(identity(),pod), 'MIGRATION_ARCHIVE_LIVE_SCOPE_CHANGED');
  return {schema:'kodex.component-migration-archive-readback/v1',observedAt:new Date().toISOString(),
    ...before,gooseVersion:proof.gooseVersion,schemaCatalogSHA256,
    postgresPodUID:pod.uid,postgresStatefulSetUID:pod.ownerUID,postgresBackendSHA256:fingerprint(migration.postgresBackend),
    archivedPodUID:terminal.podUID,archivedExecutionSHA256:terminal.executionSHA256,
    archivedTerminalSHA256:terminal.terminalSHA256,migrationPlanSHA256:terminal.migrationPlanSHA256,
    migrationEvidenceSHA256:terminal.migrationEvidenceSHA256};
}

// Отдельный reader допускает только смену содержимого прежних serving объектов.
// Он не объявляет текущий spec разрешённым: точный BEFORE/AFTER проверяет consumer.
export function observeMigrationContinuity(migration, restore, terminal, context, kube) {
  const uuid = value => typeof value === 'string' && /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
  const sha = (value, length=64) => typeof value === 'string' && new RegExp('^[a-f0-9]{'+length+'}$').test(value);
  const get = (...args) => parseAuthorityPrivateJSON(kube(['get',...args,'-o','json']));
  const target = migration.portableTarget;
  ensure(context === migration.context && context === restore.target.context &&
    scopeFields.every(key => target[key] === restore.target[key]), 'MIGRATION_CONTINUITY_TARGET_CHANGED');
  const namespaces = ['kube-system','kodex-system'].map(name => {
    const value = get('namespace',name);
    ensure(value.apiVersion === 'v1' && value.kind === 'Namespace' && value.metadata?.name === name &&
      uuid(value.metadata.uid) && !value.metadata.deletionTimestamp, 'MIGRATION_CONTINUITY_NAMESPACE_CHANGED');
    return value.metadata.uid;
  });
  ensure(namespaces[0] === target.clusterUID && namespaces[1] === target.namespaceUID,
    'MIGRATION_CONTINUITY_NAMESPACE_CHANGED');
  const installation = get('-n','kodex-system','configmap','kodex-installation-profile');
  const service = get('-n','kodex-system','service','control-api-gateway');
  const deployment = get('-n','kodex-system','deployment','control-api-gateway');
  const job = get('-n','kodex-system','job',terminal.jobName);
  const owned = (value,api,kind,name,expectedUID,serving=false) => {
    const meta = value?.metadata;
    ensure(value?.apiVersion === api && value.kind === kind && meta?.name === name && meta.namespace === 'kodex-system' &&
      uuid(meta.uid) && meta.uid === expectedUID && !meta.deletionTimestamp && /^\d+$/.test(meta.resourceVersion ?? '') &&
      meta.labels?.['app.kubernetes.io/part-of'] === 'kodex' && (!serving ||
        meta.labels?.['kodex.dev/profile'] === 'web-only' && meta.labels?.['kodex.dev/environment'] === 'production'),
      'MIGRATION_CONTINUITY_RESOURCE_CHANGED');
  };
  const registered = (profile,api,kind,name,expectedUID) => {
    const rows = profile.resources?.filter(value => value.apiVersion === api && value.kind === kind && value.name === name);
    ensure(rows?.length === 1 && rows[0].uid === expectedUID, 'MIGRATION_CONTINUITY_TARGET_CHANGED');
  };
  registered(restore.target,'v1','Service','control-api-gateway',restore.serviceUID);
  registered(target,'apps/v1','Deployment','control-api-gateway',terminal.deploymentUID);
  registered(restore.target,'apps/v1','Deployment','control-api-gateway',terminal.deploymentUID);
  owned(installation,'v1','ConfigMap','kodex-installation-profile',target.installationUID);
  owned(service,'v1','Service','control-api-gateway',restore.serviceUID,true);
  owned(deployment,'apps/v1','Deployment','control-api-gateway',terminal.deploymentUID,true);
  ensure(installation.metadata.labels?.['app.kubernetes.io/managed-by'] === 'kodex-install' &&
    installation.data && Object.values(installation.data).every(value => typeof value === 'string') &&
    !installation.binaryData && sha(installation.data['source.sha'],40) && sha(installation.data['release-lock.sha256']),
    'MIGRATION_CONTINUITY_INSTALLATION_CHANGED');
  const declaredSchemaHeads = parseAuthorityPrivateJSON(installation.data['schema-heads.json']);
  ensure(Object.keys(declaredSchemaHeads).sort().join(',') === 'control-plane,email-bridge,internal-rpc-authority' &&
    Object.values(declaredSchemaHeads).every(value => Number.isSafeInteger(value) && value > 0) &&
    service.spec && deployment.spec && restore.afterSpec && sha(terminal.apiSpecSHA256),
    'MIGRATION_CONTINUITY_DECLARATION_CHANGED');
  ensure(job.apiVersion === 'batch/v1' && job.kind === 'Job' && job.metadata?.namespace === 'kodex-system' &&
    job.metadata.name === terminal.jobName && job.metadata.uid === terminal.jobUID && !job.metadata.deletionTimestamp &&
    /^\d+$/.test(job.metadata.resourceVersion ?? '') && job.status?.succeeded === 1 &&
    !job.status.active && !job.status.failed && !job.status.terminating &&
    job.status.conditions?.filter(row => row.type === 'Complete' && row.status === 'True').length === 1 &&
    migrationReadback(job,migration,'',[]).status === 'READBACK_UNCONFIRMED', 'MIGRATION_CONTINUITY_JOB_CHANGED');
  return {clusterUID:namespaces[0],namespaceUID:namespaces[1],installationUID:installation.metadata.uid,
    installationSHA256:fingerprint(installation.data),installationResourceVersion:installation.metadata.resourceVersion,
    declaredSourceSHA:installation.data['source.sha'],declaredReleaseLockSHA256:installation.data['release-lock.sha256'],declaredSchemaHeads,
    serviceUID:service.metadata.uid,serviceResourceVersion:service.metadata.resourceVersion,
    serviceSpecSHA256:fingerprint(service.spec),serviceAnnotationsSHA256:fingerprint(service.metadata.annotations ?? {}),
    deploymentUID:deployment.metadata.uid,deploymentResourceVersion:deployment.metadata.resourceVersion,
    deploymentSpecSHA256:fingerprint(deployment.spec),jobUID:job.metadata.uid,
    versions:[installation,job,service,deployment].map(value => value.metadata.resourceVersion),
    beforeApiDeploymentSpecSHA256:terminal.apiSpecSHA256,beforeApiServiceSpecSHA256:fingerprint(restore.afterSpec),
    beforeApiServiceAnnotationsSHA256:fingerprint(restore.afterAnnotations ?? {})};
}

// Каталог и backend проверяются тем же неизменным SQL; старый strict reader не меняется.
export function readMigrationArchiveContinuity(migration, restore, terminal, context, kube) {
  const before = observeMigrationContinuity(migration,restore,terminal,context,kube);
  const sql = readFileSync(new URL('./workflow-file-provenance-backup-migration.sql',import.meta.url));
  ensure(digest(sql) === migration.postconditionSQLSHA256, 'MIGRATION_ARCHIVE_SQL_CHANGED');
  const identity = () => authorityPostgresIdentity(kube);
  const postgres = identity();
  ensure(postgres.ownerUID === migration.postgres.ownerUID, 'MIGRATION_ARCHIVE_POSTGRES_OWNER_CHANGED');
  const proof = migrationOwnerDatabase(kube,identity).read(sql,postgres,migration.postgresBackend);
  const schemaCatalogSHA256 = fingerprint(proof.schemaCatalog);
  ensure(validWorkflowFileProvenanceComposedPostcondition(proof) &&
    schemaCatalogSHA256 === terminal.postcondition.schemaCatalogSHA256, 'MIGRATION_ARCHIVE_DATABASE_CHANGED');
  ensure(same(observeMigrationContinuity(migration,restore,terminal,context,kube),before) && same(identity(),postgres),
    'MIGRATION_CONTINUITY_LIVE_SCOPE_CHANGED');
  return {...before,gooseVersion:proof.gooseVersion,schemaCatalogSHA256,
    postgresPodUID:postgres.uid,postgresStatefulSetUID:postgres.ownerUID};
}

export function readPinnedArchiveInput({path, sha256}, journal = false) {
  ensure(typeof path === 'string' && path.startsWith('/') && !/[\r\n\0]/.test(path) &&
    /^[a-f0-9]{64}$/.test(sha256), 'MIGRATION_ARCHIVE_INPUT_PIN_REQUIRED');
  const raw = readAuthorityPrivateText(path);
  ensure(digest(raw) === sha256 && (!journal || raw.endsWith('\n')), 'MIGRATION_ARCHIVE_INPUT_PIN_MISMATCH');
  return journal ? raw.slice(0,-1).split('\n').map(parseAuthorityPrivateJSON) : parseAuthorityPrivateJSON(raw);
}

function main(args) {
  ensure(args.length === 10, 'MIGRATION_ARCHIVE_ARGUMENTS_REJECTED');
  const [context,kubeconfig,migrationPath,migrationSHA,migrationJournal,migrationJournalSHA,
    restorePath,restoreSHA,restoreJournal,restoreJournalSHA] = args;
  ensure(/^[A-Za-z0-9_.:@/-]{1,160}$/.test(context) && kubeconfig.startsWith('/') && !/[\r\n\0]/.test(kubeconfig),
    'MIGRATION_ARCHIVE_ARGUMENTS_REJECTED');
  const read = (path, sha256, journal) => readPinnedArchiveInput({path,sha256},journal);
  const migration = read(migrationPath,migrationSHA), restore = read(restorePath,restoreSHA);
  const terminal = verifyArchivedMigrationRestoration(migration,read(migrationJournal,migrationJournalSHA,true),
    restore,read(restoreJournal,restoreJournalSHA,true));
  ensure(migration.context === context && migration.kubeconfig === kubeconfig, 'MIGRATION_ARCHIVE_TARGET_MISMATCH');
  const kube = (argv,input) => execFileSync('kubectl',['--kubeconfig',kubeconfig,'--context',context,
    '--request-timeout=20s',...argv],{input,encoding:'utf8',timeout:24000,maxBuffer:4<<20,stdio:['pipe','pipe','pipe']});
  const state = readMigrationArchiveState(migration,restore,terminal,context,kube);
  console.log(JSON.stringify({...state,archiveInputsSHA256:{
    migrationPlan:migrationSHA,migrationJournal:migrationJournalSHA,restorePlan:restoreSHA,restoreJournal:restoreJournalSHA,
  }}));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try { main(process.argv.slice(2)); }
  catch (error) {
    console.error(/^[A-Z][A-Z_0-9]+$/.test(error.message) ? error.message : 'MIGRATION_ARCHIVE_READBACK_REJECTED');
    process.exitCode = 1;
  }
}
