import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,readFileSync,rmSync,chmodSync,existsSync,statSync,symlinkSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {execFileSync} from 'node:child_process';
import {fingerprint} from './scoped-release.mjs';
import {readPairedTokenReleaseOwner} from './paired-token-release-plan.mjs';
import {capturePairedTokenReleasePlan,pairedTokenReleaseCLI,boundPairedTokenReleaseArtifact,pairedTokenReleaseOutputMaxBytes} from './paired-token-release-cli.mjs';
import {pairedTokenReleaseFixture as fixture,pairedTokenReleaseFixtureSource as source,
 pairedTokenReleaseFixtureDigest as digest} from './fixtures/paired-token-release-synthetic.mjs';

// Только factory adapters заменены synthetic портами; parser, custody, original
// journals, manifest/proofs и plan/status выполняются настоящими функциями.
function setup(t){
 const f=fixture(),directory=mkdtempSync(join(tmpdir(),'kodex-paired-token-'));chmodSync(directory,0o700);
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const json=(name,value)=>{const p=join(directory,name);writeFileSync(p,JSON.stringify(value)+'\n',{mode:0o600});return p;};
 const journal=(name,events)=>{const p=join(directory,name);writeFileSync(p,events.map(e=>JSON.stringify(e)+'\n').join(''),{mode:0o600});return p;};
 const config={schema:'kodex.paired-token-release-input/v1',manifest:f.manifest,source:{path:'/synthetic/source',revision:source},
  authority:{planPath:json('authority-plan.json',{}),evidencePath:journal('authority.jsonl',[{status:'SYNTHETIC_PORT_FIXTURE_ONLY'}])},
  admission:{planPath:json('closed.json',f.api.closed.plan),evidencePath:journal('closed.jsonl',f.api.closed.events),
   trafficPlanPath:json('traffic.json',f.api.traffic.plan),trafficEvidencePath:journal('traffic.jsonl',f.api.traffic.events)},
  nodes:f.nodeReadback.binding};
 const input=json('input.json',config),kubeconfig=join(directory,'synthetic-kubeconfig'),plan=join(directory,'plan.json');
 writeFileSync(kubeconfig,'synthetic fixture only\n',{mode:0o600});
 const trace=[],io={
  inspectSource:path=>{trace.push({operation:'source',path});return {revision:path===config.source.path?source:'e'.repeat(40)};},
  requireAncestor:(_source,before,after)=>{trace.push({operation:'ancestor',before,after});assert.equal(after,source);},
  sourceArchiveHash:async(revision,path)=>{assert.equal(revision,source);assert.equal(path,config.source.path);return digest;},
  readServing:(_kube,name)=>{trace.push({operation:'serving',name});return structuredClone(name==='control-api-gateway'?f.api.resources:f.observed.serving[name]);},
  readOwner:manifest=>{trace.push({operation:'owner'});return readPairedTokenReleaseOwner(manifest,f.ports);},
  readNodes:async options=>{trace.push({operation:'node',mode:options.mode});assert.equal(options.mode,'readback');
   assert.equal(options.kubeconfig,kubeconfig);assert.deepEqual(options.images,f.nodeReadback.images.map(i=>i.imageRef));
   return structuredClone(f.nodeReadback);},
  kube:args=>{
   trace.push({operation:'kube',args});assert.ok(args.includes('get'));assert.ok(!args.some(v=>['create','patch','replace','delete','apply','exec'].includes(v)));
   const i=args.indexOf('get'),kind=args[i+1],name=args[i+2];
   if(kind==='namespace')return JSON.stringify({metadata:{uid:name==='kube-system'?config.manifest.target.clusterUID:config.manifest.target.namespaceUID}});
   if(kind==='nodes')return JSON.stringify({items:f.observed.nodes});
   if(kind==='configmap')return JSON.stringify(f.observed.installation);
   if(kind==='deployment')return JSON.stringify(f.observed.deployments.find(d=>d.metadata.name===name));
   if(kind==='service')return JSON.stringify(f.api.service);
   if(kind==='endpointslices')return JSON.stringify(f.api.slices);
   if(kind==='pods')return JSON.stringify(f.api.pods);
   throw Error('UNEXPECTED_SYNTHETIC_READ');
  },
 };
 const run=(command='plan',output=plan)=>pairedTokenReleaseCLI([command,'--input',input,'--kubeconfig',kubeconfig,'--output',output,
  ...(command==='status'?['--plan',plan]:[])],io);
 return {f,directory,config,input,kubeconfig,plan,trace,io,run,json};
}

test('runnable plan/status сохраняет bounded private evidence и не выдаёт apply; current backend читается после Gateway',async t=>{
 const s=setup(t),summary=await s.run(),record=JSON.parse(readFileSync(s.plan));
 assert.equal(summary.state,'PLAN_ONLY');assert.equal(summary.releaseReadiness,'BLOCKED_LIFECYCLE_NOT_IMPLEMENTED');
 assert.equal(statSync(s.plan).mode&0o777,0o600);assert.ok(statSync(s.plan).size<=pairedTokenReleaseOutputMaxBytes);
 assert.equal(record.plan.components.length,2);assert.equal(record.componentEvidence.length,2);assert.equal(record.current,undefined);
 const ownerIndices=s.trace.flatMap((e,i)=>e.operation==='owner'?[i]:[]);
 const gatewayIndices=s.trace.flatMap((e,i)=>e.operation==='serving'&&e.name==='control-api-gateway'?[i]:[]);
 assert.equal(ownerIndices.length,3);assert.equal(gatewayIndices.length,2);assert.ok(ownerIndices[2]>gatewayIndices[1]);
 const original=readFileSync(s.plan);const status=await s.run('status',join(s.directory,'status.json'));
 assert.equal(status.state,'PLAN_ONLY');assert.deepEqual(readFileSync(s.plan),original);
 assert.ok(!JSON.stringify(summary).includes('beforeSpec'));assert.ok(!JSON.stringify(summary).includes('synthetic-kubeconfig'));
 await assert.rejects(()=>s.run(),/EEXIST/);assert.deepEqual(readFileSync(s.plan),original);
});

test('последний Gateway/backend, source и original journal drift закрывают output без replay',async t=>{
 for(const variant of ['backend','authority','api','source','journal','CRI','pagination','context']){
  const s=setup(t),originalOwner=s.io.readOwner;let calls=0;
  if(variant==='backend'||variant==='authority')s.io.readOwner=manifest=>{
   calls++;if(calls===3){
    if(variant==='backend')s.f.controlPlane.backend.databaseOID='20009';
    else{delete s.f.authority.baselineSHA256;s.f.authority.snapshotRevision++;s.f.authority.baselineSHA256=fingerprint(s.f.authority);}
   }return originalOwner(manifest);
  };
  if(variant==='api'){
   const read=s.io.readServing;let n=0;s.io.readServing=(kube,name)=>{
    if(name==='control-api-gateway'&&++n===2)s.f.api.service.spec.selector={};return read(kube,name);
   };
  }
  if(variant==='source')s.io.inspectSource=path=>({revision:path===s.config.source.path?source:'f'.repeat(40-(calls++>0?1:0))});
  if(variant==='journal'){
   const read=s.io.readNodes;s.io.readNodes=async options=>{writeFileSync(s.config.admission.evidencePath,'{}\n');return read(options);};
  }
  if(variant==='CRI')s.io.readNodes=async()=>({...s.f.nodeReadback,images:[]});
  if(variant==='context')s.io.sourceArchiveHash=async()=>'f'.repeat(64);
  if(variant==='pagination'){
   const read=s.io.kube;s.io.kube=args=>args[args.indexOf('get')+1]==='nodes'?
    JSON.stringify({items:s.f.observed.nodes,metadata:{continue:'truncated'}}):read(args);
  }
  await assert.rejects(()=>s.run());assert.equal(existsSync(s.plan),false);
 }
});

test('status не принимает другой original executor/config, fake APPLIED или изменённый component receipt',async t=>{
 for(const alter of [r=>r.executorSHA='f'.repeat(40),r=>r.configSHA256='f'.repeat(64),r=>r.plan.state='APPLIED',
  r=>r.componentEvidence[0].servingSHA256='f'.repeat(64)]){
  const s=setup(t);await s.run();const record=JSON.parse(readFileSync(s.plan));alter(record);
  writeFileSync(s.plan,JSON.stringify(record)+'\n');
  await assert.rejects(()=>s.run('status',join(s.directory,'status.json')));assert.equal(existsSync(join(s.directory,'status.json')),false);
 }
});

test('закрыты mutation commands, ambient kubeconfig, symlink, input duplicates и неприватный output',async t=>{
 const s=setup(t);
 for(const args of [[],['apply'],['rollback'],['up'],['plan','--input',s.input,'--output',s.plan],
  ['plan','--input',s.input,'--input',s.input],['plan','--command','up']]){
  await assert.rejects(()=>pairedTokenReleaseCLI(args,s.io));assert.equal(s.trace.length,0);
 }
 const link=join(s.directory,'linked');symlinkSync(s.kubeconfig,link);
 await assert.rejects(()=>pairedTokenReleaseCLI(['plan','--input',s.input,'--kubeconfig',link,'--output',s.plan],s.io));
 chmodSync(s.directory,0o755);await assert.rejects(()=>s.run(),/PRIVATE_OUTPUT_REQUIRED/);chmodSync(s.directory,0o700);
 writeFileSync(s.input,'{"schema":"first","schema":"second"}\n');await assert.rejects(()=>s.run(),/PRIVATE_JSON_REJECTED/);
 assert.equal(existsSync(s.plan),false);
});

test('artefact cap применяется к фактическим JSON bytes; CLI неизвестной команды не исполняет дочерние процессы',()=>{
 assert.throws(()=>boundPairedTokenReleaseArtifact({payload:'x'.repeat(pairedTokenReleaseOutputMaxBytes)}),/OUTPUT_BOUND_EXCEEDED/);
 assert.equal(boundPairedTokenReleaseArtifact({bounded:true}).bounded,true);
 assert.throws(()=>execFileSync(process.execPath,[new URL('./paired-token-release-cli.mjs',import.meta.url).pathname,'apply'],
  {timeout:10000,maxBuffer:65536,stdio:['ignore','pipe','pipe']}),error=>{
  assert.equal(error.status,1);assert.match(String(error.stderr),/^PAIRED_TOKEN_RELEASE_READ_ONLY_COMMAND_REQUIRED\n$/);return true;
 });
});
