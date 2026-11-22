import {execFileSync} from 'node:child_process';
import {readSync} from 'node:fs';
import {performance} from 'node:perf_hooks';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {readPairedAuthorityPublisherServingInventory,pairedAuthorityPublisher} from './multi-component-serving-inventory.mjs';
import {createPairedTokenNativeReadClient} from './paired-token-native-cas.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {componentServingReady} from './portable-component-migration.mjs';
import {fingerprint} from './scoped-release.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';

const file=fileURLToPath(import.meta.url),maxInput=16384,ceiling=5000;
const ensure=(value,code)=>{if(!value)throw Error(code);};
const rejected='PAIRED_AUTHORITY_PUBLISHER_INPUT_REJECTED',changed='PAIRED_AUTHORITY_PUBLISHER_CHANGED';

// Оба чтения используют одни installation pins, полный Deployment и
// монотонный предел. Наружу не передаются спецификации Pod или credentials.
export async function observePairedAuthorityPublisher(client,{target,deploymentSHA256},deadline){
 ensure(typeof deploymentSHA256==='string'&&/^[a-f0-9]{64}$/.test(deploymentSHA256),rejected);
 const read=async()=>{
  const {deployment,observed}=await client.readAuthorityPublisherBoundary({deadline});
  ensure(deployment?.apiVersion==='apps/v1'&&deployment.kind==='Deployment'&&
   deployment.metadata?.name===pairedAuthorityPublisher&&portableRunnerTarget(target,observed).allows(deployment)&&
   fingerprint(deployment)===deploymentSHA256,changed);
  return deployment;
 };
 const deployment=await read(),uid=deployment.metadata.uid;
 const inventory=await readPairedAuthorityPublisherServingInventory(client,uid,{deadline});
 ensure(componentServingReady(deployment,activeDeploymentPods(inventory,uid),deployment.spec.replicas),
  'CATALOG_BASELINE_PUBLISHER_NOT_READY');
 await read();
 ensure(performance.now()<deadline,'PAIRED_AUTHORITY_PUBLISHER_EXPIRED');
}

// Синхронная граница нужна только для текущего owner verifier. Запускается
// один read-only дочерний процесс под исходным пределом CLI, с закрытым stdin.
export function createPairedAuthorityPublisherReader(context,kubeconfig,deadline=Infinity){
 return (deployment,target)=>{
  ensure(target?.context===context,rejected);
  const input=JSON.stringify({target,deploymentSHA256:fingerprint(deployment)});
  ensure(Buffer.byteLength(input)<=maxInput,rejected);
  const timeout=Math.min(ceiling,Math.floor(deadline-performance.now()));
  ensure(timeout>0,'PAIRED_AUTHORITY_PUBLISHER_EXPIRED');
  let result;
  try{
   result=execFileSync(process.execPath,[file,'--kubeconfig',kubeconfig],{
    input,encoding:'utf8',timeout,killSignal:'SIGKILL',maxBuffer:1024,
    env:Object.fromEntries(['HOME','PATH','LANG'].filter(key=>process.env[key]).map(key=>[key,process.env[key]])),
    stdio:['pipe','pipe','pipe'],
   });
  }catch{throw Error('PAIRED_AUTHORITY_PUBLISHER_READ_FAILED');}
  ensure(result==='READY\n'&&performance.now()<deadline,'PAIRED_AUTHORITY_PUBLISHER_READ_FAILED');
 };
}

if(process.argv[1]&&resolve(process.argv[1])===file){
 try{
  const deadline=performance.now()+ceiling;
  const [flag,kubeconfig,...extra]=process.argv.slice(2);
  ensure(flag==='--kubeconfig'&&kubeconfig&&extra.length===0,rejected);
  const buffer=Buffer.alloc(maxInput+1);let read=0,count;
  while(read<buffer.length&&(count=readSync(0,buffer,read,buffer.length-read,null))>0)read+=count;
  ensure(read<=maxInput,rejected);
  const input=parseAuthorityPrivateJSON(buffer.subarray(0,read).toString('utf8'));
  ensure(input&&Object.keys(input).sort().join(',')==='deploymentSHA256,target',rejected);
  const client=createPairedTokenNativeReadClient(input.target?.context,kubeconfig);
  await observePairedAuthorityPublisher(client,input,deadline);
  process.stdout.write('READY\n');
 }catch{process.stderr.write('PAIRED_AUTHORITY_PUBLISHER_READ_FAILED\n');process.exitCode=1;}
}
