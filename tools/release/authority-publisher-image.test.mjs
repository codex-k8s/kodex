import assert from 'node:assert/strict';
import test from 'node:test';
import {mkdtempSync,mkdirSync,copyFileSync,rmSync,readFileSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {publisherImageSource,readPublisherImageNodes,planPublisherImage,publisherImageStep,verifyPublisherReadbackExecutor,main} from './authority-publisher-image.mjs';
import {fingerprint} from './scoped-release.mjs';
import {fixture,prepareAdmission,uid} from './fixtures/authority-catalog-synthetic.mjs';

function imageFixture(t){
 const f=fixture();f.directory=mkdtempSync(join(tmpdir(),'authority-image-'));t.after(()=>rmSync(f.directory,{recursive:true,force:true}));
 f.revision='a'.repeat(40);f.profile.context='k3d-synthetic';
 const digest='sha256:'+'e'.repeat(64),build={schema:'kodex.stable-local-build/v1',component:'internal-rpc-authority',sourceSHA:f.revision,
  contextSHA256:'b'.repeat(64),archiveSHA256:'c'.repeat(64),imageDigest:digest,dockerfile:'services/internal/internal-rpc-authority/Dockerfile',
  publication:'NOT_RUN',admission:'NOT_RUN',nodePull:'NOT_RUN'};
 const image={component:build.component,sourceSHA:f.revision,digest,tag:`stable-${f.revision}-${digest.slice(-12)}`};
 const publicationPlan={schema:'kodex.stable-image-publication/v2',artifactProfile:'built',context:f.profile.context,source:f.revision,
  clusterUID:f.profile.clusterUID,namespaceUID:f.profile.namespaceUID,receipt:build,archiveSHA256:build.archiveSHA256,image};
 const evidence={publicationPlan,publicationEvidence:[{schema:'kodex.stable-image-publication-attempt/v1',status:'INTENT',mode:'apply',planSHA256:fingerprint(publicationPlan),component:build.component,digest},
  {status:'PASS',phase:'REGISTRY_PUBLICATION_ONLY',nodePull:'NOT_RUN'}]};
 const source=publisherImageSource(f.revision,evidence,build,build.contextSHA256);
 const nodes=[0,1].map(index=>({apiVersion:'v1',kind:'Node',metadata:{name:`k3d-synthetic-agent-${index}`,uid:uid(200+index)},
  status:{conditions:[{type:'Ready',status:'True'},{type:'DiskPressure',status:'False'}]}}));
 const containers=nodes.map((node,index)=>({Id:String(index+1).repeat(64),Image:'sha256:'+'d'.repeat(64),Name:'/'+node.metadata.name,State:{Running:true},
  Config:{Labels:{'k3d.cluster':'synthetic','k3d.role':'agent'},Cmd:['server'],Entrypoint:['entrypoint'],Env:['NO_CREDENTIAL_VALUES_EXPORTED=1']}}));
 const state={nodeReplaced:false,imageMissing:false,driftDuringCRI:false,effects:0,loseACK:false,calls:[],slices:{items:[]},isolationPods:{items:[]}};
 f.state=state;f.publisher.spec.replicas=1;
 f.publisher.spec.template.spec.containers.push({name:'unchanged-sidecar',image:'registry.example.invalid/sidecar@sha256:'+'a'.repeat(64),command:['sidecar'],args:['unchanged']});
 f.publisher.spec.template.spec.volumes=[{name:'policy',configMap:{name:'unchanged-public-policy'}}];
 f.publisher.spec.template.spec.containers[0].volumeMounts=[{name:'policy',mountPath:'/policy',readOnly:true}];
 const kube=argv=>{
  state.calls.push(['kubectl',...argv]);const at=argv.indexOf('get'),kind=argv[at+1],name=argv[at+2];
  if(kind==='nodes')return JSON.stringify({items:state.nodeReplaced?[{...nodes[0],metadata:{...nodes[0].metadata,uid:uid(999)}}]:nodes});
  const value=f.get(kind,name);assert.ok(value);return JSON.stringify(value);
 };
 const execute=(program,args)=>{
  state.calls.push([program,...args]);assert.equal(program,'docker');
  if(args[0]==='inspect')return JSON.stringify([containers.find(container=>container.Name==='/'+args.at(-1))]);
  assert.equal(args[0],'exec');assert.ok(containers.some(container=>container.Id===args[1]));
  assert.equal(args[2],'crictl');assert.ok(args.includes('inspecti'));assert.equal(args.includes('pull'),false);
  if(state.driftDuringCRI)state.nodeReplaced=true;
  return JSON.stringify({status:{id:'sha256:'+'f'.repeat(64),repoDigests:state.imageMissing?[]:[source.imageRef]}});
 };
 const observe=()=>readPublisherImageNodes({source,imageRef:source.imageRef,targetProfile:f.profile,context:f.profile.context},kube,execute);
 const nodeProof=observe(),admission=prepareAdmission(f);
 const plan=planPublisherImage(f.publisher,{source,executorSource:{path:'/synthetic-executor',revision:f.revision},targetProfile:f.profile,context:f.profile.context,kubeconfig:join(f.directory,'config'),nodeProof,admission:{...admission.paths,proof:admission.proof},imageUpdateID:uid(220)});
 const journal={events:[{type:'HEADER'}],append(row){this.events.push(row);}};
 let resources={items:[]};
 const patch=(operations,dryRun)=>{
  for(const op of operations.filter(row=>row.op==='test')){let value=f.publisher;for(const key of op.path.slice(1).split('/'))value=value[key];assert.deepEqual(value,op.value);}
  if(dryRun)return;
  for(const op of operations.filter(row=>['add','replace'].includes(row.op))){const keys=op.path.slice(1).split('/');let parent=f.publisher;for(const key of keys.slice(0,-1))parent=parent[key];parent[keys.at(-1)]=structuredClone(op.value);}
  state.effects++;f.publisher.metadata.resourceVersion=String(Number(f.publisher.metadata.resourceVersion)+1);
  f.publisher.metadata.annotations['deployment.kubernetes.io/revision']='2';
  if(state.loseACK)throw Error('SYNTHETIC_LOST_ACK');
 };
 const step=phase=>publisherImageStep({phase,plan,journal,get:()=>f.publisher,resources:()=>resources,gate:async()=>assert.deepEqual(observe(),nodeProof),patch});
 const ready=()=>{
  f.publisher.metadata.generation=2;f.publisher.status={observedGeneration:2,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1};
  const rs={kind:'ReplicaSet',metadata:{name:'publisher-rs',namespace:'kodex-system',uid:uid(230),ownerReferences:[{kind:'Deployment',controller:true,uid:f.publisher.metadata.uid}]}};
  const spec=structuredClone(f.publisher.spec.template.spec);spec.nodeName=nodes[0].metadata.name;
  resources={items:[rs,{kind:'Pod',metadata:{name:'publisher-pod',uid:uid(231),namespace:'kodex-system',ownerReferences:[{kind:'ReplicaSet',controller:true,uid:rs.metadata.uid}]},spec,
   status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:spec.containers.map(container=>({name:container.name,ready:true,imageID:container.image}))}}]};
 };
 return {...f,source,evidence,build,state,plan,journal,nodeProof,observe,step,ready,kube,execute,patch,admission,getResources:()=>resources};
}

test('publisher image requires authentic publication/source/canonical Dockerfile without a free image override',t=>{
 const f=imageFixture(t);assert.match(f.source.imageRef,/internal-rpc-authority@sha256:/);
 for(const mutate of [value=>value.publicationEvidence[1].status='NOT_RUN',value=>value.publicationPlan.receipt.component='control-plane',
  value=>value.publicationPlan.receipt.dockerfile='foreign/Dockerfile',value=>value.publicationPlan.receipt.sourceSHA='d'.repeat(40)]){
  const evidence=structuredClone(f.evidence);mutate(evidence);assert.throws(()=>publisherImageSource(f.revision,evidence,f.build,f.build.contextSHA256));
 }
 assert.throws(()=>publisherImageSource(f.revision,f.evidence,f.build,'0'.repeat(64)),/SOURCE_REJECTED/);
});

test('node proof uses actual NodeUID/containerUID/CRI repoDigest readback and never pulls',t=>{
 const f=imageFixture(t);assert.equal(f.nodeProof.nodes.length,2);assert.equal(f.nodeProof.nodes[0].containerID.length,64);
 assert.equal(f.state.calls.some(call=>call.includes('pull')),false);
 f.state.imageMissing=true;assert.throws(f.observe,/NODE_DIGEST_REJECTED/);
 f.state.imageMissing=false;f.state.driftDuringCRI=true;assert.throws(f.observe,/NODES_CHANGED/);
});

test('publisher CAS changes only its main image and waits for exact owned ready Pods',async t=>{
 const f=imageFixture(t),before=structuredClone(f.publisher.spec);
 const first=await f.step('apply');assert.equal(first.state,'CONVERGING');assert.equal(f.state.effects,1);
 const expected=structuredClone(before);expected.template.spec.containers[0].image=f.source.imageRef;
 assert.deepEqual(f.publisher.spec,expected);assert.deepEqual(f.publisher.spec.template.spec.volumes,before.template.spec.volumes);
 assert.equal((await f.step('status')).state,'CONVERGING');f.ready();assert.equal((await f.step('status')).state,'READY');
 assert.equal((await f.step('apply')).state,'READY');assert.equal(f.state.effects,1);
});

test('publisher UNKNOWN resumes by readback with no second image patch and rejects foreign UID/spec',async t=>{
 const f=imageFixture(t);f.state.loseACK=true;await assert.rejects(f.step('apply'),/OUTCOME_UNKNOWN/);assert.equal(f.state.effects,1);
 assert.equal((await f.step('status')).state,'CONVERGING');assert.equal((await f.step('apply')).state,'CONVERGING');assert.equal(f.state.effects,1);
 f.publisher.spec.template.spec.volumes=[{name:'foreign'}];await assert.rejects(f.step('apply'),/DEPLOYMENT_DRIFT/);
});

test('готовность принимает только точный Pod default enableServiceLinks=true при отсутствии поля в template',async t=>{
 const f=imageFixture(t);await f.step('apply');f.ready();
 const pod=f.getResources().items.find(value=>value.kind==='Pod');
 pod.spec.enableServiceLinks=true;
 assert.equal((await f.step('status')).state,'READY');
 for(const value of [false,null,'true',1]){
  pod.spec.enableServiceLinks=value;assert.equal((await f.step('status')).state,'CONVERGING');
 }
 pod.spec.enableServiceLinks=true;pod.spec.unknownDefault=true;
 assert.equal((await f.step('status')).state,'CONVERGING');
 delete pod.spec.unknownDefault;pod.spec.containers[0].args=['foreign'];
 assert.equal((await f.step('status')).state,'CONVERGING');
 assert.equal(f.state.effects,1);
});

test('явный enableServiceLinks=false остаётся обязательным значением template',async t=>{
 const f=imageFixture(t);
 for(const spec of [f.publisher.spec,f.plan.beforeSpec,f.plan.afterSpec])spec.template.spec.enableServiceLinks=false;
 await f.step('apply');f.ready();const pod=f.getResources().items.find(value=>value.kind==='Pod');
 assert.equal((await f.step('status')).state,'READY');
 pod.spec.enableServiceLinks=true;assert.equal((await f.step('status')).state,'CONVERGING');
 delete pod.spec.enableServiceLinks;assert.equal((await f.step('status')).state,'CONVERGING');
});

test('successor readback требует exact clean predecessor и подтверждённое Git ancestry',()=>{
 const previous={path:'/synthetic/old',revision:'a'.repeat(40)},current={path:'/synthetic/new',revision:'b'.repeat(40)};
 const inspect=path=>{assert.equal(path,previous.path);return {revision:previous.revision};};
 let joins=0;const ancestor=(path,before,after)=>{assert.equal(path,current.path);assert.equal(before,previous.revision);assert.equal(after,current.revision);joins++;};
 verifyPublisherReadbackExecutor(current,previous,previous.revision,inspect,ancestor);assert.equal(joins,1);
 assert.throws(()=>verifyPublisherReadbackExecutor(current,previous,'c'.repeat(40),inspect,ancestor),/SUCCESSOR_INVALID/);
 assert.throws(()=>verifyPublisherReadbackExecutor(previous,previous,previous.revision,inspect,ancestor),/SUCCESSOR_INVALID/);
 assert.throws(()=>verifyPublisherReadbackExecutor(current,previous,previous.revision,()=>({revision:current.revision}),ancestor),/SUCCESSOR_INVALID/);
 assert.throws(()=>verifyPublisherReadbackExecutor(current,previous,previous.revision,()=>{throw Error('SOURCE_CHECKOUT_NOT_EXACT');},ancestor),/NOT_EXACT/);
 assert.throws(()=>verifyPublisherReadbackExecutor(current,previous,previous.revision,inspect,()=>{throw Error('NON_ANCESTOR');}),/NON_ANCESTOR/);
 assert.equal(joins,1);
});

test('public image CLI consumes authentic receipts, re-reads nodes and preserves READY after Service reopens',async t=>{
 const f=imageFixture(t),path=name=>join(f.directory,name),write=(name,value)=>writeFileSync(path(name),JSON.stringify(value),{mode:0o600});
 write('publication.json',f.evidence.publicationPlan);writeFileSync(path('publication.jsonl'),f.evidence.publicationEvidence.map(row=>JSON.stringify(row)+'\n').join(''),{mode:0o600});
 write('build.json',f.build);write('target.json',f.profile);writeFileSync(path('config'),'synthetic-unused-kubeconfig',{mode:0o600});
 const source=new URL('../..',import.meta.url).pathname.replace(/\/$/,'');
 const common=['--kubeconfig',path('config'),'--context',f.profile.context];
 const sourceFlags=['--source',source,'--revision',f.revision,'--publication-plan',path('publication.json'),'--publication-evidence',path('publication.jsonl'),'--build',path('build.json'),'--target',path('target.json')];
 const io={inspectSource:()=>({revision:f.revision}),sourceArchiveHash:async()=>f.build.contextSHA256,execFileSync:(program,args,options)=>{
  if(program==='docker')return f.execute(program,args);
  assert.equal(program,'kubectl');assert.deepEqual(args.slice(0,5),[...common,'--request-timeout=25s']);
  const tail=args.slice(5),argv=[];for(let index=0;index<tail.length;index++){if(tail[index]==='-n'){assert.equal(tail[++index],'kodex-system');}else argv.push(tail[index]);}
  if(argv[0]==='get'&&argv[1]==='replicasets,pods,jobs')return f.getResources().items.map(resource=>[resource.kind,resource.metadata.name,resource.metadata.uid,
   resource.metadata.labels?.['app.kubernetes.io/name']??'',resource.metadata.namespace,JSON.stringify(resource.metadata.ownerReferences??[])].join('\t')).join('\n');
  if(argv[0]==='get'&&argv[1]==='pod')return JSON.stringify(f.getResources().items.find(resource=>resource.kind==='Pod'&&resource.metadata.name===argv[2]));
  if(argv[0]==='get'&&argv[1]==='endpointslices')return JSON.stringify(f.state.slices);
  if(argv[0]==='get'&&argv[1]==='pods')return JSON.stringify(f.state.isolationPods);
  if(argv[0]==='patch'){
   const operations=JSON.parse(readFileSync(argv[argv.indexOf('--patch-file')+1],'utf8'));
   f.patch(operations,argv.includes('--dry-run=server'));if(!argv.includes('--dry-run=server'))f.ready();return 'patched';
  }
  return f.kube(argv);
 }};
 await main(['nodes',...common,...sourceFlags,'--output',path('nodes.json')],io);
 const stored=JSON.parse(readFileSync(path('nodes.json'),'utf8'));assert.deepEqual(stored,f.nodeProof);
 await main(['plan',...common,...sourceFlags,...f.admission.flags,'--node-proof',path('nodes.json'),'--plan',path('plan.json')],io);
 const plan=JSON.parse(readFileSync(path('plan.json'),'utf8'));
 for(const phase of ['apply','plan','nodes'])await assert.rejects(main([phase,...common,'--predecessor-executor-sha',f.revision],io),/ARGUMENTS_REJECTED/);
 const changedIO={...io,inspectSource:path=>({revision:path===plan.executorSource.path?'d'.repeat(40):f.revision})};
 await assert.rejects(main(['apply',...common,'--plan',path('plan.json'),'--evidence',path('wrong-executor.jsonl'),'--approved-plan-sha',fingerprint(plan)],changedIO),/EXECUTOR_SOURCE_CHANGED/);
 assert.equal(f.state.effects,0);
 await main(['apply',...common,'--plan',path('plan.json'),'--evidence',path('image.jsonl'),'--approved-plan-sha',fingerprint(plan)],io);
 assert.equal(f.state.effects,1);assert.equal(f.publisher.spec.template.spec.containers[0].image,f.source.imageRef);
 // Read-only READY после снятия admission gate не запускает повторный patch.
 delete f.service.spec.selector['kodex.dev/component-traffic-isolation'];delete f.service.metadata.annotations['kodex.dev/component-traffic-isolation'];
 await main(['status',...common,'--plan',path('plan.json'),'--evidence',path('image.jsonl')],io);
 assert.equal(f.state.effects,1);assert.equal(f.state.calls.some(call=>call.includes('pull')),false);
 // Отдельная synthetic история моделирует прежний executor и image checkout.
 // Исходные fixture plan/journal сохраняются; новый CLI только дописывает readback.
 const inherited=structuredClone(plan),imageSource=path('image-source'),oldExecutor=path('old-executor'),nextSHA='d'.repeat(40);
 const dockerfile='services/internal/internal-rpc-authority/Dockerfile';
 mkdirSync(join(imageSource,'services/internal/internal-rpc-authority'),{recursive:true});
 copyFileSync(join(source,dockerfile),join(imageSource,dockerfile));
 inherited.source.path=imageSource;inherited.executorSource.path=oldExecutor;
 write('inherited-plan.json',inherited);
 const events=readFileSync(path('image.jsonl'),'utf8').trim().split('\n').map(JSON.parse);events[0].planSHA256=fingerprint(inherited);
 const original=events.map(value=>JSON.stringify(value)+'\n').join('');
 writeFileSync(path('inherited.jsonl'),original,{mode:0o600});
 const inheritedBytes=readFileSync(path('inherited-plan.json'));
 const successorIO={...io,inspectSource:where=>({revision:where===source?nextSHA:f.revision}),execFileSync:(program,args,options)=>{
  if(program==='git'){assert.deepEqual(args,['-C',source,'merge-base','--is-ancestor',f.revision,nextSHA]);return '';}
  return io.execFileSync(program,args,options);
 }};
 const inheritedArgs=[...common,'--plan',path('inherited-plan.json'),'--evidence',path('inherited.jsonl')];
 await assert.rejects(main(['status',...inheritedArgs],successorIO),/EXECUTOR_SOURCE_CHANGED/);
 await main(['status',...inheritedArgs,'--predecessor-executor-sha',f.revision],successorIO);
 const updated=readFileSync(path('inherited.jsonl'),'utf8');assert.ok(updated.startsWith(original));
 const rows=updated.trim().split('\n').map(JSON.parse);
 assert.deepEqual(rows.at(-2),{type:'READBACK_EXECUTOR',sourceSHA:nextSHA,predecessorExecutorSHA:f.revision,at:rows.at(-2).at});
 assert.equal(rows.at(-1).state,'READY');assert.deepEqual(readFileSync(path('inherited-plan.json')),inheritedBytes);
 assert.equal(f.state.effects,1);
});
