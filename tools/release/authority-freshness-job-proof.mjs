#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {readFileSync,writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {policyDigest} from './runner-policy-model.mjs';
const namespace='kodex-system';
const requireValue=(value,code)=>{if(!value)throw new Error(code);};
export function validateFutureJobProof(proof,job,policy,capability,namespaceUID) {
 requireValue(proof?.version===1&&proof.namespaceUID===namespaceUID&&proof.revision===capability.revision&&proof.binarySHA256===capability.imageBinaries.issuer&&
  proof.job===job.metadata.name&&proof.jobUID===job.metadata.uid&&proof.jobSpecSHA256===fingerprint(job.spec)&&
  proof.policySHA256===policy.data.policySHA256&&job.status?.succeeded===1&&
  ['image-admission','image-promotion'].includes(proof.workload)&&proof.workload===(job.metadata.labels?.['kodex.dev/image-admission-phase']==='promote'?'image-promotion':'image-admission')&&job.metadata.annotations?.['kodex.dev/admission-policy-revision']===policy.data.policyRevision,'EXACT_COMPLETED_FUTURE_JOB_PROOF_REQUIRED');
 requireValue(proof.image===policy.data.authorityIssuerImage&&typeof proof.imageID==='string'&&proof.imageID.endsWith(`@${proof.image.split('@')[1]}`),'FUTURE_JOB_IMAGE_MISMATCH');
}
export function validateFuturePolicy(policy,parameters,binding) {
 requireValue(policy.immutable===true&&policy.metadata.labels?.['kodex.dev/owner-intent']==='true'&&policy.data.policySHA256===policyDigest(policy.data)&&
  /^.+@sha256:[a-f0-9]{64}$/.test(policy.data.authorityIssuerImage??'')&&fingerprint(parameters.spec)===fingerprint(policy.data)&&
  binding.spec.paramRef.name===policy.metadata.name&&binding.spec.paramRef.namespace===namespace&&binding.spec.paramRef.parameterNotFoundAction==='Deny'&&
  fingerprint(binding.spec.validationActions)===fingerprint(['Deny']),'EXACT_FUTURE_ISSUER_POLICY_REQUIRED');
}
function main(args) {
 const options={};while(args.length){const key=args.shift();requireValue(['--context','--job','--capability','--output','--k3s-sudo'].includes(key)&&!options[key],'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift();}
 requireValue(options['--context']&&!/prod/i.test(options['--context'])&&/^mc-admit-[a-f0-9]{32}-(claim|admit|promote)$/.test(options['--job']??''),'EXACT_STAGING_JOB_REQUIRED');
 const kube=(...args)=>execFileSync(options['--k3s-sudo']?'sudo':'kubectl',options['--k3s-sudo']?['-n','k3s','kubectl','--context',options['--context'],...args]:['--context',options['--context'],...args],{encoding:'utf8',stdio:'pipe',timeout:15_000,maxBuffer:8<<20}).trim();
 const get=(...args)=>JSON.parse(kube('get',...args,'-o','json'));
 requireValue(kube('config','current-context')===options['--context'],'CONTEXT_MISMATCH');
 const ns=get('namespace',namespace);requireValue(ns.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_NAMESPACE_REQUIRED');
 const controller=get('deployment','image-admission-controller','-n',namespace),app=controller.spec.template.spec.containers.find(c=>c.name==='image-admission-controller');
 const policyName=app?.env?.find(e=>e.name==='IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP')?.value;
 const policy=get('configmap',policyName,'-n',namespace);validateFuturePolicy(policy,get('imageadmissionpolicyparameters',policyName,'-n',namespace),get('validatingadmissionpolicybinding','kodex-image-admission-controller-jobs'));
 const job=get('job',options['--job'],'-n',namespace),phase=job.metadata.labels?.['kodex.dev/image-admission-phase'];
 requireValue(job.metadata.labels?.['kodex.dev/image-admission-orchestrated']==='true'&&['claim','admit','promote'].includes(phase)&&job.metadata.annotations?.['kodex.dev/admission-policy-revision']===policy.data.policyRevision,'OWNER_JOB_POLICY_REQUIRED');
 const pods=get('pods','-n',namespace).items.filter(p=>p.metadata.ownerReferences?.some(o=>o.uid===job.metadata.uid&&o.controller)&&p.status.phase==='Running'&&!p.metadata.deletionTimestamp);
 requireValue(pods.length===1,'ONE_RUNNING_OWNER_JOB_REQUIRED');const pod=pods[0];
 const issuer=pod.spec.initContainers?.find(c=>c.name==='internal-rpc-authority-issuer'),state=pod.status.initContainerStatuses?.find(c=>c.name===issuer?.name);
 requireValue(issuer?.image===policy.data.authorityIssuerImage&&fingerprint(issuer.command)===fingerprint(['/usr/local/bin/internal-rpc-authority-issuer'])&&state?.ready&&state.state?.running,'EXACT_JOB_ISSUER_REQUIRED');
 for(const name of ['internal-rpc-authority-socket-init','platform-worker-grant-agent'])requireValue(pod.spec.initContainers?.find(c=>c.name===name)?.image===policy.data.authorityImage,'UNCHANGED_JOB_WORKER_IMAGE_REQUIRED');
 const capability=JSON.parse(readFileSync(options['--capability'],'utf8'));
 const script='count=0; result=; for entry in /proc/[0-9]*/exe; do target=$(readlink "$entry" 2>/dev/null) || continue; if [ "$target" = /usr/local/bin/internal-rpc-authority-issuer ]; then count=$((count+1)); result=$(sha256sum "$entry") || exit 1; fi; done; [ "$count" = 1 ] || exit 1; printf "%s\\n" "$result"';
 const binarySHA256=kube('exec',pod.metadata.name,'-n',namespace,'-c',issuer.name,'--','sh','-c',script).split(/\s/)[0];
 requireValue(binarySHA256===capability.imageBinaries?.issuer,'JOB_ISSUER_BINARY_MISMATCH');
 const after=get('pod',pod.metadata.name,'-n',namespace),afterState=after.status.initContainerStatuses?.find(c=>c.name===issuer.name);
 requireValue(after.metadata.uid===pod.metadata.uid&&afterState?.containerID===state.containerID&&afterState.imageID===state.imageID&&fingerprint(after.spec)===fingerprint(pod.spec),'JOB_CHANGED_DURING_PROOF');
 const proof={version:1,namespaceUID:ns.metadata.uid,revision:capability.revision,workload:phase==='promote'?'image-promotion':'image-admission',policySHA256:policy.data.policySHA256,
  job:job.metadata.name,jobUID:job.metadata.uid,jobSpecSHA256:fingerprint(job.spec),podUID:pod.metadata.uid,containerID:state.containerID,image:issuer.image,imageID:state.imageID,binarySHA256,timestampUTC:new Date().toISOString()};
 writeFileSync(options['--output'],JSON.stringify(proof,null,2)+'\n',{flag:'wx',mode:0o600});process.stdout.write(`Future job executable proof: ${fingerprint(proof)}\n`);
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){try{main(process.argv.slice(2));}catch(error){process.stderr.write(`Future authority job proof failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'PROOF_FAILED'}\n`);process.exitCode=1;}}
