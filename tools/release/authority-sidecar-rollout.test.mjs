import test from 'node:test';
import assert from 'node:assert/strict';
import {planSidecars,rollbackSidecars,validateFreshnessWatch} from './authority-sidecar-rollout.mjs';
const image='registry.invalid/authority@sha256:'+'a'.repeat(64),nextImage=image.replaceAll('a'.repeat(64),'b'.repeat(64));
const inspect=path=>({revision:(path.endsWith('next')?'b':'a').repeat(40)});
function deployment(){return {kind:'Deployment',metadata:{name:'control-api-gateway',namespace:'kodex-system',uid:'uid',resourceVersion:'7',generation:2,labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/local-profile':'hot-reload'}},status:{observedGeneration:2,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1},spec:{replicas:1,template:{spec:{containers:[{name:'app',image,volumeMounts:[{name:'source',mountPath:'/workspace',readOnly:true}]}],initContainers:[{name:'internal-rpc-authority-issuer',image,command:['/workspace/tools/dev/run-go-hot-reload.sh'],args:['services/internal/internal-rpc-authority','./cmd/internal-rpc-authority-issuer','issuer'],volumeMounts:[{name:'source',mountPath:'/workspace',readOnly:true},{name:'secret',mountPath:'/private',readOnly:true}]},{name:'platform-worker-grant-agent',image,volumeMounts:[{name:'source',mountPath:'/workspace',readOnly:true}]}],volumes:[{name:'source',hostPath:{path:'/srv/kodex-dev/old',type:'Directory'}},{name:'secret',secret:{secretName:'existing'}}]}}}};}
const target={name:'control-api-gateway',profile:'source',roles:['issuer'],source:{path:'/srv/kodex-dev/next',revision:'b'.repeat(40)}};
test('source rollout isolates issuer while app, worker, trust and original source remain exact',()=>{
 const d=deployment(),original=structuredClone(d),p=planSidecars(d,target,inspect);assert.deepEqual(d,original);
 const before=d.spec.template.spec,after=p.after.template.spec;
 assert.deepEqual(after.containers,before.containers);assert.deepEqual(after.initContainers[1],before.initContainers[1]);assert.deepEqual(after.volumes.slice(0,2),before.volumes);
 assert.equal(after.initContainers[0].volumeMounts[0].name,'dev-authority-issuer-source');assert.deepEqual(after.initContainers[0].volumeMounts[1],before.initContainers[0].volumeMounts[1]);
 assert.equal(after.volumes.at(-1).hostPath.path,target.source.path);
 const patch=rollbackSidecars({...d,spec:p.after},p);assert.deepEqual(patch.at(-1).value,d.spec);assert.equal(patch[1].path,'/metadata/resourceVersion');
 assert.throws(()=>rollbackSidecars({...d,spec:{...p.after,replicas:2}},p),/DRIFT/);
 assert.throws(()=>rollbackSidecars({...d,metadata:{...d.metadata,uid:'foreign'},spec:p.after},p),/DRIFT/);
});
test('image profile changes only exact immutable issuer and rejects hot conversion',()=>{
 assert.throws(()=>planSidecars(deployment(),{...target,profile:'image',image:nextImage},inspect),/IMMUTABLE/);
 const d=deployment(),c=d.spec.template.spec.initContainers[0];c.command=['/usr/local/bin/internal-rpc-authority-issuer'];c.args=[];
 const p=planSidecars(d,{...target,profile:'image',image:nextImage},inspect),expected=structuredClone(d.spec);expected.template.spec.initContainers[0].image=nextImage;assert.deepEqual(p.after,expected);
 assert.throws(()=>planSidecars(d,{...target,profile:'image',image:'registry.invalid/authority:latest'},inspect),/IMMUTABLE/);
});
test('foreign roles, unhealthy replicas, shared dedicated mounts and source mismatch fail closed',()=>{
 for(const mutate of [d=>d.metadata.namespace='production',d=>d.status.readyReplicas=0,d=>d.spec.template.spec.initContainers[0].volumeMounts[0].readOnly=false,d=>d.spec.template.spec.containers[0].volumeMounts[0].name='dev-authority-issuer-source']){const d=deployment();mutate(d);assert.throws(()=>planSidecars(d,target,inspect));}
 assert.throws(()=>planSidecars(deployment(),{...target,roles:['verifier']},inspect));assert.throws(()=>planSidecars(deployment(),{...target,source:{...target.source,revision:'c'.repeat(40)}},inspect));
});
function watch(){return {metadata:{namespace:'kodex-system',name:'authority-freshness-uuid'},spec:{template:{spec:{serviceAccountName:'internal-rpc-authority-migrator',automountServiceAccountToken:false,containers:[{name:'migrate',image:'docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83',command:['/workspace/tools/dev/run-go-command.sh'],args:['services/internal/internal-rpc-authority','./cmd/cli','freshness-watch'],volumeMounts:[{name:'source',mountPath:'/workspace',readOnly:true}]}],volumes:[{name:'source',hostPath:{path:'/srv/kodex-dev/next',type:'Directory'}}]}}}};}
test('policy watch pins executable source; stale, future and forged observations cannot authorize rollback',()=>{
 const now=Date.parse('2026-09-08T16:00:00Z'),cap={protocol:2,revision:'b'.repeat(40)};
 const line=(version,age)=>JSON.stringify({version,observedAt:new Date(now-age).toISOString()});
 assert.equal(validateFreshnessWatch(watch(),line(1,4000),now,cap,inspect),1);assert.equal(validateFreshnessWatch(watch(),line(2,0),now,cap,inspect),2);
 for(const [version,age]of [[1,5001],[1,-1],[3,0]])assert.throws(()=>validateFreshnessWatch(watch(),line(version,age),now,cap,inspect));
 for(const mutate of [j=>j.spec.template.spec.serviceAccountName='owner',j=>j.spec.template.spec.containers[0].image=image,j=>j.spec.template.spec.volumes[0].hostPath.path='/srv/kodex-dev/old']){const j=watch();mutate(j);assert.throws(()=>validateFreshnessWatch(j,line(1,0),now,cap,inspect));}
});

import {execFileSync,spawnSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync,chmodSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
test('public CLI recovers lost PATCH ACK once; post-activation rollback and CAS drift are rejected',()=>{
 const dir=mkdtempSync(join(tmpdir(),'authority-sidecar-'));
 try {
  const source=join(dir,'source'),bin=join(dir,'bin');mkdirSync(source,{mode:0o755});mkdirSync(bin);
  const git=(...args)=>execFileSync('git',['-C',source,...args],{stdio:'pipe'}).toString().trim();
  git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');writeFileSync(join(source,'fixture'),'synthetic\n',{mode:0o644});git('add','fixture');git('-c','user.name=kodex-agent','-c','user.email=238524843+kodex-agent@users.noreply.github.com','commit','-qm','Оснастка локального CLI');
  const capability={version:1,protocol:2,revision:git('rev-parse','HEAD'),binaries:{issuer:'b'.repeat(64),verifier:'c'.repeat(64)}};
  const capFile=join(dir,'cap.json'),manifest=join(dir,'manifest.json'),planFile=join(dir,'plan.json'),stateFile=join(dir,'state.json');writeFileSync(capFile,JSON.stringify(capability));
  const d=deployment();d.spec.template.spec.initContainers[0].command=['/usr/local/bin/internal-rpc-authority-issuer'];d.spec.template.spec.initContainers[0].args=[];
  const w=watch();w.spec.template.spec.volumes[0].hostPath.path=source;
  const initial={deployment:d,job:w,policy:1,patches:0,lostAck:true};writeFileSync(stateFile,JSON.stringify(initial));
  writeFileSync(manifest,JSON.stringify({version:1,capability:capFile,targets:[{name:d.metadata.name,profile:'image',roles:['issuer'],image:nextImage}]}));
  writeFileSync(join(bin,'kubectl'),`#!/usr/bin/env node
const fs=require('node:fs'),a=process.argv.slice(2),p=process.env.AUTHORITY_TEST_STATE,s=JSON.parse(fs.readFileSync(p));
if(a.splice(0,2).join(' ')!=='--context synthetic')process.exit(90);
let out;if(a[0]==='config')out='synthetic';else if(a[0]==='logs')out=JSON.stringify({version:s.policy,observedAt:new Date().toISOString()});
else if(a[0]==='get'){if(a[1]==='namespace')out=JSON.stringify({metadata:{uid:'namespace-uid',labels:{'kodex.dev/environment':'staging'}}});else if(a[1]==='deployment')out=JSON.stringify(s.deployment);else if(a[1]==='job')out=JSON.stringify(s.job);else process.exit(91);}
else if(a[0]==='patch'){const patch=JSON.parse(a[a.indexOf('-p')+1]);for(const test of patch.filter(p=>p.op==='test')){const value=test.path.split('/').slice(1).reduce((v,k)=>v[k],s.deployment);if(JSON.stringify(value)!==JSON.stringify(test.value))process.exit(92);}s.deployment.spec=patch.at(-1).value;s.deployment.metadata.resourceVersion=String(Number(s.deployment.metadata.resourceVersion)+1);s.patches++;fs.writeFileSync(p,JSON.stringify(s));if(s.lostAck)process.exit(93);out='patched';}else process.exit(94);process.stdout.write(out);
`,{mode:0o755});chmodSync(source,0o755);
  const cli=(...args)=>spawnSync(process.execPath,[fileURLToPath(new URL('./authority-sidecar-rollout.mjs',import.meta.url)),...args,'--context','synthetic'],{encoding:'utf8',env:{...process.env,PATH:bin+':'+process.env.PATH,AUTHORITY_TEST_STATE:stateFile},timeout:30000});
  let result=cli('plan','--manifest',manifest,'--output',planFile);assert.equal(result.status,0,result.stderr);
  result=cli('apply','--plan',planFile,'--status-job',w.metadata.name,'--evidence',join(dir,'apply.jsonl'),'--confirm','APPLY-STAGING-AUTHORITY-SIDECARS');assert.equal(result.status,0,result.stderr);
  let state=JSON.parse(readFileSync(stateFile));assert.equal(state.patches,1);assert.deepEqual(readFileSync(join(dir,'apply.jsonl'),'utf8').trim().split('\n').map(l=>JSON.parse(l).status),['INTENT','UNKNOWN','APPLIED']);
  state.policy=2;writeFileSync(stateFile,JSON.stringify(state));
  result=cli('rollback','--plan',planFile,'--status-job',w.metadata.name,'--evidence',join(dir,'rollback-denied.jsonl'),'--confirm','ROLLBACK-STAGING-AUTHORITY-SIDECARS');assert.equal(result.status,1);assert.match(result.stderr,/POST_ACTIVATION_REQUIRES_FORWARD_COMPATIBLE_PLAN/);assert.equal(JSON.parse(readFileSync(stateFile)).patches,1);
  state.policy=1;writeFileSync(stateFile,JSON.stringify(state));
  result=cli('apply','--plan',planFile,'--status-job',w.metadata.name,'--evidence',join(dir,'stale-plan.jsonl'),'--confirm','APPLY-STAGING-AUTHORITY-SIDECARS');assert.equal(result.status,1);assert.match(result.stderr,/SIDECAR_PLAN_DRIFT/);assert.equal(JSON.parse(readFileSync(stateFile)).patches,1);
  result=cli('rollback','--plan',planFile,'--status-job',w.metadata.name,'--evidence',join(dir,'rollback.jsonl'),'--confirm','ROLLBACK-STAGING-AUTHORITY-SIDECARS');assert.equal(result.status,0,result.stderr);assert.deepEqual(JSON.parse(readFileSync(stateFile)).deployment.spec,d.spec);
 }finally{rmSync(dir,{recursive:true,force:true});}
});
