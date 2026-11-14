import { constants, openSync, closeSync, fstatSync, readFileSync, writeFileSync, fsyncSync, realpathSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { resolve, dirname } from 'node:path';
import { portableRunnerTarget } from './portable-runner-target.mjs';
import { fingerprint } from './scoped-release.mjs';
import { readAuthorityPolicyEnvironment, renderAuthorityPolicyEnvironment } from './authority-policy-environment.mjs';
import { readComponentServingInventory } from './portable-component-migration.mjs';

export const catalogPolicyTransition = 'MODEL_DISCOVERY_105_106';
const policies = Object.freeze({
  predecessor: { revision:105, sha256:'c63153741d7ed867cba59ac87fcfda3e2ca50e9c5d82bb1bc064ed62926332cf', path:'./fixtures/authority-policy-revision105.json' },
  desired: { revision:106, sha256:'bd5ca7ecd2bdaa9a0d7cc9d097e11437c52be6fb3de53181551cb35b3ae315be', path:'./fixtures/authority-policy-revision106.json' },
});
const digest = raw => createHash('sha256').update(raw).digest('hex');
const requireValue = (ok, code) => { if (!ok) throw new Error(code); };

// Точный источник фиксируется независимо от runtime OIDC render. Другой revision,
// неизвестное поле, duplicate key или изменение whitespace не становятся новым допуском.
export function catalogPolicySource(role) {
  const policy = policies[role];
  requireValue(policy !== undefined, 'CATALOG_POLICY_ROLE_REJECTED');
  const raw = readFileSync(new URL(policy.path, import.meta.url), 'utf8');
  requireValue(digest(raw) === policy.sha256, 'CATALOG_POLICY_SOURCE_REJECTED');
  return { raw, revision:policy.revision, sourceSHA256:policy.sha256 };
}

export function validateCatalogPolicy(raw, role, transition) {
  requireValue(transition?.profile === catalogPolicyTransition && typeof raw === 'string', 'CATALOG_POLICY_TRANSITION_REQUIRED');
  const source = catalogPolicySource(role);
  const rendered = renderAuthorityPolicyEnvironment(source.raw, transition.environment);
  requireValue(raw === rendered.raw, 'CATALOG_POLICY_ENVIRONMENT_BYTES_REJECTED');
  return { revision:source.revision, sha256:rendered.renderedSHA256, sourceSHA256:source.sourceSHA256 };
}

// Приватные operator plans читаются тем же fd после NOFOLLOW/fstat, без TOCTOU reread.
export function readAuthorityPrivateJSON(path) {
  return parseAuthorityPrivateJSON(readAuthorityPrivateText(path));
}

// JSON.parse проверяет синтаксис, затем отдельный token scan закрывает duplicate
// keys, включая одинаковые Unicode keys с разным escape-представлением.
export function parseAuthorityPrivateJSON(raw) {
  let parsed;
  try {parsed=JSON.parse(raw);} catch {throw new Error('AUTHORITY_PRIVATE_JSON_REJECTED');}
  const stack=[],quoted=/"(?:[^"\\]|\\.)*"/y;
  for(let index=0;index<raw.length;index++) {
    const character=raw[index];
    if(character==='"') {
      quoted.lastIndex=index;const match=quoted.exec(raw);
      requireValue(match,'AUTHORITY_PRIVATE_JSON_REJECTED');
      const top=stack.at(-1);
      if(top?.keys&&top.key) {
        const key=JSON.parse(match[0]);requireValue(!top.keys.has(key),'AUTHORITY_PRIVATE_JSON_REJECTED');
        top.keys.add(key);top.key=false;
      }
      index=quoted.lastIndex-1;
    } else if(character==='{'||character==='[') {
      requireValue(stack.length<64,'AUTHORITY_PRIVATE_JSON_REJECTED');
      stack.push(character==='{'?{keys:new Set(),key:true}:{});
    } else if(character==='}'||character===']') stack.pop();
    else if(character===','&&stack.at(-1)?.keys)stack.at(-1).key=true;
  }
  return parsed;
}

export function readAuthorityPrivateText(path) {
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = fstatSync(fd);
    requireValue(stat.isFile() && stat.nlink === 1 && stat.uid === process.getuid() &&
      (stat.mode & 0o077) === 0 && stat.size > 0 && stat.size <= 4 << 20, 'AUTHORITY_PRIVATE_INPUT_REQUIRED');
    return readFileSync(fd, 'utf8');
  } finally { closeSync(fd); }
}

export function syncAuthorityJournalDirectory(path) {
  const directory=dirname(resolve(path));
  requireValue(directory===realpathSync(directory),'AUTHORITY_PRIVATE_DIRECTORY_REQUIRED');
  const fd=openSync(directory,constants.O_RDONLY|constants.O_DIRECTORY|constants.O_NOFOLLOW);
  try {
    const stat=fstatSync(fd);
    requireValue(stat.isDirectory()&&stat.uid===process.getuid()&&(stat.mode&0o077)===0,'AUTHORITY_PRIVATE_DIRECTORY_REQUIRED');
    fsyncSync(fd);
  } finally {closeSync(fd);}
}

// Plan и его directory entry устойчивы до первой mutation, а не только после close().
export function writeAuthorityPrivatePlan(path,plan) {
  syncAuthorityJournalDirectory(path);
  const fd=openSync(path,constants.O_CREAT|constants.O_EXCL|constants.O_WRONLY|constants.O_NOFOLLOW,0o600);
  try {writeFileSync(fd,JSON.stringify(plan,null,2)+'\n');fsyncSync(fd);}
  finally {closeSync(fd);}
  syncAuthorityJournalDirectory(path);
}

// Mutations используют тот же root-owned positive writer join, что закрывает API.
// Marker/replicas сами по себе не доказывают завершение всех старых API Pods.
export async function readCatalogMaintenance(planPath,evidencePath,kube,{trafficPlanPath,trafficEvidencePath}={}) {
  const plan=readAuthorityPrivateJSON(planPath),text=readAuthorityPrivateText(evidencePath);
  const events=text.split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
  const deployment=JSON.parse(kube(['-n','kodex-system','get','deployment','control-api-gateway','-o','json']));
  const resources=readComponentServingInventory(kube,'control-api-gateway',deployment.metadata.uid);
  let traffic;
  if(trafficPlanPath||trafficEvidencePath) {
    const trafficPlan=readAuthorityPrivateJSON(trafficPlanPath);
    const trafficEvents=readAuthorityPrivateText(trafficEvidencePath).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
    const service=JSON.parse(kube(['-n','kodex-system','get','service','control-api-gateway','-o','json']));
    const maintenanceID=service.spec?.selector?.['kodex.dev/component-traffic-isolation'];
    requireValue(/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(maintenanceID??''),'CATALOG_API_ADMISSION_PROOF_REQUIRED');
    const slices=JSON.parse(kube(['-n','kodex-system','get','endpointslices','-l','kubernetes.io/service-name=control-api-gateway','-o','json']));
    const pods=JSON.parse(kube(['-n','kodex-system','get','pods','-l',`kodex.dev/component-traffic-isolation=${maintenanceID}`,'-o','json']));
    traffic={plan:trafficPlan,events:trafficEvents,service,slices,pods};
  }
  const {requireClosedAPIAdmission}=await import('./component-api-traffic.mjs');
  return {planPath,evidencePath,trafficPlanPath,trafficEvidencePath,proof:requireClosedAPIAdmission(deployment,resources,{plan,events},traffic)};
}

export function verifyCatalogAdmission(maintenance,transition) {
  const resources=transition.targetProfile.resources;
  const api=resources.find(resource=>resource.apiVersion==='apps/v1'&&resource.kind==='Deployment'&&resource.name==='control-api-gateway');
  const service=resources.find(resource=>resource.apiVersion==='v1'&&resource.kind==='Service'&&resource.name==='control-api-gateway');
  requireValue(api&&service&&maintenance.proof?.targetSHA256===transition.targetSHA256&&maintenance.proof.deploymentUID===api.uid&&
    maintenance.proof.serviceUID===service.uid&&/^[a-f0-9]{64}$/.test(maintenance.proof.trafficPlanSHA256??'')&&
    typeof maintenance.trafficPlanPath==='string'&&typeof maintenance.trafficEvidencePath==='string','CATALOG_API_ADMISSION_PROOF_REQUIRED');
}

// Каждая строка принадлежит одному immutable plan; fsync предшествует сетевому effect.
// После потерянного ACK resume сначала проверяет уже записанные UID и exact readback.
export function openCatalogProjectionJournal(path,plan,resume=false) {
  const planSHA256=fingerprint(plan);
  syncAuthorityJournalDirectory(path);
  const fd=openSync(path,resume?constants.O_APPEND|constants.O_RDWR|constants.O_NOFOLLOW:constants.O_CREAT|constants.O_EXCL|constants.O_WRONLY|constants.O_NOFOLLOW,0o600);
  const stat=fstatSync(fd);
  if(!stat.isFile()||stat.nlink!==1||stat.uid!==process.getuid()||(stat.mode&0o077)!==0||stat.size>4<<20){closeSync(fd);throw new Error('AUTHORITY_PRIVATE_INPUT_REQUIRED');}
  let events;
  try {
    events=resume?readFileSync(fd,'utf8').split('\n').filter(Boolean).map(parseAuthorityPrivateJSON):[];
    if(resume)validateCatalogProjectionEvents(events,plan);
  } catch(error){closeSync(fd);throw error;}
  syncAuthorityJournalDirectory(path);
  return {events,append(event){requireValue(events.length<16384,'POLICY_PROJECTION_JOURNAL_BOUND_EXCEEDED');const row={...event,planSHA256,at:new Date().toISOString()};writeFileSync(fd,JSON.stringify(row)+'\n');fsyncSync(fd);events.push(row);},close(){closeSync(fd);}};
}

export function validateCatalogProjectionEvents(events,plan) {
  const hash=fingerprint(plan),acknowledged=new Map();
  requireValue(events.length>0&&events.length<16384&&events[0].status==='INTENT'&&events[0].operation==='APPLY','POLICY_PROJECTION_JOURNAL_DRIFT');
  for(const event of events) {
    requireValue(event.planSHA256===hash&&['INTENT','ACK','UNKNOWN'].includes(event.status)&&
      ['APPLY','RESUME','CREATE_PROJECTION','CP_POINTER_CAS'].includes(event.operation)&&
      Object.keys(event).every(key=>['planSHA256','status','operation','uid','at'].includes(key))&&
      typeof event.at==='string'&&Number.isFinite(Date.parse(event.at))&&new Date(event.at).toISOString()===event.at,'POLICY_PROJECTION_JOURNAL_DRIFT');
    if(['APPLY','RESUME'].includes(event.operation))requireValue(event.status==='INTENT'&&!Object.hasOwn(event,'uid'),'POLICY_PROJECTION_JOURNAL_DRIFT');
    else if(event.status==='ACK') {
      requireValue(/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(event.uid??'')&&
        (!acknowledged.has(event.operation)||acknowledged.get(event.operation)===event.uid)&&
        (event.operation!=='CP_POINTER_CAS'||acknowledged.has('CREATE_PROJECTION')),'POLICY_PROJECTION_JOURNAL_DRIFT');
      acknowledged.set(event.operation,event.uid);
    } else requireValue(!Object.hasOwn(event,'uid'),'POLICY_PROJECTION_JOURNAL_DRIFT');
  }
  return acknowledged;
}

// portableRunnerTarget уже связывает context, cluster/ns UID и installation CM UID/data.
// Все три существующих изменяемых ресурса дополнительно должны входить в exact allowlist.
export function readCatalogTarget(profile, context, get) {
  const cluster = get('namespace', 'kube-system');
  const namespace = get('namespace', 'kodex-system');
  const installation = get('configmap', 'kodex-installation-profile');
  const target = portableRunnerTarget(profile, {context, clusterUID:cluster.metadata.uid,
    namespaceUID:namespace.metadata.uid, installation});
  const deployment = get('deployment', 'control-plane');
  const publisher = get('deployment', 'internal-rpc-authority-publisher');
  const registry = get('configmap', 'internal-rpc-authority-publisher-target-registry');
  requireValue([deployment, publisher, registry].every(resource => target.allows(resource)), 'CATALOG_AUTHORITY_TARGET_REJECTED');
  const app = deployment.spec.template.spec.containers.find(container => container.name === 'control-plane');
  const names = [...new Set((app?.env ?? []).filter(field =>
    ['CONTROL_PLANE_OIDC_ISSUER','CONTROL_PLANE_OIDC_AUDIENCE'].includes(field.name))
    .map(field => field.valueFrom?.configMapKeyRef?.name).filter(Boolean))];
  const configMaps = names.map(name => get('configmap', name));
  const environment = readAuthorityPolicyEnvironment(deployment, configMaps, target);
  return {target, deployment, publisher, registry, configMaps,
    transition:{profile:catalogPolicyTransition, environment, targetProfile:profile,
      targetSHA256:target.digest, clusterUID:cluster.metadata.uid, namespaceUID:namespace.metadata.uid,
      installationUID:installation.metadata.uid, installationSHA256:fingerprint(installation.data)},
  };
}

export function catalogKubeconfig(path) {
  requireValue(typeof path === 'string' && path === resolve(path), 'EXPLICIT_KUBECONFIG_REQUIRED');
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try { requireValue(fstatSync(fd).isFile(), 'EXPLICIT_KUBECONFIG_REQUIRED'); }
  finally { closeSync(fd); }
  return path;
}

// CP rollout меняет RV, но не вправе менять public OIDC source, его CM versions
// или installation/cluster/namespace/CP UID во время той же authority operation.
export function verifyCatalogTransition(expected, actual) {
  const normalized = structuredClone(actual);
  normalized.environment.evidence.resourceVersion = expected.environment.evidence.resourceVersion;
  requireValue(fingerprint(normalized) === fingerprint(expected), 'CATALOG_AUTHORITY_TARGET_DRIFT');
}
