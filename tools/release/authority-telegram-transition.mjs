import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {fingerprint} from './scoped-release.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';

export const telegramPolicyTransition='TELEGRAM_NOTIFICATIONS_106_107';
export const telegramPolicyReaders=17;
const fail=(ok,code)=>{if(!ok)throw Error(code);};
const digest=raw=>createHash('sha256').update(raw).digest('hex');
const policies=Object.freeze({
 predecessor:{revision:106,sha256:'bd5ca7ecd2bdaa9a0d7cc9d097e11437c52be6fb3de53181551cb35b3ae315be',path:'./fixtures/authority-policy-revision106.json'},
 desired:{revision:107,sha256:'3e98c4231b622cef74add3ebef29a8df5299d47280ec8d38b6fa8eaddfafcd1c',path:'./fixtures/authority-policy-revision107.json'},
});
const registries=Object.freeze({
 predecessor:{count:16,sha256:'0ec8ec41a7852f2a5b70eb07ac17294e38b7aac0cfd3b0ccd23cceb22f76d954',path:'./fixtures/authority106-key-delivery-targets.yaml'},
 desired:{count:17,sha256:'7a7800dee1486acd04e3a857f34c5e72c1f6ab960731084d311d4ccb028fe94d',path:'./fixtures/authority107-key-delivery-targets.yaml'},
});
const operations=Object.freeze([
 'platform.command.telegram-subscription.configure','platform.interactions.deliveries.begin',
 'platform.query.interaction-deliveries.list','platform.query.telegram-subscription.get',
]);

// Immutable106/107 сохраняются независимо от current repository policy. Runtime
// OIDC render применяется лишь к точному source и заново связывается с owner CP.
export function telegramPolicySource(role){
 const policy=policies[role];fail(policy,'TELEGRAM_POLICY_ROLE_REJECTED');
 const raw=readFileSync(new URL(policy.path,import.meta.url),'utf8');
 fail(digest(raw)===policy.sha256,'TELEGRAM_POLICY_SOURCE_REJECTED');
 return {raw,revision:policy.revision,sourceSHA256:policy.sha256};
}
export function validateTelegramPolicy(raw,role,transition){
 fail(transition?.profile===telegramPolicyTransition&&typeof raw==='string','TELEGRAM_POLICY_TRANSITION_REQUIRED');
 const source=telegramPolicySource(role),rendered=renderAuthorityPolicyEnvironment(source.raw,transition.environment);
 fail(raw===rendered.raw,'TELEGRAM_POLICY_ENVIRONMENT_BYTES_REJECTED');
 return {revision:source.revision,sha256:rendered.renderedSHA256,sourceSHA256:source.sourceSHA256};
}

// Ровно четыре operation profiles добавляют Configure/read/history/Begin. Ни
// прежний endpoint, bearer/mTLS layer, producer либо authority source не меняется.
export function verifyTelegramPolicyDelta(){
 const before=JSON.parse(telegramPolicySource('predecessor').raw),after=JSON.parse(telegramPolicySource('desired').raw);
 const bindings=after.policy.operation_bindings.filter(b=>operations.includes(b.operation_id));
 fail(bindings.length===operations.length&&fingerprint(bindings.map(b=>b.operation_id).sort())===fingerprint(operations),
  'TELEGRAM_POLICY_OPERATION_CLOSURE_REJECTED');
 for(const binding of bindings){
  const worker=binding.operation_id==='platform.interactions.deliveries.begin',configure=binding.operation_id==='platform.command.telegram-subscription.configure';
  fail(binding.permission===binding.operation_id&&binding.caller_workload_id===(worker?'interaction-gateway':'control-api-gateway')&&
   binding.target_workload_id==='control-plane'&&binding.project_required===!worker&&
   fingerprint(binding.authority_sources)===fingerprint(worker?['DOMAIN_STATE']:['OIDC_SESSION','DOMAIN_STATE'])&&
   fingerprint(binding.request_profile)===fingerprint({mode:'UNARY_PROTO_SHA256',idempotency:configure?'REQUIRED':'FORBIDDEN',resource:'FORBIDDEN',version:'FORBIDDEN',attempt:'FORBIDDEN'}),
   'TELEGRAM_POLICY_OPERATION_CLOSURE_REJECTED');
 }
 after.policy_revision=106;
 after.policy.operation_bindings=after.policy.operation_bindings.filter(b=>!operations.includes(b.operation_id));
 for(const producer of after.policy.authority_proof_producers)producer.allowed_operation_ids=producer.allowed_operation_ids.filter(id=>!operations.includes(id));
 fail(fingerprint(before)===fingerprint(after),'TELEGRAM_POLICY_PREDECESSOR_CHANGED');
 return {predecessor:106,desired:107,addedOperations:[...operations],expectedReaders:telegramPolicyReaders};
}

const registryParser='import json,sys,yaml\nclass Unique(yaml.SafeLoader): pass\ndef mapping(loader,node,deep=False):\n out={}\n for key,value in node.value:\n  key=loader.construct_object(key,deep=deep)\n  if key in out: raise ValueError("duplicate YAML key")\n  out[key]=loader.construct_object(value,deep=deep)\n return out\nUnique.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG,mapping)\nprint(json.dumps(yaml.load(sys.stdin,Loader=Unique)))\n';
const registryWriter='import json,sys,yaml; print(yaml.safe_dump(json.load(sys.stdin),sort_keys=False),end="")';
function python(program,input){
 const env=Object.fromEntries(['PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
 try{return execFileSync('python3',['-I','-c',program],{input,env,encoding:'utf8',timeout:10000,maxBuffer:1<<20,stdio:['pipe','pipe','pipe']});}
 catch{throw Error('TELEGRAM_POLICY_REGISTRY_PARSE_REJECTED');}
}
export function telegramRegistrySource(role){
 const config=registries[role];fail(config,'TELEGRAM_POLICY_ROLE_REJECTED');
 const raw=readFileSync(new URL(config.path,import.meta.url),'utf8');fail(digest(raw)===config.sha256,'TELEGRAM_POLICY_REGISTRY_SOURCE_REJECTED');
 return {raw,sourceSHA256:config.sha256,expectedReaders:config.count};
}
function registry(raw){
 fail(typeof raw==='string'&&Buffer.byteLength(raw)>0&&Buffer.byteLength(raw)<=1<<20,'TELEGRAM_POLICY_REGISTRY_PARSE_REJECTED');
 const value=JSON.parse(python(registryParser,raw));
 fail(value?.version===1&&Number.isSafeInteger(value.source_revision)&&value.source_revision>0&&value.source_revision<9007199254740991&&Array.isArray(value.targets),
  'TELEGRAM_POLICY_REGISTRY_PARSE_REJECTED');return value;
}

// Source revision берётся из actual owner registry и только увеличивается. Число
// readers не является input:16/17 выведены из двух точных immutable memberships.
export function validateTelegramRegistry(raw,role){
 const expected=registry(telegramRegistrySource(role).raw),actual=registry(raw),normalized=structuredClone(actual);
 normalized.source_revision=expected.source_revision;
 fail(fingerprint(normalized)===fingerprint(expected),'TELEGRAM_POLICY_REGISTRY_CLOSURE_REJECTED');
 return {value:actual,sourceRevision:actual.source_revision,sourceDigestSHA256:digest(raw),expectedReaders:registries[role].count};
}
export function advanceTelegramRegistry(liveRaw,sourceRaw){
 const expected=telegramRegistrySource('desired');fail(sourceRaw===expected.raw,'TELEGRAM_POLICY_REGISTRY_SOURCE_REJECTED');
 const before=validateTelegramRegistry(liveRaw,'predecessor'),after=registry(sourceRaw);
 const retained=structuredClone(after);retained.targets=retained.targets.filter(t=>t.workload_id!=='interaction-gateway');retained.source_revision=before.sourceRevision;
 fail(fingerprint(retained)===fingerprint(before.value)&&after.targets.filter(t=>t.workload_id==='interaction-gateway'&&t.role==='AUTHORIZATION_ISSUER').length===1,
  'TELEGRAM_POLICY_REGISTRY_PREDECESSOR_CHANGED');
 after.source_revision=before.sourceRevision+1;
 const raw=python(registryWriter,JSON.stringify(after));validateTelegramRegistry(raw,'desired');return raw;
}

// DELIVER допускает только запуск новых readers под уже опубликованным материалом.
// Это отдельный промежуточный owner proof; он никогда не заменяет17 readbacks/RETIRE.
export function validateTelegramAuthorityDeliveryState(state,identity,operationID,now=Date.now()){
 const observed=Date.parse(state?.observedAt),started=Date.parse(state?.deliveryStartedAt),delivered=Date.parse(state?.deliveredAt);
 fail(state?.operationId===operationID&&state.status==='DISTRIBUTING'&&state.operationStatus==='DISTRIBUTING'&&state.phase==='DISTRIBUTE'&&
  state.registryRevision===identity.sourceRevision&&state.registrySourceDigestSHA256===identity.sourceDigestSHA256&&
  Number.isSafeInteger(state.baseRevision)&&state.baseRevision>0&&state.baseRevision<=9007199254740988&&state.sourceRevision===state.baseRevision+1&&
  /^[a-f0-9]{64}$/.test(state.sourceDigestSHA256??'')&&state.protocolVersion===2&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(state.intentId??'')&&
  state.expectedReadbackCount===telegramPolicyReaders&&Number.isSafeInteger(state.actualReadbackCount)&&state.actualReadbackCount>=0&&state.actualReadbackCount<=telegramPolicyReaders*24&&
  Number.isFinite(now)&&observed<=now+5000&&now-observed<=30000&&started<=delivered&&delivered<=observed&&
  state.abortedAt===undefined&&state.promotedAt===undefined&&state.retiredAt===undefined&&state.completedAt===undefined,
  'TELEGRAM_AUTHORITY_DELIVERY_OWNER_REJECTED');
 return {state:'PENDING_AUTHORITY',policyRevision:107,operationID,registryRevision:identity.sourceRevision,registrySHA256:identity.sourceDigestSHA256,
  snapshotRevision:state.sourceRevision,snapshotSHA256:state.sourceDigestSHA256,expectedReadbackCount:telegramPolicyReaders,
  actualReadbackCount:state.actualReadbackCount,observedAt:state.observedAt,deliveredAt:state.deliveredAt};
}
