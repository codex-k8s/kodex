#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {createHash, randomUUID} from 'node:crypto';
import {existsSync, readFileSync, readdirSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {inspectSource, sourceGitArguments} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';
import {validatedStablePublication} from './stable-image-publication.mjs';
import {createCanonicalRotationTemplate, validateRotationPrerequisites, verifyCatalogAuthorityJobPod} from './authority-rotation-transition.mjs';
import {materializeAuthorityJobDefaults} from './authority-job-defaults.mjs';
import {authorityPostgresIdentity} from './catalog-authority-baseline.mjs';
import {authorityOwnerDatabase, validateAuthorityDatabaseIdentity} from './migration-owner-database.mjs';
import {isProvenStoppedPod} from './stable-archive-transition.mjs';
import {catalogKubeconfig, parseAuthorityPrivateJSON, readAuthorityPrivateJSON, readAuthorityPrivateText, syncAuthorityJournalDirectory, writeAuthorityPrivatePlan} from './authority-catalog-transition.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {telegramProducerSecretReferences} from './telegram-introduction-inventory.mjs';

const diagnostic = Object.freeze({
  ackInvalid: 'ISSUER_MIGRATION_ACK_INVALID',
  ackRejected: 'ISSUER_MIGRATION_ACK_REJECTED',
  argumentRejected: 'ISSUER_MIGRATION_ARGUMENT_REJECTED',
  boundaryChanged: 'ISSUER_MIGRATION_BOUNDARY_CHANGED',
  createRejected: 'ISSUER_MIGRATION_CREATE_REJECTED',
  dependencyChanged: 'ISSUER_MIGRATION_DEPENDENCY_CHANGED',
  endpointChanged: 'ISSUER_MIGRATION_ENDPOINT_CHANGED',
  failedJobNotJoined: 'ISSUER_MIGRATION_FAILED_JOB_NOT_JOINED',
  foreignPod: 'ISSUER_MIGRATION_FOREIGN_POD',
  jobIdentityChanged: 'ISSUER_MIGRATION_JOB_IDENTITY_CHANGED',
  jobNotJoined: 'ISSUER_MIGRATION_JOB_NOT_JOINED',
  jobSpecChanged: 'ISSUER_MIGRATION_JOB_SPEC_CHANGED',
  joinChanged: 'ISSUER_MIGRATION_JOIN_CHANGED',
  journalRejected: 'ISSUER_MIGRATION_JOURNAL_REJECTED',
  listIncomplete: 'ISSUER_MIGRATION_LIST_INCOMPLETE',
  otherJobActive: 'ISSUER_MIGRATION_OTHER_JOB_ACTIVE',
  otherPodActive: 'ISSUER_MIGRATION_OTHER_POD_ACTIVE',
  ownerStateRejected: 'ISSUER_MIGRATION_OWNER_STATE_REJECTED',
  phaseRejected: 'ISSUER_MIGRATION_PHASE_REJECTED',
  planNotApproved: 'ISSUER_MIGRATION_PLAN_NOT_APPROVED',
  planRejected: 'ISSUER_MIGRATION_PLAN_REJECTED',
  preimageChanged: 'ISSUER_MIGRATION_PREIMAGE_CHANGED',
  serviceChanged: 'ISSUER_MIGRATION_SERVICE_CHANGED',
  sourceChanged: 'ISSUER_MIGRATION_SOURCE_CHANGED',
  sourceHeadChanged: 'ISSUER_MIGRATION_SOURCE_HEAD_CHANGED',
  sourceRejected: 'ISSUER_MIGRATION_SOURCE_REJECTED',
  sqlChanged: 'ISSUER_MIGRATION_SQL_CHANGED',
  targetChanged: 'ISSUER_MIGRATION_TARGET_CHANGED',
  targetOperationUnconfirmed: 'ISSUER_MIGRATION_TARGET_OPERATION_UNCONFIRMED',
  targetRejected: 'ISSUER_MIGRATION_TARGET_REJECTED',
  templateRejected: 'ISSUER_MIGRATION_TEMPLATE_REJECTED',
  terminalChanged: 'ISSUER_MIGRATION_TERMINAL_CHANGED',
  unknownReadbackRequired: 'ISSUER_MIGRATION_UNKNOWN_READBACK_REQUIRED',
});

const root = resolve(fileURLToPath(new URL('../../', import.meta.url)));
const namespace = 'kodex-system';
const schema = 'kodex.artifact-retention-issuer-migration/v1';
const beforeHead = 20261115000100, afterHead = 20261115000200;
const command = 'up-artifact-retention-issuer';
const jobName = 'authority-artifact-retention-issuer-' + afterHead;
const annotation = 'kodex.dev/artifact-retention-issuer-migration-plan';
const sqlBase = 'services/internal/internal-rpc-authority/cmd/cli/';
const files = {
  head: ['sql/artifact_retention_issuer_head.sql', '2ad967c67cab7dee091d99e66482162f4bf6f6284e6368a641845955255fa4d2'],
  role: ['sql/artifact_retention_issuer_readback.sql', '3988867304ae1c0fd53c55ff240440fa3d526eea51d664aa2a727ade593f924b'],
  owner: ['sql/artifact_retention_readback_role.sql', '768d44fe8ae1aab7e1e6a76b1cb0efc3e526a5ec16fc46f2019853802afe52f3'],
  migration: ['migrations/20261115000200_artifact_retention_issuer.sql', '388cabcae4d4563670140615f1522c71504f30950a2cda4d52dc060fc0fbcfbc'],
};
const uuid = value => typeof value === 'string' && /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const sha = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
const revision = value => typeof value === 'string' && /^[a-f0-9]{40}$/.test(value);
const same = (a,b) => fingerprint(a) === fingerprint(b);
const hash = value => createHash('sha256').update(value).digest('hex');
const requireValue = (ok, code) => { if (!ok) throw Error(code); };

// Observer повторно использует exact SQL из Go CLI, не копирует role predicate.
// Отдельные счётчики закрывают новый admission при чужом migrator/rotation.
export function issuerMigrationSQL(source) {
  const content = Object.fromEntries(Object.entries(files).map(([name,[file,expected]]) => {
    const raw = readFileSync(resolve(source,sqlBase,file),'utf8');
    requireValue(hash(raw) === expected,diagnostic.sqlChanged);
    return [name,raw.trim().replace(/;$/,'')];
  }));
  const migrations = readdirSync(resolve(source,sqlBase,'migrations')).filter(name=>/^\d{14}_.+\.sql$/.test(name)).sort();
  requireValue(migrations.length === 11 && migrations.at(-1) === files.migration[0].slice('migrations/'.length), diagnostic.sourceHeadChanged);
  const rotation = readFileSync(resolve(source,'tools/release/artifact-retention-issuer-migration-active-rotation.sql'),'utf8');
  requireValue(hash(rotation)==='26049014400227de7063cbbd6eddbcaf753c8e2bb5a6b9a5f1cbadfaedd7640a',diagnostic.sqlChanged);
  return 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\nSET LOCAL statement_timeout=\'5s\';\nSET LOCAL lock_timeout=\'1s\';\n' +
    'WITH issuer(head,role_exists) AS (\n' + content.head + '\n) SELECT jsonb_build_object(\n' +
    "'observedAt',clock_timestamp(),'head',(SELECT head FROM issuer),'roleExists',(SELECT role_exists FROM issuer),'roleValid',(\n" + content.role + '\n),\n' +
    "'activeRotationCount',(" + rotation + "),\n" +
    "'migratorSessions',(SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE datname=pg_catalog.current_database() AND usename='internal_rpc_authority_migrator' AND backend_type='client backend'));\nROLLBACK;\n";
}

export function validateIssuerMigrationState(state, after = false, now = Date.now()) {
  requireValue(state && Object.keys(state).sort().join(',') === 'activeRotationCount,head,migratorSessions,observedAt,roleExists,roleValid' &&
    state.head === (after ? afterHead : beforeHead) && state.roleExists === after && state.roleValid === after &&
    state.activeRotationCount === 0 && state.migratorSessions === 0 && Number.isFinite(Date.parse(state.observedAt)) &&
    Date.parse(state.observedAt) <= now + 5000 && now - Date.parse(state.observedAt) <= 30000, diagnostic.ownerStateRejected);
  return state;
}

// Общий canonical immutable render сохраняет identity/TLS/network profile;
// никакой исторический status106/107 plan либо обычный up не используется.
export function renderIssuerMigrationJob(plan, template = createCanonicalRotationTemplate(plan.source.path,plan.image,true)) {
  requireValue(plan?.schema === schema && uuid(plan.intentID) && revision(plan.source?.revision) &&
    typeof plan.source.path === 'string' && plan.source.path === resolve(plan.source.path) &&
    /^registry\.local\.kodex\/kodex\/internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(plan.image ?? ''), diagnostic.planRejected);
  const spec = structuredClone(template.spec), pod = spec?.template?.spec, container = pod?.containers?.[0];
  // yq YAML1.2 читает source literal0440 как440; здесь закрепляется Kubernetes YAML1.1 mode0440.
  const ca=pod?.volumes?.find(volume=>volume.name==='postgresql-ca');
  if(ca?.configMap?.name==='internal-rpc-authority-postgresql-ca'&&ca.configMap.defaultMode===440)ca.configMap.defaultMode=288;
  requireValue(template.kind === 'Job' && template.metadata?.namespace === namespace && template.metadata.name === 'internal-rpc-authority-migrate' &&
    pod.serviceAccountName === 'internal-rpc-authority-migrator' && pod.automountServiceAccountToken === false && pod.restartPolicy === 'Never' &&
    pod.containers.length === 1 && container.name === 'migrate' && container.image === plan.image &&
    same(container.command,['/usr/local/bin/internal-rpc-authority-cli']) && same(container.args,['rotation-status']) && !container.envFrom &&
    same(container.env,[{name:'INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE',value:'/var/run/secrets/kodex/internal-rpc-authority/postgres/dsn'},
      {name:'INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME',value:'internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local'}]) &&
    !pod.hostNetwork && !pod.hostPID && !pod.hostIPC && !pod.ephemeralContainers?.length &&
    same(container.volumeMounts,[{name:'postgresql-credentials',mountPath:'/var/run/secrets/kodex/internal-rpc-authority/postgres',readOnly:true},
      {name:'postgresql-ca',mountPath:'/var/run/config/kodex/internal-rpc-authority/postgresql',readOnly:true},{name:'tmp',mountPath:'/tmp'}]) &&
    same(pod.volumes,[{name:'postgresql-credentials',secret:{secretName:'internal-rpc-authority-postgres-migration',defaultMode:292}},
      {name:'tmp',emptyDir:{sizeLimit:'16Mi'}},{name:'postgresql-ca',configMap:{name:'internal-rpc-authority-postgresql-ca',defaultMode:288}}]), diagnostic.templateRejected);
  delete spec.selector; delete spec.ttlSecondsAfterFinished;
  spec.backoffLimit = 0; spec.activeDeadlineSeconds = 300;
  materializeAuthorityJobDefaults(spec);
  pod.enableServiceLinks ??= true;
  container.args = [command];
  for (const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name']) delete spec.template.metadata?.labels?.[key];
  return {apiVersion:'batch/v1',kind:'Job',metadata:{name:jobName,namespace,
    labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/name':'internal-rpc-authority','app.kubernetes.io/component':'migrator'},
    annotations:{[annotation]:fingerprint(plan),'kodex.dev/source-revision':plan.source.revision}},spec};
}

export function verifyIssuerMigrationJob(actual, expected, uid) {
  requireValue(actual?.apiVersion === 'batch/v1' && actual.kind === 'Job' && uuid(actual.metadata?.uid) && (!uid || actual.metadata.uid === uid) &&
    !actual.metadata.deletionTimestamp && actual.metadata.name === jobName && actual.metadata.namespace === namespace &&
    same(actual.metadata.annotations,expected.metadata.annotations), diagnostic.jobIdentityChanged);
  const spec = structuredClone(actual.spec);
  delete spec.selector;
  for (const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name']) delete spec.template?.metadata?.labels?.[key];
  requireValue(same(spec,expected.spec), diagnostic.jobSpecChanged);
  return actual;
}

// Failed process тоже требует положительного join. Его завершение не заменяет
// owner readback: COMMIT мог пройти до ошибки, но такой Job не становится PASS.
function failedIssuerMigrationJoin(job, pods) {
  requireValue(job.status?.failed===1&&!job.status.succeeded&&!job.status.active&&!job.status.terminating&&!job.metadata.deletionTimestamp&&
    Array.isArray(pods?.items)&&!pods.metadata?.continue&&pods.items.length===1,diagnostic.failedJobNotJoined);
  const pod=pods.items[0],expected=job.spec.template.spec,owners=pod.metadata?.ownerReferences?.filter(value=>value.controller===true)??[];
  requireValue(pod.kind==='Pod'&&pod.metadata?.namespace===namespace&&uuid(pod.metadata.uid)&&!pod.metadata.deletionTimestamp&&
    owners.length===1&&owners[0].kind==='Job'&&owners[0].name===job.metadata.name&&owners[0].uid===job.metadata.uid&&
    pod.status?.phase==='Failed'&&isProvenStoppedPod(pod)&&!['NodeLost','Unknown'].includes(pod.status.reason)&&
    !pod.status.conditions?.some(value=>value.status==='Unknown')&&
    Object.keys(pod.spec).every(key=>Object.hasOwn(expected,key)||['nodeName','preemptionPolicy','priority','tolerations'].includes(key))&&
    Object.entries(expected).every(([key,value])=>same(pod.spec[key],value)),diagnostic.failedJobNotJoined);
  for(const key of ['containers','initContainers']) {
    const statuses=pod.status[key==='containers'?'containerStatuses':'initContainerStatuses']??[];
    requireValue(statuses.length===(expected[key]??[]).length&&(expected[key]??[]).every(container=>{
      const state=statuses.find(value=>value.name===container.name),digest=container.image.split('@')[1];
      return state?.restartCount===0&&/^(?:(?:docker-pullable|containerd):\/\/)?(?:[a-z0-9][a-z0-9./:_-]*@)?sha256:[a-f0-9]{64}$/.test(state.imageID??'')&&state.imageID.endsWith(digest);
    }),diagnostic.failedJobNotJoined);
  }
  return {jobUID:job.metadata.uid,podUID:pod.metadata.uid};
}

const migratorServiceAccount = 'internal-rpc-authority-migrator';
const migratorSecret = 'internal-rpc-authority-postgres-migration';
const privileged = spec => spec?.serviceAccountName===migratorServiceAccount||spec?.serviceAccount===migratorServiceAccount||
  telegramProducerSecretReferences(spec).includes(migratorSecret)||
  spec?.volumes?.some(volume=>volume.csi?.nodePublishSecretRef?.name===migratorSecret);

function list(kube, kind) {
  const base=kind==='jobs'?'.spec.template.spec':'.spec';
  // Namespace history может быть большим. Сначала берём только identity/ref
  // metadata всех объектов, затем тело читаем у точных privileged candidates.
  const refs=['serviceAccountName','serviceAccount','.secretName','.secretKeyRef.name','.secretRef.name','.secret.name',
    '.imagePullSecrets[*].name','.nodePublishSecretRef.name'];
  const projection='jsonpath={.metadata.continue}{"\\n"}{range .items[*]}'+['.metadata.name','.metadata.uid',...refs.map(field=>base+'.'+field)]
    .map(path=>'{'+path+'}').join('{"\\t"}')+'{"\\n"}{end}';
  const raw=kube(['-n',namespace,'get',kind,'--chunk-size=0','-o',projection]);
  const [continuation,...lines]=raw.split('\n');
  const rows=lines.filter(Boolean);
  requireValue(continuation===''&&raw.endsWith('\n')&&rows.length<=4096,diagnostic.listIncomplete);
  const seen=new Set();
  return rows.map(row=>{
    const [name,valueUID,serviceAccount,alias,...secrets]=row.split('\t');
    requireValue(/^[a-z0-9][a-z0-9.-]{0,252}$/.test(name)&&uuid(valueUID)&&secrets.length===6&&!seen.has(name),diagnostic.listIncomplete);
    seen.add(name);
    if(serviceAccount!==migratorServiceAccount&&alias!==migratorServiceAccount&&!secrets.some(value=>value.split(' ').includes(migratorSecret))) {
      return {metadata:{name,uid:valueUID}};
    }
    const item=parseAuthorityPrivateJSON(kube(['-n',namespace,'get',kind.slice(0,-1),name,'-o','json']));
    requireValue(item.metadata?.uid===valueUID&&item.metadata.name===name&&item.metadata.namespace===namespace&&
      privileged(kind==='jobs'?item.spec?.template?.spec:item.spec),diagnostic.listIncomplete);
    return item;
  });
}

// Прежний OCI допустим при обновлении оператора, если на том же стенде
// опубликован его точный digest, а все inputs компиляции остались прежними.
function migrationImage(source, publication, target) {
  const receipt = publication?.publicationPlan;
  const built = receipt?.image?.sourceSHA;
  const image = validatedStablePublication(built, publication, 'internal-rpc-authority');
  requireValue(['context','clusterUID','namespaceUID'].every(key=>receipt[key]===target[key]) &&
    receipt.receipt.dockerfile === 'services/internal/internal-rpc-authority/Dockerfile', diagnostic.sourceRejected);
  if (built !== source.revision) {
    const git = (...args) => execFileSync('git',sourceGitArguments(source.path,...args),{
      timeout:10000,maxBuffer:65536,stdio:['ignore','pipe','pipe'],
    });
    try {
      git('merge-base','--is-ancestor',built,source.revision);
      git('diff','--quiet',built,source.revision,'--','services/internal/internal-rpc-authority/','libs/','contracts/',
        'go.mod','go.sum','go.work','go.work.sum');
    } catch { throw Error(diagnostic.sourceChanged); }
  }
  return image;
}

function readIssuerDatabaseCA(source, kube) {
  const name = 'internal-rpc-authority-postgresql-ca';
  const expected = {sources:[{secret:{name:'kodex-installation-ca',key:'tls.crt'}}],
    target:{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':namespace}},configMap:{key:'ca.pem'}}};
  const canonical = parseAuthorityPrivateJSON(execFileSync('yq',['-o=json',
    'select(.kind == "Bundle" and .metadata.name == "internal-rpc-authority-postgresql-ca") | .spec',
    resolve(source.path,'deploy/k8s/base/platform-state/ca-bundles.yaml')],
  {encoding:'utf8',timeout:5000,maxBuffer:262144,stdio:['ignore','pipe','pipe']}));
  const cm = parseAuthorityPrivateJSON(kube(['-n',namespace,'get','configmap',name,'-o','json']));
  const bundle = parseAuthorityPrivateJSON(kube(['get','bundles.trust.cert-manager.io',name,'-o','json']));
  const owners = cm.metadata?.ownerReferences ?? [];
  requireValue(same(canonical,expected) && cm.kind==='ConfigMap' && cm.metadata?.namespace===namespace &&
    cm.metadata.name===name && cm.metadata.labels?.['trust.cert-manager.io/bundle']===name &&
    !cm.metadata.deletionTimestamp && Object.keys(cm.data??{}).join(',')==='ca.pem' &&
    typeof cm.data['ca.pem']==='string' && cm.data['ca.pem'].startsWith('-----BEGIN CERTIFICATE-----') &&
    owners.length===1 && owners[0].apiVersion==='trust.cert-manager.io/v1alpha1' && owners[0].kind==='Bundle' &&
    owners[0].name===name && owners[0].controller===true && owners[0].uid===bundle.metadata?.uid &&
    bundle.apiVersion==='trust.cert-manager.io/v1alpha1' && bundle.kind==='Bundle' && bundle.metadata.name===name &&
    !bundle.metadata.deletionTimestamp && same(bundle.spec,expected) &&
    bundle.status?.conditions?.some(condition=>condition.type==='Synced'&&condition.status==='True') &&
    [cm,bundle].every(item=>uuid(item.metadata.uid)&&/^[1-9][0-9]*$/.test(item.metadata.resourceVersion??'')),
    diagnostic.dependencyChanged);
  return {resource:'configmap/'+name,uid:cm.metadata.uid,version:cm.metadata.resourceVersion,certificateSHA256:hash(cm.data['ca.pem']),
    bundle:{uid:bundle.metadata.uid,version:bundle.metadata.resourceVersion,specSHA256:fingerprint(expected)}};
}

// Все namespace Job/Pod с этой privileged service identity проверяются вместе;
// terminal Job без положительного завершения живого Pod не скрывает процесс.
export function assertNoOtherAuthorityWork(jobs, pods, expected) {
  const owned = job => privileged(job.spec?.template?.spec);
  const selected = jobs.find(job=>owned(job)&&job.metadata?.name===expected);
  const expectedUID = selected?.metadata?.uid;
  const others = jobs.filter(job => owned(job) && job.metadata?.name !== expected);
  for (const job of others) requireValue(job.metadata?.namespace === namespace && uuid(job.metadata.uid) && !job.metadata.deletionTimestamp &&
    !job.status?.active && !job.status?.terminating && job.status?.conditions?.filter(c=>['Complete','Failed'].includes(c.type)&&c.status==='True').length === 1 && !job.status.conditions.some(c=>c.status==='Unknown'),
    diagnostic.otherJobActive);
  for (const pod of pods.filter(p=>privileged(p.spec))) {
    const owners = pod.metadata?.ownerReferences?.filter(o=>o.controller===true) ?? [];
    requireValue(pod.metadata?.namespace === namespace && uuid(pod.metadata.uid) && owners.length === 1 && owners[0].kind === 'Job' && uuid(owners[0].uid), diagnostic.foreignPod);
    if (owners[0].name === expected && owners[0].uid === expectedUID) continue;
    requireValue(!['NodeLost','Unknown'].includes(pod.status?.reason) && !pod.status?.conditions?.some(c=>c.status==='Unknown') && isProvenStoppedPod(pod), diagnostic.otherPodActive);
  }
}

function readTarget(target,kube,source) {
  requireValue(target && /^[A-Za-z0-9_.:@/-]{1,160}$/.test(target.context ?? '') && uuid(target.clusterUID) && uuid(target.namespaceUID), diagnostic.targetRejected);
  const get = (kind,name) => parseAuthorityPrivateJSON(kube(['-n',namespace,'get',kind,name,'-o','json']));
  const cluster = parseAuthorityPrivateJSON(kube(['get','namespace','kube-system','-o','json']));
  const space = parseAuthorityPrivateJSON(kube(['get','namespace',namespace,'-o','json']));
  const installation = get('configmap','kodex-installation-profile');
  requireValue(cluster.metadata?.uid === target.clusterUID && space.metadata?.uid === target.namespaceUID && space.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' &&
    installation.metadata?.namespace === namespace && uuid(installation.metadata.uid), diagnostic.targetChanged);
  const service = get('service','internal-rpc-authority-postgresql-rw');
  requireValue(service.metadata?.namespace === namespace && uuid(service.metadata.uid) && same(service.spec?.selector,{'app.kubernetes.io/name':'kodex-postgresql'}) &&
    service.spec.type === 'ClusterIP' && service.spec.ports?.length === 1 && service.spec.ports[0].port === 5432 && service.spec.ports[0].targetPort === 'postgresql' &&
    (service.spec.ports[0].protocol ?? 'TCP') === 'TCP', diagnostic.serviceChanged);
  validateRotationPrerequisites(get('serviceaccount','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-postgresql-from-migrator'));
  const name='internal-rpc-authority-postgres-migration';
  const [dependencyUID,version,owner,...extra] = kube(['-n',namespace,'get','secret',name,'-o','jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.metadata.labels.app\\.kubernetes\\.io/part-of}']).trim().split('|');
  requireValue(uuid(dependencyUID) && /^[1-9][0-9]*$/.test(version ?? '') && owner === 'kodex' && !extra.length,diagnostic.dependencyChanged);
  const dependencies=[{resource:'secret/'+name,uid:dependencyUID,version},readIssuerDatabaseCA(source,kube)];
  return {context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,installationUID:installation.metadata.uid,installationSHA256:fingerprint(installation.data),serviceUID:service.metadata.uid,serviceSHA256:fingerprint(service.spec),dependencies};
}

function readBoundary(target,kube,source) {
  const scope = readTarget(target,kube,source), postgres = authorityPostgresIdentity(kube);
  const database = authorityOwnerDatabase(kube,()=>authorityPostgresIdentity(kube));
  const backend = database.identity(postgres);
  const pod = parseAuthorityPrivateJSON(kube(['-n',namespace,'get','pod',postgres.pod,'-o','json']));
  requireValue(pod.metadata?.uid===postgres.uid && Array.isArray(pod.status?.podIPs),diagnostic.endpointChanged);
  const slices = parseAuthorityPrivateJSON(kube(['-n',namespace,'get','endpointslices','-l','kubernetes.io/service-name=internal-rpc-authority-postgresql-rw','-o','json']));
  requireValue(Array.isArray(slices.items)&&!slices.metadata?.continue&&slices.items.length>=1&&slices.items.length<=2,diagnostic.endpointChanged);
  scope.endpoints=slices.items.map(slice=>{
    const owners=slice.metadata?.ownerReferences?.filter(value=>value.controller===true)??[];
    requireValue(slice.apiVersion==='discovery.k8s.io/v1'&&slice.kind==='EndpointSlice'&&uuid(slice.metadata?.uid)&&slice.metadata.namespace===namespace&&
      slice.metadata.labels?.['kubernetes.io/service-name']==='internal-rpc-authority-postgresql-rw'&&
      slice.metadata.labels?.['endpointslice.kubernetes.io/managed-by']==='endpointslice-controller.k8s.io'&&
      owners.length===1&&owners[0].kind==='Service'&&owners[0].name==='internal-rpc-authority-postgresql-rw'&&owners[0].uid===scope.serviceUID&&
      ['IPv4','IPv6'].includes(slice.addressType)&&slice.ports?.length===1&&slice.ports[0].port===5432&&slice.ports[0].protocol==='TCP'&&
      slice.endpoints?.length===1&&slice.endpoints[0].conditions?.ready===true&&slice.endpoints[0].conditions.serving!==false&&slice.endpoints[0].conditions.terminating!==true&&
      slice.endpoints[0].targetRef?.kind==='Pod'&&slice.endpoints[0].targetRef.name===postgres.pod&&slice.endpoints[0].targetRef.namespace===namespace&&slice.endpoints[0].targetRef.uid===postgres.uid&&
      slice.endpoints[0].addresses?.length===1&&pod.status.podIPs.some(item=>item.ip===slice.endpoints[0].addresses[0]),diagnostic.endpointChanged);
    return {uid:slice.metadata.uid,name:slice.metadata.name,specSHA256:fingerprint({addressType:slice.addressType,ports:slice.ports,endpoints:slice.endpoints})};
  }).sort((a,b)=>a.uid.localeCompare(b.uid));
  return {target:scope,postgres,backend,database};
}

export function planIssuerMigration({target,source,publication,evidencePath,intentID = randomUUID()},kube) {
  requireValue(revision(source?.revision) && source.path === resolve(source.path) && uuid(intentID),diagnostic.sourceRejected);
  requireValue(typeof evidencePath==='string'&&evidencePath===resolve(evidencePath)&&!existsSync(evidencePath),diagnostic.journalRejected);
  syncAuthorityJournalDirectory(evidencePath);
  const image = migrationImage(source,publication,target);
  const sql = issuerMigrationSQL(source.path), boundary = readBoundary(target,kube,source);
  const jobs=list(kube,'jobs');
  requireValue(!jobs.some(job=>job.metadata?.name===jobName),diagnostic.createRejected);
  assertNoOtherAuthorityWork(jobs,list(kube,'pods'),null);
  const before = boundary.database.read(sql,boundary.postgres,boundary.backend);
  validateIssuerMigrationState(before);
  const plan={schema,intentID,evidencePath,source,publication,image,target:boundary.target,postgres:boundary.postgres,backend:boundary.backend,sqlSHA256:hash(sql),before};
  plan.jobSpecSHA256=fingerprint(renderIssuerMigrationJob(plan).spec);
  return plan;
}

function validatePlan(plan) {
  requireValue(plan?.schema === schema && uuid(plan.intentID) && typeof plan.evidencePath==='string'&&plan.evidencePath===resolve(plan.evidencePath)&&revision(plan.source?.revision) && plan.source.path === resolve(plan.source.path) &&
    hash(issuerMigrationSQL(plan.source.path)) === plan.sqlSHA256 && migrationImage(plan.source,plan.publication,plan.target) === plan.image &&
    sha(plan.jobSpecSHA256)&&fingerprint(renderIssuerMigrationJob(plan).spec)===plan.jobSpecSHA256,
    diagnostic.planRejected);
  validateAuthorityDatabaseIdentity(plan.backend);
  validateIssuerMigrationState(plan.before,false,Date.parse(plan.before.observedAt));
}

export function issuerMigrationJournalHeader(plan) { return {schema,planSHA256:fingerprint(plan),intentID:plan.intentID}; }

export function validateIssuerMigrationJournal(events,plan,expected) {
  requireValue(Array.isArray(events) && events.length >= 1 && events.length <= 5,diagnostic.journalRejected);
  const exact = (row,value) => row && Number.isFinite(Date.parse(row.at)) && same(Object.fromEntries(Object.entries(row).filter(([key])=>key!=='at')),value);
  requireValue(exact(events[0],{type:'HEADER',...issuerMigrationJournalHeader(plan)}),diagnostic.journalRejected);
  if (events[1]) requireValue(exact(events[1],{type:'CREATE_INTENT',jobName,jobSpecSHA256:fingerprint(expected.spec)}),diagnostic.journalRejected);
  let body = events;
  const rejected = events.at(-1)?.type === 'ACK_REJECTED';
  if (rejected) { requireValue(events.length>=3 && exact(events.at(-1),{type:'ACK_REJECTED'}),diagnostic.journalRejected); body=events.slice(0,-1); }
  if (body[2]) requireValue(uuid(body[2].jobUID) && exact(body[2],{type:'CREATE_ACK',jobUID:body[2].jobUID}),diagnostic.journalRejected);
  if (body[3]) {
    const terminal=body[3];
    requireValue(['PASS','FAILED'].includes(terminal.type)&&exact(terminal,{type:terminal.type,jobUID:body[2].jobUID,podUID:terminal.podUID,stateSHA256:terminal.stateSHA256,
      ...(terminal.type==='FAILED'?{ownerAfter:terminal.ownerAfter}:{})})&&uuid(terminal.podUID)&&sha(terminal.stateSHA256)&&
      (terminal.type!=='FAILED'||typeof terminal.ownerAfter==='boolean'),diagnostic.journalRejected);
  }
  requireValue(body.length <= 4,diagnostic.journalRejected);
  return {intent:body[1],ack:body[2],terminal:body[3],rejected};
}

// CREATE_INTENT фиксируется fsync до единственного create. Даже отсутствие Job
// после неизвестного ACK допускает только status/readback, не повтор создания.
export async function executeIssuerMigration(phase,plan,journal,io) {
  requireValue(['apply','status'].includes(phase),diagnostic.phaseRejected);
  const expected = io.expectedJob(), history = validateIssuerMigrationJournal(journal.events,plan,expected);
  requireValue(!history.rejected,diagnostic.ackRejected);
  await io.preflight();
  let actual = await io.getJob();
  if (!history.intent) {
    requireValue(phase === 'apply' && !actual,diagnostic.createRejected);
    const state = await io.readState();
    validateIssuerMigrationState(state);
    requireValue(same({...plan.before,observedAt:null},{...state,observedAt:null}),diagnostic.preimageChanged);
    journal.append({type:'CREATE_INTENT',jobName,jobSpecSHA256:fingerprint(expected.spec)});
    try { actual = await io.createJob(expected); }
    catch(error) { if(error.message===diagnostic.ackInvalid)journal.append({type:'ACK_REJECTED'}); throw error; }
    if (!actual) { journal.append({type:'ACK_REJECTED'}); throw Error(diagnostic.ackRejected); }
  }
  requireValue(actual,diagnostic.unknownReadbackRequired);
  if (!history.ack && uuid(actual.metadata?.uid)) journal.append({type:'CREATE_ACK',jobUID:actual.metadata.uid});
  try { verifyIssuerMigrationJob(actual,expected,history.ack?.jobUID); }
  catch(error) { if(!history.ack)journal.append({type:'ACK_REJECTED'}); throw error; }
  const failed=!!actual.status?.failed||actual.status?.conditions?.some(c=>c.type==='Failed'&&c.status==='True');
  if (actual.status?.succeeded !== 1&&!failed) return {status:'RUNNING',jobName,jobUID:actual.metadata.uid};
  if(history.terminal)requireValue((history.terminal.type==='FAILED')===failed,diagnostic.terminalChanged);
  const join = (job,pods) => {requireValue(!job.status?.terminating,diagnostic.jobNotJoined);return (failed ? failedIssuerMigrationJoin : verifyCatalogAuthorityJobPod)(job,pods);};
  const joined = join(actual,await io.getPods(actual));
  await io.preflight();
  const after = await io.readState();
  const ownerAfter=after.head===afterHead;
  validateIssuerMigrationState(after,failed?ownerAfter:true);
  const final = await io.getJob();
  verifyIssuerMigrationJob(final,expected,joined.jobUID);
  requireValue(same(join(final,await io.getPods(final)),joined),diagnostic.joinChanged);
  if (!history.terminal) journal.append({type:failed?'FAILED':'PASS',...joined,stateSHA256:fingerprint(after),...(failed?{ownerAfter}:{})});
  else requireValue(history.terminal.jobUID===joined.jobUID && history.terminal.podUID===joined.podUID,diagnostic.joinChanged);
  return {status:failed?'FAILED':'PASS',jobName,...joined,migrationVersion:after.head,...(failed?{ownerAfter,roleReady:after.roleValid}:{}),forwardOnly:true};
}

export function issuerMigrationIO(plan,kube) {
  validatePlan(plan);
  const expected = renderIssuerMigrationJob(plan);
  const database = authorityOwnerDatabase(kube,()=>authorityPostgresIdentity(kube));
  return {
    expectedJob:()=>expected,
    preflight() {
      validatePlan(plan);
      const actual = readBoundary(plan.target,kube,plan.source);
      requireValue(same(actual.target,plan.target) && same(actual.postgres,plan.postgres) && same(actual.backend,plan.backend),diagnostic.boundaryChanged);
      assertNoOtherAuthorityWork(list(kube,'jobs'),list(kube,'pods'),jobName);
    },
    readState:()=>database.read(issuerMigrationSQL(plan.source.path),plan.postgres,plan.backend),
    getJob() { const raw=kube(['-n',namespace,'get','job',jobName,'--ignore-not-found=true','-o','json']); return raw.trim()?parseAuthorityPrivateJSON(raw):null; },
    createJob(job) { const raw=kube(['create','-f','-','-o','json'],JSON.stringify(job)); try { return parseAuthorityPrivateJSON(raw); } catch { throw Error(diagnostic.ackInvalid); } },
    getPods(job) { return parseAuthorityPrivateJSON(kube(['-n',namespace,'get','pods','-l',`batch.kubernetes.io/controller-uid=${job.metadata.uid}`,'-o','json'])); },
  };
}

// Отдельный metadata-only reader для исторического READ контекста. Он не
// меняет original plan: пригодность текущего Pod/endpoint проверяется тем же
// canonical boundary, а mutating issuerMigrationIO сохраняет exact old pins.
export function issuerMigrationHistoricalReadinessBoundary(plan,kube){
  validatePlan(plan);
  const {target,postgres,backend}=readBoundary(plan.target,kube,plan.source);
  return {target,postgres,backend};
}

// Этот адаптер не имеет write port даже при ошибочном вызове через apply.
// Новая Pod incarnation допустима только в explicit historical context;
// installation/dependencies/Service/StatefulSet/durable backend остаются exact.
export function issuerMigrationHistoricalReadinessIO(plan,kube,current){
  const original=issuerMigrationIO(plan,kube);
  current=structuredClone(current);
  const stable=target=>Object.fromEntries(Object.entries(target).filter(([key])=>key!=='endpoints'));
  requireValue(current?.purpose==='HISTORICAL_ROLE_ONLY'&&same(stable(current.target),stable(plan.target))&&
    current.postgres?.pod===plan.postgres.pod&&current.postgres.ownerUID===plan.postgres.ownerUID&&uuid(current.postgres.uid)&&
    same(current.backend,plan.backend),diagnostic.boundaryChanged);
  const database=authorityOwnerDatabase(kube,()=>authorityPostgresIdentity(kube));
  return {...original,
    preflight(){
      validatePlan(plan);
      const actual=readBoundary(plan.target,kube,plan.source);
      requireValue(same(actual.target,current.target)&&same(actual.postgres,current.postgres)&&same(actual.backend,plan.backend),diagnostic.boundaryChanged);
      assertNoOtherAuthorityWork(list(kube,'jobs'),list(kube,'pods'),jobName);
    },
    readState:()=>database.read(issuerMigrationSQL(plan.source.path),current.postgres,plan.backend),
    createJob(){throw Error('ISSUER_MIGRATION_HISTORICAL_READ_ONLY');},
  };
}

// Перед допуском 049 подтверждаем прежний durable PASS и заново перечитываем
// exact Job/Pod join и owner schema. Этот проход не приписывает журналу receipt.
export async function readArtifactRetentionIssuerMigrationReady({planPath,evidencePath,sourceSHA,target},kube) {
  const plan=readAuthorityPrivateJSON(planPath);
  const keys=['context','clusterUID','namespaceUID','installationUID','installationSHA256'];
  requireValue(plan?.source?.revision===sourceSHA&&inspectSource(plan.source.path).revision===sourceSHA&&
    plan.evidencePath===evidencePath&&keys.every(key=>plan.target?.[key]===target?.[key]),diagnostic.sourceChanged);
  const journal=privateJournal(evidencePath,issuerMigrationJournalHeader(plan),{readOnly:true});
  try {
    const io=issuerMigrationIO(plan,kube);
    const known=validateIssuerMigrationJournal(journal.events,plan,io.expectedJob());
    requireValue(known.terminal?.type==='PASS'&&!known.rejected,diagnostic.journalRejected);
    const result=await executeIssuerMigration('status',plan,journal,io);
    requireValue(result.status==='PASS'&&result.migrationVersion===afterHead,diagnostic.ownerStateRejected);
    return {schema:'kodex.artifact-retention-issuer-migration-ready/v1',sourceSHA,
      target:Object.fromEntries(keys.map(key=>[key,plan.target[key]])),planSHA256:fingerprint(plan),evidenceSHA256:journal.bytesSHA256,
      image:plan.image,jobUID:result.jobUID,podUID:result.podUID,migrationVersion:result.migrationVersion};
  } finally { journal.close(); }
}

export async function mainIssuerMigration(args) {
  const phase=args.shift(), opts={};
  const allowed={plan:['--cluster-uid','--namespace-uid','--publication-plan','--publication-evidence','--output','--evidence'],apply:['--plan','--evidence','--approved-plan-sha'],status:['--plan','--evidence']};
  requireValue(Object.hasOwn(allowed,phase ?? ''),diagnostic.argumentRejected);
  const flags=['--context','--kubeconfig',...allowed[phase]];
  while(args.length) { const key=args.shift(); requireValue(flags.includes(key)&&!Object.hasOwn(opts,key)&&args[0]&&!args[0].startsWith('--'),diagnostic.argumentRejected); opts[key]=args.shift(); }
  requireValue(Object.keys(opts).length===flags.length && /^[A-Za-z0-9_.:@/-]{1,160}$/.test(opts['--context']),diagnostic.argumentRejected);
  const config=catalogKubeconfig(opts['--kubeconfig']);
  const kube=(argv,input)=>{try{return execFileSync('kubectl',['--kubeconfig',config,'--context',opts['--context'],'--request-timeout=10s',...argv],{input,encoding:'utf8',timeout:12000,maxBuffer:4<<20,stdio:['pipe','pipe','pipe']});}catch{throw Error(diagnostic.targetOperationUnconfirmed);}};
  const source={path:root,revision:inspectSource(root).revision};
  if(phase==='plan') {
    const publication={publicationPlan:readAuthorityPrivateJSON(opts['--publication-plan']),publicationEvidence:readAuthorityPrivateText(opts['--publication-evidence']).trimEnd().split('\n').map(parseAuthorityPrivateJSON)};
    const plan=planIssuerMigration({target:{context:opts['--context'],clusterUID:opts['--cluster-uid'],namespaceUID:opts['--namespace-uid']},source,publication,evidencePath:opts['--evidence']},kube);
    plan.kubeconfig=config;
    writeAuthorityPrivatePlan(opts['--output'],plan);
    console.log(JSON.stringify({status:'PLANNED',planSHA256:fingerprint(plan),migrationVersion:afterHead}));return;
  }
  const plan=readAuthorityPrivateJSON(opts['--plan']);
  requireValue(plan.source.revision===source.revision&&plan.source.path===source.path&&plan.kubeconfig===config&&plan.target.context===opts['--context']&&plan.evidencePath===opts['--evidence'],diagnostic.sourceChanged);
  if(phase==='apply')requireValue(opts['--approved-plan-sha']===fingerprint(plan),diagnostic.planNotApproved);
  syncAuthorityJournalDirectory(opts['--evidence']);
  if(existsSync(opts['--evidence']))readAuthorityPrivateText(opts['--evidence']);
  const journal=privateJournal(opts['--evidence'],issuerMigrationJournalHeader(plan),{readOnly:phase==='status'&&!existsSync(opts['--evidence'])});
  try { const result=await executeIssuerMigration(phase,plan,journal,issuerMigrationIO(plan,kube));console.log(JSON.stringify(result));if(result.status==='FAILED')process.exitCode=1; }
  finally { journal.close(); }
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))mainIssuerMigration(process.argv.slice(2)).catch(error=>{process.stderr.write('Artifact-retention issuer migration failed: '+(/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED')+'\n');process.exitCode=1;});
