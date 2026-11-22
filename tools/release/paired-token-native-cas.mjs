import {execFileSync} from 'node:child_process';
import {realpathSync} from 'node:fs';
import {request as httpsRequest} from 'node:https';
import {performance} from 'node:perf_hooks';
import {isIPv4} from 'node:net';
import {resolve} from 'node:path';
import {fingerprint,kubectlInvocation} from './scoped-release.mjs';
import {parseAuthorityPrivateJSON,readAuthorityPrivateText} from './authority-catalog-transition.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {validatePairedTokenReleaseManifest} from './paired-token-release-plan.mjs';
import {takePairedTokenCASPermit} from './paired-token-release-receiver.mjs';
import {takePairedTokenServiceRestorePermit} from './paired-token-service-restore.mjs';

const namespace='kodex-system';
const maxBytes=1<<20;
const ensure=(value,code)=>{if(!value)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const keys=(value,names)=>value&&typeof value==='object'&&!Array.isArray(value)&&
 Object.keys(value).length===names.length&&names.every(key=>Object.hasOwn(value,key));
const version=value=>typeof value==='string'&&/^[1-9][0-9]{0,19}$/.test(value);

function tlsBytes(value,label){
 ensure(typeof value==='string'&&value.length<=65536&&value.length%4===0&&/^[A-Za-z0-9+/]+={0,2}$/.test(value),
  'PAIRED_NATIVE_CAS_KUBECONFIG_UNSUPPORTED');
 const data=Buffer.from(value,'base64');
 const header=data.toString('ascii',0,80).split(/\r?\n/,1)[0];
 ensure(data.toString('base64')===value&&label.includes(header),
  'PAIRED_NATIVE_CAS_KUBECONFIG_UNSUPPORTED');
 return data;
}

// Клиент ограничен локальным k3d с embedded mTLS. Kubectl только разбирает
// выбранный kubeconfig, закрытый stdout которых не возвращается вызывающему.
function readClient(context,kubeconfig){
 ensure(typeof kubeconfig==='string'&&kubeconfig.startsWith('/')&&resolve(kubeconfig)===kubeconfig&&
  !/[\r\n\0]/.test(kubeconfig),'PAIRED_NATIVE_CAS_KUBECONFIG_REQUIRED');
 const digest=()=>{
  ensure(realpathSync(kubeconfig)===kubeconfig,'PAIRED_NATIVE_CAS_KUBECONFIG_REQUIRED');
  return fingerprint(readAuthorityPrivateText(kubeconfig));
 };
 const initial=digest();
 const verify=()=>ensure(digest()===initial,'PAIRED_NATIVE_CAS_KUBECONFIG_CHANGED');
 const call=kubectlInvocation(context,['config','view','--raw','--minify','-o','json'],false,kubeconfig);
 const env=Object.fromEntries(['PATH','HOME','LANG'].filter(key=>process.env[key]).map(key=>[key,process.env[key]]));
 try{
  const data=parseAuthorityPrivateJSON(execFileSync(call.file,call.arguments,{encoding:'utf8',timeout:3000,maxBuffer:maxBytes,
   killSignal:'SIGKILL',env:{...env,KUBECTL_KUBERC:'false'},stdio:['ignore','pipe','pipe']}));
  const current=data.contexts?.[0],cluster=data.clusters?.[0],user=data.users?.[0];
  ensure(data.apiVersion==='v1'&&data.kind==='Config'&&data['current-context']===context&&
   data.contexts?.length===1&&data.clusters?.length===1&&data.users?.length===1&&
   current.name===context&&current.context.cluster===cluster.name&&current.context.user===user.name&&
   keys(cluster.cluster,['server','certificate-authority-data'])&&keys(user.user,['client-certificate-data','client-key-data']),
   'PAIRED_NATIVE_CAS_KUBECONFIG_UNSUPPORTED');
  const origin=new URL(cluster.cluster.server);
  ensure(origin.protocol==='https:'&&isIPv4(origin.hostname)&&origin.hostname.startsWith('127.')&&origin.port&&origin.pathname==='/'&&
   !origin.username&&!origin.password&&!origin.search&&!origin.hash,'PAIRED_NATIVE_CAS_KUBECONFIG_UNSUPPORTED');
  verify();
  const certificate=['-----BEGIN CERTIFICATE-----'];
  return {origin,verify,ca:tlsBytes(cluster.cluster['certificate-authority-data'],certificate),
   cert:tlsBytes(user.user['client-certificate-data'],certificate),
   key:tlsBytes(user.user['client-key-data'],['-----BEGIN PRIVATE KEY-----','-----BEGIN RSA PRIVATE KEY-----','-----BEGIN EC PRIVATE KEY-----'])};
 }catch{throw Error('PAIRED_NATIVE_CAS_KUBECONFIG_UNSUPPORTED');}
}

// Новый сокет для каждого запроса. Redirect/Retry-After не образуют повтор,
// а общий deadline учитывает установление TLS и чтение полного ответа.
function requestOnce(client,path,remaining,body,beforeWrite=remaining,{accept='application/json',limit=maxBytes,outerSignal}={}){
 const budget=remaining();
 return new Promise((resolve,reject)=>{
  const timeout=AbortSignal.timeout(budget);
  const signal=outerSignal?AbortSignal.any([timeout,outerSignal]):timeout;
  const fail=()=>reject(Error('PAIRED_NATIVE_CAS_HTTP_FAILED'));
  const options={method:body?'PATCH':'GET',agent:false,signal,ca:client.ca,cert:client.cert,key:client.key,
   rejectUnauthorized:true, minVersion:'TLSv1.2',headers:{Accept:accept,...(body?
    {'Content-Type':'application/json-patch+json','Content-Length':body.length}:{})}};
  let req;
  try{
   req=httpsRequest(new URL(path,client.origin),options,response=>{
    response.on('error',fail);
    response.on('aborted',fail);
    if(response.statusCode!==200){fail();req.destroy();return;}
    const chunks=[];let length=0;
    response.on('data',chunk=>{
     length+=chunk.length;
     if(length>limit){fail();req.destroy();return;}
     chunks.push(chunk);
    });
    response.on('end',()=>{
     try{remaining();resolve(parseAuthorityPrivateJSON(Buffer.concat(chunks,length).toString('utf8')));}
     catch{fail();}
    });
   });
   req.on('error',fail);
   req.once('socket',socket=>{
    socket.once('secureConnect',()=>{
     try{beforeWrite();req.end(body);}
     catch{fail();req.destroy();}
    });
   });
  }catch{fail();req?.destroy();}
 });
}

const metadataRoutes=new Map([
 ['Pod','/api/v1/namespaces/kodex-system/pods'],
 ['ReplicaSet','/apis/apps/v1/namespaces/kodex-system/replicasets'],
 ['Job','/apis/batch/v1/namespaces/kodex-system/jobs'],
]);
const namespaceBase='/api/v1/namespaces/';
const deploymentBase='/apis/apps/v1/namespaces/'+namespace+'/deployments/';
const serviceEndpoint=namespaceBase+namespace+'/services/control-api-gateway';
const apiSlicesEndpoint='/apis/discovery.k8s.io/v1/namespaces/'+namespace+'/endpointslices?' +
 new URLSearchParams({labelSelector:'kubernetes.io/service-name=control-api-gateway',limit:'100'});

// Закрытый набор четырёх GET проверяет ту же installation boundary для
// чтения join и для CAS preimage. Вызывающий не задаёт маршруты.
async function readScopedResource(context,endpoint,read){
 const observed=await Promise.allSettled([namespaceBase+'kube-system',namespaceBase+namespace,
  namespaceBase+namespace+'/configmaps/kodex-installation-profile',endpoint].map(read));
 ensure(observed.every(item=>item.status==='fulfilled'),'PAIRED_NATIVE_CAS_SCOPE_UNAVAILABLE');
 const [cluster,targetNS,installation,resource]=observed.map(item=>item.value);
 const namespaceUID=(item,name)=>item?.apiVersion==='v1'&&item.kind==='Namespace'&&item.metadata?.name===name&&
  !item.metadata.deletionTimestamp?item.metadata.uid:undefined;
 return {resource,observed:{context,clusterUID:namespaceUID(cluster,'kube-system'),
  namespaceUID:namespaceUID(targetNS,namespace),installation}};
}
async function readDeploymentBoundary(context,name,read){
 const {resource,observed}=await readScopedResource(context,deploymentBase+name,read);
 return {deployment:resource,observed};
}

// Отдельный read-only интерфейс для полного lineage discovery. Даже local
// caller выбирает только закрытый вид и точное имя в namespace профиля.
export function createPairedTokenNativeReadClient(context,kubeconfig){
 ensure(/^k3d-[a-z0-9][a-z0-9.-]*$/.test(context),'PAIRED_NATIVE_READ_CONTEXT_REQUIRED');
 const client=readClient(context,kubeconfig);
 async function read(path,options,{signal,deadline}={}){
  ensure((signal===undefined||signal instanceof AbortSignal)&&(deadline===undefined||Number.isFinite(deadline)),
   'PAIRED_NATIVE_READ_BUDGET_REJECTED');
  const expires=Math.min(performance.now()+5000,deadline??Infinity);
  const remaining=()=>{
   const budget=Math.floor(expires-performance.now());
   ensure(!signal?.aborted&&budget>0,'PAIRED_NATIVE_READ_EXPIRED');return budget;
  };
  client.verify();
  const result=await requestOnce(client,path,remaining,undefined,remaining,{...options,outerSignal:signal});
  client.verify();remaining();
  return result;
 }
 return Object.freeze({
  readMetadata(kind,budget){
   const route=metadataRoutes.get(kind);
   ensure(route,'PAIRED_NATIVE_READ_RESOURCE_REJECTED');
   return read(route+'?limit=1000',{
    accept:'application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1',limit:8<<20},budget);
  },
  readObject(kind,name,budget){
   ensure((kind==='Pod'||kind==='Job')&&typeof name==='string'&&/^[a-z0-9][a-z0-9.-]{0,252}$/.test(name),
    'PAIRED_NATIVE_READ_RESOURCE_REJECTED');
   return read(metadataRoutes.get(kind)+'/'+name,undefined,budget);
  },
  readReplicaSet(name,budget){
   ensure(typeof name==='string'&&/^[a-z0-9][a-z0-9.-]{0,252}$/.test(name),'PAIRED_NATIVE_READ_RESOURCE_REJECTED');
   return read(metadataRoutes.get('ReplicaSet')+'/'+name,undefined,budget);
  },
  readDeploymentBoundary(name,budget){
   ensure(['control-plane','integration-gateway','control-api-gateway'].includes(name),'PAIRED_NATIVE_READ_RESOURCE_REJECTED');
   return readDeploymentBoundary(context,name,path=>read(path,undefined,budget));
  },
  readAuthorityPublisherBoundary(budget){
   return readDeploymentBoundary(context,'internal-rpc-authority-publisher',path=>read(path,undefined,budget));
  },
  async readPairedOwnerBoundary(budget){
   const get=path=>read(path,undefined,budget);
   const results=await Promise.allSettled([
    readDeploymentBoundary(context,'internal-rpc-authority-publisher',get),
    get(namespaceBase+namespace+'/configmaps/internal-rpc-authority-publisher-target-registry'),
    get(metadataRoutes.get('Pod')+'/kodex-postgresql-0'),
    get('/apis/apps/v1/namespaces/'+namespace+'/statefulsets/kodex-postgresql'),
   ]);
   for(const item of results)if(item.status==='rejected')throw item.reason;
   const [publisher,registry,pod,statefulSet]=results.map(item=>item.value);
   return {...publisher,registry,pod,statefulSet};
  },
  async readControlAPIServiceBoundary(budget){
   const [bound,slices]=await Promise.allSettled([
    readScopedResource(context,serviceEndpoint,path=>read(path,undefined,budget)),
    read(apiSlicesEndpoint,undefined,budget),
   ]);
   if(bound.status==='rejected')throw bound.reason;
   if(slices.status==='rejected')throw slices.reason;
   return {service:bound.value.resource,observed:bound.value.observed,slices:slices.value};
  },
  readIsolationPods(nonce,budget){
   ensure(typeof nonce==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(nonce),'PAIRED_NATIVE_READ_RESOURCE_REJECTED');
   const query=new URLSearchParams({labelSelector:'kodex.dev/component-traffic-isolation='+nonce,limit:'1000'});
   return read(metadataRoutes.get('Pod')+'?'+query,undefined,budget);
  },
 });
}

function patchBody(operation){
 const body=Buffer.from(JSON.stringify([
  {op:'test',path:'/metadata/uid',value:operation.uid},
  {op:'test',path:'/metadata/resourceVersion',value:operation.resourceVersion},
  {op:'test',path:'/spec',value:operation.beforeSpec},
  {op:'replace',path:'/spec',value:operation.afterSpec},
 ]));
 ensure(body.length<=maxBytes,'PAIRED_NATIVE_CAS_REQUEST_OVERSIZED');
 return body;
}

// Port не публикует отдельную команду записи. Effect доступен только внутри
// receiver с одноразовым допуском, привязанным к fsync INTENT той же пары.
export function createPairedTokenNativeCAS({manifest,record,journal,kubeconfig}){
 validatePairedTokenReleaseManifest(manifest);
 const target=structuredClone(manifest.target),recordSHA256=fingerprint(record);
 ensure(/^k3d-[a-z0-9][a-z0-9.-]*$/.test(target.context)&&record?.plan?.manifestSHA256===fingerprint(manifest)&&
  journal?.events?.[0]?.recordSHA256===recordSHA256,'PAIRED_NATIVE_CAS_SOURCE_REQUIRED');
 const parts=structuredClone(record.plan.components);
 const client=readClient(target.context,kubeconfig);
 async function check(operation,remaining,dryRun,authorize){
  ensure(keys(operation,['name','uid','resourceVersion','beforeSpec','afterSpec'])&&version(operation.resourceVersion),
   'PAIRED_NATIVE_CAS_REQUEST_REJECTED');
  const part=parts.find(p=>p.name===operation.name);
  ensure(part?.uid===operation.uid,'PAIRED_NATIVE_CAS_REQUEST_REJECTED');
  const allowed=[[part.beforeSpec,part.afterSpec],[part.afterSpec,part.beforeSpec]].some(([before,after])=>{
   const stopped={...before,replicas:0},staged={...after,replicas:0};
   return [[before,stopped],[stopped,staged],[staged,after]].some(([a,b])=>
    same(operation.beforeSpec,a)&&same(operation.afterSpec,b));
  });
  ensure(allowed,'PAIRED_NATIVE_CAS_REQUEST_REJECTED');
  client.verify();
  const endpoint=deploymentBase+operation.name;
  const {deployment:current,observed}=await readDeploymentBoundary(target.context,operation.name,
   path=>requestOnce(client,path,remaining));
  const scope=portableRunnerTarget(target,observed);
  ensure(scope.allows(current)&&current.metadata.name===operation.name&&current.metadata.uid===operation.uid&&
   current.metadata.resourceVersion===operation.resourceVersion&&
   same(current.spec,operation.beforeSpec),'PAIRED_NATIVE_CAS_PREIMAGE_CHANGED');
  const options='?fieldValidation=Strict&fieldManager=kodex-paired-token-release'+(dryRun?'&dryRun=All':'');
  client.verify();
  const response=await requestOnce(client,endpoint+options,remaining,patchBody(operation),()=>{
   authorize?.();client.verify();remaining();
  });
  client.verify();remaining();
  ensure(scope.allows(response)&&response.metadata.name===operation.name&&response.metadata.uid===operation.uid&&version(response.metadata.resourceVersion)&&
   same(response.spec,operation.afterSpec)&&(dryRun||BigInt(response.metadata.resourceVersion)>BigInt(operation.resourceVersion)),
   'PAIRED_NATIVE_CAS_ACK_REJECTED');
  return {uid:response.metadata.uid,resourceVersion:response.metadata.resourceVersion,specSHA256:fingerprint(response.spec)};
 }
 return Object.freeze({
  dryRunComponent(request){
   const deadline=performance.now()+5000;
   return check(structuredClone(request),()=>{
    const budget=Math.floor(deadline-performance.now());
    ensure(budget>0,'PAIRED_NATIVE_CAS_DRY_RUN_EXPIRED');return budget;
   },true);
  },
  replaceFrozenComponentSpec(request,permit){
   const {remaining,authorize}=takePairedTokenCASPermit(permit,request,journal,recordSHA256);
   return check(structuredClone(request),remaining,false,authorize);
  },
 });
}

// Отдельный фиксированный PATCH того же Service. Persisted запрос требует
// одноразового допуска из журнала восстановления, записанного до отправки.
export function createPairedTokenNativeServiceRestore({pair,isolationPlan,kubeconfig}){
 validatePairedTokenReleaseManifest(pair.manifest);
 const pairJournal=pair.journal;
 const target=structuredClone(pair.manifest.target),recordSHA256=fingerprint(pair.record),isolation=structuredClone(isolationPlan);
 const uid=target.resources.find(r=>r.kind==='Service'&&r.name==='control-api-gateway')?.uid;
 ensure(/^k3d-[a-z0-9][a-z0-9.-]*$/.test(target.context)&&uid===isolation?.serviceUID&&
  pair.record?.plan?.apiAdmission?.trafficPlanSHA256===fingerprint(isolation),'PAIRED_RESTORE_SOURCE_REQUIRED');
 const client=readClient(target.context,kubeconfig);
 async function send(op,remaining,dryRun,authorize,api){
  ensure(keys(op,['uid','resourceVersion','beforeSpec','beforeAnnotations','afterSpec','afterAnnotations'])&&version(op.resourceVersion)&&
   op.uid===uid&&same(op.beforeSpec,isolation.afterSpec)&&same(op.beforeAnnotations,isolation.afterAnnotations)&&
   same(op.afterSpec,isolation.beforeSpec)&&same(op.afterAnnotations,isolation.beforeAnnotations),'PAIRED_RESTORE_REQUEST_REJECTED');
  client.verify();
  const get=path=>requestOnce(client,path,remaining);
  const reads=await Promise.allSettled([
   readScopedResource(target.context,serviceEndpoint,get),
   ...(dryRun?[]:[get(deploymentBase+'control-api-gateway')]),
  ]);
  for(const read of reads)if(read.status==='rejected')throw read.reason;
  const {resource,observed}=reads[0].value;
  const scope=portableRunnerTarget(target,observed);
  if(!dryRun){
   const gateway=reads[1].value;
   ensure(scope.allows(gateway)&&gateway.kind==='Deployment'&&gateway.metadata.name==='control-api-gateway'&&
    gateway.metadata.uid===api?.deploymentUID&&gateway.metadata.resourceVersion===api?.resourceVersion,
    'PAIRED_RESTORE_GATEWAY_CHANGED');
  }
  const matches=(value,spec,annotations)=>scope.allows(value)&&value.kind==='Service'&&value.metadata.name==='control-api-gateway'&&
   value.metadata.uid===uid&&same(value.spec,spec)&&same(value.metadata.annotations??{},annotations);
  ensure(matches(resource,op.beforeSpec,op.beforeAnnotations)&&resource.metadata.resourceVersion===op.resourceVersion,
   'PAIRED_RESTORE_PREIMAGE_CHANGED');
  const patch=Buffer.from(JSON.stringify([
   {op:'test',path:'/metadata/uid',value:uid},
   {op:'test',path:'/metadata/resourceVersion',value:op.resourceVersion},
   {op:'test',path:'/spec',value:op.beforeSpec},
   {op:'test',path:'/metadata/annotations',value:op.beforeAnnotations},
   {op:'replace',path:'/spec',value:op.afterSpec},
   {op:'replace',path:'/metadata/annotations',value:op.afterAnnotations},
  ]));
  ensure(patch.length<=maxBytes,'PAIRED_NATIVE_CAS_REQUEST_OVERSIZED');
  const options='?fieldValidation=Strict&fieldManager=kodex-paired-token-release'+(dryRun?'&dryRun=All':'');
  const response=await requestOnce(client,serviceEndpoint+options,remaining,patch,()=>{
   authorize?.();client.verify();remaining();
  });
  client.verify();remaining();
  ensure(matches(response,op.afterSpec,op.afterAnnotations)&&version(response.metadata.resourceVersion)&&
   (dryRun||BigInt(response.metadata.resourceVersion)>BigInt(op.resourceVersion)),'PAIRED_RESTORE_RESPONSE_REJECTED');
 }
 return Object.freeze({
  dryRun(request){
   const until=performance.now()+5000;
   return send(structuredClone(request),()=>{
    const remaining=Math.floor(until-performance.now());
    ensure(remaining>0,'PAIRED_RESTORE_DRY_RUN_EXPIRED');return remaining;
   },true);
  },
  replaceService(request,permit){
   const {remaining,authorize,api}=takePairedTokenServiceRestorePermit(permit,request,recordSHA256,pairJournal);
   return send(structuredClone(request),remaining,false,authorize,api);
  },
 });
}
