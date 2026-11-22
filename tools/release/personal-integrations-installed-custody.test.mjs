import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {artifactRetentionIssuerAssetSteps,issuerAssetsJournalHeader} from './artifact-retention-issuer-assets.mjs';
import {readPersonalIntegrationsInstalledCustody} from './personal-integrations-installed-custody.mjs';

const uid=n=>'20000000-0000-4000-8000-'+String(n).padStart(12,'0');
const marker='kodex.dev/artifact-retention-issuer-assets',owner='app.kubernetes.io/part-of';
const names=['issuer-key','issuer-readback-credential','issuer-readback-possession','issuer-restore-ack','issuer-restore-credential','manifest-trust','proof-trust'].map(n=>'internal-rpc-authority-artifact-retention-'+n);
const policies=['control-plane-exact-runtime-paths','internal-rpc-authority-readback-attestor-exact-paths','internal-rpc-authority-restore-controller-exact-paths'];
const resource=(kind,name,n,body)=>({apiVersion:{Role:'rbac.authorization.k8s.io/v1',NetworkPolicy:'networking.k8s.io/v1',Certificate:'cert-manager.io/v1'}[kind]??'v1',kind,
 metadata:{name,namespace:'kodex-system',uid:uid(n),resourceVersion:'1',labels:{[owner]:'kodex'}},...body});

// Только исходные intent и metadata API; private Secret values отсутствуют даже
// в fixture. Dynamic generation разрешён как наблюдение, никогда как Ready.
function fixture({runtimeAlreadyReady=false,issuerGroup='cert-manager.io'}={}){
 const peer={podSelector:{matchLabels:{'app.kubernetes.io/name':'artifact-retention','app.kubernetes.io/component':'retention-job'}}};
 const from=(n,added)=>n?[{podSelector:{matchExpressions:[{key:'app.kubernetes.io/name',operator:'In',values:added?['session-archive','artifact-retention']:['session-archive']}]}}]:
  [{podSelector:{matchLabels:{'app.kubernetes.io/name':'session-archive'}}},...(added?[peer]:[])];
 const network=(name,n,added)=>resource('NetworkPolicy',name,20+n,{spec:{podSelector:{matchLabels:{fixture:name}},policyTypes:['Ingress','Egress'],
  ingress:[{from:from(n,added),ports:[{protocol:'TCP',port:8443}]}],egress:[]}});
 const inputs={sourceSHA:'a'.repeat(40),issuerSecretNames:names,
  deployment:{spec:{template:{spec:{}}}},certificate:resource('Certificate','internal-rpc-authority-artifact-retention-workload',1,{spec:{
   secretName:'internal-rpc-authority-artifact-retention-workload-tls',issuerRef:{kind:'Issuer',name:'kodex-installation-ca'},
   uris:['spiffe://kodex.local/ns/kodex-system/sa/artifact-retention']}}),
  issuerNetworkPolicy:network('artifact-retention-internal-rpc-authority-exact-paths',3,true),ingressPolicies:policies.map((name,n)=>network(name,n,true))};
 const runtime=added=>resource('NetworkPolicy','artifact-retention-exact-runtime-paths',29,{spec:{podSelector:peer.podSelector,policyTypes:['Ingress','Egress'],ingress:[],egress:[{to:[{podSelector:{matchLabels:{fixture:'preserved'}}}],ports:[{protocol:'TCP',port:443}]},...(added?[{to:[{podSelector:{matchLabels:{'app.kubernetes.io/name':'control-plane'}}}],ports:[{protocol:'TCP',port:8443}]}]:[])]}});
 inputs.runtimeNetworkPolicy=runtime(true);
 const before=[resource('Role','internal-rpc-authority-publisher',10,{rules:[{apiGroups:[''],resources:['secrets'],verbs:['get','update'],resourceNames:['old-projection']}]}),
  ...policies.map((name,n)=>network(name,n,false)),runtime(runtimeAlreadyReady)];
 const target={context:'synthetic',clusterUID:uid(50),namespaceUID:uid(51),installationUID:uid(52),installationSHA256:'e'.repeat(64)};
 const plan={schema:'kodex.artifact-retention-issuer-assets/v2',source:{path:'/synthetic/source',revision:inputs.sourceSHA},target,intentID:uid(60),
  evidencePath:'/synthetic/private/events',apiAdmissionSHA256:'b'.repeat(64),inputsSHA256:fingerprint(inputs),workload:{uid:uid(61)},
  admission:Object.fromEntries(['planPath','evidencePath','trafficPlanPath','trafficEvidencePath','migrationPlanPath','migrationEvidencePath'].map(key=>[key,'/synthetic/private/'+key])),
  steps:artifactRetentionIssuerAssetSteps(inputs,before,uid(60)),stoppedAdmission:{schema:'kodex.personal-integrations-stopped-admission/v1',sourceSHA:inputs.sourceSHA,apiAdmissionSHA256:'b'.repeat(64)}};
 const events=[{type:'HEADER',...issuerAssetsJournalHeader(plan),at:new Date().toISOString()}],map=new Map(),keys=new Map();
 for(const step of plan.steps){
  const actual=step.action==='create'?structuredClone(step.body):{...structuredClone(step.scope.before),[step.scope.field]:structuredClone(step.scope.after)};
  actual.metadata.uid=uid(100+step.id);if(step.action!=='create')actual.metadata.uid=step.scope.before.metadata.uid;
  if(step.action!=='observe')actual.metadata.annotations={...actual.metadata.annotations,[marker]:plan.intentID};
  if(step.kind==='Secret'){
   delete actual.data;actual.dataCount='0';actual.stringDataCount='0';actual.mutability='mutable';keys.set(step.name,[]);
  }
  if(step.kind==='Certificate'){actual.metadata.generation=1;actual.status={conditions:[{type:'Ready',status:'True',observedGeneration:1}]};}
  map.set(step.name,actual);
  for(const row of step.action==='observe'?[{type:'NOOP_VERIFIED',id:step.id,uid:actual.metadata.uid,bodySHA256:step.bodySHA256}]:[{type:'INTENT',id:step.id,bodySHA256:step.bodySHA256},{type:'ACK',id:step.id,uid:actual.metadata.uid},{type:'VERIFIED',id:step.id,uid:actual.metadata.uid}])events.push({...row,at:new Date().toISOString()});
 }
 const certificate=map.get(inputs.certificate.metadata.name);
 const tls=[uid(707),certificate.spec.secretName,'kodex-system','kubernetes.io/tls',certificate.metadata.name,issuerGroup,'Issuer','kodex-installation-ca','',certificate.spec.uris.join(','),
  'ca.crt,tls.crt,tls.key,','1','1','','live'];
 events.push({type:'TLS_VERIFIED',certificateUID:certificate.metadata.uid,tlsSecretUID:uid(707),tlsMetadataSHA256:fingerprint(tls),at:new Date().toISOString()});
 const calls=[];
 const kube=args=>{
  calls.push(args);assert.equal(args[2],'get');
  const name=args[4],value=map.get(name),format=args.at(-1);
  if(args[3]!=='secret')return JSON.stringify(value);
  assert.ok(format.startsWith('go-template='));
  if(name===certificate.spec.secretName)return tls.map((field,index)=>index===5?(field===null?'':'='+field):field).join('\t');
  if(format.includes('{{range $key'))return [value.metadata.uid,value.metadata.annotations['kodex.dev/secret-generation'],keys.get(name).join(',')+(keys.get(name).length?',':''),'live'].join('\t');
  return [value.metadata.uid,name,'kodex-system','Opaque',value.metadata.labels[owner],value.metadata.labels['app.kubernetes.io/managed-by'],
   value.metadata.annotations['kodex.dev/secret-generation'],plan.intentID,value.dataCount,'0',value.mutability,'live'].join('\t');
 };
 const io={readInputs:()=>inputs,readProjections:()=>({secrets:names.map(name=>({name,dynamic:true,items:[{key:'allowed'}]}))})};
 const read=()=>readPersonalIntegrationsInstalledCustody({plan,events,sourceSHA:inputs.sourceSHA,target},kube,io);
 return {plan,events,map,keys,tls,inputs,calls,read};
}

test('late custody accepts empty/filled projections, preserving7 UID and TLS, without Ready or snapshot claims',()=>{
 const f=fixture(),empty=f.read();assert.equal(empty.purpose,'CUSTODY_ONLY');assert.equal(Object.hasOwn(empty,'status'),false);
 const secret=f.map.get(names[0]);secret.metadata.annotations['kodex.dev/secret-generation']='5';secret.dataCount='2';f.keys.set(names[0],['_generation','allowed']);
 const filled=f.read();assert.equal(filled.dynamic[0].generation,'5');assert.deepEqual(filled.resources,empty.resources);
 assert.equal(Object.hasOwn(filled,'snapshotRevision'),false);assert.ok(f.calls.every(args=>!args.includes('create')&&!args.includes('patch')));
});

test('new incarnation, foreign manager/key/GVK и stale TLS Secret rejected after publication',()=>{
 for(const mutate of [f=>f.map.get(names[0]).metadata.uid=uid(999),f=>f.map.get(names[0]).metadata.labels['app.kubernetes.io/managed-by']='foreign',
  f=>{const s=f.map.get(names[0]);s.metadata.annotations['kodex.dev/secret-generation']='1';s.dataCount='2';f.keys.set(names[0],['_generation','private_foreign']);},
  f=>{const s=f.map.get(names[0]);s.dataCount='1';f.keys.set(names[0],['allowed']);},f=>f.tls[0]=uid(999),
  f=>f.map.get('internal-rpc-authority-publisher').apiVersion='v1',f=>f.map.get('artifact-retention-exact-runtime-paths').spec.egress.pop()]){
  const f=fixture();mutate(f);assert.throws(f.read);
 }
});

test('late custody rejects legacy13/v1 even with original receipts',()=>{const f=fixture();f.plan.schema='kodex.artifact-retention-issuer-assets/v1';assert.throws(f.read);});

test('Late TLS custody сохраняет принятый digest для пустой штатной группы',()=>{
 const f=fixture({issuerGroup:''});
 assert.equal(f.read().certificate.tlsMetadataSHA256,fingerprint(f.tls));
 f.tls[5]=null;assert.throws(f.read,/TLS_CUSTODY_REJECTED/);
 f.tls[5]='cert-manager.io';assert.throws(f.read,/TLS_CUSTODY_REJECTED/);
});


test('v2 observe/NOOP_VERIFIED сохраняет14-й egress UID без нового marker или PATCH',()=>{
 const f=fixture({runtimeAlreadyReady:true});assert.equal(f.plan.steps[13].action,'observe');
 assert.equal(f.events.filter(row=>row.type==='NOOP_VERIFIED').length,1);
 const result=f.read();assert.equal(result.resources.length,14);assert.equal(result.resources[13].action,'observe');
 assert.ok(f.calls.every(args=>!args.includes('patch')));
});
