import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,readFileSync,rmSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {fingerprint} from './scoped-release.mjs';
import {validateHoldCapability} from './image-admission-hold-capability.mjs';
import {deletionRequest,executePhase,executeRollbackPhase,inspectHostController,loadDesiredBundle,verifyControllerRuntime} from './image-admission-hold-delivery.mjs';
import {admissionResourceWithAPIDefaults,buildDeliveryPlan,inspectPhase,mutationFor,phaseAfter,phases,rollbackPhases,validateDeliveryPlan} from './image-admission-hold-delivery-model.mjs';

const oldImage=`registry.local.kodex/kodex/image-admission@sha256:${'1'.repeat(64)}`;
const nextImage=`registry.local.kodex/kodex/image-admission@sha256:${'2'.repeat(64)}`;
const executable='3'.repeat(64),context='k3s-staging';
let sequence=10;
const nextUID=()=>`${String(sequence++).padStart(8,'0')}-1111-4111-8111-111111111111`;
const metadata=(name,namespaced=true)=>({name,...(namespaced?{namespace:'kodex-system'}:{}),uid:nextUID(),resourceVersion:String(sequence++),
 labels:namespaced?{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging','kodex.dev/local-profile':'hot-reload'}:undefined});

function bundle() {
 const predecessorSpec={failurePolicy:'Fail',matchConstraints:{resourceRules:[{operations:['CREATE'],resources:['jobs'],scope:'Namespaced'}]},variables:[{name:'protected',expression:'old'}],validations:[{message:'Image admission Job lifecycle differs from the bounded contract.',expression:"object.name == 'a b' &&\n old"}]};
 const jobsSpec={failurePolicy:'Fail',matchConstraints:{resourceRules:[{operations:['CREATE'],resources:['jobs'],scope:'Namespaced'}]},variables:[{name:'protected',expression:'old'},{name:'proofHeld',expression:'held'}],validations:[
  {message:'Image admission Job lifecycle differs from the bounded contract.',expression:"object.name == 'a b' &&\n held lifecycle"},
  {message:'Image admission executable proof reservation is invalid.',expression:'reservation'},
 ]};
 const policy=(name,spec)=>({apiVersion:'admissionregistration.k8s.io/v1',kind:'ValidatingAdmissionPolicy',metadata:{name},spec});
 const predecessorJobsPolicy=policy('kodex-image-admission-controller-jobs',predecessorSpec),jobsPolicy=policy('kodex-image-admission-controller-jobs',jobsSpec);
 const releasePolicy={apiVersion:'admissionregistration.k8s.io/v1',kind:'ValidatingAdmissionPolicy',metadata:{name:'kodex-image-admission-proof-release'},
   spec:{failurePolicy:'Fail',matchConstraints:{resourceRules:[{operations:['UPDATE'],resources:['jobs']}]},validations:[{expression:'exact release'}]}},
  releaseBinding={apiVersion:'admissionregistration.k8s.io/v1',kind:'ValidatingAdmissionPolicyBinding',metadata:{name:'kodex-image-admission-proof-release'},
   spec:{policyName:'kodex-image-admission-proof-release',validationActions:['Deny'],matchResources:{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':'kodex-system'}}}}};
 return {version:1,revision:'4'.repeat(40),source:'/srv/kodex-dev/source',predecessorJobsPolicy,
  predecessorJobsPolicyRendered:{...structuredClone(predecessorJobsPolicy),spec:{...structuredClone(predecessorSpec),validations:[{...predecessorSpec.validations[0],expression:"object.name == 'a b' &&\n\n old"}]}},
  jobsPolicy,jobsPolicyRendered:structuredClone(jobsPolicy),releasePolicy,releasePolicyRendered:structuredClone(releasePolicy),
  releaseBinding,releaseBindingRendered:structuredClone(releaseBinding),
  controllerHoldEnvironment:[{name:'IMAGE_ADMISSION_CONTROLLER_HOLD_PROOF_JOBS',value:'false'},
   {name:'IMAGE_ADMISSION_CONTROLLER_PROOF_HOLD_UNTIL',value:'1970-01-01T00:00:00Z'}]};
}

function controller() {
 const meta=metadata('image-admission-controller'),container={name:'image-admission-controller',image:oldImage,
  command:['/usr/local/bin/image-admission-controller'],env:[{name:'IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP',value:'kodex-image-admission-policy'}]};
 return {apiVersion:'apps/v1',kind:'Deployment',metadata:{...meta,generation:1},spec:{replicas:1,strategy:{type:'Recreate'},template:{spec:{containers:[container]}},selector:{matchLabels:{app:'controller'}}},
  status:{observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1}};
}

function podFor(deployment,imageID=deployment.spec.template.spec.containers[0].image) {
 const container=structuredClone(deployment.spec.template.spec.containers[0]);
 return {apiVersion:'v1',kind:'Pod',metadata:{name:'image-admission-controller-pod',namespace:'kodex-system',uid:nextUID(),
  ownerReferences:[{uid:deployment.metadata.uid,controller:true}]},spec:{containers:[container]},status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],
  containerStatuses:[{name:'image-admission-controller',ready:true,state:{running:{}},containerID:`containerd://${'5'.repeat(64)}`,imageID,restartCount:0}]}};
}

function snapshot() {
 const desired=bundle(),deployment=controller(),policyName='kodex-image-admission-policy';
 const jobsPolicy=admissionResourceWithAPIDefaults({apiVersion:'admissionregistration.k8s.io/v1',kind:'ValidatingAdmissionPolicy',metadata:metadata('kodex-image-admission-controller-jobs',false),spec:structuredClone(desired.predecessorJobsPolicy.spec)});
 const jobsBinding={apiVersion:'admissionregistration.k8s.io/v1',kind:'ValidatingAdmissionPolicyBinding',metadata:metadata('kodex-image-admission-controller-jobs',false),
  spec:{policyName:'kodex-image-admission-controller-jobs',validationActions:['Deny'],paramRef:{name:policyName,namespace:'kodex-system',parameterNotFoundAction:'Deny'}}};
 const parameters={apiVersion:'supplychain.kodex.dev/v1alpha1',kind:'ImageAdmissionPolicyParameters',metadata:metadata(policyName),spec:{policyRevision:'18',policySHA256:'6'.repeat(64)}};
 const policyConfig={apiVersion:'v1',kind:'ConfigMap',metadata:metadata(policyName),immutable:true,data:structuredClone(parameters.spec)};
 const neighbor={apiVersion:'apps/v1',kind:'Deployment',metadata:metadata('control-plane'),spec:{replicas:2,strategy:{type:'RollingUpdate'}}};
 return {clusterUID:nextUID(),namespaceUID:nextUID(),namespace:{metadata:{labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging'}}},
  controller:deployment,controllerPods:[podFor(deployment)],deployments:[deployment,neighbor],jobs:[],pvcs:[],jobsPolicy,jobsBinding,parameters,policyConfig,
  releasePolicy:null,releaseBinding:null,controllerExecutableSHA256:'6'.repeat(64),ownerState:{at:new Date().toISOString(),openBuilds:0,pendingAdmissions:0,pendingPromotions:0,activeRuntimeRuns:0,
   claimedRuntimeLeases:0,promotedArtifactCount:2,promotedPinsSHA256:'7'.repeat(64)}};
}

function planAndState() {
 const state=snapshot(),desired=bundle(),capability={version:1,profile:'image-admission-hold-delivery',revision:desired.revision,go:'go1.26.6',image:nextImage,executableSHA256:executable,
  recipe:'CGO_ENABLED=0 GOWORK=off GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -buildvcs=false -ldflags="-s -w" ./cmd/image-admission-controller'},
  plan=buildDeliveryPlan(state,desired,{context,k3sSudo:true,capability,
  intent:'88888888-8888-4888-8888-888888888888'});return {state,desired,plan};
}

test('capability schema binds the exact source, image, executable and build recipe',()=>{
 const {plan}=planAndState();
 assert.equal(validateHoldCapability(plan.capability),plan.capability);
 for(const changed of [
  {...plan.capability,extra:true},
  {...plan.capability,image:`registry.local.kodex/kodex/other@sha256:${'2'.repeat(64)}`},
  {...plan.capability,go:'go1.26.5'},
  {...plan.capability,recipe:'go build ./cmd/image-admission-controller'},
 ])assert.throws(()=>validateHoldCapability(changed),/INVALID_HOLD_DELIVERY_CAPABILITY/);
});

test('policy rollback DELETE carries exact Kubernetes UID and resourceVersion preconditions',()=>{
 const request=deletionRequest('ValidatingAdmissionPolicyBinding','kodex-image-admission-proof-release','12345678-1111-4111-8111-111111111111','77');
 assert.equal(request.path,'/apis/admissionregistration.k8s.io/v1/validatingadmissionpolicybindings/kodex-image-admission-proof-release');
 assert.deepEqual(request.body,{apiVersion:'v1',kind:'DeleteOptions',propagationPolicy:'Foreground',
  preconditions:{uid:'12345678-1111-4111-8111-111111111111',resourceVersion:'77'}});
 assert.throws(()=>deletionRequest('Deployment','foreign','uid','1'),/KUBERNETES_CAS_DELETE_INPUT_REQUIRED/);
});

function setControllerSpec(state,spec) {
 state.controller.spec=structuredClone(spec);state.controller.metadata.resourceVersion=String(Number(state.controller.metadata.resourceVersion)+1);
 state.controller.metadata.generation++;state.controller.status.observedGeneration=state.controller.metadata.generation;
 state.controllerPods=[podFor(state.controller)];
}

test('exact source materializes the approved #1381 delta',()=>{
 const source=process.cwd(),revisionValue=process.env.KODEX_TEST_HEAD;
 if(!revisionValue)return;
 const directory=mkdtempSync(join(tmpdir(),'hold-source-')),clone=join(directory,'source');
 try {
  execFileSync('git',['clone','--local','--no-hardlinks','--no-checkout',source,clone],{stdio:'pipe'});
  execFileSync('git',['-C',clone,'remote','set-url','origin','https://github.com/codex-k8s/kodex.git'],{stdio:'pipe'});
  execFileSync('git',['-C',clone,'checkout','--detach',revisionValue],{stdio:'pipe'});
  const result=loadDesiredBundle(clone,revisionValue);
  assert.equal(result.revision,revisionValue);assert.equal(result.releasePolicy.metadata.name,'kodex-image-admission-proof-release');
  assert.equal(result.jobsPolicy.spec.variables.filter(item=>item.name==='proofHeld').length,1);
  const rendered=execFileSync('kubectl',['kustomize',join(clone,'deploy/k8s/overlays/staging/image-supply-chain')],{encoding:'utf8',stdio:'pipe'}),
   documents=execFileSync('yq',['-o=json','-I=0','.','-'],{input:rendered,encoding:'utf8',stdio:'pipe'}).trim().split('\n').map(JSON.parse),
   renderedJobs=documents.find(item=>item.kind==='ValidatingAdmissionPolicy'&&item.metadata?.name==='kodex-image-admission-controller-jobs');
  assert.equal(fingerprint(renderedJobs.spec),fingerprint(result.jobsPolicyRendered.spec));
  const changed=result.predecessorJobsPolicy.spec.validations.map((item,index)=>
   item.expression===result.predecessorJobsPolicyRendered.spec.validations[index].expression?null:index).filter(index=>index!==null);
  assert.deepEqual(changed,[0,4,5,6,7,8,9,10]);
  assert.equal(result.version,2);
  // Readback #1421 от 2026-09-09: сравнивается весь spec, а не только CEL-фрагмент.
  const transitioned=admissionResourceWithAPIDefaults(result.predecessorJobsPolicyTransitionedRendered);
  assert.equal(fingerprint(transitioned.spec),'315bf2fdd46347af9695915052c54f0e0505f66d06d382e23ceb00a5544ac2e5');
  assert.notEqual(fingerprint(transitioned.spec),fingerprint(admissionResourceWithAPIDefaults(result.predecessorJobsPolicyRendered).spec));
  const fixture=planAndState();fixture.state.jobsPolicy.spec=structuredClone(transitioned.spec);
  const options={context,k3sSudo:true,capability:{...fixture.plan.capability,revision:revisionValue},intent:'77777777-7777-4777-8777-777777777777'};
  const plan=buildDeliveryPlan(fixture.state,result,options);
  assert.deepEqual(plan.phaseTargets['policy-jobs'].before,transitioned.spec);
  assert.deepEqual(plan.rollbackTargets['rollback-policy-jobs'].after,transitioned.spec);
  validateDeliveryPlan(JSON.parse(JSON.stringify(plan)),context,true);
  for(const mutate of [
   spec=>{spec.validations[5].expression=spec.validations[5].expression.replace('params.spec.authorityIssuerImage : params.spec.authorityImage','params.spec.authorityImage : params.spec.authorityImage');},
   spec=>{spec.validations[5].expression=spec.validations[5].expression.replace("'Always'","'Always '");},
   spec=>{spec.validations[5].expression=spec.validations[5].expression.replace('image == (has','image ==  (has');},
   spec=>{spec.validations[0].expression+=' && true';},
  ]) {
   const state=structuredClone(fixture.state);mutate(state.jobsPolicy.spec);
   assert.throws(()=>buildDeliveryPlan(state,result,options),/EXACT_JOBS_POLICY_PREDECESSOR_REQUIRED/);
  }
  const legacy=loadDesiredBundle(clone,revisionValue,undefined,1);
  assert.equal(legacy.version,1);assert.equal(Object.hasOwn(legacy,'predecessorJobsPolicyTransitioned'),false);
  assert.throws(()=>buildDeliveryPlan(fixture.state,legacy,options),/EXACT_JOBS_POLICY_PREDECESSOR_REQUIRED/);
  fixture.state.jobsPolicy.spec=admissionResourceWithAPIDefaults(legacy.predecessorJobsPolicyRendered).spec;
  validateDeliveryPlan(buildDeliveryPlan(fixture.state,legacy,options),context,true);
 } finally {rmSync(directory,{recursive:true,force:true});}
});

test('only exact source, Kustomize and Kubernetes API-defaulted policy forms are accepted',()=>{
 const fixture=planAndState(),options={context,k3sSudo:true,capability:fixture.plan.capability,intent:'77777777-7777-4777-8777-777777777777'};
 fixture.state.jobsPolicy.spec=admissionResourceWithAPIDefaults(fixture.desired.predecessorJobsPolicyRendered).spec;
 assert.equal(buildDeliveryPlan(fixture.state,fixture.desired,options).phaseTargets['policy-jobs'].action,'patch');
 const rejects=mutate=>{const state=structuredClone(fixture.state);mutate(state);assert.throws(()=>buildDeliveryPlan(state,fixture.desired,options),/EXACT_JOBS_POLICY_PREDECESSOR_REQUIRED/);};
 rejects(state=>{state.jobsPolicy.spec.validations[0].expression+=' && false';});
 rejects(state=>{state.jobsPolicy.spec.validations[0].expression=state.jobsPolicy.spec.validations[0].expression.replace("'a b'","'a  b'");});
 rejects(state=>{state.jobsPolicy.spec.matchConstraints.namespaceSelector={matchLabels:{'kodex.dev/unsafe':'true'}};});
 rejects(state=>{state.jobsPolicy.spec.failurePolicy='Ignore';});
 const denied=structuredClone(fixture.state);
 denied.releasePolicy={...admissionResourceWithAPIDefaults(fixture.desired.releasePolicy),metadata:metadata('kodex-image-admission-proof-release',false)};
 denied.releaseBinding={...admissionResourceWithAPIDefaults(fixture.desired.releaseBinding),metadata:metadata('kodex-image-admission-proof-release',false)};
 denied.releaseBinding.spec.validationActions=['Warn'];
 assert.throws(()=>buildDeliveryPlan(denied,fixture.desired,options),/RELEASE_BINDING_DRIFT/);
});

test('plan changes only pause, exact reader image and approved hold policies',()=>{
 const {state,plan}=planAndState();validateDeliveryPlan(plan,context,true);
 const initial=state.controller.spec.template.spec.containers[0],pause=plan.controllerSpecs.pause.template.spec.containers[0],reader=plan.controllerSpecs.reader.template.spec.containers[0];
 assert.equal(pause.image,initial.image);assert.equal(pause.env.find(item=>item.name==='IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS').value,'true');
 assert.equal(reader.image,nextImage);assert.equal(reader.env.find(item=>item.name==='IMAGE_ADMISSION_CONTROLLER_HOLD_PROOF_JOBS').value,'false');
 assert.equal(plan.phaseTargets['policy-jobs'].action,'patch');assert.equal(plan.phaseTargets['policy-release'].action,'create');
 assert.equal(plan.phaseTargets['binding-release'].action,'create');
 const mutation=mutationFor(plan,state,'pause');assert.deepEqual(mutation.patch.slice(0,3).map(item=>item.path),['/metadata/uid','/metadata/resourceVersion','/spec']);
 assert.equal(fingerprint(state.deployments[1].spec),plan.guards.neighbors[0].specSHA256);
});

test('phase order requires drained work, exact reader proof and complete fail-closed policy',()=>{
 const {state,plan}=planAndState();
 state.jobs=[{apiVersion:'batch/v1',kind:'Job',metadata:{name:'mc-admit-job',namespace:'kodex-system',uid:nextUID()},spec:{suspend:false},status:{}}];
 setControllerSpec(state,plan.controllerSpecs.pause);
 assert.throws(()=>mutationFor(plan,state,'reader'),/ADMISSION_JOB_HISTORY_CHANGED/);
 state.jobs=[];setControllerSpec(state,plan.controllerSpecs.reader);
 state.controllerExecutableSHA256='9'.repeat(64);
 assert.throws(()=>mutationFor(plan,state,'policy-jobs'),/CONTROLLER_EXECUTABLE_MISMATCH/);
 state.controllerExecutableSHA256=executable;
 assert.equal(mutationFor(plan,state,'policy-jobs').kind,'ValidatingAdmissionPolicy');
 state.jobsPolicy.spec=structuredClone(plan.desired.jobsPolicySpec);state.jobsPolicy.metadata.resourceVersion='99';
 assert.throws(()=>mutationFor(plan,state,'binding-release'),/RELEASE_HOLD_POLICY_REQUIRED/);
});

test('pause drains pinned Job and PVC work while rejecting terminating or replaced history',()=>{
 const active=planAndState(),job={apiVersion:'batch/v1',kind:'Job',metadata:{name:'mc-admit-active',namespace:'kodex-system',uid:nextUID()},spec:{suspend:false},status:{}};
 const pvc={apiVersion:'v1',kind:'PersistentVolumeClaim',metadata:{name:'mc-admit-active',namespace:'kodex-system',uid:nextUID()},spec:{resources:{requests:{storage:'1Gi'}}}};
 active.state.jobs=[job];active.state.pvcs=[pvc];active.state.ownerState.openBuilds=1;
 const plan=buildDeliveryPlan(active.state,active.desired,{context,k3sSudo:true,capability:active.plan.capability,
  intent:'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'});
 setControllerSpec(active.state,plan.controllerSpecs.pause);active.state.jobs[0].status.succeeded=1;active.state.pvcs=[];active.state.ownerState.openBuilds=0;
 assert.equal(mutationFor(plan,active.state,'reader').action,'patch');
 active.state.jobs=[];assert.throws(()=>inspectPhase(plan,active.state,'reader'),/ADMISSION_JOB_HISTORY_CHANGED/);
 const terminating=planAndState();terminating.state.jobs=[{...job,metadata:{...job.metadata,deletionTimestamp:'2026-09-09T00:00:00Z'}}];
 assert.throws(()=>buildDeliveryPlan(terminating.state,terminating.desired,{context,k3sSudo:true,capability:terminating.plan.capability,
  intent:'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'}),/TERMINATING_ADMISSION_WORK_REJECTED/);
});

test('UID, resource and neighbor drift are closed before mutation',()=>{
 const {state,plan}=planAndState();state.deployments[1].spec.replicas=3;
 assert.throws(()=>inspectPhase(plan,state,'pause'),/NEIGHBOR_DEPLOYMENT_CHANGED/);
 const clean=planAndState();clean.state.jobsBinding.metadata.resourceVersion='999';
 assert.throws(()=>inspectPhase(clean.plan,clean.state,'pause'),/JOBS_BINDING_CHANGED/);
 const changed=planAndState();changed.state.controller.metadata.uid=nextUID();
 assert.throws(()=>inspectPhase(changed.plan,changed.state,'pause'),/HOLD_DELIVERY_IDENTITY_CHANGED/);
 const terminating=planAndState();const old=podFor(terminating.state.controller);old.metadata.deletionTimestamp='2026-09-09T00:00:00Z';terminating.state.controllerPods.push(old);
 assert.throws(()=>inspectPhase(terminating.plan,terminating.state,'pause'),/EXACT_CONTROLLER_POD_REQUIRED/);
 const targetChanged=JSON.parse(JSON.stringify(planAndState().plan));targetChanged.phaseTargets['policy-release'].resource.metadata.name='foreign';
 assert.throws(()=>validateDeliveryPlan(targetChanged,context,true),/HOLD_DELIVERY_PLAN_INVALID/);
 const rollbackChanged=JSON.parse(JSON.stringify(planAndState().plan));rollbackChanged.rollbackTargets['rollback-policy-jobs'].name='foreign';
 assert.throws(()=>validateDeliveryPlan(rollbackChanged,context,true),/HOLD_DELIVERY_PLAN_INVALID/);
});

function fakeRuntime(initial,expectedExecutable=executable) {
 const state=structuredClone(initial),initialExecutable=initial.controllerExecutableSHA256,calls={patch:0,create:0,delete:0,rollout:0},deletes=[];
 const updatePods=()=>{state.controllerPods=[podFor(state.controller)];};
 const get=(kind,name)=>{
  if(kind==='namespace')return name==='kube-system'?{metadata:{uid:state.clusterUID}}:{metadata:{uid:state.namespaceUID,labels:state.namespace.metadata.labels}};
  if(kind==='deployment')return state.controller;
  if(kind==='validatingadmissionpolicy')return name==='kodex-image-admission-controller-jobs'?state.jobsPolicy:state.releasePolicy;
  if(kind==='validatingadmissionpolicybinding')return name==='kodex-image-admission-controller-jobs'?state.jobsBinding:state.releaseBinding;
  if(kind==='imageadmissionpolicyparameters')return state.parameters;
  if(kind==='configmap')return state.policyConfig;
  throw new Error('unexpected get');
 };
 return {state,calls,deletes,get,list:kind=>kind==='deployments,replicasets,pods'?[...state.deployments,...state.controllerPods]:kind==='jobs'?state.jobs:state.pvcs,
  readOwnerState:()=>state.ownerState,controllerExecutable:()=>state.controller.spec.template.spec.containers[0].image===nextImage?expectedExecutable:initialExecutable,
  rollout:()=>{calls.rollout++;},
  patch:(kind,name,_namespaced,patch)=>{calls.patch++;const next=patch.at(-1).value;if(kind==='Deployment'){
   state.controller.spec=structuredClone(next);state.controller.metadata.resourceVersion=String(Number(state.controller.metadata.resourceVersion)+1);
   state.controller.metadata.generation++;state.controller.status.observedGeneration=state.controller.metadata.generation;state.deployments[0]=state.controller;updatePods();
  }else {state.jobsPolicy.spec=admissionResourceWithAPIDefaults({...state.jobsPolicy,spec:structuredClone(next)}).spec;state.jobsPolicy.metadata.resourceVersion=String(Number(state.jobsPolicy.metadata.resourceVersion)+1);}},
  create:resource=>{calls.create++;const created={...structuredClone(resource),metadata:{...resource.metadata,uid:nextUID(),resourceVersion:String(sequence++)}};
   const defaulted=admissionResourceWithAPIDefaults(created);if(resource.kind==='ValidatingAdmissionPolicy')state.releasePolicy=defaulted;else state.releaseBinding=defaulted;},
  delete:(kind,name,uid,resourceVersion)=>{calls.delete++;deletes.push({kind,name,uid,resourceVersion});if(kind==='ValidatingAdmissionPolicy')state.releasePolicy=null;else state.releaseBinding=null;},
 };
}

test('fsync intent precedes mutation and uncertain resume never repeats patch',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'hold-delivery-')),evidence=join(directory,'evidence.jsonl'),{state,plan}=planAndState(),rt=fakeRuntime(state);
 try {
  rt.patch=()=>{rt.calls.patch++;};
  let result=await executePhase(plan,'pause',evidence,'apply',rt);assert.equal(result.status,'UNKNOWN');assert.equal(rt.calls.patch,1);
  const first=JSON.parse(readFileSync(evidence,'utf8').trim().split('\n')[0]);assert.equal(first.status,'INTENT');assert.equal(first.phase,'pause');
  result=await executePhase(plan,'pause',evidence,'resume',rt);assert.equal(result.status,'UNKNOWN');assert.equal(rt.calls.patch,1);
 } finally {rmSync(directory,{recursive:true,force:true});}
});

test('definite precondition rejection records FAIL before any mutation',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'hold-delivery-fail-')),evidence=join(directory,'evidence.jsonl'),{state,plan}=planAndState(),rt=fakeRuntime(state);
 try {
  rt.state.deployments[1].spec.replicas=3;
  const result=await executePhase(plan,'pause',evidence,'apply',rt);
  assert.deepEqual(result,{status:'FAIL',code:'NEIGHBOR_DEPLOYMENT_CHANGED'});assert.equal(rt.calls.patch,0);
  const rows=readFileSync(evidence,'utf8').trim().split('\n').map(JSON.parse);
  assert.deepEqual(rows.map(row=>row.status),['FAIL']);assert.equal(rows[0].phase,'pause');
 } finally {rmSync(directory,{recursive:true,force:true});}
});

test('one immutable journal advances pause and reader with authoritative executable proof',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'hold-delivery-pass-')),evidence=join(directory,'evidence.jsonl'),{state,plan}=planAndState(),rt=fakeRuntime(state);
 try {
  assert.equal((await executePhase(plan,'pause',evidence,'apply',rt)).status,'PASS');
  assert.equal((await executePhase(plan,'reader',evidence,'apply',rt)).status,'PASS');
  rt.state.controllerExecutableSHA256=executable;
  assert.equal(rt.calls.patch,2);assert.equal(rt.calls.rollout,2);assert.equal(phaseAfter(plan,rt.state,'reader').target,'AFTER');
  const rows=readFileSync(evidence,'utf8').trim().split('\n').map(JSON.parse);assert.deepEqual(rows.filter(row=>row.status==='PASS').map(row=>row.phase),['pause','reader']);
 } finally {rmSync(directory,{recursive:true,force:true});}
});

test('one journal enforces the complete phase order and changes one resource per phase',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'hold-delivery-full-')),evidence=join(directory,'evidence.jsonl'),{state,plan}=planAndState(),rt=fakeRuntime(state);
 try {
  assert.equal((await executePhase(plan,'pause',evidence,'apply',rt)).status,'PASS');
  await assert.rejects(()=>executePhase(plan,'policy-jobs',evidence,'apply',rt),/PHASE_REQUIRES_RESUME/);
  assert.equal((await executePhase(plan,'reader',evidence,'apply',rt)).status,'PASS');
  assert.equal((await executePhase(plan,'policy-jobs',evidence,'apply',rt)).status,'PASS');
  assert.equal((await executePhase(plan,'policy-release',evidence,'apply',rt)).status,'PASS');
  assert.equal((await executePhase(plan,'binding-release',evidence,'apply',rt)).status,'PASS');
  assert.equal((await executePhase(plan,'open',evidence,'apply',rt)).status,'PASS');
  assert.deepEqual(rt.calls,{patch:4,create:2,delete:0,rollout:3});
  const rows=readFileSync(evidence,'utf8').trim().split('\n').map(JSON.parse);
  assert.deepEqual(rows.filter(row=>row.status==='PASS').map(row=>row.phase),phases);
  assert.equal(rt.state.controller.spec.template.spec.containers[0].env.find(item=>item.name==='IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS').value,'false');
  assert.equal(rt.state.jobsPolicy.spec.matchConstraints.matchPolicy,'Equivalent');
  assert.deepEqual(rt.state.releasePolicy.spec.matchConstraints.objectSelector,{});
  assert.equal(rt.state.releaseBinding.spec.matchResources.matchPolicy,'Equivalent');
 } finally {rmSync(directory,{recursive:true,force:true});}
});

test('exact rollback restores controller and primary VAP with CAS deletes before hold release',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'hold-delivery-rollback-')),forward=join(directory,'forward.jsonl'),rollback=join(directory,'rollback.jsonl'),{state,plan}=planAndState(),rt=fakeRuntime(state);
 try {
  for(const phase of phases)assert.equal((await executePhase(plan,phase,forward,'apply',rt)).status,'PASS');
  for(const phase of rollbackPhases)assert.equal((await executeRollbackPhase(plan,phase,rollback,'apply',rt)).status,'PASS');
  assert.equal(fingerprint(rt.state.controller.spec),fingerprint(plan.controllerSpecs.initial));
  assert.equal(fingerprint(rt.state.jobsPolicy.spec),fingerprint(plan.phaseTargets['policy-jobs'].before));
  assert.equal(rt.state.releasePolicy,null);assert.equal(rt.state.releaseBinding,null);
  assert.deepEqual(rt.calls,{patch:8,create:2,delete:2,rollout:6});
  assert.equal(rt.deletes.every(item=>item.name==='kodex-image-admission-proof-release'&&/^[a-f0-9-]+$/.test(item.uid)&&/^\d+$/.test(item.resourceVersion)),true);
  const rows=readFileSync(rollback,'utf8').trim().split('\n').map(JSON.parse);
  assert.deepEqual(rows.filter(row=>row.status==='PASS').map(row=>row.phase),rollbackPhases);
 } finally {rmSync(directory,{recursive:true,force:true});}
});

test('rollback UNKNOWN resume reads the same delete intent without repeating it',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'hold-delivery-rollback-unknown-')),forward=join(directory,'forward.jsonl'),rollback=join(directory,'rollback.jsonl'),{state,plan}=planAndState(),rt=fakeRuntime(state);
 try {
  for(const phase of phases)assert.equal((await executePhase(plan,phase,forward,'apply',rt)).status,'PASS');
  assert.equal((await executeRollbackPhase(plan,'rollback-pause',rollback,'apply',rt)).status,'PASS');
  rt.delete=()=>{rt.calls.delete++;};
  assert.equal((await executeRollbackPhase(plan,'rollback-binding-release',rollback,'apply',rt)).status,'UNKNOWN');
  assert.equal(rt.calls.delete,1);
  assert.equal((await executeRollbackPhase(plan,'rollback-binding-release',rollback,'resume',rt)).status,'UNKNOWN');
  assert.equal(rt.calls.delete,1);
 } finally {rmSync(directory,{recursive:true,force:true});}
});

test('rollback can close an early paused transition without applying policy',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'hold-delivery-rollback-early-')),forward=join(directory,'forward.jsonl'),rollback=join(directory,'rollback.jsonl'),{state,plan}=planAndState(),rt=fakeRuntime(state);
 try {
  assert.equal((await executePhase(plan,'pause',forward,'apply',rt)).status,'PASS');
  for(const phase of rollbackPhases)assert.equal((await executeRollbackPhase(plan,phase,rollback,'apply',rt)).status,'PASS');
  assert.equal(fingerprint(rt.state.controller.spec),fingerprint(plan.controllerSpecs.initial));
  assert.equal(rt.calls.create,0);assert.equal(rt.calls.delete,0);assert.equal(rt.calls.patch,2);
 } finally {rmSync(directory,{recursive:true,force:true});}
});

test('already exact policy phase records a read-only PASS and performs no mutation',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'hold-delivery-none-')),evidence=join(directory,'evidence.jsonl'),fixture=planAndState();
 fixture.state.jobsPolicy.spec=admissionResourceWithAPIDefaults(fixture.desired.jobsPolicy).spec;
 const plan=buildDeliveryPlan(fixture.state,fixture.desired,{context,k3sSudo:true,capability:fixture.plan.capability,
  intent:'99999999-9999-4999-8999-999999999999'}),rt=fakeRuntime(fixture.state);
 try {
  for(const phase of ['pause','reader','policy-jobs'])assert.equal((await executePhase(plan,phase,evidence,'apply',rt)).status,'PASS');
  assert.equal(rt.calls.patch,2);
  const rows=readFileSync(evidence,'utf8').trim().split('\n').map(JSON.parse);
  assert.equal(rows.find(row=>row.phase==='policy-jobs'&&row.status==='PASS').action,'none');
  assert.equal(rows.some(row=>row.phase==='policy-jobs'&&row.status==='INTENT'),false);
 } finally {rmSync(directory,{recursive:true,force:true});}
});

test('host proof binds CRI identity and stable /proc executable digest',()=>{
 const target={containerID:'5'.repeat(64),imageID:nextImage,image:nextImage,podUID:'11111111-1111-4111-8111-111111111111',podName:'controller-pod',
  namespace:'kodex-system',container:'image-admission-controller',restartCount:0,executable:'/usr/local/bin/image-admission-controller'};
 const runtime={status:{id:target.containerID,state:'CONTAINER_RUNNING',imageRef:target.imageID,metadata:{name:target.container,attempt:0},labels:{
  'io.kubernetes.pod.uid':target.podUID,'io.kubernetes.pod.name':target.podName,'io.kubernetes.pod.namespace':target.namespace,
  'io.kubernetes.container.name':target.container}},info:{pid:42,runtimeSpec:{process:{args:[target.executable]}}}};
 assert.equal(verifyControllerRuntime(target,runtime),42);
 const proof={binarySHA256:executable,startTicks:'100',device:'1',inode:'2'};
 assert.equal(inspectHostController(target,{runtime:()=>runtime,processReader:()=>proof}).binarySHA256,executable);
 const bad=structuredClone(runtime);bad.info.runtimeSpec.process.args=['/bin/sh'];assert.throws(()=>verifyControllerRuntime(target,bad),/CONTROLLER_CRI_BINDING_REJECTED/);
});
