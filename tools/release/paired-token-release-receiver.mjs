import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {performance} from 'node:perf_hooks';
import {resolve} from 'node:path';
import {lstatSync} from 'node:fs';
import {fingerprint} from './scoped-release.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {componentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {requireClosedAPIMaintenance} from './component-api-maintenance.mjs';
import {requireClosedAPIAdmission} from './component-api-traffic.mjs';
import {sameCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';
import {pairedTokenReleaseNames,pairedTokenReleaseProfile,validatePairedTokenReleaseManifest,
 pairedTokenReleaseOwnerDeadline,verifyPairedTokenReleasePlan,verifyPairedTokenReleaseOwner} from './paired-token-release-plan.mjs';

const ensure=(value,code)=>{if(!value)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const stages=Object.freeze(['stop-ig','stop-cp','stage-cp','stage-ig','resume-cp','resume-ig']);
const component=step=>step.endsWith('-ig')?'integration-gateway':'control-plane';
const schema='kodex.paired-token-effect-journal/v1';
const maxEvents=64;
const receiverFlights=new WeakSet();
const casPermits=new WeakMap();
const writableEffectJournals=new WeakSet();
const effectJournalPaths=new WeakMap();
const maxCASMilliseconds=5000;

// Только живой advance после fsync INTENT может выдать одноразовый допуск.
// Clone, повтор и передача его другому журналу закрыты до транспорта.
export function takePairedTokenCASPermit(permit,request,journal,recordSHA256){
 const claim=casPermits.get(permit);
 ensure(claim&&writableEffectJournals.has(journal)&&claim.journal===journal&&claim.recordSHA256===recordSHA256&&
  claim.requestSHA256===fingerprint(request),'PAIRED_RECEIVER_CAS_PERMIT_REQUIRED');
 casPermits.delete(permit);
 return {remaining:claim.remaining,authorize:claim.authorize};
}

const forwardReceiptInspection=Symbol('forward-receipt-inspection');

// Metadata-only consumer общего parser; он не создаёт ports/receiver methods
// и не заменяет текущие owner/API/readiness проверки terminal reader.
export function inspectPairedTokenForwardReceipt({manifest,record,journal}){
 return pairedTokenReleaseReceiver({manifest,record,journal},forwardReceiptInspection);
}

function validateRecord(manifest,record){
 validatePairedTokenReleaseManifest(manifest);
 ensure(record?.schema==='kodex.paired-token-release-record/v1'&&record.plan?.schema==='kodex.paired-token-release-plan/v1'&&
  record.plan.profile===pairedTokenReleaseProfile&&record.plan.state==='PLAN_ONLY'&&
  record.plan.releaseReadiness==='BLOCKED_LIFECYCLE_NOT_IMPLEMENTED'&&record.plan.sourceSHA===manifest.sourceSHA&&
  record.plan.manifestSHA256===fingerprint(manifest)&&same(record.plan.target,manifest.target)&&
  same(record.plan.components?.map(c=>c.name),pairedTokenReleaseNames),'PAIRED_RECEIVER_RECORD_REJECTED');
 for(const p of record.plan.components){
  const part=manifest.components.find(c=>c.name===p.name),expected=structuredClone(p.beforeSpec);
  ensure(p.uid===part.uid&&fingerprint(expected)===part.beforeSpecSHA256&&
   Number.isSafeInteger(expected.replicas)&&expected.replicas>=1&&expected.replicas<=8&&
   same(p.servingPodUIDs,[...new Set(p.servingPodUIDs)].sort())&&p.servingPodUIDs.length===expected.replicas,
   'PAIRED_RECEIVER_PREIMAGE_CHANGED');
  const app=expected.template.spec.containers.find(c=>c.name===p.name);
  ensure(app?.image===part.beforeImage,'PAIRED_RECEIVER_PREIMAGE_CHANGED');
  app.image=`registry.local.kodex/kodex/${p.name}@${part.publication.publicationPlan.image.digest}`;
  expected.template.metadata.annotations['kodex.dev/component-source-sha']=manifest.sourceSHA;
  ensure(same(expected,p.afterSpec)&&p.imageRef===app.image,'PAIRED_RECEIVER_PREIMAGE_CHANGED');
 }
}

// Scope принадлежат исходному plan; privateJournal уже обеспечивает fsync,
// exact header, process-exclusive lock и отказ symlink/truncated history.
export function openPairedTokenEffectJournal(path,manifest,record,options={}){
 validateRecord(manifest,record);
 const journal=privateJournal(path,{schema,recordSHA256:fingerprint(record),manifestSHA256:fingerprint(manifest),
  sourceSHA:manifest.sourceSHA,targetSHA256:fingerprint(manifest.target)},options);
 effectJournalPaths.set(journal,resolve(path));
 const close=journal.close;
 journal.close=()=>{effectJournalPaths.delete(journal);writableEffectJournals.delete(journal);close();};
 if(!options.readOnly){
  writableEffectJournals.add(journal);
 }
 return journal;
}

// Журнал восстановления Service однозначно наследует путь исходной пары.
// Закрытый или read-only handle не может открыть новый журнал эффектов.
export function pairedTokenRestoreJournalPath(journal){
 const path=effectJournalPaths.get(journal);
 ensure(path&&writableEffectJournals.has(journal),'PAIRED_RESTORE_JOURNAL_SOURCE_REQUIRED');
 return path+'.service-restore.jsonl';
}

// Восстановление Service и rollback используют один допуск к переходу пары.
export async function withPairedTokenEffectFlight(journal,operation){
 ensure(!receiverFlights.has(journal),'PAIRED_RECEIVER_BUSY');receiverFlights.add(journal);
 try{return await operation();}finally{receiverFlights.delete(journal);}
}

function requireNoServiceRestoreIntent(journal,recordSHA256){
 const path=pairedTokenRestoreJournalPath(journal);
 try{lstatSync(path);}catch(error){if(error.code==='ENOENT')return;throw error;}
 const restore=privateJournal(path,{schema:'kodex.paired-token-service-restore/v1',recordSHA256},{readOnly:true});
 try{ensure(restore.events.length===1,'PAIRED_RECEIVER_SERVICE_RESTORE_STARTED');}finally{restore.close();}
}

function specifications(part,direction){
 const before=direction==='forward'?part.beforeSpec:part.afterSpec;
 const after=direction==='forward'?part.afterSpec:part.beforeSpec;
 return {before:structuredClone(before),stopped:{...structuredClone(before),replicas:0},
  staged:{...structuredClone(after),replicas:0},after:structuredClone(after)};
}

function desired(part,direction,step){
 const specs=specifications(part,direction);
 return step.startsWith('stop-')?{before:specs.before,after:specs.stopped}:
  step.startsWith('stage-')?{before:specs.stopped,after:specs.staged}:{before:specs.staged,after:specs.after};
}

// Это закрытый receiver ровно шести действий над CP+IG, не raw patch API.
// Native kube binding намеренно отсутствует: существующий CLI не импортирует
// этот модуль и по-прежнему отклоняет apply/rollback до дочернего процесса.
export function pairedTokenReleaseReceiver({manifest,record,journal,ports},inspection){
 validateRecord(manifest,record);
 manifest=structuredClone(manifest);record=structuredClone(record);
 const recordSeal=fingerprint(record),manifestSeal=fingerprint(manifest);
 const header={schema,recordSHA256:fingerprint(record),manifestSHA256:fingerprint(manifest),sourceSHA:manifest.sourceSHA,targetSHA256:fingerprint(manifest.target)};
 ensure(journal?.events?.[0]?.type==='HEADER'&&Object.entries(header).every(([k,v])=>same(journal.events[0][k],v)),
  'PAIRED_RECEIVER_JOURNAL_SCOPE_CHANGED');
 ensure(inspection===forwardReceiptInspection||ports&&['captureBefore','readComponent','readOwner','readAPI','readSource','readJoin','dryRunComponent','replaceFrozenComponentSpec']
  .every(k=>typeof ports[k]==='function'),'PAIRED_RECEIVER_NATIVE_FACTORY_UNBOUND');
 const append=row=>{ensure(journal.events.length<maxEvents,'PAIRED_RECEIVER_JOURNAL_BOUND_EXCEEDED');journal.append(row);};
 const part=name=>record.plan.components.find(p=>p.name===name);
 const rows=(direction,type)=>journal.events.filter(e=>e.direction===direction&&e.type===type);
 const seals=()=>ensure(fingerprint(record)===recordSeal&&fingerprint(manifest)===manifestSeal&&
  Object.entries(header).every(([k,v])=>same(journal.events[0][k],v)),'PAIRED_RECEIVER_RECORD_CHANGED');
 function validateHistory(){
  seals();
  ensure(journal.events.length<=maxEvents,'PAIRED_RECEIVER_JOURNAL_BOUND_EXCEEDED');
  const allowed=['HEADER','BOUND','ROLLBACK_BOUND','INTENT','ACCEPTED','UNKNOWN','ACK','PAIR_COMPLETE'];
  ensure(journal.events.every(e=>allowed.includes(e.type))&&journal.events.filter(e=>e.type==='BOUND').length<=1,
   'PAIRED_RECEIVER_JOURNAL_INVALID');
  const keys={HEADER:['type','schema','recordSHA256','manifestSHA256','sourceSHA','targetSHA256','at'],
   BOUND:['type','recordSHA256','postgres','backend','authorityHead','at'],
   ROLLBACK_BOUND:['type','compatibilitySHA256','at'],
   INTENT:['type','direction','step','name','uid','beforeResourceVersion','beforeSpecSHA256','afterSpecSHA256','priorPodUIDs','at'],
   ACCEPTED:['type','direction','step','name','uid','resourceVersion','at'],UNKNOWN:['type','direction','step','name','at'],
   ACK:['type','direction','step','name','uid','afterSpecSHA256','podUIDs','at'],PAIR_COMPLETE:['type','direction','recordSHA256','at']};
  ensure(journal.events.every((e,i)=>same(Object.keys(e).filter(k=>k!=='joinSHA256').sort(),[...keys[e.type]].sort())&&
   (!Object.hasOwn(e,'joinSHA256')||e.type==='ACK')&&
   (i===0?e.type==='HEADER':e.type!=='HEADER')&&(!['HEADER','BOUND','ROLLBACK_BOUND'].includes(e.type)?['forward','rollback'].includes(e.direction):true)&&
   (e.type!=='ACK'||Array.isArray(e.podUIDs)&&new Set(e.podUIDs).size===e.podUIDs.length&&
    (e.step.startsWith('resume-')?!Object.hasOwn(e,'joinSHA256'):typeof e.joinSHA256==='string'&&/^[a-f0-9]{64}$/.test(e.joinSHA256)))),
   'PAIRED_RECEIVER_JOURNAL_INVALID');
  const bound=binding();
  ensure(!bound||bound.recordSHA256===fingerprint(record),'PAIRED_RECEIVER_JOURNAL_INVALID');
  ensure(!journal.events.some(e=>e.type==='INTENT')||bound&&journal.events.indexOf(bound)===1,
   'PAIRED_RECEIVER_JOURNAL_INVALID');
  ensure(journal.events.filter(e=>e.type==='ROLLBACK_BOUND').length<=1,'PAIRED_RECEIVER_JOURNAL_INVALID');
  for(const direction of ['forward','rollback']){
   const intents=rows(direction,'INTENT'),acks=rows(direction,'ACK');
   ensure(intents.length<=6&&acks.length<=intents.length&&intents.length<=acks.length+1,
    'PAIRED_RECEIVER_JOURNAL_INVALID');
   ensure(intents.every((e,i)=>e.step===stages[i]&&e.name===component(e.step))&&
    acks.every((e,i)=>e.step===stages[i]&&e.name===component(e.step)),
    'PAIRED_RECEIVER_JOURNAL_INVALID');
   for(const [ordinal,intent] of intents.entries()){
    const index=journal.events.indexOf(intent),p=part(intent.name),spec=desired(p,direction,intent.step);
    ensure(intent.uid===p.uid&&intent.beforeSpecSHA256===fingerprint(spec.before)&&intent.afterSpecSHA256===fingerprint(spec.after)&&
     /^[1-9][0-9]{0,19}$/.test(intent.beforeResourceVersion)&&
     (ordinal===0||journal.events.indexOf(intent)>journal.events.indexOf(acks[ordinal-1]))&&
     Array.isArray(intent.priorPodUIDs)&&new Set(intent.priorPodUIDs).size===intent.priorPodUIDs.length&&
     (intent.step.startsWith('stop-')?intent.priorPodUIDs.length===spec.before.replicas:intent.priorPodUIDs.length===0)&&
     journal.events.filter(e=>e.direction===direction&&e.step===intent.step&&e.type==='ACCEPTED').length<=1&&
     journal.events.filter(e=>e.direction===direction&&e.step===intent.step&&e.type==='UNKNOWN').length<=1&&
     journal.events.filter(e=>e.direction===direction&&e.step===intent.step&&e.type==='ACK').every(e=>
      journal.events.indexOf(e)>index&&e.uid===p.uid&&e.afterSpecSHA256===intent.afterSpecSHA256),
     'PAIRED_RECEIVER_JOURNAL_INVALID');
   }
   for(const event of journal.events.filter(e=>e.direction===direction&&['ACCEPTED','UNKNOWN','ACK'].includes(e.type))){
    const intent=intents.find(e=>e.step===event.step);
    ensure(intent&&journal.events.indexOf(event)>journal.events.indexOf(intent)&&event.name===intent.name&&
     (event.type!=='ACCEPTED'||event.uid===intent.uid&&/^[1-9][0-9]{0,19}$/.test(event.resourceVersion)&&
      BigInt(event.resourceVersion)>BigInt(intent.beforeResourceVersion)), 'PAIRED_RECEIVER_JOURNAL_INVALID');
   }
   ensure(rows(direction,'PAIR_COMPLETE').length<=1&&rows(direction,'PAIR_COMPLETE').every(e=>acks.length===6&&e.recordSHA256===fingerprint(record)&&
    journal.events.indexOf(e)>journal.events.indexOf(acks[5])),
    'PAIRED_RECEIVER_JOURNAL_INVALID');
  }
  if(rows('rollback','INTENT').length)ensure(rows('forward','ACK').length===6&&rows('forward','PAIR_COMPLETE').length===1&&
   journal.events.indexOf(rows('rollback','INTENT')[0])>journal.events.indexOf(rows('forward','PAIR_COMPLETE')[0]),
   'PAIRED_RECEIVER_FORWARD_RECEIPT_REQUIRED');
 }
 const binding=()=>journal.events.find(e=>e.type==='BOUND');
 function observe(name){
  const result=ports.readComponent(name),p=part(name),actual=result.deployment;
  ensure(actual?.apiVersion==='apps/v1'&&actual.kind==='Deployment'&&actual.metadata?.name===name&&actual.metadata.namespace==='kodex-system'&&
   actual.metadata.uid===p.uid&&!actual.metadata.deletionTimestamp&&/^[1-9][0-9]{0,19}$/.test(actual.metadata.resourceVersion),
   'PAIRED_RECEIVER_COMPONENT_CHANGED');
  ensure(actual.metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&actual.metadata.labels?.['kodex.dev/profile']==='web-only'&&
   actual.metadata.labels?.['kodex.dev/environment']==='production','PAIRED_RECEIVER_COMPONENT_CHANGED');
  const pods=activeDeploymentPods(componentServingInventory(result.resources,name,p.uid),p.uid);
  const joined=actual.spec?.replicas===0&&actual.status?.observedGeneration>=actual.metadata.generation&&
   ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k=>(actual.status[k]??0)===0)&&pods.length===0;
  return {...result,pods,joined,ready:componentServingReady(actual,pods,actual.spec?.replicas)};
 }
 function requireRollbackCompatibility(){
  ensure(typeof ports.readRollbackCompatibility==='function','PAIRED_RECEIVER_ROLLBACK_COMPATIBILITY_REQUIRED');
  const expected={schema:'kodex.paired-token-rollback-compatibility/v1',profile:pairedTokenReleaseProfile,
   recordSHA256:fingerprint(record),forwardReceiptSHA256:fingerprint(journal.events.filter(e=>e.direction==='forward')),
   sourceSHA:manifest.sourceSHA,schemaHeads:manifest.schemaHeadsBefore,
   previous:manifest.components.map(p=>({name:p.name,sourceSHA:p.beforeSourceSHA,image:p.beforeImage})),
   contract:'TOKEN_ALLOWANCE_ADDITIVE_FIELD_5'};
  const receipt=ports.readRollbackCompatibility(expected);
  ensure(receipt&&Object.keys(receipt).length===Object.keys(expected).length+1&&
   Object.entries(expected).every(([k,v])=>same(receipt[k],v))&&
   typeof receipt.sourceReviewSHA256==='string'&&/^[a-f0-9]{64}$/.test(receipt.sourceReviewSHA256)&&!/^0+$/.test(receipt.sourceReviewSHA256),
   'PAIRED_RECEIVER_ROLLBACK_COMPATIBILITY_REQUIRED');
  const bound=journal.events.find(e=>e.type==='ROLLBACK_BOUND'),hash=fingerprint(receipt);
  ensure(!bound||bound.compatibilitySHA256===hash,'PAIRED_RECEIVER_ROLLBACK_COMPATIBILITY_CHANGED');
  return hash;
 }
 function requireSource(){
  const source=ports.readSource();ensure(source?.sourceSHA===manifest.sourceSHA&&source.executorSHA===record.executorSHA,
   'PAIRED_RECEIVER_SOURCE_CHANGED');
 }
 function requireAPIIsolation(){
  const api=ports.readAPI(),scope=portableRunnerTarget(manifest.target,api.observed);
  ensure(scope.allows(api.deployment)&&scope.allows(api.service)&&fingerprint(api.deployment.spec)===manifest.apiSpecSHA256,
   'PAIRED_RECEIVER_API_CHANGED');
  requireClosedAPIMaintenance(api.deployment,api.resources,api.closed);
  const proof=requireClosedAPIAdmission(api.deployment,api.resources,api.closed,{...api.traffic,service:api.service,slices:api.slices,pods:api.pods});
  ensure(same(proof,record.plan.apiAdmission),'PAIRED_RECEIVER_API_CHANGED');
 }
 function gate(direction='forward'){
  seals();requireSource();requireAPIIsolation();
  // Последний owner READ следует за Gateway; первый BOUND backend не remint.
  const owner=verifyPairedTokenReleaseOwner(manifest,ports.readOwner(manifest)),bound=binding();
  ensure(sameCatalogAuthorityBaseline(record.plan.authority,owner.authority)&&(!bound||
   same(bound.postgres,owner.controlPlane.postgres)&&same(bound.backend,owner.controlPlane.backend)&&same(bound.authorityHead,owner.authorityHead)),
   'PAIRED_RECEIVER_OWNER_CHANGED');
  if(direction==='rollback')requireRollbackCompatibility();
  return owner;
 }
 async function bind(){
  if(binding())return;
  ensure(journal.events.length===1,'PAIRED_RECEIVER_UNBOUND_HISTORY');
  const fresh=await ports.captureBefore();verifyPairedTokenReleasePlan(record.plan,manifest,fresh.current);
  ensure(fresh.executorSHA===record.executorSHA&&fresh.configSHA256===record.configSHA256&&fresh.referencesSHA256===record.referencesSHA256&&
   same(fresh.componentEvidence,record.componentEvidence),'PAIRED_RECEIVER_RECORD_CHANGED');
  const owner=gate();
  append({type:'BOUND',recordSHA256:fingerprint(record),postgres:owner.controlPlane.postgres,
   backend:owner.controlPlane.backend,authorityHead:owner.authorityHead});
 }
 function expectedPair(direction,prefix,pendingAfter=false){
  const expected=Object.fromEntries(pairedTokenReleaseNames.map(name=>[name,specifications(part(name),direction).before]));
  for(let i=0;i<prefix+(pendingAfter?1:0);i++){const step=stages[i],name=component(step);expected[name]=desired(part(name),direction,step).after;}
  return expected;
 }
 function inspectPair(direction,prefix,pendingAfter=false){
  const expected=expectedPair(direction,prefix,pendingAfter),states={};
  for(const name of pairedTokenReleaseNames){const state=observe(name);ensure(same(state.deployment.spec,expected[name]),'PAIRED_RECEIVER_SPEC_CHANGED');states[name]=state;}
  return states;
 }
 const podUIDs=state=>state.pods.map(p=>p.metadata.uid).sort();
 const resumeUIDs=(direction,name)=>rows(direction,'ACK').find(e=>e.name===name&&e.step.startsWith('resume-'))?.podUIDs;
 function requireStopUIDs(direction,name,state){
  const expected=direction==='forward'?part(name).servingPodUIDs:resumeUIDs('forward',name);
  ensure(Array.isArray(expected)&&same(podUIDs(state),expected),'PAIRED_RECEIVER_STOP_UIDS_CHANGED');
 }
 function join(name,state,direction){
  ensure(state.joined,'PAIRED_RECEIVER_POSITIVE_JOIN_REQUIRED');
  const previous=rows(direction,'INTENT').find(e=>e.step===`stop-${name==='control-plane'?'cp':'ig'}`)?.priorPodUIDs;
  ensure(Array.isArray(previous)&&previous.length>0,'PAIRED_RECEIVER_POSITIVE_JOIN_REQUIRED');
  // Typed physical join остаётся обязательным port: отсутствие API Pods либо
  // boolean true не заменяет exact old UID/native binding proof.
  const monotonicBegan=performance.now(),wallBegan=Date.now();
  const witness=ports.readJoin({name,uid:part(name).uid,podUIDs:previous,nodeBinding:record.plan.nodeReadback.binding});
  ensure(witness?.schema==='kodex.paired-component-join/v1'&&witness.name===name&&witness.uid===part(name).uid&&
   same(witness.podUIDs,previous)&&witness.nodeBindingSHA256===fingerprint(record.plan.nodeReadback.binding)&&
   witness.nativeState==='JOINED'&&Number.isFinite(witness.observedAt)&&Date.now()>=witness.observedAt&&Date.now()-witness.observedAt<=5000,
   'PAIRED_RECEIVER_POSITIVE_JOIN_REQUIRED');
  // Первый witness задаёт ceiling до blocking read/dry-run/owner IO. Новый
  // owner либо коррекция wall clock не продлевают срок этого join.
  const credit={sha256:fingerprint(witness),wallUntil:witness.observedAt+5000,
   monotonicUntil:monotonicBegan+Math.min(5000,witness.observedAt+5000-wallBegan)};
  verifyJoins([credit]);return credit;
 }
 function verifyJoins(credits){
  ensure(credits.every(c=>performance.now()<c.monotonicUntil&&Date.now()<c.wallUntil),
   'PAIRED_RECEIVER_JOIN_CREDIT_EXPIRED');
 }
 async function status(direction='forward'){
  validateHistory();ensure(['forward','rollback'].includes(direction),'PAIRED_RECEIVER_DIRECTION_REJECTED');
  const prefix=rows(direction,'ACK').length,intent=rows(direction,'INTENT')[prefix];
  if(!intent){
   if(prefix===0&&!binding())return {state:'UNBOUND',direction};
   inspectPair(direction,prefix);
   const admitted=gate(direction),states=inspectPair(direction,prefix);
   if(prefix===6){
    ensure(pairedTokenReleaseNames.every(name=>states[name].ready&&same(podUIDs(states[name]),resumeUIDs(direction,name))),
     'PAIRED_RECEIVER_PAIR_NOT_READY');
    requireSource();requireAPIIsolation();verifyPairedTokenReleaseOwner(manifest,admitted);
    if(!rows(direction,'PAIR_COMPLETE').length)append({type:'PAIR_COMPLETE',direction,recordSHA256:fingerprint(record)});
   }
   return {state:prefix===6?'PAIR_READY_TERMINAL_BLOCKED':'BEFORE',direction,completed:prefix};
  }
  const state=observe(intent.name),spec=desired(part(intent.name),direction,intent.step);
  if(same(state.deployment.spec,spec.before)){inspectPair(direction,prefix);return {state:'UNKNOWN',direction,step:intent.step};}
  ensure(same(state.deployment.spec,spec.after)&&BigInt(state.deployment.metadata.resourceVersion)>BigInt(intent.beforeResourceVersion),
   'PAIRED_RECEIVER_UNKNOWN_OUTCOME');
  const accepted=rows(direction,'ACCEPTED').find(e=>e.step===intent.step);
  ensure(!accepted||BigInt(state.deployment.metadata.resourceVersion)>=BigInt(accepted.resourceVersion),
   'PAIRED_RECEIVER_STALE_ACK_READBACK');
  inspectPair(direction,prefix,true);
  const resuming=intent.step.startsWith('resume-');
  if(resuming?!state.ready:!state.joined)return {state:'CONVERGING',direction,step:intent.step};
  // Полное чтение owner выполняется до первого короткого native witness.
  const admitted=gate(direction);
  const observed=inspectPair(direction,prefix,true)[intent.name];
  if(resuming?!observed.ready:!observed.joined)return {state:'CONVERGING',direction,step:intent.step};
  const joinCredit=resuming?undefined:join(intent.name,observed,direction);
  // Последний snapshot фиксирует реальные готовые Pod UID для receipt.
  const final=inspectPair(direction,prefix,true),current=final[intent.name];
  const finalVersion=BigInt(current.deployment.metadata.resourceVersion);
  ensure([state,observed].every(item=>finalVersion>=BigInt(item.deployment.metadata.resourceVersion)),
   'PAIRED_RECEIVER_STALE_ACK_READBACK');
  if(resuming?!current.ready:!current.joined)return {state:'CONVERGING',direction,step:intent.step};
  if(prefix===5)ensure(pairedTokenReleaseNames.every(name=>final[name].ready&&
   (name===intent.name||same(podUIDs(final[name]),resumeUIDs(direction,name)))),'PAIRED_RECEIVER_PAIR_NOT_READY');
  requireSource();requireAPIIsolation();
  verifyPairedTokenReleaseOwner(manifest,admitted);
  seals();
  verifyJoins(joinCredit?[joinCredit]:[]);
  append({type:'ACK',direction,step:intent.step,name:intent.name,uid:intent.uid,
   afterSpecSHA256:intent.afterSpecSHA256,podUIDs:podUIDs(current),...(joinCredit?{joinSHA256:joinCredit.sha256}:{})});
  if(prefix===5){
   append({type:'PAIR_COMPLETE',direction,recordSHA256:fingerprint(record)});}
  return {state:prefix===5?'PAIR_READY_TERMINAL_BLOCKED':'ACKNOWLEDGED',direction,step:intent.step,completed:prefix+1};
 }
 async function advance(direction='forward'){
  validateHistory();ensure(['forward','rollback'].includes(direction),'PAIRED_RECEIVER_DIRECTION_REJECTED');
  if(direction==='rollback')ensure(rows('forward','ACK').length===6&&rows('forward','PAIR_COMPLETE').length===1,
   'PAIRED_RECEIVER_FORWARD_RECEIPT_REQUIRED');
  else ensure(rows('rollback','INTENT').length===0,'PAIRED_RECEIVER_FORWARD_CLOSED');
  await bind();
  validateHistory();
  const prefix=rows(direction,'ACK').length;
  if(rows(direction,'INTENT').length>prefix)return status(direction);
  if(prefix===6)return status(direction);
  const step=stages[prefix],name=component(step),states=inspectPair(direction,prefix),p=part(name),state=states[name],spec=desired(p,direction,step);
  ensure(Object.values(states).every(s=>s.deployment.spec.replicas===0||s.ready),'PAIRED_RECEIVER_PAIR_NOT_READY');
  const request={name,uid:p.uid,resourceVersion:state.deployment.metadata.resourceVersion,beforeSpec:structuredClone(spec.before),afterSpec:structuredClone(spec.after)};
  const requestSHA256=fingerprint(request);
  await ports.dryRunComponent(request);ensure(fingerprint(request)===requestSHA256,'PAIRED_RECEIVER_REQUEST_CHANGED');
  validateHistory();ensure(rows(direction,'ACK').length===prefix&&rows(direction,'INTENT').length===prefix,
   'PAIRED_RECEIVER_JOURNAL_CHANGED');
  const admitted=gate(direction);
  const reread=inspectPair(direction,prefix);
  ensure(reread[name].deployment.metadata.resourceVersion===request.resourceVersion,'PAIRED_RECEIVER_VERSION_CHANGED');
  ensure(Object.values(reread).every(s=>s.deployment.spec.replicas===0||s.ready),'PAIRED_RECEIVER_PAIR_NOT_READY');
  if(direction==='rollback'&&!journal.events.some(e=>e.type==='ROLLBACK_BOUND'))append({type:'ROLLBACK_BOUND',compatibilitySHA256:requireRollbackCompatibility()});
  // Выдаём первый короткий witness только после server dry-run и полного
  // чтения owner. Каждая остановленная сторона снова подтверждает join.
  const joinCredits=[];
  for(const other of pairedTokenReleaseNames)if(reread[other].deployment.spec.replicas===0)joinCredits.push(join(other,reread[other],direction));
  const final=inspectPair(direction,prefix);
  ensure(final[name].deployment.metadata.resourceVersion===request.resourceVersion,'PAIRED_RECEIVER_VERSION_CHANGED');
  ensure(Object.values(final).every(s=>s.deployment.spec.replicas===0?s.joined:s.ready),'PAIRED_RECEIVER_PAIR_NOT_READY');
  if(step.startsWith('stop-'))requireStopUIDs(direction,name,final[name]);
  requireSource();requireAPIIsolation();verifyJoins(joinCredits);
  const monotonicUntil=Math.min(performance.now()+maxCASMilliseconds,
   pairedTokenReleaseOwnerDeadline(manifest,admitted),...joinCredits.map(c=>c.monotonicUntil));
  append({type:'INTENT',direction,step,name,uid:p.uid,beforeResourceVersion:request.resourceVersion,
   beforeSpecSHA256:fingerprint(spec.before),afterSpecSHA256:fingerprint(spec.after),priorPodUIDs:podUIDs(final[name])});
  const intent=journal.events.at(-1),intentSHA256=fingerprint(intent);
  const remaining=()=>{
   seals();verifyPairedTokenReleaseOwner(manifest,admitted);verifyJoins(joinCredits);
   ensure(writableEffectJournals.has(journal)&&journal.events.at(-1)===intent&&fingerprint(intent)===intentSHA256&&fingerprint(request)===requestSHA256,
    'PAIRED_RECEIVER_JOURNAL_CHANGED');
   const budget=Math.floor(Math.min(monotonicUntil-performance.now(),admitted.began+30000-Date.now(),
    ...joinCredits.map(c=>c.wallUntil-Date.now())));
   ensure(budget>0,'PAIRED_RECEIVER_CAS_DEADLINE_EXPIRED');return budget;
  };
  const permit=Object.freeze({});
  try{
   remaining();
   const authorize=()=>{
    remaining();
    requireSource();requireAPIIsolation();
    if(step.startsWith('stop-'))requireStopUIDs(direction,name,observe(name));
    if(direction==='rollback')requireRollbackCompatibility();
    return remaining();
   };
   casPermits.set(permit,{requestSHA256,journal,recordSHA256:recordSeal,remaining,authorize});
   const accepted=await ports.replaceFrozenComponentSpec(request,permit);
   remaining();
   ensure(accepted?.uid===p.uid&&accepted.specSHA256===fingerprint(spec.after)&&/^[1-9][0-9]{0,19}$/.test(accepted.resourceVersion)&&
    BigInt(accepted.resourceVersion)>BigInt(request.resourceVersion),'PAIRED_RECEIVER_ACK_REJECTED');
   append({type:'ACCEPTED',direction,step,name,uid:p.uid,resourceVersion:accepted.resourceVersion});
  }catch{append({type:'UNKNOWN',direction,step,name});throw Error('PAIRED_RECEIVER_CAS_UNKNOWN');}
  finally{casPermits.delete(permit);}
  return status(direction);
 }
 validateHistory();
 if(inspection===forwardReceiptInspection){
  ensure(binding()&&rows('forward','ACK').length===6&&rows('forward','PAIR_COMPLETE').length===1&&
   !journal.events.some(e=>e.direction==='rollback'||e.type==='ROLLBACK_BOUND'),
   'PAIRED_RECEIVER_FORWARD_RECEIPT_REQUIRED');
  const components=record.plan.components.map(p=>{
   const suffix=p.name==='control-plane'?'cp':'ig',ack=rows('forward','ACK').find(e=>e.step==='resume-'+suffix);
   ensure(ack.podUIDs.length===p.afterSpec.replicas&&same(ack.podUIDs,[...new Set(ack.podUIDs)].sort())&&
    ack.podUIDs.every(v=>typeof v==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v)),
    'PAIRED_RECEIVER_FORWARD_RECEIPT_REQUIRED');
   return {name:p.name,uid:p.uid,imageRef:p.imageRef,specSHA256:fingerprint(p.afterSpec),podUIDs:ack.podUIDs,
    stopJoinSHA256:rows('forward','ACK').find(e=>e.step==='stop-'+suffix).joinSHA256,
    stagedJoinSHA256:rows('forward','ACK').find(e=>e.step==='stage-'+suffix).joinSHA256};
  });
  return structuredClone({schema:'kodex.paired-token-forward-receipt/v1',recordSHA256:fingerprint(record),
   manifestSHA256:fingerprint(manifest),sourceSHA:manifest.sourceSHA,executorSHA:record.executorSHA,
   targetSHA256:fingerprint(manifest.target),effectJournalSHA256:fingerprint(journal.events),binding:binding(),components});
 }
 // Singleflight охватывает весь async вызов и все receiver на одном handle.
 // Параллельный caller не ставится в очередь после возможного UNKNOWN.
 const exclusive=operation=>(...args)=>withPairedTokenEffectFlight(journal,()=>operation(...args));
 return {advance:exclusive(direction=>{
  if(direction==='rollback')requireNoServiceRestoreIntent(journal,recordSeal);
  return advance(direction);
 }),status:exclusive(status),readiness:'BLOCKED_TERMINAL_REOPEN_NOT_IMPLEMENTED'};
}
