import assert from 'node:assert/strict';
import test from 'node:test';
import {classifyRotationStatus,createRotationJob,verifyRotationJobReadback} from './authority-rotation-transition.mjs';

function template(){return {apiVersion:'batch/v1',kind:'Job',metadata:{name:'internal-rpc-authority-migrate',namespace:'kodex-system',uid:'13900000-0000-4000-8000-000000000001',resourceVersion:'42'},status:{succeeded:1},spec:{template:{metadata:{labels:{}},spec:{restartPolicy:'Never',serviceAccountName:'internal-rpc-authority-migrator',automountServiceAccountToken:false,containers:[{name:'migrate',image:'docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83',workingDir:'/workspace/services/internal/internal-rpc-authority',command:['/workspace/tools/dev/run-go-command.sh'],args:['services/internal/internal-rpc-authority','./cmd/cli','up'],env:[{name:'INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE',value:'redacted'},{name:'INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME',value:'postgres'}],volumeMounts:[{name:'dev-source',mountPath:'/workspace',readOnly:true},{name:'postgresql-credentials',mountPath:'/var/run/secrets/kodex/internal-rpc-authority/postgres',readOnly:true}]}],volumes:[{name:'dev-source',hostPath:{path:'/srv/kodex-dev/current',type:'Directory'}},{name:'postgresql-credentials',secret:{secretName:'internal-rpc-authority-postgres-migration'}}]}}}};}
const plan={version:1,operationID:'13900000-0000-4000-8000-000000000002',source:'/srv/kodex-dev/next',revision:'a'.repeat(40),action:'abort',rotation:{intentID:'13900000-0000-4000-8000-000000000003',sourceRevision:2,sourceDigestSHA256:'b'.repeat(64)}};

test('rotation job pins a safe pre-delivery abort',()=>{
 const job=createRotationJob(template(),plan);assert.deepEqual(job.spec.template.spec.containers[0].args.slice(2),['rotation-abort','--intent-id',plan.rotation.intentID,'--source-revision','2','--source-digest-sha256',plan.rotation.sourceDigestSHA256,'--confirm','ABORT-STAGING-AUTHORITY-ROTATION']);
 const actual=structuredClone(job);actual.metadata.uid='13900000-0000-4000-8000-000000000004';actual.spec.selector={matchLabels:{controller:'generated'}};verifyRotationJobReadback(actual,job);
});

test('rotation readback is closed and contains no payload',()=>{
 const state={observedAt:new Date().toISOString(),intentId:plan.rotation.intentID,protocolVersion:2,sourceRevision:2,sourceDigestSHA256:plan.rotation.sourceDigestSHA256,status:'DELIVERED'};
 assert.deepEqual(classifyRotationStatus({status:{succeeded:1}},JSON.stringify(state)),{phase:'SUCCEEDED',state});
 assert.throws(()=>classifyRotationStatus({status:{succeeded:1}},JSON.stringify({...state,status:'UNKNOWN'})),/ROTATION_READBACK_REQUIRED/);
});
