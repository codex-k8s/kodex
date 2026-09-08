#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { lstatSync, readFileSync, writeFileSync, realpathSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fingerprint } from '../release/scoped-release.mjs';
import { inspectSource } from '../release/application-source.mjs';

const sha = /^[a-f0-9]{64}$/;
const revision = /^[a-f0-9]{40}$/;
const safeName = /^[a-z0-9][a-z0-9.-]{0,252}$/;
function requireValue(value, code) { if (!value) throw new Error(code); }
function exact(value, keys) { return value && typeof value === 'object' && !Array.isArray(value) && Object.keys(value).sort().join() === [...keys].sort().join(); }
let deadline = Infinity;
const run = (command, args) => {
  const remaining = deadline - Date.now();
  requireValue(remaining > 0, 'READBACK_BUDGET_EXHAUSTED');
  return execFileSync(command, args, { encoding: 'utf8', timeout: Math.min(30000, remaining), maxBuffer: 32 << 20, stdio: ['ignore', 'pipe', 'pipe'] }).trim();
};
const digest = value => createHash('sha256').update(value).digest('hex');
const owner = (object, uid) => (object.metadata?.ownerReferences ?? []).some(ref => ref.uid === uid && ref.controller === true);

export function workloadPods(workload, resources) {
  const replicas = resources.filter(item => item.kind === 'ReplicaSet' && owner(item, workload.metadata.uid));
  return resources.filter(item => item.kind === 'Pod' && !item.metadata.deletionTimestamp && item.status?.phase !== 'Succeeded' && item.status?.phase !== 'Failed' &&
    (owner(item, workload.metadata.uid) || replicas.some(replica => owner(item, replica.metadata.uid))));
}

export function projectWorkload(workload, resources, inspect = () => null, executable = () => null) {
  const { metadata, spec, status } = workload;
  requireValue(['Deployment', 'StatefulSet', 'DaemonSet'].includes(workload.kind) && safeName.test(metadata?.name) && metadata.uid &&
    status?.observedGeneration >= metadata.generation, 'WORKLOAD_NOT_OBSERVED');
  const desired = workload.kind === 'DaemonSet' ? status.desiredNumberScheduled : spec.replicas;
  requireValue(Number.isInteger(desired) && desired > 0, 'WORKLOAD_REPLICAS_REQUIRED');
  const pods = workloadPods(workload, resources);
  requireValue(pods.length === desired, 'WORKLOAD_ROLLOUT_INCOMPLETE');
  const containers = [...(spec.template.spec.containers ?? []), ...(spec.template.spec.initContainers ?? [])];
  const sources = [];
  for (const container of containers) for (const mount of container.volumeMounts ?? []) {
    if (mount.mountPath !== '/workspace' && !mount.mountPath.startsWith('/workspace/')) continue;
    const volume = spec.template.spec.volumes?.find(item => item.name === mount.name);
    if (!volume?.hostPath || mount.mountPath.includes('/node_modules') || mount.mountPath.endsWith('/public/config')) continue;
    requireValue(mount.readOnly === true && !mount.subPathExpr, 'SOURCE_MOUNT_NOT_READONLY');
    const source = inspect(volume.hostPath.path);
    requireValue(source && revision.test(source.revision), 'SOURCE_IDENTITY_INVALID');
    sources.push({ container: container.name, mount: mount.mountPath, ...source });
  }
  const resultPods = pods.map(pod => {
    requireValue(pod.status.phase === 'Running' && pod.status.conditions?.some(item => item.type === 'Ready' && item.status === 'True'), 'POD_NOT_READY');
    for (const [key, value] of Object.entries(spec.template.metadata?.annotations ?? {}))
      requireValue(pod.metadata.annotations?.[key] === value, 'POD_TEMPLATE_ANNOTATION_MISMATCH');
    for (const volume of spec.template.spec.volumes ?? [])
      requireValue(fingerprint(pod.spec.volumes?.find(item => item.name === volume.name) ?? null) === fingerprint(volume), 'POD_VOLUME_MISMATCH');
    const statuses = [...(pod.status.containerStatuses ?? []), ...(pod.status.initContainerStatuses ?? [])];
    const images = containers.map(container => {
      const actual = [...(pod.spec.containers ?? []), ...(pod.spec.initContainers ?? [])].find(item => item.name === container.name);
      const state = statuses.find(item => item.name === container.name);
      for (const key of ['command', 'args', 'env', 'envFrom', 'workingDir'])
        requireValue(fingerprint(actual?.[key] ?? null) === fingerprint(container[key] ?? null), 'POD_CONTAINER_SPEC_MISMATCH');
      for (const mount of container.volumeMounts ?? [])
        requireValue(fingerprint(actual?.volumeMounts?.find(item => item.name === mount.name && item.mountPath === mount.mountPath) ?? null) === fingerprint(mount), 'POD_MOUNT_MISMATCH');
      requireValue(/@sha256:[a-f0-9]{64}$/.test(container.image ?? '') && actual?.image === container.image && state && /sha256:[a-f0-9]{64}$/.test(state.imageID ?? ''), 'CONTAINER_IMAGE_MISMATCH');
      const init = (spec.template.spec.initContainers ?? []).some(item => item.name === container.name);
      requireValue(init && container.restartPolicy !== 'Always' ? state.state?.terminated?.exitCode === 0 : state.ready === true && state.state?.running, 'CONTAINER_NOT_READY');
      const hotReload = container.command?.includes('/workspace/tools/dev/run-go-hot-reload.sh');
      const binarySHA256 = hotReload ? executable(pod, container) : null;
      requireValue(!hotReload || sha.test(binarySHA256 ?? ''), 'RUNNING_EXECUTABLE_REQUIRED');
      return { name: container.name, image: container.image, imageID: state.imageID, binarySHA256 };
    });
    return { uid: pod.metadata.uid, name: pod.metadata.name, images, restarts: statuses.map(item => ({ name: item.name, count: item.restartCount ?? 0 })) };
  });
  // UID и restart counters входят в отдельное наблюдение, но не запрещают законную замену Pod той же спецификации.
  const images = resultPods[0].images;
  requireValue(resultPods.every(pod => fingerprint(pod.images) === fingerprint(images)), 'REPLICA_BINARY_MISMATCH');
  return { kind: workload.kind, name: metadata.name, uid: metadata.uid, specSHA256: fingerprint(spec), images, sources, pods: resultPods.map(({ uid, name, restarts }) => ({ uid, name, restarts })) };
}

export function verifyManifest(expected, actual) {
  requireValue(expected?.version === 1 && expected.profile === 'component-revisions' && expected.clusterUID === actual.clusterUID && expected.namespace === actual.namespace && expected.namespaceUID === actual.namespaceUID, 'MANIFEST_IDENTITY_MISMATCH');
  requireValue(Array.isArray(expected.components) && expected.components.length > 0 && expected.components.length === actual.components.length, 'MANIFEST_COMPONENT_SET_MISMATCH');
  requireValue(new Set(expected.components.map(item => `${item.kind}/${item.name}`)).size === expected.components.length, 'DUPLICATE_COMPONENT');
  for (const component of expected.components) {
    const observed = actual.components.find(item => item.kind === component.kind && item.name === component.name);
    requireValue(observed && component.uid === observed.uid, 'COMPONENT_UID_MISMATCH');
    for (const key of ['specSHA256', 'images', 'sources']) requireValue(fingerprint(component[key] ?? null) === fingerprint(observed[key] ?? null), `COMPONENT_${key.toUpperCase()}_MISMATCH`);
  }
  requireValue(fingerprint(expected.compatibility) === fingerprint(actual.compatibility), 'COMPATIBILITY_CHANGED');
  return true;
}

// Матрицу совместимости назначает release plan. Совпадение SHA само по себе не является решением о совместимости.
export function validateCompatibility(value, components, read = readFileSync) {
  requireValue(exact(value, ['version', 'components', 'evidence']) && value.version === 1 && Array.isArray(value.components) && Array.isArray(value.evidence) && value.evidence.length > 0, 'COMPATIBILITY_REQUIRED');
  const names = components.map(item => `${item.kind}/${item.name}`).sort();
  requireValue(fingerprint(value.components.map(item => item.component).sort()) === fingerprint(names), 'COMPATIBILITY_COMPONENT_SET_MISMATCH');
  for (const entry of value.components) {
    requireValue(exact(entry, ['component', 'revisions', 'provides', 'requires']) && entry.provides && typeof entry.provides === 'object' && !Array.isArray(entry.provides) && Array.isArray(entry.requires), 'COMPATIBILITY_PROFILE_INVALID');
    const actualComponent = components.find(item => `${item.kind}/${item.name}` === entry.component);
    const sourceRevisions = [...new Set(actualComponent.sources.map(source => source.revision))].sort();
    const imageIDs = [...new Set(actualComponent.images.map(image => image.imageID))].sort();
    requireValue(exact(entry.revisions, ['source', 'imageIDs']) && fingerprint(entry.revisions.source) === fingerprint(sourceRevisions) && fingerprint(entry.revisions.imageIDs) === fingerprint(imageIDs), 'COMPATIBILITY_REVISION_MISMATCH');
    requireValue(Object.entries(entry.provides).every(([name, hash]) => safeName.test(name) && sha.test(hash)), 'CONTRACT_DIGEST_INVALID');
    for (const requirement of entry.requires) {
      requireValue(exact(requirement, ['component', 'contract', 'acceptedSHA256']) && Array.isArray(requirement.acceptedSHA256) && requirement.acceptedSHA256.length > 0 && requirement.acceptedSHA256.every(hash => sha.test(hash)), 'CONTRACT_REQUIREMENT_INVALID');
      const producer = value.components.find(item => item.component === requirement.component);
      requireValue(producer && requirement.acceptedSHA256.includes(producer.provides?.[requirement.contract]), 'CONTRACT_INCOMPATIBLE');
    }
  }
  for (const evidence of value.evidence) {
    requireValue(exact(evidence, ['path', 'sha256']) && typeof evidence.path === 'string' && evidence.path.startsWith('/') && sha.test(evidence.sha256), 'COMPATIBILITY_EVIDENCE_INVALID');
    requireValue(digest(read(evidence.path)) === evidence.sha256, 'COMPATIBILITY_EVIDENCE_CHANGED');
  }
  return value;
}

function main(args) {
  deadline = Date.now() + 600000;
  const command = args.shift(), options = {};
  requireValue(['inventory', 'capture', 'verify'].includes(command), 'INVALID_COMMAND');
  while (args.length) { const key = args.shift(); requireValue(['--context', '--manifest', '--output', '--compatibility'].includes(key) && !options[key] && args.length, 'INVALID_ARGUMENT'); options[key] = args.shift(); }
  requireValue(options['--context'] && !/prod/i.test(options['--context']), 'EXACT_STAGING_CONTEXT_REQUIRED');
  requireValue(options['--output'] && (command === 'inventory' || (command === 'capture' ? options['--compatibility'] : options['--manifest'])), 'REQUIRED_ARGUMENT_MISSING');
  const kubectl = (...args) => run('kubectl', ['--context', options['--context'], ...args]);
  requireValue(run('kubectl', ['config', 'current-context']) === options['--context'], 'CONTEXT_MISMATCH');
  const get = (...args) => JSON.parse(kubectl('get', ...args, '-o', 'json'));
  const namespace = get('namespace', 'kodex-system');
  requireValue(namespace.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' && namespace.metadata.labels?.['kodex.dev/environment'] === 'staging', 'STAGING_NAMESPACE_REQUIRED');
  const expected = command === 'verify' ? JSON.parse(readFileSync(options['--manifest'], 'utf8')) : null;
  const compatibility = command === 'inventory' ? null : expected?.compatibility ?? JSON.parse(readFileSync(options['--compatibility'], 'utf8'));
  const resources = get('deployments,statefulsets,daemonsets,replicasets,pods', '-n', 'kodex-system').items;
  const cache = new Map();
  const inspect = path => {
    requireValue(realpathSync(path) === path, 'SOURCE_SYMLINK_FORBIDDEN');
    const directory = lstatSync(path).isDirectory() ? path : dirname(path);
    const root = run('git', ['-C', directory, 'rev-parse', '--show-toplevel']);
    if (!cache.has(root)) cache.set(root, { path: root, ...inspectSource(root) });
    return { ...cache.get(root), mountedPath: path };
  };
  const executable = (pod, container) => {
    const name = container.args?.[2];
    requireValue(safeName.test(name ?? ''), 'HOT_RELOAD_PROCESS_INVALID');
    // Читается /proc/PID/exe фактически работающего процесса, а не заменяемый Air файл build/main.
    const script = 'expected=$1; count=0; result=; for entry in /proc/[0-9]*/exe; do target=$(readlink "$entry" 2>/dev/null) || continue; if [ "$target" = "$expected" ] || [ "$target" = "$expected (deleted)" ]; then count=$((count+1)); result=$(sha256sum "$entry") || exit 1; fi; done; [ "$count" = 1 ] || exit 1; printf "%s\\n" "$result"';
    const result = kubectl('-n', 'kodex-system', 'exec', pod.metadata.name, '-c', container.name, '--', 'sh', '-c', script, 'component-manifest', `/tmp/kodex-dev-${name}/build/main`);
    return result.split(/\s/)[0];
  };
  const components = resources.filter(item => ['Deployment', 'StatefulSet', 'DaemonSet'].includes(item.kind)).sort((a,b) => `${a.kind}/${a.metadata.name}`.localeCompare(`${b.kind}/${b.metadata.name}`)).map(item => projectWorkload(item, resources, inspect, executable));
  const after = get('deployments,statefulsets,daemonsets,replicasets,pods', '-n', 'kodex-system').items;
  const stable = items => {
    const workloads = items.filter(item => ['Deployment', 'StatefulSet', 'DaemonSet'].includes(item.kind));
    const pods = workloads.flatMap(workload => workloadPods(workload, items));
    return [...workloads, ...pods].map(item => ({ uid:item.metadata.uid, spec:item.spec,
      statuses:item.status?.containerStatuses, initStatuses:item.status?.initContainerStatuses,
      ready:item.status?.conditions?.find(condition=>condition.type==='Ready') })).sort((a,b)=>a.uid.localeCompare(b.uid));
  };
  requireValue(fingerprint(stable(resources)) === fingerprint(stable(after)), 'WORKLOAD_CHANGED_DURING_READBACK');
  for (const [root, source] of cache)
    requireValue(inspectSource(root).revision === source.revision, 'SOURCE_CHANGED_DURING_READBACK');
  for (const workload of resources.filter(item => ['Deployment', 'StatefulSet', 'DaemonSet'].includes(item.kind))) {
    const component = components.find(item => item.kind === workload.kind && item.name === workload.metadata.name);
    const hotContainers = [...(workload.spec.template.spec.containers ?? []), ...(workload.spec.template.spec.initContainers ?? [])]
      .filter(container => container.command?.includes('/workspace/tools/dev/run-go-hot-reload.sh'));
    for (const pod of workloadPods(workload, after)) for (const container of hotContainers)
      requireValue(executable(pod, container) === component.images.find(image => image.name === container.name).binarySHA256, 'EXECUTABLE_CHANGED_DURING_READBACK');
  }
  const actual = { version: 1, profile: 'component-revisions', clusterUID: get('namespace', 'kube-system').metadata.uid, namespace: 'kodex-system', namespaceUID: namespace.metadata.uid, components, compatibility: command === 'inventory' ? null : validateCompatibility(compatibility, components) };
  if (expected) verifyManifest(expected, actual);
  requireValue(Date.now() < deadline, 'READBACK_BUDGET_EXHAUSTED');
  const pinned = expected ?? actual;
  const identity = Object.fromEntries(['version','profile','clusterUID','namespace','namespaceUID','components','compatibility'].map(key => [key,pinned[key]]));
  const evidence = { ...actual, status: command === 'inventory' ? 'INVENTORIED' : command === 'capture' ? 'CAPTURED' : 'PASS', timestampUTC: new Date().toISOString(), manifestSHA256: fingerprint(identity), servingEvidence: 'source-mount-and-running-binary-readback' };
  writeFileSync(options['--output'], `${JSON.stringify(evidence, null, 2)}\n`, { flag: 'wx', mode: 0o600 });
  process.stdout.write(`Component manifest ${evidence.status}: ${evidence.manifestSHA256}\n`);
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(process.argv.slice(2)); } catch (error) { process.stderr.write(`Component manifest failed: ${/^[A-Z0-9_]+$/.test(error.message) ? error.message : 'READBACK_FAILED'}\n`); process.exitCode = 1; }
}
