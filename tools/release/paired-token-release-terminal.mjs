import {performance} from 'node:perf_hooks';
import {fingerprint} from './scoped-release.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {componentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {requireOpenedAPIMaintenance} from './component-api-maintenance.mjs';
import {requireClosedAPIAdmission} from './component-api-traffic.mjs';
import {sameCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';
import {pairedTokenReleaseOwnerDeadline,verifyPairedTokenReleaseOwner} from './paired-token-release-plan.mjs';
import {inspectPairedTokenForwardReceipt} from './paired-token-release-receiver.mjs';

const ensure=(value,code)=>{if(!value)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const proofs=new WeakMap();
const terminalFlights=new WeakSet();
const preparationCeiling=30000;
const finalCeiling=5000;
const apiChanged='PAIRED_TERMINAL_API_CHANGED';
const running=pods=>pods.every(p=>p.status?.phase==='Running'&&!['Unknown','NodeLost'].includes(p.status.reason)&&
 !p.status.conditions?.some(c=>c.status==='Unknown'));

// Это terminal readback, а не разрешение mutation: функции patch/restore здесь
// отсутствуют. Сериализация metadata не переносит свежесть private proof.
export async function readPairedTokenReleaseTerminal({manifest,record,journal},ports){
 ensure(journal&&typeof journal==='object'&&!terminalFlights.has(journal),'PAIRED_TERMINAL_BUSY');
 terminalFlights.add(journal);
 try{
  const began=Date.now(),preparationLimit=performance.now()+preparationCeiling;
  let until=preparationLimit,wallBegan=began,wallUntil=began+preparationCeiling;
  const original={manifest,record},frozen=structuredClone(original),seal=fingerprint(frozen);
  manifest=frozen.manifest;record=frozen.record;
  ensure(ports&&['readOwner','recheckOwnerEpoch','readSource','readObservation','readFinalObservation'].every(k=>typeof ports[k]==='function'),
   'PAIRED_TERMINAL_READERS_REQUIRED');
  const initial=inspectPairedTokenForwardReceipt({manifest,record,journal});
  function fresh(){
   ensure(performance.now()<until&&Date.now()>=wallBegan&&Date.now()<wallUntil,'PAIRED_TERMINAL_EXPIRED');
   ensure(fingerprint(original)===seal&&same(inspectPairedTokenForwardReceipt({manifest,record,journal}),initial),
    'PAIRED_TERMINAL_RECEIPT_CHANGED');
  }
  async function source(){
   const value=await ports.readSource({deadline:until});fresh();
   ensure(value?.sourceSHA===manifest.sourceSHA&&value.executorSHA===record.executorSHA,'PAIRED_TERMINAL_SOURCE_CHANGED');
  }
  const admitted=[];
  async function owner(){
   const value=verifyPairedTokenReleaseOwner(manifest,await ports.readOwner(manifest,{deadline:until}));fresh();
   const bound=initial.binding;
   ensure(sameCatalogAuthorityBaseline(record.plan.authority,value.authority)&&
    same(bound.postgres,value.controlPlane.postgres)&&same(bound.backend,value.controlPlane.backend)&&
    same(bound.authorityHead,value.authorityHead),'PAIRED_TERMINAL_OWNER_CHANGED');
   // Новое чтение не продлевает первоначальную authority/монотонный потолок.
   for(const previous of admitted)verifyPairedTokenReleaseOwner(manifest,previous);
   until=Math.min(until,pairedTokenReleaseOwnerDeadline(manifest,value));
   wallUntil=Math.min(wallUntil,value.began+preparationCeiling);
   admitted.push(value);return value;
  }
  function components(observed){
   const result=[];
   for(const expected of initial.components){
    const current=observed[expected.name];
    const d=current?.deployment,p=record.plan.components.find(p=>p.name===expected.name);
    ensure(d?.apiVersion==='apps/v1'&&d.kind==='Deployment'&&d.metadata?.name===p.name&&
     d.metadata.namespace==='kodex-system'&&d.metadata.uid===p.uid&&!d.metadata.deletionTimestamp&&
     /^[1-9][0-9]{0,19}$/.test(d.metadata.resourceVersion??'')&&same(d.spec,p.afterSpec)&&
     d.metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&d.metadata.labels?.['kodex.dev/profile']==='web-only'&&
     d.metadata.labels?.['kodex.dev/environment']==='production','PAIRED_TERMINAL_COMPONENT_CHANGED');
    const pods=activeDeploymentPods(componentServingInventory(current.resources,p.name,p.uid),p.uid);
    ensure(componentServingReady(d,pods,p.afterSpec.replicas)&&running(pods)&&
     same(pods.map(p=>p.metadata.uid).sort(),expected.podUIDs),'PAIRED_TERMINAL_SERVING_CHANGED');
    result.push({...expected,resourceVersion:d.metadata.resourceVersion});
   }
   return result;
  }
  function api(value){
   const scope=portableRunnerTarget(manifest.target,value.observed),d=value.deployment;
   ensure(scope.allows(d)&&scope.allows(value.service)&&value.opened?.plan.sourceSHA===record.executorSHA&&
    fingerprint(value.opened.plan.beforeSpec)===manifest.apiSpecSHA256&&
    same(value.opened.plan.beforeAnnotations,value.closed?.plan.afterAnnotations)&&
    ['context','clusterUID','namespaceUID','installationUID','installationSHA256'].every(k=>
     value.closed?.plan.target?.[k]===manifest.target[k]),apiChanged);
   const traffic={...value.traffic,service:value.service,slices:value.slices,pods:value.pods};
   const closed=requireClosedAPIAdmission(d,value.resources,value.closed,traffic);
   ensure(same(closed,record.plan.apiAdmission),apiChanged);
   const opened=requireOpenedAPIMaintenance(d,value.resources,value.closed,value.opened,traffic);
   const pods=activeDeploymentPods(componentServingInventory(value.resources,'control-api-gateway',d.metadata.uid),d.metadata.uid);
   ensure(running(pods),apiChanged);
   return {...opened,resourceVersion:d.metadata.resourceVersion,podUIDs:pods.map(p=>p.metadata.uid).sort(),
    serviceResourceVersion:value.service.metadata.resourceVersion,serviceSpecSHA256:fingerprint(value.service.spec),
    closeEvidenceSHA256:fingerprint(value.closed.events),isolationEvidenceSHA256:fingerprint(value.traffic.events)};
  }
  async function observation(read=ports.readObservation){
   const deadline=Math.min(until,performance.now()+finalCeiling);
   const value=structuredClone(await read({deadline}));fresh();
   ensure(performance.now()<deadline,'PAIRED_TERMINAL_EXPIRED');
   ensure(value?.components&&value.api,'PAIRED_TERMINAL_OBSERVATION_INCOMPLETE');
   return {components:components(value.components),api:api(value.api)};
  }
  await source();await owner();
  const first=await observation();
  const finalOwner=await owner();fresh();
  // Короткое окно начинается до нового native read и охватывает его, epoch и
  // последующее использование proof. Ни read, ни отправка PATCH не продлевают его.
  const observedAt=Date.now();
  wallBegan=observedAt;
  until=Math.min(until,performance.now()+finalCeiling);
  wallUntil=Math.min(wallUntil,observedAt+finalCeiling);
  const final=await observation(budget=>ports.readFinalObservation(finalOwner,budget));
  ensure(same(first,final),'PAIRED_TERMINAL_OBSERVATION_CHANGED');
  ensure(await ports.recheckOwnerEpoch(manifest,finalOwner,{deadline:until})===finalOwner,
   'PAIRED_TERMINAL_OWNER_CHANGED');
  await source();fresh();
  for(const previous of admitted)verifyPairedTokenReleaseOwner(manifest,previous);
  const body={schema:'kodex.paired-same-schema-api-terminal/v1',phase:'ISOLATED_API_READY',
   purpose:'CONTROL_API_GATEWAY_SERVICE_RESTORE',sourceSHA:manifest.sourceSHA,executorSHA:record.executorSHA,
   manifestSHA256:initial.manifestSHA256,recordSHA256:initial.recordSHA256,targetSHA256:initial.targetSHA256,
   forwardReceiptSHA256:fingerprint(initial),effectJournalSHA256:initial.effectJournalSHA256,
   schemaHeads:finalOwner.schemaHeads,authority:structuredClone(finalOwner.authority),
   postgres:finalOwner.controlPlane.postgres,backend:finalOwner.controlPlane.backend,
   authorityHead:finalOwner.authorityHead,components:final.components,api:final.api,
   observedAt:new Date(observedAt).toISOString(),expiresAt:new Date(wallUntil).toISOString(),
   releaseReadiness:'BLOCKED_NATIVE_RESTORE_CONSUMER_UNBOUND'};
  ensure(Buffer.byteLength(JSON.stringify(body))<=65536,'PAIRED_TERMINAL_OUTPUT_BOUND_EXCEEDED');
  const proof=Object.freeze(structuredClone(body));
  proofs.set(proof,{hash:fingerprint(proof),until,began:observedAt,wallUntil,owners:admitted,manifest,record,journal,initial,original,seal});
  return proof;
 }finally{terminalFlights.delete(journal);}
}

// Будущий pre-CAS consumer должен получить новый readback, затем удерживать
// именно этот private proof. JSON/file/restart и новый owner не renew-ят его.
export function verifyPairedTokenReleaseTerminal(proof,{manifest,record,journal}){
 const pin=proofs.get(proof);
 ensure(pin&&performance.now()<pin.until&&Date.now()>=pin.began&&Date.now()<pin.wallUntil&&
  pin.hash===fingerprint(proof)&&pin.journal===journal&&same(manifest,pin.manifest)&&same(record,pin.record)&&
  fingerprint(pin.original)===pin.seal&&same(inspectPairedTokenForwardReceipt({manifest,record,journal}),pin.initial),
  'PAIRED_TERMINAL_PROOF_REQUIRED');
 for(const owner of pin.owners)verifyPairedTokenReleaseOwner(manifest,owner);
 return proof;
}

// Транспорт использует исходную монотонную границу terminal proof.
export function pairedTokenReleaseTerminalDeadline(proof,pair){
 verifyPairedTokenReleaseTerminal(proof,pair);
 return proofs.get(proof).until;
}
