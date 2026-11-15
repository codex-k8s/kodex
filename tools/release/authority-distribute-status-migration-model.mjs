import {fingerprint} from './scoped-release.mjs';
import {createCanonicalRotationTemplate,createRotationJob,verifyRotationJobReadback} from './authority-rotation-transition.mjs';
import {telegramPolicyTransition} from './authority-telegram-transition.mjs';

export const authorityDistributeStatusMigration = Object.freeze({
 schema:'kodex.authority-distribute-status-migration/v1',before:20260912000200,after:20261115000100,
 file:'20261115000100_authority_distribute_delivery_status.sql',
 beforeDefinitionSHA256:'c615a3d183ca67cadff5cf027d12adb7207bf9a4cf456dea02affe6f63b86af1',
 afterDefinitionSHA256:'689aa6b17f033dc9509df6bb45417a563d7be3dd029152ff2a8f543fdd253f5b',
});
const fail=(ok,code)=>{if(!ok)throw Error(code);};
const hash=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const functionAttributes=value=>Object.fromEntries(Object.entries(value).filter(([key])=>key!=='definitionSHA256'));

// Это допуск только исправления наблюдаемого статуса уже доставленного107.
// Наличие намерения либо registry не заменяет exact durable snapshot history.
export function validateAuthorityDistributeState(state,rotation,now=Date.now()){
 const op=state?.operation,p=state?.phase,i=state?.intent,h=state?.snapshot,fn=state?.function;
 const observed=Date.parse(state?.observedAt),delivered=Date.parse(i?.deliveredAt),published=Date.parse(h?.publishedAt);
 fail(rotation?.policyTransition?.profile===telegramPolicyTransition&&uuid(rotation.ownerOperationID)&&
  Number.isFinite(now)&&Number.isFinite(observed)&&observed<=now+5000&&now-observed<=30000,
 'AUTHORITY_STATUS_MIGRATION_SCOPE_REJECTED');
 fail(op?.id===rotation.ownerOperationID&&op.status==='DISTRIBUTING'&&
  op.registryRevision===rotation.registry.sourceRevision&&op.registryDigest===rotation.registry.sourceDigestSHA256&&
  Number.isSafeInteger(op.baseRevision)&&op.baseRevision>0&&op.baseRevision<9007199254740988&&hash(op.baseDigest)&&op.expectedReaders===15&&
  p?.phase==='DISTRIBUTE'&&uuid(p.intentID)&&p.sourceRevision===op.baseRevision+1&&p.registryDigest===op.registryDigest&&hash(p.inputDigest)&&
  i?.id===p.intentID&&i.status==='DELIVERED'&&i.protocol===2&&i.sourceRevision===p.sourceRevision&&hash(i.sourceDigest)&&
  i.predecessorRevision===op.baseRevision&&i.predecessorDigest===op.baseDigest&&i.expectedReaders===15&&
  i.promotedAt===null&&i.retiredAt===null&&i.abortedAt===null&&
  h?.sourceRevision===p.sourceRevision&&h.sourceDigest===i.sourceDigest&&h.policyRevision===107&&h.intentID===p.intentID&&
  h.inputDigest===p.inputDigest&&h.predecessorRevision===op.baseRevision&&h.predecessorDigest===op.baseDigest&&h.expectedReaders===15&&
  Number.isFinite(delivered)&&Number.isFinite(published)&&published<=delivered&&delivered<=observed&&
  state.completedPhasePublicationCount===0&&hash(state.ledgerSHA256)&&hash(state.goosePredecessorSHA256)&&
  state.owner?.operationId===op.id&&state.owner.status==='DISTRIBUTING'&&state.owner.operationStatus==='DISTRIBUTING'&&
  state.owner.phase==='DISTRIBUTE'&&state.owner.intentId===p.intentID&&state.owner.registryRevision===op.registryRevision&&
  state.owner.registrySourceDigestSHA256===op.registryDigest,
 'AUTHORITY_STATUS_MIGRATION_DELIVERY_REJECTED');
 fail(fn&&/^[1-9][0-9]*$/.test(fn.oid??'')&&fn.owner==='internal_rpc_authority_readback_owner'&&
  fn.acl==='{internal_rpc_authority_readback_owner=X/internal_rpc_authority_readback_owner,internal_rpc_authority_migrator=X/internal_rpc_authority_readback_owner}'&&
  fn.securityDefiner===true&&fn.volatility==='v'&&same(fn.config,['search_path=pg_catalog, internal_rpc_authority, pg_temp'])&&
  fn.result==='jsonb'&&fn.args===''&&fn.argNames===null&&fn.language==='sql'&&fn.parallel==='u'&&fn.strict===false&&
  fn.leakproof===false&&fn.returnsSet===false&&fn.cost===100&&fn.rows===0&&hash(fn.definitionSHA256),
 'AUTHORITY_STATUS_MIGRATION_FUNCTION_REJECTED');
 return state;
}

export function validateAuthorityDistributePrecondition(state,rotation,now=Date.now()){
 validateAuthorityDistributeState(state,rotation,now);
 fail(state.gooseVersion===authorityDistributeStatusMigration.before&&state.gooseRows===10&&
  state.function.definitionSHA256===authorityDistributeStatusMigration.beforeDefinitionSHA256,
 'AUTHORITY_STATUS_MIGRATION_PREDECESSOR_REJECTED');
 return state;
}

// Только новые Goose row и function definition могут отличаться. Состояние
// operation/intent/history и прежняя Goose history не изменяются этим repair.
export function validateAuthorityDistributePostcondition(before,after,rotation,now=Date.now()){
 validateAuthorityDistributeState(after,rotation,now);
 fail(after.gooseVersion===authorityDistributeStatusMigration.after&&after.gooseRows===11&&
  after.function.definitionSHA256===authorityDistributeStatusMigration.afterDefinitionSHA256&&same(functionAttributes(before.function),functionAttributes(after.function))&&
  after.goosePredecessorSHA256===before.goosePredecessorSHA256&&after.ledgerSHA256===before.ledgerSHA256&&
  same(after.operation,before.operation)&&same(after.phase,before.phase)&&same(after.intent,before.intent)&&same(after.snapshot,before.snapshot)&&
  after.owner?.operationId===rotation.ownerOperationID&&after.owner.status==='DISTRIBUTING'&&
  after.owner.operationStatus==='DISTRIBUTING'&&after.owner.phase==='DISTRIBUTE'&&
  after.owner.sourceRevision===after.snapshot.sourceRevision&&after.owner.sourceDigestSHA256===after.snapshot.sourceDigest&&
  after.owner.expectedReadbackCount===15&&Number.isSafeInteger(after.owner.actualReadbackCount)&&
  after.owner.actualReadbackCount>=0&&after.owner.actualReadbackCount<=384,
 'AUTHORITY_STATUS_MIGRATION_POSTCONDITION_REJECTED');
 return after;
}

// Job использует canonical migrator identity, TLS mounts и immutable CLI.
// Ни publisher restart, ни новая rotation не входят в этот lifecycle.
export function createAuthorityDistributeStatusJob(plan){
 fail(plan.schema===authorityDistributeStatusMigration.schema&&uuid(plan.intentID)&&
  /^[a-f0-9]{40}$/.test(plan.source?.revision??'')&&typeof plan.source?.path==='string',
 'AUTHORITY_STATUS_MIGRATION_PLAN_REJECTED');
 const template=createCanonicalRotationTemplate(plan.source.path,plan.image,true);
 const job=createRotationJob(template,{version:3,intentID:plan.intentID,revision:plan.source.revision,source:plan.source.path,
  action:'status',policyTransition:{profile:telegramPolicyTransition}});
 // Один Kubernetes create key на версию исключает параллельные Job из разных
 // operator plans. Потеря ACK никогда не разрешает повторный create.
 job.metadata.name=`authority-status-${authorityDistributeStatusMigration.after}`;
 job.metadata.annotations['kodex.dev/source-delivery-intent']=plan.intentID;
 job.metadata.annotations['kodex.dev/authority-status-migration-plan']=fingerprint(plan);
 job.spec.template.spec.containers[0].args=['up'];
 return job;
}

export function verifyAuthorityDistributeStatusJob(actual,expected,uid){
 verifyRotationJobReadback(actual,expected);
 fail(actual.apiVersion==='batch/v1'&&actual.kind==='Job'&&uuid(actual.metadata?.uid)&&(!uid||actual.metadata.uid===uid)&&!actual.metadata.deletionTimestamp&&
  actual.metadata.annotations?.['kodex.dev/source-delivery-intent']===expected.metadata.annotations['kodex.dev/source-delivery-intent']&&
  actual.metadata.annotations?.['kodex.dev/authority-status-migration-plan']===expected.metadata.annotations['kodex.dev/authority-status-migration-plan'],
 'AUTHORITY_STATUS_MIGRATION_JOB_REPLACED');
 return actual;
}
