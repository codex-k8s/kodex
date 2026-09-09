import {fingerprint} from './scoped-release.mjs';

export const namespace='kodex-system';
export const controllerName='image-admission-controller';
export const jobsPolicyName='kodex-image-admission-controller-jobs';
export const releasePolicyName='kodex-image-admission-proof-release';
export const pauseEnvironment='IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS';
export const holdEnvironment='IMAGE_ADMISSION_CONTROLLER_HOLD_PROOF_JOBS';
export const holdUntilEnvironment='IMAGE_ADMISSION_CONTROLLER_PROOF_HOLD_UNTIL';
export const holdUntilDisabled='1970-01-01T00:00:00Z';
export const phases=['pause','reader','policy-jobs','policy-release','binding-release','open'];
export const rollbackPhases=['rollback-pause','rollback-binding-release','rollback-policy-release','rollback-policy-jobs','rollback-reader','rollback-open'];

const sha=/^[a-f0-9]{64}$/;
const image=/^registry\.local\.kodex\/kodex\/image-admission@sha256:[a-f0-9]{64}$/;
const uid=/^[a-f0-9][a-f0-9-]{7,127}$/;
const requireValue=(value,code)=>{if(!value)throw new Error(code);};
const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&
 Object.keys(value).sort().join('\0')===[...keys].sort().join('\0');

function meta(resource,name,namespaced=true) {
 const metadata=resource?.metadata;
 requireValue(metadata?.name===name&&(!namespaced||metadata.namespace===namespace)&&uid.test(metadata.uid??'')&&
  /^\d+$/.test(metadata.resourceVersion??'')&&!metadata.deletionTimestamp,'EXACT_RESOURCE_IDENTITY_REQUIRED');
}

function application(deployment) {
 meta(deployment,controllerName);
 requireValue(deployment.apiVersion==='apps/v1'&&deployment.kind==='Deployment'&&
  (deployment.metadata.labels?.['kodex.dev/environment']==='staging'||deployment.metadata.labels?.['kodex.dev/local-profile']==='hot-reload')&&
  deployment.spec?.replicas===1&&deployment.spec?.strategy?.type==='Recreate'&&deployment.spec.paused!==true&&
  deployment.status?.observedGeneration>=deployment.metadata.generation&&
  ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(key=>deployment.status?.[key]===1),
 'EXACT_HEALTHY_CONTROLLER_REQUIRED');
 const containers=deployment.spec.template?.spec?.containers??[];
 requireValue(containers.filter(item=>item.name===controllerName).length===1,'EXACT_CONTROLLER_CONTAINER_REQUIRED');
 const container=containers.find(item=>item.name===controllerName);
 requireValue(fingerprint(container.command)===fingerprint(['/usr/local/bin/image-admission-controller'])&&
  (!container.args||container.args.length===0)&&/@sha256:[a-f0-9]{64}$/.test(container.image??''),'EXACT_CONTROLLER_COMMAND_REQUIRED');
 return container;
}

function literal(container,name,allowed,required=false) {
 const entries=(container.env??[]).filter(item=>item.name===name);
 requireValue(entries.length<2&&(!required||entries.length===1),'EXACT_LITERAL_ENVIRONMENT_REQUIRED');
 if(entries.length===0)return null;
 requireValue(typeof entries[0].value==='string'&&!entries[0].valueFrom&&allowed.includes(entries[0].value),'EXACT_LITERAL_ENVIRONMENT_REQUIRED');
 return entries[0];
}

function setLiteral(container,name,value,{allowMissing=false,expected=[]}={}) {
 container.env??=[];
 const entries=container.env.filter(item=>item.name===name);
 requireValue(entries.length===1||allowMissing&&entries.length===0,'EXACT_LITERAL_ENVIRONMENT_REQUIRED');
 if(entries.length===0)container.env.push({name,value});
 else {
  requireValue(typeof entries[0].value==='string'&&!entries[0].valueFrom&&
   (expected.length===0||expected.includes(entries[0].value)),'EXACT_LITERAL_ENVIRONMENT_REQUIRED');
  entries[0].value=value;
 }
}

function targetState(resource) {
 if(resource===null)return {present:false};
 return {present:true,apiVersion:resource.apiVersion,kind:resource.kind,name:resource.metadata.name,
  uid:resource.metadata.uid,resourceVersion:resource.metadata.resourceVersion,specSHA256:fingerprint(resource.spec??resource.data)};
}

function controllerPodState(snapshot) {
 const app=application(snapshot.controller),pods=snapshot.controllerPods??[];
 requireValue(pods.length===1,'EXACT_CONTROLLER_POD_REQUIRED');
 const pod=pods[0],status=(pod.status?.containerStatuses??[]).filter(item=>item.name===controllerName),
  container=(pod.spec?.containers??[]).filter(item=>item.name===controllerName);
 requireValue(pod.metadata?.namespace===namespace&&uid.test(pod.metadata.uid??'')&&!pod.metadata.deletionTimestamp&&
  pod.status?.phase==='Running'&&pod.status?.conditions?.some(item=>item.type==='Ready'&&item.status==='True')&&
  container.length===1&&fingerprint(container[0])===fingerprint(app)&&status.length===1&&status[0].ready===true&&status[0].state?.running&&
  /^containerd:\/\/[a-f0-9]{64}$/.test(status[0].containerID??'')&&status[0].imageID?.endsWith(`@${app.image.split('@')[1]}`)&&
  Number.isSafeInteger(status[0].restartCount)&&status[0].restartCount>=0,'EXACT_CONTROLLER_POD_REQUIRED');
 return {name:pod.metadata.name,uid:pod.metadata.uid,containerID:status[0].containerID,imageID:status[0].imageID,restarts:status[0].restartCount};
}

function deploymentGuards(deployments) {
 return deployments.filter(item=>item.metadata?.name!==controllerName).map(item=>{
  requireValue(item.kind==='Deployment'&&item.metadata?.namespace===namespace&&uid.test(item.metadata.uid??''),'EXACT_NEIGHBOR_DEPLOYMENT_REQUIRED');
  return {name:item.metadata.name,uid:item.metadata.uid,specSHA256:fingerprint(item.spec)};
 }).sort((a,b)=>a.name.localeCompare(b.name));
}

function workState(snapshot) {
 const jobs=(snapshot.jobs??[]).map(job=>{
  requireValue(job.kind==='Job'&&job.metadata?.namespace===namespace&&uid.test(job.metadata.uid??''),'EXACT_ADMISSION_JOB_REQUIRED');
  const terminal=job.status?.succeeded>0||job.status?.failed>0||job.status?.conditions?.some(item=>['Complete','Failed'].includes(item.type)&&item.status==='True');
  return {name:job.metadata.name,uid:job.metadata.uid,specSHA256:fingerprint(job.spec),terminal:Boolean(terminal),terminating:Boolean(job.metadata.deletionTimestamp)};
 }).sort((a,b)=>a.name.localeCompare(b.name));
 const pvcs=(snapshot.pvcs??[]).map(pvc=>{
  requireValue(pvc.kind==='PersistentVolumeClaim'&&pvc.metadata?.namespace===namespace&&uid.test(pvc.metadata.uid??''),'EXACT_ADMISSION_PVC_REQUIRED');
  return {name:pvc.metadata.name,uid:pvc.metadata.uid,specSHA256:fingerprint(pvc.spec),terminating:Boolean(pvc.metadata.deletionTimestamp)};
 }).sort((a,b)=>a.name.localeCompare(b.name));
 return {jobs,pvcs};
}

function initialWork(state) {
 requireValue(state.jobs.every(item=>!item.terminating)&&state.pvcs.every(item=>!item.terminating),'TERMINATING_ADMISSION_WORK_REJECTED');return state;
}

function workCompatible(expected,snapshot) {
 const actual=workState(snapshot),jobIdentity=item=>({name:item.name,uid:item.uid,specSHA256:item.specSHA256});
 requireValue(fingerprint(actual.jobs.map(jobIdentity))===fingerprint(expected.jobs.map(jobIdentity))&&
  actual.jobs.every((item,index)=>!item.terminating&&(!expected.jobs[index].terminal||item.terminal)),'ADMISSION_JOB_HISTORY_CHANGED');
 const expectedPVC=new Map(expected.pvcs.map(item=>[item.name,item]));
 requireValue(actual.pvcs.every(item=>{const before=expectedPVC.get(item.name);return before&&!item.terminating&&item.uid===before.uid&&item.specSHA256===before.specSHA256;}),
 'ADMISSION_PVC_HISTORY_CHANGED');return actual;
}

function policyIdentity(resource,name,kind) {
 if(resource===null)return null;
 meta(resource,name,false);
 requireValue(resource.apiVersion==='admissionregistration.k8s.io/v1'&&resource.kind===kind,'EXACT_ADMISSION_RESOURCE_REQUIRED');
 return targetState(resource);
}

function requirePrimaryBoundary(snapshot,bundle) {
 meta(snapshot.jobsPolicy,jobsPolicyName,false);
 requireValue(snapshot.jobsPolicy.apiVersion==='admissionregistration.k8s.io/v1'&&snapshot.jobsPolicy.kind==='ValidatingAdmissionPolicy'&&
  snapshot.jobsPolicy.spec?.failurePolicy==='Fail'&&
  [fingerprint(bundle.predecessorJobsPolicySpec),fingerprint(bundle.jobsPolicySpec)].includes(fingerprint(snapshot.jobsPolicy.spec)),
 'EXACT_JOBS_POLICY_PREDECESSOR_REQUIRED');
 meta(snapshot.jobsBinding,jobsPolicyName,false);
 const binding=snapshot.jobsBinding;
 requireValue(binding.kind==='ValidatingAdmissionPolicyBinding'&&binding.spec?.policyName===jobsPolicyName&&
  fingerprint(binding.spec.validationActions)===fingerprint(['Deny'])&&binding.spec.paramRef?.namespace===namespace&&
  binding.spec.paramRef?.parameterNotFoundAction==='Deny'&&typeof binding.spec.paramRef.name==='string','EXACT_JOBS_BINDING_REQUIRED');
 meta(snapshot.parameters,binding.spec.paramRef.name);
 requireValue(snapshot.parameters.kind==='ImageAdmissionPolicyParameters'&&snapshot.parameters.spec&&typeof snapshot.parameters.spec==='object','EXACT_POLICY_PARAMETERS_REQUIRED');
 meta(snapshot.policyConfig,binding.spec.paramRef.name);
 requireValue(snapshot.policyConfig.kind==='ConfigMap'&&snapshot.policyConfig.immutable===true&&
  fingerprint(snapshot.policyConfig.data)===fingerprint(snapshot.parameters.spec),'EXACT_POLICY_PARAMETER_BINDING_REQUIRED');
 const app=application(snapshot.controller);
 requireValue(literal(app,'IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP',[binding.spec.paramRef.name],true),'EXACT_CONTROLLER_POLICY_BINDING_REQUIRED');
 const releasePolicy=policyIdentity(snapshot.releasePolicy,releasePolicyName,'ValidatingAdmissionPolicy');
 const releaseBinding=policyIdentity(snapshot.releaseBinding,releasePolicyName,'ValidatingAdmissionPolicyBinding');
 requireValue(!releasePolicy||fingerprint(snapshot.releasePolicy.spec)===fingerprint(bundle.releasePolicy.spec),'RELEASE_POLICY_DRIFT');
 requireValue(!releaseBinding||fingerprint(snapshot.releaseBinding.spec)===fingerprint(bundle.releaseBinding.spec),'RELEASE_BINDING_DRIFT');
 return {releasePolicy,releaseBinding};
}

function controllerSpecs(controller,targetImage,bundle) {
 const current=application(controller);
 requireValue(image.test(targetImage),'EXACT_CONTROLLER_IMAGE_REQUIRED');
 literal(current,pauseEnvironment,['false'],false);
 literal(current,holdEnvironment,['false'],false);
 literal(current,holdUntilEnvironment,[holdUntilDisabled],false);
 const pause=structuredClone(controller.spec),pauseApp=pause.template.spec.containers.find(item=>item.name===controllerName);
 setLiteral(pauseApp,pauseEnvironment,'true',{allowMissing:true,expected:['false']});
 const reader=structuredClone(pause),readerApp=reader.template.spec.containers.find(item=>item.name===controllerName);
 readerApp.image=targetImage;
 for(const entry of bundle.controllerHoldEnvironment)setLiteral(readerApp,entry.name,entry.value,{allowMissing:true,expected:[entry.value]});
 const open=structuredClone(reader),openApp=open.template.spec.containers.find(item=>item.name===controllerName);
 setLiteral(openApp,pauseEnvironment,'false',{expected:['true']});
 requireValue(new Set([fingerprint(controller.spec),fingerprint(pause),fingerprint(reader),fingerprint(open)]).size>=3,'CONTROLLER_TRANSITION_UNCHANGED');
 return {initial:controller.spec,pause,reader,open};
}

export function validateDesiredBundle(bundle) {
 requireValue(exact(bundle,['version','revision','source','predecessorJobsPolicySpec','jobsPolicySpec','releasePolicy','releaseBinding','controllerHoldEnvironment'])&&
  bundle.version===1&&/^[a-f0-9]{40}$/.test(bundle.revision)&&typeof bundle.source==='string'&&bundle.source.startsWith('/')&&
  bundle.jobsPolicySpec?.failurePolicy==='Fail'&&
  bundle.releasePolicy?.apiVersion==='admissionregistration.k8s.io/v1'&&bundle.releasePolicy.kind==='ValidatingAdmissionPolicy'&&
  bundle.releasePolicy.metadata?.name===releasePolicyName&&bundle.releasePolicy.spec?.failurePolicy==='Fail'&&
  bundle.releaseBinding?.apiVersion==='admissionregistration.k8s.io/v1'&&bundle.releaseBinding.kind==='ValidatingAdmissionPolicyBinding'&&
  bundle.releaseBinding.metadata?.name===releasePolicyName&&bundle.releaseBinding.spec?.policyName===releasePolicyName&&
  fingerprint(bundle.releaseBinding.spec.validationActions)===fingerprint(['Deny'])&&
  bundle.controllerHoldEnvironment.length===2,'INVALID_HOLD_DELIVERY_BUNDLE');
 const variables=bundle.jobsPolicySpec.variables??[],validations=bundle.jobsPolicySpec.validations??[];
 requireValue(variables.filter(item=>item.name==='proofHeld').length===1&&
  validations.filter(item=>item.message==='Image admission executable proof reservation is invalid.').length===1&&
  fingerprint(bundle.controllerHoldEnvironment)===fingerprint([
   {name:holdEnvironment,value:'false'},{name:holdUntilEnvironment,value:holdUntilDisabled},
  ]),'INVALID_HOLD_DELIVERY_BUNDLE');
 return bundle;
}

export function buildDeliveryPlan(snapshot,bundle,{context,k3sSudo,capability,intent}) {
 validateDesiredBundle(bundle);
 const targetImage=capability?.image,executableSHA256=capability?.executableSHA256;
 requireValue(typeof context==='string'&&context.length>0&&!/prod/i.test(context)&&typeof k3sSudo==='boolean'&&
  exact(capability,['version','profile','revision','go','image','executableSHA256','recipe'])&&capability.version===1&&
  capability.profile==='image-admission-hold-delivery'&&capability.revision===bundle.revision&&capability.go==='go1.26.6'&&
  image.test(targetImage)&&sha.test(executableSHA256)&&sha.test(snapshot.controllerExecutableSHA256??'')&&uid.test(intent),'INVALID_HOLD_DELIVERY_INPUT');
 requireValue(snapshot.clusterUID&&snapshot.namespaceUID&&snapshot.namespace?.metadata?.labels?.['app.kubernetes.io/part-of']==='kodex'&&
  snapshot.namespace.metadata.labels?.['kodex.dev/environment']==='staging','EXACT_STAGING_NAMESPACE_REQUIRED');
 const policies=requirePrimaryBoundary(snapshot,bundle),specs=controllerSpecs(snapshot.controller,targetImage,bundle),work=initialWork(workState(snapshot));
 const reader=controllerPodState(snapshot);
 const guards={neighbors:deploymentGuards(snapshot.deployments),jobsBinding:targetState(snapshot.jobsBinding),parameters:targetState(snapshot.parameters),
  policyConfig:targetState(snapshot.policyConfig),ownerState:snapshot.ownerState,initialWork:work};
 const primaryAfter=fingerprint(snapshot.jobsPolicy.spec)===fingerprint(bundle.jobsPolicySpec)?snapshot.jobsPolicy.spec:bundle.jobsPolicySpec;
 const phaseTargets={
  pause:{action:'patch',kind:'Deployment',name:controllerName,namespaced:true,before:specs.initial,after:specs.pause},
  reader:{action:'patch',kind:'Deployment',name:controllerName,namespaced:true,before:specs.pause,after:specs.reader},
  'policy-jobs':{action:fingerprint(snapshot.jobsPolicy.spec)===fingerprint(bundle.jobsPolicySpec)?'none':'patch',kind:'ValidatingAdmissionPolicy',name:jobsPolicyName,namespaced:false,before:snapshot.jobsPolicy.spec,after:primaryAfter},
  'policy-release':{action:snapshot.releasePolicy?'none':'create',kind:'ValidatingAdmissionPolicy',name:releasePolicyName,namespaced:false,before:null,after:bundle.releasePolicy.spec,
   ...(!snapshot.releasePolicy?{resource:bundle.releasePolicy}:{})},
  'binding-release':{action:snapshot.releaseBinding?'none':'create',kind:'ValidatingAdmissionPolicyBinding',name:releasePolicyName,namespaced:false,before:null,after:bundle.releaseBinding.spec,
   ...(!snapshot.releaseBinding?{resource:bundle.releaseBinding}:{})},
  open:{action:'patch',kind:'Deployment',name:controllerName,namespaced:true,before:specs.reader,after:specs.open},
 };
 const rollbackTargets={
  'rollback-pause':{action:'patch',kind:'Deployment',name:controllerName,namespaced:true,before:specs.open,after:specs.reader},
  'rollback-binding-release':releaseBindingInitialTarget(policies.releaseBinding,bundle.releaseBinding),
  'rollback-policy-release':releasePolicyInitialTarget(policies.releasePolicy,bundle.releasePolicy),
  'rollback-policy-jobs':{action:phaseTargets['policy-jobs'].action==='none'?'none':'patch',kind:'ValidatingAdmissionPolicy',name:jobsPolicyName,
   namespaced:false,before:bundle.jobsPolicySpec,after:snapshot.jobsPolicy.spec},
  'rollback-reader':{action:'patch',kind:'Deployment',name:controllerName,namespaced:true,before:specs.reader,after:specs.pause},
  'rollback-open':{action:'patch',kind:'Deployment',name:controllerName,namespaced:true,before:specs.pause,after:specs.initial},
 };
 return {version:1,intent,context,k3sSudo,clusterUID:snapshot.clusterUID,namespaceUID:snapshot.namespaceUID,
  source:bundle.source,revision:bundle.revision,bundle,capability,targetImage,executableSHA256,controllerUID:snapshot.controller.metadata.uid,
  controllerInitialResourceVersion:snapshot.controller.metadata.resourceVersion,controllerInitialReader:reader,
  controllerInitialExecutableSHA256:snapshot.controllerExecutableSHA256,jobsPolicyUID:snapshot.jobsPolicy.metadata.uid,
  jobsPolicyInitialResourceVersion:snapshot.jobsPolicy.metadata.resourceVersion,releasePolicyInitial:policies.releasePolicy,
  releaseBindingInitial:policies.releaseBinding,desired:{jobsPolicySpec:bundle.jobsPolicySpec,releasePolicy:bundle.releasePolicy,releaseBinding:bundle.releaseBinding},
  controllerSpecs:specs,guards,phaseTargets,rollbackTargets};
}

function releasePolicyInitialTarget(initial,desired) {
 return initial?{action:'none',kind:'ValidatingAdmissionPolicy',name:releasePolicyName,namespaced:false,before:desired.spec,after:desired.spec}:
  {action:'delete',kind:'ValidatingAdmissionPolicy',name:releasePolicyName,namespaced:false,before:desired.spec,after:null};
}

function releaseBindingInitialTarget(initial,desired) {
 return initial?{action:'none',kind:'ValidatingAdmissionPolicyBinding',name:releasePolicyName,namespaced:false,before:desired.spec,after:desired.spec}:
  {action:'delete',kind:'ValidatingAdmissionPolicyBinding',name:releasePolicyName,namespaced:false,before:desired.spec,after:null};
}

export function validateDeliveryPlan(plan,context,k3sSudo) {
 validateDesiredBundle(plan?.bundle);
 requireValue(plan?.version===1&&uid.test(plan.intent??'')&&plan.context===context&&plan.k3sSudo===k3sSudo&&
  uid.test(plan.clusterUID??'')&&uid.test(plan.namespaceUID??'')&&/^[a-f0-9]{40}$/.test(plan.revision??'')&&
  typeof plan.source==='string'&&plan.source.startsWith('/')&&image.test(plan.targetImage??'')&&sha.test(plan.executableSHA256??'')&&sha.test(plan.controllerInitialExecutableSHA256??'')&&
  uid.test(plan.controllerUID??'')&&/^\d+$/.test(plan.controllerInitialResourceVersion??'')&&uid.test(plan.controllerInitialReader?.uid??'')&&uid.test(plan.jobsPolicyUID??'')&&
  /^\d+$/.test(plan.jobsPolicyInitialResourceVersion??'')&&exact(plan.phaseTargets,phases)&&exact(plan.rollbackTargets,rollbackPhases),'HOLD_DELIVERY_PLAN_INVALID');
 requireValue(plan.source===plan.bundle.source&&plan.revision===plan.bundle.revision&&
  exact(plan.capability,['version','profile','revision','go','image','executableSHA256','recipe'])&&plan.capability.version===1&&
  plan.capability.profile==='image-admission-hold-delivery'&&plan.capability.revision===plan.revision&&
  plan.capability.go==='go1.26.6'&&plan.capability.image===plan.targetImage&&plan.capability.executableSHA256===plan.executableSHA256&&
  fingerprint(plan.desired.jobsPolicySpec)===fingerprint(plan.bundle.jobsPolicySpec)&&
  fingerprint(plan.desired.releasePolicy)===fingerprint(plan.bundle.releasePolicy)&&
  fingerprint(plan.desired.releaseBinding)===fingerprint(plan.bundle.releaseBinding),'HOLD_DELIVERY_PLAN_INVALID');
 const specs=plan.controllerSpecs,expectedPause=structuredClone(specs.initial),pauseApp=expectedPause.template?.spec?.containers?.find(item=>item.name===controllerName);
 requireValue(pauseApp,'HOLD_DELIVERY_PLAN_INVALID');
 setLiteral(pauseApp,pauseEnvironment,'true',{allowMissing:true,expected:['false']});
 const expectedReader=structuredClone(expectedPause),readerApp=expectedReader.template.spec.containers.find(item=>item.name===controllerName);
 readerApp.image=plan.targetImage;
 for(const entry of plan.bundle.controllerHoldEnvironment)setLiteral(readerApp,entry.name,entry.value,{allowMissing:true,expected:[entry.value]});
 const expectedOpen=structuredClone(expectedReader),openApp=expectedOpen.template.spec.containers.find(item=>item.name===controllerName);
 setLiteral(openApp,pauseEnvironment,'false',{expected:['true']});
 requireValue(fingerprint(specs.pause)===fingerprint(expectedPause)&&fingerprint(specs.reader)===fingerprint(expectedReader)&&
  fingerprint(specs.open)===fingerprint(expectedOpen),'HOLD_DELIVERY_PLAN_INVALID');
 for(const phase of phases) {
  const target=plan.phaseTargets[phase];
  requireValue(exact(target,target.action==='create'?['action','kind','name','namespaced','before','after','resource']:['action','kind','name','namespaced','before','after'])&&
   ['patch','create','none'].includes(target.action)&&typeof target.name==='string'&&typeof target.namespaced==='boolean'&&
   (target.before===null||sha.test(fingerprint(target.before)))&&sha.test(fingerprint(target.after)),'HOLD_DELIVERY_PLAN_INVALID');
 }
 for(const phase of rollbackPhases) {
  const target=plan.rollbackTargets[phase];
  requireValue(exact(target,['action','kind','name','namespaced','before','after'])&&['patch','delete','none'].includes(target.action)&&
   typeof target.name==='string'&&typeof target.namespaced==='boolean'&&
   (target.before===null||sha.test(fingerprint(target.before)))&&(target.after===null||sha.test(fingerprint(target.after))),'HOLD_DELIVERY_PLAN_INVALID');
 }
 requireValue(plan.phaseTargets.pause.action==='patch'&&plan.phaseTargets.pause.kind==='Deployment'&&plan.phaseTargets.pause.name===controllerName&&plan.phaseTargets.pause.namespaced===true&&
  plan.phaseTargets.reader.action==='patch'&&plan.phaseTargets.reader.kind==='Deployment'&&plan.phaseTargets.reader.name===controllerName&&plan.phaseTargets.reader.namespaced===true&&
  plan.phaseTargets.open.action==='patch'&&plan.phaseTargets.open.kind==='Deployment'&&plan.phaseTargets.open.name===controllerName&&plan.phaseTargets.open.namespaced===true&&
  plan.phaseTargets['policy-jobs'].kind==='ValidatingAdmissionPolicy'&&plan.phaseTargets['policy-jobs'].name===jobsPolicyName&&plan.phaseTargets['policy-jobs'].namespaced===false&&
  plan.phaseTargets['policy-jobs'].action===(fingerprint(plan.phaseTargets['policy-jobs'].before)===fingerprint(plan.bundle.jobsPolicySpec)?'none':'patch')&&
  plan.phaseTargets['policy-release'].kind==='ValidatingAdmissionPolicy'&&plan.phaseTargets['policy-release'].name===releasePolicyName&&plan.phaseTargets['policy-release'].namespaced===false&&
  plan.phaseTargets['policy-release'].action===(plan.releasePolicyInitial===null?'create':'none')&&
  plan.phaseTargets['binding-release'].kind==='ValidatingAdmissionPolicyBinding'&&plan.phaseTargets['binding-release'].name===releasePolicyName&&plan.phaseTargets['binding-release'].namespaced===false&&
  plan.phaseTargets['binding-release'].action===(plan.releaseBindingInitial===null?'create':'none')&&
  (plan.phaseTargets['policy-release'].action!=='create'||fingerprint(plan.phaseTargets['policy-release'].resource)===fingerprint(plan.bundle.releasePolicy))&&
  (plan.phaseTargets['binding-release'].action!=='create'||fingerprint(plan.phaseTargets['binding-release'].resource)===fingerprint(plan.bundle.releaseBinding))&&
  fingerprint(plan.phaseTargets.pause.before)===fingerprint(specs.initial)&&fingerprint(plan.phaseTargets.pause.after)===fingerprint(specs.pause)&&
  fingerprint(plan.phaseTargets.reader.before)===fingerprint(specs.pause)&&fingerprint(plan.phaseTargets.reader.after)===fingerprint(specs.reader)&&
  fingerprint(plan.phaseTargets.open.before)===fingerprint(specs.reader)&&fingerprint(plan.phaseTargets.open.after)===fingerprint(specs.open)&&
  fingerprint(plan.phaseTargets['policy-jobs'].after)===fingerprint(plan.bundle.jobsPolicySpec)&&
  fingerprint(plan.phaseTargets['policy-release'].after)===fingerprint(plan.bundle.releasePolicy.spec)&&
  fingerprint(plan.phaseTargets['binding-release'].after)===fingerprint(plan.bundle.releaseBinding.spec)&&
  plan.rollbackTargets['rollback-pause'].action==='patch'&&plan.rollbackTargets['rollback-pause'].kind==='Deployment'&&plan.rollbackTargets['rollback-pause'].name===controllerName&&plan.rollbackTargets['rollback-pause'].namespaced===true&&
  plan.rollbackTargets['rollback-reader'].action==='patch'&&plan.rollbackTargets['rollback-reader'].kind==='Deployment'&&plan.rollbackTargets['rollback-reader'].name===controllerName&&plan.rollbackTargets['rollback-reader'].namespaced===true&&
  plan.rollbackTargets['rollback-open'].action==='patch'&&plan.rollbackTargets['rollback-open'].kind==='Deployment'&&plan.rollbackTargets['rollback-open'].name===controllerName&&plan.rollbackTargets['rollback-open'].namespaced===true&&
  plan.rollbackTargets['rollback-policy-jobs'].kind==='ValidatingAdmissionPolicy'&&plan.rollbackTargets['rollback-policy-jobs'].name===jobsPolicyName&&plan.rollbackTargets['rollback-policy-jobs'].namespaced===false&&
  plan.rollbackTargets['rollback-policy-jobs'].action===(plan.phaseTargets['policy-jobs'].action==='none'?'none':'patch')&&
  plan.rollbackTargets['rollback-policy-release'].kind==='ValidatingAdmissionPolicy'&&plan.rollbackTargets['rollback-policy-release'].name===releasePolicyName&&plan.rollbackTargets['rollback-policy-release'].namespaced===false&&
  plan.rollbackTargets['rollback-binding-release'].kind==='ValidatingAdmissionPolicyBinding'&&plan.rollbackTargets['rollback-binding-release'].name===releasePolicyName&&plan.rollbackTargets['rollback-binding-release'].namespaced===false&&
  fingerprint(plan.rollbackTargets['rollback-pause'].before)===fingerprint(specs.open)&&fingerprint(plan.rollbackTargets['rollback-pause'].after)===fingerprint(specs.reader)&&
  fingerprint(plan.rollbackTargets['rollback-reader'].before)===fingerprint(specs.reader)&&fingerprint(plan.rollbackTargets['rollback-reader'].after)===fingerprint(specs.pause)&&
  fingerprint(plan.rollbackTargets['rollback-open'].before)===fingerprint(specs.pause)&&fingerprint(plan.rollbackTargets['rollback-open'].after)===fingerprint(specs.initial)&&
  fingerprint(plan.rollbackTargets['rollback-policy-jobs'].before)===fingerprint(plan.bundle.jobsPolicySpec)&&
  fingerprint(plan.rollbackTargets['rollback-policy-jobs'].after)===fingerprint(plan.phaseTargets['policy-jobs'].before)&&
  (plan.rollbackTargets['rollback-policy-release'].action==='delete')===(plan.releasePolicyInitial===null)&&
  (plan.rollbackTargets['rollback-binding-release'].action==='delete')===(plan.releaseBindingInitial===null),
 'HOLD_DELIVERY_PLAN_INVALID');
 return plan;
}

function exactNeighbors(expected,actual) {
 const projected=deploymentGuards(actual.deployments);
 requireValue(fingerprint(projected)===fingerprint(expected),'NEIGHBOR_DEPLOYMENT_CHANGED');
}

function exactPinned(resource,expected,code) {
 requireValue(resource&&resource.metadata.uid===expected.uid&&resource.metadata.resourceVersion===expected.resourceVersion&&
  fingerprint(resource.spec??resource.data)===expected.specSHA256,code);
}

function idle(snapshot) {
 const state=workState(snapshot);
 requireValue(state.jobs.every(item=>item.terminal&&!item.terminating)&&state.pvcs.length===0,'ADMISSION_WORK_NOT_DRAINED');
 const owner=snapshot.ownerState;
 requireValue(owner&&['openBuilds','pendingAdmissions','pendingPromotions','activeRuntimeRuns','claimedRuntimeLeases'].every(key=>owner[key]===0)&&
  Number.isSafeInteger(owner.promotedArtifactCount)&&owner.promotedArtifactCount>=0&&sha.test(owner.promotedPinsSHA256??''),'FRESH_IDLE_OWNER_STATE_REQUIRED');
}

function ownerUnchanged(expected,actual) {
 requireValue(actual&&actual.promotedArtifactCount===expected.promotedArtifactCount&&actual.promotedPinsSHA256===expected.promotedPinsSHA256,
  'PUBLISHED_PINS_CHANGED');
}

function resourceState(resource,target,uidExpected=null) {
 if(target.action==='create') {
  if(resource===null)return 'BEFORE';
  requireValue(resource.metadata?.name===target.name&&fingerprint(resource.spec)===fingerprint(target.after),'TARGET_RESOURCE_DRIFT');
  return 'AFTER';
 }
 if(target.action==='delete') {
  if(resource===null)return 'AFTER';
  requireValue((!uidExpected||resource.metadata.uid===uidExpected)&&fingerprint(resource.spec)===fingerprint(target.before),'TARGET_RESOURCE_DRIFT');
  return 'BEFORE';
 }
 requireValue(resource&&(!uidExpected||resource.metadata.uid===uidExpected),'TARGET_UID_CHANGED');
 const digest=fingerprint(resource.spec);
 if(digest===fingerprint(target.after))return 'AFTER';
 if(digest===fingerprint(target.before))return 'BEFORE';
 throw new Error('TARGET_SPEC_DRIFT');
}

function requirePolicies(plan,snapshot,phase) {
 const primary=resourceState(snapshot.jobsPolicy,plan.phaseTargets['policy-jobs'],plan.jobsPolicyUID);
 const release=resourceState(snapshot.releasePolicy,plan.phaseTargets['policy-release'],plan.releasePolicyInitial?.uid);
 const binding=resourceState(snapshot.releaseBinding,plan.phaseTargets['binding-release'],plan.releaseBindingInitial?.uid);
 if(['policy-release','binding-release','open'].includes(phase))requireValue(primary==='AFTER','PRIMARY_HOLD_POLICY_REQUIRED');
 if(['binding-release','open'].includes(phase))requireValue(release==='AFTER','RELEASE_HOLD_POLICY_REQUIRED');
 if(phase==='open')requireValue(binding==='AFTER','RELEASE_HOLD_BINDING_REQUIRED');
 return {primary,release,binding};
}

export function inspectPhase(plan,snapshot,phase,{requireExecutable=true}={}) {
 validateDeliveryPlan(plan,plan.context,plan.k3sSudo);
 requireValue(phases.includes(phase)&&snapshot.clusterUID===plan.clusterUID&&snapshot.namespaceUID===plan.namespaceUID&&
  snapshot.controller.metadata.uid===plan.controllerUID,'HOLD_DELIVERY_IDENTITY_CHANGED');
 exactNeighbors(plan.guards.neighbors,snapshot);
 exactPinned(snapshot.jobsBinding,plan.guards.jobsBinding,'JOBS_BINDING_CHANGED');
 exactPinned(snapshot.parameters,plan.guards.parameters,'POLICY_PARAMETERS_CHANGED');
 requireValue(snapshot.policyConfig.metadata.uid===plan.guards.policyConfig.uid&&snapshot.policyConfig.metadata.resourceVersion===plan.guards.policyConfig.resourceVersion&&
  fingerprint(snapshot.policyConfig.data)===plan.guards.policyConfig.specSHA256,'POLICY_CONFIG_CHANGED');
 ownerUnchanged(plan.guards.ownerState,snapshot.ownerState);
 workCompatible(plan.guards.initialWork,snapshot);
 const reader=controllerPodState(snapshot);
 const target=plan.phaseTargets[phase],controllerDigest=fingerprint(snapshot.controller.spec),policies=requirePolicies(plan,snapshot,phase);
 let targetResource;
 if(phase==='policy-jobs')targetResource=snapshot.jobsPolicy;
 else if(phase==='policy-release')targetResource=snapshot.releasePolicy;
 else if(phase==='binding-release')targetResource=snapshot.releaseBinding;
 else targetResource=snapshot.controller;
 const targetStateValue=resourceState(targetResource,target,phase==='policy-jobs'?plan.jobsPolicyUID:phase==='policy-release'?plan.releasePolicyInitial?.uid:phase==='binding-release'?plan.releaseBindingInitial?.uid:plan.controllerUID);
 const controllerState=Object.entries(plan.controllerSpecs).find(([,spec])=>fingerprint(spec)===controllerDigest)?.[0]??'DRIFT';
 const requiredController=phase==='pause'?'initial':phase==='reader'?'pause':phase==='open'?'reader':'reader';
 if(targetStateValue==='BEFORE')requireValue(controllerState===requiredController,'CONTROLLER_PHASE_ORDER_REJECTED');
 if(phase!=='pause')idle(snapshot);
 if(['policy-jobs','policy-release','binding-release','open'].includes(phase)||phase==='reader'&&targetStateValue==='AFTER') {
  requireValue(controllerState==='reader'||phase==='open'&&targetStateValue==='AFTER'&&controllerState==='open','COMPATIBLE_HOLD_READER_REQUIRED');
  if(requireExecutable)requireValue(snapshot.controllerExecutableSHA256===plan.executableSHA256,'CONTROLLER_EXECUTABLE_MISMATCH');
 }
 return {phase,target:targetStateValue,controller:controllerState,reader,policies,work:workState(snapshot)};
}

export function mutationFor(plan,snapshot,phase) {
 const state=inspectPhase(plan,snapshot,phase,{requireExecutable:phase!=='pause'&&phase!=='reader'}),target=plan.phaseTargets[phase];
 requireValue(target.action!=='none','PHASE_ALREADY_DELIVERED');
 requireValue(state.target==='BEFORE','PHASE_ALREADY_APPLIED');
 if(target.action==='create')return {action:'create',resource:target.resource};
 const resource=phase==='policy-jobs'?snapshot.jobsPolicy:snapshot.controller;
 return {action:'patch',kind:target.kind,name:target.name,namespaced:target.namespaced,uid:resource.metadata.uid,
  resourceVersion:resource.metadata.resourceVersion,patch:[
   {op:'test',path:'/metadata/uid',value:resource.metadata.uid},
   {op:'test',path:'/metadata/resourceVersion',value:resource.metadata.resourceVersion},
   {op:'test',path:'/spec',value:target.before},
   {op:'replace',path:'/spec',value:target.after},
  ]};
}

export function phaseAfter(plan,snapshot,phase) {
 const result=inspectPhase(plan,snapshot,phase);
 requireValue(result.target==='AFTER','PHASE_NOT_APPLIED');
 if(phase==='pause')idle(snapshot);
 return result;
}

function rollbackPolicies(plan,snapshot) {
 return {
  primary:resourceState(snapshot.jobsPolicy,plan.rollbackTargets['rollback-policy-jobs'],plan.jobsPolicyUID),
  release:resourceState(snapshot.releasePolicy,plan.rollbackTargets['rollback-policy-release'],plan.releasePolicyInitial?.uid),
  binding:resourceState(snapshot.releaseBinding,plan.rollbackTargets['rollback-binding-release'],plan.releaseBindingInitial?.uid),
 };
}

export function inspectRollbackPhase(plan,snapshot,phase,{requireExecutable=true}={}) {
 validateDeliveryPlan(plan,plan.context,plan.k3sSudo);
 requireValue(rollbackPhases.includes(phase)&&snapshot.clusterUID===plan.clusterUID&&snapshot.namespaceUID===plan.namespaceUID&&
  snapshot.controller.metadata.uid===plan.controllerUID,'HOLD_DELIVERY_IDENTITY_CHANGED');
 exactNeighbors(plan.guards.neighbors,snapshot);
 exactPinned(snapshot.jobsBinding,plan.guards.jobsBinding,'JOBS_BINDING_CHANGED');
 exactPinned(snapshot.parameters,plan.guards.parameters,'POLICY_PARAMETERS_CHANGED');
 requireValue(snapshot.policyConfig.metadata.uid===plan.guards.policyConfig.uid&&snapshot.policyConfig.metadata.resourceVersion===plan.guards.policyConfig.resourceVersion&&
  fingerprint(snapshot.policyConfig.data)===plan.guards.policyConfig.specSHA256,'POLICY_CONFIG_CHANGED');
 ownerUnchanged(plan.guards.ownerState,snapshot.ownerState);idle(snapshot);workCompatible(plan.guards.initialWork,snapshot);
 const reader=controllerPodState(snapshot),policies=rollbackPolicies(plan,snapshot),target=plan.rollbackTargets[phase];
 let resource;
 if(phase==='rollback-policy-jobs')resource=snapshot.jobsPolicy;
 else if(phase==='rollback-policy-release')resource=snapshot.releasePolicy;
 else if(phase==='rollback-binding-release')resource=snapshot.releaseBinding;
 else resource=snapshot.controller;
 const controllerState=Object.entries(plan.controllerSpecs).find(([,spec])=>fingerprint(spec)===fingerprint(snapshot.controller.spec))?.[0]??'DRIFT';
 let state;
 if(phase==='rollback-pause')state=controllerState==='open'?'BEFORE':['reader','pause','initial'].includes(controllerState)?'AFTER':'DRIFT';
 else if(phase==='rollback-reader')state=controllerState==='reader'?'BEFORE':['pause','initial'].includes(controllerState)?'AFTER':'DRIFT';
 else if(phase==='rollback-open')state=controllerState==='pause'?'BEFORE':controllerState==='initial'?'AFTER':'DRIFT';
 else state=resourceState(resource,target,phase==='rollback-policy-jobs'?plan.jobsPolicyUID:
  phase==='rollback-policy-release'?plan.releasePolicyInitial?.uid:plan.releaseBindingInitial?.uid);
 requireValue(state!=='DRIFT','ROLLBACK_CONTROLLER_ORDER_REJECTED');
 if(['rollback-binding-release','rollback-policy-release','rollback-policy-jobs'].includes(phase))
  requireValue((state==='AFTER'&&['reader','pause','initial'].includes(controllerState))||(state==='BEFORE'&&controllerState==='reader'),'ROLLBACK_CONTROLLER_ORDER_REJECTED');
 if(phase==='rollback-policy-release')requireValue(policies.binding==='AFTER','ROLLBACK_BINDING_REQUIRED');
 if(phase==='rollback-policy-jobs')requireValue(policies.binding==='AFTER'&&policies.release==='AFTER','ROLLBACK_RELEASE_POLICY_REQUIRED');
 if(phase==='rollback-reader')requireValue(policies.primary==='AFTER'&&policies.release==='AFTER'&&policies.binding==='AFTER'&&['reader','pause','initial'].includes(controllerState),'ROLLBACK_POLICY_STATE_REQUIRED');
 if(phase==='rollback-open')requireValue(policies.primary==='AFTER'&&policies.release==='AFTER'&&policies.binding==='AFTER'&&['pause','initial'].includes(controllerState),'ROLLBACK_POLICY_STATE_REQUIRED');
 if(requireExecutable) {
  const expected=['reader','open'].includes(controllerState)?plan.executableSHA256:plan.controllerInitialExecutableSHA256;
  requireValue(snapshot.controllerExecutableSHA256===expected,'CONTROLLER_EXECUTABLE_MISMATCH');
 }
 return {phase,target:state,controller:controllerState,reader,policies,work:workState(snapshot)};
}

export function rollbackMutationFor(plan,snapshot,phase) {
 const state=inspectRollbackPhase(plan,snapshot,phase),target=plan.rollbackTargets[phase];
 requireValue(target.action!=='none'&&state.target==='BEFORE','ROLLBACK_PHASE_ALREADY_APPLIED');
 let resource;
 if(phase==='rollback-policy-jobs')resource=snapshot.jobsPolicy;
 else if(phase==='rollback-policy-release')resource=snapshot.releasePolicy;
 else if(phase==='rollback-binding-release')resource=snapshot.releaseBinding;
 else resource=snapshot.controller;
 if(target.action==='delete')return {action:'delete',kind:target.kind,name:target.name,namespaced:target.namespaced,
  uid:resource.metadata.uid,resourceVersion:resource.metadata.resourceVersion};
 return {action:'patch',kind:target.kind,name:target.name,namespaced:target.namespaced,uid:resource.metadata.uid,
  resourceVersion:resource.metadata.resourceVersion,patch:[
   {op:'test',path:'/metadata/uid',value:resource.metadata.uid},
   {op:'test',path:'/metadata/resourceVersion',value:resource.metadata.resourceVersion},
   {op:'test',path:'/spec',value:target.before},
   {op:'replace',path:'/spec',value:target.after},
  ]};
}

export function rollbackPhaseAfter(plan,snapshot,phase) {
 const result=inspectRollbackPhase(plan,snapshot,phase);
 requireValue(result.target==='AFTER','ROLLBACK_PHASE_NOT_APPLIED');return result;
}
