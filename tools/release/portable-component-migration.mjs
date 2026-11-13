import { createHash } from 'node:crypto';
import { fingerprint } from './scoped-release.mjs';
import { portableRunnerTarget } from './portable-runner-target.mjs';

const requireValue = (ok, code) => { if (!ok) throw new Error(code); };
const fields = (value, keys) => value && typeof value === 'object' && !Array.isArray(value) &&
  Object.keys(value).sort().join(',') === [...keys].sort().join(',');
const sha = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
const revision = value => typeof value === 'string' && /^[a-f0-9]{40}$/.test(value) && value !== '0'.repeat(40);
const uid = value => typeof value === 'string' && /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const image = value => typeof value === 'string' && /^[a-z0-9][a-z0-9.-]*(?::[1-9][0-9]{0,4})?\/[a-z0-9][a-z0-9._/-]*@sha256:[a-f0-9]{64}$/.test(value) && !value.includes('/../');
export const componentMigrationSchema = 'kodex.portable-component-migration/v1';
const names = ['control-plane', 'runtime-controller'];

// Только installation profile: его producer deployment_profile.py использует
// sort_keys=True, separators=(",", ":"), ensure_ascii=True. Ключи допустимого
// stock profile фиксированы в ASCII; значения URL могут содержать Unicode.
function installationProfileFingerprint(profile) {
  const wire=JSON.stringify(profile,(_key,value) => value && typeof value === 'object' && !Array.isArray(value)
    ? Object.fromEntries(Object.keys(value).sort().map(key => [key,value[key]])) : value)
    .replace(/[\u007f-\uffff]/g,character => `\\u${character.charCodeAt(0).toString(16).padStart(4,'0')}`);
  return createHash('sha256').update(wire).digest('hex');
}

// Честный alternate receipt: stock component-lock bytes и выполненный CRI proof,
// а не синтезированная stable-publication INTENT/PASS история другого publisher.
export function componentMigrationPublication(value, source, profile = 'workspace-unbound-stop') {
  requireValue(fields(value, ['schema','profile','context','clusterUID','namespaceUID','baseSourceSHA','baseReleaseLockSHA256',
    'schemaHeadsBefore','schemaHeadsAfter','runtimeABI','runnerInput','registry','repositoryPrefix','nodeUIDs','components']) &&
    value.schema === componentMigrationSchema && value.profile === profile && profile === 'workspace-unbound-stop' &&
    typeof value.context === 'string' && /^[A-Za-z0-9_.:@/-]{1,160}$/.test(value.context) && uid(value.clusterUID) && uid(value.namespaceUID) &&
    revision(source?.revision) && revision(value.baseSourceSHA) && sha(value.baseReleaseLockSHA256) &&
    value.runtimeABI === 5 && value.runnerInput === 'v10' &&
    typeof value.registry === 'string' && /^[a-z0-9][a-z0-9.-]*(?::[1-9][0-9]{0,4})?$/.test(value.registry) &&
    typeof value.repositoryPrefix === 'string' && /^[a-z0-9][a-z0-9._/-]*$/.test(value.repositoryPrefix) && !value.repositoryPrefix.includes('..'), 'COMPONENT_MIGRATION_PUBLICATION_INVALID');
  const schemaKeys = ['control-plane','internal-rpc-authority','email-bridge'];
  requireValue(fields(value.schemaHeadsBefore,schemaKeys) && fields(value.schemaHeadsAfter,schemaKeys) &&
    schemaKeys.every(k => Number.isSafeInteger(value.schemaHeadsBefore[k]) && value.schemaHeadsBefore[k] > 0) &&
    value.schemaHeadsBefore['control-plane'] === 20261112000100 && value.schemaHeadsAfter['control-plane'] === 20261113000100 &&
    schemaKeys.filter(k => k !== 'control-plane').every(k => value.schemaHeadsAfter[k] === value.schemaHeadsBefore[k]), 'COMPONENT_MIGRATION_SCHEMA_INVALID');
  requireValue(Array.isArray(value.nodeUIDs) && value.nodeUIDs.length > 0 && value.nodeUIDs.length <= 64 &&
    value.nodeUIDs.every(uid) && new Set(value.nodeUIDs).size === value.nodeUIDs.length &&
    Array.isArray(value.components) && value.components.length === 2 &&
    value.components.map(c => c.component).join(',') === names.join(','), 'COMPONENT_MIGRATION_PAIR_INVALID');
  const components = value.components.map(part => {
    requireValue(fields(part,['component','expectedImageRef','componentLockRawBase64','componentLockSHA256','nodeProof']) &&
      image(part.expectedImageRef) && sha(part.componentLockSHA256) && typeof part.componentLockRawBase64 === 'string' &&
      part.componentLockRawBase64.length <= 90000, 'COMPONENT_MIGRATION_LOCK_INVALID');
    const raw = Buffer.from(part.componentLockRawBase64,'base64');
    requireValue(raw.length > 0 && raw.length <= 65536 && raw.toString('base64') === part.componentLockRawBase64 &&
      createHash('sha256').update(raw).digest('hex') === part.componentLockSHA256, 'COMPONENT_MIGRATION_LOCK_HASH_INVALID');
    let lock;
    try { lock = JSON.parse(raw.toString('utf8')); } catch { throw Error('COMPONENT_MIGRATION_LOCK_INVALID'); }
    // Штатный publisher сохраняет json.dumps(indent=2) + LF. Все допустимые
    // строки ASCII; roundtrip исключает duplicate keys без второго JSON parser.
    requireValue(raw.equals(Buffer.from(JSON.stringify(lock,null,2)+'\n')), 'COMPONENT_MIGRATION_LOCK_ENCODING_INVALID');
    requireValue(fields(lock,['schema','component','source_sha','base_application_source_sha','base_release_lock_sha256','image_ref','digest',
      'production_file_sha256','schema_heads','compatibility','build','qualification']) && lock.schema === 'kodex.component-image/v1' &&
      lock.component === part.component && lock.source_sha === source.revision && lock.base_application_source_sha === value.baseSourceSHA &&
      lock.base_release_lock_sha256 === value.baseReleaseLockSHA256 && fingerprint(lock.schema_heads) === fingerprint(value.schemaHeadsAfter) &&
      typeof lock.digest === 'string' && /^sha256:[a-f0-9]{64}$/.test(lock.digest) && lock.digest !== 'sha256:'+'0'.repeat(64) &&
      lock.image_ref === `${value.registry}/${value.repositoryPrefix}/${part.component}@${lock.digest}` && image(lock.image_ref) &&
      lock.image_ref !== part.expectedImageRef && lock.qualification === 'PUBLISHED_NOT_NODE_QUALIFIED_NOT_DEPLOYED' &&
      sha(lock.production_file_sha256) && fields(lock.build,['dockerfile_sha256','metadata_sha256','private_source_sha','platform']) &&
      sha(lock.build?.dockerfile_sha256) && sha(lock.build?.metadata_sha256) &&
      revision(lock.build?.private_source_sha) && lock.build.platform === 'linux/amd64', 'COMPONENT_MIGRATION_LOCK_PROVENANCE_INVALID');
    const compatibility = lock.compatibility;
    const manifest = values => Array.isArray(values) && values.length > 0 && values.length <= 4096 &&
      values.every(v => typeof v === 'string' && /^[A-Za-z0-9_./-]{1,512}$/.test(v) && !v.split('/').includes('..') &&
        ['contracts/','libs/go/',`services/internal/${part.component}/`].some(prefix => v.startsWith(prefix))) &&
      fingerprint(values) === fingerprint([...new Set(values)].sort());
    const cohort=compatibility?.verified_cohort;
    requireValue(fields(compatibility,['changed_files','production_files','shared_libs_contracts_unchanged','verified_cohort']) &&
      fields(cohort,['components','changed_files','production_files','anchor','contract_change','schema_change','runner_input_abi']) &&
      manifest(compatibility.changed_files) && manifest(compatibility.production_files) &&
      fingerprint(compatibility.changed_files) === fingerprint(cohort.changed_files) &&
      fingerprint(compatibility.production_files) === fingerprint(cohort.production_files) &&
      compatibility.production_files.every(v => compatibility.changed_files.includes(v)) &&
      compatibility.production_files.includes(cohort.anchor) && cohort.anchor.startsWith(`services/internal/${part.component}/`) &&
      compatibility?.shared_libs_contracts_unchanged === false &&
      compatibility.verified_cohort?.contract_change === 'WORKSPACE_UNBOUND_STOP_UPGRADE' &&
      compatibility.verified_cohort.schema_change === true && compatibility.verified_cohort.runner_input_abi === 'UNCHANGED' &&
      fingerprint(compatibility.verified_cohort.components) === fingerprint(names), 'COMPONENT_MIGRATION_COMPATIBILITY_INVALID');
    const proof = part.nodeProof;
    requireValue(fields(proof,['schema','component','sourceSHA','componentLockSHA256','imageRef','clusterUID','namespaceUID','nodes']) &&
      proof.schema === 'kodex.component-node-proof/v1' && proof.component === part.component && proof.sourceSHA === source.revision &&
      proof.componentLockSHA256 === part.componentLockSHA256 && proof.imageRef === lock.image_ref &&
      proof.clusterUID === value.clusterUID && proof.namespaceUID === value.namespaceUID && Array.isArray(proof.nodes) &&
      proof.nodes.length === value.nodeUIDs.length &&
      fingerprint(proof.nodes.map(n => n.uid).sort()) === fingerprint([...value.nodeUIDs].sort()) &&
      proof.nodes.every(n => fields(n,['name','uid','imageRef','repoDigestVerified']) &&
        typeof n.name === 'string' && /^[a-z0-9][a-z0-9.-]{0,252}$/.test(n.name) && n.imageRef === lock.image_ref && n.repoDigestVerified === true) &&
      new Set(proof.nodes.map(n => n.name)).size === proof.nodes.length, 'COMPONENT_MIGRATION_NODE_PROOF_INVALID');
    return {component:part.component,imageRef:lock.image_ref,digest:lock.digest,sourceSHA:lock.source_sha,
      expectedImageRef:part.expectedImageRef,lock,componentLockSHA256:part.componentLockSHA256};
  });
  return {digest:fingerprint(value),components,context:value.context,clusterUID:value.clusterUID,namespaceUID:value.namespaceUID};
}

// UID/installation seal разрешают переносимость context и registry, не подмену
// чужого Deployment и не расширение закрытого двухкомпонентного schema unit.
export function componentMigrationTarget(target, publication, observed) {
  const validated = componentMigrationPublication(publication,{revision:observed.sourceRevision});
  requireValue(validated.context === observed.context && validated.clusterUID === observed.clusterUID &&
    validated.namespaceUID === observed.namespaceUID, 'COMPONENT_MIGRATION_TARGET_MISMATCH');
  const scope = portableRunnerTarget(target,observed);
  const pins=observed.installation.data;
  requireValue(fields(pins,['profile.json','profile.sha256','source.sha','release-lock.sha256','schema-heads.json']) &&
    Object.values(pins).every(v => typeof v === 'string') && pins['source.sha'] === publication.baseSourceSHA &&
    pins['release-lock.sha256'] === publication.baseReleaseLockSHA256, 'COMPONENT_MIGRATION_INSTALLATION_PINS_MISMATCH');
  let heads,profile;
  try {heads=JSON.parse(pins['schema-heads.json']);profile=JSON.parse(pins['profile.json']);}
  catch {throw Error('COMPONENT_MIGRATION_INSTALLATION_PINS_MISMATCH');}
  requireValue(fingerprint(heads) === fingerprint(publication.schemaHeadsBefore) &&
    fields(profile,['version','identity','public_tls']) && profile.version === 1 &&
    installationProfileFingerprint(profile) === pins['profile.sha256'], 'COMPONENT_MIGRATION_INSTALLATION_PINS_MISMATCH');
  requireValue(target.resources.length === 3 && ['control-plane','runtime-controller','control-api-gateway'].every(name =>
    target.resources.some(r => r.apiVersion === 'apps/v1' && r.kind === 'Deployment' && r.name === name)) &&
    observed.deployments.length === 3 && observed.deployments.every(d => scope.allows(d)) &&
    fingerprint(observed.nodes.map(n => n.metadata.uid).sort()) === fingerprint([...publication.nodeUIDs].sort()) &&
    publication.components.every(c => c.nodeProof.nodes.every(n => observed.nodes.some(actual => actual.metadata.uid === n.uid && actual.metadata.name === n.name &&
      !actual.metadata.deletionTimestamp && actual.status?.conditions?.some(v => v.type === 'Ready' && v.status === 'True')))), 'COMPONENT_MIGRATION_TARGET_MISMATCH');
  const gateway = observed.deployments.find(d => d.metadata.name === 'control-api-gateway');
  requireValue(gateway.spec.replicas === 0, 'COMPONENT_MIGRATION_MAINTENANCE_REQUIRED');
  return {...validated,targetSHA256:scope.digest};
}

// Предыдущие env/volumes/sidecars остаются точными; только source/image пары
// изменяются между stopped template и возобновлением исходного числа реплик.
export function planComponentMigrationHandoff(publication, deployments, source) {
  const validated = componentMigrationPublication(publication,source);
  return {schema:'kodex.component-migration-handoff/v1',publicationSHA256:validated.digest,
    components:validated.components.map(part => {
      const matches = deployments.filter(d => d.kind === 'Deployment' && d.metadata.name === part.component && d.metadata.namespace === 'kodex-system');
      requireValue(matches.length === 1,'COMPONENT_MIGRATION_HANDOFF_SCOPE_INVALID');
      const before = matches[0], count = before.spec?.replicas;
      requireValue(uid(before.metadata.uid) && /^\d+$/.test(before.metadata.resourceVersion ?? '') &&
        Number.isSafeInteger(count) && count > 0 && count <= 8 && (part.component !== 'control-plane' || count === 3) && !before.spec.paused &&
        before.status?.observedGeneration >= before.metadata.generation &&
        ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k => before.status[k] === count), 'COMPONENT_MIGRATION_HANDOFF_HEALTH_REQUIRED');
      const stopped = structuredClone(before.spec);stopped.replicas=0;
      const staged = structuredClone(stopped);
      const mains = staged.template.spec.containers.filter(c => c.name === part.component);
      requireValue(mains.length === 1 && mains[0].image === part.expectedImageRef,'COMPONENT_MIGRATION_HANDOFF_PREDECESSOR_INVALID');
      mains[0].image=part.imageRef;
      const annotations = staged.template.metadata ??= {};
      const values = annotations.annotations ??= {};
      delete values['kodex.dev/component-image-profile-sha256'];
      Object.assign(values,{'kodex.dev/component-source-sha':source.revision,'kodex.dev/component-base-lock-sha256':publication.baseReleaseLockSHA256,
        'kodex.dev/component-upgrade-sha256':validated.digest});
      const resumed=structuredClone(staged);resumed.replicas=count;
      return {name:part.component,uid:before.metadata.uid,replicas:count,beforeSpec:structuredClone(before.spec),stoppedSpec:stopped,stagedSpec:staged,resumedSpec:resumed};
    })};
}

// Journal принадлежит исходному migration protocol. Пара не получает ни собственного
// DDL/retry loop, ни права оживить прежний бинарник после успешного forward043.
export function componentMigrationLifecycle({plan,kube,journal,readJob,observeCP,preconditionSQL,postgresIdentity,validateInputs,activePods,wait,clock}) {
  const namespace='kodex-system', names=['control-plane','runtime-controller'];
  const originals=plan.componentHandoff.components.map(c => ({kind:'Deployment',metadata:{name:c.name,namespace,uid:c.uid,resourceVersion:'1',generation:1},
    spec:c.beforeSpec,status:{observedGeneration:1,replicas:c.replicas,updatedReplicas:c.replicas,readyReplicas:c.replicas,availableReplicas:c.replicas}}));
  requireValue(fingerprint(planComponentMigrationHandoff(plan.imageEvidence,originals,plan.source)) === fingerprint(plan.componentHandoff), 'COMPONENT_MIGRATION_HANDOFF_PLAN_DRIFT');
  const part = name => plan.componentHandoff.components.find(c => c.name === name);
  const observe = name => {
    const expected=part(name), actual=JSON.parse(kube(['-n',namespace,'get','deployment',name,'-o','json']));
    requireValue(actual.metadata.uid === expected.uid,'COMPONENT_MIGRATION_HANDOFF_UID_CHANGED');
    const stage=['beforeSpec','stoppedSpec','stagedSpec','resumedSpec'].find(k => fingerprint(expected[k]) === fingerprint(actual.spec));
    requireValue(stage,'COMPONENT_MIGRATION_HANDOFF_SPEC_CHANGED');
    const list=JSON.parse(kube(['-n',namespace,'get','replicasets,pods','-l',`app.kubernetes.io/name=${name}`,'-o','json']));
    const pods=activePods(list,expected.uid);
    const joined=actual.status?.observedGeneration >= actual.metadata.generation && !(actual.status.replicas ?? 0) && pods.length === 0;
    const ready=actual.status?.observedGeneration >= actual.metadata.generation && actual.spec.replicas === expected.replicas &&
      ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k => actual.status[k] === expected.replicas) && pods.length === expected.replicas &&
      pods.every(p => !p.metadata.deletionTimestamp && p.status?.conditions?.some(c => c.type === 'Ready' && c.status === 'True') &&
        ['containers','initContainers'].every(field => (actual.spec.template.spec[field] ?? []).every(c => {
          const status=(p.status[field === 'containers' ? 'containerStatuses' : 'initContainerStatuses'] ?? []).find(v => v.name === c.name);
          return image(c.image) && status?.imageID?.endsWith(c.image.split('@').at(-1)) &&
            (field === 'containers' || c.restartPolicy === 'Always' ? status.ready === true : status.state?.terminated?.exitCode === 0);
        })));
    return {actual,stage,joined,ready};
  };
  const inspect = () => names.map(name => {
    const state=observe(name);
    journal.append({type:'PAIR_INSPECT',name,stage:state.stage,resourceVersion:state.actual.metadata.resourceVersion});
    return {name,stage:state.stage,joined:state.joined,ready:state.ready};
  });
  const transition = (name,stage,type) => {
    const state=observe(name), expected=part(name), wanted=expected[stage];
    const last=journal.events.findLastIndex(e => e.type === type && e.name === name);
    if (state.stage === stage) {
      requireValue(last >= 0,'COMPONENT_MIGRATION_UNOWNED_HANDOFF');return;
    }
    const previous=stage === 'stoppedSpec' ? 'beforeSpec' : stage === 'stagedSpec' ? 'stoppedSpec' : 'stagedSpec';
    requireValue(state.stage === previous,'COMPONENT_MIGRATION_HANDOFF_ORDER_INVALID');
    if (last >= 0) requireValue(journal.events.findLastIndex(e => e.type === 'PAIR_INSPECT' && e.name === name &&
      e.stage === state.stage && e.resourceVersion === state.actual.metadata.resourceVersion) > last,'COMPONENT_MIGRATION_UNKNOWN_INSPECT_REQUIRED');
    const patch=[{op:'test',path:'/metadata/uid',value:expected.uid},{op:'test',path:'/metadata/resourceVersion',value:state.actual.metadata.resourceVersion},
      {op:'test',path:'/spec',value:state.actual.spec},{op:'replace',path:'/spec',value:wanted}];
    const args=['-n',namespace,'patch','deployment',name,'--type=json','--patch-file=/dev/stdin'];
    kube([...args,'--dry-run=server'],JSON.stringify(patch));
    journal.append({type,name,stage,resourceVersion:state.actual.metadata.resourceVersion});
    try { kube(args,JSON.stringify(patch)); }
    catch(error) {journal.append({type:'UNKNOWN',step:type,name,code:error.message});throw error;}
    requireValue(observe(name).stage === stage,'COMPONENT_MIGRATION_HANDOFF_UNCONFIRMED');
  };
  const schemaReady = () => {
    readJob(true); // Полный terminal Job/Pod writer join перед schema postcondition.
    const result=readJob();
    requireValue(result.status === 'PASS' && result.schemaPostcondition === 'PASS','COMPONENT_MIGRATION_POSTCONDITION_REQUIRED');
    const sql=preconditionSQL(plan.source);
    requireValue(createHash('sha256').update(sql).digest('hex') === plan.preconditionSQLSHA256 &&
      fingerprint(postgresIdentity()) === fingerprint(plan.postgres),'MIGRATION_PRECONDITION_SOURCE_CHANGED');
    const proof=JSON.parse(kube(['-n',namespace,'exec','-i','kodex-postgresql-0','--','psql','-XqAt','-U','postgres','-d','control_plane','-v','ON_ERROR_STOP=1'],sql));
    requireValue(proof.database === 'control_plane' && proof.readOnly === 'on' && proof.gooseVersion === Number(plan.migrationVersion) &&
      ['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions',
        'pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks'].every(k => proof[k] === 0), 'MIGRATION_OWNER_PRECONDITION_FAILED');
    requireValue(fingerprint(postgresIdentity()) === fingerprint(plan.postgres),'MIGRATION_POSTGRES_IDENTITY_CHANGED');
    validateInputs();
  };
  return {
    inspect,
    requireStopped() {
      const state=observe('runtime-controller');
      requireValue(state.stage === 'stoppedSpec' && state.joined && journal.events.some(e => e.type === 'PAIR_STOP_ACK'),'COMPONENT_MIGRATION_CONTROLLER_JOIN_REQUIRED');
    },
    async stopController() {
      transition('runtime-controller','stoppedSpec','PAIR_STOP_INTENT');
      await wait(() => observe('runtime-controller'),s => s.stage === 'stoppedSpec' && s.joined,'COMPONENT_MIGRATION_CONTROLLER_STOP_TIMEOUT',clock);
      journal.append({type:'PAIR_STOP_ACK',name:'runtime-controller'});
    },
    async handoff() {
      schemaReady();
      const cp=observeCP();
      requireValue(['STOPPED','STAGED'].includes(cp.stage) && cp.joined && journal.events.some(e => e.type === 'STOP_ACK'),'MIGRATION_STOP_JOIN_REQUIRED');
      requireValue(observe('runtime-controller').joined && journal.events.some(e => e.type === 'PAIR_STOP_ACK'),'COMPONENT_MIGRATION_CONTROLLER_JOIN_REQUIRED');
      transition('runtime-controller','stagedSpec','PAIR_HANDOFF_INTENT');
      transition('control-plane','stagedSpec','PAIR_HANDOFF_INTENT');
      journal.append({type:'PAIR_HANDOFF_ACK',publicationSHA256:plan.componentHandoff.publicationSHA256});
      return {status:'STAGED',schemaPostcondition:'PASS',cpStage:observeCP().stage,forwardOnly:true};
    },
    async resume() {
      schemaReady();requireValue(journal.events.some(e => e.type === 'PAIR_HANDOFF_ACK'),'COMPONENT_MIGRATION_HANDOFF_REQUIRED');
      for (const name of names) {
        transition(name,'resumedSpec',name === 'control-plane' ? 'RESUME_INTENT' : 'PAIR_RESUME_INTENT');
        await wait(() => observe(name),s => s.stage === 'resumedSpec' && s.ready,'COMPONENT_MIGRATION_RESUME_TIMEOUT',clock);
        journal.append({type:name === 'control-plane' ? 'RESUME_ACK' : 'PAIR_RESUME_ACK',name});
      }
      return {status:'RESUMED',cpStage:'UPGRADED',schemaPostcondition:'PASS',pairReady:true,forwardOnly:true};
    },
  };
}
