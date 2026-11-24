import {resolve} from 'node:path';
import {isIP} from 'node:net';
import {fingerprint} from './scoped-release.mjs';
import {logicalRegistryHost} from './stable-node-registry-model.mjs';
import {promotedServicePullRegistry} from './component-node-qualification.mjs';

export const telegramResourceCreateKeys=Object.freeze([
 'v1|ServiceAccount|interaction-gateway','v1|ConfigMap|interaction-gateway-runtime',
 'networking.k8s.io/v1|NetworkPolicy|interaction-gateway-default-deny',
 'networking.k8s.io/v1|NetworkPolicy|interaction-gateway-exact-runtime-paths',
 'networking.k8s.io/v1|NetworkPolicy|interaction-gateway-internal-rpc-authority-exact-paths',
 'v1|Service|interaction-gateway','policy/v1|PodDisruptionBudget|interaction-gateway',
 'monitoring.coreos.com/v1|ServiceMonitor|interaction-gateway','monitoring.coreos.com/v1|PrometheusRule|interaction-gateway',
 'apps/v1|Deployment|interaction-gateway',
]);

const telegramResourceKeysWithoutMonitoring=Object.freeze(telegramResourceCreateKeys.filter(key=>!key.startsWith('monitoring.coreos.com/')));

export function telegramResourceKeysFor(monitoringResources) {
 if(monitoringResources===undefined)return telegramResourceCreateKeys;
 if(monitoringResources==='DISABLED')return telegramResourceKeysWithoutMonitoring;
 throw Error('TELEGRAM_MONITORING_RESOURCES_INVALID');
}

// Допускаются два штатных адреса одной promoted repository.
export function isTelegramInteractionImage(ref, registry) {
 if(typeof ref!=='string'||ref.length>512)return false;
 const [host,repository,image,...additionalParts]=ref.split('/');
 if(![logicalRegistryHost,promotedServicePullRegistry].includes(host)||registry!==undefined&&registry!==host)return false;
 return repository==='kodex'&&additionalParts.length===0&&/^interaction-gateway@sha256:[a-f0-9]{64}$/.test(image??'')&&!image.endsWith('0'.repeat(64));
}

// Установленный publisher определяет immutable authority image для sidecars.
// В новом режиме это по-прежнему exact live ref из Deployment выбранной установки.
export function isTelegramAuthorityImage(ref, registry) {
 if(typeof ref!=='string'||ref.length>512||ref.endsWith('0'.repeat(64)))return false;
 const logical=/^registry\.local\.kodex\/kodex\/internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(ref);
 return logical||registry===promotedServicePullRegistry&&
  /^[a-z0-9-]+\.[a-z0-9-]+\.svc\.cluster\.local\/(?:[a-z0-9_-]+\/)*internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(ref);
}

// Это проверка формы immutable metadata, а не authorization. Полный admission
// отдельно перечитывает source/publication/create journal и actual owned objects.
export function validateTelegramIntroducedReceiverProof(proof,sourceSHA){
 const fail=ok=>{if(!ok)throw Error('TELEGRAM_INTRODUCTION_PROOF_INVALID');};
 const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&fingerprint(Object.keys(value).sort())===fingerprint([...keys].sort());
 const monitoring=Object.hasOwn(proof??{},'monitoringResources');
 fail(!monitoring||proof.monitoringResources==='DISABLED');
 const keys=telegramResourceKeysFor(proof?.monitoringResources);
 const digest=v=>/^[a-f0-9]{64}$/.test(v??''),uid=v=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v??'');
 fail(exact(proof,['schema','state','sourceSHA','planPath','evidencePath','publicationPath','publicationSHA256','createPlanSHA256','createID','targetSHA256','deploymentUID','specSHA256','imageRef','intendedReplicas','resources',...(monitoring?['monitoringResources']:[])])&&
  proof.schema==='kodex.telegram-introduced-receiver/v1'&&proof.state==='PENDING_AUTHORITY'&&proof.sourceSHA===sourceSHA&&/^[a-f0-9]{40}$/.test(sourceSHA??'')&&
  proof.intendedReplicas===3&&uid(proof.createID)&&uid(proof.deploymentUID)&&
  ['publicationSHA256','createPlanSHA256','targetSHA256','specSHA256'].every(k=>digest(proof[k]))&&
  ['planPath','evidencePath','publicationPath'].every(k=>typeof proof[k]==='string'&&proof[k]===resolve(proof[k])&&!/[\r\n\0]/.test(proof[k]))&&
  new Set([proof.planPath,proof.evidencePath,proof.publicationPath]).size===3&&
  isTelegramInteractionImage(proof.imageRef)&&Array.isArray(proof.resources)&&proof.resources.length===keys.length);
 fail(fingerprint(proof.resources.map(r=>r.resource))===fingerprint(keys)&&new Set(proof.resources.map(r=>r.uid)).size===keys.length);
 for(const resource of proof.resources){
  const service=resource.resource==='v1|Service|interaction-gateway';
  fail(exact(resource,['resource','state','uid','resourceSHA256',...(service?['assigned']:[])])&&resource.state==='CREATED_DORMANT'&&uid(resource.uid)&&digest(resource.resourceSHA256));
  if(service){const a=resource.assigned;fail(exact(a,['clusterIP','clusterIPs','ipFamilies','ipFamilyPolicy'])&&isIP(a.clusterIP)!==0&&
   fingerprint(a.clusterIPs)===fingerprint([a.clusterIP])&&fingerprint(a.ipFamilies)===fingerprint([isIP(a.clusterIP)===4?'IPv4':'IPv6'])&&a.ipFamilyPolicy==='SingleStack');}
 }
 fail(proof.resources.at(-1).uid===proof.deploymentUID);return proof;
}
