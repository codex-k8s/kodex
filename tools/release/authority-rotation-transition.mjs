#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {closeSync, constants, fstatSync, fsyncSync, openSync, readFileSync, writeFileSync, writeSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {inspectSource} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';

const namespace='kodex-system';
const uuid=/^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/;
const sha=/^[a-f0-9]{64}$/;
const revision=/^[1-9][0-9]{0,15}$/;
function requireValue(ok,code){if(!ok)throw new Error(code);}
const run=(command,args,input)=>execFileSync(command,args,{input,encoding:'utf8',stdio:['pipe','pipe','pipe'],timeout:30_000,maxBuffer:16<<20}).trim();
function validateRotationTemplate(job){
 const pod=job.spec?.template?.spec,container=pod?.containers?.[0];
 requireValue(job.kind==='Job'&&job.metadata?.namespace===namespace&&pod?.restartPolicy==='Never'&&
  pod.serviceAccountName==='internal-rpc-authority-migrator'&&pod.automountServiceAccountToken===false&&
	 job.spec.template.metadata?.labels?.['app.kubernetes.io/name']==='internal-rpc-authority'&&
	 job.spec.template.metadata?.labels?.['app.kubernetes.io/component']==='migrator'&&
  pod.containers?.length===1&&container.name==='migrate'&&container.command?.length===1&&
  container.command[0]==='/usr/local/bin/internal-rpc-authority-cli'&&
  /^ghcr\.io\/codex-k8s\/kodex\/internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(container.image)&&
  container.volumeMounts?.some(mount=>mount.name==='postgresql-credentials'&&mount.readOnly===true)&&
	 container.volumeMounts?.some(mount=>mount.name==='postgresql-ca'&&mount.readOnly===true)&&
  pod.volumes?.some(volume=>volume.name==='postgresql-credentials'&&volume.secret?.secretName==='internal-rpc-authority-postgres-migration'),
 'EXACT_ROTATION_CLI_TEMPLATE_REQUIRED');
}

export function validateRotationPrerequisites(serviceAccount,egressPolicy,ingressPolicy){
	const labels={'app.kubernetes.io/name':'internal-rpc-authority','app.kubernetes.io/component':'migrator'};
	const exactLabels=value=>Object.entries(labels).every(([key,expected])=>value?.[key]===expected);
	const postgresEgress=egressPolicy?.spec?.egress?.find(rule=>rule.to?.length===1&&rule.to[0].podSelector?.matchLabels?.['app.kubernetes.io/name']==='kodex-postgresql');
	const dnsEgress=egressPolicy?.spec?.egress?.find(rule=>rule.to?.length===1&&rule.to[0].namespaceSelector?.matchLabels?.['kubernetes.io/metadata.name']==='kube-system'&&rule.to[0].podSelector?.matchLabels?.['k8s-app']==='kube-dns');
	const postgresIngress=ingressPolicy?.spec?.ingress?.[0];
	requireValue(serviceAccount?.kind==='ServiceAccount'&&serviceAccount.metadata?.name==='internal-rpc-authority-migrator'&&
	 serviceAccount.automountServiceAccountToken===false&&exactLabels(serviceAccount.metadata.labels)&&
	 egressPolicy?.kind==='NetworkPolicy'&&egressPolicy.metadata?.name==='internal-rpc-authority-migrator'&&
	 exactLabels(egressPolicy.spec?.podSelector?.matchLabels)&&fingerprint(egressPolicy.spec?.policyTypes)===fingerprint(['Ingress','Egress'])&&
	 egressPolicy.spec?.ingress?.length===0&&egressPolicy.spec?.egress?.length===2&&
	 fingerprint(postgresEgress?.ports)===fingerprint([{port:5432,protocol:'TCP'}])&&
	 fingerprint(dnsEgress?.ports)===fingerprint([{port:53,protocol:'UDP'},{port:53,protocol:'TCP'}])&&
	 ingressPolicy?.kind==='NetworkPolicy'&&ingressPolicy.metadata?.name==='internal-rpc-authority-postgresql-from-migrator'&&
	 ingressPolicy.spec?.podSelector?.matchLabels?.['app.kubernetes.io/name']==='kodex-postgresql'&&
	 ingressPolicy.spec?.ingress?.length===1&&postgresIngress.from?.length===1&&exactLabels(postgresIngress.from[0]?.podSelector?.matchLabels)&&
	 fingerprint(postgresIngress.ports)===fingerprint([{port:5432,protocol:'TCP'}]),
	 'ROTATION_JOB_PREREQUISITES_REJECTED');
}

export function createRotationJob(template,plan){
 validateRotationTemplate(template);
 requireValue(plan.version===2&&uuid.test(plan.operationID)&&['status','abort','rotate'].includes(plan.action)&&/^[a-f0-9]{40}$/.test(plan.revision)&&
  /^\/srv\/kodex-dev\/[A-Za-z0-9._-]+$/.test(plan.source),'INVALID_ROTATION_PLAN');
 if(plan.action==='abort')requireValue(uuid.test(plan.rotation.intentID)&&revision.test(String(plan.rotation.sourceRevision))&&
  Number(plan.rotation.sourceRevision)<=9007199254740991&&sha.test(plan.rotation.sourceDigestSHA256),'INVALID_ROTATION_ABORT');
 const spec=structuredClone(template.spec);delete spec.selector;delete spec.ttlSecondsAfterFinished;
 spec.backoffLimit=0;spec.activeDeadlineSeconds=300;
 for(const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name'])delete spec.template.metadata?.labels?.[key];
 const args=[plan.action==='abort'?'rotation-abort':plan.action==='rotate'?'rotation-watch':'rotation-status'];
 if(plan.action==='rotate')args.push('--operation-id',plan.operationID);
 if(plan.action==='abort')args.push('--intent-id',plan.rotation.intentID,'--source-revision',String(plan.rotation.sourceRevision),
  '--source-digest-sha256',plan.rotation.sourceDigestSHA256,'--confirm','ABORT-STAGING-AUTHORITY-ROTATION');
 spec.template.spec.containers[0].args=args;
 return {apiVersion:'batch/v1',kind:'Job',metadata:{name:`authority-rotation-${plan.operationID}`,namespace,
  labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging'},annotations:{
   'kodex.dev/rotation-operation':plan.operationID,'kodex.dev/rotation-plan-sha256':fingerprint(plan)}},spec};
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

export function createRegistryCAS(current,plan){
 requireValue(current?.kind==='ConfigMap'&&current.metadata?.uid===plan.registry.uid&&
  current.metadata?.resourceVersion===plan.registry.resourceVersion&&
  fingerprint(current.data)===plan.registry.currentDataSHA256,'ROTATION_REGISTRY_CAS_DRIFT');
 const desired=structuredClone(current);delete desired.status;
 for(const key of ['managedFields','creationTimestamp','generation'])delete desired.metadata[key];
 desired.data=structuredClone(plan.registry.desiredData);
 desired.metadata.annotations={...desired.metadata.annotations,'kodex.dev/rotation-operation':plan.operationID,
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
  'kodex.dev/authority-rotation-operation':plan.operationID};
 return desired;
}

export function verifyRotationJobReadback(job,expected){
 requireValue(job.metadata?.namespace===namespace&&job.metadata.name===expected.metadata.name&&
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

async function main(args){
 const command=args.shift(),options={};requireValue(['plan','apply','observe','resume'].includes(command),'INVALID_COMMAND');
 while(args.length){const key=args.shift();requireValue(['--context','--source','--revision','--action','--registry-file','--output','--plan','--evidence','--intent-id','--source-revision','--source-digest-sha256','--confirm','--k3s-sudo'].includes(key)&&!options[key],'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift();}
 requireValue(options['--context']&&!/prod/i.test(options['--context']),'EXACT_STAGING_CONTEXT_REQUIRED');
 const kube=(...argv)=>options['--k3s-sudo']?run('sudo',['-n','k3s','kubectl','--context',options['--context'],...argv]):run('kubectl',['--context',options['--context'],...argv]);
	const kubeInput=(input,...argv)=>options['--k3s-sudo']?run('sudo',['-n','k3s','kubectl','--context',options['--context'],...argv],input):run('kubectl',['--context',options['--context'],...argv],input);
	const replace=value=>options['--k3s-sudo']?
	 run('sudo',['-n','k3s','kubectl','--context',options['--context'],'replace','-f','-'],JSON.stringify(value)):
	 run('kubectl',['--context',options['--context'],'replace','-f','-'],JSON.stringify(value));
 requireValue(kube('config','current-context')===options['--context'],'CONTEXT_MISMATCH');
 const get=(kind,name)=>JSON.parse(kube('get',kind,name,'-n',namespace,'-o','json'));
 const ns=get('namespace',namespace);requireValue(ns.metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&ns.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_NAMESPACE_REQUIRED');
	validateRotationPrerequisites(get('serviceaccount','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-postgresql-from-migrator'));
	requireValue(kube('auth','can-i','create','jobs','-n',namespace)==='yes'&&
	 kube('get','secret','internal-rpc-authority-postgres-migration','-n',namespace,'-o','name')==='secret/internal-rpc-authority-postgres-migration'&&
	 kube('get','configmap','internal-rpc-authority-postgresql-ca','-n',namespace,'-o','name')==='configmap/internal-rpc-authority-postgresql-ca','ROTATION_JOB_API_PREREQUISITES_REJECTED');
 const livePublisher=get('deployment','internal-rpc-authority-publisher');
 const publisherContainer=livePublisher.spec.template.spec.containers.find(container=>container.name==='publisher');
 requireValue(publisherContainer?.command?.length===1&&publisherContainer.command[0]==='/usr/local/bin/internal-rpc-authority-publisher'&&
  /^ghcr\.io\/codex-k8s\/kodex\/internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(publisherContainer.image),'EXACT_PUBLISHER_EXECUTABLE_REQUIRED');
 const loadedPlan=command==='plan'?null:JSON.parse(readFileSync(options['--plan'],'utf8'));
	const source=command==='plan'?options['--source']:loadedPlan.source;
	const template=createCanonicalRotationTemplate(source,publisherContainer.image);validateRotationTemplate(template);
 let plan;
 if(command==='plan'){
  requireValue(['status','abort','rotate'].includes(options['--action']),'ROTATION_ACTION_REQUIRED');
  plan={version:2,operationID:randomUUID(),context:options['--context'],namespaceUID:ns.metadata.uid,
   templateSpecSHA256:fingerprint(template.spec),cliImage:template.spec.template.spec.containers[0].image,
   cliCommand:template.spec.template.spec.containers[0].command,source:options['--source'],revision:options['--revision'],action:options['--action']};
  if(plan.action==='rotate'){
   const liveRegistry=get('configmap','internal-rpc-authority-publisher-target-registry');
   requireValue(options['--registry-file']?.startsWith(`${plan.source}/`)&&/key-delivery-targets\.yaml$/.test(options['--registry-file']),'EXACT_ROTATION_REGISTRY_FILE_REQUIRED');
   const desiredData=structuredClone(liveRegistry.data);desiredData['key-delivery-targets.yaml']=readFileSync(options['--registry-file'],'utf8');
   requireValue(fingerprint(liveRegistry.data)!==fingerprint(desiredData),'ROTATION_REGISTRY_MUST_ADVANCE');
	   plan.registry={uid:liveRegistry.metadata.uid,resourceVersion:liveRegistry.metadata.resourceVersion,
	    currentDataSHA256:fingerprint(liveRegistry.data),desiredData,desiredDataSHA256:fingerprint(desiredData),sourceFile:options['--registry-file'].slice(plan.source.length+1)};
   plan.publisher={uid:livePublisher.metadata.uid,resourceVersion:livePublisher.metadata.resourceVersion,
    specSHA256:fingerprint(livePublisher.spec),image:livePublisher.spec.template.spec.containers.find(container=>container.name==='publisher').image,
    command:['/usr/local/bin/internal-rpc-authority-publisher']};
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
	 /^(deploy\/k8s\/base\/internal-rpc-authority-publisher|deploy\/k8s\/profiles\/web-with-mattermost)\/key-delivery-targets\.yaml$/.test(plan.registry.sourceFile)&&
	 uuid.test(plan.publisher?.uid)&&String(plan.publisher.resourceVersion).length>0&&sha.test(plan.publisher.specSHA256)&&sha.test(plan.publisher.desiredSpecSHA256)&&
	 plan.publisher.image===plan.cliImage&&fingerprint(plan.publisher.command)===fingerprint(['/usr/local/bin/internal-rpc-authority-publisher']),'ROTATION_PLAN_DRIFT');
 requireValue(inspectSource(plan.source).revision===plan.revision,'EXACT_SOURCE_REQUIRED');const job=createRotationJob(template,plan);
	if(plan.action==='rotate')requireValue(readFileSync(`${plan.source}/${plan.registry.sourceFile}`,'utf8')===plan.registry.desiredData['key-delivery-targets.yaml'],'ROTATION_REGISTRY_SOURCE_DRIFT');
	if(command==='plan'){
	 const dryRun=JSON.parse(kubeInput(JSON.stringify(job),'create','--dry-run=server','-f','-','-o','json'));
	 verifyRotationJobReadback(dryRun,job);
	 writeFileSync(options['--output'],JSON.stringify(plan,null,2)+'\n',{flag:'wx',mode:0o600});process.stdout.write(`Authority rotation plan: ${fingerprint(plan)}\n`);return;
	}
 let fd;const evidence=record=>{if(fd===undefined)return;writeSync(fd,JSON.stringify({at:new Date().toISOString(),operationID:plan.operationID,...record})+'\n');fsyncSync(fd);};
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
    liveRegistry.metadata.annotations?.['kodex.dev/rotation-operation']===plan.operationID,'ROTATION_REGISTRY_READBACK_REJECTED');
   evidence({status:'APPLIED',operation:'REGISTRY_CAS',resourceVersion:liveRegistry.metadata.resourceVersion});
   let livePublisher=get('deployment','internal-rpc-authority-publisher');
   if(livePublisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-operation']!==plan.operationID){
    const desired=createPublisherRestart(livePublisher,plan);evidence({status:'INTENT',operation:'PUBLISHER_RESTART'});
    try{replace(desired);}
    catch{evidence({status:'UNKNOWN',operation:'PUBLISHER_RESTART'});}
    livePublisher=get('deployment','internal-rpc-authority-publisher');
   }
   requireValue(livePublisher.metadata.uid===plan.publisher.uid&&livePublisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-operation']===plan.operationID&&
    fingerprint(livePublisher.spec)===plan.publisher.desiredSpecSHA256&&
    livePublisher.spec.template.spec.containers.some(container=>container.name==='publisher'&&container.image===plan.publisher.image&&fingerprint(container.command)===fingerprint(plan.publisher.command)),'ROTATION_PUBLISHER_READBACK_REJECTED');
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
  process.stdout.write(JSON.stringify({operationID:plan.operationID,job:job.metadata.name,jobUID:created.metadata.uid,...result})+'\n');
  if(result.phase==='FAILED')process.exitCode=1;
 }finally{if(fd!==undefined)closeSync(fd);}
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Authority rotation transition failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED'}\n`);process.exitCode=1;});
