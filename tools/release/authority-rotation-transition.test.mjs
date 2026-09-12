import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import test from 'node:test';
import {fileURLToPath} from 'node:url';
import {canonicalRegistryMatches,classifyRotationStatus,createCanonicalRotationTemplate,createPublisherRestart,createRegistryCAS,createRotationJob,createSourceRotationTemplate,deriveCanonicalRegistryAdvance,deriveRegistryRotationIdentity,deriveRotationOperationID,rotationPrerequisiteSpecSHA256,validateRotationPolicy,validateRotationPrerequisites,validateSourceMigrationPrerequisite,verifyRotationJobReadback} from './authority-rotation-transition.mjs';
import {authorityGoImage,buildSourceMigrationReceipt,createSourceMigrationJob} from './authority-rotation-source-delivery-model.mjs';
import {fingerprint} from './scoped-release.mjs';

const image='ghcr.io/codex-k8s/kodex/internal-rpc-authority@sha256:'+'a'.repeat(64);
const source=fileURLToPath(new URL('../..',import.meta.url)).replace(/\/$/,'');
function template(){return createCanonicalRotationTemplate(source,image);}
const plan={version:3,intentID:'13900000-0000-4000-8000-000000000002',source:'/srv/kodex-dev/next',revision:'a'.repeat(40),action:'abort',rotation:{intentID:'13900000-0000-4000-8000-000000000003',sourceRevision:2,sourceDigestSHA256:'b'.repeat(64)}};

function prerequisiteFixture(){
	const labels={'app.kubernetes.io/name':'internal-rpc-authority','app.kubernetes.io/component':'migrator'};
	const serviceAccount={kind:'ServiceAccount',metadata:{name:'internal-rpc-authority-migrator',labels},automountServiceAccountToken:false};
	const egressPolicy={kind:'NetworkPolicy',metadata:{name:'internal-rpc-authority-migrator'},spec:{podSelector:{matchLabels:labels},policyTypes:['Ingress','Egress'],ingress:[],egress:[{to:[{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':'kube-system'}},podSelector:{matchLabels:{'k8s-app':'kube-dns'}}}],ports:[{port:53,protocol:'UDP'},{port:53,protocol:'TCP'}]},{to:[{podSelector:{matchLabels:{'app.kubernetes.io/name':'kodex-postgresql'}}}],ports:[{port:5432,protocol:'TCP'}]}]}};
	const ingressPolicy={kind:'NetworkPolicy',metadata:{name:'internal-rpc-authority-postgresql-from-migrator'},spec:{podSelector:{matchLabels:{'app.kubernetes.io/name':'kodex-postgresql'}},policyTypes:['Ingress'],ingress:[{from:[{podSelector:{matchLabels:labels}}],ports:[{port:5432,protocol:'TCP'}]}]}};
	return {serviceAccount,egressPolicy,ingressPolicy};
}

test('deny-ingress prerequisite normalizes only omitted ingress',()=>{
	const explicit=prerequisiteFixture(),omitted=structuredClone(explicit);delete omitted.egressPolicy.spec.ingress;
	validateRotationPrerequisites(explicit.serviceAccount,explicit.egressPolicy,explicit.ingressPolicy);validateRotationPrerequisites(omitted.serviceAccount,omitted.egressPolicy,omitted.ingressPolicy);
	assert.equal(rotationPrerequisiteSpecSHA256(explicit.egressPolicy),rotationPrerequisiteSpecSHA256(omitted.egressPolicy));
	const mutations=[
		value=>value.egressPolicy.spec.ingress=null,
		value=>value.egressPolicy.spec.ingress={},
		value=>value.egressPolicy.spec.ingress=[{}],
		value=>value.egressPolicy.spec.unknown=true,
		value=>value.egressPolicy.spec.podSelector.matchLabels.unknown='workload',
		value=>value.egressPolicy.spec.egress[0].to[0].podSelector.matchLabels['k8s-app']='foreign-dns',
		value=>value.egressPolicy.spec.egress[0].ports[0].port=54,
		value=>value.egressPolicy.spec.egress[0].unknown=true,
		value=>value.egressPolicy.spec.egress[0].to.push({podSelector:{}}),
		value=>value.ingressPolicy.spec.ingress[0].from[0].podSelector.matchLabels.unknown='caller',
	];
	for(const mutate of mutations){const changed=structuredClone(omitted);mutate(changed);assert.throws(()=>validateRotationPrerequisites(changed.serviceAccount,changed.egressPolicy,changed.ingressPolicy),/ROTATION_JOB_PREREQUISITES_REJECTED/);}
});

test('rendered ingress-only prerequisite accepts only empty egress omitted by API',()=>{
 const live=prerequisiteFixture(),rendered=structuredClone(live);rendered.ingressPolicy.spec.egress=[];
 validateRotationPrerequisites(rendered.serviceAccount,rendered.egressPolicy,rendered.ingressPolicy);
 assert.equal(rotationPrerequisiteSpecSHA256(live.ingressPolicy),rotationPrerequisiteSpecSHA256(rendered.ingressPolicy));
 assert.deepEqual(rendered.ingressPolicy.spec.egress,[]);
 for(const mutate of [value=>value.spec.egress=null,value=>value.spec.egress={},value=>value.spec.egress=[{}],value=>value.spec.policyTypes.push('Egress'),value=>value.spec.unknown=true,value=>value.spec.ingress[0].ports[0].port=5433]){
  const changed=structuredClone(rendered.ingressPolicy);mutate(changed);
  assert.throws(()=>validateRotationPrerequisites(rendered.serviceAccount,rendered.egressPolicy,changed),/ROTATION_JOB_PREREQUISITES_REJECTED/);
 }
});

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
test('source rotation job uses exact offline Go wrapper and target source',()=>{
 const cache=name=>({name,hostPath:{path:`/srv/kodex-dev/cache/${name}`,type:'Directory'}}),mount=(name,mountPath,readOnly)=>({name,mountPath,readOnly});
 const env=Object.entries({INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE:'/var/run/secrets/kodex/internal-rpc-authority/postgres/dsn',INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME:'internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local',GOMODCACHE:'/go/pkg/mod',GOCACHE:'/go/build-cache/cache',GOWORK:'off',GOTOOLCHAIN:'local',GOTMPDIR:'/go/build-cache/tmp',HOME:'/go/build-cache/home'}).map(([name,value])=>({name,value}));
 const rendered={kind:'Job',metadata:{name:'internal-rpc-authority-migrate',namespace:'kodex-system'},spec:{template:{metadata:{labels:{'app.kubernetes.io/name':'internal-rpc-authority','app.kubernetes.io/component':'migrator'}},spec:{restartPolicy:'Never',serviceAccountName:'internal-rpc-authority-migrator',automountServiceAccountToken:false,containers:[{name:'migrate',image:authorityGoImage,workingDir:'/workspace/services/internal/internal-rpc-authority',command:['/workspace/tools/dev/run-go-command.sh'],args:['services/internal/internal-rpc-authority','./cmd/cli','up'],env,volumeMounts:[mount('dev-source','/workspace',true),mount('dev-go-mod','/go/pkg/mod',true),mount('dev-go-sumdb','/go/pkg/sumdb',true),mount('dev-go-tools','/go/tools',true),mount('dev-build-migrate','/go/build-cache',false),mount('tmp','/tmp',false),mount('postgresql-credentials','/var/run/secrets/kodex/internal-rpc-authority/postgres',true),mount('postgresql-ca','/var/run/config/kodex/internal-rpc-authority/postgresql',true)]}],volumes:[{name:'dev-source',hostPath:{path:'/srv/kodex-dev/old',type:'Directory'}},cache('dev-go-mod'),cache('dev-go-sumdb'),cache('dev-go-tools'),cache('dev-build-migrate'),{name:'tmp',emptyDir:{}},{name:'postgresql-credentials',secret:{secretName:'internal-rpc-authority-postgres-migration'}},{name:'postgresql-ca',configMap:{name:'internal-rpc-authority-postgresql-ca'}}]}}}};
 const rotate={...plan,action:'rotate',ownerOperationID:'14340000-0000-5000-8000-000000000009'};const job=createRotationJob(createSourceRotationTemplate(rendered,plan.source),rotate);
 assert.deepEqual(job.spec.template.spec.containers[0].args,['services/internal/internal-rpc-authority','./cmd/cli','rotation-watch','--operation-id',rotate.ownerOperationID]);assert.equal(job.spec.template.spec.volumes.find(v=>v.name==='dev-source').hostPath.path,plan.source);
 const delivery={version:1,kind:'AUTHORITY_SOURCE_DELIVERY',intentID:'14340000-0000-4000-8000-000000000002',source:plan.source,revision:plan.revision,namespaceUID:'14340000-0000-4000-8000-000000000003',publisher:{uid:'publisher',beforeSpecSHA256:'a',desiredSpecSHA256:'b',beforeAnnotations:{},beforeAnnotationsPresent:false},migration:{rendered,renderedSpecSHA256:'c'},boundary:{registryDataSHA256:'d',neighborsSHA256:'e'}};
 const migration=createSourceMigrationJob(rendered,delivery),actual=structuredClone(migration);actual.metadata.uid='14340000-0000-4000-8000-000000000004';actual.status={succeeded:1};const receipt=buildSourceMigrationReceipt(actual,migration,delivery);
 validateSourceMigrationPrerequisite(delivery,receipt,actual);const replacement=structuredClone(actual);replacement.metadata.uid='14340000-0000-4000-8000-000000000005';assert.throws(()=>validateSourceMigrationPrerequisite(delivery,receipt,replacement),/SOURCE_MIGRATION_REPLACED/);
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
	const nextPolicy=readFileSync(`${source}/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json`,'utf8');
	const previousPolicy=JSON.parse(nextPolicy);previousPolicy.policy_revision=76;previousPolicy.policy.operation_bindings.find(value=>value.operation_id==='platform.command.integration-definitions.create-draft').project_required=true;const currentPolicy=JSON.stringify(previousPolicy);
	const previousIdentity=deriveRegistryRotationIdentity(currentRegistry),identity=deriveRegistryRotationIdentity(nextRegistry);
	const rotate={...plan,action:'rotate',ownerOperationID:identity.ownerOperationID,registry:{uid:'13900000-0000-4000-8000-000000000010',resourceVersion:'10',currentDataSHA256:'',desiredData:{'key-delivery-targets.yaml':nextRegistry,'authority-policy.json':nextPolicy},desiredDataSHA256:'',previousSourceRevision:previousIdentity.sourceRevision,previousSourceDigestSHA256:previousIdentity.sourceDigestSHA256,sourceRevision:identity.sourceRevision,sourceDigestSHA256:identity.sourceDigestSHA256,previousPolicyRevision:76,previousPolicySHA256:createHash('sha256').update(currentPolicy).digest('hex'),policySHA256:'763028a7176c8c3394d0a01686b8d66a3a7cc465af90c2480a06064816b5e504'},publisher:{uid:'13900000-0000-4000-8000-000000000011',resourceVersion:'11',specSHA256:'',image,command:['/usr/local/bin/internal-rpc-authority-publisher']}};
 const registry={apiVersion:'v1',kind:'ConfigMap',metadata:{uid:rotate.registry.uid,resourceVersion:'10'},data:{'key-delivery-targets.yaml':currentRegistry,'authority-policy.json':currentPolicy}};
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

test('canonical registry advances live revision with exactly base web-only targets and policy 77',()=>{
 const canonical=readFileSync(`${source}/deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml`,'utf8'),live=canonical.replace('source_revision: 7','source_revision: 18');
 const next=deriveCanonicalRegistryAdvance(live,canonical);assert.equal(next.identity.sourceRevision,19);assert.equal(canonicalRegistryMatches(next.raw,canonical),true);assert.equal(next.raw.includes('interaction-gateway'),false);
 const policy=readFileSync(`${source}/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json`,'utf8');assert.deepEqual(validateRotationPolicy(policy),{revision:77,sha256:'763028a7176c8c3394d0a01686b8d66a3a7cc465af90c2480a06064816b5e504'});
 const binding=JSON.parse(policy).policy.operation_bindings.filter(value=>value.operation_id==='platform.command.integration-definitions.create-draft');assert.equal(binding.length,1);assert.equal(binding[0].project_required,false);
});

test('rotation readback is closed and contains no payload',()=>{
 const state={observedAt:new Date().toISOString(),intentId:plan.rotation.intentID,protocolVersion:2,sourceRevision:2,sourceDigestSHA256:plan.rotation.sourceDigestSHA256,status:'DELIVERED'};
 assert.deepEqual(classifyRotationStatus({status:{succeeded:1}},JSON.stringify(state)),{phase:'SUCCEEDED',state});
 assert.throws(()=>classifyRotationStatus({status:{succeeded:1}},JSON.stringify({...state,status:'UNKNOWN'})),/ROTATION_READBACK_REQUIRED/);
 const operation={observedAt:new Date().toISOString(),operationId:deriveRotationOperationID(8,'c'.repeat(64)),operationStatus:'WAITING_SWITCH',status:'WAITING_SWITCH',registryRevision:8,baseRevision:7,registrySourceDigestSHA256:'c'.repeat(64),baseDigestSHA256:'d'.repeat(64),phase:'DISTRIBUTE',switchNotBefore:new Date(Date.now()+30_000).toISOString()};
 assert.equal(classifyRotationStatus({status:{succeeded:1}},JSON.stringify(operation)).state.operationStatus,'WAITING_SWITCH');
 assert.throws(()=>classifyRotationStatus({status:{succeeded:1}},JSON.stringify({...operation,switchNotBefore:undefined})),/ROTATION_OPERATION_DEADLINE_REJECTED/);
});


test('rotation defaults are pinned before hash and foreign readback remains rejected',()=>{
 const job=createRotationJob(template(),{...plan,action:'status'}),pod=job.spec.template.spec;
 assert.equal(job.spec.parallelism,1);assert.equal(job.spec.completions,1);
 assert.equal(job.spec.podReplacementPolicy,'TerminatingOrFailed');
 assert.equal(pod.serviceAccount,pod.serviceAccountName);
 assert.equal(pod.schedulerName,'default-scheduler');
 for(const container of [...pod.containers,...(pod.initContainers??[])]){
  assert.equal(container.imagePullPolicy,'IfNotPresent');assert.equal(container.terminationMessagePolicy,'File');
 }
 verifyRotationJobReadback(structuredClone(job),job);
 for(const mutate of [value=>value.spec.parallelism=2,value=>value.spec.template.spec.containers[0].imagePullPolicy='Always',value=>value.spec.template.spec.initContainers[0].terminationMessagePath='/foreign',value=>value.spec.unknown=true]){
  const changed=structuredClone(job);mutate(changed);assert.throws(()=>verifyRotationJobReadback(changed,job),/ROTATION_JOB_SPEC_DRIFT/);
 }
 const configured=template();configured.spec.parallelism=2;configured.spec.template.spec.dnsPolicy='Default';
 const explicit=createRotationJob(configured,{...plan,action:'status'});
 assert.equal(explicit.spec.parallelism,2);assert.equal(explicit.spec.template.spec.dnsPolicy,'Default');
});
