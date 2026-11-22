import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {resolve} from 'node:path';
import {inspectSource} from './application-source.mjs';
import {parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {fingerprint} from './scoped-release.mjs';

const namespace = 'kodex-system';
export const legacyPersonalStagedInputsSchema='kodex.personal-integrations-staged-inputs/v1';
export const personalStagedInputsSchema='kodex.personal-integrations-staged-inputs/v2';
const workload = 'artifact-retention';
const publisher = 'internal-rpc-authority-publisher';
const authorityPrefix = 'internal-rpc-authority-' + workload + '-';
const issuerSecretSources = {
  'issuer-key': authorityPrefix + 'issuer-key',
  'issuer-readback-credential': workload + '-issuer-readback-credential',
  'issuer-readback-possession': workload + '-issuer-readback-key',
  'issuer-restore-ack': workload + '-issuer-restore-ack',
  'issuer-restore-credential': workload + '-issuer-restore-credential',
  'manifest-trust': authorityPrefix + 'manifest-trust',
  'proof-trust': authorityPrefix + 'proof-trust',
};
const issuerSecretNames = Object.keys(issuerSecretSources).map(suffix => authorityPrefix + suffix);
const ingressPolicies = ['control-plane-exact-runtime-paths', 'internal-rpc-authority-readback-attestor-exact-paths',
  'internal-rpc-authority-restore-controller-exact-paths'];
const issuerNetworkPolicy = workload + '-internal-rpc-authority-exact-paths';
const certificateName = authorityPrefix + 'workload';
const cpTrustMount = {name:'application-grants',mountPath:'/var/run/config/kodex/control-plane/application-grants',readOnly:true};
const cpTrustVolume = {name:'application-grants',secret:{secretName:'control-plane-application-grants',defaultMode:292}};
const captureSpool = 'kubernetes-capture-spool';
const captureSpoolPath = '/work/' + captureSpool;
const cpEnv = [
  {name:'CONTROL_PLANE_ARTIFACT_RETENTION_GRANT_TRUST_FILE',value:cpTrustMount.mountPath+'/artifact-retention.platform-worker.public.jwk'},
  {name:'CONTROL_PLANE_KUBERNETES_CAPTURE_SPOOL_ROOT',value:captureSpoolPath},
];
const cpSpoolMount = {name:captureSpool,mountPath:captureSpoolPath};
const cpSpoolVolume = {name:captureSpool,emptyDir:{sizeLimit:'512Mi'}};
const ownerLabel = 'app.kubernetes.io/part-of';
const uuid = /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/;
const revision = /^[a-f0-9]{40}$/;
const same = (left, right) => fingerprint(left) === fingerprint(right);
const ensure = (condition, code) => { if (!condition) throw Error(code); };

function one(values, predicate, code = 'ARTIFACT_RETENTION_ISSUER_INPUT_SOURCE_REJECTED') {
  const found = values?.filter(predicate) ?? [];
  ensure(found.length === 1, code);
  return found[0];
}

const named = (values, name) => one(values, value => value.name === name);
const object = (values, kind, name) => one(values, item => item.kind === kind && item.metadata?.name === name &&
  item.metadata.namespace === namespace && item.metadata.labels?.[ownerLabel] === 'kodex');

function immutableImage(value, component) {
  return typeof value === 'string' && /^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(value) &&
    value.includes('/' + component + '@sha256:') && !value.includes('..') && !value.endsWith('0'.repeat(64));
}

function requireNamedInputs(actual, expected) {
  ensure(expected.every(field=>same(named(actual,field.name),field)), 'ARTIFACT_RETENTION_ISSUER_INPUT_SOURCE_REJECTED');
}

// Читаем только public manifest из закреплённого, чистого source. Kustomize здесь
// рендерит исходники, не обращается к API и не получает значения Secret.
export function readArtifactRetentionIssuerInputs(source, kube) {
  ensure(revision.test(source?.revision ?? '') && inspectSource(source.path).revision === source.revision,
    'ARTIFACT_RETENTION_ISSUER_SOURCE_CHANGED');
  const yaml = kube(['kustomize', resolve(source.path, 'deploy/k8s/profiles/web-only')]);
  const json = execFileSync('yq', ['-o=json', '-I=0', 'select(.metadata.namespace == "kodex-system")', '-'], {
    input: yaml, encoding: 'utf8', timeout: 10000, maxBuffer: 16 << 20, stdio: ['pipe', 'pipe', 'pipe'],
  });
  const resources = json.trim().split('\n').map(parseAuthorityPrivateJSON);
  const projectionSource = parseAuthorityPrivateJSON(readFileSync(resolve(source.path, 'tools/install/secret-projections.json'), 'utf8'));
  ensure(inspectSource(source.path).revision === source.revision, 'ARTIFACT_RETENTION_ISSUER_SOURCE_CHANGED');
  const publisherRule = one(object(resources, 'Role', publisher).rules, rule => same(rule.apiGroups, ['']) &&
    same(rule.resources, ['secrets']) && same(rule.verbs, ['get', 'update']));
  for (const [suffix, sourceRef] of Object.entries(issuerSecretSources)) {
    const name = authorityPrefix + suffix;
    const projection = one(projectionSource.secrets, item => item.name === name);
    ensure(projection.dynamic === true && projection.items?.length > 0 &&
      projection.items.every(item => item.source?.type === 'authority' && item.source.ref === sourceRef) &&
      publisherRule.resourceNames.includes(name), 'ARTIFACT_RETENTION_ISSUER_SECRET_PROJECTION_CHANGED');
  }
  const certificate = object(resources, 'Certificate', certificateName);
  ensure(same(certificate.spec.issuerRef, {kind: 'Issuer', name: 'kodex-installation-ca'}) &&
    certificate.spec.secretName === authorityPrefix + 'workload-tls' &&
    same(certificate.spec.uris, ['spiffe://kodex.local/ns/kodex-system/sa/artifact-retention']),
    'ARTIFACT_RETENTION_ISSUER_CERTIFICATE_SOURCE_CHANGED');
  const cp = object(resources,'Deployment','control-plane').spec.template.spec;
  const cpApp = named(cp.containers,'control-plane');
  requireNamedInputs(cpApp.env,cpEnv);
  requireNamedInputs(cpApp.volumeMounts,[cpTrustMount,cpSpoolMount]);
  requireNamedInputs(cp.volumes,[cpTrustVolume,cpSpoolVolume]);
  return {sourceSHA: source.revision, issuerSecretNames: [...issuerSecretNames],
    deployment: object(resources, 'Deployment', workload), certificate,
    issuerNetworkPolicy: object(resources, 'NetworkPolicy', issuerNetworkPolicy),
    runtimeNetworkPolicy: object(resources, 'NetworkPolicy', workload + '-exact-runtime-paths'),
    ingressPolicies: ingressPolicies.map(name => object(resources, 'NetworkPolicy', name))};
}

// В остановленный CP добавляются только адрес ключа нового worker и bounded
// capture spool. Existing DB, provider, backup и TLS inputs сохраняют свои байты.
export function stagePersonalIntegrationsControlPlaneSpec(stopped) {
  const result = structuredClone(stopped), pod = result.template.spec;
  ensure(stopped.replicas===0&&pod.serviceAccountName==='control-plane',
    'ARTIFACT_RETENTION_CONTROL_PLANE_STOPPED_SPEC_REJECTED');
  const app = named(pod.containers,'control-plane');
  requireNamedInputs(app.volumeMounts,[cpTrustMount]);
  requireNamedInputs(pod.volumes,[cpTrustVolume]);
  const append = (list,expected) => {
    const matches = list.filter(value=>value.name===expected.name||
      (Object.hasOwn(expected,'mountPath')&&value.mountPath===expected.mountPath));
    ensure(matches.length===0||matches.length===1&&same(matches[0],expected),
      'ARTIFACT_RETENTION_CONTROL_PLANE_INPUT_CONFLICT');
    if (matches.length===0) list.push(structuredClone(expected));
  };
  for (const env of cpEnv) append(app.env,env);
  append(app.volumeMounts,cpSpoolMount);
  append(pod.volumes,cpSpoolVolume);
  return result;
}

// Добавляем только issuer-контейнеры и их входы из checked render. Внешние DSN,
// локальный S3 CA, observability и прежние параметры приложения остаются в spec.
export function stageArtifactRetentionIssuerSpec(stopped, inputs, componentImage, authorityImage, schema=personalStagedInputsSchema) {
  const desired = inputs.deployment.spec.template;
  const spec = structuredClone(stopped), template = spec.template, pod = template.spec;
  ensure(stopped.replicas === 0 && pod.serviceAccountName === workload && pod.automountServiceAccountToken === false &&
    !pod.hostNetwork && !pod.hostPID && !pod.hostIPC && pod.containers?.length === 1 &&
    !pod.initContainers?.length && !pod.ephemeralContainers?.length &&
    immutableImage(componentImage, workload) && immutableImage(authorityImage, 'internal-rpc-authority'),
    'ARTIFACT_RETENTION_ISSUER_STOPPED_SPEC_REJECTED');
  const app = named(pod.containers, workload), sourceApp = named(desired.spec.containers, workload);
  const issuer = structuredClone(named(desired.spec.containers, 'internal-rpc-authority-issuer'));
  const worker = structuredClone(named(desired.spec.containers, 'platform-worker-grant-agent'));
  const init = structuredClone(named(desired.spec.initContainers, 'internal-rpc-authority-socket-init'));
  ensure(desired.spec.containers.length === 3 && desired.spec.initContainers.length === 1,
    'ARTIFACT_RETENTION_ISSUER_CONTAINERS_REJECTED');
  if(schema!==legacyPersonalStagedInputsSchema){
    const socketGroup = 29000;
    const security = desired.spec.securityContext;
    ensure(schema===personalStagedInputsSchema&&security?.fsGroup === socketGroup && security.fsGroupChangePolicy === 'OnRootMismatch' &&
      init.securityContext?.runAsGroup === socketGroup && init.securityContext.runAsUser === socketGroup &&
      pod.securityContext?.fsGroup === 10001 && pod.securityContext.fsGroupChangePolicy === security.fsGroupChangePolicy,
      'ARTIFACT_RETENTION_ISSUER_SOCKET_GROUP_REJECTED');
    // Kubelet назначает группе init доступ к общему emptyDir до запуска контейнеров.
    pod.securityContext.fsGroup = socketGroup;
  }
  const append = (target, additions) => {
    for (const addition of additions) {
      ensure(!target.some(entry => entry.name === addition.name), 'ARTIFACT_RETENTION_ISSUER_INPUT_CONFLICT');
      target.push(structuredClone(addition));
    }
  };
  const socketEnv = ['INTERNAL_RPC_AUTHORITY_ISSUER_SOCKET', 'INTERNAL_RPC_AUTHORITY_LOCAL_ROLE'];
  const mounts = ['internal-rpc-authority-sockets', 'workload-tls', 'control-plane-ca', 'application-grant'];
  append(app.env, socketEnv.map(name => named(sourceApp.env, name)));
  append(app.volumeMounts, mounts.map(name => named(sourceApp.volumeMounts, name)));
  const addedVolumes = new Set([issuer, worker, init].flatMap(container => container.volumeMounts.map(mount => mount.name)));
  mounts.forEach(name => addedVolumes.add(name));
  const required = [...addedVolumes].map(name => named(desired.spec.volumes, name));
  append(pod.volumes, required);
  const disabled = app.env.some(entry => entry.name === 'OTEL_SDK_DISABLED' && entry.value === 'true');
  if (disabled) for (const container of [issuer, worker]) {
    container.env ??= [];
    append(container.env, [{name: 'OTEL_SDK_DISABLED', value: 'true'}]);
  }
  app.image = componentImage;
  for (const container of [issuer, worker, init]) container.image = authorityImage;
  pod.initContainers = [init];
  pod.containers.push(issuer, worker);
  for (const field of ['kodex.dev/internal-rpc-authority-abi', 'kodex.dev/internal-rpc-authority-issuer']) {
    ensure(!Object.hasOwn(template.metadata.labels, field) && typeof desired.metadata.labels[field] === 'string',
      'ARTIFACT_RETENTION_ISSUER_LABEL_CONFLICT');
    template.metadata.labels[field] = desired.metadata.labels[field];
  }
  return spec;
}

// Publisher получает get/update только для семи адресных проекций. Ingress
// открывается только существующей политикой до конкретного workload на 8443.
export function artifactRetentionIssuerPatchFields(inputs, liveResources) {
  ensure(same(inputs.issuerSecretNames, issuerSecretNames), 'ARTIFACT_RETENTION_ISSUER_SECRET_PROJECTION_CHANGED');
  const role = object(liveResources, 'Role', publisher);
  ensure(role.rules.length === 1, 'ARTIFACT_RETENTION_ISSUER_PUBLISHER_ROLE_REJECTED');
  const rules = structuredClone(role.rules), rule = rules[0];
  ensure(same(rule.apiGroups, ['']) && same(rule.resources, ['secrets']) && same(rule.verbs, ['get', 'update']) &&
    Array.isArray(rule.resourceNames) && rule.resourceNames.length > 0 &&
    new Set(rule.resourceNames).size === rule.resourceNames.length && rule.resourceNames.every(name => /^[a-z0-9.-]+$/.test(name)),
    'ARTIFACT_RETENTION_ISSUER_PUBLISHER_ROLE_REJECTED');
  for (const name of inputs.issuerSecretNames) if (!rule.resourceNames.includes(name)) rule.resourceNames.push(name);
  const result = [{before: role, field: 'rules', after: rules}];
  const peer = {podSelector: {matchLabels: {'app.kubernetes.io/name': workload, 'app.kubernetes.io/component': 'retention-job'}}};
  for (const name of ingressPolicies) {
    const before = object(liveResources, 'NetworkPolicy', name);
    const source = object(inputs.ingressPolicies, 'NetworkPolicy', name);
    const spec = structuredClone(before.spec);
    ensure(same(spec.podSelector, source.spec.podSelector) && same(spec.policyTypes, source.spec.policyTypes),
      'ARTIFACT_RETENTION_ISSUER_INGRESS_SCOPE_CHANGED');
    const port = ingress => same(ingress.ports, [{protocol: 'TCP', port: 8443}]);
    const next = one(spec.ingress, port), desired = one(source.spec.ingress, port);
    if (name === 'control-plane-exact-runtime-paths') {
      one(desired.from, entry => same(entry, peer));
      if (!next.from.some(entry => same(entry, peer))) next.from.push(structuredClone(peer));
    } else {
      const expression = entry => one(one(entry.from, item => item.podSelector?.matchExpressions?.some(
        expression => expression.key === 'app.kubernetes.io/name' && expression.operator === 'In')).podSelector.matchExpressions,
      selector => selector.key === 'app.kubernetes.io/name' && selector.operator === 'In');
      const values = expression(next).values;
      ensure(expression(desired).values.includes(workload) &&
        Array.isArray(values) && values.every(value => /^[a-z0-9-]+$/.test(value)) && new Set(values).size === values.length,
        'ARTIFACT_RETENTION_ISSUER_INGRESS_SCOPE_CHANGED');
      if (!values.includes(workload)) values.push(workload);
    }
    result.push({before, field: 'spec', after: spec});
  }
  ensure(result.every(({before}) => uuid.test(before.metadata.uid ?? '') &&
    /^[1-9][0-9]*$/.test(before.metadata.resourceVersion ?? '') && !before.metadata.deletionTimestamp),
    'ARTIFACT_RETENTION_ISSUER_INPUT_OWNER_CHANGED');
  return result;
}
