import test from "node:test";
import assert from "node:assert/strict";
import { sourceDatabasePod, backupOptions } from "./control-plane-pre-migration-backup.mjs";
import { mkdtemp, writeFile, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { createHash } from "node:crypto";

const execute=promisify(execFile);
const clusterUID="11111111-1111-4111-8111-111111111111",namespaceUID="22222222-2222-4222-8222-222222222222";
const options=parent=>["--context","k3d-kodex","--kubeconfig","/synthetic/kubeconfig","--cluster-uid",clusterUID,"--namespace-uid",namespaceUID,"--output-directory",parent];

test("backup requires explicit kubeconfig and independently inventoried target UIDs",()=>{
 assert.equal(backupOptions(options("/tmp/private")).clusterUID,clusterUID);
 for(const args of [options("/tmp/private").slice(2),[...options("/tmp/private"),"--context","k3d-kodex"],
  options("/tmp/private").map(v=>v==="/synthetic/kubeconfig"?"relative":v),
  options("/tmp/private").map(v=>v===clusterUID?"unknown":v),
  options("/tmp/private").map(v=>v==="k3d-kodex"?"other":v)])assert.throws(()=>backupOptions(args));
});

// Настоящий CLI запускается с синтетическими внешними командами; живого кластера нет.
for(const scenario of ["success","foreign-before","foreign-after","replacement"]){
 test(`backup CLI ${scenario} preserves explicit target and refuses uncertain source`,async()=>{
  const root=await mkdtemp(join(tmpdir(),"kodex-preimage-test-"));
  try{
   const calls=join(root,"calls.jsonl");
   const mock=`#!${process.execPath}
const fs=require('node:fs');const args=process.argv.slice(2),log=${JSON.stringify(calls)};
let previous=[];try{previous=fs.readFileSync(log,'utf8').trim().split('\\n').filter(Boolean).map(JSON.parse);}catch{}
fs.appendFileSync(log,JSON.stringify(args)+'\\n');
const pos=args.indexOf('get'),kind=args[pos+1],name=args[pos+2],after=previous.some(a=>a.includes('exec'));
if(args.includes('exec')){process.stdout.write('PGDMP-synthetic-fixture');process.exit(0);}
let value;
if(kind==='namespace')value={kind:'Namespace',metadata:{name,uid:name==='kube-system'?${JSON.stringify(clusterUID)}:${JSON.stringify(namespaceUID)}}};
if(kind==='namespace' && name==='kube-system' && (${JSON.stringify(scenario)}==='foreign-before'||(${JSON.stringify(scenario)}==='foreign-after'&&after)))value.metadata.uid='foreign';
if(kind==='statefulset')value={kind:'StatefulSet',metadata:{name:'kodex-postgresql',namespace:'kodex-system',uid:'database'},spec:{replicas:1}};
if(kind==='pods')value={items:[{metadata:{name:'database-0',uid:after&&${JSON.stringify(scenario)}==='replacement'?'replacement':'pod',ownerReferences:[{kind:'StatefulSet',controller:true,uid:'database'}]},status:{phase:'Running',containerStatuses:[{name:'postgresql',ready:true,containerID:'container',imageID:'image',restartCount:0}]}}]};
if(!value)process.exit(2);process.stdout.write(JSON.stringify(value));
`;
   await writeFile(join(root,"kubectl"),mock,{mode:0o700});
   await writeFile(join(root,"pg_restore"),`#!${process.execPath}\nprocess.stdout.write('TABLE DATA control_plane runs owner\\nTABLE DATA control_plane artifact_heads owner\\n');\n`,{mode:0o700});
   let result,failed=false;
   try{result=await execute(process.execPath,[fileURLToPath(new URL("./control-plane-pre-migration-backup.mjs",import.meta.url)),...options(root)],
    {env:{PATH:root},timeout:15000});}catch(error){result=error;failed=true;}
   assert.equal(failed,scenario!=="success",result.stderr);
   const invoked=(await readFile(calls,"utf8")).trim().split("\n").map(JSON.parse);
   assert.ok(invoked.every(args=>args[0]==="--kubeconfig"&&args[1]==="/synthetic/kubeconfig"&&args[2]==="--context"&&args[3]==="k3d-kodex"));
   assert.equal(invoked.some(args=>args.includes("exec")),scenario!=="foreign-before");
   const snapshots=(await readdir(root)).filter(name=>name.startsWith("cp-pre-migration-"));
   if(scenario==="success"){
    const receipt=JSON.parse(result.stdout),bytes=await readFile(receipt.path);
    assert.equal(receipt.sha256,createHash("sha256").update(bytes).digest("hex"));
    assert.equal(receipt.clusterUID,clusterUID);assert.equal(receipt.namespaceUID,namespaceUID);
    assert.equal(receipt.restore,"NOT RUN");assert.equal(snapshots.length,1);
   }else for(const directory of snapshots)assert.ok(!(await readdir(join(root,directory))).includes("receipt.json"));
  }finally{await rm(root,{recursive:true,force:true});}
 });
}

test("pre-migration backup pins singleton database and exact ready instance",()=>{
 const stateful={kind:"StatefulSet",metadata:{name:"kodex-postgresql",namespace:"kodex-system",uid:"database"},spec:{replicas:1}};
 const pod={metadata:{name:"database-0",uid:"pod",ownerReferences:[{kind:"StatefulSet",controller:true,uid:"database"}]},
  status:{phase:"Running",containerStatuses:[{name:"postgresql",ready:true,containerID:"container",imageID:"image",restartCount:0}]}};
 assert.equal(sourceDatabasePod(stateful,[pod]).podUID,"pod");
 for(const change of [value=>value.metadata.namespace="foreign",value=>value.spec.replicas=2,value=>value.metadata.uid="replacement"]){
  const value=structuredClone(stateful);change(value);assert.throws(()=>sourceDatabasePod(value,[pod]));
 }
 assert.throws(()=>sourceDatabasePod(stateful,[pod,pod]));
 pod.status.containerStatuses[0].ready=false;assert.throws(()=>sourceDatabasePod(stateful,[pod]));
});
