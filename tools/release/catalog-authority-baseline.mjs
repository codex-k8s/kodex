import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import {fingerprint} from './scoped-release.mjs';
import {inspectSource} from './application-source.mjs';
import {authorityOwnerDatabase,validateAuthorityDatabaseIdentity} from './migration-owner-database.mjs';
import {postgresBackupBoundary} from './backup-postgres-boundary.mjs';
import {authorityReadbackContract} from './authority-readback-targets.mjs';
import {readCatalogTarget,verifyCatalogTransition,validateCatalogPolicy} from './authority-catalog-transition.mjs';
import {canonicalRegistryMatches,deriveRegistryRotationIdentity,validateCatalogAuthorityEvidence,validateRetiredCatalogOwnerState} from './authority-rotation-transition.mjs';
import {readComponentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';

const ensure=(value,code)=>{if(!value)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const sha=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
export const catalogAuthorityBaselineSchema='kodex.catalog-authority-baseline/v1';
export const catalogAuthorityBaselineSQLSHA256='af23479e604042a981e2f1428e1100a7140248eb0ae80035566b006da56cf97f';

// Новый owner reader имеет отдельную фиксированную database и обращается лишь
// к штатной status-функции. Capture не запускает rotation/status Job или DDL.
export function catalogAuthorityBaselineSQL(){
 const sql=readFileSync(new URL('./catalog-authority-baseline-readback.sql',import.meta.url));
 ensure(createHash('sha256').update(sql).digest('hex')===catalogAuthorityBaselineSQLSHA256,'CATALOG_BASELINE_SQL_CHANGED');
 return sql;
}

// Проверяются оба actual UID, контролируемый source template и работающий
// единственный PostgreSQL container. OwnerReference сам по себе не равен STS.
export function authorityPostgresIdentity(kube){
 const get=(kind,name)=>JSON.parse(kube(['-n','kodex-system','get',kind,name,'-o','json']));
 const pod=get('pod','kodex-postgresql-0'),set=get('statefulset','kodex-postgresql');
 const owner=pod.metadata?.ownerReferences?.filter(value=>value.controller===true);
 ensure(pod.apiVersion==='v1'&&pod.kind==='Pod'&&pod.metadata?.name==='kodex-postgresql-0'&&pod.metadata.namespace==='kodex-system'&&uuid(pod.metadata.uid)&&
  !pod.metadata.deletionTimestamp&&pod.metadata.labels?.['app.kubernetes.io/name']==='kodex-postgresql'&&
  set.apiVersion==='apps/v1'&&set.kind==='StatefulSet'&&set.metadata?.name==='kodex-postgresql'&&set.metadata.namespace==='kodex-system'&&uuid(set.metadata.uid)&&
  !set.metadata.deletionTimestamp&&owner?.length===1&&owner[0].apiVersion==='apps/v1'&&owner[0].kind==='StatefulSet'&&owner[0].name===set.metadata.name&&owner[0].uid===set.metadata.uid,
  'CATALOG_BASELINE_POSTGRES_CHANGED');
 postgresBackupBoundary(pod.spec,'Pod');postgresBackupBoundary(set.spec?.template?.spec,'StatefulSet');
 const status=pod.status?.containerStatuses,container=pod.spec.containers[0];
 ensure(pod.status?.phase==='Running'&&!['NodeLost','Unknown'].includes(pod.status.reason)&&
  pod.status.conditions?.some(value=>value.type==='Ready'&&value.status==='True')&&!pod.status.conditions.some(value=>value.status==='Unknown')&&
  Array.isArray(status)&&status.length===1&&status[0].name==='postgresql'&&status[0].ready===true&&status[0].state?.running&&
  typeof status[0].imageID==='string'&&status[0].imageID.endsWith(container.image.slice(container.image.indexOf('@')+1)),
  'CATALOG_BASELINE_POSTGRES_NOT_READY');
 return {pod:'kodex-postgresql-0',uid:pod.metadata.uid,ownerUID:set.metadata.uid};
}

export function validateCatalogAuthorityBaseline(proof,now=Date.now()){
 ensure(proof?.schema===catalogAuthorityBaselineSchema&&proof.policyRevision===106&&uuid(proof.operationID)&&uuid(proof.registryUID)&&uuid(proof.publisherUID)&&
  ['policySHA256','registryDataSHA256','registrySHA256','publisherSpecSHA256','snapshotSHA256','rotationPlanSHA256','rotationEvidenceSHA256','targetSHA256','requiredReadbackTargetsSHA256'].every(key=>sha(proof[key]))&&
  Number.isSafeInteger(proof.registryRevision)&&proof.registryRevision>0&&Number.isSafeInteger(proof.snapshotRevision)&&proof.snapshotRevision>0&&
  Number.isSafeInteger(proof.expectedReadbackCount)&&proof.expectedReadbackCount>0&&proof.expectedReadbackCount<=384&&
  Number.isSafeInteger(proof.registeredReadbackCount)&&proof.registeredReadbackCount>=proof.expectedReadbackCount&&proof.registeredReadbackCount<=384&&
  Number.isSafeInteger(proof.actualReadbackCount)&&proof.actualReadbackCount>=proof.expectedReadbackCount&&proof.actualReadbackCount<=384&&
  proof.sqlSHA256===catalogAuthorityBaselineSQLSHA256&&uuid(proof.scope?.clusterUID)&&uuid(proof.scope.namespaceUID)&&uuid(proof.scope.installationUID)&&sha(proof.scope.installationSHA256)&&
  typeof proof.scope.context==='string'&&/^[A-Za-z0-9_.:@/-]{1,160}$/.test(proof.scope.context)&&
  proof.postgres?.pod==='kodex-postgresql-0'&&uuid(proof.postgres.uid)&&uuid(proof.postgres.ownerUID)&&
  proof.postgresBackend?.database==='internal_rpc_authority'&&Number.isFinite(now)&&
  Date.parse(proof.observedAt)<=now+5000&&now-Date.parse(proof.observedAt)<=30000,
  'CATALOG_BASELINE_PROOF_REJECTED');
 validateAuthorityDatabaseIdentity(proof.postgresBackend);
 const {baselineSHA256,...body}=proof;
 ensure(sha(baselineSHA256)&&baselineSHA256===fingerprint(body),'CATALOG_BASELINE_PROOF_REJECTED');
 return proof;
}

// Смена времени READ и количества уже подтверждённых optional readers не
// означает новую authority. Все owner/registry/publisher/backend pins неизменны.
export function sameCatalogAuthorityBaseline(before,after){
 const immutable=proof=>Object.fromEntries(Object.entries(proof??{}).filter(([key])=>
  !['baselineSHA256','observedAt','actualReadbackCount'].includes(key)));
 return same(immutable(before),immutable(after));
}

export function readCatalogAuthorityBaseline({plan,events,expected,now=Date.now},kube,io={}){
 const original=validateCatalogAuthorityEvidence(plan,events);
 ensure(plan.action==='rotate'&&original.jobUID&&events.some(row=>row.status==='APPLIED'&&row.operation==='REGISTRY_CAS')&&
  events.some(row=>row.status==='APPLIED'&&row.operation==='PUBLISHER_RESTART')&&events.some(row=>row.status==='SUCCEEDED'&&row.rotationStatus==='RETIRED'),
  'CATALOG_BASELINE_ROTATION_RECEIPT_REQUIRED');
 ensure((io.inspectSource??inspectSource)(plan.source).revision===plan.revision,'EXACT_SOURCE_REQUIRED');
 const get=(kind,name)=>JSON.parse(kube(['-n','kodex-system','get',kind,name,'-o','json']));
 const target=readCatalogTarget(plan.policyTransition.targetProfile,plan.context,get);
 verifyCatalogTransition(plan.policyTransition,target.transition);
 const {registry,publisher}=target;
 ensure(registry.metadata.uid===plan.registry.uid&&same(registry.data,plan.registry.desiredData)&&fingerprint(registry.data)===plan.registry.desiredDataSHA256&&
  registry.metadata.annotations?.['kodex.dev/rotation-intent']===plan.intentID&&registry.metadata.annotations?.['kodex.dev/rotation-operation']===plan.ownerOperationID&&
  publisher.metadata.uid===plan.publisher.uid&&fingerprint(publisher.spec)===plan.publisher.desiredSpecSHA256&&
  publisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-operation']===plan.ownerOperationID,
  'CATALOG_BASELINE_TARGET_CHANGED');
 const policy=validateCatalogPolicy(registry.data['authority-policy.json'],'desired',plan.policyTransition);
 const identity=deriveRegistryRotationIdentity(registry.data['key-delivery-targets.yaml']);
 ensure(policy.revision===106&&identity.ownerOperationID===plan.ownerOperationID&&identity.sourceRevision===plan.registry.sourceRevision&&
  identity.sourceDigestSHA256===plan.registry.sourceDigestSHA256&&canonicalRegistryMatches(registry.data['key-delivery-targets.yaml'],
   readFileSync(`${plan.source}/deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml`,'utf8')),
  'CATALOG_BASELINE_REGISTRY_CHANGED');
 const readers=authorityReadbackContract(registry.data['key-delivery-targets.yaml']);
 ensure(componentServingReady(publisher,activeDeploymentPods(readComponentServingInventory(kube,'internal-rpc-authority-publisher',publisher.metadata.uid),publisher.metadata.uid),publisher.spec.replicas),
  'CATALOG_BASELINE_PUBLISHER_NOT_READY');
 const postgresIdentity=()=>authorityPostgresIdentity(kube),postgres=postgresIdentity();
 const database=authorityOwnerDatabase(kube,postgresIdentity),backend=expected?.postgresBackend??database.identity(postgres);
 if(expected)ensure(same(postgres,expected.postgres),'CATALOG_BASELINE_POSTGRES_CHANGED');
 const state=database.read(catalogAuthorityBaselineSQL(),postgres,backend);
 validateRetiredCatalogOwnerState(state,plan,identity,readers,now());
 ensure(same(get('configmap','internal-rpc-authority-publisher-target-registry'),registry)&&same(get('deployment','internal-rpc-authority-publisher'),publisher),
  'CATALOG_BASELINE_TARGET_CHANGED');
 const scope=Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(key=>[key,plan.policyTransition[key]]));
 scope.context=plan.context;
 const body={schema:catalogAuthorityBaselineSchema,policyRevision:106,policySHA256:policy.sha256,operationID:state.operationId,
  registryUID:registry.metadata.uid,registryDataSHA256:fingerprint(registry.data),registryRevision:identity.sourceRevision,registrySHA256:identity.sourceDigestSHA256,
  publisherUID:publisher.metadata.uid,publisherSpecSHA256:fingerprint(publisher.spec),snapshotRevision:state.sourceRevision,snapshotSHA256:state.sourceDigestSHA256,
  ...readers,actualReadbackCount:state.actualReadbackCount,observedAt:state.observedAt,sqlSHA256:catalogAuthorityBaselineSQLSHA256,
  rotationPlanSHA256:fingerprint(plan),rotationEvidenceSHA256:fingerprint(events),targetSHA256:plan.policyTransition.targetSHA256,scope,postgres,postgresBackend:backend};
 const proof={...body,baselineSHA256:fingerprint(body)};
 validateCatalogAuthorityBaseline(proof,now());
 if(expected)ensure(sameCatalogAuthorityBaseline(expected,proof),'CATALOG_BASELINE_CHANGED');
 return Object.freeze(proof);
}
