import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {inspectSource} from './application-source.mjs';
import {authorityOwnerDatabase,validateAuthorityDatabaseIdentity} from './migration-owner-database.mjs';
import {authorityPostgresIdentity,catalogAuthorityBaselineSQL,catalogAuthorityBaselineSQLSHA256} from './catalog-authority-baseline.mjs';
import {telegramPolicyTransition} from './authority-telegram-transition.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const digest=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const schema='kodex.telegram-authority-owner-scope/v1';
const root=resolve(fileURLToPath(new URL('../../',import.meta.url)));
const ownerFields=new Set(['observedAt','operationId','operationStatus','phase','intentId','protocolVersion','sourceRevision','sourceDigestSHA256','registrySourceDigestSHA256','registryRevision','baseRevision','baseDigestSHA256','switchNotBefore','previousNotAfter','completedAt','predecessorRevision','predecessorDigestSHA256','deliveryStartedAt','deliveredAt','promotedAt','overlapUntil','retiredAt','abortedAt','status','expectedReadbackCount','actualReadbackCount']);
const timestampFields=new Set(['switchNotBefore','previousNotAfter','completedAt','deliveryStartedAt','deliveredAt','promotedAt','overlapUntil','retiredAt','abortedAt']);

// SQL/Go JSON могут представлять один instant как Z и +00:00. Fraction сохраняет
// всю PostgreSQL precision; сравнение immutable owner coordinates не округляет ms.
function timestampIdentity(value){
 const match=typeof value==='string'&&/^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
 fail(match,'TELEGRAM_OWNER_HISTORY_REJECTED');
 const seconds=Date.parse(`${match[1]}T${match[2]}${match[4]}`)/1000;
 fail(Number.isSafeInteger(seconds),'TELEGRAM_OWNER_HISTORY_REJECTED');
 return `${seconds}.${(match[3]??'').padEnd(9,'0')}`;
}

export function validateTelegramOwnerScope(scope,plan){
 fail(scope&&Object.keys(scope).sort().join(',')==='context,executorRevision,executorSource,kubeconfig,postgres,postgresBackend,schema,sourceRevision,sqlSHA256,targetSHA256'&&
  scope.schema===schema&&scope.context===plan.context&&scope.kubeconfig===plan.kubeconfig&&scope.sourceRevision===plan.revision&&
  plan.policyTransition?.profile===telegramPolicyTransition&&scope.targetSHA256===plan.policyTransition.targetSHA256&&digest(scope.targetSHA256)&&
  scope.sqlSHA256===catalogAuthorityBaselineSQLSHA256&&typeof scope.executorSource==='string'&&scope.executorSource===resolve(scope.executorSource)&&
  /^[a-f0-9]{40}$/.test(scope.executorRevision??'')&&scope.postgres?.pod==='kodex-postgresql-0'&&uuid(scope.postgres.uid)&&uuid(scope.postgres.ownerUID),
  'TELEGRAM_OWNER_SCOPE_REJECTED');
 validateAuthorityDatabaseIdentity(scope.postgresBackend);
 return scope;
}

// Новый status plan закрепляет только immutable backend identity. Сам capture
// не объявляет DELIVER/RETIRE и не создаёт вместо owner grant обычный JSON.
export function captureTelegramOwnerScope(plan,kube,io={}){
 const postgresIdentity=()=>authorityPostgresIdentity(kube),postgres=postgresIdentity();
 const database=authorityOwnerDatabase(kube,postgresIdentity),postgresBackend=database.identity(postgres);
 const scope={schema,context:plan.context,kubeconfig:plan.kubeconfig,sourceRevision:plan.revision,targetSHA256:plan.policyTransition.targetSHA256,
  postgres,postgresBackend,sqlSHA256:catalogAuthorityBaselineSQLSHA256,executorSource:root,
  executorRevision:(io.inspectSource??inspectSource)(root).revision};
 return Object.freeze(validateTelegramOwnerScope(scope,plan));
}

// Только actual fixed READ после тяжёлых scope/history checks обновляет время.
// ObservedAt старого status Job остаётся неизменным историческим наблюдением.
export function readTelegramOwner(scope,plan,recorded,kube,io={}){
 validateTelegramOwnerScope(scope,plan);
 fail(scope.executorSource===root&&(io.inspectSource??inspectSource)(scope.executorSource).revision===scope.executorRevision,'TELEGRAM_OWNER_EXECUTOR_CHANGED');
 const postgresIdentity=()=>authorityPostgresIdentity(kube),database=authorityOwnerDatabase(kube,postgresIdentity);
 fail(same(postgresIdentity(),scope.postgres),'TELEGRAM_OWNER_POSTGRES_CHANGED');
 const state=database.read(catalogAuthorityBaselineSQL(),scope.postgres,scope.postgresBackend);
 const now=(io.now??Date.now)(),currentTime=Date.parse(state?.observedAt);
 fail(Number.isFinite(now)&&Number.isFinite(currentTime)&&currentTime<=now+5000&&now-currentTime<=30000,'TELEGRAM_OWNER_OBSERVATION_STALE');
 validateTelegramOwnerHistory(recorded,state,now);
 return {state,scope};
}

// Общая проверка wire representation используется и late reader, и native SQL→Go
// qualification. Она не выдаёт grant и отдельно от freshness не допускает effect.
export function validateTelegramOwnerHistory(recorded,state,now=Date.now()){
 fail(recorded&&state&&[recorded,state].every(value=>typeof value==='object'&&!Array.isArray(value)&&Object.keys(value).every(key=>ownerFields.has(key))),'TELEGRAM_OWNER_HISTORY_REJECTED');
 const oldTime=Date.parse(recorded.observedAt),currentTime=Date.parse(state.observedAt);
 fail(Number.isFinite(oldTime)&&oldTime<=now+5000&&Number.isFinite(currentTime)&&currentTime>=oldTime&&
  Number.isSafeInteger(recorded.actualReadbackCount)&&recorded.actualReadbackCount>=0&&recorded.actualReadbackCount<=384&&
  Number.isSafeInteger(state.actualReadbackCount)&&state.actualReadbackCount>=recorded.actualReadbackCount&&state.actualReadbackCount<=384,
  'TELEGRAM_OWNER_HISTORY_REJECTED');
 const immutable=value=>Object.fromEntries(Object.entries(value).filter(([key])=>!['observedAt','actualReadbackCount'].includes(key)).map(([key,item])=>
  [key,timestampFields.has(key)?timestampIdentity(item):item]));
 fail(same(immutable(recorded),immutable(state)),'TELEGRAM_OWNER_HISTORY_CHANGED');
 return state;
}
