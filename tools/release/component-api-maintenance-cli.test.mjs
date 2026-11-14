import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync,existsSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';

function setup(t){
  const directory=mkdtempSync(join(tmpdir(),'kodex-api-maintenance-'));t.after(()=>rmSync(directory,{recursive:true,force:true}));
  mkdirSync(join(directory,'bin'),{mode:0o700});
  const uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`,source='a'.repeat(40),name='control-api-gateway';
  const labels={'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'};
  const deployment={apiVersion:'apps/v1',kind:'Deployment',metadata:{uid:uid(4),name,namespace:'kodex-system',labels,resourceVersion:'10',generation:1},
    spec:{replicas:3,template:{spec:{containers:[{name:'api',image:'example.invalid/api@sha256:'+'1'.repeat(64)}]}}},
    status:{observedGeneration:1,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3}};
  const service={apiVersion:'v1',kind:'Service',metadata:{uid:uid(5),name,namespace:'kodex-system',labels,resourceVersion:'20'},
    spec:{type:'ClusterIP',clusterIP:'192.0.2.1',selector:{'app.kubernetes.io/name':name},ports:[{port:8443,targetPort:'https'}]}};
  const installation={apiVersion:'v1',kind:'ConfigMap',metadata:{uid:uid(3),name:'kodex-installation-profile',namespace:'kodex-system',
    labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/managed-by':'kodex-install'}},data:{profile:'synthetic'}};
  const target={schema:'kodex.portable-runner-target/v1',context:'synthetic',clusterUID:uid(1),namespaceUID:uid(2),installationUID:uid(3),
    installationSHA256:fingerprint(installation.data),resources:[deployment,service].map(r=>({apiVersion:r.apiVersion,kind:r.kind,name:r.metadata.name,uid:r.metadata.uid}))};
  const save=(name,value)=>writeFileSync(join(directory,name),JSON.stringify(value,null,2)+'\n',{mode:0o600});
  save('target.json',target);save('state.json',{source,target,installation,deployment,service,slices:{items:[]},pods:{items:[]},effects:[],calls:[],patchPaths:[]});
  const mock=`#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),base=path.join(__dirname,'..'),a=process.argv.slice(2),file=path.join(base,'state.json'),s=JSON.parse(fs.readFileSync(file));
const program=path.basename(process.argv[1]),save=()=>fs.writeFileSync(file,JSON.stringify(s)),out=v=>{save();console.log(JSON.stringify(v));};
s.calls.push({program,args:a});
if(program==='git'){save();if(a[0]==='rev-parse')console.log(s.source);else if(a[0]==='status'){if(s.dirty)console.log(' M fixture');}else process.exit(4);process.exit(0);}
if(a[a.indexOf('--context')+1]!==s.target.context||a[a.indexOf('--kubeconfig')+1]!==path.join(base,'explicit config'))process.exit(5);
const inventory=()=>{
 const d=s.deployment,rs={kind:'ReplicaSet',metadata:{name:'api-rs',uid:'api-rs',namespace:'kodex-system',ownerReferences:[{kind:'Deployment',uid:d.metadata.uid,controller:true}]}},pods=[];
 if(!s.emptyServing)for(let i=0;i<d.spec.replicas;i++)pods.push({apiVersion:'v1',kind:'Pod',metadata:{name:'api-'+i,uid:'api-'+i,namespace:'kodex-system',labels:{'app.kubernetes.io/name':'control-api-gateway'},ownerReferences:[{kind:'ReplicaSet',uid:'api-rs',controller:true}]},
   spec:d.spec.template.spec,status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:d.spec.template.spec.containers.map(c=>({name:c.name,ready:true,imageID:c.image}))}});
 return [rs,...pods];
};
const g=a.indexOf('get'),p=a.indexOf('patch');
if(g>=0){
 const kind=a[g+1],name=a[g+2];
 if(kind==='namespace')out({metadata:{uid:s.foreign?'foreign':name==='kube-system'?s.target.clusterUID:s.target.namespaceUID}});
 else if(kind==='configmap')out(s.installation);else if(kind==='deployment')out(s.deployment);else if(kind==='service')out(s.service);
 else if(kind==='replicasets,pods,jobs'){save();process.stdout.write(inventory().map(r=>[r.kind,r.metadata.name,r.metadata.uid,r.metadata.labels?.['app.kubernetes.io/name']??'',r.metadata.namespace,JSON.stringify(r.metadata.ownerReferences)].join('\\t')).join('\\n')+'\\n');}
 else if(kind==='pod')out(inventory().find(r=>r.metadata.name===name));
 else if(kind==='endpointslices')out(s.slices);else if(kind==='pods')out(s.pods);else process.exit(6);
}else if(p>=0){
 const resource=s[a[p+1]],patchPath=a[a.indexOf('--patch-file')+1];
 if(!resource||!patchPath||((fs.statSync(patchPath).mode&0o777)!==0o600)||((fs.statSync(path.dirname(patchPath)).mode&0o777)!==0o700))process.exit(7);
 const operations=JSON.parse(fs.readFileSync(patchPath));s.patchPaths.push(patchPath);
 for(const op of operations.filter(o=>o.op==='test')){let v=resource;for(const k of op.path.slice(1).split('/'))v=v[k];if(JSON.stringify(v)!==JSON.stringify(op.value)){save();process.exit(8);}}
 if(a.includes('--dry-run=server')){save();console.log('dry run');process.exit(0);}
 s.effects.push(a[p+1]);resource.metadata.annotations=operations[3].value;resource.spec=operations[4].value;
 resource.metadata.resourceVersion=String(Number(resource.metadata.resourceVersion)+1);
 if(a[p+1]==='deployment'){resource.metadata.generation++;resource.status={observedGeneration:resource.metadata.generation,
   ...Object.fromEntries(['replicas','updatedReplicas','readyReplicas','availableReplicas'].map(k=>[k,resource.spec.replicas]))};}
 save();if(s.lost===a[p+1]){console.error('private synthetic credential-like diagnostic');process.exit(9);}console.log('patched');
}else process.exit(10);
`;
  for(const name of ['git','kubectl'])writeFileSync(join(directory,'bin',name),mock,{mode:0o700});
  const read=name=>JSON.parse(readFileSync(join(directory,name),'utf8')),state=()=>read('state.json');
  const edit=fn=>{const value=state();fn(value);save('state.json',value);};
  const run=(tool,phase,plan,extra=[])=>spawnSync(process.execPath,[fileURLToPath(new URL(`./component-api-${tool}.mjs`,import.meta.url)),phase,
    '--plan',join(directory,plan+'.json'),'--kubeconfig',join(directory,'explicit config'),'--context','synthetic',
    ...(phase==='plan'?['--target',join(directory,'target.json')]:['--evidence',join(directory,plan+'.jsonl')]),
    ...(tool==='traffic'?['--closed-plan',join(directory,'close.json'),'--closed-evidence',join(directory,'close.jsonl')]:[]),
    ...(phase==='apply'?['--approved-plan-sha',fingerprint(read(plan+'.json'))]:[]),...extra],
    {env:{PATH:join(directory,'bin')+':/usr/bin:/bin'},encoding:'utf8',timeout:15000});
  const pass=(...args)=>{const r=run(...args);assert.equal(r.status,0,r.stderr);return JSON.parse(r.stdout);};
  const closed=['--closed-plan',join(directory,'close.json'),'--closed-evidence',join(directory,'close.jsonl')];
  const isolated=['--isolation-plan',join(directory,'isolate.json'),'--isolation-evidence',join(directory,'isolate.jsonl')];
  return {directory,read,state,edit,run,pass,closed,isolated};
}
test('настоящие CLI: close → isolate → новое приложение → open readers → restore Service',t=>{
  const f=setup(t);
  f.pass('maintenance','plan','close',['--action','close']);assert.equal(f.state().effects.length,0);
  assert.equal(f.pass('maintenance','apply','close').state,'CLOSED');
  f.pass('traffic','plan','isolate',['--action','isolate']);assert.equal(f.pass('traffic','apply','isolate').state,'ISOLATED');
  f.edit(s=>{s.deployment.spec.template.spec.containers[0].image='example.invalid/api@sha256:'+'2'.repeat(64);});
  f.pass('maintenance','plan','open',['--action','open',...f.closed,...f.isolated]);assert.equal(f.pass('maintenance','apply','open',[...f.closed,...f.isolated]).state,'OPEN');
  assert.equal(f.pass('traffic','status','isolate').state,'ISOLATED');
  f.pass('traffic','plan','restore',['--action','restore',...f.isolated]);assert.equal(f.pass('traffic','apply','restore',f.isolated).state,'RESTORED');
  assert.equal(f.pass('traffic','apply','restore',f.isolated).state,'RESTORED');
  assert.deepEqual(f.state().effects,['deployment','service','deployment','service']);
  assert.equal(f.state().deployment.spec.template.spec.containers[0].image,'example.invalid/api@sha256:'+'2'.repeat(64));
  assert.ok(f.state().patchPaths.every(p=>!existsSync(p)));
});
test('CLI UNKNOWN не раскрывает stderr и readback не повторяет effect',t=>{
  const f=setup(t);f.pass('maintenance','plan','close',['--action','close']);f.edit(s=>s.lost='deployment');
  const lost=f.run('maintenance','apply','close');assert.notEqual(lost.status,0);assert.doesNotMatch(lost.stderr,/private synthetic/);
  assert.equal(f.pass('maintenance','status','close').state,'CLOSED');f.pass('maintenance','apply','close');assert.equal(f.state().effects.length,1);
  f.edit(s=>s.lost='service');f.pass('traffic','plan','isolate',['--action','isolate']);assert.notEqual(f.run('traffic','apply','isolate').status,0);
  f.pass('traffic','status','isolate');f.pass('traffic','apply','isolate');assert.equal(f.state().effects.length,2);
});
test('CLI закрыто отклоняют другой cluster и грязный source до effects',t=>{
  const f=setup(t);f.pass('maintenance','plan','close',['--action','close']);f.edit(s=>s.foreign=true);
  assert.notEqual(f.run('maintenance','apply','close').status,0);assert.equal(f.state().effects.length,0);
  f.edit(s=>{s.foreign=false;s.dirty=true;});assert.notEqual(f.run('maintenance','apply','close').status,0);assert.equal(f.state().effects.length,0);
});
