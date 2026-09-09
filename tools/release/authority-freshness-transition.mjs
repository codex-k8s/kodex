#!/usr/bin/env node
import {readAuthorityExecutable} from './authority-executable-readback.mjs';
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { readFileSync, writeFileSync, openSync, writeSync, fsyncSync, closeSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fingerprint } from './scoped-release.mjs';
import { inspectSource } from './application-source.mjs';
import { authorityDeploymentRoles } from './authority-sidecar-rollout.mjs';
import {validateFuturePolicy,validateFutureJobProof} from './authority-freshness-job-proof.mjs';
import { workloadPods } from '../dev/component-manifest.mjs';

const namespace = 'kodex-system', templateName = 'internal-rpc-authority-migrate';
const modulePath = 'services/internal/internal-rpc-authority';
const image = 'docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83';
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const sha = /^[a-f0-9]{64}$/;
function requireValue(ok, code) { if (!ok) throw new Error(code); }
const run = (command, args, input) => execFileSync(command,args,{ input,encoding:'utf8',stdio:['pipe','pipe','pipe'],timeout:30_000,maxBuffer:16<<20 }).trim();

export function validateMigrationTemplate(job) {
  const m=job.metadata,s=job.spec,p=s?.template?.spec,c=p?.containers?.[0];
  requireValue(job.kind==='Job' && m?.name===templateName && m.namespace===namespace && uuid.test(m.uid) && /^\d+$/.test(m.resourceVersion) && job.status?.succeeded===1 &&
    !m.deletionTimestamp && p?.restartPolicy==='Never' && p.serviceAccountName==='internal-rpc-authority-migrator' && p.automountServiceAccountToken===false &&
    p.containers.length===1 && c.name==='migrate' && c.image===image && c.workingDir===`/workspace/${modulePath}` &&
    fingerprint(c.command)===fingerprint(['/workspace/tools/dev/run-go-command.sh']) && fingerprint(c.args)===fingerprint([modulePath,'./cmd/cli','up']), 'EXACT_COMPLETED_MIGRATOR_REQUIRED');
  requireValue(c.volumeMounts.some(mount=>mount.name==='dev-source'&&mount.mountPath==='/workspace'&&mount.readOnly===true) &&
    p.volumes.some(volume=>volume.name==='dev-source'&&volume.hostPath?.type==='Directory') &&
    c.volumeMounts.some(mount=>mount.name==='postgresql-credentials'&&mount.mountPath==='/var/run/secrets/kodex/internal-rpc-authority/postgres'&&mount.readOnly===true) &&
    p.volumes.some(volume=>volume.name==='postgresql-credentials'&&volume.secret?.secretName==='internal-rpc-authority-postgres-migration'), 'EXACT_MIGRATOR_MOUNTS_REQUIRED');
  for (const name of ['INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE','INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME'])
    requireValue(c.env?.filter(item=>item.name===name).length===1,'MIGRATOR_ENVIRONMENT_REQUIRED');
}

export function createMigrationJob(template, plan) {
  validateMigrationTemplate(template);
  requireValue(['up','freshness-status','freshness-watch','freshness-activate'].includes(plan.action) && uuid.test(plan.intent) && /^[a-f0-9]{40}$/.test(plan.revision) &&
    /^\/srv\/kodex-dev\/[A-Za-z0-9._-]+$/.test(plan.source), 'INVALID_FRESHNESS_PLAN');
  const spec=structuredClone(template.spec);
  delete spec.selector;delete spec.ttlSecondsAfterFinished;
  spec.backoffLimit=0;spec.activeDeadlineSeconds=300;
  for(const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name']) delete spec.template.metadata?.labels?.[key];
  spec.template.spec.volumes.find(volume=>volume.name==='dev-source').hostPath.path=plan.source;
  const args=[modulePath,'./cmd/cli',plan.action];
  if(plan.action==='freshness-activate') args.push('--expected-version','1','--activation-id',plan.intent,'--confirm','ACTIVATE-STAGING-AUTHORITY-FRESHNESS');
  spec.template.spec.containers[0].args=args;
  return {apiVersion:'batch/v1',kind:'Job',metadata:{name:`authority-freshness-${plan.intent}`,namespace,
    labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging'},
    annotations:{'kodex.dev/freshness-intent':plan.intent,'kodex.dev/freshness-plan-sha256':fingerprint(plan)}},spec};
}

// Только выполняемый authority server: worker grant writer и publisher не
// меняются вслед за прикладным SHA. Image profile проверяется по actual exe.
export function authorityConsumers(resources) {
  const result=[];
  for(const workload of resources.filter(item=>['Deployment','StatefulSet','DaemonSet'].includes(item.kind))) {
    const containers=[...(workload.spec.template.spec.containers??[]),...(workload.spec.template.spec.initContainers??[])];
    for(const container of containers) {
      const hot=container.command?.includes('/workspace/tools/dev/run-go-hot-reload.sh') && container.args?.[0]===modulePath && ['./cmd/internal-rpc-authority-issuer','./cmd/internal-rpc-authority-verifier'].includes(container.args?.[1]);
      const role = ['issuer','verifier'].find(role=>container.args?.[1]===`./cmd/internal-rpc-authority-${role}`||container.command?.includes(`/usr/local/bin/internal-rpc-authority-${role}`));
      const immutable=role&&container.command?.includes(`/usr/local/bin/internal-rpc-authority-${role}`);
      if(!hot&&!immutable) {requireValue(!/^internal-rpc-authority-(issuer|verifier)$/.test(container.name),'UNKNOWN_AUTHORITY_CONSUMER_COMMAND');continue;}
      requireValue(authorityDeploymentRoles[workload.metadata.name]?.includes(role),'UNKNOWN_AUTHORITY_WORKLOAD');
      const replicaUIDs=resources.filter(item=>item.kind==='ReplicaSet'&&item.metadata.ownerReferences?.some(owner=>owner.uid===workload.metadata.uid&&owner.controller)).map(item=>item.metadata.uid);
      requireValue(!resources.some(item=>item.kind==='Pod'&&item.metadata.deletionTimestamp&&item.status.phase==='Running'&&item.metadata.ownerReferences?.some(owner=>replicaUIDs.includes(owner.uid)&&owner.controller)),'OLD_AUTHORITY_CONSUMER_MUST_DRAIN');
      const pods=workloadPods(workload,resources);
      requireValue(workload.status?.observedGeneration>=workload.metadata.generation && pods.length===workload.spec.replicas && pods.length>0,'ALL_AUTHORITY_CONSUMERS_REQUIRED');
      for(const pod of pods) {
        const actual=[...(pod.spec.containers??[]),...(pod.spec.initContainers??[])].find(item=>item.name===container.name);
        const status=[...(pod.status.containerStatuses??[]),...(pod.status.initContainerStatuses??[])].find(item=>item.name===container.name);
        requireValue(pod.status.phase==='Running' && status?.ready===true && status.state?.running && /^containerd:\/\/[a-f0-9]{64}$/.test(status.containerID??'') && /@sha256:[a-f0-9]{64}$/.test(status.imageID??'') &&
          authorityContainerMaterialized(workload.spec.template.spec,pod.spec,container,actual), 'AUTHORITY_CONSUMER_NOT_STABLE');
        const process=hot?`/tmp/kodex-dev-${container.args[2]}/build/main`:`/usr/local/bin/internal-rpc-authority-${role}`;
        requireValue(/^\/(tmp\/kodex-dev-[a-z0-9-]+\/build\/main|usr\/local\/bin\/internal-rpc-authority-(issuer|verifier))$/.test(process),'UNKNOWN_AUTHORITY_PROCESS');
        result.push({role,profile:hot?'source':'image',workload:workload.metadata.name,workloadUID:workload.metadata.uid,specSHA256:fingerprint(workload.spec),pod:pod.metadata.name,podUID:pod.metadata.uid,
          container:container.name,containerID:status.containerID,imageID:status.imageID,process});
      }
    }
  }
  requireValue(result.length>0,'AUTHORITY_CONSUMERS_MISSING');
  for(const workload of resources.filter(item=>item.kind==='Deployment'&&Object.hasOwn(authorityDeploymentRoles,item.metadata.name)))
    for(const role of authorityDeploymentRoles[workload.metadata.name])requireValue(result.some(item=>item.workload===workload.metadata.name&&item.role===role),'REGISTERED_AUTHORITY_CONSUMER_MISSING');
  return result.sort((a,b)=>`${a.pod}/${a.container}`.localeCompare(`${b.pod}/${b.container}`));
}

function authorityContainerMaterialized(templatePod,actualPod,templateContainer,actualContainer) {
  if(!actualContainer)return false;
  const declared=structuredClone(templateContainer),actual=structuredClone(actualContainer);
  const declaredMounts=declared.volumeMounts??[],actualMounts=actual.volumeMounts??[];
  delete declared.volumeMounts;delete actual.volumeMounts;
  if(fingerprint(declared)!==fingerprint(actual))return false;
  if(!declaredMounts.every(mount=>actualMounts.filter(item=>item.name===mount.name||item.mountPath===mount.mountPath).length===1&&
    actualMounts.some(item=>item.name===mount.name&&item.mountPath===mount.mountPath&&fingerprint(item)===fingerprint(mount))))return false;
  const declaredMountKeys=new Set(declaredMounts.map(mount=>`${mount.name}\0${mount.mountPath}`));
  const extraMounts=actualMounts.filter(mount=>!declaredMountKeys.has(`${mount.name}\0${mount.mountPath}`));
  const declaredVolumes=templatePod.volumes??[],actualVolumes=actualPod.volumes??[];
  if(!declaredVolumes.every(volume=>actualVolumes.filter(item=>item.name===volume.name).length===1&&
    actualVolumes.some(item=>item.name===volume.name&&fingerprint(item)===fingerprint(volume))))return false;
  const declaredVolumeNames=new Set(declaredVolumes.map(volume=>volume.name));
  const extraVolumes=actualVolumes.filter(volume=>!declaredVolumeNames.has(volume.name));
  if(extraMounts.length===0&&extraVolumes.length===0)return true;
  if(extraMounts.length!==1||extraVolumes.length!==1||templatePod.automountServiceAccountToken===false||actualPod.automountServiceAccountToken===false)return false;
  const mount=extraMounts[0],volume=extraVolumes[0];
  if(!/^kube-api-access-[a-z0-9]{5}$/.test(mount.name)||mount.name!==volume.name||fingerprint(mount)!==fingerprint({name:mount.name,mountPath:'/var/run/secrets/kubernetes.io/serviceaccount',readOnly:true}))return false;
  const projected=volume.projected,sources=projected?.sources;
  if(fingerprint(Object.keys(volume).sort())!==fingerprint(['name','projected'])||fingerprint(Object.keys(projected??{}).sort())!==fingerprint(['defaultMode','sources'])||
    projected.defaultMode!==420||!Array.isArray(sources)||sources.length!==3||sources.some(source=>Object.keys(source).length!==1))return false;
  const token=sources.filter(source=>source.serviceAccountToken).map(source=>source.serviceAccountToken);
  const rootCA=sources.filter(source=>source.configMap).map(source=>source.configMap);
  const namespace=sources.filter(source=>source.downwardAPI).map(source=>source.downwardAPI);
  return token.length===1&&Number.isInteger(token[0].expirationSeconds)&&token[0].expirationSeconds>=600&&token[0].expirationSeconds<=7200&&
    fingerprint({...token[0],expirationSeconds:0})===fingerprint({expirationSeconds:0,path:'token'})&&
    fingerprint(rootCA)===fingerprint([{name:'kube-root-ca.crt',items:[{key:'ca.crt',path:'ca.crt'}]}])&&
    fingerprint(namespace)===fingerprint([{items:[{path:'namespace',fieldRef:{apiVersion:'v1',fieldPath:'metadata.namespace'}}]}]);
}

export function verifyJobReadback(job, expected) {
  requireValue(job.metadata?.annotations?.['kodex.dev/freshness-plan-sha256']===expected.metadata.annotations['kodex.dev/freshness-plan-sha256'] &&
    job.metadata?.annotations?.['kodex.dev/freshness-intent']===expected.metadata.annotations['kodex.dev/freshness-intent'] && job.metadata.namespace===namespace && job.metadata.name===expected.metadata.name,'FRESHNESS_JOB_IDENTITY_MISMATCH');
  const actual=structuredClone(job.spec), wanted=structuredClone(expected.spec);
  // API добавляет selector и четыре controller labels только после создания.
  delete actual.selector;
  for(const value of [actual,wanted]) for(const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name']) delete value.template.metadata?.labels?.[key];
  requireValue(fingerprint(actual)===fingerprint(wanted),'FRESHNESS_JOB_SPEC_DRIFT');
}

async function main(args) {
  const command=args.shift(),options={};
  requireValue(['plan','apply','observe'].includes(command),'INVALID_COMMAND');
  while(args.length) { const key=args.shift();requireValue(['--context','--source','--revision','--action','--output','--plan','--evidence','--capability','--job-proofs','--confirm','--k3s-sudo'].includes(key)&&!options[key],'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift(); }
  requireValue(options['--context']&&!/prod/i.test(options['--context']),'EXACT_STAGING_CONTEXT_REQUIRED');
  const kube=(...args)=>options['--k3s-sudo']?run('sudo',['-n','k3s','kubectl','--context',options['--context'],...args]):run('kubectl',['--context',options['--context'],...args]);
  requireValue(kube('config','current-context')===options['--context'],'CONTEXT_MISMATCH');
  const get=(...args)=>JSON.parse(kube('get',...args,'-o','json'));
  const ns=get('namespace',namespace);
  requireValue(ns.metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&ns.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_NAMESPACE_REQUIRED');
  const template=get('job',templateName,'-n',namespace);validateMigrationTemplate(template);
  let plan=command==='plan'?{version:1,intent:randomUUID(),context:options['--context'],namespaceUID:ns.metadata.uid,
    templateUID:template.metadata.uid,templateResourceVersion:template.metadata.resourceVersion,templateSpecSHA256:fingerprint(template.spec),
    source:options['--source'],revision:options['--revision'],action:options['--action'],consumers:null,capability:null,futureJobs:null}:JSON.parse(readFileSync(options['--plan'],'utf8'));
  requireValue(plan.version===1&&plan.context===options['--context']&&plan.namespaceUID===ns.metadata.uid&&plan.templateUID===template.metadata.uid&&
    plan.templateResourceVersion===template.metadata.resourceVersion&&plan.templateSpecSHA256===fingerprint(template.spec),'FRESHNESS_PLAN_DRIFT');
  requireValue(inspectSource(plan.source).revision===plan.revision,'EXACT_SOURCE_REQUIRED');
  createMigrationJob(template,plan);
  const readConsumers=()=>{
    const inventory=get('deployments,statefulsets,daemonsets,replicasets,pods','-n',namespace).items;
    // Interaction gateway отсутствует только в штатном web-only профиле.
    for(const name of Object.keys(authorityDeploymentRoles).filter(name=>name!=='interaction-gateway'))
      requireValue(inventory.some(item=>item.kind==='Deployment'&&item.metadata.name===name),'REQUIRED_AUTHORITY_DEPLOYMENT_MISSING');
    const actual=authorityConsumers(inventory);
    const capability=command==='plan'?JSON.parse(readFileSync(options['--capability'],'utf8')):plan.capability;
    requireValue(capability?.version===1&&capability.protocol===2&&capability.revision===plan.revision&&sha.test(capability.binaries?.issuer)&&sha.test(capability.binaries?.verifier)&&['issuer','verifier'].every(role=>sha.test(capability.imageBinaries?.[role]))&&capability.imageVersion===plan.revision&&capability.go==='go1.26.6'&&
      capability.recipe==='CGO_ENABLED=0 GOWORK=off GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -buildvcs=false ./cmd/internal-rpc-authority-{issuer,verifier}','EXACT_AUTHORITY_CAPABILITY_REQUIRED');
    const verifyExecutables=()=>{for(const consumer of actual) {
      const pod=inventory.find(item=>item.kind==='Pod'&&item.metadata.uid===consumer.podUID);
      const container=[...(pod.spec.containers??[]),...(pod.spec.initContainers??[])].find(c=>c.name===consumer.container);
      requireValue(readAuthorityExecutable(pod,container,{kube,k3sSudo:options['--k3s-sudo']})===(consumer.profile==='image'?capability.imageBinaries:capability.binaries)[consumer.role],'AUTHORITY_EXECUTABLE_INCOMPATIBLE');
    }};
    verifyExecutables();
    requireValue(fingerprint(actual)===fingerprint(authorityConsumers(get('deployments,statefulsets,daemonsets,replicasets,pods','-n',namespace).items)),'AUTHORITY_CONSUMER_CHANGED');
    verifyExecutables();
    const controller=get('deployment','image-admission-controller','-n',namespace),app=controller.spec.template.spec.containers.find(c=>c.name==='image-admission-controller');
    const policyName=app?.env?.find(e=>e.name==='IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP')?.value;
    const policy=get('configmap',policyName,'-n',namespace);
    validateFuturePolicy(policy,get('imageadmissionpolicyparameters',policyName,'-n',namespace),get('validatingadmissionpolicybinding','kodex-image-admission-controller-jobs'));
    const resources=get('deployments,replicasets,pods,jobs','-n',namespace).items;
    const readers=workloadPods(controller,resources);
    requireValue(readers.length===controller.spec.replicas&&readers.length>0&&sha.test(capability.rendererSHA256),'FUTURE_JOB_READER_REQUIRED');
    for(const pod of readers) requireValue(kube('exec',pod.metadata.name,'-n',namespace,'-c','image-admission-controller','--','sha256sum','/opt/kodex/render-image-admission-job.sh').split(/\s/)[0]===capability.rendererSHA256,'FUTURE_JOB_RENDERER_INCOMPATIBLE');
    const proofs=command==='plan'?JSON.parse(readFileSync(options['--job-proofs'],'utf8')).map(path=>JSON.parse(readFileSync(path,'utf8'))):plan.futureJobs.proofs;
    requireValue(proofs.length===2&&new Set(proofs.map(p=>p.workload)).size===2,'BOTH_FUTURE_JOB_PROOFS_REQUIRED');
    for(const proof of proofs)validateFutureJobProof(proof,get('job',proof.job,'-n',namespace),policy,capability,ns.metadata.uid);
    for(const job of resources.filter(item=>item.kind==='Job'&&!item.status?.succeeded&&!item.status?.failed)) {
      for(const container of [...(job.spec.template.spec.containers??[]),...(job.spec.template.spec.initContainers??[])])
        if(container.command?.includes('/usr/local/bin/internal-rpc-authority-issuer'))requireValue(container.image===policy.data.authorityIssuerImage,'ACTIVE_LEGACY_JOB_MUST_DRAIN');
    }
    const futureJobs={controllerUID:controller.metadata.uid,controllerSpecSHA256:fingerprint(controller.spec),policyUID:policy.metadata.uid,policySHA256:policy.data.policySHA256,proofs};
    if(command==='plan') {plan.consumers=actual;plan.capability=capability;plan.futureJobs=futureJobs;}
    else requireValue(fingerprint(actual)===fingerprint(plan.consumers)&&fingerprint(futureJobs)===fingerprint(plan.futureJobs),'AUTHORITY_CONSUMER_DRIFT');
  };
  if(plan.action==='freshness-activate') readConsumers();
  if(command==='plan') {createMigrationJob(template,plan);writeFileSync(options['--output'],JSON.stringify(plan,null,2)+'\n',{flag:'wx',mode:0o600});process.stdout.write(`Freshness plan: ${fingerprint(plan)}\n`);return;}
  const job=createMigrationJob(template,plan);
  let fd;
  const evidence=record=>{writeSync(fd,JSON.stringify({at:new Date().toISOString(),intent:plan.intent,...record})+'\n');fsyncSync(fd);};
  if(command==='apply') {
    requireValue(options['--confirm']==='APPLY-STAGING-AUTHORITY-FRESHNESS','STAGING_CONFIRMATION_REQUIRED');
    requireValue(!kube('get','job',job.metadata.name,'-n',namespace,'--ignore-not-found','-o','json'),'EXISTING_INTENT_REQUIRES_OBSERVE');
    fd=openSync(options['--evidence'],'wx',0o600);evidence({status:'INTENT',planSHA256:fingerprint(plan)});
  }
  try {
    if(command==='apply') {
      try {const argv=['--context',options['--context'],'create','-f','-'];if(options['--k3s-sudo'])run('sudo',['-n','k3s','kubectl',...argv],JSON.stringify(job));else run('kubectl',argv,JSON.stringify(job));}
      catch {evidence({status:'UNKNOWN',operation:'CREATE'});}
    }
    const created=get('job',job.metadata.name,'-n',namespace);verifyJobReadback(created,job);
    const status=created.status?.succeeded===1?'SUCCEEDED':created.status?.failed?'FAILED':'RUNNING';
    if(fd!==undefined)evidence({status,jobUID:created.metadata.uid});
    process.stdout.write(JSON.stringify({status,job:job.metadata.name,jobUID:created.metadata.uid,intent:plan.intent})+'\n');
    if(status==='FAILED')process.exitCode=1;
  } finally {if(fd!==undefined)closeSync(fd);}
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)) main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Authority freshness failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED'}\n`);process.exitCode=1;});
