import {authorityPolicyReadbackContract} from './authority-catalog-transition.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {validateTelegramOwnerHistory} from './telegram-authority-owner.mjs';
import {validateRetiredCatalogOwnerState} from './authority-rotation-transition.mjs';

const errorOwnerDeadlineRejected='PERSONAL_INTEGRATIONS_OWNER_DEADLINE_REJECTED';
const errorOwnerProgressRejected='PERSONAL_INTEGRATIONS_OWNER_PROGRESS_REJECTED';
const fail=(ok,code)=>{if(!ok)throw Error(code);};
const sha=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const statuses=['DISTRIBUTING','WAITING_SWITCH','SWITCHING','WAITING_RETIRE','RETIRING','RETIRED'];
const publishedPhases={DISTRIBUTING:['DISTRIBUTE'],WAITING_SWITCH:['DISTRIBUTE'],SWITCHING:['DISTRIBUTE','SWITCH'],
 WAITING_RETIRE:['SWITCH'],RETIRING:['SWITCH','RETIRE'],RETIRED:['RETIRE']};
const offset={DISTRIBUTE:1,SWITCH:2,RETIRE:3};
const instant=value=>Date.parse(value);

function wire(value,now){
 try{validateTelegramOwnerHistory(value,value,now);}catch{throw Error('PERSONAL_INTEGRATIONS_OWNER_WIRE_REJECTED');}
}

// Startup permission переживает легитимные SWITCH/RETIRE той же operation.
// Только published/delivered current epoch допускает reader; transient intent
// без snapshot закрыт. Это не all-workload readiness и не внешний API terminal.
export function validatePersonalIntegrationsDeliveryState(state,identity,operationID,now=Date.now()){
 wire(state,now);const readers=authorityPolicyReadbackContract(personalIntegrationsAuthorityTransition);
 const observed=instant(state.observedAt),rank=statuses.indexOf(state.status),phaseOffset=offset[state.phase];
 fail(rank>=0&&state.operationStatus===state.status&&state.operationId===operationID&&uuid(operationID)&&
  state.registryRevision===identity.sourceRevision&&state.registrySourceDigestSHA256===identity.sourceDigestSHA256&&
  Number.isSafeInteger(state.baseRevision)&&state.baseRevision>0&&state.baseRevision<=9007199254740988&&sha(state.baseDigestSHA256)&&
  publishedPhases[state.status].includes(state.phase)&&state.sourceRevision===state.baseRevision+phaseOffset&&sha(state.sourceDigestSHA256)&&
  state.protocolVersion===2&&uuid(state.intentId)&&state.expectedReadbackCount===readers.expectedReadbackCount&&
  Number.isSafeInteger(state.actualReadbackCount)&&state.actualReadbackCount>=0&&state.actualReadbackCount<=384&&
  Number.isFinite(now)&&observed<=now+5000&&now-observed<=30000&&
  instant(state.deliveryStartedAt)<=instant(state.deliveredAt)&&instant(state.deliveredAt)<=observed&&
  state.abortedAt===undefined&&(state.status==='RETIRED'||state.completedAt===undefined),
  'PERSONAL_INTEGRATIONS_OWNER_DELIVERY_REJECTED');
 if(rank>=1)fail(Number.isFinite(instant(state.switchNotBefore)),errorOwnerDeadlineRejected);
 if(rank>=2)fail(Number.isFinite(instant(state.previousNotAfter))&&instant(state.switchNotBefore)<=instant(state.previousNotAfter),
  errorOwnerDeadlineRejected);
 if(['WAITING_SWITCH','WAITING_RETIRE','RETIRED'].includes(state.status))fail(state.actualReadbackCount>=readers.expectedReadbackCount,
  'PERSONAL_INTEGRATIONS_OWNER_MEMBERSHIP_REJECTED');
 if(state.status==='RETIRED')validateRetiredCatalogOwnerState(state,{ownerOperationID:operationID},identity,readers,now);
 return {state:'PENDING_AUTHORITY',policyRevision:110,operationID,ownerStatus:state.status,ownerPhase:state.phase,
  registryRevision:identity.sourceRevision,registrySHA256:identity.sourceDigestSHA256,baseRevision:state.baseRevision,baseDigestSHA256:state.baseDigestSHA256,
  snapshotRevision:state.sourceRevision,snapshotSHA256:state.sourceDigestSHA256,...readers,
  actualReadbackCount:state.actualReadbackCount,observedAt:state.observedAt,deliveredAt:state.deliveredAt};
}

// Status Job остаётся historical observation. Поздний fixed SQL вправе доказать
// только монотонный прогресс той же operation/registry/base. ACK count может
// обнулиться лишь с новым snapshot, а immutable координаты epoch не меняются.
export function validatePersonalIntegrationsOwnerProgress(recorded,current,identity,operationID,now=Date.now()){
 wire(recorded,now);wire(current,now);
 validatePersonalIntegrationsDeliveryState(current,identity,operationID,now);
 const stable=['operationId','registryRevision','registrySourceDigestSHA256','baseRevision','baseDigestSHA256','expectedReadbackCount'];
 fail(recorded.operationId===operationID&&stable.every(key=>recorded[key]===current[key])&&
  statuses.indexOf(recorded.status)>=0&&statuses.indexOf(current.status)>=statuses.indexOf(recorded.status)&&
  instant(recorded.observedAt)<=instant(current.observedAt), errorOwnerProgressRejected);
 fail(recorded.operationStatus===recorded.status&&
  (recorded.protocolVersion===undefined||recorded.protocolVersion===2)&&
  (recorded.phase===undefined||publishedPhases[recorded.status].includes(recorded.phase)),
  errorOwnerProgressRejected);
 if(recorded.sourceRevision!==undefined){
  validatePersonalIntegrationsDeliveryState(recorded,identity,operationID,instant(recorded.observedAt));
  fail(Number.isSafeInteger(recorded.sourceRevision)&&current.sourceRevision>=recorded.sourceRevision&&
   (current.sourceRevision>recorded.sourceRevision||current.actualReadbackCount>=recorded.actualReadbackCount&&
    ['sourceDigestSHA256','intentId','protocolVersion','phase','predecessorRevision','predecessorDigestSHA256'].every(key=>recorded[key]===current[key])),
   errorOwnerProgressRejected);
 }
 const timestamps=['switchNotBefore','previousNotAfter',...(recorded.sourceRevision===current.sourceRevision?
  ['deliveryStartedAt','deliveredAt','promotedAt','retiredAt','completedAt']:[])].filter(key=>recorded[key]!==undefined);
 const pick=value=>({observedAt:value.observedAt,actualReadbackCount:0,...Object.fromEntries(timestamps.map(key=>[key,value[key]]))});
 try{validateTelegramOwnerHistory(pick(recorded),pick(current),now);}catch{throw Error(errorOwnerProgressRejected);}
 return current;
}
