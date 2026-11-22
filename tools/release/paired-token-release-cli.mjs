#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {lstatSync,realpathSync} from 'node:fs';
import {resolve,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {performance} from 'node:perf_hooks';
import {inspectSource,sourceGitArguments,validSource} from './application-source.mjs';
import {fingerprint,kubectlInvocation} from './scoped-release.mjs';
import {readAuthorityPrivateText,parseAuthorityPrivateJSON,syncAuthorityJournalDirectory} from './authority-catalog-transition.mjs';
import {createPrivateJSON} from './private-json-write.mjs';
import {sourceArchiveHash} from './stable-image-publication.mjs';
import {readComponentServingInventory} from './portable-component-migration.mjs';
import {qualifyComponentNodeImages} from './component-node-qualification.mjs';
import {readPairedTokenReleaseOwnerFromDatabase,recheckPairedTokenReleaseOwnerEpoch,
 pairedTokenReleasePublisherPin} from './paired-token-release-owner.mjs';
import {sameCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';
import {validatePairedTokenReleaseManifest,planPairedTokenRelease,verifyPairedTokenReleasePlan,pairedTokenReleaseNames} from './paired-token-release-plan.mjs';
import {inspectPairedTokenForwardReceipt,openPairedTokenEffectJournal} from './paired-token-release-receiver.mjs';
import {readPairedTokenReleaseTerminal} from './paired-token-release-terminal.mjs';
import {createPairedTokenNativeReadClient} from './paired-token-native-cas.mjs';
import {readPairedTokenTerminalObservation} from './paired-token-terminal-observation.mjs';
import {createPairedAuthorityPublisherReader} from './paired-authority-publisher-read.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)).replace(/\/$/,'');
const ensure=(condition,code)=>{if(!condition)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&same(Object.keys(value).sort(),[...keys].sort());
const path=value=>typeof value==='string'&&value.startsWith('/')&&value===resolve(value)&&value.length<=4096&&!/[\r\n\0]/.test(value);
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
export const pairedTokenReleaseOutputMaxBytes=4<<20;
const inputMaxBytes=16<<20;

export function boundPairedTokenReleaseArtifact(value){
 ensure(Buffer.byteLength(JSON.stringify(value)+'\n')<=pairedTokenReleaseOutputMaxBytes,
  'PAIRED_TOKEN_RELEASE_OUTPUT_BOUND_EXCEEDED');
 return value;
}

export function validatePairedTokenReleaseConfiguration(config){
 ensure(exact(config,['schema','manifest','source','authority','admission','nodes'])&&config.schema==='kodex.paired-token-release-input/v1'&&
  exact(config.source,['path','revision'])&&validSource(config.source)&&config.source.revision===config.manifest?.sourceSHA,'PAIRED_TOKEN_RELEASE_INPUT_REJECTED');
 validatePairedTokenReleaseManifest(config.manifest);
 ensure(exact(config.authority,['planPath','evidencePath'])&&Object.values(config.authority).every(path)&&
  exact(config.admission,['planPath','evidencePath','trafficPlanPath','trafficEvidencePath'])&&Object.values(config.admission).every(path)&&
  Array.isArray(config.nodes)&&config.nodes.length>=1&&config.nodes.length<=8&&config.nodes.every(n=>exact(n,['name','uid'])&&uuid(n.uid)&&
   /^[a-z0-9][a-z0-9.-]{0,252}$/.test(n.name??''))&&new Set(config.nodes.map(n=>n.uid)).size===config.nodes.length&&
  new Set(config.nodes.map(n=>n.name)).size===config.nodes.length,'PAIRED_TOKEN_RELEASE_INPUT_REJECTED');
 ensure(Buffer.byteLength(JSON.stringify(config))<=pairedTokenReleaseOutputMaxBytes,'PAIRED_TOKEN_RELEASE_INPUT_BOUND_EXCEEDED');
 return config;
}

function readReferences(config,open){
 let bytes=0;
 const read=(name,lines=false)=>{
  const raw=readAuthorityPrivateText(name);bytes+=Buffer.byteLength(raw);ensure(bytes<=inputMaxBytes,'PAIRED_TOKEN_RELEASE_INPUT_BOUND_EXCEEDED');
  if(!lines)return parseAuthorityPrivateJSON(raw);
  if(open)ensure(raw.endsWith('\n'),'PAIRED_TOKEN_RELEASE_JOURNAL_TRUNCATED');
  const rows=raw.split('\n').filter(Boolean);ensure(rows.length<=8192,'PAIRED_TOKEN_RELEASE_JOURNAL_BOUND_EXCEEDED');
  return rows.map(parseAuthorityPrivateJSON);
 };
 return {authority:{plan:read(config.authority.planPath),events:read(config.authority.evidencePath,true)},
  closed:{plan:read(config.admission.planPath),events:read(config.admission.evidencePath,true)},
  traffic:{plan:read(config.admission.trafficPlanPath),events:read(config.admission.trafficEvidencePath,true)},
  ...(open?{opened:{plan:read(open.plan),events:read(open.evidence,true)}}:{})};
}

function readOnlyKube(context,kubeconfig,deadline=Infinity){
 return (args,input)=>{
  const call=kubectlInvocation(context,args,false,kubeconfig);
  const remaining=Math.min(35000,Math.floor(deadline-performance.now()));
  ensure(remaining>0,'PAIRED_TERMINAL_EXPIRED');
  try{return execFileSync(call.file,call.arguments,{input,encoding:'utf8',timeout:remaining,killSignal:'SIGKILL',maxBuffer:8<<20,
   env:Object.fromEntries(['HOME','PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]])),stdio:[input===undefined?'ignore':'pipe','pipe','pipe']});}
  catch{throw Error(performance.now()>=deadline?'PAIRED_TERMINAL_EXPIRED':'PAIRED_TOKEN_RELEASE_READ_FAILED');}
 };
}

function requireAncestor(source,before,after){
 try{execFileSync('git',sourceGitArguments(source.path,'merge-base','--is-ancestor',before,after),
  {encoding:'utf8',timeout:10000,maxBuffer:65536,stdio:['ignore','pipe','pipe']});}
 catch{throw Error('PAIRED_TOKEN_RELEASE_SOURCE_LINEAGE_CHANGED');}
}

// Actual factory читает только текущие owner/API/serving/node metadata.
// Injectable ports нужны локальным synthetic тестам; CLI не принимает их из JSON.
export async function capturePairedTokenReleasePlan(config,kubeconfig,io={}){
 validatePairedTokenReleaseConfiguration(config);
 const inspect=io.inspectSource??inspectSource,kube=io.kube??readOnlyKube(config.manifest.target.context,kubeconfig);
 const source=inspect(config.source.path),executor=inspect(root),manifest=config.manifest,target=manifest.target;
 ensure(source.revision===manifest.sourceSHA,'PAIRED_TOKEN_RELEASE_SOURCE_CHANGED');
 const references=readReferences(config),referencesSHA256=fingerprint(references);
 const ancestor=io.requireAncestor??requireAncestor;
 for(const ref of [references.closed.plan,references.traffic.plan]){
  ensure(/^[a-f0-9]{40}$/.test(ref.sourceSHA??''),'PAIRED_TOKEN_RELEASE_SOURCE_LINEAGE_CHANGED');
  ancestor(config.source,ref.sourceSHA,source.revision);
 }
 for(const part of manifest.components)ancestor(config.source,part.beforeSourceSHA,source.revision);
 ensure(references.traffic.plan.closePlanSHA256===fingerprint(references.closed.plan),
  'PAIRED_TOKEN_RELEASE_API_HISTORY_CHANGED');
 const sourceHash=await(io.sourceArchiveHash??sourceArchiveHash)(source.revision,config.source.path);
 ensure(manifest.components.every(c=>c.publication.publicationPlan.receipt.contextSHA256===sourceHash),
  'PAIRED_TOKEN_RELEASE_BUILD_SOURCE_CHANGED');
 const get=(kind,name)=>JSON.parse(kube(['-n','kodex-system','get',kind,name,'-o','json']));
 const identity=()=>({context:target.context,
  clusterUID:JSON.parse(kube(['get','namespace','kube-system','-o','json'])).metadata?.uid,
  namespaceUID:JSON.parse(kube(['get','namespace','kodex-system','-o','json'])).metadata?.uid,
  installation:get('configmap','kodex-installation-profile')});
 const readServing=io.readServing??readComponentServingInventory;
 const observation=()=>{
  const nodes=JSON.parse(kube(['get','nodes','-o','json']));
  ensure(Array.isArray(nodes.items)&&!nodes.metadata?.continue,'PAIRED_TOKEN_RELEASE_NODE_PROOF_REQUIRED');
  const observed={...identity(),deployments:[],serving:{},nodes:nodes.items};
  for(const name of [...pairedTokenReleaseNames,'control-api-gateway']){
   const actual=get('deployment',name);observed.deployments.push(actual);
   observed.serving[name]=readServing(kube,name,actual.metadata.uid,{scopeProof:true});
  }
  const nonce=references.traffic.plan.afterSpec?.selector?.['kodex.dev/component-traffic-isolation'];
  ensure(uuid(nonce),'PAIRED_TOKEN_RELEASE_API_HISTORY_CHANGED');
  return {observed,api:{closed:references.closed,traffic:references.traffic,resources:observed.serving['control-api-gateway'],
   service:get('service','control-api-gateway'),
   slices:JSON.parse(kube(['-n','kodex-system','get','endpointslices','-l','kubernetes.io/service-name=control-api-gateway','-o','json'])),
   pods:JSON.parse(kube(['-n','kodex-system','get','pods','-l',`kodex.dev/component-traffic-isolation=${nonce}`,'-o','json']))}};
 };
 const readOwner=io.readOwner??((value,authority,kube)=>readPairedTokenReleaseOwnerFromDatabase(value,authority,kube,{
  readPublisher:target.context.startsWith('k3d-')?createPairedAuthorityPublisherReader(target.context,kubeconfig):undefined,
 }));
 const firstOwner=readOwner(manifest,references.authority,kube);
 const first=observation();
 const images=manifest.components.map(c=>`registry.local.kodex/kodex/${c.name}@${c.publication.publicationPlan.image.digest}`);
 const nodeReadback=await(io.readNodes??qualifyComponentNodeImages)({target:{context:target.context,clusterUID:target.clusterUID,
  namespaceUID:target.namespaceUID,nodes:config.nodes},kubeconfig,images,mode:'readback'});
 const initial=planPairedTokenRelease(manifest,{...first,owner:firstOwner,source,nodeReadback});
 // Не обновляем первый ceiling после Owner/Gateway/CRI чтений: второе owner
 // наблюдение лишь закрывает races, не продлевает первую проверку.
 const finalOwner=readOwner(manifest,references.authority,kube),last=observation();
 ensure(sameCatalogAuthorityBaseline(firstOwner.authority,finalOwner.authority)&&
  same(firstOwner.controlPlane.backend,finalOwner.controlPlane.backend)&&same(firstOwner.controlPlane.postgres,finalOwner.controlPlane.postgres)&&
  same(firstOwner.authorityHead,finalOwner.authorityHead),'PAIRED_TOKEN_RELEASE_OWNER_SCOPE_CHANGED');
 // Ещё один exact backend read после последнего Gateway discovery. Original
 // authority/custody остаются первыми, а не latest подменой same-name database.
 const tailOwner=readOwner(manifest,references.authority,kube);
 ensure(sameCatalogAuthorityBaseline(firstOwner.authority,tailOwner.authority)&&
  same(firstOwner.controlPlane.backend,tailOwner.controlPlane.backend)&&same(firstOwner.controlPlane.postgres,tailOwner.controlPlane.postgres)&&
  same(firstOwner.authorityHead,tailOwner.authorityHead),'PAIRED_TOKEN_RELEASE_OWNER_SCOPE_CHANGED');
 ensure(same(inspect(config.source.path),source)&&same(inspect(root),executor)&&
  fingerprint(readReferences(config))===referencesSHA256,'PAIRED_TOKEN_RELEASE_SOURCE_LINEAGE_CHANGED');
 const current={...last,owner:firstOwner,source,nodeReadback};
 verifyPairedTokenReleasePlan(initial,manifest,current);
 return {schema:'kodex.paired-token-release-record/v1',executorSHA:executor.revision,
  configSHA256:fingerprint(config),referencesSHA256,plan:initial,
  componentEvidence:pairedTokenReleaseNames.map(name=>({name,servingSHA256:fingerprint(last.observed.serving[name])})),
  current};
}

const recordFields=['schema','executorSHA','configSHA256','referencesSHA256','plan','componentEvidence'];

async function captureTerminal(config,inputs,io){
 validatePairedTokenReleaseConfiguration(config);
 const manifest=config.manifest;
 const savedRaw=readAuthorityPrivateText(inputs['--plan']),record=parseAuthorityPrivateJSON(savedRaw);
 const effect=inputs['--effect-journal'],effectRaw=readAuthorityPrivateText(effect);
 ensure(effectRaw.endsWith('\n'),'PAIRED_TOKEN_RELEASE_JOURNAL_TRUNCATED');
 const events=effectRaw.trimEnd().split('\n').map(parseAuthorityPrivateJSON);
 const open={plan:inputs['--open-plan'],evidence:inputs['--open-evidence']};
 const references=readReferences(config,open),{opened,...original}=references;
 const referencesSHA256=fingerprint(references);
 ensure(exact(record,recordFields)&&record.schema==='kodex.paired-token-release-record/v1'&&
  record.configSHA256===fingerprint(config)&&record.referencesSHA256===fingerprint(original),
  'PAIRED_TOKEN_RELEASE_RECORD_CHANGED');
 const journal=openPairedTokenEffectJournal(effect,manifest,record,{readOnly:true});
 try{
  ensure(same(journal.events,events),'PAIRED_TERMINAL_RECEIPT_CHANGED');
  const pair={manifest,record,journal};
  inspectPairedTokenForwardReceipt(pair);
  const inspect=io.inspectSource??inspectSource,kubeconfig=inputs['--kubeconfig'],target=manifest.target;
  const client=(io.createReadClient??createPairedTokenNativeReadClient)(target.context,kubeconfig);
  const readOwner=io.readOwner??readPairedTokenReleaseOwnerFromDatabase;
  const recheckOwnerEpoch=io.recheckOwnerEpoch??recheckPairedTokenReleaseOwnerEpoch;
  const publisherPin=io.publisherPin??pairedTokenReleasePublisherPin;
  const readSource=()=>{
   ensure(readAuthorityPrivateText(inputs['--plan'])===savedRaw&&readAuthorityPrivateText(effect)===effectRaw&&
    same(parseAuthorityPrivateJSON(readAuthorityPrivateText(inputs['--input'])),config)&&
    fingerprint(readReferences(config,open))===referencesSHA256,'PAIRED_TERMINAL_RECEIPT_CHANGED');
   return {sourceSHA:inspect(config.source.path).revision,executorSHA:inspect(root).revision};
  };
  const result=await readPairedTokenReleaseTerminal(pair,{
   readSource,
   readOwner:(value,{deadline})=>readOwner(value,references.authority,io.kube??readOnlyKube(target.context,kubeconfig,deadline),{
    readPublisher:createPairedAuthorityPublisherReader(target.context,kubeconfig,deadline),
   }),
   recheckOwnerEpoch:(value,owner,budget)=>recheckOwnerEpoch(value,owner,io.kube??readOnlyKube(target.context,kubeconfig,budget.deadline),client,budget),
   readObservation:budget=>readPairedTokenTerminalObservation(client,target,references,budget),
   readFinalObservation:(owner,budget)=>readPairedTokenTerminalObservation(client,target,references,budget,publisherPin(manifest,owner)),
  });
  const artifact=boundPairedTokenReleaseArtifact(result);
  await createPrivateJSON(inputs['--output'],artifact);syncAuthorityJournalDirectory(inputs['--output']);
  return {state:artifact.phase,releaseReadiness:artifact.releaseReadiness,terminalSHA256:fingerprint(artifact)};
 }finally{journal.close();}
}

export async function pairedTokenReleaseCLI(args,io={}){
 const [command,...rest]=args,options={};
 ensure(['plan','status','terminal-status'].includes(command),'PAIRED_TOKEN_RELEASE_READ_ONLY_COMMAND_REQUIRED');
 const terminal=command==='terminal-status',extra=['--effect-journal','--open-plan','--open-evidence'];
 const accepted=['--input','--kubeconfig','--output','--plan',...(terminal?extra:[])];
 while(rest.length){const key=rest.shift();ensure(accepted.includes(key)&&
  !Object.hasOwn(options,key)&&rest.length>0,'PAIRED_TOKEN_RELEASE_ARGUMENTS_REJECTED');options[key]=rest.shift();}
 ensure(['--input','--kubeconfig','--output'].every(k=>path(options[k]))&&
  (command==='plan'?!options['--plan']:path(options['--plan']))&&(!terminal||extra.every(k=>path(options[k]))),
  'PAIRED_TOKEN_RELEASE_ARGUMENTS_REJECTED');
 const kstat=lstatSync(options['--kubeconfig']);
 ensure(kstat.isFile()&&!kstat.isSymbolicLink()&&kstat.uid===process.getuid()&&kstat.nlink===1&&(kstat.mode&0o077)===0&&
  realpathSync(options['--kubeconfig'])===options['--kubeconfig'],
  'PAIRED_TOKEN_RELEASE_EXPLICIT_KUBECONFIG_REQUIRED');
 const directory=dirname(options['--output']),stat=lstatSync(directory);
 ensure(stat.isDirectory()&&stat.uid===process.getuid()&&(stat.mode&0o077)===0&&realpathSync(directory)===directory,
  'PAIRED_TOKEN_RELEASE_PRIVATE_OUTPUT_REQUIRED');
 const began=performance.now(),config=parseAuthorityPrivateJSON(readAuthorityPrivateText(options['--input']));
 if(terminal)return captureTerminal(config,options,io);
 const captured=await capturePairedTokenReleasePlan(config,options['--kubeconfig'],io);
 const {current,...record}=captured;
 if(command==='status'){
  const saved=parseAuthorityPrivateJSON(readAuthorityPrivateText(options['--plan']));
  ensure(exact(saved,recordFields)&&saved.schema===record.schema&&
   saved.executorSHA===record.executorSHA&&saved.configSHA256===record.configSHA256&&saved.referencesSHA256===record.referencesSHA256&&
   same(saved.componentEvidence,record.componentEvidence),'PAIRED_TOKEN_RELEASE_RECORD_CHANGED');
  verifyPairedTokenReleasePlan(saved.plan,config.manifest,current);
 }
 ensure(performance.now()-began<180000,'PAIRED_TOKEN_RELEASE_CAPTURE_EXPIRED');
 // Повторный private output — отдельный R/O artifact, не перезапись исходного
 // плана и не effect receipt. Тот же путь после lost write ACK не открывается.
 boundPairedTokenReleaseArtifact(record);
 await createPrivateJSON(options['--output'],record);syncAuthorityJournalDirectory(options['--output']);
 return {state:record.plan.state,releaseReadiness:record.plan.releaseReadiness,planSHA256:fingerprint(record.plan),
  components:record.plan.components.map(c=>({name:c.name,uid:c.uid,imageRef:c.imageRef}))};
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){
 try{const result=await pairedTokenReleaseCLI(process.argv.slice(2));process.stdout.write(JSON.stringify(result)+'\n');}
 catch(error){process.stderr.write((/^[A-Z_]{1,100}$/.test(error.message)?error.message:'PAIRED_TOKEN_RELEASE_READ_FAILED')+'\n');process.exitCode=1;}
}
