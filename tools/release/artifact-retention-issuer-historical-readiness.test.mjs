import test from 'node:test';
import assert from 'node:assert/strict';
import {writeFileSync,readFileSync} from 'node:fs';
import {dirname,join} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {root,uid,after,publication,fixture as journalFixture,ownerFixture} from './fixtures/issuer-migration-synthetic.mjs';
import {planIssuerMigration,issuerMigrationIO,issuerMigrationSQL,issuerMigrationJournalHeader,executeIssuerMigration,
 issuerMigrationHistoricalReadinessIO} from './artifact-retention-issuer-migration.mjs';
import {captureIssuerMigrationHistoricalReadiness,readArtifactRetentionIssuerHistoricalMigrationReady,readOptionalIssuerMigrationReadiness,normalizeIssuerMigrationReadinessReference} from './artifact-retention-issuer-historical-readiness.mjs';

async function fixture(t){
 const f=ownerFixture(),local=journalFixture(t),directory=dirname(local.path),calls=[];
 f.state.pod.spec.nodeName='synthetic-node';
 const affinity={required:{nodeSelectorTerms:[{matchExpressions:[{key:'kubernetes.io/hostname',operator:'In',values:['synthetic-node']}]}]}};
 const claim={apiVersion:'v1',kind:'PersistentVolumeClaim',metadata:{name:'data-kodex-postgresql-0',namespace:'kodex-system',uid:uid(100)},
  spec:{accessModes:['ReadWriteOnce'],volumeName:'synthetic-pv',storageClassName:'local-path'},status:{phase:'Bound'}};
 const pv={apiVersion:'v1',kind:'PersistentVolume',metadata:{name:'synthetic-pv',uid:uid(101)},
  spec:{accessModes:['ReadWriteOnce'],storageClassName:'local-path',nodeAffinity:affinity,local:{path:'/synthetic/pvc-data'},
   claimRef:{kind:'PersistentVolumeClaim',name:claim.metadata.name,namespace:'kodex-system',uid:claim.metadata.uid}},status:{phase:'Bound'}};
 const node={apiVersion:'v1',kind:'Node',metadata:{name:'synthetic-node',uid:uid(102),labels:{'kubernetes.io/hostname':'synthetic-node'}}};
 const absence={api:[],runtime:[],runtimeFailure:false,unexpectedNode:false};
 const kube=(args,sql)=>{
  calls.push({args,sql});
  if(args.includes('--raw')){if(absence.runtimeFailure)throw Error('SYNTHETIC_RUNTIME_UNAVAILABLE');return JSON.stringify(Object.hasOwn(absence,'runtimeEnvelope')?absence.runtimeEnvelope:{apiVersion:'v1',kind:'PodList',items:absence.runtime});}
  if(args.includes('nodes'))return '\nsynthetic-node\t'+(absence.unexpectedNode?uid(199):node.metadata.uid)+'\t'+(absence.nodeReady??'True')+'\t\tlive\n';
  if(args.includes('pods')&&args.at(-1).startsWith('go-template='))return Object.hasOwn(absence,'apiRaw')?absence.apiRaw:'\n'+absence.api.map(p=>[p.uid,p.ns??'kodex-system',p.name??'orphan',p.owner??'',p.label??'','','live'].join('\t')+'\n').join('');
  if(args.includes('pvc'))return JSON.stringify(claim);
  if(args.includes('pv'))return JSON.stringify(pv);
  if(args.includes('node'))return JSON.stringify(node);
  if(args.includes('exec')&&String(sql).startsWith('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;')&&f.state.ownerOverride)
   return JSON.stringify({...after(),...f.state.ownerOverride});
  return f.kube(args,sql);
 };
 const plan=planIssuerMigration({target:{context:'fixture',clusterUID:uid(2),namespaceUID:uid(3)},source:{path:root,revision:'a'.repeat(40)},publication:publication(),intentID:uid(1),evidencePath:local.path},kube);
 let journal=privateJournal(local.path,issuerMigrationJournalHeader(plan));
 try{assert.equal((await executeIssuerMigration('apply',plan,journal,issuerMigrationIO(plan,kube))).status,'PASS');}finally{journal.close();}
 const planPath=join(directory,'original-plan.json'),incidentPlanPath=join(directory,'incident-before.json'),incidentReceiptPath=join(directory,'incident-after.json');
 const before={scope:{context:'fixture',clusterUID:uid(2),namespaceUID:uid(3)},pvs:[{uid:pv.metadata.uid,name:pv.metadata.name,claimUID:claim.metadata.uid,ns:'kodex-system',phase:'Bound',nodeAffinitySHA256:fingerprint(affinity)}],entries:[{name:node.metadata.name,uid:node.metadata.uid}]};
 const recovered={status:'RECOVERED',clusterUID:uid(2),namespaceUID:uid(3),existingPVsUnchanged:1,observedAt:new Date().toISOString()};
 const write=()=>{for(const [path,body] of [[planPath,plan],[incidentPlanPath,before],[incidentReceiptPath,recovered]])writeFileSync(path,JSON.stringify(body)+'\n',{mode:0o600});};write();
 const input={planPath,evidencePath:local.path,sourceSHA:plan.source.revision,target:plan.target,incidentPlanPath,incidentReceiptPath};
 const originalPlan=readFileSync(planPath),originalReceipt=readFileSync(local.path);
 const replace=()=>{f.state.pod.metadata.uid=uid(44);f.state.pod.status.podIPs=[{ip:'10.42.0.44'}];
  f.state.endpoint.endpoints[0].targetRef.uid=uid(44);f.state.endpoint.endpoints[0].addresses=['10.42.0.44'];};
 const io={inspectSource:()=>({revision:plan.source.revision})};calls.length=0;
 return {...f,plan,input,before,recovered,claim,pv,node,kube,io,replace,calls,write,originalPlan,originalReceipt,absence,
  capture:()=>captureIssuerMigrationHistoricalReadiness(input,kube,io),
  read:context=>readArtifactRetentionIssuerHistoricalMigrationReady({...input,context},kube,io)};
}

test('original fresh-Pod refusal воспроизводится; historical R/O сохраняет original terminal receipt',async t=>{
 const f=await fixture(t);f.replace();assert.throws(()=>issuerMigrationIO(f.plan,f.kube).preflight(),/BOUNDARY_CHANGED/);
 const context=await f.capture(),result=await f.read(context);
 assert.equal(result.purpose,'HISTORICAL_ROLE_ONLY');assert.equal(result.ready.migrationVersion,20261115000200);
 assert.equal(result.ready.jobUID,uid(6));assert.equal(result.ready.podUID,uid(7));assert.equal(result.postgres.uid,uid(44));
 assert.equal(context.originalPostgres.uid,uid(4));assert.equal(context.storage.claim.uid,uid(100));
 assert.deepEqual(readFileSync(f.input.planPath),f.originalPlan);assert.deepEqual(readFileSync(f.input.evidencePath),f.originalReceipt);
 assert.equal(f.state.creates,1);assert.ok(f.calls.every(({args})=>!args.some(arg=>['create','patch','replace','delete','apply'].includes(arg))));
 const io=issuerMigrationHistoricalReadinessIO(f.plan,f.kube,context.current);assert.throws(()=>io.createJob(),/HISTORICAL_READ_ONLY/);
 assert.throws(()=>issuerMigrationIO(f.plan,f.kube).preflight(),/BOUNDARY_CHANGED/,'mutating plan remains bound to original Pod');
});

test('backend/SystemID/OID, PV/PVC/node substitution и неверный role head закрыты',async t=>{
 for(const mutate of [f=>f.state.backend.systemIdentifier='18446744073709551614',f=>f.state.backend.databaseOID='20000',
  f=>f.pv.metadata.uid=uid(199),f=>f.claim.metadata.uid=uid(198),f=>f.node.metadata.uid=uid(197),
  f=>f.pv.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[0].values=['foreign-node'],
  f=>f.state.ownerOverride={roleValid:false},f=>f.state.ownerOverride={activeRotationCount:1},f=>f.state.ownerOverride={head:20261115000100}]){
  const f=await fixture(t);f.replace();mutate(f);await assert.rejects(f.capture,undefined,String(mutate));
  assert.deepEqual(readFileSync(f.input.evidencePath),f.originalReceipt);assert.equal(f.state.creates,1);
 }
});

test('sealed context не допускает поздний storage/endpoint/Job/receipt drift',async t=>{
 for(const mutate of [f=>f.pv.spec.local.path='/foreign/data',f=>f.state.endpoint.metadata.uid=uid(196),
  f=>f.state.pod.metadata.uid=uid(195),f=>f.state.job.metadata.uid=uid(194),f=>f.state.jobPod.metadata.uid=uid(193),
  f=>{f.before.pvs[0].claimUID=uid(192);f.write();}]){
  const f=await fixture(t);f.replace();const context=await f.capture();mutate(f);await assert.rejects(()=>f.read(context),undefined,String(mutate));
  assert.deepEqual(readFileSync(f.input.evidencePath),f.originalReceipt);assert.equal(f.state.creates,1);
 }
});


test('explicit optional ref сохраняет отдельный stable historical proof и original ready',async t=>{
 const f=await fixture(t);f.replace();const context=await f.capture();
 const contextPath=join(dirname(f.input.planPath),'context.json');writeFileSync(contextPath,JSON.stringify(context)+'\n',{mode:0o600});
 const input={issuerMigration:{planPath:f.input.planPath,evidencePath:f.input.evidencePath,sourceSHA:f.input.sourceSHA},
  issuerMigrationReadiness:{contextPath,contextSHA256:context.contextSHA256},target:f.input.target};
 const first=await readOptionalIssuerMigrationReadiness(input,f.kube,f.io),second=await readOptionalIssuerMigrationReadiness(input,f.kube,f.io);
 assert.deepEqual(first,second);assert.equal(first.historicalReadiness.schema,'kodex.artifact-retention-issuer-historical-readiness/v1');
 assert.deepEqual(first.ready,context.acceptedReceipt);assert.equal(Object.hasOwn(first.historicalReadiness,'observedAt'),false);
 await assert.rejects(readOptionalIssuerMigrationReadiness({...input,issuerMigrationReadiness:{...input.issuerMigrationReadiness,contextSHA256:'f'.repeat(64)}},f.kube,f.io),/REFERENCE_CHANGED/);
 for(const bad of [{contextPath,contextSHA256:context.contextSHA256,ready:true},{contextPath:'relative',contextSHA256:'f'.repeat(64)}])
  assert.throws(()=>normalizeIssuerMigrationReadinessReference(bad),/REFERENCE_REJECTED/);
});

test('исторический контекст закрывает unknown fields и подмену принятой квитанции',async t=>{
 for(const change of [c=>c.acceptedReceipt.podUID=uid(199),c=>c.extra=true,c=>c.current.extra=true,c=>delete c.acceptedReceipt]){
  const f=await fixture(t);f.replace();const context=await f.capture();change(context);
  const {contextSHA256,...body}=context;context.contextSHA256=fingerprint(body);
  await assert.rejects(()=>f.read(context),undefined,String(change));
 }
});

test('точный hostPath local-path storage допускается с теми же API/SQL anchors',async t=>{
 const f=await fixture(t);f.replace();f.pv.spec.hostPath={path:f.pv.spec.local.path,type:'DirectoryOrCreate'};delete f.pv.spec.local;
 const context=await f.capture();assert.equal(context.storage.volume.source.kind,'hostPath');
 assert.equal((await f.read(context)).ready.jobUID,uid(6));
});


test('prior PASS + retained complete Job позволяет historical R/O после GC Pod без нового join/00200',async t=>{
 const f=await fixture(t);f.replace();
 f.state.job.status.completionTime=new Date(Date.now()-10000).toISOString();
 f.state.jobPod=null;
 const context=await f.capture(),proof=await f.read(context);
 assert.equal(proof.ready.podUID,uid(7));assert.equal(proof.ready.jobUID,uid(6));assert.equal(Object.hasOwn(proof,'joined'),false);
 assert.deepEqual(readFileSync(f.input.evidencePath),f.originalReceipt);assert.equal(f.state.creates,1);
 assert.ok(f.calls.some(({args})=>args.includes('--raw')&&args.at(-1).endsWith('/proxy/runningpods/')));
});

test('missing/stale/active Job, API orphan, CRI evidence и недоступный node не дают historical fallback',async t=>{
 for(const change of [f=>f.state.job=null,f=>f.state.job.status.completionTime=new Date(Date.now()+10000).toISOString(),
  f=>f.state.job.status.active=1,f=>f.state.job.metadata.uid=uid(199),f=>f.state.job.status.terminating=1,
  f=>f.state.job.status.uncountedTerminatedPods={succeeded:[uid(7)]},
  f=>f.absence.api=[{uid:uid(7)}],f=>f.absence.api=[{uid:uid(198),owner:uid(6)}],
  f=>f.absence.runtime=[{metadata:{uid:uid(7),name:'old',namespace:'kodex-system'}}],
  f=>f.absence.runtime=[{metadata:{uid:uid(198),name:f.state.job.metadata.name+'-orphan',namespace:'kodex-system'}}],
  f=>f.absence.api=[{uid:uid(198),name:f.state.job.metadata.name+'-spoofed',owner:uid(197),label:uid(196)}],
  f=>f.absence.apiRaw='',f=>f.absence.apiRaw='\n'+[uid(198),'kodex-system','unrelated','','','','live'].join('\t'),
  f=>f.absence.runtimeEnvelope={apiVersion:'v1',kind:'PodList'},
  f=>f.absence.runtimeEnvelope={apiVersion:'v1',kind:'PodList',items:null},
  f=>f.absence.runtimeFailure=true,f=>f.absence.unexpectedNode=true,f=>f.absence.nodeReady='Unknown',
  f=>f.state.job.status.conditions.push({type:'FailureTarget',status:'True'})]){
  const f=await fixture(t);f.replace();f.state.job.status.completionTime=new Date(Date.now()-10000).toISOString();f.state.jobPod=null;change(f);
  await assert.rejects(f.capture,undefined,String(change));assert.equal(f.state.creates,1);assert.deepEqual(readFileSync(f.input.evidencePath),f.originalReceipt);
 }
});


test('unrelated API/CRI Pod activity не меняет stable historical proof',async t=>{
 const f=await fixture(t);f.replace();f.state.job.status.completionTime=new Date(Date.now()-10000).toISOString();f.state.jobPod=null;
 const context=await f.capture(),before=await f.read(context);
 f.absence.api.push({uid:uid(191),name:'unrelated'});
 f.absence.runtime.push({metadata:{uid:uid(191),namespace:'kodex-system',name:'unrelated'}});
 assert.deepEqual(await f.read(context),before);
});
