import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {inspectSource} from './application-source.mjs';
import {authorityOwnerDatabase,validateAuthorityDatabaseIdentity} from './migration-owner-database.mjs';
import {authorityPostgresIdentity,catalogAuthorityBaselineSQL,catalogAuthorityBaselineSQLSHA256} from './catalog-authority-baseline.mjs';
import {validateTelegramOwnerHistory} from './telegram-authority-owner.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const digest=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const schema='kodex.personal-integrations-authority-owner-scope/v1';
const root=resolve(fileURLToPath(new URL('../../',import.meta.url)));

// Scope относится только к owner authority107→110. Cohort/API target не
// подставляется вместо этого hash: отдельный consumer связывает оба scope.
export function validatePersonalIntegrationsOwnerScope(scope,plan){
 fail(scope&&Object.keys(scope).sort().join(',')==='context,executorRevision,executorSource,kubeconfig,postgres,postgresBackend,profile,schema,sourceRevision,sqlSHA256,targetSHA256'&&
  scope.schema===schema&&scope.profile===personalIntegrationsAuthorityTransition&&
  plan.policyTransition?.profile===personalIntegrationsAuthorityTransition&&
  scope.context===plan.context&&scope.kubeconfig===plan.kubeconfig&&scope.sourceRevision===plan.revision&&
  scope.targetSHA256===plan.policyTransition.targetSHA256&&digest(scope.targetSHA256)&&
  scope.sqlSHA256===catalogAuthorityBaselineSQLSHA256&&typeof scope.executorSource==='string'&&scope.executorSource===resolve(scope.executorSource)&&
  /^[a-f0-9]{40}$/.test(scope.executorRevision??'')&&scope.postgres?.pod==='kodex-postgresql-0'&&uuid(scope.postgres.uid)&&uuid(scope.postgres.ownerUID),
  'PERSONAL_INTEGRATIONS_OWNER_SCOPE_REJECTED');
 validateAuthorityDatabaseIdentity(scope.postgresBackend);
 return scope;
}

// Только fixed R/O owner transport и immutable PostgreSQL identity. Capture не
// создаёт status/rotation Job, не меняет registry и не объявляет DELIVER/RETIRE.
export function capturePersonalIntegrationsOwnerScope(plan,kube,io={}){
 const postgresIdentity=()=>authorityPostgresIdentity(kube),postgres=postgresIdentity();
 const database=authorityOwnerDatabase(kube,postgresIdentity),postgresBackend=database.identity(postgres);
 const scope={schema,profile:personalIntegrationsAuthorityTransition,context:plan.context,kubeconfig:plan.kubeconfig,
  sourceRevision:plan.revision,targetSHA256:plan.policyTransition.targetSHA256,postgres,postgresBackend,
  sqlSHA256:catalogAuthorityBaselineSQLSHA256,executorSource:root,executorRevision:(io.inspectSource??inspectSource)(root).revision};
 return Object.freeze(validatePersonalIntegrationsOwnerScope(scope,plan));
}

// Позднее чтение обновляет только observedAt/монотонный optional readback count
// точной прежней операции. Сохраняется прежний qualified wire-history validator;
// Telegram scope/профиль не расширяется. State сам по себе не является admission.
export function readPersonalIntegrationsCurrentOwner(scope,plan,kube,io={}){
 validatePersonalIntegrationsOwnerScope(scope,plan);
 fail(scope.executorSource===root&&(io.inspectSource??inspectSource)(scope.executorSource).revision===scope.executorRevision,
  'PERSONAL_INTEGRATIONS_OWNER_EXECUTOR_CHANGED');
 const postgresIdentity=()=>authorityPostgresIdentity(kube),database=authorityOwnerDatabase(kube,postgresIdentity);
 fail(same(postgresIdentity(),scope.postgres),'PERSONAL_INTEGRATIONS_OWNER_POSTGRES_CHANGED');
 const state=database.read(catalogAuthorityBaselineSQL(),scope.postgres,scope.postgresBackend);
 const now=(io.now??Date.now)(),observed=Date.parse(state?.observedAt);
 fail(Number.isFinite(now)&&Number.isFinite(observed)&&observed<=now+5000&&now-observed<=30000,
  'PERSONAL_INTEGRATIONS_OWNER_OBSERVATION_STALE');
 fail(same(postgresIdentity(),scope.postgres),'PERSONAL_INTEGRATIONS_OWNER_POSTGRES_CHANGED');
 return {state,scope};
}

// Позднее чтение той же операции сохраняет immutable историю status Job;
// только observation time и монотонный optional readback count обновляются.
export function readPersonalIntegrationsOwner(scope,plan,recorded,kube,io={}){
 const current=readPersonalIntegrationsCurrentOwner(scope,plan,kube,io);
 validateTelegramOwnerHistory(recorded,current.state,(io.now??Date.now)());
 return current;
}
