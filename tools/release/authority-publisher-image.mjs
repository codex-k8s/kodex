#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {randomUUID,createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {validatedStablePublication,sourceArchiveHash} from './stable-image-publication.mjs';
import {validateNodeContainer} from './stable-node-registry-model.mjs';
import {inspectSource} from './application-source.mjs';
import {fingerprint,withPrivatePatchFile} from './scoped-release.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {readComponentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON,writeAuthorityPrivatePlan,
  readCatalogTarget,catalogKubeconfig,readCatalogMaintenance,verifyCatalogAdmission} from './authority-catalog-transition.mjs';

const name='internal-rpc-authority-publisher',namespace='kodex-system',marker='kodex.dev/authority-publisher-image';
const requireValue=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const uuid=value=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value??'');
const sha=value=>/^[a-f0-9]{64}$/.test(value??'');
const header=plan=>({schema:'kodex.authority-publisher-image-evidence/v1',planSHA256:fingerprint(plan),imageUpdateID:plan.imageUpdateID});
const sameAnnotations=(actual,expected)=>{
 const strip=value=>{const copy={...(value??{})};delete copy['deployment.kubernetes.io/revision'];return copy;};
 return [actual,expected].every(value=>!Object.hasOwn(value??{},'deployment.kubernetes.io/revision')||/^[1-9][0-9]*$/.test(value['deployment.kubernetes.io/revision']))&&same(strip(actual),strip(expected));
};

// Publication связывается с canonical Dockerfile и exact public source; build
// archive/node pull не заменяются строкой --image или caller-owned boolean.
export function publisherImageSource(sourceSHA,evidence,build,contextSHA256){
 const imageRef=validatedStablePublication(sourceSHA,evidence,'internal-rpc-authority');
 requireValue(same(build,evidence.publicationPlan.receipt)&&build.dockerfile==='services/internal/internal-rpc-authority/Dockerfile'&&
  build.contextSHA256===contextSHA256&&evidence.publicationPlan.source===sourceSHA,'AUTHORITY_IMAGE_SOURCE_REJECTED');
 return {imageRef,sourceSHA,publicationSHA256:fingerprint(evidence),buildSHA256:fingerprint(build),contextSHA256};
}

// Readback использует существующие k3d node/container identity checks и только
// crictl inspecti. Pull/registry configuration/credential export отсутствуют.
export function readPublisherImageNodes({source,imageRef,targetProfile,context},kube,execute){
 const get=(kind,resource)=>parseAuthorityPrivateJSON(kube(['-n',namespace,'get',kind,resource,'-o','json']));
 const target=readCatalogTarget(targetProfile,context,get);
 requireValue(/^k3d-[a-z0-9-]+$/.test(context),'AUTHORITY_IMAGE_NODE_PROFILE_UNSUPPORTED');
 const snapshot=()=>{
  const list=parseAuthorityPrivateJSON(kube(['get','nodes','-o','json']));
  requireValue(Array.isArray(list.items)&&list.items.length>0&&list.items.length<=64&&!list.metadata?.continue&&
   new Set(list.items.map(node=>node.metadata?.uid)).size===list.items.length&&list.items.every(node=>uuid(node.metadata?.uid)&&
    !node.metadata.deletionTimestamp&&node.status?.conditions?.some(condition=>condition.type==='DiskPressure'&&condition.status==='False')),
   'AUTHORITY_IMAGE_NODES_REJECTED');
  return list.items.map(node=>{
   const containers=parseAuthorityPrivateJSON(execute('docker',['inspect','--type','container',node.metadata.name]));
   requireValue(Array.isArray(containers)&&containers.length===1,'AUTHORITY_IMAGE_NODE_CONTAINER_REJECTED');
   return validateNodeContainer(node,containers[0],context.slice(4));
  }).sort((a,b)=>a.name.localeCompare(b.name));
 };
 const before=snapshot(),nodes=[];
 for(const node of before){
  requireValue(same(snapshot(),before),'AUTHORITY_IMAGE_NODES_CHANGED');
  const data=parseAuthorityPrivateJSON(execute('docker',['exec',node.containerID,'crictl','--runtime-endpoint','unix:///run/k3s/containerd/containerd.sock',
   '--image-endpoint','unix:///run/k3s/containerd/containerd.sock','inspecti','-q','-o','json',imageRef]));
  requireValue(/^sha256:[a-f0-9]{64}$/.test(data.status?.id??'')&&Array.isArray(data.status.repoDigests)&&data.status.repoDigests.includes(imageRef),
   'AUTHORITY_IMAGE_NODE_DIGEST_REJECTED');
  nodes.push({...node,criImageID:data.status.id,imageRef,repoDigestVerified:true});
 }
 requireValue(same(snapshot(),before),'AUTHORITY_IMAGE_NODES_CHANGED');
 const after=readCatalogTarget(targetProfile,context,get);
 requireValue(target.transition.targetSHA256===after.transition.targetSHA256,'AUTHORITY_IMAGE_TARGET_CHANGED');
 return {schema:'kodex.authority-publisher-node-proof/v1',sourceSHA:source.sourceSHA,imageRef,targetSHA256:target.transition.targetSHA256,
  clusterUID:targetProfile.clusterUID,namespaceUID:targetProfile.namespaceUID,nodes};
}

export function planPublisherImage(deployment,{source,executorSource,targetProfile,context,kubeconfig,nodeProof,admission,imageUpdateID=randomUUID()}){
 requireValue(deployment?.kind==='Deployment'&&deployment.metadata?.name===name&&deployment.metadata.namespace===namespace&&
  targetProfile.resources.some(resource=>resource.kind==='Deployment'&&resource.name===name&&resource.uid===deployment.metadata.uid)&&
  !deployment.metadata.deletionTimestamp&&uuid(imageUpdateID)&&typeof deployment.metadata.resourceVersion==='string'&&
  Number.isInteger(deployment.spec?.replicas)&&deployment.spec.replicas>0&&deployment.spec.paused!==true&&
  nodeProof.schema==='kodex.authority-publisher-node-proof/v1'&&nodeProof.sourceSHA===source.sourceSHA&&nodeProof.imageRef===source.imageRef&&
  nodeProof.targetSHA256===fingerprint(targetProfile)&&admission.proof.targetSHA256===nodeProof.targetSHA256,
  'AUTHORITY_IMAGE_PLAN_REJECTED');
 const mains=deployment.spec.template.spec.containers.filter(container=>container.name==='publisher');
 requireValue(mains.length===1&&mains[0].command?.length===1&&mains[0].command[0]==='/usr/local/bin/internal-rpc-authority-publisher'&&
  /@sha256:[a-f0-9]{64}$/.test(mains[0].image)&&mains[0].image!==source.imageRef,'AUTHORITY_IMAGE_PREDECESSOR_REJECTED');
 const afterSpec=structuredClone(deployment.spec);afterSpec.template.spec.containers.find(container=>container.name==='publisher').image=source.imageRef;
 const annotations=deployment.metadata.annotations??{};
 const plan={schema:'kodex.authority-publisher-image-plan/v1',imageUpdateID,source,executorSource,targetProfile,context,kubeconfig,nodeProof,admission,
  deploymentUID:deployment.metadata.uid,beforeResourceVersion:deployment.metadata.resourceVersion,beforeSpec:structuredClone(deployment.spec),afterSpec,
  beforeAnnotations:structuredClone(annotations),afterAnnotations:{...annotations,[marker]:JSON.stringify({imageUpdateID,sourceSHA:source.sourceSHA,publicationSHA256:source.publicationSHA256})}};
 validatePublisherImagePlan(plan);return plan;
}

export function validatePublisherImagePlan(plan){
 requireValue(plan?.schema==='kodex.authority-publisher-image-plan/v1'&&uuid(plan.imageUpdateID)&&uuid(plan.deploymentUID)&&
  typeof plan.executorSource?.path==='string'&&resolve(plan.executorSource.path)===plan.executorSource.path&&/^[a-f0-9]{40}$/.test(plan.executorSource.revision??'')&&
  /^[0-9]+$/.test(plan.beforeResourceVersion??'')&&/^[a-f0-9]{40}$/.test(plan.source?.sourceSHA??'')&&
  ['publicationSHA256','buildSHA256','contextSHA256'].every(key=>sha(plan.source[key]))&&
  /^registry\.local\.kodex\/kodex\/internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(plan.source.imageRef??'')&&
  Number.isInteger(plan.beforeSpec?.replicas)&&plan.beforeSpec.replicas>0&&plan.beforeSpec.paused!==true,
  'AUTHORITY_IMAGE_PLAN_REJECTED');
 const mains=plan.beforeSpec.template?.spec?.containers?.filter(container=>container.name==='publisher');
 requireValue(mains?.length===1&&mains[0].command?.length===1&&mains[0].command[0]==='/usr/local/bin/internal-rpc-authority-publisher'&&
  /@sha256:[a-f0-9]{64}$/.test(mains[0].image??'')&&mains[0].image!==plan.source.imageRef,'AUTHORITY_IMAGE_PLAN_REJECTED');
 const expected=structuredClone(plan.beforeSpec);expected.template.spec.containers.find(container=>container.name==='publisher').image=plan.source.imageRef;
 requireValue(same(expected,plan.afterSpec)&&same(plan.afterAnnotations,{...plan.beforeAnnotations,[marker]:JSON.stringify({imageUpdateID:plan.imageUpdateID,
  sourceSHA:plan.source.sourceSHA,publicationSHA256:plan.source.publicationSHA256})})&&
  plan.nodeProof?.targetSHA256===fingerprint(plan.targetProfile)&&plan.nodeProof.imageRef===plan.source.imageRef&&
  plan.nodeProof.sourceSHA===plan.source.sourceSHA&&Array.isArray(plan.nodeProof.nodes)&&plan.nodeProof.nodes.length>0&&plan.nodeProof.nodes.length<=64&&
  new Set(plan.nodeProof.nodes.map(node=>node.uid)).size===plan.nodeProof.nodes.length&&
  plan.nodeProof.nodes.every(node=>uuid(node.uid)&&/^[a-z0-9-]+$/.test(node.name)&&/^[a-f0-9]{64}$/.test(node.containerID)&&
   /^sha256:[a-f0-9]{64}$/.test(node.criImageID)&&node.imageRef===plan.source.imageRef&&node.repoDigestVerified===true)&&
  plan.admission?.proof?.targetSHA256===fingerprint(plan.targetProfile),'AUTHORITY_IMAGE_PLAN_REJECTED');
}

// Один exact image CAS сохраняет sidecars, volumes, replica count и policy pins.
// UNKNOWN требует нового BEFORE readback; CONVERGING не объявляется READY.
export async function publisherImageStep({phase,plan,journal,get,resources,gate,patch}){
 validatePublisherImagePlan(plan);
 requireValue(['apply','status'].includes(phase),
  'AUTHORITY_IMAGE_PLAN_REJECTED');
 const current=get();requireValue(current.kind==='Deployment'&&current.metadata?.uid===plan.deploymentUID&&current.metadata.name===name&&
  current.metadata.namespace===namespace&&!current.metadata.deletionTimestamp,'AUTHORITY_IMAGE_DEPLOYMENT_REPLACED');
 const before=same(current.spec,plan.beforeSpec)&&sameAnnotations(current.metadata.annotations,plan.beforeAnnotations);
 const after=same(current.spec,plan.afterSpec)&&sameAnnotations(current.metadata.annotations,plan.afterAnnotations);
 requireValue(before||after,'AUTHORITY_IMAGE_DEPLOYMENT_DRIFT');
 const pods=activeDeploymentPods(resources(),plan.deploymentUID);
 const ready=after&&componentServingReady(current,pods,plan.afterSpec.replicas)&&pods.every(pod=>
  plan.nodeProof.nodes.some(node=>node.name===pod.spec.nodeName));
 const state=before?'BEFORE':ready?'READY':'CONVERGING';
 const observation={type:'OBSERVED',state,deploymentUID:plan.deploymentUID,resourceVersion:current.metadata.resourceVersion};
 if(phase==='status'){journal.append(observation);return observation;}
 const intent=journal.events.findLastIndex(event=>event.type==='INTENT'&&event.step==='apply');
 if(after){requireValue(intent>0,'AUTHORITY_IMAGE_UNOWNED_STATE');journal.append(observation);return observation;}
 if(intent>=0)requireValue(journal.events.some((event,index)=>index>intent&&event.type==='OBSERVED'&&event.state==='BEFORE'&&
  event.resourceVersion===current.metadata.resourceVersion),'AUTHORITY_IMAGE_OUTCOME_UNKNOWN');
 else requireValue(current.metadata.resourceVersion===plan.beforeResourceVersion,'AUTHORITY_IMAGE_VERSION_CHANGED');
 const afterAnnotations={...plan.afterAnnotations};
 if(current.metadata.annotations?.['deployment.kubernetes.io/revision'])afterAnnotations['deployment.kubernetes.io/revision']=current.metadata.annotations['deployment.kubernetes.io/revision'];
 const operations=[{op:'test',path:'/metadata/uid',value:plan.deploymentUID},{op:'test',path:'/metadata/resourceVersion',value:current.metadata.resourceVersion},
  {op:'test',path:'/spec',value:plan.beforeSpec},...(current.metadata.annotations?[{op:'test',path:'/metadata/annotations',value:current.metadata.annotations}]:[]),
  {op:'add',path:'/metadata/annotations',value:afterAnnotations},{op:'replace',path:'/spec',value:plan.afterSpec}];
 patch(operations,true);await gate();journal.append({type:'INTENT',step:'apply'});
 try{patch(operations,false);}catch{journal.append({type:'UNKNOWN',step:'apply'});throw Error('AUTHORITY_IMAGE_OUTCOME_UNKNOWN');}
 const actual=get();requireValue(actual.metadata.uid===plan.deploymentUID&&same(actual.spec,plan.afterSpec)&&sameAnnotations(actual.metadata.annotations,plan.afterAnnotations),
  'AUTHORITY_IMAGE_OUTCOME_UNKNOWN');
 journal.append({type:'ACK',step:'apply',deploymentUID:actual.metadata.uid});
 const active=activeDeploymentPods(resources(),plan.deploymentUID),complete=componentServingReady(actual,active,plan.afterSpec.replicas)&&
  active.every(pod=>plan.nodeProof.nodes.some(node=>node.name===pod.spec.nodeName));
 const result={...observation,state:complete?'READY':'CONVERGING',resourceVersion:actual.metadata.resourceVersion};journal.append(result);return result;
}

export async function main(args,io={}){
 const [phase,...raw]=args,options={};
 for(let i=0;i<raw.length;i++){const key=raw[i];requireValue(['--source','--revision','--publication-plan','--publication-evidence','--build','--target','--node-proof','--maintenance-plan','--maintenance-evidence',
  '--traffic-plan','--traffic-evidence','--kubeconfig','--context','--plan','--evidence','--output','--approved-plan-sha'].includes(key)&&!Object.hasOwn(options,key)&&raw[i+1],
  'AUTHORITY_IMAGE_ARGUMENTS_REJECTED');options[key]=raw[++i];}
 requireValue(['nodes','plan','apply','status'].includes(phase),'AUTHORITY_IMAGE_ARGUMENTS_REJECTED');
 const common=['--kubeconfig','--context'];
 const sourceFlags=['--source','--revision','--publication-plan','--publication-evidence','--build','--target'];
 const gateFlags=['--maintenance-plan','--maintenance-evidence','--traffic-plan','--traffic-evidence'];
 const required=phase==='nodes'?[...common,...sourceFlags,'--output']:phase==='plan'?[...common,...sourceFlags,...gateFlags,'--node-proof','--plan']:
  [...common,'--plan','--evidence',...(phase==='apply'?['--approved-plan-sha']:[])];
 requireValue(required.every(key=>typeof options[key]==='string')&&Object.keys(options).every(key=>required.includes(key)),'AUTHORITY_IMAGE_ARGUMENTS_REJECTED');
 const execute=(program,argv,input)=>{
  try{return (io.execFileSync??execFileSync)(program,argv,{input,encoding:'utf8',timeout:35000,maxBuffer:8<<20,stdio:['pipe','pipe','pipe'],
   env:Object.fromEntries(['HOME','PATH','LANG'].filter(key=>process.env[key]).map(key=>[key,process.env[key]]))});}
  catch{throw Error('AUTHORITY_IMAGE_OPERATION_FAILED_OR_UNKNOWN');}
 };
 const kubeconfig=catalogKubeconfig(options['--kubeconfig']),context=options['--context'];
 requireValue(/^[A-Za-z0-9_.:@/-]{1,160}$/.test(context??''),'AUTHORITY_IMAGE_ARGUMENTS_REJECTED');
 const kube=(argv,input)=>execute('kubectl',['--kubeconfig',kubeconfig,'--context',context,'--request-timeout=25s',...argv],input);
 const get=(kind,resource)=>parseAuthorityPrivateJSON(kube(['-n',namespace,'get',kind,resource,'-o','json']));
 const retained=['apply','status'].includes(phase)?readAuthorityPrivateJSON(options['--plan']):undefined;
 const executorPath=resolve(fileURLToPath(new URL('../..',import.meta.url)));
 const executorSource={path:executorPath,revision:(io.inspectSource??inspectSource)(executorPath).revision};
 requireValue(/^[a-f0-9]{40}$/.test(executorSource.revision??'')&&(!retained||same(executorSource,retained.executorSource)),'AUTHORITY_IMAGE_EXECUTOR_SOURCE_CHANGED');
 const sourcePath=retained?.source.path??options['--source'],sourceSHA=retained?.source.sourceSHA??options['--revision'];
 requireValue((io.inspectSource??inspectSource)(sourcePath).revision===sourceSHA,'AUTHORITY_IMAGE_SOURCE_REJECTED');
 const publicationPlanPath=retained?.publicationPlanPath??options['--publication-plan'],publicationEvidencePath=retained?.publicationEvidencePath??options['--publication-evidence'],buildPath=retained?.buildPath??options['--build'];
 const evidence={publicationPlan:readAuthorityPrivateJSON(publicationPlanPath),publicationEvidence:readAuthorityPrivateText(publicationEvidencePath).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON)},build=readAuthorityPrivateJSON(buildPath);
 const contextHash=await (io.sourceArchiveHash??sourceArchiveHash)(sourceSHA,sourcePath);
 const source={...publisherImageSource(sourceSHA,evidence,build,contextHash),path:sourcePath,
  dockerfileSHA256:createHash('sha256').update(readFileSync(`${sourcePath}/services/internal/internal-rpc-authority/Dockerfile`)).digest('hex')};
 const profile=retained?.targetProfile??readAuthorityPrivateJSON(options['--target']);
 const target=readCatalogTarget(profile,context,get);
 const freshNodes=()=>readPublisherImageNodes({source,imageRef:source.imageRef,targetProfile:profile,context},kube,execute);
 if(phase==='nodes'){
  writeAuthorityPrivatePlan(options['--output'],freshNodes());console.log(JSON.stringify({status:'READBACK',sourceSHA,imageRef:source.imageRef}));return;
 }
 const paths=retained?.admission??{planPath:options['--maintenance-plan'],evidencePath:options['--maintenance-evidence'],
  trafficPlanPath:options['--traffic-plan'],trafficEvidencePath:options['--traffic-evidence']};
 const admission=phase==='status'?retained.admission:await readCatalogMaintenance(paths.planPath,paths.evidencePath,kube,paths);verifyCatalogAdmission(admission,target.transition);
 const nodeProof=retained?.nodeProof??readAuthorityPrivateJSON(options['--node-proof']);
 requireValue(same(nodeProof,freshNodes()),'AUTHORITY_IMAGE_NODE_PROOF_CHANGED');
 if(phase==='plan'){
  const plan={...planPublisherImage(target.publisher,{source,executorSource,targetProfile:profile,context,kubeconfig,nodeProof,admission}),publicationPlanPath,publicationEvidencePath,buildPath};
  writeAuthorityPrivatePlan(options['--plan'],plan);console.log(JSON.stringify({status:'PLAN_ONLY',planSHA256:fingerprint(plan)}));return;
 }
 const plan=retained;requireValue(same(source,plan.source)&&same(admission,plan.admission)&&context===plan.context&&kubeconfig===plan.kubeconfig&&
  (phase!=='apply'||options['--approved-plan-sha']===fingerprint(plan)),'AUTHORITY_IMAGE_PLAN_CHANGED');
 const mainOnly=structuredClone(plan.beforeSpec);mainOnly.template.spec.containers.find(container=>container.name==='publisher').image=source.imageRef;
 requireValue(same(mainOnly,plan.afterSpec),'AUTHORITY_IMAGE_PLAN_CHANGED');
 const journal=privateJournal(options['--evidence'],header(plan),{readOnly:false});
 try {
  const result=await publisherImageStep({phase,plan,journal,get:()=>get('deployment',name),
   resources:()=>readComponentServingInventory(kube,name,plan.deploymentUID),gate:async()=>{
    const current=readCatalogTarget(profile,context,get);requireValue(current.transition.targetSHA256===fingerprint(profile)&&same(nodeProof,freshNodes()),'AUTHORITY_IMAGE_TARGET_CHANGED');
    const fresh=await readCatalogMaintenance(paths.planPath,paths.evidencePath,kube,paths);verifyCatalogAdmission(fresh,current.transition);
    requireValue(same(fresh,plan.admission),'AUTHORITY_IMAGE_ADMISSION_CHANGED');
   },patch:(operations,dryRun)=>withPrivatePatchFile(operations,path=>kube(['-n',namespace,'patch','deployment',name,'--type=json','--patch-file',path,
    ...(dryRun?['--dry-run=server']:[]),'-o','name']))});
  console.log(JSON.stringify(result));
 } finally {journal.close();}
}

if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)main(process.argv.slice(2)).catch(error=>{
 console.error(JSON.stringify({status:'FAIL_OR_UNKNOWN',code:/^[A-Z_]+$/.test(error.message??'')?error.message:'AUTHORITY_IMAGE_FAILED'}));process.exitCode=1;
});
