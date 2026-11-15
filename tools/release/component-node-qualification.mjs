import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {resolve} from 'node:path';
import {fingerprint} from './stable-inventory.mjs';
import {validateNodeContainer} from './stable-node-registry-model.mjs';

const fail=ok=>{if(!ok)throw Error('COMPONENT_NODE_QUALIFICATION_REJECTED');};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const uid=value=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value??'');
const execute=promisify(execFile);
async function run(program,args,timeout=30000){
 try{return (await execute(program,args,{timeout,maxBuffer:8<<20,
  env:Object.fromEntries(['HOME','PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]))})).stdout;}
 catch{throw Error('COMPONENT_NODE_OPERATION_FAILED_OR_UNKNOWN');}
}

// Общая identity boundary старого cohort publisher и адресного image repair.
// Никаких registry credentials/config mutations: только exact Kubernetes/Docker readback.
export async function readComponentNodeTarget(target,kubeconfig,operation=run){
 fail(target&&/^k3d-[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(target.context??'')&&uid(target.clusterUID)&&uid(target.namespaceUID)&&
  Array.isArray(target.nodes)&&target.nodes.length>0&&target.nodes.length<=8&&target.nodes.every(n=>uid(n.uid)&&typeof n.name==='string')&&
  new Set(target.nodes.map(n=>n.uid)).size===target.nodes.length&&new Set(target.nodes.map(n=>n.name)).size===target.nodes.length&&
  typeof kubeconfig==='string'&&kubeconfig===resolve(kubeconfig)&&!/[\r\n\0]/.test(kubeconfig));
 const kube=async args=>JSON.parse(await operation('kubectl',['--kubeconfig',kubeconfig,'--context',target.context,'--request-timeout=15s',...args,'-o','json']));
 const cluster=await kube(['get','namespace','kube-system']),namespace=await kube(['get','namespace','kodex-system']),nodes=await kube(['get','nodes']);
 fail(cluster.metadata?.uid===target.clusterUID&&namespace.metadata?.uid===target.namespaceUID&&Array.isArray(nodes.items)&&
  nodes.items.length===target.nodes.length&&nodes.items.every(n=>target.nodes.some(c=>c.name===n.metadata?.name&&c.uid===n.metadata?.uid))&&
  nodes.items.every(n=>!n.metadata.deletionTimestamp&&n.status?.conditions?.some(c=>c.type==='DiskPressure'&&c.status==='False')));
 const bound=[];
 for(const node of nodes.items){const values=JSON.parse(await operation('docker',['inspect','--type','container',node.metadata.name]));
  fail(Array.isArray(values)&&values.length===1);bound.push(validateNodeContainer(node,values[0],target.context.slice(4)));}
 return bound.sort((a,b)=>a.name.localeCompare(b.name));
}

// Единственный effect — CRI pull exact digest в уже принадлежащие target узлы.
// Потерянный pull ACK не выдаётся за PASS; readback не повторяет download.
export async function qualifyComponentNodeImages({target,kubeconfig,images,mode,expectedBinding},operation=run){
 fail(['qualify','readback'].includes(mode)&&Array.isArray(images)&&images.length>0&&images.length<=32&&new Set(images).size===images.length&&
  images.every(image=>/^registry\.local\.kodex\/kodex\/[a-z-]+@sha256:[a-f0-9]{64}$/.test(image)&&!image.endsWith('0'.repeat(64))));
 const snapshot=()=>readComponentNodeTarget(target,kubeconfig,operation),before=await snapshot();
 fail(!expectedBinding||same(before,expectedBinding));
 const results=[];
 for(const imageRef of images){const nodes=[];
  for(const node of before){
   fail(same(await snapshot(),before));
   const cri=['exec',node.containerID,'crictl','--runtime-endpoint','unix:///run/k3s/containerd/containerd.sock',
    '--image-endpoint','unix:///run/k3s/containerd/containerd.sock'];
   if(mode==='qualify')await operation('docker',[...cri,'pull',imageRef],180000);
   const image=JSON.parse(await operation('docker',[...cri,'inspecti','-q','-o','json',imageRef]));
   fail(/^sha256:[a-f0-9]{64}$/.test(image.status?.id??'')&&image.status?.repoDigests?.includes(imageRef));
   nodes.push({name:node.name,uid:node.uid,imageRef,repoDigestVerified:true});
  }
  results.push({imageRef,nodes});
 }
 fail(same(await snapshot(),before));return {binding:before,images:results};
}
