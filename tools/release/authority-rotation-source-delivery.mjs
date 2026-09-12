#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {closeSync,fsyncSync,lstatSync,openSync,readFileSync,writeFileSync,writeSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {inspectSource} from './application-source.mjs';
import {readAuthorityExecutable} from './authority-executable-readback.mjs';
import {fingerprint} from './scoped-release.mjs';
import {buildSourceMigrationReceipt,classifyPublisherSourceState,createPublisherRollbackCAS,createPublisherSourceCAS,createSourceMigrationJob,validateRenderedSourceMigration,validateSourceDeliveryPlan,validateSourcePublisher,verifySourceMigrationReadback} from './authority-rotation-source-delivery-model.mjs';
import {publishSourceMigrationIntent,publishSourceMigrationReceipt,readSourceMigrationIntent,readSourceMigrationReceipt,resolveSourceMigrationReceipt} from './authority-source-migration-receipt.mjs';
import {rotationPrerequisiteSpecSHA256,validateRotationPrerequisites} from './authority-rotation-transition.mjs';
import {workloadPods} from '../dev/component-manifest.mjs';

const namespace='kodex-system',sha=/^[a-f0-9]{64}$/;
const requireValue=(value,code)=>{if(!value)throw new Error(code);};
const run=(command,args,input)=>execFileSync(command,args,{input,encoding:'utf8',stdio:['pipe','pipe','pipe'],timeout:30_000,maxBuffer:32<<20}).trim();
function privateJSON(path){const stat=lstatSync(path);requireValue(stat.isFile()&&!stat.isSymbolicLink()&&(stat.mode&0o077)===0&&stat.size>0&&stat.size<=32<<20,'PRIVATE_INPUT_INVALID');return JSON.parse(readFileSync(path,'utf8'));}
function boundary(resources){return fingerprint(resources.map(item=>({kind:item.kind,name:item.metadata.name,uid:item.metadata.uid,generation:item.metadata.generation,specSHA256:fingerprint(item.spec)})).sort((a,b)=>`${a.kind}/${a.name}`.localeCompare(`${b.kind}/${b.name}`)));}
function sourcePublisherPods(deployment,resources){
 const owned=workloadPods(deployment,resources).filter(p=>!p.metadata.deletionTimestamp);
 requireValue(deployment.status?.observedGeneration>=deployment.metadata.generation&&deployment.status?.availableReplicas===deployment.spec.replicas&&owned.length===deployment.spec.replicas&&owned.every(p=>p.status?.phase==='Running'),'SOURCE_PUBLISHER_NOT_STABLE');
 return owned.map(pod=>{const container=pod.spec.containers.find(c=>c.name==='publisher'),status=pod.status.containerStatuses?.find(c=>c.name==='publisher');requireValue(status?.ready&&status.state?.running&&status.restartCount>=0,'SOURCE_PUBLISHER_NOT_READY');return {pod,container};});
}
function sourcePublisherExecutable(deployment,resources,kube,k3sSudo){const values=sourcePublisherPods(deployment,resources).map(({pod,container})=>readAuthorityExecutable(pod,container,{kube,k3sSudo}));requireValue(new Set(values).size===1,'SOURCE_PUBLISHER_EXECUTABLES_DIFFER');return values[0];}
export async function main(args,io={}){
 const runCommand=io.run??run,inspect=io.inspectSource??inspectSource,readPublisherExecutable=io.sourcePublisherExecutable??sourcePublisherExecutable;
 const command=args.shift(),options={};requireValue(['plan','apply-migration','observe-migration','apply-publisher','observe-publisher','rollback'].includes(command),'INVALID_COMMAND');
 while(args.length){const key=args.shift();requireValue(['--context','--source','--revision','--render','--capability','--output','--plan','--migration-receipt','--evidence','--confirm','--k3s-sudo'].includes(key)&&!Object.hasOwn(options,key),'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift();}
 requireValue(options['--context']&&!/prod/i.test(options['--context']),'EXACT_STAGING_CONTEXT_REQUIRED');
 const kube=(...argv)=>options['--k3s-sudo']?runCommand('sudo',['-n','k3s','kubectl','--context',options['--context'],...argv]):runCommand('kubectl',['--context',options['--context'],...argv]);
 const kubeInput=(input,...argv)=>options['--k3s-sudo']?runCommand('sudo',['-n','k3s','kubectl','--context',options['--context'],...argv],input):runCommand('kubectl',['--context',options['--context'],...argv],input);
 const get=(kind,name)=>JSON.parse(kube('get',kind,...(name?[name]:[]),'-n',namespace,'-o','json'));
 requireValue(kube('config','current-context')===options['--context'],'CONTEXT_MISMATCH');const ns=get('namespace',namespace);
 requireValue(ns.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_NAMESPACE_REQUIRED');
 validateRotationPrerequisites(get('serviceaccount','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-postgresql-from-migrator'));
 let publisher=get('deployment','internal-rpc-authority-publisher'),registry=get('configmap','internal-rpc-authority-publisher-target-registry');const publisherState=validateSourcePublisher(publisher);
 const inventory=()=>get('deployments',null).items.filter(d=>d.metadata.name!=='internal-rpc-authority-publisher');const neighborsSHA256=boundary(inventory());
 let plan=command==='plan'?null:privateJSON(options['--plan']);
 if(command==='plan'){
  requireValue(options['--source']&&options['--revision']&&options['--render']&&options['--capability']&&options['--output'],'PLAN_INPUT_REQUIRED');
  const source={...inspect(options['--source']),path:options['--source']};requireValue(source.revision===options['--revision'],'EXACT_CLEAN_SOURCE_REQUIRED');
  const renderedResources=JSON.parse(runCommand('yq',['eval-all','-o=json','[.]',options['--render']]));
  const findRendered=(kind,name)=>renderedResources.find(item=>item.kind===kind&&item.metadata?.name===name),rendered=findRendered('Job','internal-rpc-authority-migrate');validateRenderedSourceMigration(rendered);
  const renderedSA=findRendered('ServiceAccount','internal-rpc-authority-migrator'),renderedEgress=findRendered('NetworkPolicy','internal-rpc-authority-migrator'),renderedIngress=findRendered('NetworkPolicy','internal-rpc-authority-postgresql-from-migrator');
  validateRotationPrerequisites(renderedSA,renderedEgress,renderedIngress);
  const liveSA=get('serviceaccount','internal-rpc-authority-migrator'),liveEgress=get('networkpolicy','internal-rpc-authority-migrator'),liveIngress=get('networkpolicy','internal-rpc-authority-postgresql-from-migrator');validateRotationPrerequisites(liveSA,liveEgress,liveIngress);
  requireValue(fingerprint({labels:renderedSA.metadata.labels,automountServiceAccountToken:renderedSA.automountServiceAccountToken})===fingerprint({labels:liveSA.metadata.labels,automountServiceAccountToken:liveSA.automountServiceAccountToken})&&rotationPrerequisiteSpecSHA256(renderedEgress)===rotationPrerequisiteSpecSHA256(liveEgress)&&
   rotationPrerequisiteSpecSHA256(renderedIngress)===rotationPrerequisiteSpecSHA256(liveIngress),'RENDERED_ROTATION_PREREQUISITES_DRIFT');
  const old=inspect(publisherState.source),capability=privateJSON(options['--capability']);
  requireValue(capability.version===1&&capability.protocol===1&&capability.revision===source.revision&&capability.go==='go1.26.6'&&sha.test(capability.publisherSHA256)&&Array.isArray(capability.migrations)&&capability.migrations.length>0,'EXACT_SOURCE_CAPABILITY_REQUIRED');
  const currentExecutableSHA256=readPublisherExecutable(publisher,get('replicasets,pods',null).items,kube,options['--k3s-sudo']);
  plan={version:1,kind:'AUTHORITY_SOURCE_DELIVERY',intentID:randomUUID(),context:options['--context'],namespaceUID:ns.metadata.uid,source:source.path,revision:source.revision,capability,
   publisher:{uid:publisher.metadata.uid,resourceVersion:publisher.metadata.resourceVersion,beforeSpecSHA256:fingerprint(publisher.spec),beforeSource:publisherState.source,beforeRevision:old.revision,beforeExecutableSHA256:currentExecutableSHA256,
    beforeAnnotations:Object.fromEntries(['kodex.dev/source-revision','kodex.dev/source-delivery-intent'].flatMap(key=>publisher.spec.template.metadata.annotations?.[key]===undefined?[]:[[key,publisher.spec.template.metadata.annotations[key]]])),
    beforeAnnotationsPresent:Object.hasOwn(publisher.spec.template.metadata,'annotations')},
   migration:{rendered,renderedSpecSHA256:fingerprint(rendered.spec)},boundary:{registryUID:registry.metadata.uid,registryDataSHA256:fingerprint(registry.data),neighborsSHA256}};
  plan.publisher.desiredSpecSHA256=fingerprint(createPublisherSourceCAS(publisher,plan).spec);
  const job=createSourceMigrationJob(rendered,plan);const dryRun=JSON.parse(kubeInput(JSON.stringify(job),'create','--dry-run=server','-f','-','-o','json'));verifySourceMigrationReadback(dryRun,job);
  requireValue(boundary(inventory())===neighborsSHA256&&fingerprint(get('configmap','internal-rpc-authority-publisher-target-registry').data)===plan.boundary.registryDataSHA256,'SOURCE_DELIVERY_BOUNDARY_CHANGED');
  writeFileSync(options['--output'],JSON.stringify(plan,null,2)+'\n',{flag:'wx',mode:0o600});process.stdout.write(`Authority source delivery plan: ${fingerprint(plan)}\n`);return;
 }
 validateSourceDeliveryPlan(plan);requireValue(plan.context===options['--context']&&plan.namespaceUID===ns.metadata.uid&&inspect(plan.source).revision===plan.revision,'SOURCE_DELIVERY_PLAN_DRIFT');
 requireValue(options['--migration-receipt'],'SOURCE_MIGRATION_RECEIPT_REQUIRED');
 requireValue(registry.metadata.uid===plan.boundary.registryUID&&fingerprint(registry.data)===plan.boundary.registryDataSHA256&&boundary(inventory())===plan.boundary.neighborsSHA256,'SOURCE_DELIVERY_BOUNDARY_CHANGED');
 const job=createSourceMigrationJob(plan.migration.rendered,plan),maybeJob=()=>{const raw=kube('get','job',job.metadata.name,'-n',namespace,'--ignore-not-found','-o','json');return raw?JSON.parse(raw):null;};
 if(command==='apply-migration'){
  requireValue(options['--confirm']==='APPLY-STAGING-AUTHORITY-SOURCE-MIGRATION'&&options['--evidence'],'STAGING_CONFIRMATION_REQUIRED');requireValue(!maybeJob()&&!readSourceMigrationIntent(options['--migration-receipt'],job,plan,{optional:true})&&!readSourceMigrationReceipt(options['--migration-receipt'],job,plan,{optional:true}),'EXISTING_INTENT_REQUIRES_OBSERVE');
  let created;const fd=openSync(options['--evidence'],'wx',0o600);try{writeSync(fd,JSON.stringify({at:new Date().toISOString(),status:'INTENT',operation:'CREATE_MIGRATION',planSHA256:fingerprint(plan)})+'\n');fsyncSync(fd);publishSourceMigrationIntent(options['--migration-receipt'],job,plan);try{created=JSON.parse(kubeInput(JSON.stringify(job),'create','-f','-','-o','json'));}catch{writeSync(fd,JSON.stringify({at:new Date().toISOString(),status:'UNKNOWN',operation:'CREATE_MIGRATION'})+'\n');fsyncSync(fd);}}
  finally{closeSync(fd);}
  if(created){verifySourceMigrationReadback(created,job);publishSourceMigrationReceipt(options['--migration-receipt'],buildSourceMigrationReceipt(created,job,plan),job,plan);}
 }
 if(['apply-migration','observe-migration','apply-publisher','observe-publisher','rollback'].includes(command)){
  readSourceMigrationIntent(options['--migration-receipt'],job,plan);const actual=maybeJob();requireValue(actual,'SOURCE_MIGRATION_MISSING');resolveSourceMigrationReceipt(options['--migration-receipt'],actual,job,plan,{allowFirstObservation:command==='observe-migration'});
  const status=actual.status?.succeeded===1?'SUCCEEDED':actual.status?.failed?'FAILED':'RUNNING';if(['apply-migration','observe-migration'].includes(command)){process.stdout.write(JSON.stringify({status,job:job.metadata.name,uid:actual.metadata.uid})+'\n');if(status==='FAILED')process.exitCode=1;return;}requireValue(status==='SUCCEEDED','SOURCE_MIGRATION_NOT_SUCCEEDED');
 }
 if(command==='apply-publisher'){
  requireValue(options['--confirm']==='APPLY-STAGING-AUTHORITY-PUBLISHER-SOURCE'&&options['--evidence'],'STAGING_CONFIRMATION_REQUIRED');
  requireValue(classifyPublisherSourceState(publisher,plan)==='BEFORE','EXISTING_INTENT_REQUIRES_OBSERVE');
  const desired=createPublisherSourceCAS(publisher,plan);requireValue(fingerprint(desired.spec)===plan.publisher.desiredSpecSHA256,'SOURCE_PUBLISHER_DESIRED_CHANGED');const fd=openSync(options['--evidence'],'wx',0o600);
  try{writeSync(fd,JSON.stringify({at:new Date().toISOString(),status:'INTENT',operation:'PUBLISHER_SOURCE_CAS',planSHA256:fingerprint(plan)})+'\n');fsyncSync(fd);try{kubeInput(JSON.stringify(desired),'replace','-f','-');}catch{writeSync(fd,JSON.stringify({at:new Date().toISOString(),status:'UNKNOWN',operation:'PUBLISHER_SOURCE_CAS'})+'\n');fsyncSync(fd);}}
  finally{closeSync(fd);}
 }
 if(command==='rollback'){
  requireValue(options['--confirm']==='ROLLBACK-STAGING-AUTHORITY-PUBLISHER-SOURCE'&&options['--evidence'],'STAGING_CONFIRMATION_REQUIRED');
  requireValue(classifyPublisherSourceState(publisher,plan)==='AFTER','SOURCE_ROLLBACK_STATE_REJECTED');const desired=createPublisherRollbackCAS(publisher,plan),fd=openSync(options['--evidence'],'wx',0o600);
  try{writeSync(fd,JSON.stringify({at:new Date().toISOString(),status:'INTENT',operation:'PUBLISHER_SOURCE_ROLLBACK',planSHA256:fingerprint(plan)})+'\n');fsyncSync(fd);try{kubeInput(JSON.stringify(desired),'replace','-f','-');}catch{writeSync(fd,JSON.stringify({at:new Date().toISOString(),status:'UNKNOWN',operation:'PUBLISHER_SOURCE_ROLLBACK'})+'\n');fsyncSync(fd);}}
  finally{closeSync(fd);}publisher=get('deployment','internal-rpc-authority-publisher');requireValue(publisher.metadata.uid===plan.publisher.uid&&fingerprint(publisher.spec)===plan.publisher.beforeSpecSHA256&&validateSourcePublisher(publisher).source===plan.publisher.beforeSource,'SOURCE_PUBLISHER_ROLLBACK_READBACK_REJECTED');
  kube('rollout','status','deployment/internal-rpc-authority-publisher','--timeout=300s','-n',namespace);publisher=get('deployment','internal-rpc-authority-publisher');
  requireValue(readPublisherExecutable(publisher,get('replicasets,pods',null).items,kube,options['--k3s-sudo'])===plan.publisher.beforeExecutableSHA256,'SOURCE_PUBLISHER_ROLLBACK_EXECUTABLE_MISMATCH');process.stdout.write(JSON.stringify({status:'ROLLED_BACK',intentID:plan.intentID})+'\n');return;
 }
 publisher=get('deployment','internal-rpc-authority-publisher');requireValue(classifyPublisherSourceState(publisher,plan)==='AFTER','SOURCE_PUBLISHER_READBACK_REJECTED');const state=validateSourcePublisher(publisher);
 requireValue(publisher.metadata.uid===plan.publisher.uid&&state.source===plan.source&&publisher.spec.template.metadata.annotations?.['kodex.dev/source-delivery-intent']===plan.intentID,'SOURCE_PUBLISHER_READBACK_REJECTED');
 kube('rollout','status','deployment/internal-rpc-authority-publisher','--timeout=300s','-n',namespace);publisher=get('deployment','internal-rpc-authority-publisher');
 requireValue(readPublisherExecutable(publisher,get('replicasets,pods',null).items,kube,options['--k3s-sudo'])===plan.capability.publisherSHA256,'SOURCE_PUBLISHER_EXECUTABLE_MISMATCH');
 requireValue(fingerprint(get('configmap','internal-rpc-authority-publisher-target-registry').data)===plan.boundary.registryDataSHA256&&boundary(inventory())===plan.boundary.neighborsSHA256,'SOURCE_DELIVERY_BOUNDARY_CHANGED');
 process.stdout.write(JSON.stringify({status:'PASS',intentID:plan.intentID,publisherUID:publisher.metadata.uid,revision:plan.revision})+'\n');
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Authority source delivery failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED'}\n`);process.exitCode=1;});
