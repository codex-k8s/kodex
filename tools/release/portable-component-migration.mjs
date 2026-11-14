import { createHash } from 'node:crypto';
import { fingerprint, withPrivatePatchFile } from './scoped-release.mjs';
import { portableRunnerTarget } from './portable-runner-target.mjs';
import { isProvenStoppedPod } from './stable-archive-transition.mjs';

const requireValue = (ok, code) => { if (!ok) throw new Error(code); };
const fields = (value, keys) => value && typeof value === 'object' && !Array.isArray(value) &&
  Object.keys(value).sort().join(',') === [...keys].sort().join(',');
const sha = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
const revision = value => typeof value === 'string' && /^[a-f0-9]{40}$/.test(value) && value !== '0'.repeat(40);
const uid = value => typeof value === 'string' && /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const image = value => typeof value === 'string' && /^[a-z0-9][a-z0-9.-]*(?::[1-9][0-9]{0,4})?\/[a-z0-9][a-z0-9._/-]*@sha256:[a-f0-9]{64}$/.test(value) && !value.includes('/../');
export const componentMigrationSchema = 'kodex.portable-component-migration/v1';
export const componentMigrationCohortSchema = 'kodex.portable-component-migration/v2';
const profiles = Object.freeze({
  'workspace-unbound-stop': Object.freeze({schema:componentMigrationSchema,before:20261112000100,after:20261113000100,
    contract:'WORKSPACE_UNBOUND_STOP_UPGRADE',components:Object.freeze(['control-plane','runtime-controller'])}),
  'github-repository-scope': Object.freeze({schema:componentMigrationCohortSchema,before:20261113000100,after:20261114120000,
    contract:'GITHUB_REPOSITORY_SCOPE_UPGRADE',components:Object.freeze(['control-plane','runtime-controller','integration-gateway','control-api-gateway','staff-control-center'])}),
  'github-repository-scope-backup': Object.freeze({schema:componentMigrationCohortSchema,before:20261113000100,after:20261114120000,
    contract:'GITHUB_REPOSITORY_SCOPE_UPGRADE',components:Object.freeze(['control-plane','runtime-controller','integration-gateway','control-api-gateway','staff-control-center'])}),
});
const componentSources = Object.freeze({'control-plane':'services/internal/control-plane/',
  'runtime-controller':'services/internal/runtime-controller/','integration-gateway':'services/external/integration-gateway/',
  'control-api-gateway':'services/external/control-api-gateway/','staff-control-center':'services/staff/control-center/'});
const names = Object.keys(componentSources);

// Реестр закрыт: имя schema или произвольный массив компонентов не добавляют профиль.
export const isComponentMigrationProfile = profile => Object.hasOwn(profiles,profile);
export const isComponentMigrationPublication = value => value?.schema === componentMigrationSchema || value?.schema === componentMigrationCohortSchema;
export function componentMigrationNames(profile) {
  requireValue(isComponentMigrationProfile(profile),'COMPONENT_MIGRATION_PROFILE_INVALID');
  return [...profiles[profile].components];
}
export function componentMigrationTargetNames(profile) {
  return [...new Set([...componentMigrationNames(profile),'control-api-gateway'])];
}

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
  requireValue(isComponentMigrationProfile(profile),'COMPONENT_MIGRATION_PROFILE_INVALID');
  const configuration=profiles[profile], componentsNames=configuration.components;
  const extended=configuration.schema === componentMigrationCohortSchema;
  requireValue(fields(value, ['schema','profile','context','clusterUID','namespaceUID','baseSourceSHA','baseReleaseLockSHA256',
    'schemaHeadsBefore','schemaHeadsAfter','runtimeABI','runnerInput','registry','repositoryPrefix','nodeUIDs','components',...(extended?['baseSchemaHeads']:[])]) &&
    value.schema === configuration.schema && value.profile === profile &&
    typeof value.context === 'string' && /^[A-Za-z0-9_.:@/-]{1,160}$/.test(value.context) && uid(value.clusterUID) && uid(value.namespaceUID) &&
    revision(source?.revision) && revision(value.baseSourceSHA) && sha(value.baseReleaseLockSHA256) &&
    value.runtimeABI === 5 && value.runnerInput === 'v10' &&
    typeof value.registry === 'string' && /^[a-z0-9][a-z0-9.-]*(?::[1-9][0-9]{0,4})?$/.test(value.registry) &&
    typeof value.repositoryPrefix === 'string' && /^[a-z0-9][a-z0-9._/-]*$/.test(value.repositoryPrefix) && !value.repositoryPrefix.includes('..'), 'COMPONENT_MIGRATION_PUBLICATION_INVALID');
  const schemaKeys = ['control-plane','internal-rpc-authority','email-bridge'];
  requireValue(fields(value.schemaHeadsBefore,schemaKeys) && fields(value.schemaHeadsAfter,schemaKeys) &&
    schemaKeys.every(k => Number.isSafeInteger(value.schemaHeadsBefore[k]) && value.schemaHeadsBefore[k] > 0) &&
    value.schemaHeadsBefore['control-plane'] === configuration.before && value.schemaHeadsAfter['control-plane'] === configuration.after &&
    schemaKeys.filter(k => k !== 'control-plane').every(k => value.schemaHeadsAfter[k] === value.schemaHeadsBefore[k]), 'COMPONENT_MIGRATION_SCHEMA_INVALID');
  // Seal описывает исходную полную установку. Текущий predecessor проверяется
  // отдельно полным PG catalog; после043 исходный seal042 не переписывается.
  if (extended) requireValue(fields(value.baseSchemaHeads,schemaKeys) &&
    [20261112000100,20261113000100].includes(value.baseSchemaHeads['control-plane']) &&
    schemaKeys.filter(k=>k!=='control-plane').every(k=>value.baseSchemaHeads[k]===value.schemaHeadsBefore[k]),'COMPONENT_MIGRATION_BASE_SCHEMA_INVALID');
  requireValue(Array.isArray(value.nodeUIDs) && value.nodeUIDs.length > 0 && value.nodeUIDs.length <= 64 &&
    value.nodeUIDs.every(uid) && new Set(value.nodeUIDs).size === value.nodeUIDs.length &&
    Array.isArray(value.components) && value.components.length === componentsNames.length &&
    value.components.map(c => c.component).join(',') === componentsNames.join(','), 'COMPONENT_MIGRATION_PAIR_INVALID');
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
        ['contracts/','libs/go/',componentSources[part.component]].some(prefix => v.startsWith(prefix))) &&
      fingerprint(values) === fingerprint([...new Set(values)].sort());
    const cohort=compatibility?.verified_cohort;
    requireValue(fields(compatibility,['changed_files','production_files','shared_libs_contracts_unchanged','verified_cohort']) &&
      fields(cohort,['components','changed_files','production_files','anchor','contract_change','schema_change','runner_input_abi']) &&
      manifest(compatibility.changed_files) && manifest(compatibility.production_files) &&
      fingerprint(compatibility.changed_files) === fingerprint(cohort.changed_files) &&
      fingerprint(compatibility.production_files) === fingerprint(cohort.production_files) &&
      compatibility.production_files.every(v => compatibility.changed_files.includes(v)) &&
      compatibility.production_files.includes(cohort.anchor) && cohort.anchor.startsWith(componentSources[part.component]) &&
      compatibility?.shared_libs_contracts_unchanged === false &&
      compatibility.verified_cohort?.contract_change === configuration.contract &&
      compatibility.verified_cohort.schema_change === true && compatibility.verified_cohort.runner_input_abi === 'UNCHANGED' &&
      fingerprint(compatibility.verified_cohort.components) === fingerprint(componentsNames), 'COMPONENT_MIGRATION_COMPATIBILITY_INVALID');
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
  const validated = componentMigrationPublication(publication,{revision:observed.sourceRevision},publication.profile);
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
  requireValue(fingerprint(heads) === fingerprint(publication.baseSchemaHeads ?? publication.schemaHeadsBefore) &&
    fields(profile,['version','identity','public_tls']) && profile.version === 1 &&
    installationProfileFingerprint(profile) === pins['profile.sha256'], 'COMPONENT_MIGRATION_INSTALLATION_PINS_MISMATCH');
  const targetNames=componentMigrationTargetNames(publication.profile);
  requireValue(target.resources.length === targetNames.length && targetNames.every(name =>
    target.resources.some(r => r.apiVersion === 'apps/v1' && r.kind === 'Deployment' && r.name === name)) &&
    observed.deployments.length === targetNames.length && new Set(observed.deployments.map(d=>d.metadata?.name)).size === targetNames.length &&
    observed.deployments.every(d => targetNames.includes(d.metadata?.name) && scope.allows(d)) &&
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
  const validated = componentMigrationPublication(publication,source,publication.profile);
  return {schema:'kodex.component-migration-handoff/v1',publicationSHA256:validated.digest,
    components:validated.components.map(part => {
      const matches = deployments.filter(d => d.kind === 'Deployment' && d.metadata.name === part.component && d.metadata.namespace === 'kodex-system');
      requireValue(matches.length === 1,'COMPONENT_MIGRATION_HANDOFF_SCOPE_INVALID');
      const before = matches[0], count = before.spec?.replicas;
      const maintenance=publication.schema === componentMigrationCohortSchema && part.component === 'control-api-gateway';
      requireValue(uid(before.metadata.uid) && /^\d+$/.test(before.metadata.resourceVersion ?? '') &&
        Number.isSafeInteger(count) && (maintenance ? count === 0 : count > 0 && count <= 8) && (part.component !== 'control-plane' || count === 3) && !before.spec.paused &&
        before.status?.observedGeneration >= before.metadata.generation &&
        ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k => (before.status[k] ?? (maintenance?0:undefined)) === count), 'COMPONENT_MIGRATION_HANDOFF_HEALTH_REQUIRED');
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
      return {name:part.component,uid:before.metadata.uid,replicas:count,...(maintenance?{maintenance:true}:{}),beforeSpec:structuredClone(before.spec),stoppedSpec:stopped,stagedSpec:staged,resumedSpec:resumed};
    })};
}

// Общий namespace inventory сохраняет Deployment descendants даже при дрейфе labels.
// Чужой Pod из serving selector исключается только как подтверждённая завершённая
// repo-owned CLI Job; labels, terminal phase или имя Job по отдельности недостаточны.
export function componentServingInventory(list, name, deploymentUID) {
  requireValue(Array.isArray(list?.items) && names.includes(name), 'COMPONENT_MIGRATION_POD_INVENTORY_INVALID');
  const sets=list.items.filter(r => r.kind === 'ReplicaSet');
  const owners=new Set(sets.filter(r => r.metadata?.ownerReferences?.some(o =>
    o.kind === 'Deployment' && o.controller === true && o.uid === deploymentUID)).map(r => r.metadata.uid));
  const jobs=list.items.filter(r => r.kind === 'Job');
  const retainedJobPod = pod => {
    if (name !== 'control-plane' || !pod.metadata?.uid || pod.metadata.namespace !== 'kodex-system' ||
      !isProvenStoppedPod(pod) || ['NodeLost','Unknown'].includes(pod.status.reason) ||
      pod.status.conditions?.some(c => c.status === 'Unknown')) return false;
    const refs=(pod.metadata.ownerReferences ?? []).filter(o => o.controller === true);
    if (refs.length !== 1 || refs[0].kind !== 'Job' || !refs[0].uid) return false;
    const matches=jobs.filter(j => j.apiVersion === 'batch/v1' && j.metadata?.namespace === pod.metadata.namespace &&
      j.metadata.uid === refs[0].uid && j.metadata.name === refs[0].name);
    if (matches.length !== 1) return false;
    const job=matches[0], labels=job.metadata.labels ?? {}, spec=job.spec?.template?.spec, main=spec?.containers?.[0];
    const migration=labels['app.kubernetes.io/component'] === 'migration' &&
      (job.metadata.name === 'control-plane-migrate' || /^control-plane-(?:migrate-|migration-check-)[0-9]{14}(?:-[a-z0-9-]+)?$/.test(job.metadata.name)) &&
      spec?.serviceAccountName === 'control-plane-migrator' && main?.name === 'migrate' &&
      main.args?.length === 1 && ['up','check'].includes(main.args[0]);
    const bootstrap=labels['app.kubernetes.io/component'] === 'broker-bootstrap' && job.metadata.name === 'control-plane-broker-bootstrap' &&
      spec?.serviceAccountName === 'control-plane-broker-bootstrap' && main?.name === 'bootstrap' &&
      Array.isArray(main.args) && main.args.length === 2 && main.args[0] === 'broker' && main.args[1] === 'bootstrap';
    return labels['app.kubernetes.io/name'] === name && labels['app.kubernetes.io/part-of'] === 'kodex' && (migration || bootstrap) &&
      pod.metadata.labels?.['app.kubernetes.io/name'] === name && pod.metadata.labels?.['app.kubernetes.io/component'] === labels['app.kubernetes.io/component'] &&
      !job.status?.active && !job.status?.terminating && job.status?.conditions?.filter(c => ['Complete','Failed'].includes(c.type) && c.status === 'True').length === 1 &&
      !job.status.conditions.some(c => c.status === 'Unknown') && spec.containers.length === 1 && image(main.image) &&
      main.command?.length === 1 && main.command[0] === '/usr/local/bin/control-plane-cli' && spec.restartPolicy === 'Never' && spec.automountServiceAccountToken === false &&
      pod.spec.serviceAccountName === spec.serviceAccountName && pod.spec.restartPolicy === spec.restartPolicy && pod.spec.automountServiceAccountToken === false &&
      !pod.spec.hostNetwork && !pod.spec.hostPID && !pod.spec.hostIPC &&
      ['containers','initContainers','ephemeralContainers'].every(field => {
        const expected=spec[field] ?? [], actual=pod.spec[field] ?? [];
        return actual.length === expected.length && expected.every((container,index) => actual[index] &&
          Object.keys(container).every(key => Object.hasOwn(actual[index],key) && fingerprint(actual[index][key]) === fingerprint(container[key])));
      });
  };
  const pods=list.items.filter(r => r.kind === 'Pod' && (r.metadata?.labels?.['app.kubernetes.io/name'] === name ||
    r.metadata?.ownerReferences?.some(o => o.kind === 'ReplicaSet' && o.controller === true && owners.has(o.uid))));
  requireValue(pods.every(p => !['NodeLost','Unknown'].includes(p.status?.reason) && p.status?.phase !== 'Unknown' &&
    !p.status?.conditions?.some(c => c.status === 'Unknown')), 'COMPONENT_MIGRATION_POD_STATE_UNKNOWN');
  return {items:[...sets,...pods.filter(p => !retainedJobPod(p))]};
}

// Spec и annotations соседних workloads не входят в namespace discovery.
// Полные Pod/Job читаются отдельно и связываются с UID/lineage первого snapshot.
export function readComponentServingInventory(kube, name, deploymentUID) {
  const validName=value => typeof value === 'string' && /^[a-z0-9][a-z0-9.-]{0,252}$/.test(value);
  const projection='{range .items[*]}{.kind}{"\\t"}{.metadata.name}{"\\t"}{.metadata.uid}{"\\t"}' +
    '{.metadata.labels.app\\.kubernetes\\.io/name}{"\\t"}{.metadata.namespace}{"\\t"}{.metadata.ownerReferences}{"\\n"}{end}';
  const metadata=() => {
    const text=kube(['-n','kodex-system','get','replicasets,pods,jobs','-o','jsonpath='+projection]);
    requireValue(Buffer.byteLength(text) < 1 << 20, 'COMPONENT_MIGRATION_METADATA_BOUND_EXCEEDED');
    const lines=text.split('\n').filter(line => line !== '');
    requireValue(lines.length <= 4096, 'COMPONENT_MIGRATION_METADATA_BOUND_EXCEEDED');
    const items=lines.map(line => {
      const fields=line.split('\t'), [kind,resourceName,resourceUID,app,namespace,owners]=fields;
      requireValue(fields.length === 6 && ['Pod','ReplicaSet','Job'].includes(kind) && namespace === 'kodex-system' &&
        validName(resourceName) && resourceUID && !/\s/.test(resourceUID), 'COMPONENT_MIGRATION_METADATA_INVALID');
      const ownerReferences=owners ? JSON.parse(owners) : [];
      requireValue(Array.isArray(ownerReferences), 'COMPONENT_MIGRATION_METADATA_INVALID');
      return {kind,metadata:{name:resourceName,uid:resourceUID,namespace,labels:{'app.kubernetes.io/name':app},ownerReferences}};
    });
    requireValue(new Set(items.map(r => r.kind+'/'+r.metadata.name)).size === items.length, 'COMPONENT_MIGRATION_METADATA_INVALID');
    return items;
  };
  const scoped=items => {
    const sets=items.filter(r => r.kind === 'ReplicaSet' && r.metadata.ownerReferences.some(o =>
      o.kind === 'Deployment' && o.controller === true && o.uid === deploymentUID));
    const owners=new Set(sets.map(r => r.metadata.uid));
    const pods=items.filter(r => r.kind === 'Pod' && (r.metadata.labels['app.kubernetes.io/name'] === name ||
      r.metadata.ownerReferences.some(o => o.kind === 'ReplicaSet' && o.controller === true && owners.has(o.uid))));
    const jobs=new Set(pods.flatMap(p => p.metadata.ownerReferences.filter(o => o.kind === 'Job' && o.controller === true).map(o => o.name)));
    return [...sets,...pods,...items.filter(r => r.kind === 'Job' && jobs.has(r.metadata.name))]
      .sort((a,b) => (a.kind+'/'+a.metadata.name).localeCompare(b.kind+'/'+b.metadata.name));
  };
  const first=scoped(metadata()), items=first.filter(r => r.kind === 'ReplicaSet'), jobs=new Map();
  const full=(kind,resourceName) => {
    requireValue(['pod','job'].includes(kind) && validName(resourceName), 'COMPONENT_MIGRATION_METADATA_INVALID');
    const text=kube(['-n','kodex-system','get',kind,resourceName,'--ignore-not-found=true','-o','jsonpath={@}']);
    requireValue(Buffer.byteLength(text) < 1 << 20, 'COMPONENT_MIGRATION_OBJECT_BOUND_EXCEEDED');
    return text.trim() ? JSON.parse(text) : undefined;
  };
  for (const pin of first.filter(r => r.kind === 'Pod')) {
    const pod=full('pod',pin.metadata.name);
    requireValue(pod?.kind === 'Pod' && pod.metadata?.namespace === pin.metadata.namespace && pod.metadata.name === pin.metadata.name &&
      pod.metadata.uid === pin.metadata.uid && (pod.metadata.labels?.['app.kubernetes.io/name'] ?? '') === pin.metadata.labels['app.kubernetes.io/name'] &&
      fingerprint(pod.metadata.ownerReferences ?? []) === fingerprint(pin.metadata.ownerReferences), 'COMPONENT_MIGRATION_POD_DISCOVERY_CHANGED');
    items.push(pod);
    for (const owner of pod.metadata.ownerReferences ?? []) if (owner.controller === true && owner.kind === 'Job' && !jobs.has(owner.name)) {
      const pin=first.find(r => r.kind === 'Job' && r.metadata.name === owner.name);
      if (!pin) {jobs.set(owner.name,undefined);continue;}
      requireValue(pin.metadata.uid === owner.uid, 'COMPONENT_MIGRATION_JOB_DISCOVERY_CHANGED');
      const job=full('job',owner.name);
      if (job) requireValue(job.kind === 'Job' && job.metadata?.name === owner.name && job.metadata.namespace === 'kodex-system' &&
        job.metadata.uid === owner.uid, 'COMPONENT_MIGRATION_JOB_DISCOVERY_CHANGED');
      jobs.set(owner.name,job);
    }
  }
  requireValue(fingerprint(scoped(metadata())) === fingerprint(first), 'COMPONENT_MIGRATION_POD_DISCOVERY_CHANGED');
  return componentServingInventory({items:[...items,...[...jobs.values()].filter(Boolean)]},name,deploymentUID);
}

// Journal принадлежит исходному migration protocol. Пара не получает ни собственного
// DDL/retry loop, ни права оживить прежний бинарник после успешного forward043.
export function componentMigrationLifecycle({plan,kube,journal,readJob,observeCP,preconditionSQL,drainSQL,postgresIdentity,validateInputs,activePods,wait,clock}) {
  const namespace='kodex-system', names=componentMigrationNames(plan.imageEvidence.profile);
  const workers=names.filter(name=>name!=='control-plane');
  const originals=plan.componentHandoff.components.map(c => ({kind:'Deployment',metadata:{name:c.name,namespace,uid:c.uid,resourceVersion:'1',generation:1},
    spec:c.beforeSpec,status:{observedGeneration:1,replicas:c.replicas,updatedReplicas:c.replicas,readyReplicas:c.replicas,availableReplicas:c.replicas}}));
  requireValue(fingerprint(planComponentMigrationHandoff(plan.imageEvidence,originals,plan.source)) === fingerprint(plan.componentHandoff), 'COMPONENT_MIGRATION_HANDOFF_PLAN_DRIFT');
  const part = name => plan.componentHandoff.components.find(c => c.name === name);
  const observe = name => {
    const expected=part(name), actual=JSON.parse(kube(['-n',namespace,'get','deployment',name,'-o','json']));
    requireValue(actual.metadata.uid === expected.uid,'COMPONENT_MIGRATION_HANDOFF_UID_CHANGED');
    const stages=expected.maintenance?['stoppedSpec','stagedSpec']:['beforeSpec','stoppedSpec','stagedSpec','resumedSpec'];
    const stage=stages.find(k => fingerprint(expected[k]) === fingerprint(actual.spec));
    requireValue(stage,'COMPONENT_MIGRATION_HANDOFF_SPEC_CHANGED');
    const pods=activePods(readComponentServingInventory(kube,name,expected.uid),expected.uid);
    const joined=actual.status?.observedGeneration >= actual.metadata.generation && !(actual.status.replicas ?? 0) && pods.length === 0;
    const ready=actual.status?.observedGeneration >= actual.metadata.generation && actual.spec.replicas === expected.replicas &&
      ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k => actual.status[k] === expected.replicas) && pods.length === expected.replicas &&
      pods.every(p => !p.metadata.deletionTimestamp && p.status?.conditions?.some(c => c.type === 'Ready' && c.status === 'True') &&
        ['containers','initContainers','ephemeralContainers'].every(field => {
          const expected=actual.spec.template.spec[field] ?? [], observed=p.spec?.[field] ?? [];
          const statusField={containers:'containerStatuses',initContainers:'initContainerStatuses',ephemeralContainers:'ephemeralContainerStatuses'}[field];
          const statuses=p.status[statusField] ?? [];
          // Deployment template — полный согласованный состав. Admission/debug
          // не вправе незаметно добавить writer, заменить command/env или создать
          // второй status с тем же именем. Ephemeral writers не входят в профиль.
          return Array.isArray(expected) && Array.isArray(observed) && Array.isArray(statuses) &&
            (field !== 'ephemeralContainers' || expected.length === 0) &&
            fingerprint(observed) === fingerprint(expected) && statuses.length === expected.length &&
            expected.every(c => {
              const matches=statuses.filter(v => v.name === c.name), status=matches[0], digest=c.image?.split('@').at(-1);
              return image(c.image) && matches.length === 1 && typeof status.imageID === 'string' &&
                (status.imageID === digest || status.imageID === 'containerd://'+digest || status.imageID.endsWith('@'+digest)) &&
                (field === 'containers' || c.restartPolicy === 'Always' ? status.ready === true : status.state?.terminated?.exitCode === 0);
            });
        }));
    return {actual,stage,joined,ready};
  };
  const inspect = () => names.map(name => {
    const state=observe(name);
    journal.append({type:'PAIR_INSPECT',name,stage:state.stage,resourceVersion:state.actual.metadata.resourceVersion});
    return {name,stage:state.stage,joined:state.joined,ready:state.ready,...(part(name).maintenance?{maintenance:true}:{})};
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
    withPrivatePatchFile(patch, path => {
      const args=['-n',namespace,'patch','deployment',name,'--type=json',`--patch-file=${path}`];
      kube([...args,'--dry-run=server']);
      journal.append({type,name,stage,resourceVersion:state.actual.metadata.resourceVersion});
      try { kube(args); }
      catch(error) {journal.append({type:'UNKNOWN',step:type,name,code:error.message});throw error;}
    });
    requireValue(observe(name).stage === stage,'COMPONENT_MIGRATION_HANDOFF_UNCONFIRMED');
  };
  const schemaReady = (resuming = false) => {
    readJob(true); // Полный terminal Job/Pod writer join перед schema postcondition.
    const result=readJob();
    requireValue(result.status === 'PASS' && result.schemaPostcondition === 'PASS','COMPONENT_MIGRATION_POSTCONDITION_REQUIRED');
    const extended=plan.imageEvidence.schema === componentMigrationCohortSchema;
    requireValue(!extended || typeof drainSQL === 'function' && sha(plan.componentDrainSQLSHA256),'COMPONENT_MIGRATION_DRAIN_SOURCE_REQUIRED');
    const sql=extended?drainSQL(plan.source):preconditionSQL(plan.source);
    requireValue(createHash('sha256').update(sql).digest('hex') === (extended?plan.componentDrainSQLSHA256:plan.preconditionSQLSHA256) &&
      fingerprint(postgresIdentity()) === fingerprint(plan.postgres),'MIGRATION_PRECONDITION_SOURCE_CHANGED');
    const proof=JSON.parse(kube(['-n',namespace,'exec','-i','kodex-postgresql-0','--','psql','-XqAt','-U','postgres','-d','control_plane','-v','ON_ERROR_STOP=1'],sql));
    requireValue(proof.database === 'control_plane' && proof.readOnly === 'on' && proof.gooseVersion === Number(plan.migrationVersion) &&
      ['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions',
        'pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks'].every(k => proof[k] === 0), 'MIGRATION_OWNER_PRECONDITION_FAILED');
    // После подтверждённого запуска нового CP его обычный outbox loop снова
    // открывает claim sessions. Это не старый writer: exact staged image,
    // положительная readiness всех Pods и наш durable resume intent обязательны.
    // Handoff и первый resume по-прежнему требуют отсутствия любых claims.
    const resumedCP = resuming && extended ? observe('control-plane') : undefined;
    const currentClaims = resumedCP?.stage === 'resumedSpec' && resumedCP.ready &&
      journal.events.some(e => e.type === 'RESUME_INTENT' && e.name === 'control-plane');
    if(extended) requireValue(proof.profile === 'PORTABLE_COMPONENT_DRAIN_V1' &&
      Number.isSafeInteger(proof.oldClaimSessions) && proof.oldClaimSessions >= 0 &&
      (currentClaims || proof.oldClaimSessions === 0) &&
      ['activeRuntimeLeases','activeIntegrationInvocations','activeIntegrationConnectionTests','activePersonalCredentialTests',
        'activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks'].every(k=>proof[k]===0),'MIGRATION_OWNER_PRECONDITION_FAILED');
    requireValue(fingerprint(postgresIdentity()) === fingerprint(plan.postgres),'MIGRATION_POSTGRES_IDENTITY_CHANGED');
    validateInputs();
  };
  return {
    inspect,
    requireStopped() {
      for(const name of workers) {
        const state=observe(name);
        requireValue(state.stage === 'stoppedSpec' && state.joined && journal.events.some(e => e.type === 'PAIR_STOP_ACK' && e.name === name),'COMPONENT_MIGRATION_CONTROLLER_JOIN_REQUIRED');
      }
    },
    async stopController() {
      for(const name of workers) {
        // Внешний API уже закрыт проверенным maintenance target. Его нулевые
        // replicas сохраняются; ACK подтверждает join, не выдуманный patch.
        if(!part(name).maintenance) transition(name,'stoppedSpec','PAIR_STOP_INTENT');
        await wait(() => observe(name),s => s.stage === 'stoppedSpec' && s.joined,'COMPONENT_MIGRATION_CONTROLLER_STOP_TIMEOUT',clock);
        journal.append({type:'PAIR_STOP_ACK',name});
      }
    },
    async handoff() {
      schemaReady();
      const cp=observeCP();
      requireValue(['STOPPED','STAGED'].includes(cp.stage) && cp.joined && journal.events.some(e => e.type === 'STOP_ACK'),'MIGRATION_STOP_JOIN_REQUIRED');
      for(const name of workers) {
        requireValue(observe(name).joined && journal.events.some(e => e.type === 'PAIR_STOP_ACK' && e.name === name),'COMPONENT_MIGRATION_CONTROLLER_JOIN_REQUIRED');
        transition(name,'stagedSpec','PAIR_HANDOFF_INTENT');
      }
      transition('control-plane','stagedSpec','PAIR_HANDOFF_INTENT');
      journal.append({type:'PAIR_HANDOFF_ACK',publicationSHA256:plan.componentHandoff.publicationSHA256});
      return {status:'STAGED',schemaPostcondition:'PASS',cpStage:observeCP().stage,forwardOnly:true};
    },
    async resume() {
      schemaReady(true);requireValue(journal.events.some(e => e.type === 'PAIR_HANDOFF_ACK'),'COMPONENT_MIGRATION_HANDOFF_REQUIRED');
      for (const name of names) {
        if(part(name).maintenance) {
          const state=observe(name);
          requireValue(state.stage === 'stagedSpec' && state.joined,'COMPONENT_MIGRATION_MAINTENANCE_JOIN_REQUIRED');
          continue;
        }
        transition(name,'resumedSpec',name === 'control-plane' ? 'RESUME_INTENT' : 'PAIR_RESUME_INTENT');
        await wait(() => observe(name),s => s.stage === 'resumedSpec' && s.ready,'COMPONENT_MIGRATION_RESUME_TIMEOUT',clock);
        journal.append({type:name === 'control-plane' ? 'RESUME_ACK' : 'PAIR_RESUME_ACK',name});
      }
      return {status:'RESUMED',cpStage:'UPGRADED',schemaPostcondition:'PASS',pairReady:true,forwardOnly:true,
        ...(names.some(name=>part(name).maintenance)?{gatewayMaintenance:true}:{})};
    },
  };
}
