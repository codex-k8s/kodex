#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {readFileSync,writeFileSync,openSync,writeSync,fsyncSync,closeSync,lstatSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {createHash} from 'node:crypto';
import {inspectSource} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';
import {createSourceMigrationJob,verifySourceMigrationReadback,validateSourceDeliveryPlan} from './authority-rotation-source-delivery-model.mjs';
import {validateRotationPrerequisites} from './authority-rotation-transition.mjs';
import {readSourceMigrationReceipt} from './authority-source-migration-receipt.mjs';

const namespace='kodex-system',proofPath='/var/run/config/kodex/legacy-provenance/proof.json';
const requireValue=(value,code)=>{if(!value)throw new Error(code);};
const hash=value=>createHash('sha256').update(value).digest('hex');
const privateJSON=path=>{const stat=lstatSync(path);requireValue(stat.isFile()&&(stat.mode&0o777)===0o600,'PRIVATE_INPUT_REQUIRED');return JSON.parse(readFileSync(path,'utf8'));};
const canonical=value=>JSON.stringify(value,Object.keys(value).sort());

export function validateProofBudget(proof){
 requireValue(typeof proof?.inputPreimage==='string'&&Buffer.byteLength(proof.inputPreimage)<=1048576,'PROOF_REJECTED');
 requireValue(Buffer.byteLength(canonical(proof))<=1048576,'PROOF_ENVELOPE_TOO_LARGE');
}

export function repairResources(delivery,rotation,source,revision,proof){
 validateProofBudget(proof);
 validateSourceDeliveryPlan(delivery);
 requireValue(rotation.version===3&&rotation.action==='rotate'&&rotation.sourceDeliveryPlanSHA256===fingerprint(delivery),'EXACT_ROTATION_REQUIRED');
 requireValue(Number.isSafeInteger(proof.sourceRevision)&&proof.sourceRevision===rotation.registry.previousSourceRevision&&/^[a-f0-9]{64}$/.test(proof.snapshotDigestSHA256),'PROOF_REJECTED');
 const input=JSON.parse(proof.inputPreimage);
 requireValue(Object.keys(proof).sort().join()==='inputPreimage,snapshotDigestSHA256,sourceRevision'&&Object.keys(input).sort().join()==='manifest_bundle,policy,registry_digest_sha256'&&typeof input.manifest_bundle==='string'&&typeof input.policy==='string'&&input.registry_digest_sha256===rotation.registry.previousSourceDigestSHA256&&hash(input.policy)===rotation.registry.previousPolicySHA256,'PROOF_ROTATION_BINDING_REJECTED');
 const job=createSourceMigrationJob(delivery.migration.rendered,{...delivery,source,revision});
 job.metadata.name=`authority-legacy-repair-${rotation.intentID}`;
 job.metadata.annotations={'kodex.dev/legacy-repair-intent':rotation.intentID,'kodex.dev/legacy-repair-source':revision,'kodex.dev/legacy-repair-proof':hash(canonical(proof))};
 const config={apiVersion:'v1',kind:'ConfigMap',metadata:{name:job.metadata.name,namespace},immutable:true,data:{'proof.json':canonical(proof)}};
 job.spec.template.spec.volumes.push({name:'legacy-proof',configMap:{name:config.metadata.name,defaultMode:292}});
 const container=job.spec.template.spec.containers[0];
 container.volumeMounts.push({name:'legacy-proof',mountPath:'/var/run/config/kodex/legacy-provenance',readOnly:true});
 container.args=['services/internal/internal-rpc-authority','./cmd/cli','legacy-provenance-repair','--proof-file',proofPath,'--confirm','REPAIR-STAGING-LEGACY-PROVENANCE'];
 return {job,config};
}
export function verifyRepairJob(actual,expected){
 requireValue(fingerprint(actual.metadata.annotations)===fingerprint(expected.metadata.annotations),'REPAIR_JOB_BINDING_CHANGED');
 // Переиспользуем строгую проверку spec, исключая только server-owned selector/labels.
 verifySourceMigrationReadback({...actual,metadata:{...actual.metadata,annotations:{'kodex.dev/source-delivery-intent':'repair'}}},{...expected,metadata:{...expected.metadata,annotations:{'kodex.dev/source-delivery-intent':'repair'}}});
}
export async function main(args){
 const command=args.shift(),options={};requireValue(['plan','apply','observe'].includes(command),'INVALID_COMMAND');
 while(args.length){const key=args.shift();requireValue(['--context','--source','--revision','--rotation-plan','--source-plan','--migration-receipt','--proof','--output','--plan','--evidence','--confirm'].includes(key)&&!Object.hasOwn(options,key),'INVALID_ARGUMENT');options[key]=args.shift();}
 requireValue(options['--context']==='default','EXACT_STAGING_CONTEXT_REQUIRED');
 const kube=(argv,input)=>execFileSync('sudo',['-n','k3s','kubectl','--context',options['--context'],...argv],{input,encoding:'utf8',stdio:['pipe','pipe','pipe'],timeout:30000,maxBuffer:16<<20}).trim();
 const get=(kind,name)=>{const raw=kube(['get',kind,name,'-n',namespace,'--ignore-not-found','-o','json']);return raw?JSON.parse(raw):null;};
 const ns=get('namespace',namespace),publisher=get('deployment','internal-rpc-authority-publisher'),registry=get('configmap','internal-rpc-authority-publisher-target-registry');
 requireValue(ns?.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_REQUIRED');
 validateRotationPrerequisites(get('serviceaccount','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-postgresql-from-migrator'));
 let plan;
 if(command==='plan'){
  const source=inspectSource(options['--source']);requireValue(source.revision===options['--revision'],'EXACT_SOURCE_REQUIRED');
  const rotation=privateJSON(options['--rotation-plan']),delivery=privateJSON(options['--source-plan']),proof=privateJSON(options['--proof']);
  const oldJob=createSourceMigrationJob(delivery.migration.rendered,delivery),receipt=readSourceMigrationReceipt(options['--migration-receipt'],oldJob,delivery),actual=get('job',oldJob.metadata.name);
  verifySourceMigrationReadback(actual,oldJob,receipt);requireValue(actual.status?.succeeded===1,'SOURCE_MIGRATION_NOT_SUCCEEDED');
  requireValue(registry.metadata.uid===rotation.registry.uid&&fingerprint(registry.data)===rotation.registry.desiredDataSHA256&&publisher.metadata.uid===rotation.publisher.uid&&fingerprint(publisher.spec)===rotation.publisher.desiredSpecSHA256,'ROTATION_BOUNDARY_CHANGED');
  const resources=repairResources(delivery,rotation,options['--source'],options['--revision'],proof);
  plan={version:1,kind:'LEGACY_PROVENANCE_REPAIR',context:options['--context'],namespaceUID:ns.metadata.uid,source:options['--source'],revision:options['--revision'],publisherUID:publisher.metadata.uid,publisherSpecSHA256:fingerprint(publisher.spec),registryUID:registry.metadata.uid,registryDataSHA256:fingerprint(registry.data),...resources};
  const dry=JSON.parse(kube(['create','--dry-run=server','-f','-','-o','json'],JSON.stringify(plan.job)));verifyRepairJob(dry,plan.job);
  writeFileSync(options['--output'],JSON.stringify(plan,null,2)+'\n',{flag:'wx',mode:0o600});process.stdout.write(JSON.stringify({status:'PLANNED',planSHA256:fingerprint(plan)})+'\n');return;
 }
 plan=privateJSON(options['--plan']);requireValue(plan.version===1&&plan.kind==='LEGACY_PROVENANCE_REPAIR'&&plan.context===options['--context']&&plan.namespaceUID===ns.metadata.uid&&inspectSource(plan.source).revision===plan.revision&&publisher.metadata.uid===plan.publisherUID&&fingerprint(publisher.spec)===plan.publisherSpecSHA256&&registry.metadata.uid===plan.registryUID&&fingerprint(registry.data)===plan.registryDataSHA256,'REPAIR_BOUNDARY_CHANGED');
 const verifyConfig=value=>requireValue(value?.metadata.name===plan.config.metadata.name&&value.metadata.namespace===namespace&&value.immutable===true&&fingerprint(value.data)===fingerprint(plan.config.data),'REPAIR_CONFIG_CHANGED');
 if(command==='apply'){
  requireValue(options['--confirm']==='REPAIR-STAGING-LEGACY-PROVENANCE'&&!get('job',plan.job.metadata.name)&&!get('configmap',plan.config.metadata.name),'EXISTING_REPAIR_REQUIRES_OBSERVE');
  const fd=openSync(options['--evidence'],'wx',0o600),record=value=>{writeSync(fd,JSON.stringify({at:new Date().toISOString(),planSHA256:fingerprint(plan),...value})+'\n');fsyncSync(fd);};
  try{
   record({status:'INTENT'});
   const config=JSON.parse(kube(['create','-f','-','-o','json'],JSON.stringify(plan.config)));verifyConfig(config);record({status:'CONFIG_CREATED',uid:config.metadata.uid});
   const job=JSON.parse(kube(['create','-f','-','-o','json'],JSON.stringify(plan.job)));verifyRepairJob(job,plan.job);record({status:'JOB_CREATED',uid:job.metadata.uid});
  }catch{record({status:'UNKNOWN'});throw new Error('REPAIR_REQUIRES_OBSERVE');}finally{closeSync(fd);}
 }
 const rows=readFileSync(options['--evidence'],'utf8').trim().split('\n').map(JSON.parse);requireValue(rows.length>0&&rows.every(row=>row.planSHA256===fingerprint(plan)),'REPAIR_EVIDENCE_CHANGED');
 const actualConfig=get('configmap',plan.config.metadata.name),actualJob=get('job',plan.job.metadata.name);verifyConfig(actualConfig);requireValue(actualJob,'REPAIR_JOB_MISSING');verifyRepairJob(actualJob,plan.job);
 for(const [status,value] of [['CONFIG_CREATED',actualConfig],['JOB_CREATED',actualJob]]){const prior=rows.find(row=>row.status===status);if(prior)requireValue(prior.uid===value.metadata.uid,'REPAIR_UID_RECEIPT_REQUIRED');else{requireValue(command==='observe'&&rows[0].status==='INTENT','REPAIR_UID_RECEIPT_REQUIRED');const fd=openSync(options['--evidence'],'a',0o600);try{writeSync(fd,JSON.stringify({at:new Date().toISOString(),planSHA256:fingerprint(plan),status,uid:value.metadata.uid,firstAuthoritativeObservation:true})+'\n');fsyncSync(fd);}finally{closeSync(fd);}}}
 const status=actualJob.status?.succeeded===1?'PASS':actualJob.status?.failed?'FAIL':'RUNNING';
 if(status==='PASS'){
  const log=kube(['logs',`job/${actualJob.metadata.name}`,'-n',namespace,'-c','migrate']);const result=log.trim().split('\n').filter(line=>line.startsWith('{')).map(JSON.parse).at(-1);
  requireValue(result?.status==='PASS'&&result.sourceRevision===JSON.parse(plan.config.data['proof.json']).sourceRevision,'REPAIR_RESULT_REQUIRED');
 }
 process.stdout.write(JSON.stringify({status,job:actualJob.metadata.name,uid:actualJob.metadata.uid})+'\n');if(status==='FAIL')process.exitCode=1;
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Legacy provenance repair failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED'}\n`);process.exitCode=1;});
