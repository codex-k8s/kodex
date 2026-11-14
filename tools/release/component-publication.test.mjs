import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync,existsSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './stable-inventory.mjs';
import {componentMigrationConfiguration,componentMigrationPublication,componentImageLockMaxBytes,componentImageLockMaxBase64,componentPublicationMaxBytes} from './portable-component-migration.mjs';
import {validateStableReceipt} from './stable-image-publication.mjs';
import {createComponentImageLock,validateComponentPublicationConfiguration} from './component-publication.mjs';
const sha=x=>createHash('sha256').update(x).digest('hex'),revision='a'.repeat(40),uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const cli=fileURLToPath(new URL('./component-publication.mjs',import.meta.url));
function fixture(profile='github-repository-scope'){
  const cfg=componentMigrationConfiguration(profile),baseHeads={'control-plane':20261112000100,'internal-rpc-authority':2,'email-bridge':3};
  const config={schema:'kodex.component-publication-config/v1',profile,sourceSHA:revision,sourceCheckout:'/private/source',context:'k3d-fixture',
    clusterUID:uid(1),namespaceUID:uid(2),baseSourceSHA:'b'.repeat(40),baseReleaseLockSHA256:'c'.repeat(64),baseReleaseLock:'/private/base.json',
    baseSchemaHeads:baseHeads,schemaHeadsBefore:{...baseHeads,'control-plane':cfg.before},schemaHeadsAfter:{...baseHeads,'control-plane':cfg.after},
    nodes:[{name:'k3d-fixture-agent-0',uid:uid(3)}],components:cfg.components.map(name=>({name,
      expectedImageRef:`registry.local.kodex/kodex/${name}@sha256:${'f'.repeat(64)}`,anchor:cfg.sources[name]+'internal/synthetic.go',
      buildReceipt:`/private/${name}/build.json`,publicationPlan:`/private/${name}/plan.json`,publicationEvidence:`/private/${name}/journal.jsonl`}))};
  const evidence=component=>{
    const receipt={schema:'kodex.stable-local-build/v1',component:component.name,sourceSHA:revision,contextSHA256:sha('synthetic archive'),
      archiveSHA256:'d'.repeat(64),imageDigest:'sha256:'+'e'.repeat(64),dockerfile:cfg.sources[component.name]+'Dockerfile',target:'',
      admission:'NOT_RUN',publication:'NOT_RUN',nodePull:'NOT_RUN'};
    const plan={schema:'kodex.stable-image-publication/v2',artifactProfile:'built',context:config.context,clusterUID:config.clusterUID,
      namespaceUID:config.namespaceUID,source:revision,receipt,image:validateStableReceipt(receipt),archiveSHA256:receipt.archiveSHA256};
    return {publicationPlan:plan,publicationEvidence:[{schema:'kodex.stable-image-publication-attempt/v1',status:'INTENT',mode:'apply',
      planSHA256:fingerprint(plan),component:component.name,digest:plan.image.digest},{status:'PASS',phase:'REGISTRY_PUBLICATION_ONLY',nodePull:'NOT_RUN'}]};
  };
  return {config,evidence};
}
test('component locks bind actual publisher journal source target and full code manifest',()=>{
  for(const profile of ['workspace-unbound-stop','github-repository-scope','github-repository-scope-backup']){
    const f=fixture(profile),component=f.config.components[0],e=f.evidence(component),source={receipt:e.publicationPlan.receipt,
      receiptSHA256:'1'.repeat(64),dockerfileSHA256:'2'.repeat(64),productionSHA256:'3'.repeat(64),contextSHA256:sha('synthetic archive'),
      changedFiles:[component.anchor]};
    const lock=createComponentImageLock(f.config,component,e,source);
    assert.equal(lock.qualification,'PUBLISHED_NOT_NODE_QUALIFIED_NOT_DEPLOYED');assert.equal(lock.source_sha,revision);
    for(const alter of [x=>x.publicationEvidence.pop(),x=>x.publicationEvidence[1].nodePull='PASS',x=>x.publicationEvidence[0].planSHA256='0'.repeat(64),
      x=>x.publicationPlan.image.digest='sha256:'+'1'.repeat(64),x=>x.publicationPlan.namespaceUID=uid(99),
      x=>x.publicationPlan.receipt.sourceSHA='9'.repeat(40)]){
      const bad=structuredClone(e);alter(bad);assert.throws(()=>createComponentImageLock(f.config,component,bad,source));
    }
    for(const patch of [{changedFiles:['../../foreign']},{changedFiles:[]},{receiptSHA256:'bad'},{contextSHA256:'0'.repeat(64)}])
      assert.throws(()=>createComponentImageLock(f.config,component,e,{...source,...patch}));
  }
});
test('configuration rejects broader component target node and schema sets',()=>{
  const f=fixture();assert.equal(validateComponentPublicationConfiguration(f.config).components.length,5);
  for(const change of [c=>c.components.reverse(),c=>c.components.push(c.components[0]),c=>c.nodes.push(c.nodes[0]),
    c=>c.nodes[0].name='--foreign',c=>c.schemaHeadsAfter['control-plane']++,c=>c.baseSchemaHeads['email-bridge']++,
    c=>c.components[0].anchor='services/internal/control-plane/../foreign',c=>c.components[0].expectedImageRef='foreign/other:latest',
    c=>c.sourceCheckout='relative',c=>c.unexpected=true]){
    const bad=structuredClone(f.config);change(bad);assert.throws(()=>validateComponentPublicationConfiguration(bad));
  }
});
const mock=`#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),base=path.join(__dirname,'..'),a=process.argv.slice(2),program=path.basename(process.argv[1]);
const p=path.join(base,'state.json'),s=JSON.parse(fs.readFileSync(p)),c=JSON.parse(fs.readFileSync(path.join(base,'config.json')));
fs.appendFileSync(path.join(base,'trace'),JSON.stringify({program,args:a,cwd:process.cwd()})+'\\n');
const save=()=>fs.writeFileSync(p,JSON.stringify(s));
if(program==='git'){
 if(a[0]==='rev-parse')console.log(s.sourceDrift?'f'.repeat(40):c.sourceSHA);
 else if(a[0]==='status'){if(s.dirty)console.log(' M synthetic');}
 else if(a[0]==='archive')process.stdout.write('synthetic archive');
 else if(a[0]==='show')process.stdout.write('FROM scratch\\n');
 else if(a[0]==='diff'){
  if(a.includes('--name-only'))process.stdout.write((s.manifests?.[a.at(-1)]??[a.at(-1)+'internal/synthetic.go']).join('\\0')+'\\0');
  else process.stdout.write('synthetic binary diff');
 }else if(!['merge-base','cat-file'].includes(a[0]))process.exit(2);
}else if(program==='kubectl'){
 if(a[a.indexOf('--context')+1]!==c.context||a[a.indexOf('--kubeconfig')+1]!==path.join(base,'explicit kubeconfig'))process.exit(3);
 const i=a.indexOf('get');
 if(a[i+1]==='namespace')console.log(JSON.stringify({metadata:{uid:a[i+2]==='kube-system'?c.clusterUID:c.namespaceUID}}));
 else if(a[i+1]==='nodes')console.log(JSON.stringify({items:c.nodes.map(n=>({metadata:{name:n.name,uid:s.nodeDrift?'foreign':n.uid},
  status:{conditions:[{type:'Ready',status:'True'},{type:'DiskPressure',status:s.diskPressure?'True':'False'}]}}))}));else process.exit(4);
}else if(program==='docker'){
 if(a[0]==='inspect')console.log(JSON.stringify([{Name:'/k3d-fixture-agent-0',Image:'sha256:'+'1'.repeat(64),Id:'2'.repeat(64),
  State:{Running:true},Config:{Cmd:[],Entrypoint:[],Env:[],Labels:{'k3d.cluster':s.foreignNode?'other':'fixture','k3d.role':'agent'}}}]));
 else if(a[0]==='exec'){
  if(a[1]!=='2'.repeat(64)||a[2]!=='crictl'||a[4]!=='unix:///run/k3s/containerd/containerd.sock'||a[6]!==a[4])process.exit(5);
  const image=a.at(-1);
  if(a[7]==='pull'){s.pulls++;s.images.push(image);save();if(s.lost&&s.pulls===c.components.length)process.exit(9);}
  else if(a[7]==='inspecti'){
   if(!s.images.includes(image))process.exit(6);
   console.log(JSON.stringify({status:{id:'sha256:'+'3'.repeat(64),repoDigests:[s.wrongDigest?'foreign@sha256:'+'4'.repeat(64):image]}}));
  }else process.exit(7);
 }else process.exit(8);
}else process.exit(10);
`;
function setup(profile='workspace-unbound-stop'){
  const directory=mkdtempSync(join(tmpdir(),'kodex-component-publication-')),f=fixture(profile);
  mkdirSync(join(directory,'bin'));mkdirSync(join(directory,'source'));
  for(const name of ['git','docker','kubectl'])writeFileSync(join(directory,'bin',name),mock,{mode:0o700});
  const store=(path,value,lines=false)=>writeFileSync(path,lines?value.map(v=>JSON.stringify(v)+'\n').join(''):JSON.stringify(value,null,2)+'\n',{mode:0o600});
  f.config.sourceCheckout=join(directory,'source');f.config.baseReleaseLock=join(directory,'base.json');
  store(f.config.baseReleaseLock,{source_sha:f.config.baseSourceSHA});f.config.baseReleaseLockSHA256=sha(readFileSync(f.config.baseReleaseLock));
  for(const component of f.config.components){
    for(const [field,suffix] of [['buildReceipt','build.json'],['publicationPlan','plan.json'],['publicationEvidence','journal.jsonl']])
      component[field]=join(directory,component.name+'-'+suffix);
    const e=f.evidence(component);store(component.buildReceipt,e.publicationPlan.receipt);store(component.publicationPlan,e.publicationPlan);
    store(component.publicationEvidence,e.publicationEvidence,true);
  }
  store(join(directory,'config.json'),f.config);store(join(directory,'state.json'),{pulls:0,images:[]});
  const state=()=>JSON.parse(readFileSync(join(directory,'state.json'))),change=patch=>store(join(directory,'state.json'),{...state(),...patch});
  const run=(mode,out,extra=[])=>{
    const args=mode==='prepare'?['--configuration',join(directory,'config.json')]:['--prepared',join(directory,'prepared.json'),
      '--context',f.config.context,'--kubeconfig',join(directory,'explicit kubeconfig')];
    try{return {code:0,out:execFileSync(process.execPath,[cli,mode,...args,'--output',join(directory,out),...extra],
      {env:{PATH:join(directory,'bin')+':/usr/bin:/bin'},encoding:'utf8',timeout:30000,stdio:['ignore','pipe','pipe']})};}
    catch(e){return {code:e.status,out:String(e.stdout??'')+String(e.stderr??'')};}
  };
  return {directory,config:f.config,run,state,change,store,prepared:()=>JSON.parse(readFileSync(join(directory,'prepared.json')))};
}
for(const profile of ['workspace-unbound-stop','github-repository-scope','github-repository-scope-backup'])test(`real CLI prepares locks then proves actual CRI and readback does not pull: ${profile}`,()=>{
  const f=setup(profile);
  try{
    const p=f.run('prepare','prepared.json');assert.equal(p.code,0,p.out);
    let trace=readFileSync(join(f.directory,'trace'),'utf8').trim().split('\n').map(JSON.parse);
    assert.ok(trace.every(x=>x.program==='git'&&x.cwd===f.config.sourceCheckout));
    assert.equal(f.prepared().publication.components[0].nodeProof,undefined);
    const approved=['--approved-plan-sha',fingerprint(f.prepared())];
    const rejected=f.run('qualify','unapproved.json');assert.notEqual(rejected.code,0);assert.equal(f.state().pulls,0);
    const q=f.run('qualify','publication.json',approved);assert.equal(q.code,0,q.out);
    const publication=JSON.parse(readFileSync(join(f.directory,'publication.json')));
    assert.equal(componentMigrationPublication(publication,{revision},profile).components.length,f.config.components.length);
    const pulls=f.state().pulls,readback=f.run('readback','readback.json');assert.equal(readback.code,0,readback.out);
    assert.equal(f.state().pulls,pulls);assert.deepEqual(JSON.parse(readFileSync(join(f.directory,'readback.json'))),publication);
    trace=readFileSync(join(f.directory,'trace'),'utf8').trim().split('\n').map(JSON.parse);
    assert.ok(trace.filter(x=>x.program==='docker'&&x.args[0]==='exec').every(x=>x.args[1]==='2'.repeat(64)));
  }finally{rmSync(f.directory,{recursive:true,force:true});}
});
test('large cohort keeps the full manifest within 64KiB using canonical compact encoding',()=>{
  const f=setup('github-repository-scope');
  try{
    const sample=fixture('github-repository-scope'),component=sample.config.components[0],e=sample.evidence(component);
    let manifest;
    for(let count=100;count<250;count++){
      const changedFiles=[component.anchor,...Array.from({length:count},(_,i)=>
        'services/internal/control-plane/internal/production/'+String(i).padStart(4,'0')+'_'+'x'.repeat(20)+'.go')].sort();
      const lock=createComponentImageLock(sample.config,component,e,{receipt:e.publicationPlan.receipt,receiptSHA256:'1'.repeat(64),
        dockerfileSHA256:'2'.repeat(64),productionSHA256:'3'.repeat(64),contextSHA256:sha('synthetic archive'),changedFiles});
      if(Buffer.byteLength(JSON.stringify(lock,null,2)+'\n')>65536 && Buffer.byteLength(JSON.stringify(lock)+'\n')<65536){manifest=changedFiles;break;}
    }
    assert.ok(manifest,'fixture crosses the pretty encoding budget');
    f.change({manifests:{'services/internal/control-plane/':manifest}});
    const p=f.run('prepare','prepared.json');assert.equal(p.code,0,p.out);
    const part=f.prepared().publication.components[0],raw=Buffer.from(part.componentLockRawBase64,'base64'),lock=JSON.parse(raw);
    assert.ok(raw.length<=65536);assert.ok(Buffer.byteLength(JSON.stringify(lock,null,2)+'\n')>65536);
    assert.deepEqual(lock.compatibility.changed_files,manifest);assert.deepEqual(lock.compatibility.production_files,manifest);
    assert.deepEqual(lock.compatibility.verified_cohort.changed_files,manifest);assert.deepEqual(lock.compatibility.verified_cohort.production_files,manifest);
    const q=f.run('qualify','publication.json',['--approved-plan-sha',fingerprint(f.prepared())]);assert.equal(q.code,0,q.out);
    const publication=JSON.parse(readFileSync(join(f.directory,'publication.json')));
    assert.equal(componentMigrationPublication(publication,{revision},'github-repository-scope').components.length,5);
    const duplicate=Buffer.from(raw.toString().replace('{','{"component":"control-plane",'));
    publication.components[0].componentLockRawBase64=duplicate.toString('base64');publication.components[0].componentLockSHA256=sha(duplicate);
    assert.throws(()=>componentMigrationPublication(publication,{revision},'github-repository-scope'),/LOCK_ENCODING_INVALID/);
  }finally{rmSync(f.directory,{recursive:true,force:true});}
});
test('full manifest above64KiB survives publisher, exact CRI proof and consumer with bounded128KiB lock',()=>{
  const f=setup('github-repository-scope');
  try{
    const component=f.config.components[0];
    const manifest=[component.anchor,...Array.from({length:300},(_,i)=>
      'services/internal/control-plane/internal/production/'+String(i).padStart(4,'0')+'_'+'x'.repeat(20)+'.go')].sort();
    f.change({manifests:{'services/internal/control-plane/':manifest}});
    const prepared=f.run('prepare','prepared.json');assert.equal(prepared.code,0,prepared.out);
    const raw=Buffer.from(f.prepared().publication.components[0].componentLockRawBase64,'base64');
    assert.ok(raw.length>65536);assert.ok(raw.length<=componentImageLockMaxBytes);
    const qualified=f.run('qualify','publication.json',['--approved-plan-sha',fingerprint(f.prepared())]);assert.equal(qualified.code,0,qualified.out);
    const publication=JSON.parse(readFileSync(join(f.directory,'publication.json')));
    const accepted=componentMigrationPublication(publication,{revision},'github-repository-scope');
    assert.deepEqual(accepted.components[0].lock.compatibility.changed_files,manifest);
    assert.deepEqual(accepted.components[0].lock.compatibility.verified_cohort.production_files,manifest);
    const pulls=f.state().pulls;assert.equal(f.run('readback','readback.json').code,0);assert.equal(f.state().pulls,pulls);
    for(const length of [componentImageLockMaxBytes+1,componentImageLockMaxBytes+3]){
      const altered=structuredClone(publication),part=altered.components[0],oversized=Buffer.alloc(length,32);
      part.componentLockRawBase64=oversized.toString('base64');part.componentLockSHA256=sha(oversized);
      assert.throws(()=>componentMigrationPublication(altered,{revision},'github-repository-scope'),/LOCK_(?:HASH_)?INVALID/);
    }
    assert.equal(componentImageLockMaxBase64,4*Math.ceil(componentImageLockMaxBytes/3));
  }finally{rmSync(f.directory,{recursive:true,force:true});}
});
test('prepared aggregate budget is finite and rejected before any node operation',()=>{
  const f=setup('github-repository-scope');
  try{
    assert.equal(f.run('prepare','prepared.json').code,0);
    const prepared=f.prepared();prepared.untrusted='x'.repeat(componentPublicationMaxBytes);
    f.store(join(f.directory,'prepared.json'),prepared);
    const rejected=f.run('qualify','rejected.json',['--approved-plan-sha',fingerprint(prepared)]);
    assert.notEqual(rejected.code,0);assert.match(rejected.out,/PRIVATE_FILE_REQUIRED/);assert.equal(f.state().pulls,0);
    assert.throws(()=>componentMigrationPublication({untrusted:prepared.untrusted},{revision},'github-repository-scope'),/PUBLICATION_TOO_LARGE/);
  }finally{rmSync(f.directory,{recursive:true,force:true});}
});
test('publisher rejects oversize full manifest before publication without silently truncating files',()=>{
  const f=setup('github-repository-scope');
  try{
    const manifest=[f.config.components[0].anchor,...Array.from({length:500},(_,i)=>
      'services/internal/control-plane/internal/production/'+String(i).padStart(4,'0')+'_'+'x'.repeat(20)+'.go')].sort();
    f.change({manifests:{'services/internal/control-plane/':manifest}});
    const result=f.run('prepare','prepared.json');assert.notEqual(result.code,0);assert.match(result.out,/LOCK_TOO_LARGE/);
    assert.equal(existsSync(join(f.directory,'prepared.json')),false);assert.equal(f.state().pulls,0);
  }finally{rmSync(f.directory,{recursive:true,force:true});}
});
test('lost final pull response is UNKNOWN, readback proves resident digests without replay',()=>{
  const f=setup();try{
    assert.equal(f.run('prepare','prepared.json').code,0);f.change({lost:true});
    const r=f.run('qualify','failed.json',['--approved-plan-sha',fingerprint(f.prepared())]);
    assert.notEqual(r.code,0);assert.match(r.out,/FAILED_OR_UNKNOWN/);assert.equal(existsSync(join(f.directory,'failed.json')),false);
    const before=f.state().pulls,readback=f.run('readback','recovered.json');assert.equal(readback.code,0,readback.out);assert.equal(f.state().pulls,before);
  }finally{rmSync(f.directory,{recursive:true,force:true});}
});
test('target/source drift wrong digest and duplicate JSON cannot yield qualified publication',()=>{
  for(const patch of [{sourceDrift:true},{dirty:true},{nodeDrift:true},{foreignNode:true},{diskPressure:true},{wrongDigest:true}]){
    const f=setup();try{
      assert.equal(f.run('prepare','prepared.json').code,0);f.change(patch);
      const q=f.run('qualify','rejected.json',['--approved-plan-sha',fingerprint(f.prepared())]);
      assert.notEqual(q.code,0,q.out);assert.equal(existsSync(join(f.directory,'rejected.json')),false);
      if(!patch.wrongDigest)assert.equal(f.state().pulls,0);
    }finally{rmSync(f.directory,{recursive:true,force:true});}
  }
  const f=setup();try{
    const path=join(f.directory,'config.json'),raw=readFileSync(path,'utf8');
    writeFileSync(path,raw.replace('{','{"sourceSHA":"f",'));
    const result=f.run('prepare','rejected.json');assert.notEqual(result.code,0);assert.match(result.out,/NONCANONICAL/);
    assert.equal(existsSync(join(f.directory,'trace')),false);
  }finally{rmSync(f.directory,{recursive:true,force:true});}
});
