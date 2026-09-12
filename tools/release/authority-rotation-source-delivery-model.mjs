import {fingerprint} from './scoped-release.mjs';

export const authorityGoImage='docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83';
export const authorityModule='services/internal/internal-rpc-authority';
const sourceCommand=['/workspace/tools/dev/run-go-hot-reload.sh'];
const jobCommand=['/workspace/tools/dev/run-go-command.sh'];
const uuid=/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const revision=/^[a-f0-9]{40}$/;
const sourcePath=/^\/srv\/kodex-dev\/[A-Za-z0-9._-]+$/;
const cachePath=/^\/srv\/kodex-dev\/[A-Za-z0-9._\/-]+$/;
const requiredMounts={
 'dev-source':['/workspace',true],
 'dev-go-mod':['/go/pkg/mod',true],
 'dev-go-sumdb':['/go/pkg/sumdb',true],
 'dev-go-tools':['/go/tools',true],
 'dev-build-publisher':['/go/build-cache',false],
 tmp:['/tmp',false],
};
const requireValue=(value,code)=>{if(!value)throw new Error(code);};
const exactly=(items,predicate)=>Array.isArray(items)&&items.filter(predicate).length===1;

export function validateSourcePublisher(deployment) {
 const pod=deployment?.spec?.template?.spec,c=pod?.containers?.find(item=>item.name==='publisher');
 requireValue(deployment?.kind==='Deployment'&&deployment.metadata?.namespace==='kodex-system'&&deployment.metadata.name==='internal-rpc-authority-publisher'&&
  uuid.test(deployment.metadata.uid??'')&&/^\d+$/.test(deployment.metadata.resourceVersion??'')&&Number.isInteger(deployment.spec.replicas)&&deployment.spec.replicas>=1&&deployment.spec.replicas<=2&&
  c?.image===authorityGoImage&&c.workingDir===`/workspace/${authorityModule}`&&fingerprint(c.command)===fingerprint(sourceCommand)&&
  fingerprint(c.args)===fingerprint([authorityModule,'./cmd/internal-rpc-authority-publisher','publisher']),'EXACT_SOURCE_PUBLISHER_REQUIRED');
 for(const [name,[mountPath,readOnly]] of Object.entries(requiredMounts)) {
  requireValue(exactly(c.volumeMounts,m=>m.name===name&&m.mountPath===mountPath&&(readOnly?m.readOnly===true:m.readOnly!==true)),'EXACT_SOURCE_PUBLISHER_MOUNTS_REQUIRED');
  const volume=pod.volumes?.find(v=>v.name===name);
  requireValue(volume&&(name==='tmp'?fingerprint(volume)===fingerprint({name:'tmp',emptyDir:{}}):volume.hostPath?.type==='Directory'&&(name==='dev-source'?sourcePath:cachePath).test(volume.hostPath.path)),
   'EXACT_SOURCE_PUBLISHER_VOLUMES_REQUIRED');
 }
 for(const [name,value] of Object.entries({GOMODCACHE:'/go/pkg/mod',GOCACHE:'/go/build-cache/cache',GOWORK:'off',GOTOOLCHAIN:'local',GOTMPDIR:'/go/build-cache/tmp',HOME:'/go/build-cache/home'}))
  requireValue(exactly(c.env,e=>e.name===name&&e.value===value),'EXACT_SOURCE_PUBLISHER_ENV_REQUIRED');
 return {container:c,source:pod.volumes.find(v=>v.name==='dev-source').hostPath.path};
}

export function validateRenderedSourceMigration(job) {
 const p=job?.spec?.template?.spec,c=p?.containers?.[0];
 requireValue(job?.kind==='Job'&&job.metadata?.namespace==='kodex-system'&&job.metadata.name==='internal-rpc-authority-migrate'&&
  p?.serviceAccountName==='internal-rpc-authority-migrator'&&p.automountServiceAccountToken===false&&p.restartPolicy==='Never'&&p.containers?.length===1&&
  c?.name==='migrate'&&c.image===authorityGoImage&&c.workingDir===`/workspace/${authorityModule}`&&fingerprint(c.command)===fingerprint(jobCommand)&&
  fingerprint(c.args)===fingerprint([authorityModule,'./cmd/cli','up']),'EXACT_RENDERED_SOURCE_MIGRATION_REQUIRED');
 for(const [name,path,readOnly] of [['dev-source','/workspace',true],['dev-go-mod','/go/pkg/mod',true],['dev-go-sumdb','/go/pkg/sumdb',true],['dev-go-tools','/go/tools',true]]) {
  requireValue(exactly(c.volumeMounts,m=>m.name===name&&m.mountPath===path&&m.readOnly===readOnly)&&
   exactly(p.volumes,v=>v.name===name&&v.hostPath?.type==='Directory'&&(name==='dev-source'?sourcePath:cachePath).test(v.hostPath.path)),'EXACT_RENDERED_MIGRATION_CACHE_REQUIRED');
 }
 requireValue(exactly(c.volumeMounts,m=>m.mountPath==='/go/build-cache'&&m.readOnly!==true)&&
  exactly(p.volumes,v=>v.name===c.volumeMounts.find(m=>m.mountPath==='/go/build-cache').name&&v.hostPath?.type==='Directory'&&cachePath.test(v.hostPath.path)),
 'EXACT_RENDERED_MIGRATION_BUILD_CACHE_REQUIRED');
 requireValue(exactly(c.volumeMounts,m=>m.name==='tmp'&&m.mountPath==='/tmp'&&m.readOnly!==true)&&exactly(p.volumes,v=>fingerprint(v)===fingerprint({name:'tmp',emptyDir:{}}))&&
  exactly(c.volumeMounts,m=>m.name==='postgresql-credentials'&&m.mountPath==='/var/run/secrets/kodex/internal-rpc-authority/postgres'&&m.readOnly===true)&&
  exactly(p.volumes,v=>v.name==='postgresql-credentials'&&v.secret?.secretName==='internal-rpc-authority-postgres-migration')&&
  exactly(c.volumeMounts,m=>m.name==='postgresql-ca'&&m.mountPath==='/var/run/config/kodex/internal-rpc-authority/postgresql'&&m.readOnly===true)&&
  exactly(p.volumes,v=>v.name==='postgresql-ca'&&v.configMap?.name==='internal-rpc-authority-postgresql-ca'),'EXACT_RENDERED_MIGRATION_RUNTIME_REQUIRED');
 for(const [name,value] of Object.entries({INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE:'/var/run/secrets/kodex/internal-rpc-authority/postgres/dsn',INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME:'internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local',
  GOMODCACHE:'/go/pkg/mod',GOCACHE:'/go/build-cache/cache',GOWORK:'off',GOTOOLCHAIN:'local',GOTMPDIR:'/go/build-cache/tmp',HOME:'/go/build-cache/home'}))
  requireValue(exactly(c.env,e=>e.name===name&&e.value===value&&Object.keys(e).length===2),'EXACT_MIGRATION_ENV_REQUIRED');
 return job;
}

export function createSourceMigrationJob(rendered,plan) {
 validateRenderedSourceMigration(rendered);
 requireValue(uuid.test(plan?.intentID??'')&&revision.test(plan?.revision??'')&&sourcePath.test(plan?.source??''),'SOURCE_DELIVERY_PLAN_INVALID');
 const spec=structuredClone(rendered.spec);delete spec.selector;delete spec.ttlSecondsAfterFinished;
 spec.backoffLimit=0;spec.activeDeadlineSeconds=300;
 // Материализуем defaults до intent/hash, а не удаляем их из readback.
 // Явные rendered values сохраняются; любое другое изменение spec — отказ.
 const absent=(value,key,fallback)=>{if(!Object.hasOwn(value,key))value[key]=fallback;};
 if(!Object.hasOwn(spec,'completions')&&!Object.hasOwn(spec,'parallelism'))spec.completions=1;
 for(const [key,value] of Object.entries({parallelism:1,completionMode:'NonIndexed',manualSelector:false,suspend:false,
  podReplacementPolicy:spec.podFailurePolicy?'Failed':'TerminatingOrFailed'}))absent(spec,key,value);
 const pod=spec.template.spec;
 for(const [key,value] of Object.entries({dnsPolicy:'ClusterFirst',schedulerName:'default-scheduler',
  serviceAccount:pod.serviceAccountName,terminationGracePeriodSeconds:30}))absent(pod,key,value);
 for(const container of [...pod.containers,...(pod.initContainers??[])]){
  if(!Object.hasOwn(container,'imagePullPolicy')){
   requireValue(/@sha256:[a-f0-9]{64}$/.test(container.image??''),'SOURCE_MIGRATION_DEFAULT_IMAGE_UNPINNED');
   container.imagePullPolicy='IfNotPresent';
  }
  absent(container,'terminationMessagePath','/dev/termination-log');absent(container,'terminationMessagePolicy','File');
 }
 for(const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name'])delete spec.template.metadata?.labels?.[key];
 spec.template.spec.volumes.find(v=>v.name==='dev-source').hostPath.path=plan.source;
 return {apiVersion:'batch/v1',kind:'Job',metadata:{namespace:'kodex-system',name:`authority-source-${plan.intentID}`,
  labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging'},annotations:{'kodex.dev/source-delivery-intent':plan.intentID,
   'kodex.dev/source-revision':plan.revision}},spec};
}

export function sourceMigrationIdentity(actual,expected,{requireUID=true}={}) {
 requireValue((!requireUID||uuid.test(actual?.metadata?.uid??''))&&actual?.metadata?.name===expected.metadata.name&&actual.metadata.namespace==='kodex-system'&&
  actual.metadata.annotations?.['kodex.dev/source-delivery-intent']===expected.metadata.annotations['kodex.dev/source-delivery-intent'],'SOURCE_MIGRATION_IDENTITY_CHANGED');
 const got=structuredClone(actual.spec),want=structuredClone(expected.spec);delete got.selector;
 for(const value of [got,want])for(const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name'])delete value.template.metadata?.labels?.[key];
 requireValue(fingerprint(got)===fingerprint(want),'SOURCE_MIGRATION_SPEC_CHANGED');
 return {jobName:actual.metadata.name,jobUID:actual.metadata.uid,jobSpecSHA256:fingerprint(want)};
}
function expectedMigrationSpecSHA256(expected){const spec=structuredClone(expected.spec);for(const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name'])delete spec.template.metadata?.labels?.[key];return fingerprint(spec);}
export function buildSourceMigrationIntent(expected,plan) {
 return {version:1,kind:'AUTHORITY_SOURCE_MIGRATION_INTENT',planSHA256:fingerprint(plan),intentID:plan.intentID,jobName:expected.metadata.name,jobSpecSHA256:expectedMigrationSpecSHA256(expected)};
}
export function validateSourceMigrationIntent(intent,expected,plan) {
 requireValue(intent?.version===1&&intent.kind==='AUTHORITY_SOURCE_MIGRATION_INTENT'&&intent.planSHA256===fingerprint(plan)&&intent.intentID===plan.intentID&&
  intent.jobName===expected.metadata.name&&intent.jobSpecSHA256===expectedMigrationSpecSHA256(expected),'SOURCE_MIGRATION_INTENT_REJECTED');return intent;
}
export function buildSourceMigrationReceipt(actual,expected,plan) {
 const identity=sourceMigrationIdentity(actual,expected);
 return {version:1,kind:'AUTHORITY_SOURCE_MIGRATION_RECEIPT',planSHA256:fingerprint(plan),intentID:plan.intentID,...identity};
}
export function validateSourceMigrationReceipt(receipt,expected,plan) {
 requireValue(receipt?.version===1&&receipt.kind==='AUTHORITY_SOURCE_MIGRATION_RECEIPT'&&receipt.planSHA256===fingerprint(plan)&&receipt.intentID===plan.intentID&&
  receipt.jobName===expected.metadata.name&&uuid.test(receipt.jobUID??'')&&receipt.jobSpecSHA256===expectedMigrationSpecSHA256(expected),'SOURCE_MIGRATION_RECEIPT_REJECTED');return receipt;
}
export function verifySourceMigrationReadback(actual,expected,receipt) {
 const identity=sourceMigrationIdentity(actual,expected,{requireUID:Boolean(receipt)});
 if(receipt)requireValue(identity.jobUID===receipt.jobUID&&identity.jobName===receipt.jobName&&identity.jobSpecSHA256===receipt.jobSpecSHA256,'SOURCE_MIGRATION_REPLACED');
 return identity;
}

export function createPublisherSourceCAS(deployment,plan,rollback=false) {
 const {source}=validateSourcePublisher(deployment),target=rollback?plan.publisher.beforeSource:plan.source;
 requireValue(deployment.metadata.uid===plan.publisher.uid&&deployment.metadata.resourceVersion===plan.publisher.resourceVersion&&
  fingerprint(deployment.spec)===plan.publisher.beforeSpecSHA256&&source===plan.publisher.beforeSource&&sourcePath.test(target),'SOURCE_PUBLISHER_PREDECESSOR_CHANGED');
 const next=structuredClone(deployment);next.spec.template.spec.volumes.find(v=>v.name==='dev-source').hostPath.path=target;
 next.spec.template.metadata.annotations??={};next.spec.template.metadata.annotations['kodex.dev/source-revision']=rollback?plan.publisher.beforeRevision:plan.revision;
 next.spec.template.metadata.annotations['kodex.dev/source-delivery-intent']=plan.intentID;
 return next;
}

export function createPublisherRollbackCAS(deployment,plan) {
 const {source}=validateSourcePublisher(deployment);
 requireValue(deployment.metadata.uid===plan.publisher.uid&&fingerprint(deployment.spec)===plan.publisher.desiredSpecSHA256&&source===plan.source&&
  deployment.spec.template.metadata.annotations?.['kodex.dev/source-delivery-intent']===plan.intentID,'SOURCE_PUBLISHER_ROLLBACK_PREDECESSOR_CHANGED');
 const next=structuredClone(deployment);next.spec.template.spec.volumes.find(v=>v.name==='dev-source').hostPath.path=plan.publisher.beforeSource;
 const annotations=next.spec.template.metadata.annotations;
 for(const key of ['kodex.dev/source-revision','kodex.dev/source-delivery-intent']){
  const value=plan.publisher.beforeAnnotations[key];if(value===undefined)delete annotations[key];else annotations[key]=value;
 }
 if(!plan.publisher.beforeAnnotationsPresent&&Object.keys(annotations).length===0)delete next.spec.template.metadata.annotations;
 requireValue(fingerprint(next.spec)===plan.publisher.beforeSpecSHA256,'SOURCE_PUBLISHER_ROLLBACK_NOT_EXACT');return next;
}
export function classifyPublisherSourceState(deployment,plan){
 requireValue(deployment?.metadata?.uid===plan.publisher.uid,'SOURCE_PUBLISHER_REPLACED');
 const digest=fingerprint(deployment.spec);
 if(digest===plan.publisher.beforeSpecSHA256)return 'BEFORE';
 if(digest===plan.publisher.desiredSpecSHA256)return 'AFTER';
 throw new Error('SOURCE_PUBLISHER_SPEC_CHANGED');
}

export function validateSourceDeliveryPlan(plan) {
 requireValue(plan?.version===1&&plan.kind==='AUTHORITY_SOURCE_DELIVERY'&&uuid.test(plan.intentID??'')&&revision.test(plan.revision??'')&&
  sourcePath.test(plan.source??'')&&uuid.test(plan.namespaceUID??'')&&plan.publisher?.uid&&plan.publisher.beforeSpecSHA256&&
  plan.publisher.desiredSpecSHA256&&plan.publisher.beforeAnnotations&&typeof plan.publisher.beforeAnnotationsPresent==='boolean'&&plan.migration?.renderedSpecSHA256&&plan.boundary?.registryDataSHA256&&plan.boundary?.neighborsSHA256,'SOURCE_DELIVERY_PLAN_INVALID');
 createSourceMigrationJob(plan.migration.rendered,plan);
}
