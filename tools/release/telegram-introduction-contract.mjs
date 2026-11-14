import {resolve} from 'node:path';
import {isIP} from 'node:net';
import {fingerprint} from './scoped-release.mjs';

export const telegramResourceCreateKeys=Object.freeze([
 'v1|ServiceAccount|interaction-gateway','v1|ConfigMap|interaction-gateway-runtime',
 'networking.k8s.io/v1|NetworkPolicy|interaction-gateway-default-deny',
 'networking.k8s.io/v1|NetworkPolicy|interaction-gateway-exact-runtime-paths',
 'networking.k8s.io/v1|NetworkPolicy|interaction-gateway-internal-rpc-authority-exact-paths',
 'v1|Service|interaction-gateway','policy/v1|PodDisruptionBudget|interaction-gateway',
 'monitoring.coreos.com/v1|ServiceMonitor|interaction-gateway','monitoring.coreos.com/v1|PrometheusRule|interaction-gateway',
 'apps/v1|Deployment|interaction-gateway',
]);

// Это проверка формы immutable metadata, а не authorization. Полный admission
// отдельно перечитывает source/publication/create journal и actual owned objects.
export function validateTelegramIntroducedReceiverProof(proof,sourceSHA){
 const fail=ok=>{if(!ok)throw Error('TELEGRAM_INTRODUCTION_PROOF_INVALID');};
 const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&fingerprint(Object.keys(value).sort())===fingerprint([...keys].sort());
 const digest=v=>/^[a-f0-9]{64}$/.test(v??''),uid=v=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v??'');
 fail(exact(proof,['schema','state','sourceSHA','planPath','evidencePath','publicationPath','publicationSHA256','createPlanSHA256','createID','targetSHA256','deploymentUID','specSHA256','imageRef','intendedReplicas','resources'])&&
  proof.schema==='kodex.telegram-introduced-receiver/v1'&&proof.state==='PENDING_AUTHORITY'&&proof.sourceSHA===sourceSHA&&/^[a-f0-9]{40}$/.test(sourceSHA??'')&&
  proof.intendedReplicas===3&&uid(proof.createID)&&uid(proof.deploymentUID)&&
  ['publicationSHA256','createPlanSHA256','targetSHA256','specSHA256'].every(k=>digest(proof[k]))&&
  ['planPath','evidencePath','publicationPath'].every(k=>typeof proof[k]==='string'&&proof[k]===resolve(proof[k])&&!/[\r\n\0]/.test(proof[k]))&&
  new Set([proof.planPath,proof.evidencePath,proof.publicationPath]).size===3&&
  /^registry\.local\.kodex\/kodex\/interaction-gateway@sha256:[a-f0-9]{64}$/.test(proof.imageRef??'')&&
  !proof.imageRef.endsWith('0'.repeat(64))&&Array.isArray(proof.resources)&&proof.resources.length===telegramResourceCreateKeys.length);
 fail(fingerprint(proof.resources.map(r=>r.resource))===fingerprint(telegramResourceCreateKeys)&&new Set(proof.resources.map(r=>r.uid)).size===telegramResourceCreateKeys.length);
 for(const resource of proof.resources){
  const service=resource.resource==='v1|Service|interaction-gateway';
  fail(exact(resource,['resource','state','uid','resourceSHA256',...(service?['assigned']:[])])&&resource.state==='CREATED_DORMANT'&&uid(resource.uid)&&digest(resource.resourceSHA256));
  if(service){const a=resource.assigned;fail(exact(a,['clusterIP','clusterIPs','ipFamilies','ipFamilyPolicy'])&&isIP(a.clusterIP)!==0&&
   fingerprint(a.clusterIPs)===fingerprint([a.clusterIP])&&fingerprint(a.ipFamilies)===fingerprint([isIP(a.clusterIP)===4?'IPv4':'IPv6'])&&a.ipFamilyPolicy==='SingleStack');}
 }
 fail(proof.resources.at(-1).uid===proof.deploymentUID);return proof;
}
