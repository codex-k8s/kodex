import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {decodeTelegramResourceCreateSource} from './telegram-resource-create.mjs';
import {telegramRuntimeInputProjection,telegramDeploymentInputs,planTelegramRuntimeInputs,verifyTelegramRuntimeInputs,applyTelegramRuntimeInputs} from './telegram-runtime-inputs.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)),uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const execute=(command,args,input,maxBuffer=16<<20)=>execFileSync(command,args,{cwd:root,input,encoding:'utf8',timeout:20000,maxBuffer,
 env:{PATH:process.env.PATH,LANG:'C.UTF-8'},stdio:['pipe','pipe','pipe']});
// Kustomize здесь читает только repository files; config/context переданы явно,
// Kubernetes API и реальные credentials этим render не запрашиваются.
const source=decodeTelegramResourceCreateSource(execute('kubectl',['--kubeconfig','/synthetic/unused-kubeconfig','--context','synthetic',
 'kustomize','deploy/k8s/profiles/web-only']),execute);
function fixture(){
 const resources=structuredClone(source),projection=telegramRuntimeInputProjection(resources),stored=new Map(),key=(kind,name)=>kind+'/'+name;
 let sequence=10;for(const r of resources){const copy=structuredClone(r);Object.assign(copy.metadata,{uid:uid(sequence++),resourceVersion:'1'});stored.set(key(copy.kind,copy.metadata.name),copy);}
 stored.delete(key('ConfigMap',projection.policy.metadata.name));
 const endpoints=stored.get(key('ConfigMap','kodex-platform-endpoints'));endpoints.data.publicOrigin='https://synthetic.example.invalid';
 const installation={apiVersion:'v1',kind:'ConfigMap',metadata:{name:'kodex-installation-profile',namespace:'kodex-system',uid:uid(1),labels:{'app.kubernetes.io/part-of':'kodex'}},data:{synthetic:'installation'}};
 for(const name of projection.networkSources.map(r=>r.metadata.name)){
  const p=stored.get(key('NetworkPolicy',name));
  p.spec.ingress=p.spec.ingress.filter(rule=>!rule.ports?.some(port=>port.port===8086));
  for(const rule of p.spec.ingress)if(rule.from){
   rule.from=rule.from.filter(peer=>peer.podSelector?.matchLabels?.['app.kubernetes.io/name']!=='interaction-gateway');
   for(const peer of rule.from)for(const expression of peer.podSelector?.matchExpressions??[])expression.values=expression.values?.filter(value=>value!=='interaction-gateway');
  }
 }
 const service=stored.get(key('Service','egress-gateway'));service.spec.ports=service.spec.ports.filter(port=>port.name!=='telegram-connect');
 Object.assign(service.spec,{clusterIP:'192.0.2.1',clusterIPs:['192.0.2.1'],ipFamilies:['IPv4'],ipFamilyPolicy:'SingleStack'});
 const role=stored.get(key('Role','internal-rpc-authority-publisher'));role.rules[0].resourceNames=role.rules[0].resourceNames.filter(name=>!name.includes('interaction-gateway'));
 const get=(kind,name)=>structuredClone(stored.get(key(kind,name))??null),state={lostACK:'',lostBefore:'',effects:[],gateDenied:false};
 const input={resources,get,installation,sourceSHA:'a'.repeat(40),migrationID:uid(2)},plan=planTelegramRuntimeInputs(input);
 const journal={events:[],append(event){this.events.push(structuredClone(event));}};
 const kube=(args,input)=>{
  if(args.includes('get')){const index=args.indexOf('get'),kind={configmap:'ConfigMap',networkpolicy:'NetworkPolicy',service:'Service',role:'Role'}[args[index+1]];return JSON.stringify(get(kind,args[index+2]))==='null'?'':JSON.stringify(get(kind,args[index+2]));}
  if(args[0]==='create'){
   const request=JSON.parse(input),actual=structuredClone(request);Object.assign(actual.metadata,{uid:uid(900),resourceVersion:'1'});
   if(!args.includes('--dry-run=server')){stored.set(key(actual.kind,actual.metadata.name),actual);state.effects.push('CREATE');if(state.lostACK==='CREATE')throw Error('SYNTHETIC_LOST_ACK');}
   return JSON.stringify(actual);
  }
  if(args.includes('patch')){
   const index=args.indexOf('patch'),kind={networkpolicy:'NetworkPolicy',service:'Service',role:'Role'}[args[index+1]],name=args[index+2];
   const actual=stored.get(key(kind,name)),patch=JSON.parse(readFileSync(args.find(value=>value.startsWith('--patch-file=')).slice(13)));
   assert.equal(patch[0].value,actual.metadata.uid);assert.equal(patch[1].value,actual.metadata.resourceVersion);const field=patch[2].path.slice(1);assert.deepEqual(patch[2].value,actual[field]);
   if(!args.includes('--dry-run=server')){if(state.lostBefore===name)throw Error('SYNTHETIC_BEFORE_EFFECT');actual[field]=structuredClone(patch[3].value);actual.metadata.resourceVersion='2';state.effects.push(name);if(state.lostACK===name)throw Error('SYNTHETIC_LOST_ACK');}
   return JSON.stringify(actual);
  }
  throw Error('SYNTHETIC_COMMAND_REJECTED');
 };
 const step=()=>applyTelegramRuntimeInputs({plan,journal,kube,gate:()=>{if(state.gateDenied)throw Error('SYNTHETIC_TARGET_DRIFT');verifyTelegramRuntimeInputs(plan,resources,installation,get('ConfigMap','kodex-platform-endpoints'));}});
 return {input,resources,projection,stored,get,state,plan,journal,kube,step,key};
}

test('runtime inputs берутся из полного source render; существующие policy/spec/allocation не заменяются',()=>{
 const f=fixture();verifyTelegramRuntimeInputs(f.plan,f.resources,f.input.installation,f.get('ConfigMap','kodex-platform-endpoints'));
 assert.equal(f.plan.patches.length,7);
 for(const name of ['control-plane','egress-gateway']){
  const spec=structuredClone(f.resources.find(r=>r.kind==='Deployment'&&r.metadata.name===name).spec),before=structuredClone(spec);
  const unchanged=telegramDeploymentInputs(before,name,f.projection);assert.deepEqual(unchanged,spec);
  const container=before.template.spec.containers.find(c=>c.name===name),selected=name==='control-plane'?f.projection.cp:f.projection.egress;
  container.env=container.env.filter(e=>!selected.env.some(value=>e.name===value.name));container.volumeMounts=container.volumeMounts.filter(v=>v.name!==selected.volumeMount.name);before.template.spec.volumes=before.template.spec.volumes.filter(v=>v.name!==selected.volume.name);
  if(name==='egress-gateway')container.ports=container.ports.filter(p=>p.name!=='telegram-connect');
  const after=telegramDeploymentInputs(before,name,f.projection),wanted=after.template.spec.containers.find(c=>c.name===name);
  for(const env of container.env)assert.deepEqual(wanted.env.find(e=>e.name===env.name),env);
  if(name==='egress-gateway')assert.equal(wanted.env.find(e=>e.name==='EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST').value,'8529a00f3e8923e59d1776ee64d1965ee1e8f891daa17b94927c816248d6f218');
 }
 const result=f.step();assert.equal(result.state,'INPUTS_STAGED');assert.equal(f.state.effects.length,8);
 const count=f.state.effects.length;assert.equal(applyTelegramRuntimeInputs({plan:f.plan,journal:f.journal,kube:f.kube,readOnly:true}).state,'INPUTS_STAGED');assert.equal(f.step().policyUID,result.policyUID);assert.equal(f.state.effects.length,count);
 const service=f.get('Service','egress-gateway');assert.equal(service.spec.clusterIP,'192.0.2.1');assert.equal(service.spec.ports.filter(p=>p.port===8086).length,1);
});

test('create и каждый patch lostACK восстанавливаются чтением exact UID/body без второго effect',()=>{
 for(const failure of ['CREATE','egress-gateway-exact-runtime-paths','internal-rpc-authority-publisher','egress-gateway']){
  const f=fixture();f.state.lostACK=failure;assert.throws(f.step,/SYNTHETIC_LOST_ACK/);const count=f.state.effects.filter(name=>name===failure).length;
  f.state.lostACK='';assert.equal(f.step().state,'INPUTS_STAGED');assert.equal(f.state.effects.filter(name=>name===failure).length,count);
 }
});

test('foreign/replaced/missing policy, target drift и расширенные source/after отказывают до нового effect',()=>{
 const f=fixture();f.state.gateDenied=true;assert.throws(f.step,/TARGET_DRIFT/);assert.equal(f.state.effects.length,0);
 for(const mutate of [x=>x.resources.find(r=>r.kind==='ConfigMap'&&r.metadata.name===x.projection.policy.metadata.name).data['policy.json']+=' ',
  x=>x.plan.patches.at(-1).after[0].verbs.push('create'),x=>x.plan.patches[0].after.egress.push({ports:[{port:443}]}),
  x=>x.input.installation.metadata.uid=uid(999),x=>x.stored.get(x.key('ConfigMap','kodex-platform-endpoints')).data.publicOrigin='https://foreign.example.invalid']){
  const x=fixture();mutate(x);assert.throws(x.step);assert.equal(x.state.effects.length,0);
 }
 const foreign=fixture();foreign.stored.set(foreign.key('ConfigMap',foreign.plan.policy.metadata.name),{...structuredClone(foreign.plan.policy),metadata:{...foreign.plan.policy.metadata,uid:uid(950)}});assert.throws(foreign.step,/FOREIGN/);assert.equal(foreign.state.effects.length,0);
 const replaced=fixture();replaced.step();replaced.stored.get(replaced.key('ConfigMap',replaced.plan.policy.metadata.name)).metadata.uid=uid(999);assert.throws(replaced.step,/FOREIGN/);
 const missing=fixture();missing.step();missing.stored.delete(missing.key('ConfigMap',missing.plan.policy.metadata.name));assert.throws(missing.step,/MISSING_AFTER_INTENT/);
});

test('UNKNOWN до patch требует отдельный BEFORE inspect, read-only status не выдаёт staging PASS',()=>{
 const f=fixture();f.state.lostBefore='egress-gateway-exact-runtime-paths';assert.throws(f.step,/BEFORE_EFFECT/);
 f.state.lostBefore='';assert.throws(f.step,/OUTCOME_UNKNOWN/);
 assert.throws(()=>applyTelegramRuntimeInputs({plan:f.plan,journal:f.journal,kube:f.kube,readOnly:true}),/NOT_STAGED/);
 assert.equal(applyTelegramRuntimeInputs({plan:f.plan,journal:f.journal,kube:f.kube,readOnly:true,requireStaged:false}).state,'INPUTS_PARTIAL');
 assert.equal(f.step().state,'INPUTS_STAGED');
 f.stored.get(f.key('ConfigMap',f.plan.policy.metadata.name)).binaryData={injected:'YQ=='};
 assert.throws(f.step,/FOREIGN/);
});

test('immutable policy create ACK UID не заменяется новым UID между ACK и первым GET',()=>{
 const f=fixture();let acknowledgementUID;
 const kube=(args,input)=>{const raw=f.kube(args,input);if(args[0]==='create'&&!args.includes('--dry-run=server')){
  const ack=JSON.parse(raw);acknowledgementUID=ack.metadata.uid;
  f.stored.get(f.key('ConfigMap',f.plan.policy.metadata.name)).metadata.uid=uid(990);
 }return raw;};
 const step=()=>applyTelegramRuntimeInputs({plan:f.plan,journal:f.journal,kube,gate:()=>{}});
 assert.throws(step,/POLICY_REPLACED_OR_FOREIGN/);assert.equal(f.state.effects.length,1);
 assert.equal(f.journal.events.find(e=>e.type==='TELEGRAM_INPUT_CREATE_ACK').uid,acknowledgementUID);
 assert.throws(step,/POLICY_REPLACED_OR_FOREIGN/);assert.equal(f.state.effects.length,1);
});

test('invalid successful immutable policy ACK не превращается в lostACK с adoption последующим GET',()=>{
 for(const mode of ['missing-uid','expanded-body']){
  const f=fixture();const kube=(args,input)=>{const raw=f.kube(args,input);if(args[0]!=='create'||args.includes('--dry-run=server'))return raw;
   const ack=JSON.parse(raw);if(mode==='missing-uid')delete ack.metadata.uid;else ack.binaryData={injected:'YQ=='};return JSON.stringify(ack);};
  const step=()=>applyTelegramRuntimeInputs({plan:f.plan,journal:f.journal,kube,gate:()=>{}});
  assert.throws(step,/POLICY_ACK_INVALID/);assert.equal(f.state.effects.length,1);assert.throws(step,/POLICY_ACK_INVALID/);assert.equal(f.state.effects.length,1);
 }
});
