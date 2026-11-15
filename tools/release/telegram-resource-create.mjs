#!/usr/bin/env node
import {projectTelegramRuntimeObservability,requireTelegramRuntimeObservability} from './telegram-runtime-observability.mjs';
import {readTelegramIntroductionInventory,telegramIdentitySecretNames,telegramProducerSecretReferences} from './telegram-introduction-inventory.mjs';
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {constants,lstatSync,realpathSync,openSync,fsyncSync,closeSync} from 'node:fs';
import {open} from 'node:fs/promises';
import {dirname,resolve} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {isIP} from 'node:net';
import {fingerprint,withPrivatePatchFile} from './scoped-release.mjs';
import {validatedStablePublication} from './stable-image-publication.mjs';
import {createPrivatePrettyJSONSync} from './private-json-write.mjs';
import {telegramResourceCreateKeys} from './telegram-introduction-contract.mjs';
export {telegramResourceCreateKeys} from './telegram-introduction-contract.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)),namespace='kodex-system';
const annotation='kodex.dev/telegram-resource-create';
const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const uuid=value=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value??'');
const sha=value=>/^[a-f0-9]{64}$/.test(value??'')&&value!=='0'.repeat(64);
const source=value=>/^[a-f0-9]{40}$/.test(value??'')&&value!=='0'.repeat(40);
const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&same(Object.keys(value).sort(),[...keys].sort());
const key=value=>`${value.apiVersion}|${value.kind}|${value.metadata?.name}`;

export const telegramResourceCreateHeader=plan=>({schema:'kodex.telegram-resource-create-evidence/v1',planSHA256:fingerprint(plan),createID:plan.createID});

// Новый target закрепляет существующую установку и publisher, не присваивая
// ещё отсутствующим ресурсам выдуманные UID. Их UID появляются только в readback.
export function requireTelegramResourceCreateTarget(target,observed){
 fail(exact(target,['schema','sourceSHA','context','clusterUID','namespaceUID','installationUID','installationSHA256','publisherUID','authorityImage','runtimeObservability'])&&
  target.schema==='kodex.telegram-resource-create-target/v2'&&source(target.sourceSHA)&&
  /^[A-Za-z0-9_.:@/-]{1,160}$/.test(target.context??'')&&['clusterUID','namespaceUID','installationUID','publisherUID'].every(k=>uuid(target[k]))&&
  sha(target.installationSHA256)&&/^registry\.local\.kodex\/kodex\/internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(target.authorityImage??'')&&
  !target.authorityImage.endsWith('0'.repeat(64)),'TELEGRAM_CREATE_TARGET_INVALID');
 const installation=observed?.installation,publisher=observed?.publisher;
 fail(observed?.context===target.context&&observed.clusterUID===target.clusterUID&&observed.namespaceUID===target.namespaceUID&&
  observed.namespace?.kind==='Namespace'&&observed.namespace.metadata?.name===namespace&&observed.namespace.metadata.uid===target.namespaceUID&&
  !observed.namespace.metadata.deletionTimestamp&&observed.namespace.status?.phase==='Active'&&
  installation?.apiVersion==='v1'&&installation.kind==='ConfigMap'&&installation.metadata?.name==='kodex-installation-profile'&&
  installation.metadata.namespace===namespace&&!installation.metadata.deletionTimestamp&&installation.metadata.uid===target.installationUID&&
  installation.metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&installation.metadata.labels?.['app.kubernetes.io/managed-by']==='kodex-install'&&
  fingerprint(installation.data)===target.installationSHA256&&publisher?.apiVersion==='apps/v1'&&publisher.kind==='Deployment'&&
  publisher.metadata?.name==='internal-rpc-authority-publisher'&&publisher.metadata.namespace===namespace&&publisher.metadata.uid===target.publisherUID&&
  !publisher.metadata.deletionTimestamp&&publisher.metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&
  publisher.metadata.labels?.['kodex.dev/profile']==='web-only'&&publisher.metadata.labels?.['kodex.dev/environment']==='production'&&
  publisher.spec?.template?.spec?.containers?.filter(c=>c.name==='publisher'&&c.image===target.authorityImage).length===1,
  'TELEGRAM_CREATE_TARGET_CHANGED');
 requireTelegramRuntimeObservability(target.runtimeObservability,publisher);
 return fingerprint(target);
}

// CLI передаёт только результат собственного Kustomize на закреплённом clean
// source. Caller не выбирает новые endpoints, Secret, RBAC либо произвольный kind.
export function telegramResourceCreateRequests(resources,target,createID,interactionImage){
 fail(uuid(createID)&&Array.isArray(resources)&&resources.length<=10000&&source(target.sourceSHA)&&
  /^registry\.local\.kodex\/kodex\/interaction-gateway@sha256:[a-f0-9]{64}$/.test(interactionImage??'')&&
  !interactionImage.endsWith('0'.repeat(64)),'TELEGRAM_CREATE_SOURCE_INVALID');
 const selected=resources.filter(item=>telegramResourceCreateKeys.includes(key(item)));
 fail(selected.length===telegramResourceCreateKeys.length&&new Set(selected.map(key)).size===selected.length,'TELEGRAM_CREATE_SOURCE_CLOSURE_INVALID');
 const requests=telegramResourceCreateKeys.map(identity=>structuredClone(selected.find(item=>key(item)===identity)));
 for(const item of requests){
  const metadata=item.metadata;
  fail(metadata?.namespace===namespace&&!metadata.uid&&!metadata.resourceVersion&&!metadata.generateName&&!metadata.ownerReferences?.length&&
   metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&metadata.labels?.['kodex.dev/environment']==='production'&&metadata.labels?.['kodex.dev/profile']==='web-only',
   'TELEGRAM_CREATE_SOURCE_SCOPE_INVALID');
  metadata.ownerReferences=[{apiVersion:'v1',kind:'ConfigMap',name:'kodex-installation-profile',uid:target.installationUID,controller:false,blockOwnerDeletion:false}];
  metadata.annotations={...metadata.annotations,[annotation]:JSON.stringify({createID,sourceSHA:target.sourceSHA,targetSHA256:fingerprint(target)})};
  if(item.kind==='Deployment'){
   fail(item.spec?.replicas===3&&item.spec.template?.spec?.serviceAccountName==='interaction-gateway','TELEGRAM_CREATE_SOURCE_DEPLOYMENT_INVALID');
   item.spec.replicas=0;
   const spec=item.spec.template.spec,agents=[...(spec.containers??[]),...(spec.initContainers??[])];
   const identityRefs=[...new Set(telegramProducerSecretReferences(spec).filter(name=>name.startsWith('internal-rpc-authority-interaction-gateway-')||name.startsWith('interaction-gateway-platform-worker-')))];
   fail(same(identityRefs.sort(),[...telegramIdentitySecretNames].sort()),'TELEGRAM_CREATE_SOURCE_IDENTITY_SECRET_CLOSURE_INVALID');
   const expected=['interaction-gateway','internal-rpc-authority-issuer','internal-rpc-authority-socket-init','platform-worker-grant-agent'];
   fail(agents.length===expected.length&&same(agents.map(c=>c.name).sort(),[...expected].sort())&&!spec.ephemeralContainers?.length,
    'TELEGRAM_CREATE_SOURCE_CONTAINERS_INVALID');
   for(const container of agents)container.image=container.name==='interaction-gateway'?interactionImage:target.authorityImage;
   fail(target.schema==='kodex.telegram-resource-create-target/v2'&&target.runtimeObservability,'TELEGRAM_CREATE_TARGET_INVALID');
   const projected=projectTelegramRuntimeObservability(item,target.runtimeObservability.mode);
   item.spec=projected.spec;
  }
 }
 return requests;
}

// Сравнение удаляет только серверную identity/status. Для ClusterIP сохраняется
// отдельный точный receipt назначенных API адресов; endpoints/ports не расширяются.
export function telegramResourceCreateProjection(resource){
 fail(resource&&telegramResourceCreateKeys.includes(key(resource))&&resource.metadata?.namespace===namespace&&!resource.metadata.deletionTimestamp,
  'TELEGRAM_CREATE_RESOURCE_INVALID');
 const result=structuredClone(resource);
 for(const field of ['uid','resourceVersion','creationTimestamp','managedFields','generation'])delete result.metadata[field];
 delete result.status;
 if(result.kind==='Deployment'&&Object.hasOwn(result.metadata.annotations??{},'deployment.kubernetes.io/revision')){
  fail(result.metadata.annotations['deployment.kubernetes.io/revision']==='1','TELEGRAM_CREATE_DEPLOYMENT_REVISION_CHANGED');
  delete result.metadata.annotations['deployment.kubernetes.io/revision'];
 }
 let assigned;
 if(result.kind==='Service'){
  const spec=result.spec;
  fail((spec?.type??'ClusterIP')==='ClusterIP'&&!spec.externalName&&!spec.externalIPs?.length&&
   spec.ports?.every(p=>!p.nodePort),'TELEGRAM_CREATE_SERVICE_INVALID');
  if(spec.clusterIP!==undefined){
   fail(isIP(spec.clusterIP)!==0&&Array.isArray(spec.clusterIPs)&&spec.clusterIPs.length===1&&spec.clusterIPs[0]===spec.clusterIP&&
    Array.isArray(spec.ipFamilies)&&spec.ipFamilies.length===1&&spec.ipFamilies[0]===(isIP(spec.clusterIP)===4?'IPv4':'IPv6')&&
    ['SingleStack',undefined].includes(spec.ipFamilyPolicy),'TELEGRAM_CREATE_SERVICE_ALLOCATION_INVALID');
   assigned={clusterIP:spec.clusterIP,clusterIPs:spec.clusterIPs,ipFamilies:spec.ipFamilies,ipFamilyPolicy:spec.ipFamilyPolicy??'SingleStack'};
   delete spec.clusterIP;delete spec.clusterIPs;delete spec.ipFamilies;delete spec.ipFamilyPolicy;
  }
 }
 return {value:result,...(assigned?{assigned}:{})};
}

// Дремлющий Deployment не должен усыновить orphan ReplicaSet/Pod. Namespace
// discovery без label selector запрещает любой такой прежний descendant/identity.
export function requireTelegramDormantInventory(inventory,deploymentUID){
 fail(Array.isArray(inventory?.items)&&inventory.items.length<=10000,'TELEGRAM_CREATE_INVENTORY_INVALID');
 for(const item of inventory.items){
  const owned=uuid(deploymentUID)&&item.metadata?.ownerReferences?.some(o=>o.kind==='Deployment'&&o.controller===true&&o.uid===deploymentUID);
  const candidate=owned||item.metadata?.name==='interaction-gateway'||item.metadata?.name?.startsWith('interaction-gateway-')||
   item.metadata?.labels?.['app.kubernetes.io/name']==='interaction-gateway'||item.spec?.serviceAccountName==='interaction-gateway'||
   item.spec?.template?.spec?.serviceAccountName==='interaction-gateway'||item.spec?.jobTemplate?.spec?.template?.spec?.serviceAccountName==='interaction-gateway'||
   item.spec?.template?.metadata?.labels?.['app.kubernetes.io/name']==='interaction-gateway'||
   item.spec?.jobTemplate?.spec?.template?.metadata?.labels?.['app.kubernetes.io/name']==='interaction-gateway'||
   (item.secretRefs??telegramProducerSecretReferences(item.spec)).some(name=>telegramIdentitySecretNames.includes(name));
  const assigned=uuid(deploymentUID)&&item.kind==='Deployment'&&item.metadata?.uid===deploymentUID;
  fail(!candidate||(assigned||item.kind==='ReplicaSet'&&owned)&&item.spec?.replicas===0&&(item.status?.replicas??0)===0&&
   Number.isSafeInteger(item.metadata?.generation)&&item.metadata.generation>0&&item.status?.observedGeneration>=item.metadata.generation,'TELEGRAM_CREATE_ORPHAN_WORKLOAD_PRESENT');
 }
}


// Admission не может расширить source request. Допускаются только закрытые
// Kubernetes defaults с точным значением; новый sidecar/env/volume/egress не default.
export function requireTelegramCreateAdmission(request,expected){
 const defaults={
  'Deployment|spec.progressDeadlineSeconds':600,
  'Deployment|spec.template.metadata.creationTimestamp':null,
  'Deployment|spec.template.spec.dnsPolicy':'ClusterFirst',
  'Deployment|spec.template.spec.restartPolicy':'Always',
  'Deployment|spec.template.spec.schedulerName':'default-scheduler',
  'Service|spec.type':'ClusterIP','Service|spec.sessionAffinity':'None','Service|spec.internalTrafficPolicy':'Cluster',
  'PodDisruptionBudget|spec.unhealthyPodEvictionPolicy':'IfHealthyBudget',
 };
 const allowed=(path,value)=>{
  const identity=request.kind+'|'+path;
  if(Object.hasOwn(defaults,identity))return same(value,defaults[identity]);
  if(request.kind!=='Deployment')return false;
  // API возвращает deprecated alias; он не вправе выбирать другую identity.
  if(path==='spec.template.spec.serviceAccount'){
   const account=request.spec?.template?.spec?.serviceAccountName;
   return typeof account==='string'&&account.length>0&&value===account;
  }
  const container=/^spec\.template\.spec\.(?:containers|initContainers)\.\d+\.(.*)$/.exec(path);
  if(container){
   const field=container[1],known={imagePullPolicy:'IfNotPresent',terminationMessagePath:'/dev/termination-log',terminationMessagePolicy:'File',resources:{}};
   if(Object.hasOwn(known,field))return same(value,known[field]);
   if(/^(startupProbe|readinessProbe|livenessProbe)\.(timeoutSeconds|periodSeconds|successThreshold|failureThreshold)$/.test(field))return value==={timeoutSeconds:1,periodSeconds:10,successThreshold:1,failureThreshold:3}[field.split('.').at(-1)];
   if(/^(startupProbe|readinessProbe|livenessProbe)\.httpGet\.scheme$/.test(field))return value==='HTTP';
   if(/^ports\.\d+\.protocol$/.test(field))return value==='TCP';
   if(/^env\.\d+\.valueFrom\.fieldRef\.apiVersion$/.test(field))return value==='v1';
   if(/^env\.\d+\.valueFrom\.resourceFieldRef\.divisor$/.test(field))return value==='1';
  }
  return /^spec\.template\.spec\.volumes\.\d+\.(secret|configMap|projected)\.defaultMode$/.test(path)&&value===420;
 };
 const compare=(before,after,path='')=>{
  if(Array.isArray(before))return Array.isArray(after)&&before.length===after.length&&before.every((value,index)=>compare(value,after[index],path+'.'+index));
  if(before&&typeof before==='object')return after&&typeof after==='object'&&!Array.isArray(after)&&
   Object.entries(before).every(([key,value])=>Object.hasOwn(after,key)&&compare(value,after[key],path?path+'.'+key:key))&&
   Object.keys(after).every(key=>Object.hasOwn(before,key)||allowed(path?path+'.'+key:key,after[key]));
  return same(before,after);
 };
 fail(compare(request,expected),'TELEGRAM_CREATE_ADMISSION_EXPANDED_SOURCE');
}

export function planTelegramResourceCreate({target,observed,resources,interactionImage,get,dryRun,inventory,planPath,evidencePath,createID=randomUUID()}){
 requireTelegramResourceCreateTarget(target,observed);requireTelegramDormantInventory(inventory());
 const requests=telegramResourceCreateRequests(resources,target,createID,interactionImage),objects=[];
 for(const request of requests){
  fail(get(request)===null,'TELEGRAM_CREATE_FOREIGN_RESOURCE_PRESENT');
  const expected=telegramResourceCreateProjection(dryRun(request)).value;
  requireTelegramCreateAdmission(request,expected);
  fail(same(expected.metadata.ownerReferences,request.metadata.ownerReferences)&&expected.metadata.annotations?.[annotation]===request.metadata.annotations[annotation],
   'TELEGRAM_CREATE_ADMISSION_OWNERSHIP_CHANGED');
  if(request.kind==='Deployment')fail(expected.spec.replicas===0,'TELEGRAM_CREATE_ADMISSION_STARTED_WORKLOAD');
  objects.push({request,expected});
 }
 const plan={schema:'kodex.telegram-resource-create-plan/v2',target,createID,interactionImage,intendedReplicas:3,planPath,evidencePath,objects};
 validatePlan(plan);return plan;
}

function validatePlan(plan){
 fail(exact(plan,['schema','target','createID','interactionImage','intendedReplicas','planPath','evidencePath','objects'])&&plan.schema==='kodex.telegram-resource-create-plan/v2'&&uuid(plan.createID)&&plan.intendedReplicas===3&&
  Array.isArray(plan.objects)&&same(plan.objects.map(p=>key(p.request)),telegramResourceCreateKeys)&&
  [plan.planPath,plan.evidencePath].every(path=>typeof path==='string'&&path===resolve(path)&&!/[\r\n\0]/.test(path))&&plan.planPath!==plan.evidencePath,'TELEGRAM_CREATE_PLAN_INVALID');
 for(const part of plan.objects){
  const request=part.request,metadata=request.metadata;
  fail(exact(part,['request','expected']),'TELEGRAM_CREATE_PLAN_INVALID');
  fail(metadata?.namespace===namespace&&same(metadata.ownerReferences,[{apiVersion:'v1',kind:'ConfigMap',name:'kodex-installation-profile',uid:plan.target.installationUID,controller:false,blockOwnerDeletion:false}])&&
   metadata.annotations?.[annotation]===JSON.stringify({createID:plan.createID,sourceSHA:plan.target.sourceSHA,targetSHA256:fingerprint(plan.target)})&&
   !metadata.uid&&!metadata.resourceVersion&&same(part.expected?.metadata?.annotations?.[annotation],metadata.annotations[annotation]),'TELEGRAM_CREATE_PLAN_INVALID');
  if(request.kind==='Deployment')fail(request.spec?.replicas===0&&part.expected.spec?.replicas===0,'TELEGRAM_CREATE_PLAN_INVALID');
  requireTelegramCreateAdmission(request,part.expected);
 }
}

// Ни status, ни journal не выдают готовность рабочего пути. Подтверждается только
// созданный dormant набор; 107 barrier/materialization/node pull выполняются отдельно.
export async function telegramResourceCreateStep({phase,plan,journal,observe,inventory,get,dryRun,create}){
 validatePlan(plan);fail(['apply','status'].includes(phase),'TELEGRAM_CREATE_PHASE_INVALID');
 fail(Array.isArray(journal?.events)&&journal.events[0]?.type==='HEADER'&&Object.entries(telegramResourceCreateHeader(plan)).every(([k,v])=>journal.events[0][k]===v),
  'TELEGRAM_CREATE_JOURNAL_INVALID');
 // Повреждённый успешный ACK не становится потерянным ACK: такое создание
 // закрывается до любого дальнейшего readback/adoption этого batch.
 fail(!journal.events.some(e=>e.type==='CREATE_ACK_INVALID'),'TELEGRAM_CREATE_ACK_INVALID');
 const receiptOf=(part,current)=>{
  const projection=telegramResourceCreateProjection(current);
  fail(uuid(current.metadata?.uid)&&/^\d+$/.test(current.metadata.resourceVersion??'')&&same(projection.value,part.expected),
   'TELEGRAM_CREATE_FOREIGN_OR_DRIFTED_RESOURCE');
  return {resource:key(part.request),state:'CREATED_DORMANT',uid:current.metadata.uid,
   resourceSHA256:fingerprint(projection.value),...(projection.assigned?{assigned:projection.assigned}:{})};
 };
 const sameIdentity=(pin,receipt)=>!pin||pin.uid===receipt.uid&&pin.resourceSHA256===receipt.resourceSHA256&&same(pin.assigned??{},receipt.assigned??{});
 const dormant=()=>{
  const part=plan.objects.find(p=>p.request.kind==='Deployment'),current=get(part.request);
  const intent=journal.events.some(e=>e.type==='INTENT'&&e.resource===key(part.request));
  if(current!==null){
   fail(intent&&uuid(current.metadata?.uid)&&same(telegramResourceCreateProjection(current).value,part.expected),'TELEGRAM_CREATE_FOREIGN_OR_DRIFTED_RESOURCE');
   const pin=journal.events.findLast(e=>['CREATE_ACK','OBSERVED'].includes(e.type)&&e.resource===key(part.request));
   fail(sameIdentity(pin,receiptOf(part,current)),'TELEGRAM_CREATE_UID_OR_ALLOCATION_CHANGED');
  }
  requireTelegramDormantInventory(inventory(),current?.metadata.uid);
 };
 requireTelegramResourceCreateTarget(plan.target,await observe());dormant();
 const receipts=[];
 for(const part of plan.objects){
  const identity=key(part.request),intent=journal.events.findLast(e=>e.type==='INTENT'&&e.resource===identity),prior=journal.events.findLast(e=>e.type==='OBSERVED'&&e.resource===identity);
  fail(!intent||intent.requestSHA256===fingerprint(part.request),'TELEGRAM_CREATE_JOURNAL_INVALID');
  let current=get(part.request);
  if(current===null){
   // После INTENT отсутствие не доказывает, что объект не создавался и не был
   // удалён. Новый UID автоматически не создаётся даже после потерянного ACK.
   fail(!intent&&!prior,'TELEGRAM_CREATE_OUTCOME_UNKNOWN_OR_RESOURCE_REMOVED');
   if(phase==='status'){receipts.push({resource:identity,state:'ABSENT'});continue;}
   fail(same(telegramResourceCreateProjection(dryRun(part.request)).value,part.expected),'TELEGRAM_CREATE_ADMISSION_CHANGED');
   requireTelegramResourceCreateTarget(plan.target,await observe());dormant();
   fail(get(part.request)===null,'TELEGRAM_CREATE_FOREIGN_RESOURCE_PRESENT');
   journal.append({type:'INTENT',resource:identity,requestSHA256:fingerprint(part.request)});
   let acknowledged;
   try{acknowledged=await create(part.request);}catch{journal.append({type:'UNKNOWN',resource:identity});throw Error('TELEGRAM_CREATE_OPERATION_FAILED_OR_UNKNOWN');}
   let acknowledgement;
   try{acknowledgement=receiptOf(part,acknowledged);}catch{journal.append({type:'CREATE_ACK_INVALID',resource:identity});throw Error('TELEGRAM_CREATE_ACK_INVALID');}
   // Успешный API ACK назначает UID/allocation до первого GET. Замена объекта
   // после ACK не может быть усыновлена даже с теми же nonce/annotation/spec.
   journal.append({type:'CREATE_ACK',...acknowledgement});
   current=get(part.request);fail(current!==null,'TELEGRAM_CREATE_READBACK_REQUIRED');
  }
  fail((intent||journal.events.some(e=>e.type==='INTENT'&&e.resource===identity))&&uuid(current.metadata?.uid)&&
   /^\d+$/.test(current.metadata.resourceVersion??'')&&same(telegramResourceCreateProjection(current).value,part.expected),
   'TELEGRAM_CREATE_FOREIGN_OR_DRIFTED_RESOURCE');
  const receipt=receiptOf(part,current),ack=journal.events.findLast(e=>e.type==='CREATE_ACK'&&e.resource===identity);
  fail(sameIdentity(prior,receipt)&&sameIdentity(ack,receipt),'TELEGRAM_CREATE_UID_OR_ALLOCATION_CHANGED');
  requireTelegramResourceCreateTarget(plan.target,await observe());
  journal.append({type:'OBSERVED',...receipt});receipts.push(receipt);
 }
 dormant();
 const complete=receipts.every(item=>item.state==='CREATED_DORMANT');
 return {schema:'kodex.telegram-resource-create-readback/v1',planSHA256:fingerprint(plan),state:complete?'CREATED_DORMANT':'INCOMPLETE',
  qualification:'NOT_ACTIVATED_NOT_INSTALLED',resources:receipts};
}

// Canonical JSON не допускает duplicate keys даже в private plan/evidence.
function privateDirectory(path){
 const parent=dirname(resolve(path)),info=lstatSync(parent);
 fail(realpathSync(parent)===parent&&info.isDirectory()&&info.uid===process.getuid()&&(info.mode&0o777)===0o700,'TELEGRAM_CREATE_PRIVATE_DIRECTORY_REQUIRED');
}
function syncPrivateDirectory(path){
 privateDirectory(path);
 const fd=openSync(dirname(resolve(path)),constants.O_RDONLY|constants.O_DIRECTORY|constants.O_NOFOLLOW);
 try{fsyncSync(fd);}finally{closeSync(fd);}
}
async function privateJSON(path){
 privateDirectory(path);
 const file=await open(resolve(path),constants.O_RDONLY|constants.O_NOFOLLOW);
 try{
  const before=await file.stat();fail(before.isFile()&&before.uid===process.getuid()&&before.nlink===1&&(before.mode&0o777)===0o600&&before.size>0&&before.size<=4<<20,'TELEGRAM_CREATE_PRIVATE_FILE_REQUIRED');
  const raw=await file.readFile(),after=await file.stat(),value=JSON.parse(raw.toString('utf8'));
  fail(before.ino===after.ino&&before.size===raw.length&&before.mtimeMs===after.mtimeMs&&before.ctimeMs===after.ctimeMs&&
   (raw.equals(Buffer.from(JSON.stringify(value)+'\n'))||raw.equals(Buffer.from(JSON.stringify(value,null,2)+'\n'))),'TELEGRAM_CREATE_INPUT_CHANGED_OR_NONCANONICAL');
  return value;
 }finally{await file.close();}
}

// Один strict YAML decoder обслуживает creator и107 admission: duplicate keys
// не превращаются в альтернативную canonical конфигурацию.
export function decodeTelegramResourceCreateSource(raw,run){
 const script='import json,sys,yaml\nclass Unique(yaml.SafeLoader): pass\ndef mapping(loader,node,deep=False):\n out={}\n for key,value in node.value:\n  key=loader.construct_object(key,deep=deep)\n  if key in out: raise ValueError("duplicate YAML key")\n  out[key]=loader.construct_object(value,deep=deep)\n return out\nUnique.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG,mapping)\nprint(json.dumps([d for d in yaml.load_all(sys.stdin,Loader=Unique) if d is not None]))\n';
 return JSON.parse(run('python3',['-I','-c',script],raw,32<<20));
}

async function main(args){
 const phase=args.shift(),options={};
 while(args.length){const name=args.shift();fail(['--kubeconfig','--context','--target','--publication','--plan','--evidence','--approved-plan-sha'].includes(name)&&!Object.hasOwn(options,name)&&args.length,'TELEGRAM_CREATE_ARGUMENTS_INVALID');options[name]=args.shift();}
 fail(['plan','apply','status'].includes(phase)&&options['--kubeconfig']?.startsWith('/')&&!/[\r\n\0]/.test(options['--kubeconfig'])&&
  /^[A-Za-z0-9_.:@/-]{1,160}$/.test(options['--context']??'')&&options['--publication']&&options['--plan'],'TELEGRAM_CREATE_ARGUMENTS_INVALID');
 const env=Object.fromEntries(['HOME','PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
 const run=(program,argv,input,maxBuffer=4<<20)=>{try{return execFileSync(program,argv,{cwd:root,env,input,encoding:'utf8',timeout:35000,maxBuffer,stdio:['pipe','pipe','pipe']});}catch{throw Error('TELEGRAM_CREATE_OPERATION_FAILED_OR_UNKNOWN');}};
 const kube=(argv,input,maxBuffer)=>run('kubectl',['--kubeconfig',options['--kubeconfig'],'--context',options['--context'],'--request-timeout=25s',...argv],input,maxBuffer);
 const sourceSHA=run('git',['rev-parse','HEAD']).trim();fail(source(sourceSHA)&&run('git',['status','--porcelain','--untracked-files=normal']).trim()==='','TELEGRAM_CREATE_CLEAN_SOURCE_REQUIRED');
 const publication=await privateJSON(options['--publication']),interactionImage=validatedStablePublication(sourceSHA,publication,'interaction-gateway');
 const get=(kind,name)=>JSON.parse(kube(['-n',namespace,'get',kind,name,'-o','json']));
 const observe=()=>{const ns=JSON.parse(kube(['get','namespace',namespace,'-o','json']));return {context:options['--context'],clusterUID:JSON.parse(kube(['get','namespace','kube-system','-o','json'])).metadata.uid,
  namespaceUID:ns.metadata.uid,namespace:ns,installation:get('configmap','kodex-installation-profile'),publisher:get('deployment','internal-rpc-authority-publisher')};};
 const resourceGet=request=>{const raw=kube(['-n',namespace,'get',request.kind.toLowerCase(),request.metadata.name,'--ignore-not-found','-o','json']);return raw.trim()===''?null:JSON.parse(raw);};
 const create=(request,dry=false)=>withPrivatePatchFile(request,path=>JSON.parse(kube(['-n',namespace,'create','--validate=strict',...(dry?['--dry-run=server']:[]),'-f',path,'-o','json'])));
 const inventory=()=>readTelegramIntroductionInventory(kube);
 const raw=kube(['kustomize','deploy/k8s/profiles/web-only'],undefined,32<<20);
 const resources=decodeTelegramResourceCreateSource(raw,run);
 let plan;
 if(phase==='plan'){
  fail(options['--target']&&options['--evidence']&&!options['--approved-plan-sha'],'TELEGRAM_CREATE_ARGUMENTS_INVALID');
  const target=await privateJSON(options['--target']);fail(target.sourceSHA===sourceSHA&&target.context===options['--context'],'TELEGRAM_CREATE_SOURCE_CHANGED');
  fail(publication.publicationPlan.context===target.context&&publication.publicationPlan.clusterUID===target.clusterUID&&publication.publicationPlan.namespaceUID===target.namespaceUID,'TELEGRAM_CREATE_PUBLICATION_TARGET_CHANGED');
  plan=planTelegramResourceCreate({target,observed:observe(),resources,interactionImage,get:resourceGet,dryRun:r=>create(r,true),inventory,planPath:resolve(options['--plan']),evidencePath:resolve(options['--evidence'])});
  syncPrivateDirectory(options['--plan']);syncPrivateDirectory(options['--evidence']);
  createPrivatePrettyJSONSync(resolve(options['--plan']),plan);syncPrivateDirectory(options['--plan']);
  process.stdout.write(JSON.stringify({planSHA256:fingerprint(plan),qualification:'PLAN_ONLY_NOT_APPLIED'})+'\n');return;
 }
 fail(!options['--target']&&options['--evidence'],'TELEGRAM_CREATE_ARGUMENTS_INVALID');
 plan=await privateJSON(options['--plan']);
 fail(plan.planPath===resolve(options['--plan'])&&plan.evidencePath===resolve(options['--evidence']),'TELEGRAM_CREATE_JOURNAL_PATH_CHANGED');
 fail(plan.target?.sourceSHA===sourceSHA&&plan.target.context===options['--context']&&plan.interactionImage===interactionImage&&
  same(plan.objects?.map(p=>p.request),telegramResourceCreateRequests(resources,plan.target,plan.createID,interactionImage)),'TELEGRAM_CREATE_SOURCE_CHANGED');
 fail(publication.publicationPlan.context===plan.target.context&&publication.publicationPlan.clusterUID===plan.target.clusterUID&&publication.publicationPlan.namespaceUID===plan.target.namespaceUID,'TELEGRAM_CREATE_PUBLICATION_TARGET_CHANGED');
 if(phase==='apply')fail(options['--approved-plan-sha']===fingerprint(plan),'TELEGRAM_CREATE_EXACT_PLAN_REQUIRED');
 else fail(!options['--approved-plan-sha'],'TELEGRAM_CREATE_ARGUMENTS_INVALID');
 const journal=privateJournal(options['--evidence'],telegramResourceCreateHeader(plan));
 try{syncPrivateDirectory(options['--evidence']);const result=await telegramResourceCreateStep({phase,plan,journal,observe,inventory,get:resourceGet,dryRun:r=>create(r,true),create:r=>create(r)});process.stdout.write(JSON.stringify(result)+'\n');}finally{journal.close();}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href)main(process.argv.slice(2)).catch(error=>{process.stderr.write((/^TELEGRAM_CREATE_[A-Z_]+$/.test(error.message)?error.message:'TELEGRAM_CREATE_REJECTED')+'\n');process.exitCode=1;});
