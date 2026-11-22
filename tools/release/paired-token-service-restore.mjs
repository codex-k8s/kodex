import {performance} from 'node:perf_hooks';
import {isIP} from 'node:net';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {fingerprint} from './scoped-release.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {confirmedAPIOpenMaintenance} from './component-api-maintenance.mjs';
import {requireClosedAPIAdmission} from './component-api-traffic.mjs';
import {componentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {parseAuthorityPrivateJSON,readAuthorityPrivateText} from './authority-catalog-transition.mjs';
import {inspectPairedTokenForwardReceipt,pairedTokenRestoreJournalPath,withPairedTokenEffectFlight} from './paired-token-release-receiver.mjs';
import {pairedTokenReleaseTerminalDeadline,verifyPairedTokenReleaseTerminal} from './paired-token-release-terminal.mjs';

const schema='kodex.paired-token-service-restore/v1';
const name='control-api-gateway',namespace='kodex-system';
const ensure=(value,code)=>{if(!value)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const rv=value=>typeof value==='string'&&/^[1-9][0-9]{0,19}$/.test(value);
const sha=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const uid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const exact=(value,keys)=>value&&same(Object.keys(value).sort(),[...keys].sort());
const fields=(value,keys)=>exact(value,[...keys,'at'])&&typeof value.at==='string'&&
 /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/.test(value.at)&&Number.isFinite(Date.parse(value.at));
const podUIDs=pods=>pods.map(p=>p.metadata.uid).sort();
const permits=new WeakMap();

// Transport получает исходный срок и повторную проверку той же попытки после TLS.
export function takePairedTokenServiceRestorePermit(permit,request,recordSHA256,pairJournal){
 const admission=permits.get(permit);
 ensure(admission&&admission.pairJournal===pairJournal&&admission.requestSHA256===fingerprint(request)&&admission.recordSHA256===recordSHA256,
  'PAIRED_RESTORE_PERMIT_REQUIRED');
 permits.delete(permit);
 admission.authorize();
 return {remaining:admission.remaining,authorize:admission.authorize,api:structuredClone(admission.api)};
}

// Для положительного readback все опубликованные адреса должны принадлежать
// текущим ready Pod из подтверждённого OPEN, а каждый такой Pod иметь endpoint.
function endpointPortsMatch(servicePorts,endpointPorts,pod){
 if(!Array.isArray(servicePorts)||!servicePorts.length||!Array.isArray(endpointPorts)||endpointPorts.length!==servicePorts.length)return false;
 const containers=[...pod.spec.containers,...(pod.spec.initContainers??[]).filter(c=>c.restartPolicy==='Always')];
 return servicePorts.every(port=>{
  const protocol=port.protocol??'TCP',target=port.targetPort??port.port;
  const destinations=typeof target==='number'?[target]:containers.flatMap(c=>c.ports??[])
   .filter(p=>p.name===target&&(p.protocol??'TCP')===protocol).map(p=>p.containerPort);
  return destinations.length===1&&endpointPorts.filter(p=>(p.name??'')===(port.name??'')&&
   (p.protocol??'TCP')===protocol&&p.port===destinations[0]&&p.appProtocol===port.appProtocol).length===1;
 });
}

function endpointsReady(slices,pods,service){
 ensure(Array.isArray(slices?.items)&&!slices.metadata?.continue&&slices.items.length<=64,'PAIRED_RESTORE_ENDPOINT_SCOPE_CHANGED');
 const current=new Map(pods.map(p=>[p.metadata.uid,p])),seen=new Set();
 for(const slice of slices.items){
  ensure(slice.apiVersion==='discovery.k8s.io/v1'&&slice.kind==='EndpointSlice'&&slice.metadata?.namespace===namespace&&
   !slice.metadata.deletionTimestamp&&['IPv4','IPv6'].includes(slice.addressType)&&
   slice.metadata.labels?.['kubernetes.io/service-name']===name&&
   slice.metadata.labels?.['endpointslice.kubernetes.io/managed-by']==='endpointslice-controller.k8s.io'&&
   slice.metadata.ownerReferences?.some(owner=>owner.apiVersion==='v1'&&owner.kind==='Service'&&owner.name===name&&
    owner.controller===true&&owner.uid===service.metadata.uid)&&Array.isArray(slice.endpoints)&&slice.endpoints.length<=100,
   'PAIRED_RESTORE_ENDPOINT_SCOPE_CHANGED');
  for(const endpoint of slice.endpoints){
   const ref=endpoint.targetRef,pod=current.get(ref?.uid);
   ensure(pod&&ref.kind==='Pod'&&ref.namespace===namespace&&ref.name===pod.metadata.name&&
    Array.isArray(endpoint.addresses)&&endpoint.addresses.length>=1&&endpoint.addresses.length<=2&&
    endpoint.addresses.every(address=>isIP(address)===Number(slice.addressType.at(-1))&&pod.status.podIPs?.some(x=>x.ip===address))&&
    endpointPortsMatch(service.spec.ports,slice.ports,pod),
    'PAIRED_RESTORE_ENDPOINT_SCOPE_CHANGED');
   if(endpoint.conditions?.ready===true&&endpoint.conditions?.serving===true&&endpoint.conditions?.terminating===false)seen.add(ref.uid);
   else return false;
  }
 }
 return seen.size===current.size;
}

// Привязка к исходному журналу выводит единственный путь для Service intent.
// Отдельный stock CLI ещё не подключён к этому receiver.
export function openPairedTokenServiceRestore(pair,isolationPlan,ports){
 const forward=inspectPairedTokenForwardReceipt(pair),archive=structuredClone(isolationPlan);
 const pairJournal=pair.journal,path=pairedTokenRestoreJournalPath(pairJournal);
 const recordSHA256=fingerprint(pair.record),manifestSHA256=fingerprint(pair.manifest);
 const target=structuredClone(pair.manifest.target),serviceUID=pair.record.plan.apiAdmission.serviceUID;
 const isolationSHA256=fingerprint(archive);
 ensure(uid(serviceUID)&&archive?.action==='isolate'&&archive.serviceUID===serviceUID&&
  isolationSHA256===pair.record.plan.apiAdmission.trafficPlanSHA256,'PAIRED_RESTORE_ISOLATION_CHANGED');
 ensure(ports&&['readSource','readAPI','readTerminal','dryRun','replaceService'].every(key=>typeof ports[key]==='function'),
  'PAIRED_RESTORE_PORTS_REQUIRED');
 const header={schema,recordSHA256,manifestSHA256,isolationSHA256,forwardReceiptSHA256:fingerprint(forward),serviceUID};
 const journal=privateJournal(path,header);
 try{
  const persisted=readAuthorityPrivateText(path).trimEnd().split('\n').map(parseAuthorityPrivateJSON);
  ensure(same(persisted,journal.events),'PAIRED_RESTORE_JOURNAL_REJECTED');
 }catch(error){journal.close();throw error;}
 let busy=false,closed=false;
 const events=journal.events;
 function history(){
  const [h,intent,third,last]=events,unknown=third?.type==='UNKNOWN'?third:undefined,ack=unknown?last:third;
  ensure(fields(h,['type',...Object.keys(header)])&&h.type==='HEADER'&&Object.entries(header).every(([key,val])=>h[key]===val)&&
   events.length<=4&&(!last||unknown)&&(!intent||fields(intent,['type','resourceVersion','serviceUID','requestSHA256','api'])&&
    intent.type==='INTENT'&&intent.serviceUID===serviceUID&&rv(intent.resourceVersion)&&sha(intent.requestSHA256)&&
    exact(intent.api,['deploymentUID','resourceVersion','podUIDs','openEvidenceSHA256','closeEvidenceSHA256','isolationEvidenceSHA256'])&&
    uid(intent.api.deploymentUID)&&rv(intent.api.resourceVersion)&&Array.isArray(intent.api.podUIDs)&&
    intent.api.podUIDs.length>0&&intent.api.podUIDs.length<=8&&intent.api.podUIDs.every(uid)&&
    same(intent.api.podUIDs,[...new Set(intent.api.podUIDs)].sort())&&
    ['openEvidenceSHA256','closeEvidenceSHA256','isolationEvidenceSHA256'].every(key=>sha(intent.api[key])))&&
   (!unknown||fields(unknown,['type']))&&(!ack||fields(ack,['type','resourceVersion'])&&ack.type==='ACK'&&
    rv(ack.resourceVersion)&&BigInt(ack.resourceVersion)>BigInt(intent.resourceVersion)),
   'PAIRED_RESTORE_JOURNAL_REJECTED');
  if(intent)ensure(intent.requestSHA256===fingerprint(request(intent.resourceVersion)),'PAIRED_RESTORE_JOURNAL_REJECTED');
  return {intent,ack};
 }
 function source(){
  ensure(!closed&&pair.journal===pairJournal&&fingerprint(pair.record)===recordSHA256&&fingerprint(pair.manifest)===manifestSHA256&&
   pairedTokenRestoreJournalPath(pairJournal)===path&&same(inspectPairedTokenForwardReceipt(pair),forward),'PAIRED_RESTORE_SOURCE_CHANGED');
  const read=ports.readSource();
  ensure(read?.sourceSHA===pair.manifest.sourceSHA&&read.executorSHA===pair.record.executorSHA,'PAIRED_RESTORE_SOURCE_CHANGED');
 }
 function request(resourceVersion){
  return {uid:serviceUID,resourceVersion,beforeSpec:structuredClone(archive.afterSpec),beforeAnnotations:structuredClone(archive.afterAnnotations),
   afterSpec:structuredClone(archive.beforeSpec),afterAnnotations:structuredClone(archive.beforeAnnotations)};
 }
 async function observe(){
  const value=structuredClone(await ports.readAPI()),{service,deployment,resources,closed:close,opened,traffic}=value;
  source();
  const scope=portableRunnerTarget(target,value.observed);
  ensure(scope.allows(service)&&service.kind==='Service'&&service.metadata.name===name&&service.metadata.uid===serviceUID&&
   scope.allows(deployment)&&deployment.kind==='Deployment'&&deployment.metadata.name===name&&
   rv(service.metadata.resourceVersion)&&rv(deployment.metadata.resourceVersion)&&same(traffic?.plan,archive),
   'PAIRED_RESTORE_SERVICE_SCOPE_CHANGED');
  const before=same(service.spec,archive.afterSpec)&&same(service.metadata.annotations??{},archive.afterAnnotations);
  const after=same(service.spec,archive.beforeSpec)&&same(service.metadata.annotations??{},archive.beforeAnnotations);
  ensure(before!==after,'PAIRED_RESTORE_SERVICE_CHANGED');
  const open=confirmedAPIOpenMaintenance(close,opened,traffic);
  const pods=activeDeploymentPods(componentServingInventory(resources,name,open.deploymentUID),open.deploymentUID);
  ensure(deployment.metadata.uid===open.deploymentUID&&opened.plan.sourceSHA===pair.record.executorSHA&&
   same(deployment.spec,opened.plan.afterSpec)&&same(deployment.metadata.annotations??{},opened.plan.afterAnnotations)&&
   componentServingReady(deployment,pods,open.replicas)&&pods.every(p=>p.status?.phase==='Running'),
   'PAIRED_RESTORE_GATEWAY_CHANGED');
  if(before)ensure(same(requireClosedAPIAdmission(deployment,resources,close,{...traffic,service,slices:value.slices,pods:value.pods}),
   pair.record.plan.apiAdmission),'PAIRED_RESTORE_ISOLATION_CHANGED');
  const api={deploymentUID:open.deploymentUID,resourceVersion:deployment.metadata.resourceVersion,podUIDs:podUIDs(pods),
   openEvidenceSHA256:open.openEvidenceSHA256,closeEvidenceSHA256:fingerprint(close.events),isolationEvidenceSHA256:fingerprint(traffic.events)};
  return {before,version:service.metadata.resourceVersion,api,endpoints:after&&endpointsReady(value.slices,pods,service)};
 }
 function reconcile(current){
  const {intent,ack}=history();
  if(!intent){ensure(current.before,'PAIRED_RESTORE_UNOWNED_SERVICE');return {state:'ISOLATED'};}
  if(current.before){ensure(!ack,'PAIRED_RESTORE_SERVICE_CHANGED');return {state:'UNKNOWN'};}
  ensure(same(intent.api,current.api)&&BigInt(current.version)>BigInt(intent.resourceVersion)&&
   (!ack||BigInt(current.version)>=BigInt(ack.resourceVersion)),'PAIRED_RESTORE_READBACK_CHANGED');
  if(!current.endpoints)return {state:'CONVERGING'};
  if(!ack)journal.append({type:'ACK',resourceVersion:current.version});
  return {state:'RESTORED',serviceUID,resourceVersion:current.version};
 }
 async function step(apply){
  ensure(!busy&&!closed,'PAIRED_RESTORE_BUSY_OR_CLOSED');busy=true;
  try{
   const first=await observe(),{intent}=history();
   if(!apply||intent||!first.before)return reconcile(first);
   const operation=request(first.version);
   await ports.dryRun(structuredClone(operation));
   // Полный readback до финального окна входит в подготовку. Самый свежий
   // terminal сверяет его pins; транспорт ещё раз проверяет CAG и Service.
   const current=await observe();
   ensure(current.before&&current.version===first.version,'PAIRED_RESTORE_TERMINAL_CHANGED');
   const proof=await ports.readTerminal(pair);
   const until=pairedTokenReleaseTerminalDeadline(proof,pair);
   ensure(proof.api?.serviceUID===serviceUID&&
    proof.api?.trafficPlanSHA256===isolationSHA256&&proof.api.serviceResourceVersion===current.version&&
    proof.api.serviceSpecSHA256===fingerprint(archive.afterSpec)&&Object.entries(current.api).every(([key,value])=>same(proof.api[key],value)),
    'PAIRED_RESTORE_TERMINAL_CHANGED');
   source();
   verifyPairedTokenReleaseTerminal(proof,pair);
   ensure(!history().intent,'PAIRED_RESTORE_JOURNAL_REJECTED');
   const requestSHA256=fingerprint(operation);
   journal.append({type:'INTENT',resourceVersion:current.version,serviceUID,requestSHA256,api:current.api});
   const intentSHA256=fingerprint(history().intent);
   const remaining=()=>{
    verifyPairedTokenReleaseTerminal(proof,pair);
    const timeout=Math.floor(until-performance.now());
    ensure(timeout>0,'PAIRED_RESTORE_EXPIRED');return timeout;
   };
   const authorize=()=>{
    remaining();source();
    ensure(fingerprint(history().intent)===intentSHA256&&events.length===2,'PAIRED_RESTORE_JOURNAL_REJECTED');
    return remaining();
   };
   const permit=Object.freeze({});
   try{
    authorize();
    permits.set(permit,{pairJournal,requestSHA256,recordSHA256,remaining,authorize,api:current.api});
    await ports.replaceService(structuredClone(operation),permit);
    ensure(!permits.has(permit),'PAIRED_RESTORE_PERMIT_REQUIRED');
   }catch{
    journal.append({type:'UNKNOWN'});
    throw Error('PAIRED_RESTORE_CAS_UNKNOWN');
   }finally{permits.delete(permit);}
   return reconcile(await observe());
  }finally{busy=false;}
 }
 return Object.freeze({apply:()=>withPairedTokenEffectFlight(pairJournal,()=>step(true)),status:()=>step(false),close(){
  ensure(!busy&&!closed,'PAIRED_RESTORE_BUSY_OR_CLOSED');closed=true;journal.close();
 }});
}
