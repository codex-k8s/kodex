#!/usr/bin/env node
import {materializeAuthorityJobDefaults} from './authority-job-defaults.mjs';
import {execFileSync} from 'node:child_process';
import {createHash,randomUUID} from 'node:crypto';
import {closeSync, constants, fstatSync, fsyncSync, openSync, readFileSync, writeFileSync, writeSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {inspectSource} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityExecutable} from './authority-executable-readback.mjs';
import {authorityGoImage,createSourceMigrationJob,validateRenderedSourceMigration,validateSourceDeliveryPlan,validateSourceMigrationReceipt,validateSourcePublisher,verifySourceMigrationReadback} from './authority-rotation-source-delivery-model.mjs';
import {readSourceMigrationReceipt} from './authority-source-migration-receipt.mjs';
import {workloadPods} from '../dev/component-manifest.mjs';

const namespace='kodex-system';
const uuid=/^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/;
const sha=/^[a-f0-9]{64}$/;
const revision=/^[1-9][0-9]{0,15}$/;
const policySHA256='763028a7176c8c3394d0a01686b8d66a3a7cc465af90c2480a06064816b5e504';
function requireValue(ok,code){if(!ok)throw new Error(code);}
const run=(command,args,input)=>execFileSync(command,args,{input,encoding:'utf8',stdio:['pipe','pipe','pipe'],timeout:30_000,maxBuffer:16<<20}).trim();
export function deriveRotationOperationID(sourceRevision,sourceDigestSHA256){
	requireValue(revision.test(String(sourceRevision))&&Number(sourceRevision)<=9007199254740991&&sha.test(sourceDigestSHA256),'INVALID_ROTATION_REGISTRY_IDENTITY');
	const digest=createHash('sha256').update(`authority-normal-rotation-v3\0${sourceRevision}\0${sourceDigestSHA256}`).digest();
	digest[6]=(digest[6]&0x0f)|0x50;digest[8]=(digest[8]&0x3f)|0x80;
	const value=digest.subarray(0,16).toString('hex');
	return `${value.slice(0,8)}-${value.slice(8,12)}-${value.slice(12,16)}-${value.slice(16,20)}-${value.slice(20)}`;
}
export function deriveRegistryRotationIdentity(raw){
	const sourceRevision=Number(run('yq',['-r','.source_revision','-'],raw));
	const sourceDigestSHA256=createHash('sha256').update(raw).digest('hex');
	return {sourceRevision,sourceDigestSHA256,ownerOperationID:deriveRotationOperationID(sourceRevision,sourceDigestSHA256)};
}
export function deriveCanonicalRegistryAdvance(liveRaw,canonicalRaw){
 const previous=deriveRegistryRotationIdentity(liveRaw),canonical=JSON.parse(run('yq',['-o=json','-I=0','.','-'],canonicalRaw));
 requireValue(canonical?.version===1&&Array.isArray(canonical.targets)&&canonical.targets.length===16&&
  new Set(canonical.targets.map(target=>`${target.workload_id}\0${target.role}`)).size===16&&!canonical.targets.some(target=>target.workload_id==='interaction-gateway'),'EXACT_BASE_ROTATION_REGISTRY_REQUIRED');
 canonical.source_revision=previous.sourceRevision+1;
 const raw=run('yq',['-o=yaml','.', '-'],JSON.stringify(canonical))+'\n',identity=deriveRegistryRotationIdentity(raw);
 requireValue(identity.sourceRevision===previous.sourceRevision+1,'ROTATION_REGISTRY_REVISION_MUST_ADVANCE');return {raw,previous,identity};
}
export function canonicalRegistryMatches(raw,canonicalRaw){
 const actual=JSON.parse(run('yq',['-o=json','-I=0','.','-'],raw)),canonical=JSON.parse(run('yq',['-o=json','-I=0','.','-'],canonicalRaw));canonical.source_revision=actual.source_revision;
 return fingerprint(actual)===fingerprint(canonical);
}
export function validateRotationPolicy(raw){
 requireValue(createHash('sha256').update(raw).digest('hex')===policySHA256,'EXACT_ROTATION_POLICY_REQUIRED');
 const policy=JSON.parse(raw),matches=policy?.policy?.operation_bindings?.filter(binding=>binding.operation_id==='platform.command.integration-definitions.create-draft');
 requireValue(policy.v===1&&policy.policy_revision===77&&policy.policy?.authority_abi_version===2&&matches?.length===1&&matches[0].project_required===false&&
  matches[0].request_profile?.resource==='FORBIDDEN','ROTATION_POLICY_BINDING_REJECTED');return {revision:77,sha256:policySHA256};
}
export function validatePredecessorPolicy(raw){
 const policy=JSON.parse(raw),matches=policy?.policy?.operation_bindings?.filter(binding=>binding.operation_id==='platform.command.integration-definitions.create-draft');
 const transition=policy.v===1&&policy.policy_revision===76&&policy.policy?.authority_abi_version===2&&matches?.length===1&&matches[0].project_required===true;
 const current=policy.policy_revision===77&&createHash('sha256').update(raw).digest('hex')===policySHA256;
 requireValue(transition||current,'ROTATION_PREDECESSOR_POLICY_REJECTED');return {revision:policy.policy_revision,sha256:createHash('sha256').update(raw).digest('hex')};
}
function validateRotationTemplate(job){
 const pod=job.spec?.template?.spec,container=pod?.containers?.[0];
 const immutable=container?.command?.length===1&&container.command[0]==='/usr/local/bin/internal-rpc-authority-cli'&&/^ghcr\.io\/codex-k8s\/kodex\/internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(container.image??'');
 const source=container?.image===authorityGoImage&&fingerprint(container.command)===fingerprint(['/workspace/tools/dev/run-go-command.sh'])&&
  fingerprint(container.args)===fingerprint(['services/internal/internal-rpc-authority','./cmd/cli','up']);
 requireValue(job.kind==='Job'&&job.metadata?.namespace===namespace&&pod?.restartPolicy==='Never'&&
  pod.serviceAccountName==='internal-rpc-authority-migrator'&&pod.automountServiceAccountToken===false&&
	 job.spec.template.metadata?.labels?.['app.kubernetes.io/name']==='internal-rpc-authority'&&
	 job.spec.template.metadata?.labels?.['app.kubernetes.io/component']==='migrator'&&
  pod.containers?.length===1&&container.name==='migrate'&&(immutable||source)&&
  container.volumeMounts?.some(mount=>mount.name==='postgresql-credentials'&&mount.readOnly===true)&&
	 container.volumeMounts?.some(mount=>mount.name==='postgresql-ca'&&mount.readOnly===true)&&
  pod.volumes?.some(volume=>volume.name==='postgresql-credentials'&&volume.secret?.secretName==='internal-rpc-authority-postgres-migration'),
 'EXACT_ROTATION_CLI_TEMPLATE_REQUIRED');
 if(source)validateRenderedSourceMigration({...job,metadata:{...job.metadata,name:'internal-rpc-authority-migrate'}});
}

export function validateRotationPrerequisites(serviceAccount,egressPolicy,ingressPolicy){
	const labels={'app.kubernetes.io/name':'internal-rpc-authority','app.kubernetes.io/component':'migrator'};
	const requiredLabels=value=>Object.entries(labels).every(([key,expected])=>value?.[key]===expected);
	const dns={to:[{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':'kube-system'}},podSelector:{matchLabels:{'k8s-app':'kube-dns'}}}],ports:[{port:53,protocol:'UDP'},{port:53,protocol:'TCP'}]};
	const postgres={to:[{podSelector:{matchLabels:{'app.kubernetes.io/name':'kodex-postgresql'}}}],ports:[{port:5432,protocol:'TCP'}]};
	const egressSpec=normalizeRotationPrerequisiteSpec(egressPolicy),ingressSpec=normalizeRotationPrerequisiteSpec(ingressPolicy);
	const egressRuleDigests=Array.isArray(egressSpec?.egress)?egressSpec.egress.map(fingerprint).sort():[];
	requireValue(serviceAccount?.kind==='ServiceAccount'&&serviceAccount.metadata?.name==='internal-rpc-authority-migrator'&&
	 serviceAccount.automountServiceAccountToken===false&&requiredLabels(serviceAccount.metadata.labels)&&
	 egressPolicy?.kind==='NetworkPolicy'&&egressPolicy.metadata?.name==='internal-rpc-authority-migrator'&&
	 fingerprint(egressSpec?.podSelector)===fingerprint({matchLabels:labels})&&fingerprint(egressSpec?.policyTypes)===fingerprint(['Ingress','Egress'])&&
	 fingerprint(egressSpec?.ingress)===fingerprint([])&&Object.keys(egressSpec??{}).sort().join(',')==='egress,ingress,podSelector,policyTypes'&&
	 fingerprint(egressRuleDigests)===fingerprint([fingerprint(dns),fingerprint(postgres)].sort())&&
	 ingressPolicy?.kind==='NetworkPolicy'&&ingressPolicy.metadata?.name==='internal-rpc-authority-postgresql-from-migrator'&&
	 fingerprint(ingressSpec)===fingerprint({podSelector:{matchLabels:{'app.kubernetes.io/name':'kodex-postgresql'}},policyTypes:['Ingress'],ingress:[{from:[{podSelector:{matchLabels:labels}}],ports:[{port:5432,protocol:'TCP'}]}]}),
	 'ROTATION_JOB_PREREQUISITES_REJECTED');
}

export function normalizeRotationPrerequisiteSpec(policy){
	const spec=structuredClone(policy?.spec);if(policy?.metadata?.name==='internal-rpc-authority-migrator'&&spec&&!Object.hasOwn(spec,'ingress'))spec.ingress=[];
 // Renderer записывает пустой egress даже для ingress-only policy; API его
 // опускает. Непустое/null поле и изменение policyTypes не нормализуются.
 if(policy?.kind==='NetworkPolicy'&&policy.metadata?.name==='internal-rpc-authority-postgresql-from-migrator'&&
  fingerprint(spec?.policyTypes)===fingerprint(['Ingress'])&&Array.isArray(spec?.egress)&&spec.egress.length===0)delete spec.egress;
 return spec;
}

export function rotationPrerequisiteSpecSHA256(policy){return fingerprint(normalizeRotationPrerequisiteSpec(policy));}

export function createRotationJob(template,plan){
 validateRotationTemplate(template);
 requireValue(plan.version===3&&uuid.test(plan.intentID)&&['status','abort','rotate'].includes(plan.action)&&/^[a-f0-9]{40}$/.test(plan.revision)&&
  /^\/srv\/kodex-dev\/[A-Za-z0-9._-]+$/.test(plan.source),'INVALID_ROTATION_PLAN');
	if(plan.action==='rotate')requireValue(uuid.test(plan.ownerOperationID)&&plan.ownerOperationID!==plan.intentID,'INVALID_ROTATION_OWNER_OPERATION');
 if(plan.action==='abort')requireValue(uuid.test(plan.rotation.intentID)&&revision.test(String(plan.rotation.sourceRevision))&&
  Number(plan.rotation.sourceRevision)<=9007199254740991&&sha.test(plan.rotation.sourceDigestSHA256),'INVALID_ROTATION_ABORT');
 const spec=structuredClone(template.spec);delete spec.selector;delete spec.ttlSecondsAfterFinished;
 spec.backoffLimit=0;spec.activeDeadlineSeconds=300;
 materializeAuthorityJobDefaults(spec);
 for(const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name'])delete spec.template.metadata?.labels?.[key];
 const sourceProfile=spec.template.spec.containers[0].command?.[0]==='/workspace/tools/dev/run-go-command.sh';
 const args=[...(sourceProfile?['services/internal/internal-rpc-authority','./cmd/cli']:[]),plan.action==='abort'?'rotation-abort':plan.action==='rotate'?'rotation-watch':'rotation-status'];
 if(plan.action==='rotate')args.push('--operation-id',plan.ownerOperationID);
 if(plan.action==='abort')args.push('--intent-id',plan.rotation.intentID,'--source-revision',String(plan.rotation.sourceRevision),
  '--source-digest-sha256',plan.rotation.sourceDigestSHA256,'--confirm','ABORT-STAGING-AUTHORITY-ROTATION');
 spec.template.spec.containers[0].args=args;
 const annotations={'kodex.dev/rotation-intent':plan.intentID,'kodex.dev/rotation-plan-sha256':fingerprint(plan)};
	if(plan.action==='rotate')annotations['kodex.dev/rotation-operation']=plan.ownerOperationID;
 return {apiVersion:'batch/v1',kind:'Job',metadata:{name:`authority-rotation-${plan.intentID}`,namespace,
  labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging'},annotations:{
   ...annotations}},spec};
}

export function createCanonicalRotationTemplate(source,image){
 requireValue(/^ghcr\.io\/codex-k8s\/kodex\/internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(image),'EXACT_CLI_IMAGE_REQUIRED');
	const path=`${source}/deploy/k8s/base/internal-rpc-authority-data/migration-job.yaml`;
	const canonical=JSON.parse(run('yq',['-o=json','-I=0','.',path]));
	requireValue(canonical.metadata?.name==='internal-rpc-authority-migrate','EXACT_CLI_TEMPLATE_SOURCE_REQUIRED');
	canonical.metadata.namespace=namespace;
	canonical.spec.backoffLimit=0;delete canonical.spec.ttlSecondsAfterFinished;
	canonical.spec.template.spec.containers[0].image=image;
	canonical.spec.template.spec.containers[0].args=['rotation-status'];
	return canonical;
}
export function createSourceRotationTemplate(rendered,source){
 validateRenderedSourceMigration(rendered);const template=structuredClone(rendered);template.metadata.namespace=namespace;
 template.spec.template.spec.volumes.find(volume=>volume.name==='dev-source').hostPath.path=source;validateRotationTemplate(template);return template;
}
export function validateSourceMigrationPrerequisite(sourceDelivery,receipt,actual){
 validateSourceDeliveryPlan(sourceDelivery);const expected=createSourceMigrationJob(sourceDelivery.migration.rendered,sourceDelivery);validateSourceMigrationReceipt(receipt,expected,sourceDelivery);
 verifySourceMigrationReadback(actual,expected,receipt);requireValue(actual.status?.succeeded===1,'SOURCE_DELIVERY_MIGRATION_NOT_SUCCEEDED');return expected;
}

export function createRegistryCAS(current,plan){
 requireValue(current?.kind==='ConfigMap'&&current.metadata?.uid===plan.registry.uid&&
  current.metadata?.resourceVersion===plan.registry.resourceVersion&&
  fingerprint(current.data)===plan.registry.currentDataSHA256&&
	 createHash('sha256').update(current.data?.['authority-policy.json']??'').digest('hex')===plan.registry.previousPolicySHA256&&
	 validatePredecessorPolicy(current.data?.['authority-policy.json']).revision===plan.registry.previousPolicyRevision&&
	 deriveRegistryRotationIdentity(current.data?.['key-delivery-targets.yaml']).sourceRevision===plan.registry.previousSourceRevision&&
	 deriveRegistryRotationIdentity(current.data?.['key-delivery-targets.yaml']).sourceDigestSHA256===plan.registry.previousSourceDigestSHA256&&
  validateRotationPolicy(plan.registry.desiredData?.['authority-policy.json']).sha256===plan.registry.policySHA256,
	 'ROTATION_REGISTRY_CAS_DRIFT');
 const desired=structuredClone(current);delete desired.status;
 for(const key of ['managedFields','creationTimestamp','generation'])delete desired.metadata[key];
 desired.data=structuredClone(plan.registry.desiredData);
 desired.metadata.annotations={...desired.metadata.annotations,'kodex.dev/rotation-intent':plan.intentID,
  'kodex.dev/rotation-operation':plan.ownerOperationID,
  'kodex.dev/rotation-registry-sha256':plan.registry.desiredDataSHA256};
 return desired;
}

export function createPublisherRestart(current,plan){
 requireValue(current?.kind==='Deployment'&&current.metadata?.uid===plan.publisher.uid&&
  current.metadata?.resourceVersion===plan.publisher.resourceVersion&&
  fingerprint(current.spec)===plan.publisher.specSHA256,'ROTATION_PUBLISHER_CAS_DRIFT');
 const desired=structuredClone(current);delete desired.status;
 for(const key of ['managedFields','creationTimestamp','generation'])delete desired.metadata[key];
 desired.spec.template.metadata.annotations={...desired.spec.template.metadata.annotations,
  'kodex.dev/authority-rotation-intent':plan.intentID,
  'kodex.dev/authority-rotation-operation':plan.ownerOperationID};
 return desired;
}

export function verifyRotationJobReadback(job,expected){
 requireValue(job.metadata?.namespace===namespace&&job.metadata.name===expected.metadata.name&&
  job.metadata.annotations?.['kodex.dev/rotation-intent']===expected.metadata.annotations['kodex.dev/rotation-intent']&&
  job.metadata.annotations?.['kodex.dev/rotation-operation']===expected.metadata.annotations['kodex.dev/rotation-operation']&&
  job.metadata.annotations?.['kodex.dev/rotation-plan-sha256']===expected.metadata.annotations['kodex.dev/rotation-plan-sha256'],'ROTATION_JOB_IDENTITY_MISMATCH');
 const actual=structuredClone(job.spec),wanted=structuredClone(expected.spec);delete actual.selector;
 for(const value of [actual,wanted])for(const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name'])delete value.template.metadata?.labels?.[key];
 requireValue(fingerprint(actual)===fingerprint(wanted),'ROTATION_JOB_SPEC_DRIFT');
}

export function classifyRotationStatus(job,log){
 const phase=job.status?.succeeded===1?'SUCCEEDED':job.status?.failed?'FAILED':'RUNNING';
 if(phase!=='SUCCEEDED')return {phase};
 const rows=log.trim().split('\n').filter(line=>line.startsWith('{')).map(line=>JSON.parse(line));const state=rows.at(-1);
 const statuses=['EMPTY','PREPARED','DELIVERING','DELIVERED','PROMOTED','RETIRED','ABORTED','DISTRIBUTING','WAITING_SWITCH','SWITCHING','WAITING_RETIRE','RETIRING'];
 requireValue(state&&Number.isFinite(Date.parse(state.observedAt))&&statuses.includes(state.status),'ROTATION_READBACK_REQUIRED');
 if(state.operationId)requireValue(uuid.test(state.operationId)&&state.operationStatus===state.status&&Number.isSafeInteger(state.registryRevision)&&state.registryRevision>0&&
  Number.isSafeInteger(state.baseRevision)&&state.baseRevision>0&&sha.test(state.registrySourceDigestSHA256)&&sha.test(state.baseDigestSHA256)&&
  (!state.phase||['DISTRIBUTE','SWITCH','RETIRE'].includes(state.phase)),'ROTATION_OPERATION_READBACK_REJECTED');
 if(state.operationId&&['WAITING_SWITCH','SWITCHING','WAITING_RETIRE','RETIRING','RETIRED'].includes(state.status))
  requireValue(Number.isFinite(Date.parse(state.switchNotBefore)),'ROTATION_OPERATION_DEADLINE_REJECTED');
 if(state.operationId&&['SWITCHING','WAITING_RETIRE','RETIRING','RETIRED'].includes(state.status))
  requireValue(Number.isFinite(Date.parse(state.previousNotAfter)),'ROTATION_OPERATION_DEADLINE_REJECTED');
 if(state.operationId&&state.status==='RETIRED')requireValue(Number.isFinite(Date.parse(state.completedAt)),'ROTATION_OPERATION_DEADLINE_REJECTED');
 if(!state.operationId&&state.status!=='EMPTY')requireValue(uuid.test(state.intentId)&&[1,2].includes(state.protocolVersion)&&Number.isSafeInteger(state.sourceRevision)&&state.sourceRevision>0&&sha.test(state.sourceDigestSHA256),'ROTATION_READBACK_REJECTED');
 return {phase,state};
}
function sourcePublisherExecutable(deployment,get,kube,k3sSudo){
 const resources=get('replicasets,pods',null).items,pods=workloadPods(deployment,resources).filter(p=>!p.metadata.deletionTimestamp);
 requireValue(deployment.status?.observedGeneration>=deployment.metadata.generation&&deployment.status?.availableReplicas===deployment.spec.replicas&&pods.length===deployment.spec.replicas,'SOURCE_PUBLISHER_NOT_STABLE');
 const proofs=pods.map(pod=>{const container=pod.spec.containers.find(c=>c.name==='publisher'),status=pod.status.containerStatuses?.find(c=>c.name==='publisher');requireValue(status?.ready&&status.state?.running,'SOURCE_PUBLISHER_NOT_READY');return readAuthorityExecutable(pod,container,{kube,k3sSudo});});
 requireValue(proofs.length>0&&new Set(proofs).size===1,'SOURCE_PUBLISHER_EXECUTABLES_DIFFER');return proofs[0];
}

async function main(args){
 const command=args.shift(),options={};requireValue(['plan','apply','observe','resume'].includes(command),'INVALID_COMMAND');
 while(args.length){const key=args.shift();requireValue(['--context','--source','--revision','--action','--registry-file','--source-delivery-plan','--source-migration-receipt','--output','--plan','--evidence','--intent-id','--source-revision','--source-digest-sha256','--confirm','--k3s-sudo'].includes(key)&&!options[key],'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift();}
 requireValue(options['--context']&&!/prod/i.test(options['--context']),'EXACT_STAGING_CONTEXT_REQUIRED');
 const kube=(...argv)=>options['--k3s-sudo']?run('sudo',['-n','k3s','kubectl','--context',options['--context'],...argv]):run('kubectl',['--context',options['--context'],...argv]);
	const kubeInput=(input,...argv)=>options['--k3s-sudo']?run('sudo',['-n','k3s','kubectl','--context',options['--context'],...argv],input):run('kubectl',['--context',options['--context'],...argv],input);
	const replace=value=>options['--k3s-sudo']?
	 run('sudo',['-n','k3s','kubectl','--context',options['--context'],'replace','-f','-'],JSON.stringify(value)):
	 run('kubectl',['--context',options['--context'],'replace','-f','-'],JSON.stringify(value));
 requireValue(kube('config','current-context')===options['--context'],'CONTEXT_MISMATCH');
 const get=(kind,name)=>JSON.parse(kube('get',kind,...(name?[name]:[]),'-n',namespace,'-o','json'));
 const ns=get('namespace',namespace);requireValue(ns.metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&ns.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_NAMESPACE_REQUIRED');
	validateRotationPrerequisites(get('serviceaccount','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-postgresql-from-migrator'));
	requireValue(kube('auth','can-i','create','jobs','-n',namespace)==='yes'&&
	 kube('get','secret','internal-rpc-authority-postgres-migration','-n',namespace,'-o','name')==='secret/internal-rpc-authority-postgres-migration'&&
	 kube('get','configmap','internal-rpc-authority-postgresql-ca','-n',namespace,'-o','name')==='configmap/internal-rpc-authority-postgresql-ca','ROTATION_JOB_API_PREREQUISITES_REJECTED');
 const livePublisher=get('deployment','internal-rpc-authority-publisher');
 const publisherContainer=livePublisher.spec.template.spec.containers.find(container=>container.name==='publisher');
 const loadedPlan=command==='plan'?null:JSON.parse(readFileSync(options['--plan'],'utf8'));
 const sourceProfile=publisherContainer?.command?.includes('/workspace/tools/dev/run-go-hot-reload.sh');let sourceDelivery,sourceMigrationReceipt;
 if(sourceProfile){if(command==='plan')requireValue(options['--source-delivery-plan']&&options['--source-migration-receipt'],'SOURCE_DELIVERY_EVIDENCE_REQUIRED');const publisherState=validateSourcePublisher(livePublisher);sourceDelivery=command==='plan'?JSON.parse(readFileSync(options['--source-delivery-plan'],'utf8')):loadedPlan.sourceDelivery;validateSourceDeliveryPlan(sourceDelivery);
  const sourceMigration=createSourceMigrationJob(sourceDelivery.migration.rendered,sourceDelivery);sourceMigrationReceipt=command==='plan'?readSourceMigrationReceipt(options['--source-migration-receipt'],sourceMigration,sourceDelivery):validateSourceMigrationReceipt(loadedPlan.sourceMigrationReceipt,sourceMigration,sourceDelivery);
  validateSourceMigrationPrerequisite(sourceDelivery,sourceMigrationReceipt,get('job',sourceMigration.metadata.name));
  const publisherSpecSHA256=fingerprint(livePublisher.spec),allowedPublisherSpec=publisherSpecSHA256===sourceDelivery.publisher.desiredSpecSHA256||
   (command!=='plan'&&loadedPlan.action==='rotate'&&publisherSpecSHA256===loadedPlan.publisher?.desiredSpecSHA256);
  requireValue(publisherState.source===sourceDelivery.source&&allowedPublisherSpec&&
   sourcePublisherExecutable(livePublisher,get,kube,options['--k3s-sudo'])===sourceDelivery.capability.publisherSHA256,'SOURCE_DELIVERY_NOT_ACTIVE');
 }else requireValue(publisherContainer?.command?.length===1&&publisherContainer.command[0]==='/usr/local/bin/internal-rpc-authority-publisher'&&
  /^ghcr\.io\/codex-k8s\/kodex\/internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(publisherContainer.image),'EXACT_PUBLISHER_EXECUTABLE_REQUIRED');
	const source=command==='plan'?options['--source']:loadedPlan.source;
	if(sourceProfile)requireValue(source===sourceDelivery.source&&(command!=='plan'||options['--revision']===sourceDelivery.revision),'SOURCE_DELIVERY_PLAN_DRIFT');
	const template=sourceProfile?createSourceRotationTemplate(sourceDelivery.migration.rendered,source):createCanonicalRotationTemplate(source,publisherContainer.image);validateRotationTemplate(template);
 let plan;
 if(command==='plan'){
  requireValue(['status','abort','rotate'].includes(options['--action']),'ROTATION_ACTION_REQUIRED');
  plan={version:3,intentID:randomUUID(),context:options['--context'],namespaceUID:ns.metadata.uid,
   templateSpecSHA256:fingerprint(template.spec),cliImage:template.spec.template.spec.containers[0].image,
   cliCommand:template.spec.template.spec.containers[0].command,source:options['--source'],revision:options['--revision'],action:options['--action'],
   ...(sourceProfile?{sourceDelivery,sourceDeliveryPlanSHA256:fingerprint(sourceDelivery),sourceMigrationReceipt,sourceMigrationReceiptSHA256:fingerprint(sourceMigrationReceipt),publisherExecutableSHA256:sourceDelivery.capability.publisherSHA256}:{})};
  if(plan.action==='rotate'){
   const liveRegistry=get('configmap','internal-rpc-authority-publisher-target-registry');
   const registryFile=`${plan.source}/deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml`;
   requireValue(!options['--registry-file']||options['--registry-file']===registryFile,'EXACT_ROTATION_REGISTRY_FILE_REQUIRED');
   const policyFile=`${plan.source}/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json`,policyRaw=readFileSync(policyFile,'utf8');validateRotationPolicy(policyRaw);const previousPolicy=validatePredecessorPolicy(liveRegistry.data?.['authority-policy.json']);
   const advance=deriveCanonicalRegistryAdvance(liveRegistry.data?.['key-delivery-targets.yaml'],readFileSync(registryFile,'utf8'));
   const desiredData=structuredClone(liveRegistry.data);desiredData['key-delivery-targets.yaml']=advance.raw;desiredData['authority-policy.json']=policyRaw;
   requireValue(fingerprint(liveRegistry.data)!==fingerprint(desiredData),'ROTATION_REGISTRY_MUST_ADVANCE');
	   const previousIdentity=advance.previous;
	   const identity=deriveRegistryRotationIdentity(desiredData['key-delivery-targets.yaml']);
	   requireValue(identity.sourceRevision===previousIdentity.sourceRevision+1,'ROTATION_REGISTRY_REVISION_MUST_ADVANCE');
	   plan.ownerOperationID=identity.ownerOperationID;
	   plan.registry={uid:liveRegistry.metadata.uid,resourceVersion:liveRegistry.metadata.resourceVersion,
	    currentDataSHA256:fingerprint(liveRegistry.data),desiredData,desiredDataSHA256:fingerprint(desiredData),sourceFile:registryFile.slice(plan.source.length+1),
	    previousSourceRevision:previousIdentity.sourceRevision,previousSourceDigestSHA256:previousIdentity.sourceDigestSHA256,
	    sourceRevision:identity.sourceRevision,sourceDigestSHA256:identity.sourceDigestSHA256,
      policySourceFile:policyFile.slice(plan.source.length+1),previousPolicyRevision:previousPolicy.revision,previousPolicySHA256:previousPolicy.sha256,policyRevision:77,policySHA256};
   plan.publisher={uid:livePublisher.metadata.uid,resourceVersion:livePublisher.metadata.resourceVersion,
    specSHA256:fingerprint(livePublisher.spec),image:livePublisher.spec.template.spec.containers.find(container=>container.name==='publisher').image,
    command:livePublisher.spec.template.spec.containers.find(container=>container.name==='publisher').command};
	  plan.publisher.desiredSpecSHA256=fingerprint(createPublisherRestart(livePublisher,plan).spec);
  }
  if(plan.action==='abort')plan.rotation={intentID:options['--intent-id'],sourceRevision:Number(options['--source-revision']),sourceDigestSHA256:options['--source-digest-sha256']};
 }else plan=loadedPlan;
 requireValue(plan.context===options['--context']&&plan.namespaceUID===ns.metadata.uid&&
  plan.templateSpecSHA256===fingerprint(template.spec)&&plan.cliImage===template.spec.template.spec.containers[0].image&&
  fingerprint(plan.cliCommand)===fingerprint(template.spec.template.spec.containers[0].command),'ROTATION_PLAN_DRIFT');
	if(plan.action==='rotate')requireValue(uuid.test(plan.registry?.uid)&&String(plan.registry.resourceVersion).length>0&&sha.test(plan.registry.currentDataSHA256)&&
	 sha.test(plan.registry.desiredDataSHA256)&&fingerprint(plan.registry.desiredData)===plan.registry.desiredDataSHA256&&
	 typeof plan.registry.desiredData?.['key-delivery-targets.yaml']==='string'&&
	 typeof plan.registry.desiredData?.['authority-policy.json']==='string'&&validateRotationPolicy(plan.registry.desiredData['authority-policy.json']).revision===plan.registry.policyRevision&&
	 plan.registry.policySHA256===policySHA256&&[76,77].includes(plan.registry.previousPolicyRevision)&&sha.test(plan.registry.previousPolicySHA256)&&plan.registry.policySourceFile==='deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json'&&
	 deriveRegistryRotationIdentity(plan.registry.desiredData['key-delivery-targets.yaml']).sourceRevision===plan.registry.sourceRevision&&
	 deriveRegistryRotationIdentity(plan.registry.desiredData['key-delivery-targets.yaml']).sourceDigestSHA256===plan.registry.sourceDigestSHA256&&
	 deriveRegistryRotationIdentity(plan.registry.desiredData['key-delivery-targets.yaml']).ownerOperationID===plan.ownerOperationID&&
	 revision.test(String(plan.registry.previousSourceRevision))&&sha.test(plan.registry.previousSourceDigestSHA256)&&
	 plan.registry.sourceRevision===plan.registry.previousSourceRevision+1&&
	 plan.registry.sourceFile==='deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml'&&
	 uuid.test(plan.publisher?.uid)&&String(plan.publisher.resourceVersion).length>0&&sha.test(plan.publisher.specSHA256)&&sha.test(plan.publisher.desiredSpecSHA256)&&
	 plan.publisher.image===plan.cliImage&&fingerprint(plan.publisher.command)===fingerprint(sourceProfile?['/workspace/tools/dev/run-go-hot-reload.sh']:['/usr/local/bin/internal-rpc-authority-publisher'])&&
  (!sourceProfile||(plan.sourceDeliveryPlanSHA256===fingerprint(sourceDelivery)&&plan.sourceMigrationReceiptSHA256===fingerprint(sourceMigrationReceipt)&&plan.publisherExecutableSHA256===sourceDelivery.capability.publisherSHA256)),'ROTATION_PLAN_DRIFT');
 requireValue(inspectSource(plan.source).revision===plan.revision,'EXACT_SOURCE_REQUIRED');const job=createRotationJob(template,plan);
	if(plan.action==='rotate'){
   requireValue(canonicalRegistryMatches(plan.registry.desiredData['key-delivery-targets.yaml'],readFileSync(`${plan.source}/${plan.registry.sourceFile}`,'utf8'))&&
    readFileSync(`${plan.source}/${plan.registry.policySourceFile}`,'utf8')===plan.registry.desiredData['authority-policy.json'],'ROTATION_REGISTRY_SOURCE_DRIFT');
  }
	if(command==='plan'){
	 const dryRun=JSON.parse(kubeInput(JSON.stringify(job),'create','--dry-run=server','-f','-','-o','json'));
	 verifyRotationJobReadback(dryRun,job);
	 writeFileSync(options['--output'],JSON.stringify(plan,null,2)+'\n',{flag:'wx',mode:0o600});process.stdout.write(`Authority rotation plan: ${fingerprint(plan)}\n`);return;
	}
 let fd;const evidence=record=>{if(fd===undefined)return;writeSync(fd,JSON.stringify({at:new Date().toISOString(),intentID:plan.intentID,ownerOperationID:plan.ownerOperationID,...record})+'\n');fsyncSync(fd);};
	 if(command==='apply'){
  requireValue(options['--confirm']==='APPLY-STAGING-AUTHORITY-ROTATION','STAGING_CONFIRMATION_REQUIRED');
  requireValue(!kube('get','job',job.metadata.name,'-n',namespace,'--ignore-not-found','-o','json'),'EXISTING_OPERATION_REQUIRES_RESUME');
  fd=openSync(options['--evidence'],'wx',0o600);evidence({status:'INTENT',action:plan.action,planSHA256:fingerprint(plan)});
	 }
	 if(command==='resume'){
	  requireValue(options['--evidence'],'ROTATION_EVIDENCE_REQUIRED');
	  fd=openSync(options['--evidence'],constants.O_APPEND|constants.O_WRONLY|constants.O_NOFOLLOW);
	  const evidenceStat=fstatSync(fd),expectedUID=process.getuid?.();
	  requireValue(evidenceStat.isFile()&&(evidenceStat.mode&0o777)===0o600&&
	   (expectedUID===undefined||evidenceStat.uid===expectedUID),'ROTATION_EVIDENCE_REJECTED');
	  evidence({status:'RESUME',action:plan.action,planSHA256:fingerprint(plan)});
	 }
	 if((command==='apply'||command==='resume')&&plan.action==='rotate'){
   let liveRegistry=get('configmap','internal-rpc-authority-publisher-target-registry');
   if(fingerprint(liveRegistry.data)!==plan.registry.desiredDataSHA256){
    const desired=createRegistryCAS(liveRegistry,plan);evidence({status:'INTENT',operation:'REGISTRY_CAS'});
    try{replace(desired);}
    catch{evidence({status:'UNKNOWN',operation:'REGISTRY_CAS'});}
    liveRegistry=get('configmap','internal-rpc-authority-publisher-target-registry');
   }
   requireValue(liveRegistry.metadata.uid===plan.registry.uid&&fingerprint(liveRegistry.data)===plan.registry.desiredDataSHA256&&
    liveRegistry.metadata.annotations?.['kodex.dev/rotation-intent']===plan.intentID&&
    liveRegistry.metadata.annotations?.['kodex.dev/rotation-operation']===plan.ownerOperationID,'ROTATION_REGISTRY_READBACK_REJECTED');
   evidence({status:'APPLIED',operation:'REGISTRY_CAS',resourceVersion:liveRegistry.metadata.resourceVersion});
   let livePublisher=get('deployment','internal-rpc-authority-publisher');
   if(livePublisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-intent']!==plan.intentID||
    livePublisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-operation']!==plan.ownerOperationID){
    const desired=createPublisherRestart(livePublisher,plan);evidence({status:'INTENT',operation:'PUBLISHER_RESTART'});
    try{replace(desired);}
    catch{evidence({status:'UNKNOWN',operation:'PUBLISHER_RESTART'});}
    livePublisher=get('deployment','internal-rpc-authority-publisher');
   }
   requireValue(livePublisher.metadata.uid===plan.publisher.uid&&livePublisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-intent']===plan.intentID&&
    livePublisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-operation']===plan.ownerOperationID&&
    fingerprint(livePublisher.spec)===plan.publisher.desiredSpecSHA256&&
    livePublisher.spec.template.spec.containers.some(container=>container.name==='publisher'&&container.image===plan.publisher.image&&fingerprint(container.command)===fingerprint(plan.publisher.command)),'ROTATION_PUBLISHER_READBACK_REJECTED');
   if(sourceProfile){kube('rollout','status','deployment/internal-rpc-authority-publisher','--timeout=300s','-n',namespace);livePublisher=get('deployment','internal-rpc-authority-publisher');
    requireValue(sourcePublisherExecutable(livePublisher,get,kube,options['--k3s-sudo'])===plan.publisherExecutableSHA256,'ROTATION_SOURCE_PUBLISHER_EXECUTABLE_MISMATCH');}
   liveRegistry=get('configmap','internal-rpc-authority-publisher-target-registry');requireValue(liveRegistry.metadata.uid===plan.registry.uid&&fingerprint(liveRegistry.data)===plan.registry.desiredDataSHA256&&
    validateRotationPolicy(liveRegistry.data?.['authority-policy.json']).sha256===plan.registry.policySHA256,'ROTATION_REGISTRY_POST_RESTART_DRIFT');
   evidence({status:'APPLIED',operation:'PUBLISHER_RESTART',resourceVersion:livePublisher.metadata.resourceVersion});
	 }
	 if(command==='apply'||command==='resume'){
	  const existingJob=kube('get','job',job.metadata.name,'-n',namespace,'--ignore-not-found','-o','json');
	  if(!existingJob)try{
	   const argv=['--context',options['--context'],'create','-f','-'];
	   if(options['--k3s-sudo'])run('sudo',['-n','k3s','kubectl',...argv],JSON.stringify(job));
	   else run('kubectl',argv,JSON.stringify(job));
	  }catch{evidence({status:'UNKNOWN',operation:'CREATE'});}
	 }
 try{
  const created=get('job',job.metadata.name);verifyRotationJobReadback(created,job);
  const result=classifyRotationStatus(created,created.status?.succeeded===1?kube('logs',`job/${job.metadata.name}`,'-n',namespace,'-c','migrate'):'');
  if(fd!==undefined)evidence({status:result.phase,jobUID:created.metadata.uid,rotationStatus:result.state?.status});
  process.stdout.write(JSON.stringify({intentID:plan.intentID,ownerOperationID:plan.ownerOperationID,job:job.metadata.name,jobUID:created.metadata.uid,...result})+'\n');
  if(result.phase==='FAILED')process.exitCode=1;
 }finally{if(fd!==undefined)closeSync(fd);}
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Authority rotation transition failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED'}\n`);process.exitCode=1;});
