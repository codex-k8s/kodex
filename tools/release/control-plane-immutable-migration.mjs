import { validateStableReceipt } from './stable-image-publication.mjs';
import { fingerprint } from './scoped-release.mjs';
import { createHash, randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, lstatSync, readFileSync, readdirSync, realpathSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { inspectSource } from './application-source.mjs';
import { privateJournal } from '../dev/role-image-acceptance.mjs';
import { activeDeploymentPods, isProvenStoppedPod } from './stable-archive-transition.mjs';

const ensure = (ok, code) => { if (!ok) throw new Error(code); };
const namespace = 'kodex-system';
const migrationVersion = '20261025000100';
const migrationFile = `${migrationVersion}_outbox_unpublished_predecessor_index.sql`;
const annotation = 'kodex.dev/cp-outbox-index-migration-plan';
const caBundleSpec = {sources:[{secret:{name:'kodex-installation-ca',key:'tls.crt'}}],
  target:{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':namespace}},configMap:{key:'ca.pem'}}};
const dsnPath = '/var/run/secrets/kodex/control-plane/postgres-migration/dsn';
const volumes = [
  { name:'postgres-migration', secret:{secretName:'control-plane-postgres-migration',defaultMode:292} },
  { name:'postgres-ca', configMap:{name:'control-plane-postgresql-ca',defaultMode:288,items:[{key:'ca.pem',path:'ca.pem'}]} },
];
const mounts = [
  {name:'postgres-migration',mountPath:'/var/run/secrets/kodex/control-plane/postgres-migration',readOnly:true},
  {name:'postgres-ca',mountPath:'/var/run/config/kodex/control-plane/postgres',readOnly:true},
];

// Повторно связывает проверенный stable build с точным publication receipt, без registry writes.
export function immutableMigrationImage(source, evidence) {
  const { publicationPlan: plan, publicationEvidence: records } = evidence ?? {};
  const image = validateStableReceipt(plan?.receipt);
  ensure(plan.schema === 'kodex.stable-image-publication/v2' && plan.artifactProfile === 'built' &&
    image.component === 'control-plane' && image.sourceSHA === source.revision &&
    fingerprint(image) === fingerprint(plan.image) && plan.archiveSHA256 === plan.receipt.archiveSHA256,
  'MIGRATION_IMAGE_PROVENANCE_INVALID');
  ensure(Array.isArray(records) && records.length === 2 &&
    records[0].schema === 'kodex.stable-image-publication-attempt/v1' && records[0].status === 'INTENT' &&
    ['apply','readback'].includes(records[0].mode) && records[0].planSHA256 === fingerprint(plan) &&
    records[0].component === image.component && records[0].digest === image.digest &&
    records[1].status === 'PASS' && records[1].phase === 'REGISTRY_PUBLICATION_ONLY' && records[1].nodePull === 'NOT_RUN',
  'MIGRATION_IMAGE_PUBLICATION_UNCONFIRMED');
  return `registry.local.kodex/kodex/control-plane@${image.digest}`;
}

// Штатный render задаёт только существующую migrator boundary; executable spec строится закрыто заново.
export function planImmutableControlPlaneMigration(before, source, inspect, id, evidence) {
  ensure(before?.kind === 'Job' && before.apiVersion === 'batch/v1' && before.metadata?.name === 'control-plane-migrate' &&
    before.metadata.namespace === namespace && before.metadata.uid && before.metadata.resourceVersion &&
    before.metadata.labels?.['app.kubernetes.io/name'] === 'control-plane' &&
    before.metadata.labels?.['app.kubernetes.io/component'] === 'migration' &&
    [undefined,'kodex'].includes(before.metadata.labels?.['app.kubernetes.io/part-of']) &&
    before.metadata.labels?.['kodex.dev/environment'] === 'staging', 'MIGRATOR_IDENTITY_INVALID');
  const pod = before.spec?.template?.spec, container = pod?.containers?.[0];
  // YAML 1.1 Kubernetes и YAML 1.2 yq по-разному переводят 0440 в JSON. Права из render
  // не переносятся: новый Job всегда назначает собственные явные десятичные modes.
  const refsOnly = values => (values ?? []).map(value => {
    const copy = structuredClone(value);
    if (copy.secret) delete copy.secret.defaultMode;
    if (copy.configMap) delete copy.configMap.defaultMode;
    return copy;
  });
  ensure(pod?.serviceAccountName === 'control-plane-migrator' && pod.automountServiceAccountToken === false &&
    pod.restartPolicy === 'Never' && pod.containers.length === 1 && container?.name === 'migrate' &&
    fingerprint(container.command) === fingerprint(['/usr/local/bin/control-plane-cli']) &&
    fingerprint(container.args) === fingerprint(['up']) &&
    fingerprint(container.env) === fingerprint([{name:'CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE',value:dsnPath}]) &&
    !container.envFrom && fingerprint(container.volumeMounts) === fingerprint(mounts) &&
    fingerprint(refsOnly(pod.volumes)) === fingerprint(refsOnly(volumes)) &&
    !pod.hostNetwork && !pod.hostPID && !pod.hostIPC &&
    [...(pod.initContainers ?? []),...pod.containers].every(c => !(c.volumeMounts ?? []).some(m => !mounts.some(v => v.name === m.name))),
  'IMMUTABLE_MIGRATOR_BOUNDARY_INVALID');
  ensure(/^[a-f0-9]{40}$/.test(source?.revision ?? '') && source.revision !== '0'.repeat(40) &&
    inspect(source.path).revision === source.revision, 'SOURCE_REVISION_MISMATCH');
  const image = immutableMigrationImage(source, evidence);
  // Staging overlay не назначает app part-of исходному render. Новая собственная Job
  // получает фиксированную метку; actual CP/SA/Secret проверяются независимо от render.
  const labels = { ...before.metadata.labels, 'app.kubernetes.io/part-of':'kodex' };
  const job = {apiVersion:'batch/v1',kind:'Job',metadata:{name:`control-plane-migrate-${migrationVersion}`,namespace,labels},spec:{
    backoffLimit:0,activeDeadlineSeconds:300,ttlSecondsAfterFinished:86400,
    template:{metadata:{labels,annotations:{'kodex.dev/source-revision':source.revision,
      'kodex.dev/image-context-sha256':evidence.publicationPlan.receipt.contextSHA256}},spec:{
      serviceAccountName:'control-plane-migrator',automountServiceAccountToken:false,restartPolicy:'Never',enableServiceLinks:false,
      securityContext:{runAsNonRoot:true,runAsUser:10001,runAsGroup:10001,fsGroup:10001,fsGroupChangePolicy:'OnRootMismatch',seccompProfile:{type:'RuntimeDefault'}},
      containers:[{name:'migrate',image,imagePullPolicy:'Always',command:['/usr/local/bin/control-plane-cli'],args:['up'],
        env:[{name:'CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE',value:dsnPath}],
        securityContext:{runAsNonRoot:true,runAsUser:10001,runAsGroup:10001,readOnlyRootFilesystem:true,allowPrivilegeEscalation:false,capabilities:{drop:['ALL']}},
        resources:{requests:{cpu:'25m',memory:'32Mi'},limits:{cpu:'250m',memory:'128Mi'}},volumeMounts:structuredClone(mounts)}],volumes:structuredClone(volumes)}}}};
  return {version:1,kind:'CONTROL_PLANE_MIGRATION',id,source,profile:'outbox-index',migrationVersion,
    beforeUID:before.metadata.uid,beforeResourceVersion:before.metadata.resourceVersion,beforeSpecSHA256:fingerprint(before.spec),
    imageEvidence:structuredClone(evidence),job};
}

// Terminal readback требует один exact Job-owned Pod и фактический imageID, не только logs/Complete.
export function immutableMigrationExecution(job, plan, pods) {
  ensure(job.spec.backoffLimit === 0 && job.spec.activeDeadlineSeconds === 300 &&
    job.spec.ttlSecondsAfterFinished === 86400 && !job.spec.suspend && !job.spec.podFailurePolicy && !job.spec.successPolicy &&
    (job.spec.completionMode ?? 'NonIndexed') === 'NonIndexed' &&
    (job.spec.parallelism ?? 1) === 1 && (job.spec.completions ?? 1) === 1, 'MIGRATION_JOB_LIFECYCLE_CHANGED');
  const matching = pods.filter(p => p.metadata?.namespace === namespace &&
    p.metadata?.ownerReferences?.some(o => o.controller === true && o.kind === 'Job' && o.uid === job.metadata.uid));
  ensure(matching.length <= 1 && matching.length === pods.length, 'MIGRATION_EXECUTION_SCOPE_CHANGED');
  if (matching.length !== 1) return false;
  const pod = matching[0], state = pod.status?.containerStatuses?.[0], expected = plan.job.spec.template.spec;
  const digest = plan.imageEvidence.publicationPlan.image.digest;
  ensure(pod.metadata.uid && !pod.spec?.initContainers?.length && !pod.spec?.ephemeralContainers?.length &&
    !pod.spec?.hostNetwork && !pod.spec?.hostPID && !pod.spec?.hostIPC &&
    ['serviceAccountName','automountServiceAccountToken','restartPolicy','enableServiceLinks','securityContext','volumes']
      .every(key => fingerprint(pod.spec?.[key]) === fingerprint(expected[key])) &&
    pod.spec?.containers?.length === 1 &&
    Object.keys(expected.containers[0]).every(key => fingerprint(pod.spec.containers[0][key]) === fingerprint(expected.containers[0][key])) &&
    !pod.spec.containers[0].envFrom && (pod.status?.containerStatuses?.length ?? 0) <= 1,
  'MIGRATION_EXECUTION_CHANGED');
  if (!state) return false;
  ensure(state.name === 'migrate' && (state.restartCount ?? 0) === 0, 'MIGRATION_EXECUTION_CHANGED');
  return pod.status.phase === 'Succeeded' && state.state?.terminated?.exitCode === 0 &&
    (state.imageID === digest || state.imageID === `containerd://${digest}` || state.imageID?.endsWith(`@${digest}`));
}

// Ожидание не повторяет effects и получает независимый конечный бюджет после каждой мутации.
export async function waitMigrationMaintenance(observe, ready, code, {now = Date.now, sleep = ms => new Promise(resolve => setTimeout(resolve,ms)), budget = 120000} = {}) {
  const deadline = now() + budget;
  for (;;) {
    const actual = observe();
    if (ready(actual)) return actual;
    ensure(now() < deadline, code);
    await sleep(250);
  }
}

// Существующая CLI получает отдельный закрытый протокол только для immutable outbox-index Job.
export async function runImmutableControlPlaneMigration(phase, options, io) {
  const { kube, clusterUID, privateJSON, writePrivate, renderedBase, migrationReadback } = io;
  const context = options['--context'];
  ensure(context === 'k3d-kodex', 'IMMUTABLE_MIGRATION_CONTEXT_INVALID');
  const get = (kind, name) => JSON.parse(kube(['-n',namespace,'get',kind,name,'-o','json']));
  const optionalJob = name => {
    const raw = kube(['-n',namespace,'get','job',name,'--ignore-not-found=true','-o','json']);
    return raw.trim() ? JSON.parse(raw) : undefined;
  };
  // Даже Secret запрашивается только узкой metadata projection: data/stringData не передаются процессу.
  const metadata = (kind, name) => {
    if (kind === 'configmap' && name === 'control-plane-postgresql-ca') {
      const raw = kube(['-n',namespace,'get',kind,name,'-o',
        'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.metadata.labels.trust\\.cert-manager\\.io/bundle}{"|"}{.metadata.ownerReferences}']);
      const [uid,resourceVersion,bundleName,ownersJSON,...extra] = raw.trim().split('|');
      const owners = JSON.parse(ownersJSON);
      ensure(uid && /^\d+$/.test(resourceVersion ?? '') && !extra.length && bundleName === name &&
        Array.isArray(owners) && owners.length === 1 && owners[0].apiVersion === 'trust.cert-manager.io/v1alpha1' &&
        owners[0].kind === 'Bundle' && owners[0].name === name && owners[0].controller === true && owners[0].uid,
      'MIGRATOR_CA_OWNERSHIP_INVALID');
      const [bundleUID,bundleVersion,bundleSpecJSON,...rest] = kube(['get','bundles.trust.cert-manager.io',name,'-o',
        'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.spec}']).trim().split('|');
      ensure(bundleUID === owners[0].uid && /^\d+$/.test(bundleVersion ?? '') && !rest.length &&
        fingerprint(JSON.parse(bundleSpecJSON)) === fingerprint(caBundleSpec), 'MIGRATOR_CA_BUNDLE_SOURCE_INVALID');
      return {kind,name,uid,resourceVersion,bundle:{uid:bundleUID,resourceVersion:bundleVersion,specSHA256:fingerprint(caBundleSpec)}};
    }
    const raw = kube(['-n',namespace,'get',kind,name,'-o',
      'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.metadata.labels.app\\.kubernetes\\.io/part-of}']);
    const [uid, resourceVersion, owner, ...extra] = raw.trim().split('|');
    ensure(uid && /^\d+$/.test(resourceVersion ?? '') && owner === 'kodex' && !extra.length, 'MIGRATOR_DEPENDENCY_IDENTITY_INVALID');
    return {kind,name,uid,resourceVersion};
  };
  const namespaceUID = () => kube(['get','namespace',namespace,'-o','jsonpath={.metadata.uid}']).trim();
  const dependencies = () => [metadata('serviceaccount','control-plane-migrator'),
    metadata('secret','control-plane-postgres-migration'),metadata('configmap','control-plane-postgresql-ca')];
  const deployment = () => {
    const value = get('deployment','control-plane');
    ensure(value.kind === 'Deployment' && value.metadata?.name === 'control-plane' && value.metadata.namespace === namespace &&
      value.metadata.uid && value.metadata.resourceVersion && value.metadata.labels?.['app.kubernetes.io/name'] === 'control-plane' &&
      value.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' && value.metadata.labels?.['kodex.dev/environment'] === 'staging',
    'MIGRATION_ANCHOR_IDENTITY_INVALID');
    return value;
  };
  const before = (render, anchor) => ({...render, metadata:{...render.metadata,uid:anchor.metadata.uid,resourceVersion:anchor.metadata.resourceVersion}});
  const sourceDigest = source => {
    ensure(inspectSource(source.path).revision === source.revision, 'SOURCE_REVISION_MISMATCH');
    const bundle = JSON.parse(execFileSync('yq',['-o=json',
      'select(.kind == "Bundle" and .metadata.name == "control-plane-postgresql-ca") | .spec',
      `${source.path}/deploy/k8s/base/platform-state/ca-bundles.yaml`],
    {encoding:'utf8',timeout:5000,maxBuffer:262144,stdio:['ignore','pipe','pipe']}));
    ensure(fingerprint(bundle) === fingerprint(caBundleSpec), 'MIGRATOR_CA_CANONICAL_SOURCE_INVALID');
    const directory = `${source.path}/services/internal/control-plane/cmd/cli/migrations`;
    const names = readdirSync(directory).filter(name => /^\d{14}_.+\.sql$/.test(name)).sort();
    ensure(names.at(-1) === migrationFile, 'MIGRATION_NOT_SOURCE_HEAD');
    return createHash('sha256').update(readFileSync(`${directory}/${migrationFile}`)).digest('hex');
  };
  const imageScope = (evidence, nsUID) => {
    ensure(evidence.publicationPlan.context === context && evidence.publicationPlan.clusterUID === clusterUID &&
      evidence.publicationPlan.namespaceUID === nsUID, 'MIGRATION_PUBLICATION_SCOPE_MISMATCH');
  };
  const readPublication = path => {
    path = resolve(path); const stat = lstatSync(path);
    ensure(realpathSync(path) === path && realpathSync(dirname(path)) === dirname(path) &&
      (lstatSync(dirname(path)).mode & 0o077) === 0 && stat.isFile() && stat.uid === process.getuid() &&
      stat.nlink === 1 && (stat.mode & 0o077) === 0 && stat.size < (4 << 20), 'PRIVATE_INPUT_INVALID');
    const raw = readFileSync(path,'utf8');
    ensure(raw.endsWith('\n'), 'PUBLICATION_EVIDENCE_INVALID');
    return raw.trimEnd().split('\n').map(line => JSON.parse(line));
  };
  if (phase === 'plan') {
    const anchor = deployment(), render = renderedBase(options['--base-render']);
    const source = {path:options['--source'],revision:options['--revision']};
    const imageEvidence = {publicationPlan:privateJSON(options['--publication-plan']),publicationEvidence:readPublication(options['--publication-evidence'])};
    const nsUID = namespaceUID(); ensure(nsUID, 'MIGRATION_NAMESPACE_INVALID'); imageScope(imageEvidence,nsUID);
    const plan = planImmutableControlPlaneMigration(before(render,anchor),source,inspectSource,randomUUID(),imageEvidence);
    const previousImage = anchor.spec?.template?.spec?.containers?.find(container => container.name === 'control-plane')?.image;
    ensure(anchor.spec.replicas === 2 && !anchor.spec.paused && anchor.metadata.labels['kodex.dev/local-profile'] === 'stable' &&
      anchor.status?.observedGeneration >= anchor.metadata.generation &&
      ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(key => anchor.status[key] === 2) &&
      /^registry\.local\.kodex\/kodex\/control-plane@sha256:[a-f0-9]{64}$/.test(previousImage ?? '') &&
      !previousImage.endsWith('0'.repeat(64)) && !anchor.spec.template.spec.volumes?.some(volume => volume.hostPath), 'MIGRATION_CP_IMMUTABLE_HEALTH_REQUIRED');
    ensure(!anchor.metadata.annotations?.[annotation], 'MIGRATION_ANCHOR_ALREADY_RESERVED');
    Object.assign(plan,{clusterUID,context,namespaceUID:nsUID,dependencies:dependencies(),
      anchor:{kind:'Deployment',name:'control-plane',uid:anchor.metadata.uid,resourceVersion:anchor.metadata.resourceVersion,
        specSHA256:fingerprint(anchor.spec),stoppedSpecSHA256:fingerprint({...anchor.spec,replicas:0}),previousImage},
      baseRenderPath:resolve(options['--base-render']),baseRenderSHA256:fingerprint(render),migrationFileSHA256:sourceDigest(source)});
    writePrivate(options['--plan'],plan);
    process.stdout.write(`${JSON.stringify({status:'PLANNED',jobName:plan.job.metadata.name,sourceRevision:source.revision,planSHA256:fingerprint(plan)})}\n`);
    return;
  }
  const plan = privateJSON(options['--plan']);
  ensure(plan.version === 1 && plan.kind === 'CONTROL_PLANE_MIGRATION' && plan.profile === 'outbox-index' &&
    plan.migrationVersion === migrationVersion && plan.context === context && plan.clusterUID === clusterUID &&
    plan.namespaceUID === namespaceUID() && plan.anchor?.kind === 'Deployment' && plan.anchor.name === 'control-plane', 'PLAN_SCOPE_MISMATCH');
  imageScope(plan.imageEvidence,plan.namespaceUID);
  const planSHA256 = fingerprint(plan);
  // После SIGKILL lock сохраняется. Inspect всё равно доступен без записи/снятия lock;
  // mutations закрыты до ручной проверки завершения прежнего процесса владельцем.
  const readOnly = phase === 'inspect' && existsSync(`${resolve(options['--evidence'])}.lock`);
  const durable = privateJournal(options['--evidence'], {version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256}, {readOnly});
  const journal = readOnly ? {events:durable.events,append:()=>{},close:()=>durable.close()} : durable;
  try {
    const attempts = journal.events.filter(e => e.type === 'CREATE_INTENT');
    ensure(attempts.length <= 1, 'MIGRATION_JOURNAL_INVALID');
    const output = result => {
      if (['FAIL','READBACK_UNCONFIRMED'].includes(result.status)) process.exitCode = 1;
      process.stdout.write(`${JSON.stringify({...result,...(readOnly ? {evidenceReadOnly:true} : {})})}\n`); return result;
    };
    const observeCP = () => {
      const actual = deployment(), marker = actual.metadata.annotations?.[annotation], hash = fingerprint(actual.spec);
      ensure(actual.metadata.uid === plan.anchor.uid && (!marker || marker === planSHA256), 'MIGRATION_ANCHOR_CHANGED');
      const resumed = journal.events.some(e => e.type === 'RESUME_INTENT');
      let stage;
      if (hash === plan.anchor.stoppedSpecSHA256 && marker === planSHA256) stage = 'STOPPED';
      else if (hash === plan.anchor.specSHA256 && !marker && !resumed) stage = 'BEFORE';
      else if (hash === plan.anchor.specSHA256 && marker === planSHA256 && resumed) stage = 'RESUMED';
      ensure(stage, 'MIGRATION_CP_SPEC_CHANGED');
      const list = JSON.parse(kube(['-n',namespace,'get','replicasets,pods','-l',
        'app.kubernetes.io/name=control-plane,app.kubernetes.io/component=internal-service','-o','json']));
      const pods = activeDeploymentPods(list,plan.anchor.uid);
      const joined = actual.status?.observedGeneration >= actual.metadata.generation && !(actual.status.replicas ?? 0) && pods.length === 0;
      const digest = plan.anchor.previousImage.split('@').at(-1);
      const ready = actual.status?.observedGeneration >= actual.metadata.generation &&
        ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(key => actual.status[key] === 2) && pods.length === 2 &&
        pods.every(p => !p.metadata.deletionTimestamp && p.status?.conditions?.some(c => c.type === 'Ready' && c.status === 'True') &&
          p.spec.containers?.some(c => c.name === 'control-plane' && c.image === plan.anchor.previousImage) &&
          p.status.containerStatuses?.some(c => c.name === 'control-plane' && c.ready && c.imageID?.endsWith(digest)));
      return {actual,stage,joined,ready};
    };
    const migrationPods = () => JSON.parse(kube(['-n',namespace,'get','pods','-l',`batch.kubernetes.io/job-name=${plan.job.metadata.name}`,'-o','json'])).items;
    const readJob = (terminalOnly = false) => {
      const actual = optionalJob(plan.job.metadata.name);
      ensure(actual, 'MIGRATION_JOB_MISSING_AFTER_INTENT');
      const observed = journal.events.find(e => ['CREATE_ACK','READBACK','TERMINAL_READBACK'].includes(e.type) && e.jobUID);
      ensure(!observed || observed.jobUID === actual.metadata.uid, 'MIGRATION_JOB_REPLACED');
      const pods = migrationPods();
      // Resume сохраняет прежний бинарник даже после failed/unconfirmed DDL, но лишь после
      // авторитетного terminal и доказанного прекращения всех Job-owned процессов.
      if (terminalOnly) {
        ensure(actual.metadata.uid && actual.metadata.annotations?.[annotation] === planSHA256 &&
          fingerprint(io.normalizePodSpec(actual.spec.template.spec)) === fingerprint(io.normalizePodSpec(plan.job.spec.template.spec)) &&
          actual.status?.conditions?.filter(c => ['Complete','Failed'].includes(c.type) && c.status === 'True').length === 1 &&
          !actual.status.active && pods.every(p => p.metadata?.uid && p.metadata.namespace === namespace &&
            p.metadata.ownerReferences?.some(o => o.kind === 'Job' && o.controller === true && o.uid === actual.metadata.uid) && isProvenStoppedPod(p)),
        'MIGRATION_TERMINAL_JOIN_REQUIRED');
        journal.append({type:'TERMINAL_READBACK',jobUID:actual.metadata.uid}); return;
      }
      ensure(fingerprint(dependencies()) === fingerprint(plan.dependencies), 'MIGRATION_DEPENDENCY_CHANGED');
      const logs = actual.status?.succeeded === 1 ? kube(['-n',namespace,'logs',`job/${plan.job.metadata.name}`,'-c','migrate','--limit-bytes=262144']) : '';
      const result = migrationReadback(actual,plan,logs,pods);
      journal.append({type:'READBACK',...result}); return result;
    };
    const noActiveMigrations = () => {
      const jobs = JSON.parse(kube(['-n',namespace,'get','jobs','-l','app.kubernetes.io/name=control-plane,app.kubernetes.io/component=migration','-o','json'])).items;
      ensure(jobs.every(job => job.status?.conditions?.some(c => ['Complete','Failed'].includes(c.type) && c.status === 'True')), 'MIGRATION_ALREADY_RUNNING');
    };
    const verifyInputs = anchor => {
      const render = renderedBase(plan.baseRenderPath);
      const rebuilt = planImmutableControlPlaneMigration(before(render,anchor),plan.source,inspectSource,plan.id,plan.imageEvidence);
      ensure(fingerprint(render) === plan.baseRenderSHA256 && fingerprint(rebuilt.job) === fingerprint(plan.job) &&
        sourceDigest(plan.source) === plan.migrationFileSHA256 && fingerprint(dependencies()) === fingerprint(plan.dependencies), 'MIGRATION_PLAN_DRIFT');
    };
    const transition = (step, state, replicas) => {
      const type = `${step}_INTENT`, last = journal.events.findLastIndex(e => e.type === type);
      if (last >= 0) ensure(journal.events.findLastIndex(e => e.type === 'INSPECT' && e.stage === state.stage &&
        e.resourceVersion === state.actual.metadata.resourceVersion) > last, 'MIGRATION_CP_PATCH_UNKNOWN_INSPECT_REQUIRED');
      const patch = [{op:'test',path:'/metadata/uid',value:plan.anchor.uid},
        {op:'test',path:'/metadata/resourceVersion',value:state.actual.metadata.resourceVersion},
        {op:'test',path:'/spec/replicas',value:state.actual.spec.replicas},
        {op:'replace',path:'/spec/replicas',value:replicas},
        {op:'add',path:'/metadata/annotations',value:{...(state.actual.metadata.annotations ?? {}),[annotation]:planSHA256}}];
      journal.append({type,resourceVersion:state.actual.metadata.resourceVersion});
      try { kube(['-n',namespace,'patch','deployment','control-plane','--type=json','-p',JSON.stringify(patch),'-o','json']); }
      catch(error) { journal.append({type:'UNKNOWN',step,code:error.message}); throw error; }
    };
    if (phase === 'inspect') {
      const state = observeCP();
      journal.append({type:'INSPECT',stage:state.stage,resourceVersion:state.actual.metadata.resourceVersion,podsGone:state.joined,ready:state.ready});
      if (!attempts.length) ensure(!optionalJob(plan.job.metadata.name), 'MIGRATION_JOB_WITHOUT_INTENT');
      return output({...(attempts.length ? readJob() : {status:state.stage === 'STOPPED' ? 'STOPPED_NOT_CREATED' : 'CREATE_NOT_ATTEMPTED'}),
        cpStage:state.stage,podsGone:state.joined,cpReady:state.ready});
    }
    if (phase === 'stop') {
      let state = observeCP();
      ensure(!journal.events.some(e => e.type === 'RESUME_INTENT') && !attempts.length, 'MIGRATION_STOP_LIFECYCLE_CLOSED');
      if (state.stage === 'BEFORE') {
        ensure(state.actual.metadata.resourceVersion === plan.anchor.resourceVersion, 'MIGRATION_PLAN_DRIFT');
        verifyInputs(state.actual);noActiveMigrations();ensure(!optionalJob(plan.job.metadata.name), 'MIGRATION_JOB_ALREADY_EXISTS');
        transition('STOP',state,0);
      } else ensure(journal.events.some(e => e.type === 'STOP_INTENT'), 'MIGRATION_UNOWNED_STOP');
      state = await waitMigrationMaintenance(observeCP,s => s.stage === 'STOPPED' && s.joined,'MIGRATION_CP_STOP_TIMEOUT',io.clock);
      journal.append({type:'STOP_ACK',resourceVersion:state.actual.metadata.resourceVersion});
      return output({status:'STOPPED',jobName:plan.job.metadata.name,cpStage:state.stage,podsGone:true});
    }
    if (phase === 'resume') {
      let state = observeCP();
      if (state.stage === 'BEFORE') {
        ensure(!journal.events.some(e => e.type === 'STOP_INTENT'), 'MIGRATION_STOP_OUTCOME_UNKNOWN');
        return output({status:'UNCHANGED',cpStage:state.stage});
      }
      if (state.stage === 'STOPPED') {
        ensure(journal.events.some(e => e.type === 'STOP_INTENT') && state.joined, 'MIGRATION_STOP_JOIN_REQUIRED');
        noActiveMigrations();
        if (attempts.length) readJob(true);
        else ensure(!optionalJob(plan.job.metadata.name) && migrationPods().length === 0, 'MIGRATION_JOB_WITHOUT_INTENT');
        transition('RESUME',state,2);
      }
      state = await waitMigrationMaintenance(observeCP,s => s.stage === 'RESUMED' && s.ready,'MIGRATION_CP_RESUME_TIMEOUT',io.clock);
      journal.append({type:'RESUME_ACK',resourceVersion:state.actual.metadata.resourceVersion});
      return output({status:'RESUMED',cpStage:state.stage,cpReady:true});
    }
    if (!attempts.length) {
      ensure(!journal.events.some(e => e.type === 'RESUME_INTENT'), 'MIGRATION_LIFECYCLE_CLOSED');
      const state = observeCP();
      ensure(state.stage === 'STOPPED' && state.joined && journal.events.some(e => e.type === 'STOP_INTENT'), 'MIGRATION_STOP_JOIN_REQUIRED');
      ensure(journal.events.some(e => e.type === 'STOP_ACK' || (e.type === 'INSPECT' && e.stage === 'STOPPED' && e.podsGone)), 'MIGRATION_STOP_READBACK_REQUIRED');
      verifyInputs(state.actual);noActiveMigrations();
      ensure(!optionalJob('control-plane-migrate') && !optionalJob(plan.job.metadata.name), 'MIGRATION_JOB_ALREADY_EXISTS');
      const quiesced = observeCP();ensure(quiesced.stage === 'STOPPED' && quiesced.joined, 'MIGRATION_STOP_JOIN_REQUIRED');
      const job = structuredClone(plan.job); job.metadata.annotations = {[annotation]:planSHA256};
      journal.append({type:'CREATE_INTENT',jobName:job.metadata.name});
      try {
        const ack = JSON.parse(kube(['create','-f','-','-o','json'],JSON.stringify(job)));
        ensure(ack.metadata?.uid, 'MIGRATION_CREATE_UNCONFIRMED');
        journal.append({type:'CREATE_ACK',jobUID:ack.metadata.uid});
      } catch(error) { journal.append({type:'UNKNOWN',code:error.message}); throw error; }
    }
    const result = readJob();output(result);
    if (['FAIL','READBACK_UNCONFIRMED'].includes(result.status)) process.exitCode = 1;
  } finally { journal.close(); }
}
