import {resolve} from 'node:path';
import {readIssuerHistoricalMissingPodStatus} from './artifact-retention-issuer-historical-pod.mjs';
import {createHash} from 'node:crypto';
import {fingerprint} from './scoped-release.mjs';
import {inspectSource} from './application-source.mjs';
import {parseAuthorityPrivateJSON,readAuthorityPrivateJSON,readAuthorityPrivateText} from './authority-catalog-transition.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {issuerMigrationHistoricalReadinessBoundary,issuerMigrationHistoricalReadinessIO,issuerMigrationJournalHeader,
 validateIssuerMigrationJournal,validateIssuerMigrationState,executeIssuerMigration,issuerMigrationIO,readArtifactRetentionIssuerMigrationReady} from './artifact-retention-issuer-migration.mjs';

const schema='kodex.artifact-retention-issuer-historical-context/v1',purpose='HISTORICAL_ROLE_ONLY';
const namespace='kodex-system',claimName='data-kodex-postgresql-0';
const keys=['context','clusterUID','namespaceUID','installationUID','installationSHA256'];
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const hash=raw=>createHash('sha256').update(raw).digest('hex');
const fail=(ok,code)=>{if(!ok)throw Error('ISSUER_HISTORICAL_'+code);};

// Incident records служат только старым PV/claim/affinity anchor. Число Ready
// Pods и boolean восстановления не заменяют текущие storage/SQL проверки.
// В этих receipts нет historical full PV spec или inode hash: они не выдумываются.
function incident(input,plan,io){
 const text=io.readPrivateText??readAuthorityPrivateText;
 const beforeRaw=text(input.incidentPlanPath),afterRaw=text(input.incidentReceiptPath);
 const before=parseAuthorityPrivateJSON(beforeRaw),after=parseAuthorityPrivateJSON(afterRaw);
 fail(same(before.scope,{context:plan.target.context,clusterUID:plan.target.clusterUID,namespaceUID:plan.target.namespaceUID})&&
  Array.isArray(before.entries)&&before.entries.length>0&&before.entries.length<=16&&
  Array.isArray(before.pvs)&&before.pvs.length>0&&before.pvs.length<=4096&&new Set(before.pvs.map(row=>row.uid)).size===before.pvs.length&&
  after.status==='RECOVERED'&&after.clusterUID===plan.target.clusterUID&&after.namespaceUID===plan.target.namespaceUID&&
  after.existingPVsUnchanged===before.pvs.length&&Number.isFinite(Date.parse(after.observedAt)), 'INCIDENT_REJECTED');
 return {before,proof:{planPath:input.incidentPlanPath,receiptPath:input.incidentReceiptPath,
  planSHA256:hash(beforeRaw),receiptSHA256:hash(afterRaw)}};
}

// Канонический Pod уже проверен issuer boundary. Здесь связываются его data
// claim, current bound PVC/PV и наблюдённые до инцидента UID/affinity. После
// capture закрепляется полный current spec; это не новый migration baseline.
export function readIssuerMigrationDurableStorage(current,prior,kube){
 const get=(kind,name)=>parseAuthorityPrivateJSON(kube(['-n',namespace,'get',kind,name,'-o','json']));
 const pod=get('pod',current.postgres.pod),set=get('statefulset','kodex-postgresql');
 fail(pod.metadata?.uid===current.postgres.uid&&set.metadata?.uid===current.postgres.ownerUID&&
  pod.spec.volumes?.filter(v=>v.name==='data'&&v.persistentVolumeClaim?.claimName===claimName).length===1,'STORAGE_OWNER_CHANGED');
 const claim=get('pvc',claimName);
 fail(claim.apiVersion==='v1'&&claim.kind==='PersistentVolumeClaim'&&claim.metadata?.name===claimName&&claim.metadata.namespace===namespace&&
  uuid(claim.metadata.uid)&&!claim.metadata.deletionTimestamp&&claim.status?.phase==='Bound'&&typeof claim.spec?.volumeName==='string', 'CLAIM_REJECTED');
 const pv=parseAuthorityPrivateJSON(kube(['get','pv',claim.spec.volumeName,'-o','json'])),ref=pv.spec?.claimRef;
 const rows=prior.pvs.filter(row=>row.uid===pv.metadata?.uid&&row.name===pv.metadata?.name);
 fail(pv.apiVersion==='v1'&&pv.kind==='PersistentVolume'&&pv.metadata.name===claim.spec.volumeName&&uuid(pv.metadata.uid)&&
  !pv.metadata.deletionTimestamp&&pv.status?.phase==='Bound'&&ref?.kind==='PersistentVolumeClaim'&&ref.namespace===namespace&&
  ref.name===claimName&&ref.uid===claim.metadata.uid&&rows.length===1&&rows[0].claimUID===claim.metadata.uid&&rows[0].ns===namespace&&
  rows[0].phase==='Bound'&&rows[0].nodeAffinitySHA256===fingerprint(pv.spec.nodeAffinity)&&
  same(pv.spec.accessModes,claim.spec.accessModes)&&same(pv.spec.accessModes,['ReadWriteOnce'])&&
  (pv.spec.volumeMode??'Filesystem')==='Filesystem'&&(claim.spec.volumeMode??'Filesystem')==='Filesystem'&&
  (pv.spec.storageClassName??'')===(claim.spec.storageClassName??''),'PV_CHANGED');
 const affinity=pv.spec.nodeAffinity?.required?.nodeSelectorTerms;
 fail(Array.isArray(affinity)&&affinity.length===1&&Array.isArray(affinity[0].matchExpressions)&&affinity[0].matchExpressions.length===1&&
  !affinity[0].matchFields?.length&&affinity[0].matchExpressions[0].key==='kubernetes.io/hostname'&&
  affinity[0].matchExpressions[0].operator==='In'&&same(affinity[0].matchExpressions[0].values,[pod.spec.nodeName])&&
  !pv.spec.csi&&!pv.spec.nfs,'LOCAL_STORAGE_REJECTED');
 const local=pv.spec.local,host=pv.spec.hostPath,source=local?{kind:'local',path:local.path}:
  host?{kind:'hostPath',path:host.path,type:host.type}:null;
 fail(Boolean(local)!==Boolean(host)&&typeof source?.path==='string'&&source.path.startsWith('/')&&!source.path.split('/').includes('..')&&
  (local||['Directory','DirectoryOrCreate'].includes(host.type)),'LOCAL_STORAGE_REJECTED');
 const node=parseAuthorityPrivateJSON(kube(['get','node',pod.spec.nodeName,'-o','json']));
 const priorNodes=prior.entries.filter(row=>row.name===pod.spec.nodeName);
 fail(node.kind==='Node'&&node.metadata?.name===pod.spec.nodeName&&uuid(node.metadata.uid)&&!node.metadata.deletionTimestamp&&
  node.metadata.labels?.['kubernetes.io/hostname']===pod.spec.nodeName&&priorNodes.length===1&&priorNodes[0].uid===node.metadata.uid,'NODE_CHANGED');
 return {statefulSetUID:set.metadata.uid,claim:{name:claimName,uid:claim.metadata.uid,specSHA256:fingerprint(claim.spec)},
  volume:{name:pv.metadata.name,uid:pv.metadata.uid,specSHA256:fingerprint(pv.spec),nodeAffinitySHA256:fingerprint(pv.spec.nodeAffinity),source},
  node:{name:pod.spec.nodeName,uid:node.metadata.uid}};
}

function load(input,io){
 const plan=(io.readPrivateJSON??readAuthorityPrivateJSON)(input.planPath);
 fail(plan.source?.revision===input.sourceSHA&&(io.inspectSource??inspectSource)(plan.source.path).revision===input.sourceSHA&&
  plan.evidencePath===input.evidencePath&&keys.every(key=>plan.target?.[key]===input.target?.[key]),'SOURCE_CHANGED');
 return plan;
}

function originalReady(input,plan,journal,result){
 return {schema:'kodex.artifact-retention-issuer-migration-ready/v1',sourceSHA:input.sourceSHA,
  target:Object.fromEntries(keys.map(key=>[key,plan.target[key]])),planSHA256:fingerprint(plan),evidenceSHA256:journal.bytesSHA256,
  image:plan.image,jobUID:result.jobUID,podUID:result.podUID,migrationVersion:result.migrationVersion};
}

// Создаётся только явный R/O context, не замена accepted role plan/receipt.
// Original Job/Pod/spec проходят тот же terminal status/join и fresh schema/ACL.
export async function captureIssuerMigrationHistoricalReadiness(input,kube,io={}){
 const plan=load(input,io),history=incident(input,plan,io);
 const journal=(io.openJournal??((path,header)=>privateJournal(path,header,{readOnly:true})))(input.evidencePath,issuerMigrationJournalHeader(plan));
 let acceptedReceipt;
 try{
  const expected=issuerMigrationIO(plan,kube).expectedJob();
  const history=validateIssuerMigrationJournal(journal.events,plan,expected);
  fail(history.terminal?.type==='PASS'&&!history.rejected,'ORIGINAL_PASS_REQUIRED');
  acceptedReceipt=originalReady(input,plan,journal,{...history.terminal,migrationVersion:20261115000200});
 }finally{journal.close();}
 const current={purpose,...issuerMigrationHistoricalReadinessBoundary(plan,kube)};
 const storage=readIssuerMigrationDurableStorage(current,history.before,kube);
 const body={schema,purpose,sourceSHA:input.sourceSHA,target:Object.fromEntries(keys.map(key=>[key,plan.target[key]])),
  planSHA256:fingerprint(plan),originalPostgres:plan.postgres,current,storage,incident:history.proof,acceptedReceipt};
 const context={...body,contextSHA256:fingerprint(body)};
 await readArtifactRetentionIssuerHistoricalMigrationReady({...input,context},kube,io);
 return context;
}

// Explicit context разрешает лишь проверку прежнего role PASS после замены
// PostgreSQL Pod. Mutating issuer IO и rotation CAS не используют этот reader.
export async function readArtifactRetentionIssuerHistoricalMigrationReady(input,kube,io={}){
 const plan=load(input,io),context=structuredClone(input.context);
 const {contextSHA256,...body}=context??{};
 fail(Object.keys(body).sort().join(',')==='acceptedReceipt,current,incident,originalPostgres,planSHA256,purpose,schema,sourceSHA,storage,target'&&
  body.current&&Object.keys(body.current).sort().join(',')==='backend,postgres,purpose,target'&&body.schema===schema&&body.purpose===purpose&&body.sourceSHA===input.sourceSHA&&body.planSHA256===fingerprint(plan)&&
  same(body.target,Object.fromEntries(keys.map(key=>[key,plan.target[key]])))&&same(body.originalPostgres,plan.postgres)&&
  body.acceptedReceipt?.schema==='kodex.artifact-retention-issuer-migration-ready/v1'&&
  body.acceptedReceipt.planSHA256===body.planSHA256&&contextSHA256===fingerprint(body),'CONTEXT_REJECTED');
 const incidentInput={incidentPlanPath:body.incident?.planPath,incidentReceiptPath:body.incident?.receiptPath};
 const history=incident(incidentInput,plan,io);fail(same(history.proof,body.incident),'INCIDENT_CHANGED');
 const readStorage=()=>readIssuerMigrationDurableStorage(body.current,history.before,kube);
 fail(same(readStorage(),body.storage),'STORAGE_CHANGED');
 const journal=(io.openJournal??((path,header)=>privateJournal(path,header,{readOnly:true})))(input.evidencePath,issuerMigrationJournalHeader(plan));
 try{
  const readOnly=issuerMigrationHistoricalReadinessIO(plan,kube,body.current),preflight=readOnly.preflight;
  readOnly.preflight=()=>{fail(same(readStorage(),body.storage),'STORAGE_CHANGED');preflight();};
  const known=validateIssuerMigrationJournal(journal.events,plan,readOnly.expectedJob());
  fail(known.terminal?.type==='PASS'&&!known.rejected,'ORIGINAL_PASS_REQUIRED');
  const observedJob=await readOnly.getJob();fail(observedJob,'RETAINED_JOB_REQUIRED');
  const observedPods=await readOnly.getPods(observedJob);
  fail(Array.isArray(observedPods?.items)&&!observedPods.metadata?.continue,'POD_INVENTORY_REJECTED');
  const result=observedPods.items.length===0?
   await readIssuerHistoricalMissingPodStatus(plan,journal,readOnly,history.before,kube):
   await executeIssuerMigration('status',plan,journal,readOnly);
  fail(result.status==='PASS'&&result.migrationVersion===20261115000200,'ROLE_REJECTED');
  fail(same(readStorage(),body.storage),'STORAGE_CHANGED');readOnly.preflight();
  validateIssuerMigrationState(readOnly.readState(),true);
  const ready=originalReady(input,plan,journal,result);fail(same(ready,body.acceptedReceipt),'RECEIPT_CHANGED');
  return {schema:'kodex.artifact-retention-issuer-historical-readiness/v1',purpose,contextSHA256,ready,
   postgres:body.current.postgres,backend:body.current.backend,storage:body.storage};
 }finally{journal.close();}
}


// Ref закрепляется отдельно от исходного issuer migration plan. Только owned
// private context с точным semantic SHA выбирает historical READ profile.
export function normalizeIssuerMigrationReadinessReference(value){
 if(value===undefined)return undefined;
 fail(value&&Object.keys(value).sort().join(',')==='contextPath,contextSHA256'&&typeof value.contextPath==='string'&&
  value.contextPath===resolve(value.contextPath)&&!/[\r\n\0]/.test(value.contextPath)&&/^[a-f0-9]{64}$/.test(value.contextSHA256??''),'REFERENCE_REJECTED');
 return {contextPath:value.contextPath,contextSHA256:value.contextSHA256};
}

// Совместимый facade оставляет normal путь прежним и не даёт historical
// context generic CAS. Typed proof возвращается отдельно от original ready/v1.
export async function readOptionalIssuerMigrationReadiness({issuerMigration,issuerMigrationReadiness,target},kube,io={}){
 const ref=normalizeIssuerMigrationReadinessReference(issuerMigrationReadiness);
 if(!ref)return {ready:await readArtifactRetentionIssuerMigrationReady({...issuerMigration,target},kube)};
 const context=(io.readPrivateJSON??readAuthorityPrivateJSON)(ref.contextPath);
 fail(context?.contextSHA256===ref.contextSHA256,'REFERENCE_CHANGED');
 const historicalReadiness=await readArtifactRetentionIssuerHistoricalMigrationReady({...issuerMigration,target,context},kube,io);
 return {ready:historicalReadiness.ready,historicalReadiness};
}
