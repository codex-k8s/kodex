import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {readPinnedArchiveInput,readMigrationArchiveState,readMigrationArchiveContinuity,observeMigrationContinuity,
  verifyArchivedMigrationRestoration} from './component-migration-archive.mjs';
import {authorityPostgresIdentity,catalogAuthorityBaselineIdentity,readCatalogAuthorityBaseline,sameCatalogAuthorityBaseline,
  validateCatalogAuthorityBaseline,catalogAuthorityBaselineSQL} from './catalog-authority-baseline.mjs';
import {readIssuerMigrationDurableStorage} from './artifact-retention-issuer-historical-readiness.mjs';
import {authorityOwnerDatabase,migrationOwnerDatabase} from './migration-owner-database.mjs';
import {fingerprint} from './scoped-release.mjs';
import {inspectSource} from './application-source.mjs';
import {validateAuthorityPolicy} from './authority-catalog-transition.mjs';
import {deriveRegistryRotationIdentity,validateRetiredCatalogOwnerState} from './authority-rotation-transition.mjs';
import {personalIntegrationsAuthorityTransition,personalIntegrationsAuthorityReadbackContract,
  validatePersonalIntegrationsAuthorityRegistry} from './authority-personal-integrations-source.mjs';
import {validatePersonalIntegrationsRotationJournal} from './personal-integrations-rotation-journal.mjs';
import {readComponentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';

const ensure = (ok, code) => { if (!ok) throw Error(code); };
const same = (left,right) => fingerprint(left) === fingerprint(right);
const scopeKeys = ['context','clusterUID','namespaceUID','installationUID','installationSHA256'];
const inputKinds = ['migrationPlan','migrationJournal','restorePlan','restoreJournal','rotationPlan','rotationJournal','storageContext'];
const heads = {'control-plane':20261121000200,'internal-rpc-authority':20261115000200,'email-bridge':20260905000200};
const readHeadSQL = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout='5s'; SELECT pg_catalog.json_build_object('head',max(version_id)) FROM public.goose_db_version WHERE is_applied; ROLLBACK;\n";
const readArtifactTrustSQL = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout='5s'; SELECT pg_catalog.json_build_object('enabled',count(*)=1) FROM control_plane.trusted_workload_generations WHERE workload_id='artifact-retention' AND credential_generation=1 AND enabled; ROLLBACK;\n";

// Read-only evidence: архивные времена остаются в terminal, текущие SQL и
// storage наблюдаются заново. Вызов не открывает maintenance или apply.
function readCustody(inputs, context, kubeconfig, kube, continuity=false) {
  const {migrationPlan:migration,migrationJournal:history,restorePlan:restore,restoreJournal:events,
    rotationPlan:rotation,rotationJournal:rotationHistory,storageContext:prior} = inputs;
  ensure(migration.context === context && migration.kubeconfig === kubeconfig &&
    rotation.context === context && rotation.kubeconfig === kubeconfig, 'MIGRATION_ADOPTION_TARGET_CHANGED');
  const terminal = verifyArchivedMigrationRestoration(migration, history, restore, events);
  const archive = continuity ? readMigrationArchiveContinuity(migration,restore,terminal,context,kube) :
    readMigrationArchiveState(migration, restore, terminal, context, kube);
  const {contextSHA256,...storageBody} = prior;
  ensure(prior.schema === 'kodex.artifact-retention-issuer-historical-context/v1' &&
    prior.purpose === 'HISTORICAL_ROLE_ONLY' && contextSHA256 === fingerprint(storageBody) &&
    scopeKeys.every(key => prior.target?.[key] === migration.portableTarget[key]), 'MIGRATION_ADOPTION_STORAGE_CONTEXT_CHANGED');
  const incident = readPinnedArchiveInput({path:prior.incident.planPath,sha256:prior.incident.planSHA256});
  const repaired = readPinnedArchiveInput({path:prior.incident.receiptPath,sha256:prior.incident.receiptSHA256});
  ensure(repaired.status === 'RECOVERED' && repaired.existingPVsUnchanged === incident.pvs.length &&
    [repaired,incident.scope].every(value => value.clusterUID === archive.clusterUID && value.namespaceUID === archive.namespaceUID) &&
    incident.scope.context === context, 'MIGRATION_ADOPTION_STORAGE_CONTEXT_CHANGED');
  const identity = () => authorityPostgresIdentity(kube);
  const postgres = identity();
  ensure(postgres.uid === archive.postgresPodUID && postgres.ownerUID === archive.postgresStatefulSetUID,
    'MIGRATION_ADOPTION_POSTGRES_CHANGED');
  const actualStorage = readIssuerMigrationDurableStorage({postgres},incident,kube);
  ensure(same(actualStorage,prior.storage), 'MIGRATION_ADOPTION_STORAGE_CHANGED');
  const artifact = migrationOwnerDatabase(kube,identity).read(readArtifactTrustSQL,postgres,migration.postgresBackend);
  ensure(artifact?.enabled === true, 'MIGRATION_ADOPTION_ARTIFACT_TRUST_MISSING');

  const get = (kind,name) => JSON.parse(kube(['-n','kodex-system','get',kind,name,'-o','json']));
  const emailIdentity = () => {
    const pod = get('pod','email-bridge-postgresql-0'), set = get('statefulset','email-bridge-postgresql');
    const [owner] = pod.metadata.ownerReferences.filter(value => value.controller === true);
    ensure(pod.kind === 'Pod' && pod.status.phase === 'Running' && !pod.metadata.deletionTimestamp &&
      pod.spec.containers.length === 1 && pod.spec.containers[0].name === 'postgresql' &&
      set.kind === 'StatefulSet' && !set.metadata.deletionTimestamp && set.metadata.uid &&
      pod.metadata.uid && owner?.kind === 'StatefulSet' && owner.uid === set.metadata.uid,
    'MIGRATION_ADOPTION_EMAIL_DATABASE_CHANGED');
    return {podUID:pod.metadata.uid,statefulSetUID:set.metadata.uid};
  };
  const emailBefore = emailIdentity();
  const email = JSON.parse(kube(['-n','kodex-system','exec','-i','email-bridge-postgresql-0','-c','postgresql','--',
    'env','-i','PATH=/usr/local/bin:/usr/bin:/bin','psql','-XqAt','--host=/var/run/postgresql','--port=5432',
    '--username=postgres','--dbname=email_bridge','--no-password','-v','ON_ERROR_STOP=1'],readHeadSQL));
  const authorityHead = authorityOwnerDatabase(kube,identity).read(readHeadSQL,postgres,terminal.authority.postgresBackend);
  const schemaHeads = {'control-plane':archive.gooseVersion,'internal-rpc-authority':authorityHead.head,'email-bridge':email.head};
  ensure(same(schemaHeads,heads) && same(emailIdentity(),emailBefore), 'MIGRATION_ADOPTION_SCHEMA_HEAD_DRIFT');

  const historicalAuthority = terminal.authority;
  const authority = continuity ? readMigrationContinuityAuthority(rotation,rotationHistory,historicalAuthority,kube) :
    readCatalogAuthorityBaseline({plan:rotation,events:rotationHistory,policyRevision:110},kube);
  ensure(authority.postgres.ownerUID === historicalAuthority.postgres.ownerUID && same(authority.postgres,postgres) &&
    sameCatalogAuthorityBaseline({...historicalAuthority,postgres},authority), 'MIGRATION_ADOPTION_AUTHORITY_CHANGED');
  const installation = get('configmap','kodex-installation-profile');
  const declaredSchemaHeads = JSON.parse(installation.data['schema-heads.json']);
  ensure(installation.metadata.uid === archive.installationUID && fingerprint(installation.data) === archive.installationSHA256 &&
    (continuity || installation.data['source.sha'] === migration.imageEvidence.baseSourceSHA &&
    installation.data['release-lock.sha256'] === migration.imageEvidence.baseReleaseLockSHA256 &&
    same(declaredSchemaHeads,migration.imageEvidence.baseSchemaHeads)) &&
    same(readIssuerMigrationDurableStorage({postgres},incident,kube),actualStorage) && same(identity(),postgres),
  'MIGRATION_ADOPTION_CUSTODY_CHANGED');
  validateCatalogAuthorityBaseline(authority,Date.now(),110);
  const serving = continuity ? observeMigrationContinuity(migration,restore,terminal,context,kube) : null;
  if (continuity) ensure(same(serving,Object.fromEntries(Object.keys(serving).map(key=>[key,archive[key]]))),
    'MIGRATION_CONTINUITY_LIVE_SCOPE_CHANGED');
  if (continuity) validateCatalogAuthorityBaseline(authority,Date.now(),110);
  const observation = continuity ? Object.fromEntries([
    'installationResourceVersion','serviceUID','serviceResourceVersion','serviceSpecSHA256','serviceAnnotationsSHA256',
    'deploymentUID','deploymentResourceVersion','deploymentSpecSHA256','beforeApiDeploymentSpecSHA256',
    'beforeApiServiceSpecSHA256','beforeApiServiceAnnotationsSHA256'].map(key=>[key,serving[key]])) : {};
  if (continuity) {
    // Pod может смениться внутри прежнего StatefulSet/backend. Остальные pins
    // authority, включая имя Pod и ownerUID, остаются частью durable identity.
    const {uid,...postgres} = authority.postgres;
    observation.durableAuthorityIdentitySHA256 = catalogAuthorityBaselineIdentity({...authority,postgres});
  }
  return {schema:continuity?'kodex.component-migration-continuity-readback/v1':'kodex.component-migration-adoption-readback/v2',
    observedAt:new Date().toISOString(),context,...observation,
    clusterUID:archive.clusterUID,namespaceUID:archive.namespaceUID,installationUID:archive.installationUID,
    installationSHA256:archive.installationSHA256,declaredSourceSHA:installation.data['source.sha'],
    declaredReleaseLockSHA256:installation.data['release-lock.sha256'],declaredSchemaHeads,
    migrationSourceSHA:migration.source.revision,schemaHeads,archiveTerminalSHA256:terminal.terminalSHA256,
    archiveExecutionSHA256:terminal.executionSHA256,archiveJobUID:terminal.jobUID,archivePodUID:terminal.podUID,
    authorityBaselineSHA256:authority.baselineSHA256,authorityIdentitySHA256:catalogAuthorityBaselineIdentity(authority),
    schemaCatalogSHA256:archive.schemaCatalogSHA256,storageSHA256:fingerprint(actualStorage)};
}

// Наблюдение authority сохраняет исторический scope как происхождение ротации.
// Текущая installation identity отдельно сверяется serving bracket; её data не подменяются.
export function readMigrationContinuityAuthority(rotation,events,historical,kube,io={}) {
  const original = validatePersonalIntegrationsRotationJournal(rotation,events);
  ensure(rotation.action === 'rotate' && rotation.policyTransition?.profile === personalIntegrationsAuthorityTransition &&
    original.jobUID && events.some(row=>row.status==='APPLIED' && row.operation==='REGISTRY_CAS') &&
    events.some(row=>row.status==='APPLIED' && row.operation==='PUBLISHER_RESTART') &&
    (io.inspectSource ?? inspectSource)(rotation.source).revision === rotation.revision,
    'MIGRATION_CONTINUITY_AUTHORITY_SOURCE_CHANGED');
  validateCatalogAuthorityBaseline(historical,Date.parse(historical.observedAt),110);
  ensure(historical.rotationPlanSHA256 === fingerprint(rotation) && historical.rotationEvidenceSHA256 === fingerprint(events) &&
    historical.targetSHA256 === rotation.policyTransition.targetSHA256 &&
    fingerprint(rotation.policyTransition.targetProfile) === historical.targetSHA256 &&
    scopeKeys.every(key=>historical.scope[key] === (key==='context'?rotation.context:rotation.policyTransition[key]) &&
      historical.scope[key] === rotation.policyTransition.targetProfile[key]), 'MIGRATION_CONTINUITY_AUTHORITY_SCOPE_CHANGED');
  const get = (kind,name) => JSON.parse(kube(['-n','kodex-system','get',kind,name,'-o','json']));
  const registry = get('configmap','internal-rpc-authority-publisher-target-registry');
  const publisher = get('deployment','internal-rpc-authority-publisher');
  for (const [resource,api,kind,name,uid] of [[registry,'v1','ConfigMap','internal-rpc-authority-publisher-target-registry',historical.registryUID],
    [publisher,'apps/v1','Deployment','internal-rpc-authority-publisher',historical.publisherUID]]) {
    const targets = rotation.policyTransition.targetProfile.resources.filter(row=>row.apiVersion===api && row.kind===kind && row.name===name);
    ensure(targets.length === 1 && targets[0].uid === uid && resource.apiVersion === api && resource.kind === kind && resource.metadata?.name === name &&
      resource.metadata.namespace === 'kodex-system' && resource.metadata.uid === uid && !resource.metadata.deletionTimestamp &&
      resource.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' &&
      resource.metadata.labels?.['kodex.dev/profile'] === 'web-only' &&
      resource.metadata.labels?.['kodex.dev/environment'] === 'production' && /^\d+$/.test(resource.metadata.resourceVersion ?? ''),
      'MIGRATION_CONTINUITY_AUTHORITY_RESOURCE_CHANGED');
  }
  ensure(registry.metadata.uid === rotation.registry.uid && publisher.metadata.uid === rotation.publisher.uid &&
    same(registry.data,rotation.registry.desiredData) && fingerprint(registry.data) === rotation.registry.desiredDataSHA256 &&
    fingerprint(registry.data) === historical.registryDataSHA256 &&
    registry.metadata.annotations?.['kodex.dev/rotation-intent'] === rotation.intentID &&
    registry.metadata.annotations?.['kodex.dev/rotation-operation'] === rotation.ownerOperationID &&
    fingerprint(publisher.spec) === historical.publisherSpecSHA256 &&
    fingerprint(publisher.spec) === rotation.publisher.desiredSpecSHA256 &&
    publisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-operation'] === rotation.ownerOperationID,
    'MIGRATION_CONTINUITY_AUTHORITY_CHANGED');
  const policy = validateAuthorityPolicy(registry.data['authority-policy.json'],'desired',rotation.policyTransition);
  const identity = deriveRegistryRotationIdentity(registry.data['key-delivery-targets.yaml']);
  const readers = personalIntegrationsAuthorityReadbackContract();
  validatePersonalIntegrationsAuthorityRegistry(registry.data['key-delivery-targets.yaml'],rotation.registry.predecessorRaw);
  ensure(policy.revision === 110 && policy.sha256 === historical.policySHA256 && identity.ownerOperationID === rotation.ownerOperationID &&
    identity.sourceRevision === rotation.registry.sourceRevision && identity.sourceRevision === historical.registryRevision &&
    identity.sourceDigestSHA256 === rotation.registry.sourceDigestSHA256 && identity.sourceDigestSHA256 === historical.registrySHA256 &&
    Object.keys(readers).every(key=>readers[key]===historical[key]) &&
    componentServingReady(publisher,activeDeploymentPods(readComponentServingInventory(kube,'internal-rpc-authority-publisher',publisher.metadata.uid),
      publisher.metadata.uid),publisher.spec.replicas), 'MIGRATION_CONTINUITY_AUTHORITY_NOT_READY');
  const postgresIdentity = () => authorityPostgresIdentity(kube);
  const postgres = postgresIdentity();
  ensure(postgres.ownerUID === historical.postgres.ownerUID, 'MIGRATION_CONTINUITY_AUTHORITY_POSTGRES_CHANGED');
  const state = authorityOwnerDatabase(kube,postgresIdentity).read(catalogAuthorityBaselineSQL(),postgres,historical.postgresBackend);
  validateRetiredCatalogOwnerState(state,rotation,identity,readers);
  ensure(state.operationId === historical.operationID && state.sourceRevision === historical.snapshotRevision &&
    state.sourceDigestSHA256 === historical.snapshotSHA256 && same(postgresIdentity(),postgres) &&
    same(get('configmap','internal-rpc-authority-publisher-target-registry'),registry) &&
    same(get('deployment','internal-rpc-authority-publisher'),publisher), 'MIGRATION_CONTINUITY_AUTHORITY_CHANGED');
  const {baselineSHA256,...body} = {...historical,postgres,observedAt:state.observedAt,actualReadbackCount:state.actualReadbackCount};
  const result = {...body,baselineSHA256:fingerprint(body)};
  validateCatalogAuthorityBaseline(result,Date.now(),110);
  return result;
}

function readInputs(dossierPath,dossierSHA256) {
  const dossier = readPinnedArchiveInput({path:dossierPath,sha256:dossierSHA256});
  ensure(dossier?.schema === 'kodex.component-migration-adoption-inputs/v1' &&
    Object.keys(dossier).length === inputKinds.length + 1 &&
    inputKinds.every(key => Object.keys(dossier[key] ?? {}).sort().join(',') === 'path,sha256'),
  'MIGRATION_ADOPTION_DOSSIER_REJECTED');
  const inputs = Object.fromEntries(inputKinds.map(key=>[key,readPinnedArchiveInput(dossier[key],key.endsWith('Journal'))]));
  return inputs;
}

export function readMigrationAdoption(context,kubeconfig,dossierPath,dossierSHA256,kube) {
  return {...readCustody(readInputs(dossierPath,dossierSHA256),context,kubeconfig,kube),dossierSHA256};
}

// Отдельный публичный read-only режим не принимает новый spec или target из payload.
export function readMigrationContinuity(context,kubeconfig,dossierPath,dossierSHA256,kube) {
  return {...readCustody(readInputs(dossierPath,dossierSHA256),context,kubeconfig,kube,true),dossierSHA256};
}

function main(args) {
  const continuity = args[0] === '--continuity';
  if (continuity) args = args.slice(1);
  const [context,kubeconfig,dossierPath,dossierSHA256] = args;
  ensure(args.length === 4 && /^[A-Za-z0-9_.:@/-]{1,160}$/.test(context) &&
    kubeconfig.startsWith('/') && !/[\r\n\0]/.test(kubeconfig),'MIGRATION_ADOPTION_ARGUMENTS_REJECTED');
  const deadline = performance.now() + 90000;
  const kube = (argv,input) => {
    const timeout = Math.min(24000,Math.floor(deadline - performance.now()));
    ensure(timeout > 0,'MIGRATION_ADOPTION_READBACK_DEADLINE');
    return execFileSync('kubectl',['--kubeconfig',kubeconfig,'--context',context,'--request-timeout=20s',...argv],
      {input,encoding:'utf8',timeout,maxBuffer:4<<20,stdio:['pipe','pipe','pipe']});
  };
  const read = continuity ? readMigrationContinuity : readMigrationAdoption;
  console.log(JSON.stringify(read(context,kubeconfig,dossierPath,dossierSHA256,kube)));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try { main(process.argv.slice(2)); }
  catch(error) {
    console.error(/^[A-Z][A-Z_0-9]+$/.test(error.message) ? error.message : 'MIGRATION_ADOPTION_READBACK_REJECTED');
    process.exitCode = 1;
  }
}
