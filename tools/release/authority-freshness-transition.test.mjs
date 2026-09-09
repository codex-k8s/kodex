import test from 'node:test';
import assert from 'node:assert/strict';
import { createMigrationJob, validateMigrationTemplate, authorityConsumers, verifyJobReadback } from './authority-freshness-transition.mjs';
const uid='13130000-0000-4000-8000-000000000001';
const template=()=>({apiVersion:'batch/v1',kind:'Job',metadata:{name:'internal-rpc-authority-migrate',namespace:'kodex-system',uid,resourceVersion:'7'},status:{succeeded:1},spec:{backoffLimit:2,activeDeadlineSeconds:300,ttlSecondsAfterFinished:86400,selector:{matchLabels:{'batch.kubernetes.io/controller-uid':uid}},template:{metadata:{labels:{'job-name':'internal-rpc-authority-migrate','app.kubernetes.io/component':'migrator'}},spec:{restartPolicy:'Never',serviceAccountName:'internal-rpc-authority-migrator',automountServiceAccountToken:false,containers:[{name:'migrate',image:'docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83',workingDir:'/workspace/services/internal/internal-rpc-authority',command:['/workspace/tools/dev/run-go-command.sh'],args:['services/internal/internal-rpc-authority','./cmd/cli','up'],env:[{name:'INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE',value:'/private/dsn'},{name:'INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME',value:'exact.invalid'}],volumeMounts:[{name:'dev-source',mountPath:'/workspace',readOnly:true},{name:'postgresql-credentials',mountPath:'/var/run/secrets/kodex/internal-rpc-authority/postgres',readOnly:true}]}],volumes:[{name:'dev-source',hostPath:{path:'/srv/kodex-dev/old',type:'Directory'}},{name:'postgresql-credentials',secret:{secretName:'internal-rpc-authority-postgres-migration'}}]}}}});
const plan=()=>({intent:'13130000-0000-4000-8000-000000000002',revision:'a'.repeat(40),source:'/srv/kodex-dev/freshness',action:'freshness-activate'});
test('new bounded Job preserves exact identity and mounts; old history unchanged',()=>{
 const original=template(),before=structuredClone(original),job=createMigrationJob(original,plan());
 assert.deepEqual(original,before);assert.equal(job.spec.backoffLimit,0);assert.equal(job.spec.activeDeadlineSeconds,300);
 assert.equal(job.spec.ttlSecondsAfterFinished,undefined);assert.equal(job.spec.selector,undefined);
 assert.equal(job.spec.template.metadata.labels['job-name'],undefined);
 assert.equal(job.spec.template.spec.volumes[0].hostPath.path,plan().source);
 assert.deepEqual(job.spec.template.spec.volumes[1],before.spec.template.spec.volumes[1]);
 assert.deepEqual(job.spec.template.spec.containers[0].env,before.spec.template.spec.containers[0].env);
 assert.deepEqual(job.spec.template.spec.containers[0].args.slice(-6),['--expected-version','1','--activation-id',plan().intent,'--confirm','ACTIVATE-STAGING-AUTHORITY-FRESHNESS']);
});
test('template drift, foreign service account, writable source and unsupported action rejected',()=>{
 for(const mutate of [j=>j.status.succeeded=0,j=>j.metadata.namespace='production',j=>j.spec.template.spec.serviceAccountName='owner',j=>j.spec.template.spec.containers[0].volumeMounts[0].readOnly=false,j=>j.spec.template.spec.volumes[1].secret.secretName='foreign']) {const j=template();mutate(j);assert.throws(()=>validateMigrationTemplate(j));}
 for(const action of ['down','up --force','reset'])assert.throws(()=>createMigrationJob(template(),{...plan(),action}));
 assert.throws(()=>createMigrationJob(template(),{...plan(),source:'/srv/kodex-dev/../private'}));
});
test('unknown create readback requires exact intent and entire spec',()=>{
 const expected=createMigrationJob(template(),plan()),actual=structuredClone(expected);verifyJobReadback(actual,expected);
 for(const mutate of [j=>j.metadata.annotations['kodex.dev/freshness-intent']=uid,j=>j.spec.template.spec.containers[0].args.push('--force'),j=>j.spec.template.spec.volumes[0].hostPath.path='/srv/kodex-dev/foreign']) {const j=structuredClone(actual);mutate(j);assert.throws(()=>verifyJobReadback(j,expected));}
});
function resources() {
 const c={name:'internal-rpc-authority-issuer',image:'registry.invalid/authority@sha256:'+'a'.repeat(64),command:['/workspace/tools/dev/run-go-hot-reload.sh'],args:['services/internal/internal-rpc-authority','./cmd/internal-rpc-authority-issuer','internal-rpc-authority-issuer']};
 const verifier={...c,name:'internal-rpc-authority-verifier',args:[c.args[0],'./cmd/internal-rpc-authority-verifier','internal-rpc-authority-verifier']};
 const deployment={kind:'Deployment',metadata:{name:'control-plane',uid,generation:2},spec:{replicas:1,template:{spec:{containers:[c,verifier]}}},status:{observedGeneration:2}};
 const rs={kind:'ReplicaSet',metadata:{uid:'rs',ownerReferences:[{uid,controller:true}]}};
 const pod={kind:'Pod',metadata:{name:'cp-1',uid:'pod',ownerReferences:[{uid:'rs',controller:true}]},spec:{containers:[c,verifier]},status:{phase:'Running',containerStatuses:[{name:c.name,ready:true,state:{running:{}},containerID:'containerd://'+'b'.repeat(64),imageID:c.image},{name:verifier.name,ready:true,state:{running:{}},containerID:'containerd://'+'c'.repeat(64),imageID:verifier.image}]}};
 return [deployment,rs,pod];
}
test('consumer ownership excludes completed Job and detects replacement/drift',()=>{
 const all=resources();all.push({...structuredClone(all[2]),metadata:{name:'bootstrap',uid:'foreign',ownerReferences:[{uid:'job',controller:true}]},status:{phase:'Succeeded'}});
 assert.equal(authorityConsumers(all).length,2);assert.equal(authorityConsumers(all)[0].role,'issuer');
 const changed=resources();changed[2].spec.containers=structuredClone(changed[2].spec.containers);changed[2].spec.containers[0].args[1]='./cmd/foreign';assert.throws(()=>authorityConsumers(changed));
 const missing=resources();missing[2].status.containerStatuses[0].ready=false;assert.throws(()=>authorityConsumers(missing));
});
function injectServiceAccountProjection(all) {
 const deployment=all[0],pod=all[2],name='kube-api-access-a1b2c';
 deployment.spec.template.spec.automountServiceAccountToken=true;
 deployment.spec.template.spec.volumes=[];
 pod.spec.automountServiceAccountToken=true;
 pod.spec.volumes=[{name,projected:{defaultMode:420,sources:[
  {serviceAccountToken:{expirationSeconds:3607,path:'token'}},
  {configMap:{name:'kube-root-ca.crt',items:[{key:'ca.crt',path:'ca.crt'}]}},
  {downwardAPI:{items:[{path:'namespace',fieldRef:{apiVersion:'v1',fieldPath:'metadata.namespace'}}]}},
 ]}}];
 pod.spec.containers=structuredClone(pod.spec.containers);
 for(const container of pod.spec.containers)container.volumeMounts=[{name,mountPath:'/var/run/secrets/kubernetes.io/serviceaccount',readOnly:true}];
 return all;
}
test('consumer accepts only bounded Kubernetes service-account projection',()=>{
 const healthy=injectServiceAccountProjection(resources());assert.equal(authorityConsumers(healthy).length,2);
 const mutations=[
  all=>all[2].spec.containers[0].volumeMounts[0].mountPath='/var/run/foreign',
  all=>all[2].spec.containers[0].volumeMounts[0].readOnly=false,
  all=>all[2].spec.containers[0].volumeMounts[0].name='kube-api-access-fffff',
  all=>all[2].spec.volumes[0].projected.defaultMode=511,
  all=>all[2].spec.volumes[0].projected.sources[0].serviceAccountToken.expirationSeconds=86400,
  all=>all[2].spec.volumes[0].projected.sources.push({secret:{name:'foreign'}}),
  all=>all[2].spec.volumes.push({name:'foreign',emptyDir:{}}),
  all=>all[2].spec.containers[0].volumeMounts.push({name:'foreign',mountPath:'/foreign'}),
 ];
 for(const mutate of mutations){const changed=injectServiceAccountProjection(resources());mutate(changed);assert.throws(()=>authorityConsumers(changed),/AUTHORITY_CONSUMER_NOT_STABLE/);}
});
test('consumer keeps immutable container fields and declared mounts exact',()=>{
 const source=resources();
 for(const container of source[0].spec.template.spec.containers){
  container.env=[{name:'SAFE_MODE',value:'true'}];container.securityContext={runAsNonRoot:true};container.volumeMounts=[{name:'declared',mountPath:'/declared',readOnly:true}];
 }
 source[0].spec.template.spec.volumes=[{name:'declared',emptyDir:{sizeLimit:'1Mi'}}];
 source[2].spec=structuredClone(source[0].spec.template.spec);
 assert.equal(authorityConsumers(source).length,2);
 for(const mutate of [
  all=>all[2].spec.containers[0].command=['/bin/false'],
  all=>all[2].spec.containers[0].env[0].value='false',
  all=>all[2].spec.containers[0].image='registry.invalid/foreign@sha256:'+'f'.repeat(64),
  all=>all[2].spec.containers[0].securityContext.runAsNonRoot=false,
  all=>all[2].spec.containers[0].volumeMounts[0].readOnly=false,
  all=>all[2].spec.containers[0].volumeMounts.push({name:'declared',mountPath:'/declared'}),
  all=>all[2].spec.volumes[0].emptyDir.sizeLimit='2Mi',
  all=>all[2].spec.volumes.push({name:'declared',emptyDir:{}}),
 ]) {const changed=structuredClone(source);mutate(changed);assert.throws(()=>authorityConsumers(changed),/AUTHORITY_CONSUMER_NOT_STABLE/);}
});
test('image verifier included while unchanged worker writer excluded',()=>{
 const all=resources(),c=all[0].spec.template.spec.containers[1];c.command=['/usr/local/bin/internal-rpc-authority-verifier'];c.args=[];
 assert.equal(authorityConsumers(all).find(item=>item.role==='verifier').role,'verifier');
 c.command=['/usr/local/bin/internal-rpc-authority-platform-worker-grant-agent'];assert.throws(()=>authorityConsumers(all),/UNKNOWN_AUTHORITY_CONSUMER_COMMAND|REGISTERED_AUTHORITY_CONSUMER_MISSING/);
});
