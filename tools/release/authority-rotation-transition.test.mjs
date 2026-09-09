import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import test from 'node:test';
import {fileURLToPath} from 'node:url';
import {classifyRotationStatus,createCanonicalRotationTemplate,createPublisherRestart,createRegistryCAS,createRotationJob,deriveRegistryRotationIdentity,deriveRotationOperationID,validateRotationPrerequisites,verifyRotationJobReadback} from './authority-rotation-transition.mjs';
import {fingerprint} from './scoped-release.mjs';

const image='ghcr.io/codex-k8s/kodex/internal-rpc-authority@sha256:'+'a'.repeat(64);
const source=fileURLToPath(new URL('../..',import.meta.url)).replace(/\/$/,'');
function template(){return createCanonicalRotationTemplate(source,image);}
const plan={version:3,intentID:'13900000-0000-4000-8000-000000000002',source:'/srv/kodex-dev/next',revision:'a'.repeat(40),action:'abort',rotation:{intentID:'13900000-0000-4000-8000-000000000003',sourceRevision:2,sourceDigestSHA256:'b'.repeat(64)}};

test('owner operation identity matches publisher derivation across restart and later registry revision',()=>{
	const digest='c'.repeat(64),operationID=deriveRotationOperationID(8,digest);
	assert.equal(operationID,'13657d5a-4e35-52fc-b82d-0529ee0914ca');
	assert.equal(deriveRotationOperationID(8,digest),operationID);
	assert.notEqual(deriveRotationOperationID(9,digest),operationID);
});

test('exact registry bytes bind plan owner identity to watch',()=>{
	const raw='version: 1\nsource_revision: 8\ntargets: []\n';
	const identity=deriveRegistryRotationIdentity(raw);
	assert.equal(identity.sourceRevision,8);
	assert.equal(identity.ownerOperationID,deriveRotationOperationID(8,identity.sourceDigestSHA256));
	const rotate={...plan,action:'rotate',ownerOperationID:identity.ownerOperationID};
	assert.deepEqual(createRotationJob(template(),rotate).spec.template.spec.containers[0].args,
		['rotation-watch','--operation-id',identity.ownerOperationID]);
});

test('rotation job pins a safe pre-delivery abort',()=>{
 const job=createRotationJob(template(),plan);assert.deepEqual(job.spec.template.spec.containers[0].args,['rotation-abort','--intent-id',plan.rotation.intentID,'--source-revision','2','--source-digest-sha256',plan.rotation.sourceDigestSHA256,'--confirm','ABORT-STAGING-AUTHORITY-ROTATION']);
 const actual=structuredClone(job);actual.metadata.uid='13900000-0000-4000-8000-000000000004';actual.spec.selector={matchLabels:{controller:'generated'}};verifyRotationJobReadback(actual,job);
});

test('rotation job reuses exact rendered migrator authority and network paths',()=>{
	const rendered=execFileSync('kubectl',['kustomize',`${source}/deploy/k8s/profiles/web-with-mattermost`],{encoding:'utf8',maxBuffer:16<<20});
	const resources=JSON.parse(execFileSync('yq',['eval-all','-o=json','[.]','-'],{input:rendered,encoding:'utf8',maxBuffer:16<<20}));
	const find=(kind,name)=>resources.find(value=>value.kind===kind&&value.metadata?.name===name);
	validateRotationPrerequisites(find('ServiceAccount','internal-rpc-authority-migrator'),find('NetworkPolicy','internal-rpc-authority-migrator'),find('NetworkPolicy','internal-rpc-authority-postgresql-from-migrator'));
	const pod=template().spec.template.spec;
	assert.equal(pod.initContainers[0].image,'docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7');
	assert.equal(pod.containers[0].image,image);
	assert.deepEqual(template().spec.template.metadata.labels,{'app.kubernetes.io/name':'internal-rpc-authority','app.kubernetes.io/component':'migrator'});
});

test('rotation CAS changes only registry and publisher pod template',()=>{
 const currentRegistry='version: 1\nsource_revision: 7\ntargets: []\n',nextRegistry='version: 1\nsource_revision: 8\ntargets: []\n';
	const previousIdentity=deriveRegistryRotationIdentity(currentRegistry),identity=deriveRegistryRotationIdentity(nextRegistry);
	const rotate={...plan,action:'rotate',ownerOperationID:identity.ownerOperationID,registry:{uid:'13900000-0000-4000-8000-000000000010',resourceVersion:'10',currentDataSHA256:'',desiredData:{'key-delivery-targets.yaml':nextRegistry},desiredDataSHA256:'',previousSourceRevision:previousIdentity.sourceRevision,previousSourceDigestSHA256:previousIdentity.sourceDigestSHA256,sourceRevision:identity.sourceRevision,sourceDigestSHA256:identity.sourceDigestSHA256},publisher:{uid:'13900000-0000-4000-8000-000000000011',resourceVersion:'11',specSHA256:'',image,command:['/usr/local/bin/internal-rpc-authority-publisher']}};
 const registry={apiVersion:'v1',kind:'ConfigMap',metadata:{uid:rotate.registry.uid,resourceVersion:'10'},data:{'key-delivery-targets.yaml':currentRegistry}};
 rotate.registry.currentDataSHA256=fingerprint(registry.data);rotate.registry.desiredDataSHA256=fingerprint(rotate.registry.desiredData);
 const updatedRegistry=createRegistryCAS(registry,rotate);assert.equal(updatedRegistry.data['key-delivery-targets.yaml'],nextRegistry);assert.equal(updatedRegistry.metadata.resourceVersion,'10');
	assert.throws(()=>createRegistryCAS({...registry,data:{'key-delivery-targets.yaml':nextRegistry}},rotate),/ROTATION_REGISTRY_CAS_DRIFT/);
	const job=createRotationJob(template(),rotate);
	assert.deepEqual(job.spec.template.spec.containers[0].args,['rotation-watch','--operation-id',rotate.ownerOperationID]);
	assert.equal(job.metadata.name,`authority-rotation-${rotate.intentID}`);
	assert.equal(job.metadata.annotations['kodex.dev/rotation-intent'],rotate.intentID);
	assert.equal(job.metadata.annotations['kodex.dev/rotation-operation'],rotate.ownerOperationID);
 const deployment={apiVersion:'apps/v1',kind:'Deployment',metadata:{uid:rotate.publisher.uid,resourceVersion:'11'},spec:{template:{metadata:{},spec:{containers:[{name:'publisher',image,command:rotate.publisher.command}]}}}};
 rotate.publisher.specSHA256=fingerprint(deployment.spec);const restarted=createPublisherRestart(deployment,rotate);rotate.publisher.desiredSpecSHA256=fingerprint(restarted.spec);
 assert.equal(restarted.spec.template.metadata.annotations['kodex.dev/authority-rotation-intent'],rotate.intentID);
 assert.equal(restarted.spec.template.metadata.annotations['kodex.dev/authority-rotation-operation'],rotate.ownerOperationID);assert.deepEqual(restarted.spec.template.spec.containers,deployment.spec.template.spec.containers);
});

test('rotation readback is closed and contains no payload',()=>{
 const state={observedAt:new Date().toISOString(),intentId:plan.rotation.intentID,protocolVersion:2,sourceRevision:2,sourceDigestSHA256:plan.rotation.sourceDigestSHA256,status:'DELIVERED'};
 assert.deepEqual(classifyRotationStatus({status:{succeeded:1}},JSON.stringify(state)),{phase:'SUCCEEDED',state});
 assert.throws(()=>classifyRotationStatus({status:{succeeded:1}},JSON.stringify({...state,status:'UNKNOWN'})),/ROTATION_READBACK_REQUIRED/);
 const operation={observedAt:new Date().toISOString(),operationId:deriveRotationOperationID(8,'c'.repeat(64)),operationStatus:'WAITING_SWITCH',status:'WAITING_SWITCH',registryRevision:8,baseRevision:7,registrySourceDigestSHA256:'c'.repeat(64),baseDigestSHA256:'d'.repeat(64),phase:'DISTRIBUTE',switchNotBefore:new Date(Date.now()+30_000).toISOString()};
 assert.equal(classifyRotationStatus({status:{succeeded:1}},JSON.stringify(operation)).state.operationStatus,'WAITING_SWITCH');
 assert.throws(()=>classifyRotationStatus({status:{succeeded:1}},JSON.stringify({...operation,switchNotBefore:undefined})),/ROTATION_OPERATION_DEADLINE_REJECTED/);
});
