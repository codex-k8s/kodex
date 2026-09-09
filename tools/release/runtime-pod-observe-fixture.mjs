// Синтетическая identity без server access; только local CLI/unit fixtures.
export function runtimePodFixture(binding) {
 const image=binding.image,names=['workspace-prepare','workspace-init','role-runtime','provider-runtime'];
 const annotations=Object.fromEntries(Object.entries({'revision-digest':binding.revisionDigest,'project-hash':binding.projectHash,'session-hash':binding.sessionHash,'turn-hash':binding.turnHash,attempt:String(binding.attempt),'lease-ref':'lease_fixture','execution-binding-digest':'e'.repeat(64)}).map(([k,v])=>[`runtime.kodex.dev/${k}`,v]));
 const container=name=>({name,image,...(name==='role-runtime'?{args:['runtime-session']}:{})});
 const status=(name,index)=>({name,imageID:image,containerID:`containerd://${String(index+1).repeat(64)}`,restartCount:0,ready:index>=2,state:index>=2?{running:{startedAt:'2026-09-09T01:00:00Z'}}:{terminated:{exitCode:0,startedAt:'2026-09-09T01:00:00Z'}}});
 return {metadata:{name:'runtime-fixture',namespace:'kodex-runtime',uid:'11111111-1111-4111-8111-111111111111',labels:{'runtime.kodex.dev/managed':'true','runtime.kodex.dev/mode':'turn','kodex.dev/environment':'staging'},annotations},spec:{nodeName:'fixture-node',initContainers:names.slice(0,2).map(container),containers:names.slice(2).map(container)},status:{phase:'Running',initContainerStatuses:names.slice(0,2).map(status),containerStatuses:names.slice(2).map((n,i)=>status(n,i+2))}};
}
export function runtimeCRIFixture(pod) {
 const status=pod.status.containerStatuses[0];return {status:{id:status.containerID.slice('containerd://'.length),state:'CONTAINER_RUNNING',imageRef:status.imageID,metadata:{name:'role-runtime',attempt:0},labels:{'io.kubernetes.pod.uid':pod.metadata.uid,'io.kubernetes.pod.name':pod.metadata.name,'io.kubernetes.pod.namespace':'kodex-runtime','io.kubernetes.container.name':'role-runtime'}},info:{pid:10,runtimeSpec:{process:{args:['/usr/local/bin/kodex-init','entrypoint','/usr/local/bin/kodex-agent-runner','runtime-session']}}}};
}
