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
test('image verifier included while unchanged worker writer excluded',()=>{
 const all=resources(),c=all[0].spec.template.spec.containers[1];c.command=['/usr/local/bin/internal-rpc-authority-verifier'];c.args=[];
 assert.equal(authorityConsumers(all).find(item=>item.role==='verifier').role,'verifier');
 c.command=['/usr/local/bin/internal-rpc-authority-platform-worker-grant-agent'];assert.throws(()=>authorityConsumers(all),/UNKNOWN_AUTHORITY_CONSUMER_COMMAND|REGISTERED_AUTHORITY_CONSUMER_MISSING/);
});
