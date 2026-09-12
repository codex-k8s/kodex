import assert from 'node:assert/strict';
import test from 'node:test';
import {authorityGoImage,buildSourceMigrationReceipt,classifyPublisherSourceState,createPublisherRollbackCAS,createPublisherSourceCAS,createSourceMigrationJob,validateRenderedSourceMigration,validateSourceMigrationReceipt,validateSourcePublisher,verifySourceMigrationReadback} from './authority-rotation-source-delivery-model.mjs';
import {fingerprint} from './scoped-release.mjs';

const uid='14340000-0000-4000-8000-000000000001';
const volumes=[['dev-source','/srv/kodex-dev/workspace-release1226'],['dev-go-mod','/srv/kodex-dev/cache/mod'],['dev-go-sumdb','/srv/kodex-dev/cache/sumdb'],['dev-go-tools','/srv/kodex-dev/cache/tools'],['dev-build-publisher','/srv/kodex-dev/cache/build']].map(([name,path])=>({name,hostPath:{path,type:'Directory'}})).concat({name:'tmp',emptyDir:{}});
const mounts=[['dev-source','/workspace',true],['dev-go-mod','/go/pkg/mod',true],['dev-go-sumdb','/go/pkg/sumdb',true],['dev-go-tools','/go/tools',true],['dev-build-publisher','/go/build-cache',false],['tmp','/tmp',false]].map(([name,mountPath,readOnly])=>({name,mountPath,readOnly}));
const goEnv=Object.entries({GOMODCACHE:'/go/pkg/mod',GOCACHE:'/go/build-cache/cache',GOWORK:'off',GOTOOLCHAIN:'local',GOTMPDIR:'/go/build-cache/tmp',HOME:'/go/build-cache/home'}).map(([name,value])=>({name,value}));
function publisher(){return structuredClone({kind:'Deployment',metadata:{name:'internal-rpc-authority-publisher',namespace:'kodex-system',uid,resourceVersion:'7'},spec:{replicas:1,template:{metadata:{},spec:{containers:[{name:'publisher',image:authorityGoImage,workingDir:'/workspace/services/internal/internal-rpc-authority',command:['/workspace/tools/dev/run-go-hot-reload.sh'],args:['services/internal/internal-rpc-authority','./cmd/internal-rpc-authority-publisher','publisher'],env:goEnv,volumeMounts:mounts}],volumes}}}});}
function migration(){return structuredClone({kind:'Job',metadata:{name:'internal-rpc-authority-migrate',namespace:'kodex-system'},spec:{ttlSecondsAfterFinished:60,template:{metadata:{labels:{'job-name':'old'}},spec:{serviceAccountName:'internal-rpc-authority-migrator',automountServiceAccountToken:false,restartPolicy:'Never',containers:[{name:'migrate',image:authorityGoImage,workingDir:'/workspace/services/internal/internal-rpc-authority',command:['/workspace/tools/dev/run-go-command.sh'],args:['services/internal/internal-rpc-authority','./cmd/cli','up'],env:[...goEnv,{name:'INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE',value:'/var/run/secrets/kodex/internal-rpc-authority/postgres/dsn'},{name:'INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME',value:'internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local'}],volumeMounts:[...mounts.map(m=>m.name==='dev-build-publisher'?{...m,name:'dev-build-migrate'}:m),{name:'postgresql-credentials',mountPath:'/var/run/secrets/kodex/internal-rpc-authority/postgres',readOnly:true},{name:'postgresql-ca',mountPath:'/var/run/config/kodex/internal-rpc-authority/postgresql',readOnly:true}]}],volumes:[...volumes.map(v=>v.name==='dev-build-publisher'?{...v,name:'dev-build-migrate'}:v),{name:'postgresql-credentials',secret:{secretName:'internal-rpc-authority-postgres-migration'}},{name:'postgresql-ca',configMap:{name:'internal-rpc-authority-postgresql-ca'}}]}}}});}
const plan={intentID:'14340000-0000-4000-8000-000000000002',source:'/srv/kodex-dev/next',revision:'a'.repeat(40)};

test('publisher proof pins exact command, caches, writable build and tmp',()=>{
 assert.equal(validateSourcePublisher(publisher()).source,'/srv/kodex-dev/workspace-release1226');
 for(const mutate of [d=>d.spec.template.spec.containers[0].args[1]='./cmd/cli',d=>d.spec.template.spec.containers[0].volumeMounts.find(m=>m.name==='dev-go-mod').readOnly=false,d=>d.spec.template.spec.volumes.find(v=>v.name==='dev-go-tools').hostPath.path='/tmp/tools']){const next=publisher();mutate(next);assert.throws(()=>validateSourcePublisher(next));}
});
test('source migration is cloned from actual render and preserves offline caches',()=>{
 validateRenderedSourceMigration(migration());const job=createSourceMigrationJob(migration(),plan);
 assert.equal(job.spec.template.spec.volumes.find(v=>v.name==='dev-source').hostPath.path,plan.source);
 assert.deepEqual(job.spec.template.spec.containers[0].command,['/workspace/tools/dev/run-go-command.sh']);
 assert.equal(job.spec.ttlSecondsAfterFinished,undefined);
 const actual=structuredClone(job);actual.metadata.uid=uid;actual.spec.selector={matchLabels:{'batch.kubernetes.io/controller-uid':uid}};actual.spec.template.metadata.labels['batch.kubernetes.io/controller-uid']=uid;
 verifySourceMigrationReadback(actual,job);actual.spec.template.spec.volumes.find(v=>v.name==='dev-go-mod').hostPath.path='/srv/kodex-dev/foreign';assert.throws(()=>verifySourceMigrationReadback(actual,job));
});
test('migration defaults materialized before intent keep strict app/init readback',()=>{
 const rendered=migration();rendered.spec.template.spec.initContainers=[{name:'prepare',image:authorityGoImage,command:['true']}];
 const job=createSourceMigrationJob(rendered,plan),pod=job.spec.template.spec;
 assert.equal(job.spec.completions,1);assert.equal(job.spec.parallelism,1);assert.equal(job.spec.completionMode,'NonIndexed');
 assert.equal(job.spec.manualSelector,false);assert.equal(job.spec.suspend,false);assert.equal(job.spec.podReplacementPolicy,'TerminatingOrFailed');
 assert.equal(pod.serviceAccount,pod.serviceAccountName);assert.equal(pod.dnsPolicy,'ClusterFirst');assert.equal(pod.schedulerName,'default-scheduler');assert.equal(pod.terminationGracePeriodSeconds,30);
 for(const container of [...pod.containers,...pod.initContainers]){
  assert.equal(container.imagePullPolicy,'IfNotPresent');assert.equal(container.terminationMessagePath,'/dev/termination-log');assert.equal(container.terminationMessagePolicy,'File');
 }
 assert.equal(rendered.spec.parallelism,undefined);
 const actual=structuredClone(job);actual.metadata.uid=uid;verifySourceMigrationReadback(actual,job);
 for(const mutate of [s=>s.parallelism=2,s=>s.podReplacementPolicy='Failed',s=>s.template.spec.terminationGracePeriodSeconds=60,
  s=>s.template.spec.containers[0].terminationMessagePolicy='FallbackToLogsOnError',s=>s.template.spec.initContainers[0].command=['foreign'],s=>s.template.spec.unknown=true]){
  const changed=structuredClone(actual);mutate(changed.spec);assert.throws(()=>verifySourceMigrationReadback(changed,job),/SOURCE_MIGRATION_SPEC_CHANGED/);
 }
 rendered.spec.parallelism=2;rendered.spec.template.spec.terminationGracePeriodSeconds=45;rendered.spec.template.spec.containers[0].imagePullPolicy='Always';
 const explicit=createSourceMigrationJob(rendered,plan);assert.equal(explicit.spec.parallelism,2);assert.equal(explicit.spec.completions,undefined);assert.equal(explicit.spec.template.spec.terminationGracePeriodSeconds,45);assert.equal(explicit.spec.template.spec.containers[0].imagePullPolicy,'Always');
 rendered.spec.template.spec.initContainers[0].image='unversioned:latest';assert.throws(()=>createSourceMigrationJob(rendered,plan),/SOURCE_MIGRATION_DEFAULT_IMAGE_UNPINNED/);
});

test('durable receipt pins first Job UID before and after terminal',()=>{
 const job=createSourceMigrationJob(migration(),plan),a=structuredClone(job);a.metadata.uid=uid;a.spec.selector={matchLabels:{'batch.kubernetes.io/controller-uid':uid}};
 const receipt=buildSourceMigrationReceipt(a,job,plan);validateSourceMigrationReceipt(receipt,job,plan);verifySourceMigrationReadback(a,job,receipt);
 const b=structuredClone(a);b.metadata.uid='14340000-0000-4000-8000-000000000099';assert.throws(()=>verifySourceMigrationReadback(b,job,receipt),/SOURCE_MIGRATION_REPLACED/);
 a.status={succeeded:1};verifySourceMigrationReadback(a,job,receipt);b.status={succeeded:1};assert.throws(()=>verifySourceMigrationReadback(b,job,receipt),/SOURCE_MIGRATION_REPLACED/);
 const foreignUID={...receipt,jobUID:b.metadata.uid};validateSourceMigrationReceipt(foreignUID,job,plan);assert.throws(()=>verifySourceMigrationReadback(a,job,foreignUID),/SOURCE_MIGRATION_REPLACED/);
 for(const mutate of [r=>r.planSHA256='f'.repeat(64),r=>r.intentID='foreign',r=>r.jobSpecSHA256='f'.repeat(64)]){const changed=structuredClone(receipt);mutate(changed);assert.throws(()=>validateSourceMigrationReceipt(changed,job,plan),/RECEIPT_REJECTED/);}
});
test('publisher source patch is UID, resourceVersion and full-spec CAS',()=>{
 const before=publisher();const delivery={...plan,publisher:{uid,resourceVersion:'7',beforeSpecSHA256:fingerprint(before.spec),beforeSource:'/srv/kodex-dev/workspace-release1226',beforeRevision:'b'.repeat(40),beforeAnnotations:{},beforeAnnotationsPresent:false}};
 const desired=createPublisherSourceCAS(before,delivery);delivery.publisher.desiredSpecSHA256=fingerprint(desired.spec);assert.equal(desired.spec.template.spec.volumes.find(v=>v.name==='dev-source').hostPath.path,plan.source);
 assert.equal(classifyPublisherSourceState(before,delivery),'BEFORE');assert.equal(classifyPublisherSourceState(desired,delivery),'AFTER');assert.throws(()=>createPublisherSourceCAS(desired,delivery),/PREDECESSOR_CHANGED/);
 desired.metadata.resourceVersion='8';assert.equal(fingerprint(createPublisherRollbackCAS(desired,delivery).spec),delivery.publisher.beforeSpecSHA256);
 for(const mutate of [d=>d.metadata.resourceVersion='8',d=>d.spec.template.spec.containers[0].env.push({name:'FOREIGN',value:'1'})]){const changed=publisher();mutate(changed);assert.throws(()=>createPublisherSourceCAS(changed,delivery),/PREDECESSOR_CHANGED/);}
});
test('UNKNOWN recovery rejects replacement and accepts only exact readback',()=>{
 const before=publisher(),delivery={...plan,publisher:{uid,resourceVersion:'7',beforeSpecSHA256:fingerprint(before.spec),beforeSource:'/srv/kodex-dev/workspace-release1226',beforeRevision:'b'.repeat(40),beforeAnnotations:{},beforeAnnotationsPresent:false}};
 const after=createPublisherSourceCAS(before,delivery);delivery.publisher.desiredSpecSHA256=fingerprint(after.spec);
 const replacement=structuredClone(after);replacement.metadata.uid='14340000-0000-4000-8000-000000000099';assert.throws(()=>classifyPublisherSourceState(replacement,delivery),/REPLACED/);
 const drift=structuredClone(after);drift.spec.template.spec.containers[0].args[1]='./cmd/foreign';assert.throws(()=>classifyPublisherSourceState(drift,delivery),/SPEC_CHANGED/);
 assert.equal(classifyPublisherSourceState(after,delivery),'AFTER');
});
