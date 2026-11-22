import {fingerprint} from './scoped-release.mjs';
import {isProvenStoppedPod} from './stable-archive-transition.mjs';
import {workflowFileAbortIntent,workflowFileAbortAck} from './workflow-file-provenance-migration-contract.mjs';

const ensure=(ok,code)=>{if(!ok)throw Error(code);};

// Без INTENT допустимо только отсутствие Job и любого Pod с её именем.
// После INTENT требуется положительный join точного неизменного Job и Pod.
export function readWorkflowFileAbortJob(reader,expected,intents,readbackTypes,allowSuccess) {
  const name=expected.metadata.name;
  const job=reader.optionalJob(name),jobUID=job?.metadata?.uid;
  if(!intents.length){
    ensure(!job&&reader.readPods(name,undefined).length===0,'WORKFLOW_FILE_PROVENANCE_JOB_WITHOUT_INTENT');
    return {name,outcome:'NOT_CREATED'};
  }
  ensure(jobUID&&job.apiVersion==='batch/v1'&&job.kind==='Job'&&job.metadata.name===name&&
    job.metadata.namespace==='kodex-system'&&!job.metadata.deletionTimestamp&&
    job.metadata.annotations?.[reader.annotation]===reader.planSHA256&&
    fingerprint(reader.normalize(job.spec.template.spec))===fingerprint(reader.normalize(expected.spec.template.spec))&&
    intents.length===1&&intents[0].jobName===name&&
    reader.events.filter(event=>readbackTypes.includes(event.type)&&event.jobUID).every(event=>event.jobUID===jobUID),
  'WORKFLOW_FILE_PROVENANCE_ABORT_JOB_SCOPE_CHANGED');
  const terminal=job.status?.conditions?.filter(c=>['Complete','Failed'].includes(c.type)&&c.status==='True')??[];
  const succeeded=terminal[0]?.type==='Complete',failed=terminal[0]?.type==='Failed';
  ensure(terminal.length===1&&!job.status.active&&!job.status.terminating&&
    (succeeded&&allowSuccess&&job.status.succeeded===1&&!job.status.failed||failed&&job.status.failed===1&&!job.status.succeeded),
  'WORKFLOW_FILE_PROVENANCE_ABORT_JOB_NOT_TERMINAL');
  const pods=reader.readPods(name,jobUID);
  const current=reader.optionalJob(name);
  ensure(pods.length===1&&pods[0].metadata?.uid&&!pods[0].metadata.deletionTimestamp&&
    !['NodeLost','Unknown'].includes(pods[0].status?.reason)&&
    isProvenStoppedPod(pods[0])&&reader.verifyExecution(job,pods,expected,succeeded?'succeeded':'failed')&&
    current&&fingerprint(current)===fingerprint(job),'WORKFLOW_FILE_PROVENANCE_ABORT_JOB_NOT_JOINED');
  return {name,jobUID,podUID:pods[0].metadata.uid,outcome:succeeded?'SUCCEEDED':'FAILED'};
}


export const workflowFileAbortReady=state=>state.ready&&['BEFORE','RESUMED'].includes(state.stage);

// Исполняется под locks migration и backup. INTENT фиксируется до запуска
// старого Control Plane и закрывает последующий CREATE в этом plan.
export async function applyWorkflowFileAbort({journal,observe,readProof,resume,wait}) {
  const state=observe(),events=journal.events;
  const intent=events.find(e=>e.type===workflowFileAbortIntent),ack=events.find(e=>e.type===workflowFileAbortAck);
  const stopped=events.some(e=>e.type==='STOP_INTENT');
  ensure(!events.some(e=>['PAIR_HANDOFF_INTENT','PAIR_HANDOFF_ACK','RESUME_ACK'].includes(e.type)),
    'WORKFLOW_FILE_PROVENANCE_ABORT_FORWARD_STAGE');
  ensure((state.stage==='BEFORE'&&state.ready&&!stopped||state.stage==='STOPPED'&&state.joined&&stopped||
    state.stage==='RESUMED'&&intent)&&(!ack||workflowFileAbortReady(state)),
  'WORKFLOW_FILE_PROVENANCE_ABORT_CP_SCOPE_CHANGED');
  const identity=readProof(),hash=fingerprint(identity);
  ensure(!intent||fingerprint(intent.identity)===hash,'WORKFLOW_FILE_PROVENANCE_ABORT_READBACK_CHANGED');
  if(!intent)journal.append({type:workflowFileAbortIntent,identity});
  if(state.stage==='STOPPED'){
    const current=observe();
    ensure(current.stage==='STOPPED'&&current.joined,'MIGRATION_STOP_JOIN_REQUIRED');
    resume(current);
  }
  const restored=await wait(),fresh=readProof();
  ensure(workflowFileAbortReady(restored)&&fingerprint(fresh)===hash&&(!ack||ack.abortSHA256===hash),
    'WORKFLOW_FILE_PROVENANCE_ABORT_READBACK_CHANGED');
  if(!ack)journal.append({type:workflowFileAbortAck,abortSHA256:hash});
  return {state:restored,identity:fresh};
}
