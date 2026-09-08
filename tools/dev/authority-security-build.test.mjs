import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,copyFileSync,rmSync,chmodSync} from 'node:fs';
import {execFileSync,spawnSync} from 'node:child_process';
import {join,dirname} from 'node:path';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
const repo=fileURLToPath(new URL('../..',import.meta.url));
test('security build public CLI delivers only two exact images; checks digest and revision cache boundary',()=>{
 const root=mkdtempSync(join(tmpdir(),'authority-build-'));
 try {
  const source=join(root,'source'),bin=join(root,'bin'),state=join(root,'state'),calls=join(root,'calls.jsonl');mkdirSync(source,{mode:0o755});mkdirSync(bin);mkdirSync(state);
  for(const file of ['tools/dev/Dockerfile.local-image-supply-chain','services/jobs/role-image-builder/Dockerfile','services/internal/internal-rpc-authority/Dockerfile','infra/dockerfile-frontend/Dockerfile','infra/admission-tools/Dockerfile','tools/render-image-admission-job.sh','libs/go/fixture']) {mkdirSync(dirname(join(source,file)),{recursive:true,mode:0o755});writeFileSync(join(source,file),'synthetic\n',{mode:0o644});}
  for(const file of ['tools/release/application-source.mjs','tools/dev/ensure-local-buildx-builder.sh']){mkdirSync(dirname(join(source,file)),{recursive:true});copyFileSync(join(repo,file),join(source,file));chmodSync(join(source,file),0o755);}
  const git=(...args)=>execFileSync('git',['-C',source,...args],{stdio:'pipe'}).toString().trim();git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');git('add','.');
  const commit=()=>git('-c','user.name=kodex-agent','-c','user.email=238524843+kodex-agent@users.noreply.github.com','commit','--allow-empty','-qm','Проверка recipe');commit();
  const stub=`#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto'),cp=require('node:child_process');
let name=path.basename(process.argv[1]),a=process.argv.slice(2);fs.appendFileSync(process.env.CALLS,JSON.stringify({name,args:a})+'\\n');
const manifest=JSON.stringify({schemaVersion:2,mediaType:'application/vnd.oci.image.manifest.v1+json',layers:[]}),digest='sha256:'+crypto.createHash('sha256').update(manifest).digest('hex');
if(name==='docker'){
 if(a[0]!=='buildx')process.exit(90);if(a[1]==='version')process.exit(0);if(a[1]==='inspect'){process.stdout.write('Status: running');process.exit(0);}
 if(a[1]!=='build')process.exit(91);const dest=a[a.indexOf('--output')+1].split('dest=')[1],dir=fs.mkdtempSync(path.join(process.env.TMPDIR,'oci-'));
 fs.mkdirSync(path.join(dir,'blobs','sha256'),{recursive:true});fs.writeFileSync(path.join(dir,'blobs','sha256',digest.slice(7)),manifest);fs.writeFileSync(path.join(dir,'index.json'),JSON.stringify({manifests:[{digest}]}));cp.execFileSync('tar',['-cf',dest,'-C',dir,'index.json','blobs']);process.exit(0);
}
if(name==='sudo'){
 if(a.join(' ')==='-n true')process.exit(0);if(a[0]!=='-n'||a[1]!=='k3s')process.exit(92);a=a.slice(2);
 if(a[0]==='kubectl'){if(a[1]!=='--context'||a[2]!=='synthetic')process.exit(93);if(a[3]==='config')process.stdout.write('synthetic');else process.stdout.write(JSON.stringify({metadata:{labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging'}}}));process.exit(0);}
 if(a[0]!=='ctr')process.exit(94);if(a[1]==='version')process.exit(0);a=a.slice(3);
 const refs=process.env.REFS;if(a[0]==='images'&&a[1]==='import')process.exit(0);
 if(a[0]==='images'&&a[1]==='tag'){fs.appendFileSync(refs,a.at(-1)+'\\n');process.exit(0);}
 if(a[0]==='images'&&a[1]==='list'){process.stdout.write(fs.readFileSync(refs));process.exit(0);}
 if(a[0]==='content'&&a[1]==='get'){if(a[2]!==digest)process.exit(95);process.stdout.write(process.env.CORRUPT_IMPORT?'{}':manifest);process.exit(0);}
 process.exit(96);
}
process.exit(97);
`;
  for(const name of ['docker','sudo','k3s'])writeFileSync(join(bin,name),stub,{mode:0o755});
  const run=(context='synthetic',extra={})=>spawnSync('bash',[join(repo,'tools/dev/build-local-image-supply-chain.sh'),'--source-root',source,'--state-directory',state,'--component','authority-security','--context',context],{encoding:'utf8',timeout:20000,env:{...process.env,PATH:bin+':'+process.env.PATH,CALLS:calls,REFS:join(root,'refs'),TMPDIR:root,...extra}});
  let r=run();assert.equal(r.status,0,r.stderr);let metadata=JSON.parse(readFileSync(join(state,'authority-security-images.json')));assert.equal(metadata.revision,git('rev-parse','HEAD'));assert.equal(metadata.digestReadback,true);
  let records=readFileSync(calls,'utf8').trim().split('\n').map(JSON.parse),builds=records.filter(c=>c.name==='docker'&&c.args[1]==='build');assert.equal(builds.length,2);
  assert.deepEqual(builds.map(c=>c.args[c.args.indexOf('--target')+1]),['runtime','image-admission']);assert.ok(builds[0].args.includes('VERSION='+metadata.revision));assert.ok(builds[1].args.includes('SOURCE_SHA='+metadata.revision));
  assert.ok(records.filter(c=>c.name==='sudo').every(c=>!c.args.includes('docker')));
  commit();r=run();assert.equal(r.status,0,r.stderr);records=readFileSync(calls,'utf8').trim().split('\n').map(JSON.parse);assert.equal(records.filter(c=>c.name==='docker'&&c.args[1]==='build').length,4,'same tree with new VERSION must not reuse old binary cache');
  r=run('production');assert.notEqual(r.status,0);assert.match(r.stderr,/exact staging context/);
  r=run('synthetic',{CORRUPT_IMPORT:'1'});assert.notEqual(r.status,0);assert.match(r.stderr,/manifest digest mismatch/);
 }finally{rmSync(root,{recursive:true,force:true});}
});
