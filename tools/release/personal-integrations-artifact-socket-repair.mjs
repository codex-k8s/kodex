import {fingerprint,withPrivatePatchFile} from './scoped-release.mjs';
import {personalIntegrationsArtifactAPISpec} from './personal-integrations-artifact-spec.mjs';
import {isPersonalIntegrationsMigrationProfile} from './personal-integrations-migration-contract.mjs';

const name='artifact-retention', namespace='kodex-system';
const init='internal-rpc-authority-socket-init';
const mount='internal-rpc-authority-sockets';
const marker='kodex.dev/artifact-socket-repair';
const intentType='PERSONAL_ARTIFACT_SOCKET_REPAIR_INTENT', ackType='PERSONAL_ARTIFACT_SOCKET_REPAIR_ACK';
const error='PERSONAL_ARTIFACT_SOCKET_REPAIR_REJECTED';
const requireValue=ok=>{if(!ok)throw Error(error);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const resourceVersion=value=>typeof value==='string'&&/^[1-9][0-9]*$/.test(value);

function repairSpecs(migration) {
 const parts=migration.componentHandoff?.components?.filter(part=>part.name===name);
 requireValue(isPersonalIntegrationsMigrationProfile(migration.profile)&&parts?.length===1);
 const [part]=parts;
 const before=personalIntegrationsArtifactAPISpec({profile:migration.profile,name,beforeSpec:part.beforeSpec,
  expectedSpec:part.resumedSpec,authorityImage:migration.personalIntegrationsInputs?.authority.imageRef});
 const pod=before.template.spec, initializer=pod.initContainers?.[0];
 const desired=migration.personalIntegrationsInputs?.issuerInputs?.deployment?.spec?.template?.spec?.securityContext;
 requireValue(before.replicas===part.replicas&&before.replicas>0&&!pod.hostNetwork&&!pod.hostPID&&!pod.hostIPC&&
  pod.serviceAccountName===name&&pod.automountServiceAccountToken===false&&
  pod.securityContext?.fsGroup===10001&&pod.securityContext.fsGroupChangePolicy==='OnRootMismatch'&&
  desired?.fsGroup===29000&&desired.fsGroupChangePolicy===pod.securityContext.fsGroupChangePolicy&&
  pod.initContainers.length===1&&initializer.name===init&&initializer.securityContext?.runAsUser===29000&&
  initializer.securityContext.runAsGroup===29000&&initializer.image===migration.personalIntegrationsInputs.authority.imageRef&&
  same(initializer.command,['/usr/local/bin/'+init])&&
  same(initializer.volumeMounts,[{name:mount,mountPath:'/run/kodex'}])&&
  pod.volumes?.filter(volume=>volume.name===mount&&volume.emptyDir).length===1);
 const after=structuredClone(before);
 after.template.spec.securityContext.fsGroup=initializer.securityContext.runAsGroup;
 return {part,before,after};
}

function validateDeployment(value,uid) {
 requireValue(value?.apiVersion==='apps/v1'&&value.kind==='Deployment'&&value.metadata?.name===name&&
  value.metadata.namespace===namespace&&value.metadata.uid===uid&&resourceVersion(value.metadata.resourceVersion)&&
  !value.metadata.deletionTimestamp);
}

// Новый Pod никогда не запускал writer. Перезапуск после init fail не даёт
// оснований заменить ранее готовый или частично запущенный worker.
function requireBlockedPods(pods,actual) {
 const expected=actual.spec.template.spec;
 requireValue(Array.isArray(pods)&&pods.length===actual.spec.replicas&&
  new Set(pods.map(pod=>pod.metadata.uid)).size===pods.length&&pods.every(pod=>{
   const status=pod.status, initializer=status?.initContainerStatuses?.[0];
   return !pod.metadata.deletionTimestamp&&status?.phase==='Pending'&&
    same(pod.spec?.securityContext,expected.securityContext)&&same(pod.spec?.initContainers,expected.initContainers)&&
    same(pod.spec?.containers,expected.containers)&&status.initContainerStatuses.length===1&&initializer?.name===init&&
    (initializer.state?.terminated?.exitCode===1||initializer.state?.waiting?.reason==='CrashLoopBackOff'&&initializer.lastState?.terminated?.exitCode===1)&&
    status.containerStatuses?.length===expected.containers.length&&
    expected.containers.every(container=>status.containerStatuses.filter(entry=>entry.name===container.name&&
     entry.state?.waiting?.reason==='PodInitializing'&&!entry.containerID&&!entry.ready&&!entry.started&&entry.restartCount===0).length===1);
  }));
}

function repairContext(migration,events) {
 const {part,before,after}=repairSpecs(migration);
 const binding=events.filter(row=>row.type==='PERSONAL_INTEGRATIONS_READERS_BOUND');
 const start=events.filter(row=>row.type==='PAIR_RESUME_INTENT'&&row.name===name);
 requireValue(binding.length===1&&binding[0].migrationPlanSHA256===fingerprint(migration)&&start.length===1&&
  events.indexOf(binding[0])<events.indexOf(start[0]));
 return {before,after,part,binding:binding[0],start:start[0]};
}

function repairPins(migration,context) {
 return {migrationPlanSHA256:fingerprint(migration),deploymentUID:context.part.uid,operationID:context.binding.operationID,
  beforeSHA256:fingerprint(context.before),afterSHA256:fingerprint(context.after)};
}

// Читатель принимает новый spec только по записи о нашем exact CAS и свежему
// полному readback. Подмена группы в PodTemplate сама по себе остаётся drift.
export function personalArtifactSocketRepairSpec(migration,events,actual) {
 const intents=events.filter(row=>row.type===intentType),acks=events.filter(row=>row.type===ackType);
 if(!intents.length&&!acks.length)return undefined;
 const ctx=repairContext(migration,events),pins=repairPins(migration,ctx);
 requireValue(intents.length===1&&acks.length===1&&events.indexOf(ctx.start)<events.indexOf(intents[0])&&
  events.indexOf(intents[0])<events.indexOf(acks[0])&&
  Object.entries(pins).every(([key,value])=>intents[0][key]===value&&acks[0][key]===value)&&
  intents[0].repairSHA256===fingerprint(pins)&&intents[0].repairSHA256===acks[0].repairSHA256&&
  actual?.metadata?.annotations?.[marker]===acks[0].repairSHA256&&
  resourceVersion(intents[0].resourceVersion)&&resourceVersion(acks[0].resourceVersion)&&
  intents[0].resourceVersion!==acks[0].resourceVersion);
 return ctx.after;
}

export async function repairPersonalArtifactSocket({migration,journal,kube,readPods,authority,readAPI,checkMigration,bindExecutor,approvedSHA256}) {
 const ctx=repairContext(migration,journal.events),{part,before,after}=ctx,pins=repairPins(migration,ctx);
 const get=()=>JSON.parse(kube(['-n',namespace,'get','deployment',name,'-o','json']));
 checkMigration();
 const api=readAPI();
 requireValue(api.openPlanSHA256===ctx.binding.openPlanSHA256);
 const existing=journal.events.filter(row=>row.type===intentType);
 const acknowledged=journal.events.filter(row=>row.type===ackType);
 requireValue(existing.length<=1&&acknowledged.length<=1&&
  !journal.events.some(row=>['PERSONAL_INTEGRATIONS_TERMINAL_ACCEPTED','BACKUP_RESUME_INTENT'].includes(row.type)));
 const actual=get();validateDeployment(actual,part.uid);
 if(existing.length) {
  const [intent]=existing;
  requireValue(approvedSHA256===intent.repairSHA256&&Object.entries(pins).every(([key,value])=>intent[key]===value)&&
   approvedSHA256===fingerprint(pins)&&actual.metadata.annotations?.[marker]===approvedSHA256&&
   same(actual.spec,after)&&actual.metadata.resourceVersion!==intent.resourceVersion&&journal.events.indexOf(intent)>journal.events.indexOf(ctx.start));
  if(!acknowledged.length)journal.append({type:ackType,...pins,repairSHA256:approvedSHA256,resourceVersion:actual.metadata.resourceVersion});
  requireValue(same(personalArtifactSocketRepairSpec(migration,journal.events,actual),actual.spec));
  return {status:'ARTIFACT_SOCKET_REPAIRED',repairSHA256:approvedSHA256};
 }
 requireValue(!acknowledged.length&&same(actual.spec,before)&&!Object.hasOwn(actual.metadata.annotations??{},marker));
 requireBlockedPods(readPods(part.uid),actual);
 const repairSHA256=fingerprint(pins);
 const request=path=>['-n',namespace,'patch','deployment',name,'--type=json','--patch-file='+path,'-o','json'];
 const dryRun=current=>{
  const patch=[{op:'test',path:'/metadata/uid',value:part.uid},{op:'test',path:'/metadata/resourceVersion',value:current.metadata.resourceVersion},
   {op:'test',path:'/spec',value:before},{op:'replace',path:'/spec/template/spec/securityContext/fsGroup',value:after.template.spec.securityContext.fsGroup},
   {op:'add',path:'/metadata/annotations',value:{...current.metadata.annotations,[marker]:repairSHA256}}];
  const predicted=withPrivatePatchFile(patch,path=>JSON.parse(kube([...request(path),'--dry-run=server'])));
  validateDeployment(predicted,part.uid);requireValue(same(predicted.spec,after)&&predicted.metadata.annotations?.[marker]===repairSHA256);
  return patch;
 };
 // План согласует spec, UID и rotation scope. Apply берёт актуальный RV
 // после owner readback, поскольку Deployment status может обновиться.
 if(approvedSHA256===undefined){
  dryRun(actual);
  return {status:'PLAN_ONLY',...pins,repairSHA256};
 }
 requireValue(approvedSHA256===repairSHA256);
 const proof=await authority();
 requireValue(proof.operationID===pins.operationID&&proof.bindingSHA256===ctx.binding.bindingSHA256&&
  proof.state==='PENDING_AUTHORITY'&&proof.policyRevision===110&&readAPI().openPlanSHA256===api.openPlanSHA256);
 const current=get();validateDeployment(current,part.uid);
 requireValue(same(current.spec,before)&&!Object.hasOwn(current.metadata.annotations??{},marker));
 requireBlockedPods(readPods(part.uid),current);
 const patch=dryRun(current),resourceVersion=current.metadata.resourceVersion;
 requireValue(Date.now()-Date.parse(proof.observedAt)<=30000&&Date.parse(proof.observedAt)<=Date.now()+5000);
 bindExecutor();
 journal.append({type:intentType,...pins,repairSHA256,resourceVersion});
 try{withPrivatePatchFile(patch,path=>kube(request(path)));}catch{throw Error('PERSONAL_ARTIFACT_SOCKET_REPAIR_PATCH_UNKNOWN');}
 const readback=get();validateDeployment(readback,part.uid);
 requireValue(same(readback.spec,after)&&readback.metadata.resourceVersion!==resourceVersion&&readback.metadata.annotations?.[marker]===repairSHA256);
 journal.append({type:ackType,...pins,repairSHA256,resourceVersion:readback.metadata.resourceVersion});
 return {status:'ARTIFACT_SOCKET_REPAIRED',repairSHA256};
}
