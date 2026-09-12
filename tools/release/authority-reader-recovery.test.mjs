import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync,spawnSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {planSidecars,planRecoverySidecars,rollbackSidecars,validateReaderRecovery,readerRecoveryRevision,readerRecoveryImage,readerRecoveryBinaries,readerRecoveryImageBinaries} from './authority-sidecar-rollout.mjs';
import {recoveryPatch} from './authority-reader-recovery.mjs';

function fixture(){
 const publisher={metadata:{namespace:'kodex-system',name:'internal-rpc-authority-publisher',uid:'publisher'},spec:{replicas:1}},registry={metadata:{namespace:'kodex-system',name:'internal-rpc-authority-publisher-target-registry',uid:'registry'},data:{registry:'synthetic'}};
 const rotation={version:3,action:'rotate',intentID:'ac1954d0-24c6-49bf-adb1-621b8ec8f548',ownerOperationID:'ae8d8192-e49b-57d7-8844-6639850359db',publisher:{uid:'publisher',desiredSpecSHA256:fingerprint(publisher.spec)},registry:{uid:'registry',desiredDataSHA256:fingerprint(registry.data)}};
 const capability={version:1,protocol:2,revision:readerRecoveryRevision,imageVersion:readerRecoveryRevision,binaries:{...readerRecoveryBinaries},imageBinaries:{...readerRecoveryImageBinaries}};
 const deployment={kind:'Deployment',metadata:{name:'role-image-builder',namespace:'kodex-system',uid:'deployment',resourceVersion:'4',generation:2,labels:{'app.kubernetes.io/part-of':'kodex'}},status:{observedGeneration:2,replicas:1,updatedReplicas:1,readyReplicas:0,availableReplicas:0},spec:{replicas:1,template:{spec:{containers:[{name:'application',image:'unchanged'}],initContainers:[{name:'internal-rpc-authority-issuer',command:['/usr/local/bin/internal-rpc-authority-issuer'],image:'registry.invalid/old@sha256:'+'a'.repeat(64)},{name:'grant',image:'unchanged-grant'}],volumes:[{name:'trust',secret:{secretName:'unchanged'}}]}}}};
 const target={name:deployment.metadata.name,profile:'image',roles:['issuer'],image:readerRecoveryImage};
 return {publisher,registry,rotation,capability,deployment,target};
}
test('recovery is forward-only, exact incident, and changes only the selected reader',()=>{
 const f=fixture();assert.throws(()=>planSidecars(f.deployment,f.target),/HEALTHY/);
 const plan=planRecoverySidecars(f.deployment,f.target,f.rotation,f.publisher,f.registry,f.capability);
 const expected=structuredClone(f.deployment.spec);expected.template.spec.initContainers[0].image=readerRecoveryImage;
 assert.deepEqual(plan.after,expected);assert.throws(()=>rollbackSidecars({...f.deployment,spec:plan.after},plan),/FORWARD_ONLY/);
 assert.deepEqual(recoveryPatch(f.deployment,plan).map(p=>p.path),['/metadata/uid','/metadata/resourceVersion','/spec','/spec']);
 for(const mutate of [f=>f.registry.data.registry='changed',f=>f.publisher.spec.replicas=2,f=>f.rotation.ownerOperationID='another',f=>f.capability.binaries.issuer='a'.repeat(64)]){
  const x=fixture();mutate(x);assert.throws(()=>validateReaderRecovery(x.rotation,x.publisher,x.registry,x.capability));
 }
 for(const mutate of [d=>d.metadata.resourceVersion='5',d=>d.metadata.uid='foreign',d=>d.spec.replicas=2]){const d=structuredClone(f.deployment);mutate(d);assert.throws(()=>recoveryPatch(d,plan),/CAS_DRIFT/);}
 assert.throws(()=>planRecoverySidecars(f.deployment,{...f.target,image:'registry.invalid/other@sha256:'+'b'.repeat(64)},f.rotation,f.publisher,f.registry,f.capability),/TARGET_REJECTED/);
});
test('public recovery CLI observes lost ACK without duplicate PATCH and keeps normal health guard',()=>{
 const dir=mkdtempSync(join(tmpdir(),'reader-recovery-'));
 try{
  const f=fixture(),bin=join(dir,'bin');mkdirSync(bin);
  const stateFile=join(dir,'state.json'),plan=join(dir,'plan.json'),evidence=join(dir,'evidence.jsonl');
  writeFileSync(stateFile,JSON.stringify({...f,patches:0}));
  for(const [name,value]of [['manifest',{version:1,targets:[f.target]}],['rotation',f.rotation],['capability',f.capability]])writeFileSync(join(dir,name+'.json'),JSON.stringify(value));
  writeFileSync(join(bin,'sudo'),`#!/usr/bin/env node
const fs=require('node:fs'),a=process.argv.slice(2),p=process.env.READER_TEST_STATE,s=JSON.parse(fs.readFileSync(p));
if(a.splice(0,5).join(' ')!=='-n k3s kubectl --context default')process.exit(80);
let out;if(a[0]==='config')out='default';
else if(a[0]==='get'){if(a[1]==='namespace')out=JSON.stringify({metadata:{uid:'namespace',labels:{'kodex.dev/environment':'staging'}}});else if(a[1]==='configmap')out=JSON.stringify(s.registry);else if(a[1]==='deployment')out=JSON.stringify(a[2]==='internal-rpc-authority-publisher'?s.publisher:s.deployment);else process.exit(81);}
else if(a[0]==='patch'){const patch=JSON.parse(a[a.indexOf('-p')+1]);for(const t of patch.filter(x=>x.op==='test')){const actual=t.path.split('/').slice(1).reduce((x,k)=>x[k],s.deployment);if(JSON.stringify(actual)!==JSON.stringify(t.value))process.exit(82);}s.deployment.spec=patch.at(-1).value;s.deployment.metadata.resourceVersion='5';s.patches++;fs.writeFileSync(p,JSON.stringify(s));process.exit(83);}else process.exit(84);process.stdout.write(out);
`,{mode:0o755});
  const cli=(...args)=>spawnSync(process.execPath,[fileURLToPath(new URL('./authority-reader-recovery.mjs',import.meta.url)),...args,'--context','default'],{encoding:'utf8',env:{...process.env,PATH:bin+':'+process.env.PATH,READER_TEST_STATE:stateFile},timeout:30000});
  let r=cli('plan','--manifest',join(dir,'manifest.json'),'--rotation-plan',join(dir,'rotation.json'),'--capability',join(dir,'capability.json'),'--output',plan);assert.equal(r.status,0,r.stderr);
  r=cli('apply','--plan',plan,'--evidence',evidence,'--confirm','RECOVER-STAGING-AUTHORITY-READERS');assert.equal(r.status,0,r.stderr);assert.equal(JSON.parse(r.stdout).targets[0].state,'AFTER');assert.equal(JSON.parse(r.stdout).targets[0].binariesVerified,false);
  assert.equal(JSON.parse(readFileSync(stateFile)).patches,1);assert.deepEqual(readFileSync(evidence,'utf8').trim().split('\n').map(l=>JSON.parse(l).status),['INTENT','TARGET_INTENT','UNKNOWN','APPLIED']);
  r=cli('observe','--plan',plan,'--evidence',evidence);assert.equal(r.status,0,r.stderr);assert.equal(JSON.parse(readFileSync(stateFile)).patches,1);
  r=cli('apply','--plan',plan,'--evidence',evidence,'--confirm','RECOVER-STAGING-AUTHORITY-READERS');assert.equal(r.status,1);assert.equal(JSON.parse(readFileSync(stateFile)).patches,1);
  r=cli('apply','--plan',plan,'--evidence',join(dir,'stale.jsonl'),'--confirm','RECOVER-STAGING-AUTHORITY-READERS');assert.equal(r.status,1);assert.equal(JSON.parse(readFileSync(stateFile)).patches,1);
 }finally{rmSync(dir,{recursive:true,force:true});}
});
