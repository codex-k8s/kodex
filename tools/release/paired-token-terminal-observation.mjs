import {fingerprint} from './scoped-release.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {pairedTokenReleaseNames} from './paired-token-release-plan.mjs';
import {pairedAuthorityPublisher,readMultiComponentServingInventory,readPairedTerminalServingInventory} from './multi-component-serving-inventory.mjs';
import {componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';

const gateway='control-api-gateway';
const names=[...pairedTokenReleaseNames,gateway];
const ensure=(condition,code)=>{if(!condition)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);

async function joined(reads){
 const results=await Promise.allSettled(reads);
 for(const item of results)if(item.status==='rejected')throw item.reason;
 return results.map(item=>item.value);
}

function list(value,apiVersion,kind,limit){
 ensure(value?.apiVersion===apiVersion&&value.kind===kind&&Array.isArray(value.items)&&value.items.length<=limit&&
  /^[0-9]{1,128}$/.test(value.metadata?.resourceVersion??'')&&!value.metadata?.continue&&
  (value.metadata?.remainingItemCount===undefined||value.metadata?.remainingItemCount===0),
  'PAIRED_TERMINAL_OBSERVATION_INCOMPLETE');
 const ids=value.items.map(item=>item?.metadata?.uid);
 ensure(ids.every(id=>typeof id==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(id))&&new Set(ids).size===ids.length,
  'PAIRED_TERMINAL_OBSERVATION_INCOMPLETE');
 return [...value.items].sort((a,b)=>a.metadata.uid.localeCompare(b.metadata.uid));
}

// Финальный снимок добавляет publisher к общему serving discovery и сверяет
// его с исходным owner pin. Все запросы получают один deadline.
export async function readPairedTokenTerminalObservation(client,target,{closed,traffic,opened},budget,publisher){
 const request=()=>joined([
  ...names.map(name=>client.readDeploymentBoundary(name,budget)),client.readControlAPIServiceBoundary(budget),
  ...(publisher?[client.readAuthorityPublisherBoundary(budget)]:[]),
 ]);
 const first=await request(),service=first[names.length];
 const scope=portableRunnerTarget(target,service.observed);
 const pins=names.map((name,index)=>{
  const value=first[index];
  ensure(same(value.observed,service.observed)&&scope.allows(value.deployment)&&value.deployment.kind==='Deployment'&&
   value.deployment.metadata.name===name,'PAIRED_TERMINAL_OBSERVATION_SCOPE_CHANGED');
  return {name,deploymentUID:value.deployment.metadata.uid};
 });
 ensure(scope.allows(service.service)&&service.service.kind==='Service'&&service.service.metadata.name===gateway,
  'PAIRED_TERMINAL_OBSERVATION_SCOPE_CHANGED');
 let inventory;
 if(publisher){
  const {deployment,observed}=first.at(-1),uid=deployment?.metadata?.uid;
  ensure(same(observed,service.observed)&&deployment?.apiVersion==='apps/v1'&&deployment.kind==='Deployment'&&
   deployment.metadata.name===pairedAuthorityPublisher&&portableRunnerTarget(publisher.target,observed).allows(deployment)&&
   fingerprint(deployment)===publisher.deploymentSHA256,'PAIRED_AUTHORITY_PUBLISHER_CHANGED');
  inventory=await readPairedTerminalServingInventory(client,pins,uid,budget);
  const owned=inventory.components.at(-1);
  ensure(componentServingReady(deployment,activeDeploymentPods(owned.inventory,uid),deployment.spec.replicas),
   'CATALOG_BASELINE_PUBLISHER_NOT_READY');
 }else inventory=await readMultiComponentServingInventory(client,pins,budget);
 const nonce=traffic.plan.afterSpec?.selector?.['kodex.dev/component-traffic-isolation'];
 const [last,pods]=await joined([request(),client.readIsolationPods(nonce,budget)]);
 const finalService=last[names.length];
 ensure(same(first.slice(0,names.length),last.slice(0,names.length))&&
  (!publisher||same(first.at(-1),last.at(-1)))&&same(service.observed,finalService.observed)&&
  same(service.service,finalService.service)&&same(list(service.slices,'discovery.k8s.io/v1','EndpointSliceList',100),
   list(finalService.slices,'discovery.k8s.io/v1','EndpointSliceList',100)),'PAIRED_TERMINAL_OBSERVATION_CHANGED');
 list(pods,'v1','PodList',1000);
 const entries=Object.fromEntries(inventory.components.slice(0,names.length).map(({name,inventory:resources},index)=>[
  name,{deployment:first[index].deployment,resources},
 ]));
 return {components:entries,api:{...entries[gateway],...service,pods,closed,traffic,opened}};
}
