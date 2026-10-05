#!/usr/bin/env node
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { lstatSync, readFileSync, realpathSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// Узкое owner-approved исключение Issue1797: только наблюдаемость сохранённой
// attempt. Ни idle, ни новый допуск, ни изменение authority этим путём не выдаются.
export const SOURCE_ROOT = '/home/s/projects/kodex';
export const API_SERVER = 'https://127.0.0.2:6443';
export const SCRIPT = 'deploy/k8s/base/image-supply-chain/image-admission.sh';
export const BASELINE_SCRIPT_SHA256 = '5e835394f75b34cca3447889f6607007404d8ec39128efad8b4d43ad26314072';
export const CANDIDATE_SCRIPT_SHA256 = '3d61890702c0157e944823a7282bb662865fdd7c333de05daf84840138db5e55';
const oldLine = '  IMAGE_OWNER_ADMISSION_FAILURE_CODE="$failure_code" image-admission-bridge fail 2>/dev/null || return 1\n';
const newLines = '  # Bridge печатает только закрытый gRPC code, без remote error или claim.\n' +
  '  # Сбой callback должен оставаться наблюдаемым, пока durable receipt не получен.\n' +
  '  IMAGE_OWNER_ADMISSION_FAILURE_CODE="$failure_code" image-admission-bridge fail || return 1\n';
const namespace = 'kodex-system', name = 'kodex-image-admission';
const workspace = 'mc-admit-320b1cbe46eb01e9908e39eb630a27b2';
const workspaceUID = '7bba3b9e-a3ff-4102-b484-300b733e416f';
const sha = /^[a-f0-9]{64}$/, uid = /^[a-f0-9]{8}-(?:[a-f0-9]{4}-){3}[a-f0-9]{12}$/;
const check = (value, code) => { if (!value) throw new Error(code); };
export const digest = value => createHash('sha256').update(value).digest('hex');
export function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(key => [key, canonical(value[key])]));
  return value;
}
export const fingerprint = value => digest(JSON.stringify(canonical(value)));

export function verifySourceProof(revision, gitStatus, observedCandidate, candidate, o) {
  check(revision === o.sourceRevision && observedCandidate === candidate, 'SOURCE_REVISION_MISMATCH');
  check(gitStatus === '', 'SOURCE_WORKTREE_DIRTY');
}

export function validateOptions(o) {
  check(['check', 'dry-run', 'apply'].includes(o.mode), 'MODE_INVALID');
  check(o.context === 'k3d-kodex', 'CONTEXT_INVALID');
  check(o.sourceRoot === SOURCE_ROOT && /^[a-f0-9]{40}$/.test(o.sourceRevision ?? ''), 'SOURCE_PIN_INVALID');
  check(o.apiServer === API_SERVER, 'API_SERVER_INVALID');
  for (const key of ['clusterUID', 'namespaceUID', 'configmapUID', 'serverNodeUID', 'agentNodeUID']) check(uid.test(o[key] ?? ''), 'UID_PIN_INVALID');
  check(/^[1-9][0-9]*$/.test(o.resourceVersion ?? '') && sha.test(o.baselineDataSHA256 ?? ''), 'CONFIGMAP_PIN_INVALID');
}

export function candidateConfigMap(current, candidate, o) {
  check(current.apiVersion === 'v1' && current.kind === 'ConfigMap' && current.metadata?.name === name && current.metadata?.namespace === namespace, 'CONFIGMAP_IDENTITY_INVALID');
  check(current.metadata.uid === o.configmapUID && current.metadata.resourceVersion === o.resourceVersion && !current.metadata.deletionTimestamp, 'CONFIGMAP_CAS_MISMATCH');
  check(current.immutable === false && !Object.hasOwn(current, 'binaryData'), 'CONFIGMAP_SHAPE_INVALID');
  const labels = current.metadata.labels;
  check(labels?.['app.kubernetes.io/part-of'] === 'kodex' && labels?.['kodex.dev/local-profile'] === 'hot-reload' && labels?.['kodex.dev/security-profile'] === 'trusted-cluster', 'CONFIGMAP_PROFILE_INVALID');
  check(current.data && Object.values(current.data).every(value => typeof value === 'string') && fingerprint(current.data) === o.baselineDataSHA256, 'CONFIGMAP_DATA_MISMATCH');
  const baseline = current.data['image-admission.sh'];
  check(typeof baseline === 'string' && digest(baseline) === BASELINE_SCRIPT_SHA256, 'SCRIPT_BASELINE_MISMATCH');
  check(baseline.split(oldLine).length === 2 && candidate === baseline.replace(oldLine, newLines) && digest(candidate) === CANDIDATE_SCRIPT_SHA256, 'SCRIPT_CHANGE_NOT_APPROVED');
  const result = structuredClone(current);
  result.data['image-admission.sh'] = candidate;
  return result;
}

export function verifyReadback(actual, expected, beforeRV, dryRun = false) {
  check(actual.metadata?.uid === expected.metadata.uid && actual.metadata?.name === name && actual.metadata?.namespace === namespace, 'READBACK_IDENTITY_MISMATCH');
  check(dryRun || (actual.metadata.resourceVersion !== beforeRV && /^[1-9][0-9]*$/.test(actual.metadata.resourceVersion)), 'READBACK_VERSION_INVALID');
  const normalize = value => {
    const result = structuredClone(value);
    delete result.metadata.resourceVersion;
    delete result.metadata.managedFields;
    return result;
  };
  check(fingerprint(normalize(actual)) === fingerprint(normalize(expected)), 'READBACK_CONTENT_MISMATCH');
}

// Все effect-операции принадлежат одному namespace/name. kubectl stderr никогда
// не выводится: admission warnings/errors могут содержать request или raw data.
export function refresh(o, candidate, io) {
  validateOptions(o);
  const get = (...args) => JSON.parse(io.kubectl(['get', ...args, '-o', 'json']));
  let policyPins;
  const boundary = () => {
    check(io.endpoint() === o.apiServer, 'API_SERVER_MISMATCH');
    check(get('namespace', 'kube-system').metadata?.uid === o.clusterUID, 'CLUSTER_UID_MISMATCH');
    const ns = get('namespace', namespace);
    check(ns.metadata?.uid === o.namespaceUID && !ns.metadata.deletionTimestamp && ns.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' && ns.metadata.labels?.['kodex.dev/local-profile'] === 'hot-reload', 'NAMESPACE_PROFILE_MISMATCH');
    const nodes = get('nodes').items;
    check(Array.isArray(nodes) && nodes.length === 2, 'NODE_SET_MISMATCH');
    for (const [nodeName, nodeUID] of [['k3d-kodex-server-0', o.serverNodeUID], ['k3d-kodex-agent-0', o.agentNodeUID]]) {
      const node = nodes.find(value => value.metadata?.name === nodeName);
      check(node?.metadata.uid === nodeUID && !node.metadata.deletionTimestamp && node.status?.conditions?.some(value => value.type === 'Ready' && value.status === 'True'), 'NODE_IDENTITY_MISMATCH');
    }
    const pvc = get('-n', namespace, 'persistentvolumeclaim', workspace);
    check(pvc.metadata?.uid === workspaceUID && !pvc.metadata.deletionTimestamp && pvc.metadata.labels?.['kodex.dev/image-admission-id'] === workspace.slice(9) && pvc.metadata.annotations?.['kodex.dev/admission-run-sha256'] === '320b1cbe46eb01e9908e39eb630a27b28ca7ece389f5bdcf01d4a67103d14c7e', 'WORKSPACE_IDENTITY_MISMATCH');
    const policies = ['kodex-image-admission-controller-jobs', 'kodex-image-admission-controller-workspaces'].map(policyName => {
      const policy = get('validatingadmissionpolicy', policyName);
      check(policy.metadata?.name === policyName && uid.test(policy.metadata.uid) && !policy.metadata.deletionTimestamp &&
        Number.isInteger(policy.metadata.generation) && policy.metadata.generation > 0 && policy.status?.observedGeneration === policy.metadata.generation &&
        (policy.status.typeChecking?.expressionWarnings ?? []).length === 0, 'ADMISSION_POLICY_NOT_COMPILED');
      return fingerprint(policy);
    });
    check(!policyPins || fingerprint(policyPins) === fingerprint(policies), 'ADMISSION_POLICY_DRIFT');
    policyPins = policies;
  };
  const currentCM = () => get('-n', namespace, 'configmap', name);
  boundary();
  io.source(o);
  const current = currentCM(), expected = candidateConfigMap(current, candidate, o);
  const report = { context: o.context, sourceRevision: o.sourceRevision, configmapUID: o.configmapUID, beforeResourceVersion: o.resourceVersion, beforeDataSHA256: o.baselineDataSHA256, candidateDataSHA256: fingerprint(expected.data), candidateScriptSHA256: digest(candidate) };
  if (o.mode === 'check') return { ...report, status: 'CHECKED' };
  const replace = dryRun => JSON.parse(io.kubectl(['-n', namespace, 'replace', '--validate=strict', ...(dryRun ? ['--dry-run=server'] : []), '-f', '-', '-o', 'json'], JSON.stringify(expected)));
  verifyReadback(replace(true), expected, o.resourceVersion, true);
  boundary();
  io.source(o);
  check(fingerprint(currentCM()) === fingerprint(current), 'PREFLIGHT_DRIFT');
  if (o.mode === 'dry-run') return { ...report, status: 'DRY_RUN_PASS' };
  verifyReadback(replace(false), expected, o.resourceVersion);
  const observed = currentCM();
  verifyReadback(observed, expected, o.resourceVersion);
  boundary();
  io.source(o);
  return { ...report, resourceVersion: observed.metadata.resourceVersion, status: 'APPLIED' };
}

function main() {
  const names = { context: 'context', 'source-root': 'sourceRoot', 'source-revision': 'sourceRevision', 'api-server': 'apiServer', 'cluster-uid': 'clusterUID', 'namespace-uid': 'namespaceUID', 'configmap-uid': 'configmapUID', 'resource-version': 'resourceVersion', 'baseline-data-sha256': 'baselineDataSHA256', 'server-node-uid': 'serverNodeUID', 'agent-node-uid': 'agentNodeUID' };
  const args = process.argv.slice(2), o = { mode: args.shift() };
  while (args.length) {
    const flag = args.shift(), key = names[flag?.slice(2)];
    check(flag?.startsWith('--') && key && !Object.hasOwn(o, key) && args.length, 'ARGUMENTS_INVALID');
    o[key] = args.shift();
  }
  validateOptions(o);
  check(realpathSync(SOURCE_ROOT) === SOURCE_ROOT && fileURLToPath(import.meta.url) === `${SOURCE_ROOT}/tools/dev/refresh-image-admission-diagnostics.mjs`, 'ENTRYPOINT_SOURCE_MISMATCH');
  const deadline = Date.now() + 60000;
  const run = (command, args, input) => {
    check(Date.now() < deadline, 'BUDGET_EXHAUSTED');
    const result = spawnSync(command, args, { input, encoding: 'utf8', timeout: Math.min(10000, deadline - Date.now()), maxBuffer: 2 << 20, stdio: ['pipe', 'pipe', 'pipe'] });
    check(!result.error && result.status === 0, 'COMMAND_FAILED');
    check(!result.stderr?.trim(), 'COMMAND_WARNING');
    return result.stdout.trim();
  };
  const kubectl = '/home/s/.local/state/kodex-dev/tools/bin/kubectl';
  const base = ['--kubeconfig=/home/s/.kube/config', '--context=k3d-kodex', `--server=${o.apiServer}`, '--request-timeout=8s'];
  check(JSON.parse(run(kubectl, ['version', '--client', '-o', 'json'])).clientVersion?.gitVersion === 'v1.35.5', 'KUBECTL_VERSION_MISMATCH');
  const readCandidate = () => {
    const path = `${SOURCE_ROOT}/${SCRIPT}`, stat = lstatSync(path);
    check(stat.isFile() && !stat.isSymbolicLink() && realpathSync(path) === path && stat.size < (256 << 10), 'SOURCE_FILE_INVALID');
    return readFileSync(path, 'utf8');
  };
  const candidate = readCandidate();
  const result = refresh(o, candidate, {
    kubectl: (args, input) => run(kubectl, [...base, ...args], input),
    endpoint: () => run(kubectl, ['--kubeconfig=/home/s/.kube/config', '--context=k3d-kodex', 'config', 'view', '--minify', '-o', 'jsonpath={.clusters[0].cluster.server}']),
    source: () => verifySourceProof(run('git', ['-C', SOURCE_ROOT, 'rev-parse', 'HEAD']),
      run('git', ['-C', SOURCE_ROOT, 'status', '--porcelain=v1', '--untracked-files=all', '--ignore-submodules=none']), readCandidate(), candidate, o),
  });
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(); } catch (error) {
    // Только собственный закрытый code, никогда Error.message внешней команды.
    const code = /^[A-Z_]+$/.test(error?.message ?? '') ? error.message : 'REFRESH_FAILED';
    process.stderr.write(`Image admission diagnostics refresh failed: ${code}\n`);
    process.exitCode = 1;
  }
}
