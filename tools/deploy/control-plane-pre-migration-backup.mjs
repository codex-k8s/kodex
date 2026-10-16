#!/usr/bin/env node
// Read-only снимок перед локальной forward migration. Не является restore drill
// всей платформы: S3 и другие домены этим инструментом не копируются.
import { spawn, execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { open, lstat, realpath, mkdtemp, statfs } from "node:fs/promises";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const ensure=(ok,code)=>{if(!ok)throw Error(code);};
const namespace="kodex-system";
export function sourceDatabasePod(stateful,pods) {
 ensure(stateful?.kind==="StatefulSet" && stateful.metadata?.name==="kodex-postgresql" &&
  stateful.metadata.namespace===namespace && stateful.metadata.uid && stateful.spec?.replicas===1,"BACKUP_DATABASE_SCOPE_INVALID");
 const selected=pods.filter(pod=>!pod.metadata.deletionTimestamp && pod.metadata.ownerReferences?.some(owner=>
  owner.controller===true && owner.kind==="StatefulSet" && owner.uid===stateful.metadata.uid));
 ensure(selected.length===1 && selected[0].status?.phase==="Running","BACKUP_DATABASE_POD_INVALID");
 const pod=selected[0],container=pod.status.containerStatuses?.find(item=>item.name==="postgresql");
 ensure(pod.metadata.uid && container?.ready && container.containerID && container.imageID,"BACKUP_DATABASE_NOT_READY");
 return {podName:pod.metadata.name,podUID:pod.metadata.uid,containerID:container.containerID,imageID:container.imageID,restarts:container.restartCount};
}

async function backup(context,parent) {
 ensure(context==="k3d-kodex" && resolve(parent)===parent && await realpath(parent)===parent,"BACKUP_LOCAL_CONTEXT_REQUIRED");
 const stat=await lstat(parent);
 ensure(stat.isDirectory() && stat.uid===process.getuid() && (stat.mode&0o077)===0,"BACKUP_PRIVATE_DIRECTORY_REQUIRED");
 const space=await statfs(parent);
 ensure(space.bavail*space.bsize>=4*1024**3,"BACKUP_INSUFFICIENT_DISK_SPACE");
 const kube=args=>{try{return execFileSync("kubectl",["--context",context,"--request-timeout=30s","-n",namespace,...args],
  {encoding:"utf8",timeout:35000,maxBuffer:4<<20,stdio:["ignore","pipe","pipe"]});}catch{throw Error("BACKUP_KUBERNETES_READ_FAILED");}};
 const workload=JSON.parse(kube(["get","statefulset","kodex-postgresql","-o","json"]));
 const before=sourceDatabasePod(workload,JSON.parse(kube(["get","pods","-o","json"])).items);
 const directory=await mkdtemp(join(parent,"cp-pre-migration-"));
 const path=join(directory,"control-plane.dump");
 const file=await open(path,constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL|constants.O_NOFOLLOW,0o600);
 const digest=createHash("sha256");let size=0;
 const child=spawn("kubectl",["--context",context,"-n",namespace,"exec",before.podName,"-c","postgresql","--",
  "pg_dump","--username=postgres","--dbname=control_plane","--format=custom"],{stdio:["ignore","pipe","pipe"],timeout:600000});
 child.stderr.resume();
 const finished=new Promise(resolve=>{child.once("error",()=>resolve(false));child.once("close",(code,signal)=>resolve(code===0 && !signal));});
 let success=false;
 try{
  for await(const chunk of child.stdout){
   size+=chunk.length;ensure(size<=2*1024**3,"BACKUP_SIZE_BUDGET_EXCEEDED");digest.update(chunk);
   // FileHandle.write может быть частичным даже для обычного файла.
   for(let offset=0;offset<chunk.length;){const result=await file.write(chunk,offset,chunk.length-offset);ensure(result.bytesWritten>0,"BACKUP_WRITE_FAILED");offset+=result.bytesWritten;}
  }
  ensure(await finished,"BACKUP_DUMP_FAILED");await file.sync();success=true;
 }finally{if(!success)child.kill("SIGKILL");await finished;await file.close();}
 const afterWorkload=JSON.parse(kube(["get","statefulset","kodex-postgresql","-o","json"]));
 const after=sourceDatabasePod(afterWorkload,JSON.parse(kube(["get","pods","-o","json"])).items);
 ensure(afterWorkload.metadata.uid===workload.metadata.uid && JSON.stringify(before)===JSON.stringify(after),"BACKUP_SOURCE_INSTANCE_CHANGED");
 let toc;
 try{toc=execFileSync("pg_restore",["--list",path],{encoding:"utf8",timeout:30000,maxBuffer:16<<20,stdio:["ignore","pipe","pipe"]});}
 catch{throw Error("BACKUP_ARCHIVE_READBACK_FAILED");}
 ensure(size>5 && /TABLE DATA control_plane runs /.test(toc) && /TABLE DATA control_plane artifact_heads /.test(toc),"BACKUP_REQUIRED_TABLES_ABSENT");
 const evidence={status:"PASS",scope:"control-plane-custom-dump-and-toc-only",restore:"NOT RUN",at:new Date().toISOString(),
  ...before,statefulSetUID:workload.metadata.uid,bytes:size,sha256:digest.digest("hex"),path};
 const receipt=await open(join(directory,"receipt.json"),"wx",0o600);
 try{await receipt.writeFile(`${JSON.stringify(evidence)}\n`);await receipt.sync();}finally{await receipt.close();}
 const directoryHandle=await open(directory,constants.O_RDONLY|constants.O_DIRECTORY|constants.O_NOFOLLOW);
 try{await directoryHandle.sync();}finally{await directoryHandle.close();}
 const parentHandle=await open(parent,constants.O_RDONLY|constants.O_DIRECTORY|constants.O_NOFOLLOW);
 try{await parentHandle.sync();}finally{await parentHandle.close();}
 return evidence;
}

if(process.argv[1] && pathToFileURL(process.argv[1]).href===import.meta.url){
 const args=process.argv.slice(2);
 try{ensure(args.length===4 && args[0]==="--context" && args[2]==="--output-directory","BACKUP_ARGUMENT_INVALID");
  process.stdout.write(`${JSON.stringify(await backup(args[1],args[3]))}\n`);
 }catch(error){process.stderr.write(`Control-plane backup failed: ${/^[A-Z_]+$/.test(error.message)?error.message:"BACKUP_FAILED"}\n`);process.exitCode=1;}
}
