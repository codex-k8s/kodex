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
  for(const file of ['tools/release/application-source.mjs','tools/dev/ensure-local-buildx-builder.sh','tools/dev/import-local-image.sh']){mkdirSync(dirname(join(source,file)),{recursive:true});copyFileSync(join(repo,file),join(source,file));chmodSync(join(source,file),0o755);}
  const git=(...args)=>execFileSync('git',['-C',source,...args],{stdio:'pipe'}).toString().trim();git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');git('add','.');
  const commit=()=>git('-c','user.name=kodex-agent','-c','user.email=238524843+kodex-agent@users.noreply.github.com','commit','--allow-empty','-qm','Проверка recipe');commit();
  const stub=`#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto'),cp=require('node:child_process');
let name=path.basename(process.argv[1]),a=process.argv.slice(2);fs.appendFileSync(process.env.CALLS,JSON.stringify({name,args:a})+'\\n');
const manifest=JSON.stringify({schemaVersion:2,mediaType:'application/vnd.oci.image.manifest.v1+json',layers:[]}),digest='sha256:'+crypto.createHash('sha256').update(manifest).digest('hex');
const context='k3d-synthetic',nodes=['k3d-synthetic-agent-0','k3d-synthetic-server-0'];
if(name==='kubectl'){
 if(a.join(' ')==='config current-context'){process.stdout.write(context);process.exit(0);}
 if(a.join(' ')!=='--context '+context+' get namespace kodex-system -o json')process.exit(93);
 process.stdout.write(JSON.stringify({metadata:{labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging'}}}));process.exit(0);
}
if(name==='k3d'){
 if(a.join(' ')==='node list -o json'){process.stdout.write(JSON.stringify(nodes.map(name=>({name,role:name.includes('-agent-')?'agent':'server'}))));process.exit(0);}
 if(a.length!==8||a[0]!=='image'||a[1]!=='import'||a.slice(3).join(' ')!=='--cluster synthetic --mode direct --keep-tarball'||!fs.statSync(a[2]).isFile())process.exit(94);
 process.exit(0);
}
if(name==='docker'){
 if(a.join(' ')==="inspect --format {{.HostConfig.NetworkMode}} buildx_buildkit_kodex-local-dev0"){process.stdout.write('host');process.exit(0);}
 if(a[0]==='exec'){
  if(!nodes.includes(a[1])||a.slice(2,5).join(' ')!=='ctr -n k8s.io')process.exit(95);
  const node=a[1];a=a.slice(5);const refs=process.env.REFS+'-'+node;
  if(a.length===5&&a.slice(0,3).join(' ')==='images tag --force'&&a[4]===a[3].split(':local-')[0]+'@'+digest){fs.appendFileSync(refs,a[4]+'\\n');process.exit(0);}
  if(a.join(' ')==='images list --quiet'){process.stdout.write(fs.readFileSync(refs));process.exit(0);}
  if(a.join(' ')==='content get '+digest){process.stdout.write(process.env.CORRUPT_IMPORT?'{}':manifest);process.exit(0);}
  process.exit(96);
 }
 if(a[0]!=='buildx')process.exit(90);if(a[1]==='version')process.exit(0);if(a[1]==='inspect'){process.stdout.write('Status: running');process.exit(0);}
 if(a[1]!=='build')process.exit(91);const dest=a[a.indexOf('--output')+1].split('dest=')[1],dir=fs.mkdtempSync(path.join(process.env.TMPDIR,'oci-'));
 fs.mkdirSync(path.join(dir,'blobs','sha256'),{recursive:true});fs.writeFileSync(path.join(dir,'blobs','sha256',digest.slice(7)),manifest);fs.writeFileSync(path.join(dir,'index.json'),JSON.stringify({manifests:[{digest}]}));cp.execFileSync('tar',['-cf',dest,'-C',dir,'index.json','blobs']);process.exit(0);
}
process.exit(97);
`;
  for(const name of ['docker','kubectl','k3d','sudo','k3s'])writeFileSync(join(bin,name),stub,{mode:0o755});
  const run=(context='k3d-synthetic',extra={})=>spawnSync('bash',[join(repo,'tools/dev/build-local-image-supply-chain.sh'),'--source-root',source,'--state-directory',state,'--component','authority-security','--context',context],{encoding:'utf8',timeout:20000,env:{...process.env,PATH:bin+':'+process.env.PATH,CALLS:calls,REFS:join(root,'refs'),TMPDIR:root,...extra}});
  let r=run();assert.equal(r.status,0,r.stderr);let metadata=JSON.parse(readFileSync(join(state,'authority-security-images.json')));assert.equal(metadata.revision,git('rev-parse','HEAD'));assert.equal(metadata.digestReadback,true);
  let records=readFileSync(calls,'utf8').trim().split('\n').map(JSON.parse),builds=records.filter(c=>c.name==='docker'&&c.args[1]==='build');assert.equal(builds.length,2);
  assert.deepEqual(builds.map(c=>c.args[c.args.indexOf('--target')+1]),['runtime','image-admission']);assert.ok(builds[0].args.includes('VERSION='+metadata.revision));assert.ok(builds[1].args.includes('SOURCE_SHA='+metadata.revision));
  assert.equal(records.filter(c=>c.name==='sudo'||c.name==='k3s').length,0);
  assert.deepEqual(records.filter(c=>c.name==='kubectl'&&c.args[0]==='--context').map(c=>c.args),[['--context','k3d-synthetic','get','namespace','kodex-system','-o','json']]);
  assert.equal(metadata.profile,'multi-node-k3d-image-store');
  for(const node of ['k3d-synthetic-agent-0','k3d-synthetic-server-0'])assert.equal(records.filter(c=>c.name==='docker'&&c.args[0]==='exec'&&c.args[1]===node&&c.args[5]==='content').length,2,'each exact image must be verified on each node');
  commit();r=run();assert.equal(r.status,0,r.stderr);records=readFileSync(calls,'utf8').trim().split('\n').map(JSON.parse);assert.equal(records.filter(c=>c.name==='docker'&&c.args[1]==='build').length,4,'same tree with new VERSION must not reuse old binary cache');
  r=run('production');assert.notEqual(r.status,0);assert.match(r.stderr,/exact staging context/);
  const beforeWrong=readFileSync(calls,'utf8').trim().split('\n').length;
  r=run('k3d-other');assert.notEqual(r.status,0);assert.match(r.stderr,/exact staging context/);
  assert.deepEqual(readFileSync(calls,'utf8').trim().split('\n').slice(beforeWrong).map(JSON.parse),[{name:'kubectl',args:['config','current-context']}],'wrong context must stop before namespace read, build or import');
  r=run('k3d-synthetic',{CORRUPT_IMPORT:'1'});assert.notEqual(r.status,0);assert.match(r.stderr,/manifest digest mismatch/);
 }finally{rmSync(root,{recursive:true,force:true});}
});
