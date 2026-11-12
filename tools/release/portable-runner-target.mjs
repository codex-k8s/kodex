import { fingerprint } from './scoped-release.mjs';

const validUUID = (value) => /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(value ?? '');
const requireValue = (value) => { if (!value) throw new Error('PORTABLE_RUNNER_TARGET_REJECTED'); };
const key = (resource) => [resource.apiVersion, resource.kind, resource.metadata?.namespace ?? '', resource.metadata?.name].join('|');

// Явный operator scope закрепляет только существующие объекты одной установки.
// Проверка не меняет labels, policy, grants или правила готовности исполнителей.
export function portableRunnerTarget(profile, { context, clusterUID, namespaceUID, installation }) {
  requireValue(profile && Object.keys(profile).sort().join(',') === 'clusterUID,context,installationSHA256,installationUID,namespaceUID,resources,schema' &&
    profile.schema === 'kodex.portable-runner-target/v1' && profile.context === context &&
    validUUID(clusterUID) && profile.clusterUID === clusterUID && validUUID(namespaceUID) && profile.namespaceUID === namespaceUID &&
    installation?.apiVersion === 'v1' && installation.kind === 'ConfigMap' &&
    installation.metadata?.namespace === 'kodex-system' && installation.metadata.name === 'kodex-installation-profile' &&
    installation.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' &&
    installation.metadata.labels?.['app.kubernetes.io/managed-by'] === 'kodex-install' &&
    !installation.metadata.deletionTimestamp && validUUID(installation.metadata.uid) &&
    profile.installationUID === installation.metadata.uid && profile.installationSHA256 === fingerprint(installation.data) &&
    Array.isArray(profile.resources) && profile.resources.length > 0 && profile.resources.length <= 16);
  const allowed = new Map();
  for (const item of profile.resources) {
    requireValue(item && Object.keys(item).sort().join(',') === 'apiVersion,kind,name,uid' &&
      ['v1|ConfigMap', 'apps/v1|Deployment', 'rbac.authorization.k8s.io/v1|Role', 'supplychain.kodex.dev/v1alpha1|ImageAdmissionPolicyParameters'].includes(item.apiVersion + '|' + item.kind) &&
      /^[a-z0-9][a-z0-9.-]{0,252}$/.test(item.name) && validUUID(item.uid));
    const identity = [item.apiVersion, item.kind, 'kodex-system', item.name].join('|');
    requireValue(!allowed.has(identity)); allowed.set(identity, item.uid);
  }
  return Object.freeze({ digest: fingerprint(profile), allows(resource) {
    const m = resource?.metadata;
    return Boolean(m && m.namespace === 'kodex-system' && !m.deletionTimestamp &&
      m.labels?.['app.kubernetes.io/part-of'] === 'kodex' &&
      m.labels?.['kodex.dev/profile'] === 'web-only' && m.labels?.['kodex.dev/environment'] === 'production' &&
      validUUID(m.uid) && allowed.get(key(resource)) === m.uid && /^\d+$/.test(m.resourceVersion ?? ''));
  } });
}
