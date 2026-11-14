import {randomUUID} from 'node:crypto';
import {fingerprint} from './scoped-release.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {componentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';

const name='backup-controller',namespace='kodex-system',marker='kodex.dev/backup-controller-maintenance';
const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const uuid=value=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value??'');
const kinds=['Deployment','StatefulSet','DaemonSet','CronJob','Job','ReplicaSet','Pod'];
const registry=['backup-controller','kodex-postgresql-runtime-credentials'];
const secretRegistry=['backup-controller-credentials','kodex-postgresql-runtime-credentials'];
export const backupControllerMaintenanceHeader=plan=>({schema:'kodex.backup-controller-maintenance-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID});

// Namespace discovery не использует selector: label не может скрыть прежнего
// producer. В процесс попадают только identity, owner edges и имена refs, не env values.
export function readBackupProducerInventory(kube){
 const fields=['.kind','.metadata.name','.metadata.uid','.metadata.namespace','.metadata.labels.app\\.kubernetes\\.io/name','.metadata.ownerReferences',
  '.spec..serviceAccountName','.spec..secretName','.spec..secretKeyRef.name','.spec..secretRef.name','.spec..secret.name',
  '.spec.replicas','.status.replicas','.metadata.generation','.status.observedGeneration'];
 const projection='{range .items[*]}'+fields.map(field=>'{'+field+'}').join('{"\\t"}')+'{"\\n"}{end}';
 const raw=kube(['-n',namespace,'get','deployments,statefulsets,daemonsets,cronjobs,jobs,replicasets,pods','-o','jsonpath='+projection]);
 fail(typeof raw==='string'&&Buffer.byteLength(raw)<=1<<20,'BACKUP_MAINTENANCE_INVENTORY_BOUND');
 const lines=raw.split('\n').filter(Boolean);fail(lines.length<=4096,'BACKUP_MAINTENANCE_INVENTORY_BOUND');
 const names=text=>text?text.split(' ').map(value=>{fail(/^[a-z0-9][a-z0-9.-]{0,252}$/.test(value),'BACKUP_MAINTENANCE_INVENTORY_INVALID');return value;}):[];
 const number=text=>{fail(text===''||/^(0|[1-9][0-9]*)$/.test(text),'BACKUP_MAINTENANCE_INVENTORY_INVALID');const value=text===''?undefined:Number(text);fail(value===undefined||Number.isSafeInteger(value),'BACKUP_MAINTENANCE_INVENTORY_INVALID');return value;};
 const items=lines.map(line=>{
  const v=line.split('\t');fail(v.length===fields.length&&kinds.includes(v[0])&&names(v[1]).length===1&&uuid(v[2])&&v[3]===namespace,'BACKUP_MAINTENANCE_INVENTORY_INVALID');
  const owners=v[5]?JSON.parse(v[5]):[];fail(Array.isArray(owners)&&owners.every(o=>uuid(o.uid)&&typeof o.kind==='string'&&(o.controller===undefined||typeof o.controller==='boolean')),'BACKUP_MAINTENANCE_INVENTORY_INVALID');
  return {kind:v[0],metadata:{name:v[1],uid:v[2],namespace,labels:{'app.kubernetes.io/name':v[4]},ownerReferences:owners,generation:number(v[13])},
   serviceAccounts:names(v[6]),secretRefs:[...names(v[7]),...names(v[8]),...names(v[9]),...names(v[10])],replicas:number(v[11]),statusReplicas:number(v[12]),observedGeneration:number(v[14])};
 });
 fail(new Set(items.map(item=>item.kind+'/'+item.metadata.name)).size===items.length,'BACKUP_MAINTENANCE_INVENTORY_INVALID');
 return {schema:'kodex.backup-producer-inventory/v1',namespace,kinds,items};
}

// Maintenance обслуживает только один зарегистрированный Deployment. Другие
// producers, включая Job/CronJob с тем же credential, требуют отдельной остановки;
// terminal label/счётчик не разрешает считать неизвестный workload завершённым.
export function requireBackupProducerBoundary(inventory,deploymentUID,stopped=false,postgres){
 fail(inventory?.schema==='kodex.backup-producer-inventory/v1'&&inventory.namespace===namespace&&same(inventory.kinds,kinds)&&Array.isArray(inventory.items)&&inventory.items.length<=4096,'BACKUP_MAINTENANCE_INVENTORY_INVALID');
 fail(postgres?.pod==='kodex-postgresql-0'&&uuid(postgres.uid)&&uuid(postgres.ownerUID),'BACKUP_MAINTENANCE_DATABASE_PIN_REQUIRED');
 const items=inventory.items,owned=(item,kind,uid)=>item.metadata?.ownerReferences?.some(o=>o.kind===kind&&o.uid===uid&&o.controller===true);
 const roots=items.filter(item=>item.kind==='Deployment'&&item.metadata.name===name);
 fail(roots.length===1&&roots[0].metadata.uid===deploymentUID,'BACKUP_MAINTENANCE_PRODUCER_CHANGED');
 const sets=items.filter(item=>item.kind==='ReplicaSet'&&owned(item,'Deployment',deploymentUID)),ids=new Set(sets.map(item=>item.metadata.uid));
 for(const item of items){
  const ours=item.kind==='Deployment'&&item.metadata.uid===deploymentUID||item.kind==='ReplicaSet'&&ids.has(item.metadata.uid)||
   item.kind==='Pod'&&item.metadata.ownerReferences?.some(o=>o.kind==='ReplicaSet'&&ids.has(o.uid)&&o.controller===true);
  const candidate=registry.some(value=>item.metadata.name===value||item.metadata.name.startsWith(value+'-')||item.metadata.labels?.['app.kubernetes.io/name']===value||item.serviceAccounts?.includes(value))||item.secretRefs?.some(value=>secretRegistry.includes(value));
  // Сервер БД штатно монтирует runtime credential initializer. Это исключение
  // связано с ранее проверенным backend UID, а не с одним именем/label ресурса.
  const database=item.metadata.labels?.['app.kubernetes.io/name']==='kodex-postgresql'&&
   !item.serviceAccounts?.some(value=>registry.includes(value))&&!item.secretRefs?.includes('backup-controller-credentials')&&
   (item.kind==='StatefulSet'&&item.metadata.name==='kodex-postgresql'&&item.metadata.uid===postgres.ownerUID||
    item.kind==='Pod'&&item.metadata.name===postgres.pod&&item.metadata.uid===postgres.uid&&owned(item,'StatefulSet',postgres.ownerUID));
  fail(!candidate||ours||database,'BACKUP_MAINTENANCE_OTHER_PRODUCER_PRESENT');
 }
 if(stopped)fail(roots[0].replicas===0&&(roots[0].statusReplicas??0)===0&&roots[0].observedGeneration>=roots[0].metadata.generation&&
  sets.every(item=>item.replicas===0&&(item.statusReplicas??0)===0&&item.observedGeneration>=item.metadata.generation),'BACKUP_MAINTENANCE_PRODUCER_NOT_STOPPED');
 return fingerprint(inventory);
}
const active=(deployment,resources)=>activeDeploymentPods(componentServingInventory(resources,name,deployment.metadata.uid),deployment.metadata.uid);
const stopped=(deployment,resources)=>deployment.spec.replicas===0&&deployment.status?.observedGeneration>=deployment.metadata.generation&&
 ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(key=>(deployment.status[key]??0)===0)&&active(deployment,resources).length===0;
function validate(plan){
 fail(plan?.schema==='kodex.backup-controller-maintenance-plan/v1'&&['stop','resume'].includes(plan.action)&&uuid(plan.maintenanceID)&&uuid(plan.deploymentUID)&&
  /^[a-f0-9]{40}$/.test(plan.sourceSHA??'')&&/^\d+$/.test(plan.beforeResourceVersion??'')&&Number.isSafeInteger(plan.replicas)&&plan.replicas>0&&plan.replicas<=8&&
  plan.target?.resources?.some(item=>item.kind==='Deployment'&&item.name===name&&item.uid===plan.deploymentUID)&&
  plan.postgres?.pod==='kodex-postgresql-0'&&uuid(plan.postgres.uid)&&uuid(plan.postgres.ownerUID),'BACKUP_MAINTENANCE_PLAN_INVALID');
 const spec=structuredClone(plan.beforeSpec),annotations={...plan.beforeAnnotations};
 spec.replicas=plan.action==='stop'?0:plan.replicas;
 const value=JSON.stringify({maintenanceID:plan.maintenanceID,targetSHA256:fingerprint(plan.target),replicas:plan.replicas});
 if(plan.action==='stop'){fail(plan.beforeSpec.replicas===plan.replicas&&!annotations[marker],'BACKUP_MAINTENANCE_PLAN_INVALID');annotations[marker]=value;}
 else{fail(plan.beforeSpec.replicas===0&&annotations[marker]===value&&/^[a-f0-9]{64}$/.test(plan.stopPlanSHA256??''),'BACKUP_MAINTENANCE_PLAN_INVALID');delete annotations[marker];}
 fail(same(spec,plan.afterSpec)&&same(annotations,plan.afterAnnotations),'BACKUP_MAINTENANCE_PLAN_INVALID');
}
export function requireStoppedBackupController(deployment,resources,{plan,events},producerInventory){
 validate(plan);requireBackupProducerBoundary(producerInventory,plan.deploymentUID,true,plan.postgres);
 fail(plan.action==='stop'&&deployment.kind==='Deployment'&&deployment.metadata.name===name&&deployment.metadata.namespace===namespace&&
  !deployment.metadata.deletionTimestamp&&deployment.metadata.uid===plan.deploymentUID&&same(deployment.spec,plan.afterSpec)&&same(deployment.metadata.annotations??{},plan.afterAnnotations)&&stopped(deployment,resources),'BACKUP_MAINTENANCE_STOP_REQUIRED');
 fail(Array.isArray(events)&&events[0]?.type==='HEADER'&&Object.entries(backupControllerMaintenanceHeader(plan)).every(([key,value])=>events[0][key]===value),'BACKUP_MAINTENANCE_JOURNAL_INVALID');
 const intent=events.findLastIndex(event=>event.type==='INTENT'&&event.step==='apply');
 fail(intent>0&&events.some((event,index)=>index>intent&&event.type==='OBSERVED'&&event.state==='STOPPED'&&event.deploymentUID===plan.deploymentUID&&event.maintenanceID===plan.maintenanceID),'BACKUP_MAINTENANCE_STOP_UNCONFIRMED');
 return {planSHA256:fingerprint(plan),targetSHA256:fingerprint(plan.target),deploymentUID:plan.deploymentUID,sourceSHA:plan.sourceSHA};
}
export function planBackupControllerMaintenance(deployment,target,observed,sourceSHA,action,previous,producerInventory){
 const scope=portableRunnerTarget(target,observed);
 fail(scope.allows(deployment)&&deployment.kind==='Deployment'&&deployment.metadata.name===name&&['stop','resume'].includes(action)&&/^[a-f0-9]{40}$/.test(sourceSHA??'')&&
  !deployment.spec.paused&&deployment.status?.observedGeneration>=deployment.metadata.generation,'BACKUP_MAINTENANCE_SCOPE_INVALID');
 requireBackupProducerBoundary(producerInventory,deployment.metadata.uid,action==='resume',observed.postgres);
 if(action==='resume'){
  requireStoppedBackupController(deployment,observed.resources,previous,producerInventory);
  fail(same(target,previous.plan.target)&&sourceSHA===previous.plan.sourceSHA&&same(observed.postgres,previous.plan.postgres),'BACKUP_MAINTENANCE_SCOPE_INVALID');
 }
 const maintenanceID=action==='stop'?randomUUID():previous.plan.maintenanceID,replicas=action==='stop'?deployment.spec.replicas:previous.plan.replicas;
 const afterSpec={...structuredClone(deployment.spec),replicas:action==='stop'?0:replicas},afterAnnotations={...deployment.metadata.annotations};
 if(action==='stop')afterAnnotations[marker]=JSON.stringify({maintenanceID,targetSHA256:scope.digest,replicas});else delete afterAnnotations[marker];
 const plan={schema:'kodex.backup-controller-maintenance-plan/v1',sourceSHA,action,target,postgres:structuredClone(observed.postgres),maintenanceID,replicas,deploymentUID:deployment.metadata.uid,
  beforeResourceVersion:deployment.metadata.resourceVersion,beforeSpec:structuredClone(deployment.spec),afterSpec,beforeAnnotations:{...deployment.metadata.annotations},afterAnnotations,
  ...(action==='resume'?{stopPlanSHA256:fingerprint(previous.plan)}:{})};validate(plan);return plan;
}
// Caller передаёт fresh target/owner gate: для resume он дополнительно доказывает
// CANONICAL_BACKUP043/044 и terminal cleanup. Сохранённый boolean не является gate.
export async function backupControllerMaintenanceStep({phase,plan,journal,get,resources,producerInventory,patch,gate}){
 validate(plan);fail(['apply','status'].includes(phase),'BACKUP_MAINTENANCE_PHASE_INVALID');
 fail(Array.isArray(journal?.events)&&journal.events[0]?.type==='HEADER'&&Object.entries(backupControllerMaintenanceHeader(plan)).every(([key,value])=>journal.events[0][key]===value),'BACKUP_MAINTENANCE_JOURNAL_INVALID');
 const current=get();fail(current.kind==='Deployment'&&current.metadata.name===name&&current.metadata.namespace===namespace&&current.metadata.uid===plan.deploymentUID&&!current.metadata.deletionTimestamp,'BACKUP_MAINTENANCE_UID_CHANGED');
 const inventory=producerInventory();requireBackupProducerBoundary(inventory,plan.deploymentUID,false,plan.postgres);
 const before=same(current.spec,plan.beforeSpec)&&same(current.metadata.annotations??{},plan.beforeAnnotations),after=same(current.spec,plan.afterSpec)&&same(current.metadata.annotations??{},plan.afterAnnotations);
 fail(before||after,'BACKUP_MAINTENANCE_SPEC_CHANGED');
 let state=before?'BEFORE':'CONVERGING';
 if(after&&plan.action==='stop'&&stopped(current,resources())){requireBackupProducerBoundary(inventory,plan.deploymentUID,true,plan.postgres);state='STOPPED';}
 if(after&&plan.action==='resume'&&componentServingReady(current,active(current,resources()),plan.replicas))state='RESUMED';
 const result={type:'OBSERVED',state,deploymentUID:plan.deploymentUID,maintenanceID:plan.maintenanceID,resourceVersion:current.metadata.resourceVersion};
 if(phase==='status'){journal.append(result);return result;}
 const intent=journal.events.findLastIndex(event=>event.type==='INTENT'&&event.step==='apply');
 if(after){fail(intent>0,'BACKUP_MAINTENANCE_UNOWNED_STATE');journal.append(result);return result;}
 if(intent>=0)fail(journal.events.some((event,index)=>index>intent&&event.type==='OBSERVED'&&event.state==='BEFORE'&&event.resourceVersion===current.metadata.resourceVersion),'BACKUP_MAINTENANCE_OUTCOME_UNKNOWN');
 else fail(current.metadata.resourceVersion===plan.beforeResourceVersion,'BACKUP_MAINTENANCE_VERSION_CHANGED');
 if(plan.action==='resume'){fail(stopped(current,resources()),'BACKUP_MAINTENANCE_PODS_REMAIN');requireBackupProducerBoundary(inventory,plan.deploymentUID,true,plan.postgres);}
 fail(typeof gate==='function','BACKUP_MAINTENANCE_GATE_REQUIRED');
 const operations=[{op:'test',path:'/metadata/uid',value:plan.deploymentUID},{op:'test',path:'/metadata/resourceVersion',value:current.metadata.resourceVersion},{op:'test',path:'/spec',value:plan.beforeSpec},
  {op:'add',path:'/metadata/annotations',value:plan.afterAnnotations},{op:'replace',path:'/spec',value:plan.afterSpec}];
 patch(operations,true);const proof=await gate();
 fail(proof?.targetSHA256===fingerprint(plan.target)&&proof.deploymentUID===plan.deploymentUID&&proof.sourceSHA===plan.sourceSHA&&
  (plan.action!=='resume'||proof.planSHA256===plan.stopPlanSHA256&&proof.canonicalBackup===true&&proof.cleanupTerminal===true),'BACKUP_MAINTENANCE_GATE_CHANGED');
 requireBackupProducerBoundary(producerInventory(),plan.deploymentUID,plan.action==='resume',plan.postgres);
 journal.append({type:'INTENT',step:'apply',beforeResourceVersion:current.metadata.resourceVersion});
 try{patch(operations,false);}catch(error){journal.append({type:'UNKNOWN',step:'apply'});throw error;}
 return backupControllerMaintenanceStep({phase:'status',plan,journal,get,resources,producerInventory,patch,gate});
}
