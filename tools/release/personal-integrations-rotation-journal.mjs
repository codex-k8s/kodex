import {validateCatalogAuthorityEvidence,verifyRotationJobReadback} from './authority-rotation-transition.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';

export const personalIntegrationsRotationProtocol='PERSONAL_INTEGRATIONS_ROTATION_V1';
const fail=(ok,code)=>{if(!ok)throw Error(code);};

// Отдельный personal protocol требует durable CREATE intent до сети. Old
// journals без него не принимаются задним числом как доказательство no-effect.
export function validatePersonalIntegrationsRotationJournal(plan,events){
 fail(plan.personalRotationProtocol===personalIntegrationsRotationProtocol,'PERSONAL_INTEGRATIONS_ROTATION_PROTOCOL_REQUIRED');
 const pins=validateCatalogAuthorityEvidence(plan,events,personalIntegrationsAuthorityTransition);
 let attempted=false;
 for(const row of events){
  if(row.operation==='CREATE'&&row.status==='INTENT'){
   fail(!attempted,'PERSONAL_INTEGRATIONS_ROTATION_CREATE_REPLAY');attempted=true;
  }else if(row.operation==='CREATE'||row.jobUID)fail(attempted,'PERSONAL_INTEGRATIONS_ROTATION_CREATE_INTENT_REQUIRED');
 }
 return {...pins,attempted};
}

// Absence после UNKNOWN/INTENT не доказывает, что прежняя Job не исполнялась.
// Разрешён только exact positive readback; известный UID никогда не заменяется.
export function personalIntegrationsRotationJobCustody(plan,events,current,expected){
 const pins=validatePersonalIntegrationsRotationJournal(plan,events);
 if(!current){
  fail(!pins.attempted&&!pins.jobUID,'PERSONAL_INTEGRATIONS_ROTATION_CREATE_UNRESOLVED');
  return {mayCreate:true};
 }
 fail(pins.attempted&&(!pins.jobUID||current.metadata?.uid===pins.jobUID),'PERSONAL_INTEGRATIONS_ROTATION_JOB_REPLACED');
 verifyRotationJobReadback(current,expected);
 return {mayCreate:false,jobUID:current.metadata.uid};
}
