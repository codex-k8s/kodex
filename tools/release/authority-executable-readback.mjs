import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {closeSync, fstatSync, openSync, readFileSync, readlinkSync, readdirSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';

const requireValue=(value,code)=>{if(!value)throw new Error(code);};
const sha=/^[a-f0-9]{64}$/;
const helper='/usr/local/bin/internal-rpc-authority-executable-proof';
const uuid=/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;

export function authorityImageTarget(pod, container) {
 const role=container?.name?.replace('internal-rpc-authority-','');
 requireValue(['issuer','verifier'].includes(role)&&pod?.metadata?.namespace==='kodex-system'&&uuid.test(pod.metadata.uid)&&
  !pod.metadata.deletionTimestamp&&pod.status?.phase==='Running','EXACT_AUTHORITY_IMAGE_POD_REQUIRED');
 const declared=[...(pod.spec?.containers??[]),...(pod.spec?.initContainers??[])].filter(c=>c.name===container.name);
 const statuses=[...(pod.status.containerStatuses??[]),...(pod.status.initContainerStatuses??[])].filter(c=>c.name===container.name);
 const executable=`/usr/local/bin/internal-rpc-authority-${role}`;
 requireValue(declared.length===1&&fingerprint(declared[0])===fingerprint(container)&&fingerprint(container.command)===fingerprint([executable])&&
  (!container.args||container.args.length===0)&&/^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(container.image??''),'EXACT_AUTHORITY_IMAGE_COMMAND_REQUIRED');
 const state=statuses[0];
 requireValue(statuses.length===1&&state.ready&&state.state?.running&&Number.isSafeInteger(state.restartCount)&&state.restartCount>=0&&
  /^containerd:\/\/[a-f0-9]{64}$/.test(state.containerID??'')&&state.imageID?.endsWith(`@${container.image.split('@')[1]}`),'EXACT_AUTHORITY_IMAGE_STATUS_REQUIRED');
 return {role,executable,podUID:pod.metadata.uid,podName:pod.metadata.name,namespace:pod.metadata.namespace,container:container.name,
  containerID:state.containerID.slice('containerd://'.length),imageID:state.imageID,restartCount:state.restartCount};
}

export function verifyAuthorityRuntime(target,runtime) {
 const s=runtime.status,labels=s?.labels;
 requireValue(s?.id===target.containerID&&s.state==='CONTAINER_RUNNING'&&s.imageRef===target.imageID&&
  s.metadata?.name===target.container&&s.metadata.attempt===target.restartCount&&labels?.['io.kubernetes.pod.uid']===target.podUID&&
  labels?.['io.kubernetes.pod.name']===target.podName&&labels?.['io.kubernetes.pod.namespace']===target.namespace&&
  labels?.['io.kubernetes.container.name']===target.container&&Number.isSafeInteger(runtime.info?.pid)&&runtime.info.pid>1&&
  fingerprint(runtime.info.runtimeSpec?.process?.args)===fingerprint([target.executable]),'AUTHORITY_CRI_BINDING_REJECTED');
 return runtime.info.pid;
}

function startTicks(stat) {
 const fields=stat.slice(stat.lastIndexOf(') ')+2).trim().split(/\s+/);
 requireValue(stat.includes(') ')&&/^\d+$/.test(fields[19]),'AUTHORITY_PROCESS_IDENTITY_REQUIRED');return fields[19];
}

export function readHostProcess(pid,expected,root='/proc') {
 const base=`${root}/${pid}`,before=startTicks(readFileSync(`${base}/stat`,'utf8')),ns=readlinkSync(`${base}/ns/pid`);
 const matching=()=>readdirSync(root).filter(name=>/^\d+$/.test(name)).filter(name=>{
  try{return readlinkSync(`${root}/${name}/ns/pid`)===ns&&readlinkSync(`${root}/${name}/exe`)===expected;}catch(error){if(error.code==='ENOENT'||error.code==='ESRCH')return false;throw error;}
 });
 requireValue(fingerprint(matching())===fingerprint([String(pid)])&&readlinkSync(`${base}/exe`)===expected,'ONE_AUTHORITY_PROCESS_REQUIRED');
 const fd=openSync(`${base}/exe`,'r');
 try {
  const stat=fstatSync(fd);requireValue(stat.isFile()&&stat.size>0&&stat.size<=128*1024*1024,'BOUNDED_AUTHORITY_EXECUTABLE_REQUIRED');
  const binarySHA256=createHash('sha256').update(readFileSync(fd)).digest('hex');
  requireValue(startTicks(readFileSync(`${base}/stat`,'utf8'))===before&&readlinkSync(`${base}/exe`)===expected&&
   readlinkSync(`${base}/ns/pid`)===ns&&fingerprint(matching())===fingerprint([String(pid)]),'AUTHORITY_PROCESS_CHANGED');
  return {binarySHA256,startTicks:before,device:String(stat.dev),inode:String(stat.ino)};
 }finally{closeSync(fd);}
}

// CRI JSON с env остаётся только в памяти. Наружу выходят лишь закрытая identity и digest.
export function inspectHostAuthority(target,io={}) {
 const runtime=io.runtime??(id=>JSON.parse(execFileSync('k3s',['crictl','inspect',id],{encoding:'utf8',stdio:'pipe',timeout:10_000,maxBuffer:4<<20})));
 const processReader=io.processReader??readHostProcess;
 if(!io.runtime||!io.processReader)requireValue(process.getuid?.()===0,'SRE_HOST_ROOT_REQUIRED');
 const pid=verifyAuthorityRuntime(target,runtime(target.containerID)),proof=processReader(pid,target.executable);
 requireValue(sha.test(proof.binarySHA256)&&/^\d+$/.test(proof.startTicks),'AUTHORITY_PROCESS_PROOF_REQUIRED');
 requireValue(verifyAuthorityRuntime(target,runtime(target.containerID))===pid&&
  fingerprint(processReader(pid,target.executable))===fingerprint(proof),'AUTHORITY_CRI_PROCESS_CHANGED');
 return {version:1,role:target.role,pid,...proof};
}

export function readAuthorityExecutable(pod,container,{kube,k3sSudo=false,run=execFileSync}) {
 const hot=container.command?.includes('/workspace/tools/dev/run-go-hot-reload.sh');
 if(hot) {
  const roleCommand=/^internal-rpc-authority-(issuer|verifier)$/.test(container.name)&&container.args?.[1]===`./cmd/${container.name}`;
  const publisherCommand=container.name==='publisher'&&container.args?.[1]==='./cmd/internal-rpc-authority-publisher'&&container.args?.[2]==='publisher';
  requireValue(container.args?.[0]==='services/internal/internal-rpc-authority'&&(roleCommand||publisherCommand)&&
   /^[a-z0-9-]+$/.test(container.args[2]??''),'EXACT_AUTHORITY_SOURCE_COMMAND_REQUIRED');
  const declared=[...(pod.spec?.containers??[]),...(pod.spec?.initContainers??[])].filter(value=>value.name===container.name),status=[...(pod.status?.containerStatuses??[]),...(pod.status?.initContainerStatuses??[])].filter(value=>value.name===container.name);
  requireValue(pod.metadata?.namespace==='kodex-system'&&uuid.test(pod.metadata.uid??'')&&!pod.metadata.deletionTimestamp&&pod.status?.phase==='Running'&&declared.length===1&&fingerprint(declared[0])===fingerprint(container)&&
   status.length===1&&status[0].ready&&status[0].state?.running&&Number.isSafeInteger(status[0].restartCount)&&/^containerd:\/\/[a-f0-9]{64}$/.test(status[0].containerID??''),'EXACT_AUTHORITY_SOURCE_POD_REQUIRED');
  const target={podUID:pod.metadata.uid,podSpecSHA256:fingerprint(pod.spec),containerID:status[0].containerID,restartCount:status[0].restartCount};
  const script='expected=$1; count=0; result=; for entry in /proc/[0-9]*/exe; do target=$(readlink "$entry" 2>/dev/null) || continue; if [ "$target" = "$expected" ] || [ "$target" = "$expected (deleted)" ]; then count=$((count+1)); result=$(sha256sum "$entry") || exit 1; fi; done; [ "$count" = 1 ] || exit 1; printf "%s\\n" "$result"';
  const digest=kube('exec',pod.metadata.name,'-n','kodex-system','-c',container.name,'--','sh','-c',script,'authority-source',`/tmp/kodex-dev-${container.args[2]}/build/main`).split(/\s/)[0];
  requireValue(sha.test(digest),'AUTHORITY_SOURCE_PROOF_INVALID');
  const after=JSON.parse(kube('get','pod',pod.metadata.name,'-n','kodex-system','-o','json')),afterContainer=[...(after.spec?.containers??[]),...(after.spec?.initContainers??[])].find(value=>value.name===container.name),afterStatus=[...(after.status?.containerStatuses??[]),...(after.status?.initContainerStatuses??[])].find(value=>value.name===container.name);
  requireValue(after.metadata?.uid===target.podUID&&fingerprint(after.spec)===target.podSpecSHA256&&fingerprint(afterContainer)===fingerprint(container)&&afterStatus?.containerID===target.containerID&&afterStatus.restartCount===target.restartCount&&afterStatus.ready&&afterStatus.state?.running,'AUTHORITY_SOURCE_POD_CHANGED');return digest;
 }
 const target=authorityImageTarget(pod,container);
 const proof=JSON.parse(k3sSudo?run('sudo',['-n',process.execPath,fileURLToPath(import.meta.url),'--host'],
  {input:JSON.stringify(target),encoding:'utf8',stdio:'pipe',timeout:30_000,maxBuffer:65536}):
  kube('exec',pod.metadata.name,'-n','kodex-system','-c',container.name,'--',helper,'--role',target.role));
 requireValue(proof.version===1&&proof.role===target.role&&Number.isSafeInteger(proof.pid)&&proof.pid>0&&
  /^\d+$/.test(proof.startTicks)&&sha.test(proof.binarySHA256),'AUTHORITY_IMAGE_PROOF_INVALID');
 const after=JSON.parse(kube('get','pod',pod.metadata.name,'-n','kodex-system','-o','json'));
 const afterContainer=[...(after.spec?.containers??[]),...(after.spec?.initContainers??[])].find(c=>c.name===container.name);
 requireValue(fingerprint(authorityImageTarget(after,afterContainer))===fingerprint(target)&&fingerprint(after.spec)===fingerprint(pod.spec),
  'AUTHORITY_IMAGE_POD_CHANGED');
 return proof.binarySHA256;
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
 try {
  requireValue(process.argv.length===3&&process.argv[2]==='--host','INVALID_ARGUMENT');
  const input=readFileSync(0,'utf8');requireValue(input.length<=4096,'BOUNDED_HOST_TARGET_REQUIRED');
  const target=JSON.parse(input);
  requireValue(['issuer','verifier'].includes(target.role)&&target.executable===`/usr/local/bin/internal-rpc-authority-${target.role}`&&
   sha.test(target.containerID)&&uuid.test(target.podUID)&&target.namespace==='kodex-system','EXACT_HOST_TARGET_REQUIRED');
  process.stdout.write(JSON.stringify(inspectHostAuthority(target))+'\n');
 }catch(error){process.stderr.write(`Authority executable readback failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'READBACK_FAILED'}\n`);process.exitCode=1;}
}
