#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { readFileSync, writeFileSync, openSync, writeSync, fsyncSync, closeSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fingerprint } from './scoped-release.mjs';

const namespace='kodex-system';
const targets={attestor:{name:'internal-rpc-authority-readback-attestor',port:8443},database:{name:'kodex-postgresql',port:5432}};
function requireValue(value,code){if(!value)throw new Error(code);}
export function selectorMatches(selector,labels) {
 requireValue(selector&&typeof selector==='object'&&Object.keys(selector).every(key=>['matchLabels','matchExpressions'].includes(key)),'UNKNOWN_SELECTOR');
 if(Object.entries(selector.matchLabels??{}).some(([key,value])=>labels[key]!==value))return false;
 return (selector.matchExpressions??[]).every(({key,operator,values})=>{
  if(operator==='In')return Array.isArray(values)&&values.includes(labels[key]);
  if(operator==='NotIn')return Array.isArray(values)&&!values.includes(labels[key]);
  if(operator==='Exists')return Object.hasOwn(labels,key);
  if(operator==='DoesNotExist')return !Object.hasOwn(labels,key);
  throw new Error('UNKNOWN_SELECTOR_OPERATOR');
 });
}
const policySet=policies=>policies.map(p=>({name:p.metadata.name,uid:p.metadata.uid,specSHA256:fingerprint(p.spec)})).sort((a,b)=>a.name.localeCompare(b.name));
export function planOutage(policies,pod,ns,mode) {
 requireValue(Object.hasOwn(targets,mode)&&pod.metadata.namespace===namespace&&ns.metadata.name===namespace&&ns.metadata.labels?.['kodex.dev/environment']==='staging'&&
   pod.metadata.labels?.['app.kubernetes.io/name']==='control-api-gateway'&&pod.metadata.labels?.['kodex.dev/internal-rpc-authority-issuer']==='enabled','EXACT_STAGING_API_REQUIRED');
 const target=targets[mode],changes=[];
 let selected=0;
 for(const policy of policies) {
  if(!selectorMatches(policy.spec.podSelector,pod.metadata.labels)||!policy.spec.policyTypes?.includes('Egress'))continue;
  selected++;
  const next=structuredClone(policy.spec),egress=[];
  for(const rule of next.egress??[]) {
   const relevant=!rule.ports?.length||rule.ports.some(port=>{
    requireValue(typeof port.port==='number','NAMED_EGRESS_PORT_UNSUPPORTED');
    return (port.protocol??'TCP')==='TCP'&&port.port<=target.port&&(port.endPort??port.port)>=target.port;
   });
   if(!relevant){egress.push(rule);continue;}
   requireValue(rule.to?.length>0,'UNBOUNDED_EGRESS_ALLOW');
   const peers=[];
   for(const peer of rule.to) {
    requireValue(!peer.ipBlock,'IPBLOCK_EGRESS_REQUIRES_SEPARATE_PROOF');
    if(peer.namespaceSelector&&!selectorMatches(peer.namespaceSelector,ns.metadata.labels)){peers.push(peer);continue;}
    const name=peer.podSelector?.matchLabels?.['app.kubernetes.io/name'];
    requireValue(typeof name==='string','BROAD_DESTINATION_ALLOW');
    if(name!==target.name){peers.push(peer);continue;}
    // Удаляется точный peer из всех additive allow. Пустой to нельзя оставить:
    // Kubernetes трактует его как разрешение всем адресам.
   }
   if(peers.length)egress.push({...rule,to:peers});
  }
  if(Object.hasOwn(next,'egress'))next.egress=egress;
  if(fingerprint(next)!==fingerprint(policy.spec))requireValue(policy.spec.podSelector?.matchLabels?.['app.kubernetes.io/name']==='control-api-gateway','SHARED_EGRESS_OWNER_REQUIRES_SEPARATE_PLAN');
  if(fingerprint(next)!==fingerprint(policy.spec))changes.push({name:policy.metadata.name,uid:policy.metadata.uid,resourceVersion:policy.metadata.resourceVersion,before:policy.spec,after:next});
 }
 requireValue(selected>0&&changes.length>0,'EXACT_DESTINATION_ALLOW_MISSING');
 return {version:1,intent:randomUUID(),mode,namespaceUID:ns.metadata.uid,pod:{name:pod.metadata.name,uid:pod.metadata.uid,labelsSHA256:fingerprint(pod.metadata.labels)},policySet:policySet(policies),changes};
}
export function policyPatch(current,change,direction) {
 const before=direction==='restore'?change.after:change.before,after=direction==='restore'?change.before:change.after;
 requireValue(current.metadata.uid===change.uid&&fingerprint(current.spec)===fingerprint(before),'POLICY_DRIFT');
 return [{op:'test',path:'/metadata/uid',value:change.uid},{op:'test',path:'/metadata/resourceVersion',value:current.metadata.resourceVersion},{op:'test',path:'/spec',value:before},{op:'replace',path:'/spec',value:after}];
}
async function main(args) {
 const command=args.shift(),options={};requireValue(['plan','apply','restore','observe'].includes(command),'INVALID_COMMAND');
 while(args.length){const key=args.shift();requireValue(['--context','--pod','--mode','--output','--plan','--evidence','--confirm','--k3s-sudo','--hold-seconds'].includes(key)&&!options[key],'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift();}
 requireValue(options['--context']&&!/prod/i.test(options['--context']),'EXACT_STAGING_CONTEXT_REQUIRED');
 const kube=(...args)=>execFileSync(options['--k3s-sudo']?'sudo':'kubectl',options['--k3s-sudo']?['-n','k3s','kubectl','--context',options['--context'],...args]:['--context',options['--context'],...args],{encoding:'utf8',stdio:'pipe',timeout:15_000,maxBuffer:16<<20}).trim();
 const get=(...args)=>JSON.parse(kube('get',...args,'-o','json'));
 requireValue(kube('config','current-context')===options['--context'],'CONTEXT_MISMATCH');
 const ns=get('namespace',namespace);requireValue(ns.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_NAMESPACE_REQUIRED');
 if(command==='plan') {const pod=get('pod',options['--pod'],'-n',namespace),policies=get('networkpolicies','-n',namespace).items;
  const plan={...planOutage(policies,pod,ns,options['--mode']),context:options['--context']};
  writeFileSync(options['--output'],JSON.stringify(plan,null,2)+'\n',{flag:'wx',mode:0o600});process.stdout.write(`Authority outage plan: ${fingerprint(plan)}\n`);return;}
 const plan=JSON.parse(readFileSync(options['--plan'],'utf8'));
 requireValue(plan.version===1&&plan.context===options['--context']&&plan.namespaceUID===ns.metadata.uid&&Object.hasOwn(targets,plan.mode)&&plan.changes.length>0,'OUTAGE_PLAN_MISMATCH');
 const states=()=>plan.changes.map(change=>{const current=get('networkpolicy',change.name,'-n',namespace);requireValue(current.metadata.uid===change.uid,'POLICY_REPLACED');return {name:change.name,state:fingerprint(current.spec)===fingerprint(change.before)?'RESTORED':fingerprint(current.spec)===fingerprint(change.after)?'PARTITION_POLICY':'DRIFT'};});
 if(command==='observe'){process.stdout.write(JSON.stringify({intent:plan.intent,states:states()})+'\n');return;}
 requireValue(options['--confirm']===(command==='apply'?'APPLY-STAGING-AUTHORITY-OUTAGE':'RESTORE-STAGING-AUTHORITY-OUTAGE'),'STAGING_CONFIRMATION_REQUIRED');
 const hold=Number(options['--hold-seconds']??40);requireValue(Number.isInteger(hold)&&hold>=31&&hold<=60,'BOUNDED_OUTAGE_REQUIRED');
 const fd=openSync(options['--evidence'],'wx',0o600);
 const evidence=record=>{writeSync(fd,JSON.stringify({at:new Date().toISOString(),intent:plan.intent,...record})+'\n');fsyncSync(fd);};
 const restore=()=>{
  for(const change of [...plan.changes].reverse()){
   const current=get('networkpolicy',change.name,'-n',namespace);
   if(current.metadata.uid===change.uid&&fingerprint(current.spec)===fingerprint(change.before))continue;
   const patch=policyPatch(current,change,'restore');evidence({status:'INTENT',operation:'RESTORE',policy:change.name});
   try{kube('patch','networkpolicy',change.name,'-n',namespace,'--type=json','-p',JSON.stringify(patch));}
   catch{evidence({status:'UNKNOWN',operation:'RESTORE',policy:change.name});}
   const readback=get('networkpolicy',change.name,'-n',namespace);requireValue(readback.metadata.uid===change.uid&&fingerprint(readback.spec)===fingerprint(change.before),'RESTORE_READBACK_FAILED');
   evidence({status:'RESTORED',policy:change.name});
  }
 };
 try {
  if(command==='restore'){restore();return;}
  const pod=get('pod',plan.pod.name,'-n',namespace);
  requireValue(pod.metadata.uid===plan.pod.uid&&fingerprint(pod.metadata.labels)===plan.pod.labelsSHA256,'CONSUMER_REPLACED');
  requireValue(fingerprint(policySet(get('networkpolicies','-n',namespace).items))===fingerprint(plan.policySet),'POLICY_SET_DRIFT');
  evidence({status:'INTENT',operation:'PARTITION',planSHA256:fingerprint(plan)});
  try {
   for(const change of plan.changes){const current=get('networkpolicy',change.name,'-n',namespace),patch=policyPatch(current,change,'apply');
    evidence({status:'INTENT',operation:'PATCH',policy:change.name});
    try{kube('patch','networkpolicy',change.name,'-n',namespace,'--type=json','-p',JSON.stringify(patch));}catch{evidence({status:'UNKNOWN',operation:'PATCH',policy:change.name});}
    const readback=get('networkpolicy',change.name,'-n',namespace);requireValue(readback.metadata.uid===change.uid&&fingerprint(readback.spec)===fingerprint(change.after),'PARTITION_READBACK_FAILED');
   }
   evidence({status:'PARTITION_POLICY',holdSeconds:hold});
   // Отсутствие сетевого пути и неизменность receipt доказывает параллельный
   // working-path probe. Изменённая policy сама по себе не даёт PASS outage.
   await new Promise(done=>setTimeout(done,hold*1000));
  } finally {restore();}
  evidence({status:'RESTORED',states:states()});
 } finally {closeSync(fd);}
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Authority outage failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED'}\n`);process.exitCode=1;});
