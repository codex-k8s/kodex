import test from 'node:test';
import assert from 'node:assert/strict';
import {canonicalPostgresBackupSpec,postgresBackupBoundary,canonicalPostgresBackupBoundarySHA256} from './backup-postgres-boundary.mjs';
import {readBackupProducerInventory} from './backup-controller-maintenance.mjs';

const podSpec=()=>{
 const spec=canonicalPostgresBackupSpec();spec.serviceAccountName='default';
 spec.volumes.push({name:'data',persistentVolumeClaim:{claimName:'data-kodex-postgresql-0'}});return spec;
};
test('PostgreSQL exception привязано к canonical Git container, mounts и API defaults',()=>{
 const spec=podSpec();spec.containers[0].terminationMessagePath='/dev/termination-log';
 spec.containers[0].startupProbe.successThreshold=1;spec.containers[0].startupProbe.timeoutSeconds=1;
 assert.equal(postgresBackupBoundary(spec,'Pod'),canonicalPostgresBackupBoundarySHA256());
 spec.containers[0].env.push({name:'OTEL_SDK_DISABLED',value:'true'});
 assert.equal(postgresBackupBoundary(spec,'Pod'),canonicalPostgresBackupBoundarySHA256());
 spec.containers[0].env.push({name:'OTEL_SDK_DISABLED',value:'true'});
 assert.throws(()=>postgresBackupBoundary(spec,'Pod'),/POSTGRES_EXECUTION_CHANGED/);
});
test('тот же PG Pod UID не разрешает extra/init/ephemeral container либо изменение штатного процесса',()=>{
 for(const change of [
  s=>s.ephemeralContainers=[{name:'debug',image:s.containers[0].image,volumeMounts:s.containers[0].volumeMounts}],
  s=>s.initContainers=[{name:'debug'}],s=>s.containers.push({...s.containers[0],name:'debug'}),
  s=>s.containers[0].command=['sh','-c','synthetic'],s=>s.containers[0].args=['-c','synthetic'],
  s=>s.containers[0].image='example.invalid/postgres@sha256:'+'a'.repeat(64),
  s=>s.containers[0].env.push({name:'PGOPTIONS',value:'synthetic'}),
  s=>s.containers[0].lifecycle={postStart:{exec:{command:['synthetic']}}},
  s=>s.containers[0].livenessProbe.exec.command=['synthetic'],s=>s.hostPID=true,
  s=>s.containers[0].volumeMounts.find(v=>v.name==='runtime-credentials').readOnly=false,
  s=>s.volumes.find(v=>v.name==='runtime-credentials').secret.secretName='foreign',
 ]){const spec=podSpec();change(spec);assert.throws(()=>postgresBackupBoundary(spec,'Pod'),/POSTGRES_EXECUTION_CHANGED/);}
});
test('namespace discovery проверяет PG execution до выдачи metadata-only proof',()=>{
 const uid='11111111-1111-1111-1111-111111111111';
 const row=['Pod','kodex-postgresql-0',uid,'kodex-system','kodex-postgresql','[]','default','kodex-postgresql-runtime-credentials','','','','','','',''].join('\t')+'\n';
 const pod={kind:'Pod',metadata:{name:'kodex-postgresql-0',uid,namespace:'kodex-system'},spec:podSpec()};
 const calls=[];const kube=args=>{calls.push(args);return args.at(-1)==='json'?JSON.stringify(pod):row;};
 const proof=readBackupProducerInventory(kube);
 assert.equal(proof.items[0].postgresBoundarySHA256,canonicalPostgresBackupBoundarySHA256());
 assert.equal(JSON.stringify(proof).includes('POSTGRES_PASSWORD_FILE'),false);assert.equal(calls.length,2);
 pod.spec.ephemeralContainers=[{name:'debug',targetContainerName:'postgresql',volumeMounts:pod.spec.containers[0].volumeMounts}];
 assert.throws(()=>readBackupProducerInventory(kube),/POSTGRES_EXECUTION_CHANGED/);
});
