import {createECDH, createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {lstatSync, readFileSync, readdirSync, realpathSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {catalogKubeconfig, parseAuthorityPrivateJSON, readAuthorityPrivateJSON, readAuthorityPrivateText, writeAuthorityPrivatePlan} from './authority-catalog-transition.mjs';
import {inspectSource} from './application-source.mjs';
import {fingerprint, withPrivatePatchFile} from './scoped-release.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {authorityOwnerDatabase, authorityOwnerDatabaseArgs, validateAuthorityDatabaseIdentity} from './migration-owner-database.mjs';

const namespace = 'kodex-system';
const role = 'ira_artifact_retention_issuer_g1';
const keyID = 'artifact-retention-platform-worker-g1';
const signerName = 'artifact-retention-platform-worker-grant-signer';
const databaseName = 'internal-rpc-authority-artifact-retention-issuer-postgresql';
const trustName = 'control-plane-application-grants';
const trustKey = 'artifact-retention.platform-worker.public.jwk';
const credentialResource = 'artifact-retention-issuer-login';
const planAnnotation = 'kodex.dev/artifact-retention-issuer-material-plan';
const targetFiles = ['postgresql-dsn', 'postgresql-password', 'postgresql-username', 'private.jwk', 'public.jwk'];
const dsnEnd = '@internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local:5432/internal_rpc_authority?sslmode=verify-full&sslrootcert=/var/run/config/kodex/internal-rpc-authority/postgresql/ca.pem';
const uuid = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/;
const sha = /^[0-9a-f]{64}$/;
const revision = /^[0-9a-f]{40}$/;
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const requireValue = (ok, code) => { if (!ok) throw Error(code); };

function validWorkerJWK(value, privateKey) {
  const keys = ['alg', 'crv', 'key_ops', 'kid', 'kty', 'use', 'x', 'y', ...(privateKey ? ['d'] : [])].sort();
  const coord = field => typeof field === 'string' && /^[A-Za-z0-9_-]{43}$/.test(field) &&
    Buffer.from(field, 'base64url').length === 32 && Buffer.from(field, 'base64url').toString('base64url') === field;
  return value && Object.keys(value).sort().join(',') === keys.join(',') &&
    value.alg === 'ES256' && value.use === 'sig' && value.kty === 'EC' && value.crv === 'P-256' &&
    value.kid === keyID && fingerprint(value.key_ops) === fingerprint([privateKey ? 'sign' : 'verify']) &&
    coord(value.x) && coord(value.y) && (!privateKey || coord(value.d));
}

// Материал остаётся в закрытом локальном каталоге. Наружу из этой проверки
// передаются только хеши. Скаляр отдельно сверяется с открытой точкой.
function readArtifactRetentionIssuerMaterial(directory) {
  requireValue(typeof directory === 'string' && directory === resolve(directory) && directory === realpathSync(directory),
    'ARTIFACT_RETENTION_MATERIAL_PATH_REJECTED');
  const before = lstatSync(directory);
  requireValue(before.isDirectory() && (before.mode & 0o777) === 0o700 && before.uid === process.getuid() &&
    readdirSync(directory).sort().join(',') === targetFiles.join(','), 'ARTIFACT_RETENTION_MATERIAL_CUSTODY_REJECTED');
  const files = Object.fromEntries(targetFiles.map(name => [name, readAuthorityPrivateText(resolve(directory, name))]));
  const after = lstatSync(directory);
  requireValue(after.isDirectory() && before.ino === after.ino && before.dev === after.dev && before.mtimeMs === after.mtimeMs &&
    (after.mode & 0o777) === 0o700 && after.uid === process.getuid(), 'ARTIFACT_RETENTION_MATERIAL_CHANGED');
  const privateKey = parseAuthorityPrivateJSON(files['private.jwk']);
  const publicKey = parseAuthorityPrivateJSON(files['public.jwk']);
  requireValue(validWorkerJWK(privateKey, true) && validWorkerJWK(publicKey, false), 'ARTIFACT_RETENTION_WORKER_KEY_REJECTED');
  try {
    const signer = createECDH('prime256v1');
    signer.setPrivateKey(Buffer.from(privateKey.d, 'base64url'));
    const expected = Buffer.concat([Buffer.from([4]), Buffer.from(publicKey.x, 'base64url'), Buffer.from(publicKey.y, 'base64url')]);
    requireValue(signer.getPublicKey().equals(expected) && privateKey.x === publicKey.x && privateKey.y === publicKey.y,
      'ARTIFACT_RETENTION_WORKER_KEY_MISMATCH');
  } catch {
    throw Error('ARTIFACT_RETENTION_WORKER_KEY_MISMATCH');
  }
  const password = files['postgresql-password'];
  requireValue(files['postgresql-username'] === role && sha.test(password) &&
    files['postgresql-dsn'] === 'postgresql://' + role + ':' + password + dsnEnd, 'ARTIFACT_RETENTION_DATABASE_MATERIAL_REJECTED');
  const signer = {'private.jwk': Buffer.from(files['private.jwk']).toString('base64')};
  const database = {dsn: Buffer.from(files['postgresql-dsn']).toString('base64'), username: Buffer.from(role).toString('base64')};
  return {
    metadata: {sources: Object.fromEntries(targetFiles.map(name => [name, hash(files[name])])),
      signerSHA256: fingerprint(signer), databaseSHA256: fingerprint(database), publicTrustSHA256: hash(files['public.jwk'])},
    signer, database, trustValue: Buffer.from(files['public.jwk']).toString('base64'), password,
  };
}

export function inspectArtifactRetentionIssuerMaterial(directory) {
  return readArtifactRetentionIssuerMaterial(directory).metadata;
}

function readSecretMetadata(kube, name) {
  // В выводе команды только metadata и имена ключей, значения не читаются инструментом.
  const rows = kube(['-n', namespace, 'get', 'secret', name, '--ignore-not-found=true',
    '-o', 'go-template={{.metadata.uid}}{{"\\n"}}{{.metadata.resourceVersion}}{{"\\n"}}{{index .metadata.labels "app.kubernetes.io/part-of"}}{{"\\n"}}{{.immutable}}{{"\\n"}}{{with .metadata.annotations}}{{index . "kodex.dev/artifact-retention-issuer-material-plan"}}{{end}}{{"\\n"}}{{if .metadata.annotations}}yes{{else}}no{{end}}{{"\\n"}}{{range $name,$_ := .data}}{{$name}}{{"\\n"}}{{end}}']);
  if (!rows) return null;
  const [uid, resourceVersion, owner, immutable, marker, annotations, ...keys] = rows.trimEnd().split('\n');
  requireValue(uuid.test(uid) && /^[1-9][0-9]*$/.test(resourceVersion) && owner === 'kodex' &&
    ['true', 'false', '<no value>'].includes(immutable) && keys.every(key => /^[A-Za-z0-9_.-]{1,180}$/.test(key)),
    'ARTIFACT_RETENTION_ISSUER_SECRET_OWNERSHIP_REJECTED');
  requireValue(typeof marker === 'string' && ['yes', 'no'].includes(annotations), 'ARTIFACT_RETENTION_ISSUER_SECRET_METADATA_REJECTED');
  return {uid, resourceVersion, immutable: immutable === 'true', marker: marker === '<no value>' ? '' : marker, hasAnnotations: annotations === 'yes', keys: keys.sort()};
}

function readTarget(target, kube) {
  requireValue(target && /^[a-zA-Z0-9_.:@/-]{1,160}$/.test(target.context ?? '') &&
    uuid.test(target.clusterUID ?? '') && uuid.test(target.namespaceUID ?? ''), 'ARTIFACT_RETENTION_ISSUER_PLAN_SCOPE_REJECTED');
  const get = args => JSON.parse(kube([...args, '-o', 'json']));
  const cluster = get(['get', 'namespace', 'kube-system']);
  const space = get(['get', 'namespace', namespace]);
  const installation = get(['-n', namespace, 'get', 'configmap', 'kodex-installation-profile']);
  requireValue(cluster.metadata?.uid === target.clusterUID && space.metadata?.uid === target.namespaceUID &&
    space.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' &&
    installation.metadata?.namespace === namespace && uuid.test(installation.metadata.uid),
    'ARTIFACT_RETENTION_ISSUER_TARGET_CHANGED');
  return {context: target.context, clusterUID: target.clusterUID, namespaceUID: target.namespaceUID,
    installationUID: installation.metadata.uid, installationSHA256: fingerprint(installation.data)};
}

// План использует точные UID уже выбранной установки и добавляет один открытый
// ключ в существующий Secret доверия при его прежнем resourceVersion.
export function planArtifactRetentionIssuerMaterial({target, sourceRevision, material, kube}) {
  const observed = readTarget(target, kube);
  requireValue(revision.test(sourceRevision ?? '') &&
    [material?.signerSHA256, material?.databaseSHA256, material?.publicTrustSHA256].every(value => typeof value === 'string' && sha.test(value)),
    'ARTIFACT_RETENTION_ISSUER_PLAN_SCOPE_REJECTED');
  requireValue(readSecretMetadata(kube, signerName) === null && readSecretMetadata(kube, databaseName) === null,
    'ARTIFACT_RETENTION_ISSUER_SECRET_ALREADY_EXISTS');
  const trust = readSecretMetadata(kube, trustName);
  requireValue(trust && !trust.immutable && !trust.keys.includes(trustKey) && trust.keys.length > 0 &&
    trust.keys.every(key => key.endsWith('.public.jwk')), 'ARTIFACT_RETENTION_ISSUER_TRUST_PREDECESSOR_REJECTED');
  return {
    schema: 'kodex.artifact-retention-issuer-material-plan/v1', sourceRevision,
    target: observed,
    secretCreates: [
      {name: signerName, keys: ['private.jwk'], sha256: material.signerSHA256},
      {name: databaseName, keys: ['dsn', 'username'], sha256: material.databaseSHA256},
    ],
    publicTrustAppend: {name: trustName, uid: trust.uid, resourceVersion: trust.resourceVersion,
      retainedKeyNamesSHA256: fingerprint(trust.keys), key: trustKey, sha256: material.publicTrustSHA256},
    materialSourceSHA256: fingerprint(material.sources),
  };
}

function assertMaterialPlan(plan, sourceRevision, context, kube) {
  requireValue(plan?.schema === 'kodex.artifact-retention-issuer-material-plan/v1' &&
    plan.sourceRevision === sourceRevision && plan.target?.context === context &&
    sha.test(plan.materialSourceSHA256 ?? '') && Array.isArray(plan.secretCreates) && plan.secretCreates.length === 2 &&
    plan.secretCreates[0]?.name === signerName && fingerprint(plan.secretCreates[0].keys) === fingerprint(['private.jwk']) &&
    plan.secretCreates[1]?.name === databaseName && fingerprint(plan.secretCreates[1].keys) === fingerprint(['dsn', 'username']) &&
    plan.secretCreates.every(entry => sha.test(entry.sha256 ?? '')) &&
    plan.publicTrustAppend?.name === trustName && plan.publicTrustAppend.key === trustKey &&
    uuid.test(plan.publicTrustAppend.uid ?? '') && /^[1-9][0-9]*$/.test(plan.publicTrustAppend.resourceVersion ?? '') &&
    sha.test(plan.publicTrustAppend.retainedKeyNamesSHA256 ?? '') && sha.test(plan.publicTrustAppend.sha256 ?? ''),
    'ARTIFACT_RETENTION_ISSUER_PLAN_REJECTED');
  requireValue(fingerprint(readTarget(plan.target, kube)) === fingerprint(plan.target),
    'ARTIFACT_RETENTION_ISSUER_TARGET_CHANGED');
}

function verifyIssuerRole(kube) {
  const identify = () => {
    const raw = JSON.parse(kube(['-n', namespace, 'get', 'pod', 'kodex-postgresql-0', '-o', 'json']));
    const owners = (raw.metadata?.ownerReferences ?? []).filter(owner => owner.controller === true);
    requireValue(uuid.test(raw.metadata?.uid ?? '') && !raw.metadata.deletionTimestamp && raw.status?.phase === 'Running' &&
      owners.length === 1 && owners[0].kind === 'StatefulSet' && owners[0].name === 'kodex-postgresql' && uuid.test(owners[0].uid),
      'ARTIFACT_RETENTION_ISSUER_DATABASE_OWNER_REJECTED');
    return {pod: 'kodex-postgresql-0', uid: raw.metadata.uid, ownerUID: owners[0].uid};
  };
  const database = authorityOwnerDatabase(kube, identify);
  const pod = identify();
  const backend = database.identity(pod);
  const base = new URL('../../services/internal/internal-rpc-authority/cmd/cli/sql/', import.meta.url);
  const roleSQL = readFileSync(new URL('artifact_retention_issuer_readback.sql', base), 'utf8').trim().replace(/;$/, '');
  const source = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;SET LOCAL statement_timeout='5s';" +
    "SELECT jsonb_build_object('schema',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied), 'validRole',(" +
    roleSQL + "));ROLLBACK;";
  const proof = database.read(source, pod, backend);
  requireValue(proof?.schema === 20261115000200 && proof.validRole === true, 'ARTIFACT_RETENTION_ISSUER_ROLE_NOT_READY');
  return {database, pod, backend};
}

// Читается только назначенный ключ конкретного Secret после проверки его UID.
function readExactSecretField(kube, name, uid, key) {
  const format = 'go-template={{.metadata.uid}}{{"\\n"}}{{index .data ' + JSON.stringify(key) + '}}';
  const [observed, value, ...extra] = kube(['-n', namespace, 'get', 'secret', name, '-o', format]).trimEnd().split('\n');
  requireValue(observed === uid && typeof value === 'string' && value.length > 0 &&
    /^[A-Za-z0-9+/]+={0,2}$/.test(value) && !extra.length, 'ARTIFACT_RETENTION_ISSUER_SECRET_READBACK_REJECTED');
  return value;
}

function readCreatedSecret(kube, requested, planSHA256) {
  const observed = readSecretMetadata(kube, requested.name);
  if (!observed) return null;
  requireValue(observed.immutable && observed.marker === planSHA256 &&
    fingerprint(observed.keys) === fingerprint([...requested.keys].sort()), 'ARTIFACT_RETENTION_ISSUER_SECRET_NOT_OWNED_BY_PLAN');
  const data = Object.fromEntries(requested.keys.map(key => [key, readExactSecretField(kube, requested.name, observed.uid, key)]));
  requireValue(fingerprint(data) === requested.sha256, 'ARTIFACT_RETENTION_ISSUER_SECRET_CONTENT_CHANGED');
  return observed.uid;
}

function readTrust(kube, expected, planSHA256) {
  const observed = readSecretMetadata(kube, expected.name);
  requireValue(observed?.uid === expected.uid && !observed.immutable &&
    fingerprint(observed.keys.filter(key => key !== expected.key)) === expected.retainedKeyNamesSHA256,
    'ARTIFACT_RETENTION_ISSUER_TRUST_CHANGED');
  if (!observed.keys.includes(expected.key)) {
    requireValue(observed.resourceVersion === expected.resourceVersion && !observed.marker,
      'ARTIFACT_RETENTION_ISSUER_TRUST_CHANGED');
    return {observed};
  }
  requireValue(observed.marker === planSHA256 &&
    hash(Buffer.from(readExactSecretField(kube, expected.name, expected.uid, expected.key), 'base64')) === expected.sha256,
    'ARTIFACT_RETENTION_ISSUER_TRUST_CONTENT_CHANGED');
  return {observed, uid: observed.uid};
}

// Один intent на ресурс. После неопределённого ответа повторный вызов
// подтверждает точное состояние по UID, marker и digest либо останавливается.
function materialStep({resource, journal, observe, execute}) {
  const intent = journal.events.find(event => event.type === 'INTENT' && event.resource === resource);
  const ack = journal.events.find(event => event.type === 'ACK' && event.resource === resource);
  const actual = observe();
  if (ack) {
    requireValue(actual === ack.uid, 'ARTIFACT_RETENTION_ISSUER_RESOURCE_REPLACED');
    return;
  }
  if (intent) {
    requireValue(actual, 'ARTIFACT_RETENTION_ISSUER_RECONCILIATION_REQUIRED');
    journal.append({type: 'ACK', resource, uid: actual});
    return;
  }
  requireValue(!actual, 'ARTIFACT_RETENTION_ISSUER_UNEXPECTED_RESOURCE');
  journal.append({type: 'INTENT', resource});
  try { execute(); }
  catch {
    journal.append({type: 'UNKNOWN', resource});
    throw Error('ARTIFACT_RETENTION_ISSUER_RECONCILIATION_REQUIRED');
  }
  const committed = observe();
  requireValue(committed, 'ARTIFACT_RETENTION_ISSUER_RECONCILIATION_REQUIRED');
  journal.append({type: 'ACK', resource, uid: committed});
}

export function applyArtifactRetentionIssuerMaterial({plan, materialDirectory, kube, journal}) {
  assertMaterialPlan(plan, plan.sourceRevision, plan.target.context, kube);
  verifyIssuerRole(kube);
  materialStatus({plan, kube, journal});
  const material = readArtifactRetentionIssuerMaterial(materialDirectory);
  requireValue(fingerprint(material.metadata.sources) === plan.materialSourceSHA256 &&
    material.metadata.signerSHA256 === plan.secretCreates[0].sha256 && material.metadata.databaseSHA256 === plan.secretCreates[1].sha256 &&
    material.metadata.publicTrustSHA256 === plan.publicTrustAppend.sha256, 'ARTIFACT_RETENTION_ISSUER_MATERIAL_PLAN_CHANGED');
  const planSHA256 = fingerprint(plan);
  for (const [requested, data] of [[plan.secretCreates[0], material.signer], [plan.secretCreates[1], material.database]]) {
    materialStep({resource: requested.name, journal, observe: () => readCreatedSecret(kube, requested, planSHA256), execute: () =>
      kube(['-n', namespace, 'create', '--validate=strict', '--field-manager=kodex-artifact-retention-issuer', '-f', '-', '-o', 'name'],
        JSON.stringify({apiVersion: 'v1', kind: 'Secret', type: 'Opaque', immutable: true,
          metadata: {name: requested.name, namespace, labels: {'app.kubernetes.io/part-of': 'kodex'},
            annotations: {[planAnnotation]: planSHA256}}, data})),
    });
  }
  const expected = plan.publicTrustAppend;
  materialStep({resource: expected.name, journal, observe: () => readTrust(kube, expected, planSHA256).uid, execute: () => {
    const {observed} = readTrust(kube, expected, planSHA256);
    const operations = [
      {op: 'test', path: '/metadata/uid', value: expected.uid},
      {op: 'test', path: '/metadata/resourceVersion', value: expected.resourceVersion},
      {op: 'add', path: '/data/' + trustKey, value: material.trustValue},
      observed.hasAnnotations
        ? {op: 'add', path: '/metadata/annotations/' + planAnnotation.replace('/', '~1'), value: planSHA256}
        : {op: 'add', path: '/metadata/annotations', value: {[planAnnotation]: planSHA256}},
    ];
    withPrivatePatchFile(operations, path => kube(['-n', namespace, 'patch', 'secret', trustName, '--type=json', '--patch-file', path, '-o', 'name']));
  }});
  return {status: 'MATERIAL_STAGED', planSHA256};
}

function materialStatus({plan, kube, journal}) {
  const planSHA256 = fingerprint(plan);
  const resources = plan.secretCreates.map(resource => [resource.name, readCreatedSecret(kube, resource, planSHA256)]);
  resources.push([plan.publicTrustAppend.name, readTrust(kube, plan.publicTrustAppend, planSHA256).uid]);
  requireValue(resources.every(([name, uid]) => !uid || journal.events.some(event => event.type === 'INTENT' && event.resource === name)),
    'ARTIFACT_RETENTION_ISSUER_UNEXPECTED_RESOURCE');
  for (const ack of journal.events.filter(event => event.type === 'ACK' && event.resource !== credentialResource)) {
    requireValue(resources.some(([name, uid]) => name === ack.resource && uid === ack.uid), 'ARTIFACT_RETENTION_ISSUER_RESOURCE_REPLACED');
  }
  return {status: resources.every(([, uid]) => uid) ? 'MATERIAL_STAGED' : 'MATERIAL_PENDING', planSHA256,
    verifiedResources: resources.filter(([, uid]) => uid).length};
}

function credentialEvents(journal) {
  const events = journal.events.filter(event => event.resource === credentialResource);
  const [intent, receipt] = events;
  requireValue(!intent || (intent.type === 'INTENT' && sha.test(intent.backendSHA256 ?? '') && events.length <= 2 &&
    (!receipt || receipt.type === 'UNKNOWN' || (receipt.type === 'ACK' && receipt.backendSHA256 === intent.backendSHA256 &&
      sha.test(receipt.verifierSHA256 ?? '')))), 'ARTIFACT_RETENTION_ISSUER_LOGIN_JOURNAL_REJECTED');
  return {intent, acknowledgement: receipt?.type === 'ACK' ? receipt : undefined};
}

function assertLoginReceipt(owner, state, intent, acknowledgement) {
  requireValue(fingerprint(owner.backend) === intent.backendSHA256 && state.configured && state.scram &&
    state.verifierSHA256 === acknowledgement.verifierSHA256, 'ARTIFACT_RETENTION_ISSUER_LOGIN_READBACK_REJECTED');
}

export function artifactRetentionIssuerMaterialStatus({plan, kube, journal}) {
  const staged = materialStatus({plan, kube, journal});
  const {intent, acknowledgement} = credentialEvents(journal);
  if (!intent) return staged;
  requireValue(staged.status === 'MATERIAL_STAGED', 'ARTIFACT_RETENTION_ISSUER_PROJECTIONS_NOT_READY');
  const owner = verifyIssuerRole(kube);
  const state = readLoginStatus(owner);
  if (!acknowledgement) {
    requireValue(fingerprint(owner.backend) === intent.backendSHA256, 'ARTIFACT_RETENTION_ISSUER_LOGIN_READBACK_REJECTED');
    return {...staged, status: 'LOGIN_RECONCILIATION_REQUIRED', configured: state.configured};
  }
  assertLoginReceipt(owner, state, intent, acknowledgement);
  return {...staged, status: 'LOGIN_INITIALIZED'};
}

// Для перехода authority заново проверяются сохранённые реквизиты и LOGIN
// именно в этой установке. Квитанция не содержит значений Secret либо verifier.
export function readArtifactRetentionIssuerReady({planPath, evidencePath, sourceSHA, target}, kube) {
  const plan = readAuthorityPrivateJSON(planPath);
  assertMaterialPlan(plan, sourceSHA, target.context, kube);
  requireValue(Object.entries(plan.target).every(([key, value]) => target[key] === value),
    'ARTIFACT_RETENTION_ISSUER_TARGET_CHANGED');
  const planSHA256 = fingerprint(plan);
  const journal = privateJournal(evidencePath, {planSHA256, schema: plan.schema}, {readOnly: true});
  try {
    requireValue(artifactRetentionIssuerMaterialStatus({plan, kube, journal}).status === 'LOGIN_INITIALIZED',
      'ARTIFACT_RETENTION_ISSUER_LOGIN_REQUIRED');
    return {schema: 'kodex.artifact-retention-issuer-ready/v1', sourceSHA, target: plan.target,
      planSHA256, evidenceSHA256: journal.bytesSHA256};
  } finally {
    journal.close();
  }
}

function readLoginStatus(owner) {
  const sql = readFileSync(new URL('./artifact-retention-issuer-login-readback.sql', import.meta.url), 'utf8');
  const observed = owner.database.read(sql, owner.pod, owner.backend);
  requireValue(observed && typeof observed.configured === 'boolean' && typeof observed.scram === 'boolean' &&
    (observed.configured ? sha.test(observed.verifierSHA256 ?? '') : !observed.scram && observed.verifierSHA256 === null),
    'ARTIFACT_RETENTION_ISSUER_LOGIN_READBACK_REJECTED');
  return observed;
}

// PostgreSQL psql выполняет \password на том же соединении после транзакционной
// проверки. В параметры процесса и вывод пароль не попадает.
export function activateArtifactRetentionIssuerLogin({plan, materialDirectory, kube, journal}) {
  assertMaterialPlan(plan, plan.sourceRevision, plan.target.context, kube);
  requireValue(materialStatus({plan, kube, journal}).status === 'MATERIAL_STAGED',
    'ARTIFACT_RETENTION_ISSUER_PROJECTIONS_NOT_READY');
  const material = readArtifactRetentionIssuerMaterial(materialDirectory);
  requireValue(fingerprint(material.metadata.sources) === plan.materialSourceSHA256,
    'ARTIFACT_RETENTION_ISSUER_MATERIAL_PLAN_CHANGED');
  const owner = verifyIssuerRole(kube);
  const {intent, acknowledgement} = credentialEvents(journal);
  const state = readLoginStatus(owner);
  if (acknowledgement) {
    assertLoginReceipt(owner, state, intent, acknowledgement);
    return {status: 'LOGIN_INITIALIZED', planSHA256: fingerprint(plan)};
  }
  requireValue(!intent && !state.configured, 'ARTIFACT_RETENTION_ISSUER_LOGIN_RECONCILIATION_REQUIRED');
  const {systemIdentifier, databaseOID} = validateAuthorityDatabaseIdentity(owner.backend);
  const guard = readFileSync(new URL('./artifact-retention-issuer-login-precondition.sql', import.meta.url), 'utf8')
    .replace('@SYSTEM_IDENTIFIER@', systemIdentifier)
    .replace('@DATABASE_OID@', databaseOID);
  journal.append({type: 'INTENT', resource: credentialResource, backendSHA256: fingerprint(owner.backend)});
  let output;
  try {
    const input = material.password + '\n' + material.password + '\n';
    output = kube([...authorityOwnerDatabaseArgs, '-c', guard, '-c', '\\password ' + role, '-c', "COMMIT; SELECT 'ISSUER_LOGIN_INITIALIZED';"], input);
  } catch {
    journal.append({type: 'UNKNOWN', resource: credentialResource});
    throw Error('ARTIFACT_RETENTION_ISSUER_LOGIN_RECONCILIATION_REQUIRED');
  }
  requireValue(output.trim() === 'ISSUER_LOGIN_INITIALIZED', 'ARTIFACT_RETENTION_ISSUER_LOGIN_RECONCILIATION_REQUIRED');
  const confirm = readLoginStatus(owner);
  requireValue(confirm.configured && confirm.scram, 'ARTIFACT_RETENTION_ISSUER_LOGIN_READBACK_REJECTED');
  journal.append({type: 'ACK', resource: credentialResource, backendSHA256: fingerprint(owner.backend),
    verifierSHA256: confirm.verifierSHA256});
  return {status: 'LOGIN_INITIALIZED', planSHA256: fingerprint(plan)};
}

function main(args) {
  const phase = args.shift();
  const byPhase = {
    plan: ['--cluster-uid','--namespace-uid','--material','--output'],
    apply: ['--plan','--evidence','--material','--approved-plan-sha'],
    status: ['--plan','--evidence'],
    preflight: ['--plan'],
    'activate-login': ['--plan','--evidence','--material','--approved-plan-sha'],
  };
  requireValue(Object.hasOwn(byPhase, phase ?? ''), 'ARTIFACT_RETENTION_ISSUER_PLAN_REQUIRED');
  const flags = ['--kubeconfig','--context',...byPhase[phase]];
  const opts = {};
  while (args.length) {
    const flag = args.shift();
    requireValue(flags.includes(flag) && !Object.hasOwn(opts, flag) && !!args[0] && !args[0].startsWith('--'),
      'ARTIFACT_RETENTION_ISSUER_PLAN_ARGUMENT_REJECTED');
    opts[flag] = args.shift();
  }
  requireValue(Object.keys(opts).length === flags.length, 'ARTIFACT_RETENTION_ISSUER_PLAN_ARGUMENT_REJECTED');
  for (const flag of ['--output','--plan','--evidence','--material'].filter(key => Object.hasOwn(opts,key))) {
    requireValue(opts[flag] === resolve(opts[flag]), 'ARTIFACT_RETENTION_ISSUER_PLAN_ARGUMENT_REJECTED');
  }
  const kubeconfig = catalogKubeconfig(opts['--kubeconfig']);
  const context = opts['--context'];
  requireValue(/^[a-zA-Z0-9_.:@/-]{1,160}$/.test(context), 'ARTIFACT_RETENTION_ISSUER_PLAN_ARGUMENT_REJECTED');
  const kube = (argv, input) => {
    try {
      return execFileSync('kubectl', ['--kubeconfig', kubeconfig, '--context', context, '--request-timeout=10s', ...argv], {
        input, encoding: 'utf8', timeout: 12000, maxBuffer: 1 << 20, stdio: ['pipe', 'pipe', 'pipe'],
      });
    } catch {
      throw Error('ARTIFACT_RETENTION_ISSUER_TARGET_OPERATION_UNCONFIRMED');
    }
  };
  const source = inspectSource(resolve(fileURLToPath(new URL('../../', import.meta.url))));
  if (phase === 'plan') {
    const target = {context, clusterUID: opts['--cluster-uid'], namespaceUID: opts['--namespace-uid']};
    const plan = planArtifactRetentionIssuerMaterial({target, sourceRevision: source.revision, material: inspectArtifactRetentionIssuerMaterial(opts['--material']), kube});
    writeAuthorityPrivatePlan(opts['--output'], plan);
    console.log(JSON.stringify({status: 'PLANNED', sourceRevision: plan.sourceRevision, planSHA256: fingerprint(plan)}));
    return;
  }
  const plan = readAuthorityPrivateJSON(opts['--plan']);
  assertMaterialPlan(plan, source.revision, context, kube);
  if (phase === 'preflight') {
    verifyIssuerRole(kube);
    console.log(JSON.stringify({status: 'ISSUER_ROLE_READY', planSHA256: fingerprint(plan)}));
    return;
  }
  if (phase === 'apply' || phase === 'activate-login') requireValue(opts['--approved-plan-sha'] === fingerprint(plan), 'ARTIFACT_RETENTION_ISSUER_EXACT_PLAN_REQUIRED');
  const journal = privateJournal(opts['--evidence'], {planSHA256: fingerprint(plan), schema: plan.schema}, {readOnly: phase === 'status'});
  try {
    const request = {plan, materialDirectory: opts['--material'], kube, journal};
    const result = phase === 'status' ? artifactRetentionIssuerMaterialStatus(request)
      : phase === 'activate-login' ? activateArtifactRetentionIssuerLogin(request)
      : applyArtifactRetentionIssuerMaterial(request);
    console.log(JSON.stringify(result));
  } finally {
    journal.close();
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(process.argv.slice(2)); }
  catch (error) {
    console.error(/^ARTIFACT_RETENTION_[A-Z0-9_]+$/.test(error.message) ? error.message : 'ARTIFACT_RETENTION_ISSUER_MATERIAL_REJECTED');
    process.exitCode = 1;
  }
}
