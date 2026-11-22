import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,rmSync,readFileSync,mkdirSync,writeFileSync,chmodSync,lstatSync,existsSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {join,dirname} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {readArtifactRetentionIssuerInputs,stagePersonalIntegrationsControlPlaneSpec} from './artifact-retention-issuer-inputs.mjs';
import {componentCohortFixture,replaceComponentLock} from './fixtures/component-cohort-synthetic.mjs';
import {planComponentMigrationHandoff} from './portable-component-migration.mjs';
import {planComponentAPIMaintenance} from './component-api-maintenance.mjs';
import {planComponentAPITraffic} from './component-api-traffic.mjs';
import {readPersonalIntegrationsAPIAdmission} from './personal-integrations-api-admission.mjs';
import {telegramPolicySource,telegramRegistrySource} from './authority-telegram-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';
import producerMetadata from './fixtures/telegram-producer-metadata.cjs';
import {telegramIntroductionResources as producerResources} from './telegram-introduction-inventory.mjs';
import {artifactRetentionIssuerAssetRequests,artifactRetentionIssuerAssetSteps,requireArtifactRetentionAssetsUnclaimed,
  issuerAssetsResourceScope,artifactRetentionRuntimeEgressScope,issuerAssetsJournalHeader,validateIssuerAssetsJournal,executeArtifactRetentionIssuerAssets,issuerAssetsIO,
  readArtifactRetentionIssuerAssetsReady,readArtifactRetentionIssuerAssetResources,mainIssuerAssets,planArtifactRetentionIssuerAssets} from './artifact-retention-issuer-assets.mjs';

const uid=n=>'20000000-0000-4000-8000-'+String(n).padStart(12,'0');
const owner='app.kubernetes.io/part-of',marker='kodex.dev/artifact-retention-issuer-assets';
const names=['issuer-key','issuer-readback-credential','issuer-readback-possession','issuer-restore-ack',
  'issuer-restore-credential','manifest-trust','proof-trust'].map(suffix=>'internal-rpc-authority-artifact-retention-'+suffix);
const runtimeName='artifact-retention-exact-runtime-paths';
const cpEgress={to:[{podSelector:{matchLabels:{'app.kubernetes.io/name':'control-plane'}}}],ports:[{protocol:'TCP',port:8443}]};
const policies=['control-plane-exact-runtime-paths','internal-rpc-authority-readback-attestor-exact-paths',
  'internal-rpc-authority-restore-controller-exact-paths'];
const resource=(kind,name,id,fields)=>({apiVersion:kind==='Role'?'rbac.authorization.k8s.io/v1':kind==='NetworkPolicy'?'networking.k8s.io/v1':
  kind==='Certificate'?'cert-manager.io/v1':'apps/v1',kind,
  metadata:{name,namespace:'kodex-system',uid:uid(id),resourceVersion:'7',generation:1,labels:{[owner]:'kodex'}},...fields});

function tlsRow(certificate,overrides={}){
  const value={uid:uid(707),name:certificate.spec.secretName,namespace:'kodex-system',type:'kubernetes.io/tls',
    certificate:certificate.metadata.name,group:'cert-manager.io',kind:certificate.spec.issuerRef.kind,nameIssuer:certificate.spec.issuerRef.name,
    dns:(certificate.spec.dnsNames??[]).join(','),uris:(certificate.spec.uris??[]).join(','),keys:'ca.crt,tls.crt,tls.key,',crt:'1',key:'1',owners:'',state:'live',...overrides};
  value.group=value.group===null?'':'='+value.group;
  return Object.values(value).join('\t');
}

function fixture(t){
  const directory=mkdtempSync(join(tmpdir(),'issuer-assets-'));t.after(()=>rmSync(directory,{recursive:true,force:true}));
  const old=resource('Deployment','artifact-retention',1,{spec:{replicas:0,template:{metadata:{labels:{}},spec:{serviceAccountName:'artifact-retention',
    automountServiceAccountToken:false,containers:[{name:'artifact-retention',env:[]}],volumes:[]}}},
    status:{observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0}});
  const peer={podSelector:{matchLabels:{'app.kubernetes.io/name':'artifact-retention','app.kubernetes.io/component':'retention-job'}}};
  const expression=values=>({podSelector:{matchExpressions:[{key:'app.kubernetes.io/name',operator:'In',values}]}});
  const network=(name,index,added)=>resource('NetworkPolicy',name,10+index,{spec:{podSelector:{matchLabels:{fixture:name}},
    policyTypes:['Ingress','Egress'],ingress:[{from:index?[expression(added?['session-archive','artifact-retention']:['session-archive'])]:
      added?[{podSelector:{matchLabels:{'app.kubernetes.io/name':'session-archive'}}},peer]:[{podSelector:{matchLabels:{'app.kubernetes.io/name':'session-archive'}}}],
      ports:[{protocol:'TCP',port:8443}]}],egress:[{to:[{podSelector:{matchLabels:{fixture:'egress'}}}]}]}});
  const inputs={sourceSHA:'a'.repeat(40),issuerSecretNames:names,
    deployment:resource('Deployment','artifact-retention',2,{spec:{template:{spec:{volumes:[
      {name:'new',secret:{secretName:'internal-rpc-authority-artifact-retention-issuer-postgresql'}},
      {name:'shared',secret:{secretName:'internal-rpc-authority-bootstrap-roots'}}]}}}}),
    certificate:resource('Certificate','internal-rpc-authority-artifact-retention-workload',3,{spec:{
      secretName:'internal-rpc-authority-artifact-retention-workload-tls',issuerRef:{kind:'Issuer',name:'kodex-installation-ca'},
      privateKey:{algorithm:'ECDSA',rotationPolicy:'Always',size:256},
      uris:['spiffe://kodex.local/ns/kodex-system/sa/artifact-retention'],dnsNames:['artifact-retention'],usages:['server auth','client auth']}}),
    issuerNetworkPolicy:network('artifact-retention-internal-rpc-authority-exact-paths',3,true),
    ingressPolicies:policies.map((name,index)=>network(name,index,true)),
    runtimeNetworkPolicy:resource('NetworkPolicy',runtimeName,30,{spec:{podSelector:peer.podSelector,policyTypes:['Ingress','Egress'],
      ingress:[{from:[{podSelector:{matchLabels:{fixture:'metrics'}}}],ports:[{protocol:'TCP',port:9090}]}],
      egress:[{to:[{podSelector:{matchLabels:{fixture:'storage'}}}],ports:[{protocol:'TCP',port:8333}]},cpEgress]}})};
  const runtime=structuredClone(inputs.runtimeNetworkPolicy);runtime.spec.egress.pop();
  const resources=[resource('Role','internal-rpc-authority-publisher',20,{rules:[{apiGroups:[''],resources:['secrets'],verbs:['get','update'],resourceNames:['old-projection']}]}),
    ...policies.map((name,index)=>network(name,index,false)),runtime];
  const pins=requireArtifactRetentionAssetsUnclaimed(old,{items:[]},inputs);
  const plan={schema:'kodex.artifact-retention-issuer-assets/v2',intentID:uid(99),evidencePath:join(directory,'journal.jsonl'),
    source:{path:directory,revision:'a'.repeat(40)},target:{context:'fixture'},
    admission:Object.fromEntries(['evidencePath','planPath','trafficEvidencePath','trafficPlanPath','migrationPlanPath','migrationEvidencePath'].map(name=>[name,join(directory,name)])),apiAdmissionSHA256:'b'.repeat(64),
    inputsSHA256:fingerprint(inputs),workload:pins,steps:artifactRetentionIssuerAssetSteps(inputs,resources,uid(99))};
  plan.stoppedAdmission={schema:'kodex.personal-integrations-stopped-admission/v1',sourceSHA:plan.source.revision,apiAdmissionSHA256:plan.apiAdmissionSHA256};
  const state={resources:new Map(resources.map(value=>[value.kind+'/'+value.metadata.name,structuredClone(value)])),mutations:[],
    gateCalls:0,ready:true,loseACK:null,absentAfterUnknown:false,invalidACK:false,beforeCalls:0};
  const io={gate(){state.gateCalls++;},get:step=>state.resources.get(step.kind+'/'+step.name)??null,
    beforeMutation(){state.beforeCalls++;},
    mutate(step){
      state.mutations.push(step.id);
      let actual;
      if(step.action==='create'){
        actual=structuredClone(step.body);actual.metadata.uid=uid(100+step.id);
        if(step.kind==='Secret'){delete actual.data;actual.dataCount='0';actual.stringDataCount='0';actual.mutability='mutable';}
      }else{
        actual=structuredClone(state.resources.get(step.kind+'/'+step.name));actual[step.scope.field]=structuredClone(step.scope.after);
        actual.metadata.annotations={...actual.metadata.annotations,[marker]:plan.intentID};
      }
      if(!(state.absentAfterUnknown&&state.loseACK===step.id))state.resources.set(step.kind+'/'+step.name,actual);
      if(state.loseACK===step.id)throw Error('ISSUER_ASSETS_ACK_UNKNOWN');
      return state.invalidACK?uid(999):actual.metadata.uid;
    },
    certificateReady(){assert(state.ready,'certificate not ready');return {uid:uid(107),ready:true,tlsSecretUID:uid(707),tlsMetadataSHA256:'d'.repeat(64)};},
  };
  const journal=()=>privateJournal(plan.evidencePath,issuerAssetsJournalHeader(plan));
  const run=async(phase='apply')=>{const j=journal();try{return await executeArtifactRetentionIssuerAssets(phase,plan,j,io);}finally{j.close();}};
  return {directory,old,inputs,resources,plan,state,io,journal,run};
}

test('План содержит ровно7 пустых generation0 Secret, Certificate/NetworkPolicy и5 адресных CAS',t=>{
  const f=fixture(t),requests=artifactRetentionIssuerAssetRequests(f.inputs,f.plan.intentID);
  assert.equal(requests.length,9);assert.deepEqual(requests.slice(0,7).map(value=>value.metadata.name),names);
  for(const body of requests.slice(0,7)){
    assert.equal(body.type,'Opaque');assert.deepEqual(body.data,{});assert.equal(body.stringData,undefined);
    assert.equal(body.metadata.annotations['kodex.dev/secret-generation'],'0');
    assert.equal(body.metadata.labels['app.kubernetes.io/managed-by'],'internal-rpc-authority-publisher');
  }
  assert.deepEqual(f.plan.steps.map(step=>step.kind),[...Array(7).fill('Secret'),'Certificate','NetworkPolicy','Role',...Array(4).fill('NetworkPolicy')]);
  for(const step of f.plan.steps.slice(9)){
    assert.equal(step.body[0].path,'/metadata/uid');assert.equal(step.body[1].path,'/metadata/resourceVersion');
    assert.deepEqual(step.body[2].value,step.scope.before[step.scope.field]);
  }
  assert.deepEqual(f.plan.steps[9].scope.after[0].verbs,['get','update']);
  assert.throws(()=>artifactRetentionIssuerAssetRequests({...f.inputs,issuerSecretNames:[...names.slice(0,6),'foreign']},f.plan.intentID),/SOURCE_REJECTED/);
  const foreign=structuredClone(f.inputs);foreign.certificate.apiVersion='foreign.invalid/v1';
  assert.throws(()=>artifactRetentionIssuerAssetRequests(foreign,f.plan.intentID),/SOURCE_REJECTED/);
});

test('Повторный apply/status подтверждает те же14 UID без повторного эффекта',async t=>{
  const f=fixture(t);const ready=await f.run();assert.equal(ready.status,'PASS');assert.equal(ready.resources.length,14);
  assert.equal((await f.run('status')).planSHA256,ready.planSHA256);assert.equal((await f.run()).status,'PASS');
  assert.deepEqual(f.state.mutations,Array.from({length:14},(_,index)=>index));
  assert(f.state.gateCalls>20);assert(!readFileSync(f.plan.evidencePath,'utf8').includes('dataCount'));
});

test('ART egress добавляет только source CP8443; все соседние правила и GVK/UID закреплены',t=>{
  const f=fixture(t),step=f.plan.steps[13],before=f.resources.at(-1);
  assert.equal(step.action,'patch');assert.deepEqual(step.target,{apiVersion:'networking.k8s.io/v1',kind:'NetworkPolicy',
    namespace:'kodex-system',name:runtimeName,uid:before.metadata.uid});
  assert.deepEqual(step.scope.after,{...before.spec,egress:[...before.spec.egress,cpEgress]});
  assert.deepEqual(f.plan.steps.map(step=>({apiVersion:step.action==='create'?step.body.apiVersion:step.target.apiVersion,
    namespace:'kodex-system',kind:step.kind,name:step.name})),issuerAssetsResourceScope);
  for(const mutate of [r=>r.metadata.uid='foreign',r=>r.metadata.resourceVersion='0',r=>r.apiVersion='foreign/v1',
    r=>r.metadata.namespace='foreign',r=>r.metadata.labels[owner]='foreign',r=>r.metadata.deletionTimestamp='now',
    r=>r.spec.podSelector.matchLabels['app.kubernetes.io/name']='foreign',r=>r.spec.policyTypes=['Egress'],
    r=>r.spec.egress.push(cpEgress,cpEgress)]){
    const resources=structuredClone(f.resources);mutate(resources.at(-1));
    assert.throws(()=>artifactRetentionIssuerAssetSteps(f.inputs,resources,f.plan.intentID),/EGRESS_OWNER_REJECTED|SOURCE_REJECTED/);
  }
  const inputs=structuredClone(f.inputs);inputs.runtimeNetworkPolicy.spec.egress.at(-1).ports[0].port=443;
  assert.throws(()=>artifactRetentionRuntimeEgressScope(inputs,f.resources),/EGRESS_SOURCE_REJECTED/);
  const resources=structuredClone(f.resources);resources.at(-1).metadata.annotations={[marker]:uid(888)};
  assert.throws(()=>artifactRetentionIssuerAssetSteps(f.inputs,resources,f.plan.intentID),/FOREIGN_MANAGEMENT/);
});

test('Exact existing egress остаётся read-only no-op без смены чужого marker; drift закрывает status',async t=>{
  const f=fixture(t),runtime=f.resources.at(-1);runtime.spec.egress.push(structuredClone(cpEgress));
  runtime.metadata.annotations={[marker]:uid(888),'fixture/retained':'unchanged'};
  f.state.resources.set('NetworkPolicy/'+runtimeName,structuredClone(runtime));
  f.plan.steps=artifactRetentionIssuerAssetSteps(f.inputs,f.resources,f.plan.intentID);
  assert.equal(f.plan.steps[13].action,'observe');await f.run();await f.run('status');
  assert.deepEqual(f.state.mutations,Array.from({length:13},(_,i)=>i));
  assert.deepEqual(f.state.resources.get('NetworkPolicy/'+runtimeName),runtime);
  const rows=readFileSync(f.plan.evidencePath,'utf8').trim().split('\n').map(JSON.parse);
  assert.equal(rows.filter(row=>row.id===13&&row.type==='NOOP_VERIFIED').length,1);
  assert.equal(rows.filter(row=>row.id===13&&['INTENT','ACK'].includes(row.type)).length,0);
  f.state.resources.get('NetworkPolicy/'+runtimeName).metadata.uid=uid(999);
  await assert.rejects(f.run('status'),/RESOURCE_CHANGED/);
});

test('No-op повторяет exact read после gate; отсутствие прежнего PATCH ACK не становится no-op',async t=>{
  const f=fixture(t);f.state.loseACK=13;f.state.absentAfterUnknown=true;
  await assert.rejects(f.run(),/ACK_UNKNOWN/);
  for(const phase of ['apply','status'])await assert.rejects(f.run(phase),/RESOURCE_CHANGED/);
  assert.equal(f.state.mutations.filter(id=>id===13).length,1);
  const g=fixture(t),runtime=g.resources.at(-1);runtime.spec.egress.push(structuredClone(cpEgress));
  g.state.resources.set('NetworkPolicy/'+runtimeName,structuredClone(runtime));
  g.plan.steps=artifactRetentionIssuerAssetSteps(g.inputs,g.resources,g.plan.intentID);
  const originalGate=g.io.gate;g.io.gate=()=>{originalGate();if(g.state.mutations.length===13)
    g.state.resources.get('NetworkPolicy/'+runtimeName).spec.egress.pop();};
  await assert.rejects(g.run(),/NOOP_CHANGED/);
  assert(!readFileSync(g.plan.evidencePath,'utf8').includes('NOOP_VERIFIED'));
});

test('Статус до первого intent не записывает ресурсы и не объявляет PASS',async t=>{
  const f=fixture(t);assert.equal((await f.run('status')).status,'NOT_STARTED');assert.deepEqual(f.state.mutations,[]);
});

for(const id of [0,7,9,12,13])test(`Потерянный ACK шага${id}: сначала exact GET, ни одной повторной mutation`,async t=>{
  const f=fixture(t);f.state.loseACK=id;await assert.rejects(f.run(),/ACK_UNKNOWN/);
  const after=f.state.mutations.length;const status=await f.run('status');
  assert.equal(status.status,id===13?'PASS':'NOT_STARTED');assert.equal(f.state.mutations.length,after);
  f.state.loseACK=null;assert.equal((await f.run()).status,'PASS');assert.equal(f.state.mutations.filter(value=>value===id).length,1);
});

test('Отсутствие после неизвестного ACK не разрешает новую create',async t=>{
  const f=fixture(t);f.state.loseACK=0;f.state.absentAfterUnknown=true;await assert.rejects(f.run(),/ACK_UNKNOWN/);
  for(const phase of ['apply','status'])await assert.rejects(f.run(phase),/RESOURCE_CHANGED/);
  assert.deepEqual(f.state.mutations,[0]);
});

test('UID ACK устойчив до GET: чужая подтверждённая incarnation никогда не принимается',async t=>{
  const f=fixture(t);f.state.invalidACK=true;await assert.rejects(f.run(),/RESOURCE_CHANGED/);
  assert(readFileSync(f.plan.evidencePath,'utf8').includes('"type":"ACK"'));
  await assert.rejects(f.run('status'),/RESOURCE_CHANGED/);assert.deepEqual(f.state.mutations,[0]);
});

test('Изменённые Secret payload/generation, UID, CAS field и owner marker закрывают readback',async t=>{
  for(const mutate of [f=>f.state.resources.get('Certificate/internal-rpc-authority-artifact-retention-workload').apiVersion='foreign.invalid/v1',
    f=>f.state.resources.get('Secret/'+names[0]).dataCount='1',
    f=>f.state.resources.get('Secret/'+names[0]).metadata.annotations['kodex.dev/secret-generation']='1',
    f=>f.state.resources.get('Secret/'+names[0]).metadata.uid=uid(999),
    f=>f.state.resources.get('Role/internal-rpc-authority-publisher').rules[0].verbs.push('create'),
    f=>delete f.state.resources.get('NetworkPolicy/'+policies[0]).metadata.annotations[marker]]){
    const f=fixture(t);await f.run();mutate(f);await assert.rejects(f.run('status'),/SECRET_NOT_EMPTY|RESOURCE_CHANGED|PATCH_CHANGED/);
    assert.equal(f.state.mutations.length,14);
  }
});

test('Изменение RV до CAS закрывает mutation; ошибка gate не создаёт intent',async t=>{
  const f=fixture(t);f.state.resources.get('Role/internal-rpc-authority-publisher').metadata.resourceVersion='8';
  await assert.rejects(f.run(),/PREIMAGE_CHANGED/);assert.equal(f.state.mutations.length,9);
  const g=fixture(t);g.io.gate=()=>{throw Error('gate closed');};await assert.rejects(g.run(),/gate closed/);assert.equal(g.state.mutations.length,0);
});

test('Старый ART UID допускается только без новых ссылок, с явным stopped/join и тем же spec',t=>{
  const f=fixture(t);assert.equal(f.plan.workload.uid,f.old.metadata.uid);
  assert(f.plan.workload.introducedSecrets.includes('internal-rpc-authority-artifact-retention-issuer-postgresql'));
  assert(!f.plan.workload.introducedSecrets.includes('internal-rpc-authority-bootstrap-roots'));
  const pod={kind:'Pod',metadata:{uid:uid(50),ownerReferences:[{kind:'ReplicaSet',uid:uid(51),controller:true}]},spec:{containers:[{name:'artifact-retention'}]},status:{phase:'Running'}};
  const set={kind:'ReplicaSet',metadata:{uid:uid(51),ownerReferences:[{kind:'Deployment',uid:f.old.metadata.uid,controller:true}]}};
  assert.throws(()=>requireArtifactRetentionAssetsUnclaimed(f.old,{items:[set,pod]},f.inputs),/WORKLOAD_CLAIMED/);
  for(const mutate of [d=>d.spec.replicas=1,d=>d.metadata.uid=uid(999),d=>d.spec.template.spec.volumes.push({name:'secret',secret:{secretName:names[0]}}),
    d=>d.spec.template.spec.volumes.push({name:'issuer-db',secret:{secretName:'internal-rpc-authority-artifact-retention-issuer-postgresql'}}),
    d=>d.spec.template.spec.containers.push({name:'internal-rpc-authority-issuer'}),d=>d.spec.template.spec.containers[0].env.push({name:'INTERNAL_RPC_AUTHORITY_ISSUER_SOCKET',value:'/run/socket'}),
    d=>d.status.observedGeneration=0]){
    const changed=structuredClone(f.old);mutate(changed);assert.throws(()=>requireArtifactRetentionAssetsUnclaimed(changed,{items:[]},f.inputs,f.plan.workload),/WORKLOAD_/);
  }
});

test('Journal не принимает reorder/duplicate/чужой plan и readiness не принимает незавершённые события',async t=>{
  const f=fixture(t);await f.run();const j=f.journal();let events;try{events=structuredClone(j.events);}finally{j.close();}
  for(const mutate of [e=>e.push(e[1]),e=>e[1].id=12,e=>e[0].planSHA256='c'.repeat(64),e=>e[2].uid='bad']){
    const changed=structuredClone(events);mutate(changed);assert.throws(()=>validateIssuerAssetsJournal(changed,f.plan),/JOURNAL_REJECTED/);
  }
  await assert.rejects(readArtifactRetentionIssuerAssetsReady({plan:f.plan,events:events.slice(0,2)},()=>{throw Error('must not call');}),/INCOMPLETE/);
});

test('Production resource IO выводит metadata Secret/UID ACK, CAS читает private regular file через actual child',t=>{
  const f=fixture(t),calls=[];
  const kube=(args,input)=>{
    calls.push({args,input});
    if(args.includes('patch')){
      const path=args.includes('--patch-file')?args[args.indexOf('--patch-file')+1]:args.find(value=>value.startsWith('--patch-file=')).slice('--patch-file='.length);
      let raw;try{raw=execFileSync('/bin/cat',[path],{input,encoding:'utf8',timeout:2000,stdio:['pipe','pipe','pipe']});}
      catch(error){if(/no such device or address/i.test(error.stderr??''))process.stderr.write('SYNTHETIC_STDIN_REOPEN_ENXIO\n');throw error;}
      const info=lstatSync(path);assert(info.isFile()&&!info.isSymbolicLink()&&info.nlink===1&&info.uid===process.getuid());
      assert.equal(info.mode&0o777,0o600);assert.equal(lstatSync(dirname(path)).mode&0o777,0o700);
      const id=args.includes(runtimeName)?13:9;assert.equal(raw,JSON.stringify(f.plan.steps[id].body));calls.at(-1).patchPath=path;
    }
    if(args.includes('create')||args.includes('patch'))return uid(100);
    if(args.includes('secret'))return [uid(100),names[0],'kodex-system','Opaque','kodex','internal-rpc-authority-publisher','0',f.plan.intentID,'0','0','mutable','live'].join('\t');
    throw Error('unexpected');
  };
  const io=issuerAssetsIO(f.plan,kube);assert.equal(io.get(f.plan.steps[0]).dataCount,'0');
  assert.equal(io.mutate(f.plan.steps[0]),uid(100));assert.equal(io.mutate(f.plan.steps[9]),uid(100));
  assert.equal(io.mutate(f.plan.steps[13]),uid(100));assert.equal(calls[3].args[3],'networkpolicies.networking.k8s.io');
  assert.equal(calls[3].args[4],runtimeName);assert.equal(existsSync(calls[3].patchPath),false);
  assert(calls[0].args.some(arg=>arg.startsWith('go-template=')));assert(!calls[0].args.includes('json'));
  assert(calls.slice(1).every(call=>call.args.includes('jsonpath={.metadata.uid}')));
  assert(calls[2].args.includes('--patch-file'));assert.equal(calls[2].input,undefined);assert.equal(existsSync(calls[2].patchPath),false);
  assert.equal(calls[0].args.filter(arg=>arg.includes('.data')).length,1);
});

test('Node socketpair stdin нельзя reopen; existing private patch удаляется также после неизвестного ACK',t=>{
  assert.throws(()=>execFileSync('/bin/cat',['/dev/stdin'],{input:'public-fixture',encoding:'utf8',timeout:2000,stdio:['pipe','pipe','pipe']}),
    error=>error.status===1&&/No such device or address|no such device or address/.test(error.stderr));
  const f=fixture(t);let path;
  const io=issuerAssetsIO(f.plan,args=>{path=args[args.indexOf('--patch-file')+1];assert(lstatSync(path).isFile());throw Error('synthetic lost ACK');});
  assert.throws(()=>io.mutate(f.plan.steps[9]),/ACK_UNKNOWN/);assert.equal(existsSync(path),false);
});

test('Production before-Certificate gate отвергает чужой существующий TLS Secret, не читая material',t=>{
  const f=fixture(t);let calls=0;
  const io=issuerAssetsIO(f.plan,args=>{calls++;assert(args.includes('secret'));return [uid(100),f.inputs.certificate.spec.secretName,
    'kodex-system','kubernetes.io/tls','kodex','cert-manager','<no value>','<no value>','2','0','mutable','live'].join('\t');});
  assert.throws(()=>io.beforeMutation(f.plan.steps[7]),/TLS_SECRET_EXISTS/);assert.equal(calls,1);
});

test('CLI отвергает неизвестный scope/phase/boolean и отсутствующее подтверждение',async()=>{
  for(const args of [['up'],['apply','--ready','true'],['status','--context','fixture','--kubeconfig']])
    await assert.rejects(mainIssuerAssets(args),/ARGUMENT_REJECTED/);
});

function checkedSource(f){
  const source=join(f.directory,'source');
  for(const directory of [source,join(source,'tools'),join(source,'tools/install')]){
    mkdirSync(directory);
    chmodSync(directory,0o755);
  }
  const refs=['internal-rpc-authority-artifact-retention-issuer-key','artifact-retention-issuer-readback-credential',
    'artifact-retention-issuer-readback-key','artifact-retention-issuer-restore-ack','artifact-retention-issuer-restore-credential',
    'internal-rpc-authority-artifact-retention-manifest-trust','internal-rpc-authority-artifact-retention-proof-trust'];
  const projection=join(source,'tools/install/secret-projections.json');
  writeFileSync(projection,JSON.stringify({secrets:names.map((name,index)=>({name,dynamic:true,
    items:[{source:{type:'authority',ref:refs[index]}}]}))}));
  chmodSync(projection,0o644);
  const git=(...args)=>execFileSync('git',['-C',source,...args],{encoding:'utf8',timeout:10000,stdio:['ignore','pipe','pipe']}).trim();
  git('init','--quiet');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');git('add','.');
  git('-c','user.name=Synthetic','-c','user.email=synthetic@example.invalid','commit','--quiet','-m','Синтетические публичные исходники');
  const revision=git('rev-parse','HEAD'),role=structuredClone(f.resources[0]);role.rules[0].resourceNames.push(...names);
  const cp=resource('Deployment','control-plane',800,{spec:stagePersonalIntegrationsControlPlaneSpec({replicas:0,template:{spec:{
    serviceAccountName:'control-plane',containers:[{name:'control-plane',env:[],volumeMounts:[
      {name:'application-grants',mountPath:'/var/run/config/kodex/control-plane/application-grants',readOnly:true}]}],
    volumes:[{name:'application-grants',secret:{secretName:'control-plane-application-grants',defaultMode:292}}],
  }}})});
  const resources=[f.inputs.deployment,f.inputs.certificate,f.inputs.issuerNetworkPolicy,f.inputs.runtimeNetworkPolicy,...f.inputs.ingressPolicies,role,cp];
  const kube=args=>{assert.equal(args[0],'kustomize');return resources.map(value=>JSON.stringify(value)).join('\n---\n');};
  return {source:{path:source,revision},kube,resources};
}

test('Actual checked-source reader проверяет все7 manifest projections и не принимает изменённый SHA',t=>{
  const f=fixture(t),{source,kube}=checkedSource(f);
  const inputs=readArtifactRetentionIssuerInputs(source,kube);
  assert.deepEqual(inputs.issuerSecretNames,names);assert.equal(inputs.sourceSHA,source.revision);
  assert.throws(()=>readArtifactRetentionIssuerInputs({...source,revision:'f'.repeat(40)},kube),/SOURCE_CHANGED/);
  f.inputs.certificate.spec.uris=['spiffe://foreign.invalid/issuer'];
  assert.throws(()=>readArtifactRetentionIssuerInputs(source,kube),/CERTIFICATE_SOURCE_CHANGED/);
  chmodSync(source.path,0o700);
  assert.throws(()=>readArtifactRetentionIssuerInputs(source,kube),/SOURCE_RUNTIME_ACCESS_REQUIRED/);
});

test('Certificate readback принимает только закрытые defaults и Ready exact generation',t=>{
  const f=fixture(t),certificate=structuredClone(f.plan.steps[7].body);
  certificate.metadata.uid=uid(107);certificate.metadata.generation=2;
  certificate.spec.issuerRef.group='cert-manager.io';certificate.spec.privateKey.encoding='PKCS1';certificate.spec.revisionHistoryLimit=1;
  certificate.status={conditions:[{type:'Ready',status:'True',observedGeneration:2}]};
  let tls={};
  const io=issuerAssetsIO(f.plan,args=>args.includes('certificate')?JSON.stringify(certificate):tlsRow(certificate,tls));
  assert.equal(io.certificateReady().ready,true);
  for(const mutate of [c=>c.status.conditions[0].observedGeneration=1,c=>delete c.metadata.generation,
    c=>c.status.conditions.push({type:'Ready',status:'False',observedGeneration:2}),c=>c.spec.privateKey.size=384,
    c=>c.spec.issuerRef.group='foreign.invalid',c=>c.spec.revisionHistoryLimit=100]){
    const saved=structuredClone(certificate);mutate(certificate);assert.throws(()=>io.certificateReady(),/CERTIFICATE_NOT_READY|CREATE_CHANGED/);
    Object.keys(certificate).forEach(key=>delete certificate[key]);Object.assign(certificate,saved);
  }
  for(const invalid of [{type:'Opaque'},{certificate:'foreign'},{group:'foreign'},{kind:'ClusterIssuer'},{nameIssuer:'foreign'},
    {dns:'foreign.invalid'},{uris:'spiffe://foreign.invalid/role'},{keys:'ca.crt,tls.crt,'},{key:'0'},
    {owners:['cert-manager.io/v1','Certificate',certificate.metadata.name,uid(999),'true'].join('|')+';'},{state:'deleting'}]){
    tls=invalid;assert.throws(()=>io.certificateReady(),/TLS_CUSTODY_REJECTED/);
  }
  tls={owners:['cert-manager.io/v1','Certificate',certificate.metadata.name,certificate.metadata.uid,'true'].join('|')+';'};
  assert.equal(io.certificateReady().ready,true);
  tls={};const custody=io.certificateReady();tls={uid:uid(999)};
  assert.throws(()=>io.certificateReady(certificate.metadata.uid,custody),/TLS_CUSTODY_REJECTED/);
  assert.throws(()=>issuerAssetsIO(f.plan,args=>args.includes('certificate')?JSON.stringify(certificate):'').certificateReady(),/TLS_CUSTODY_REJECTED/);
});

test('TLS custody принимает штатную пустую группу cert-manager, подтверждая наличие аннотации',t=>{
  const f=fixture(t),certificate=structuredClone(f.plan.steps[7].body);
  certificate.metadata.uid=uid(107);certificate.metadata.generation=1;
  certificate.status={conditions:[{type:'Ready',status:'True',observedGeneration:1}]};
  let group='';
  const io=issuerAssetsIO(f.plan,args=>args.includes('certificate')?JSON.stringify(certificate):tlsRow(certificate,{group}));
  const empty=io.certificateReady(),fields=tlsRow(certificate,{group}).split('\t');
  fields[5]='';
  assert.equal(empty.tlsMetadataSHA256,fingerprint(fields));
  group='cert-manager.io';
  const explicit=io.certificateReady();
  fields[5]=group;
  assert.equal(explicit.tlsMetadataSHA256,fingerprint(fields));
  assert.throws(()=>io.certificateReady(certificate.metadata.uid,empty),/TLS_CUSTODY_REJECTED/);
  for(const invalid of [null,'foreign.invalid','=']){
    group=invalid;
    assert.throws(()=>io.certificateReady(),/TLS_CUSTODY_REJECTED/);
  }
});

// Production plan/gate/resource factory остаётся настоящей. Только fixed owner
// inspect получает synthetic049 receipt; Kubernetes transport — конечная map.
function factoryFixture(t){
  const f=fixture(t),selected=checkedSource(f),source=selected.source,cohort=componentCohortFixture('personal-integrations');
  for(const part of cohort.value.components){
    const lock=JSON.parse(Buffer.from(part.componentLockRawBase64,'base64'));lock.source_sha=source.revision;
    replaceComponentLock(part,lock);part.nodeProof.sourceSHA=source.revision;
  }
  const cp=cohort.deployments.find(value=>value.metadata.name==='control-plane');
  cp.spec.template.spec.containers[0].env.push({name:'CONTROL_PLANE_OIDC_ISSUER',value:'https://sso.example.invalid/realms/kodex'});
  const old=cohort.deployments.find(value=>value.metadata.name==='artifact-retention');
  const oldImage=old.spec.template.spec.containers[0].image;
  old.spec=structuredClone(f.old.spec);old.spec.replicas=2;old.spec.template.spec.containers[0].image=oldImage;
  const api=cohort.deployments.find(value=>value.metadata.name==='control-api-gateway');api.spec.replicas=3;
  api.status={observedGeneration:1,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3};
  const labels=cp.metadata.labels;
  const service={apiVersion:'v1',kind:'Service',metadata:{name:'control-api-gateway',namespace:'kodex-system',uid:uid(400),resourceVersion:'1',labels},
    spec:{type:'ClusterIP',clusterIP:'192.0.2.4',selector:{'app.kubernetes.io/name':'control-api-gateway'},ports:[{port:8443,targetPort:'https'}]}};
  const registry=resource('ConfigMap','internal-rpc-authority-publisher-target-registry',401,{});registry.apiVersion='v1';registry.metadata.labels=labels;
  registry.data={'authority-policy.json':renderAuthorityPolicyEnvironment(telegramPolicySource('desired').raw,
    {oidcIssuer:'https://sso.example.invalid/realms/kodex',oidcAudience:'kodex-control-api'}).raw,
    'key-delivery-targets.yaml':telegramRegistrySource('desired').raw};
  const publisher=resource('Deployment','internal-rpc-authority-publisher',402,{spec:{replicas:1}});publisher.metadata.labels=labels;
  const target={...cohort.target,resources:[...cohort.target.resources,...[service,registry,publisher].map(value=>({
    apiVersion:value.apiVersion,kind:value.kind,name:value.metadata.name,uid:value.metadata.uid}))]};
  const apiTarget={...cohort.target,resources:target.resources.filter(value=>value.name==='control-api-gateway')};
  const observed={...cohort.observed,resources:{items:[]}};
  const closed={plan:planComponentAPIMaintenance(api,apiTarget,observed,source.revision,'close')};
  closed.events=[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(closed.plan),maintenanceID:closed.plan.maintenanceID},
    {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'CLOSED',deploymentUID:api.metadata.uid,maintenanceID:closed.plan.maintenanceID}];
  api.spec=structuredClone(closed.plan.afterSpec);api.metadata.annotations=structuredClone(closed.plan.afterAnnotations);
  api.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
  const traffic=planComponentAPITraffic({service,deployment:api,resources:{items:[]},target:apiTarget,observed,sourceSHA:source.revision,action:'isolate',closed});
  service.spec=structuredClone(traffic.afterSpec);service.metadata.annotations=structuredClone(traffic.afterAnnotations);
  const trafficEvents=[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(traffic),maintenanceID:traffic.maintenanceID},
    {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'ISOLATED',serviceUID:service.metadata.uid,maintenanceID:traffic.maintenanceID}];
  const admission=f.plan.admission;
  const save=(path,value,rows=false)=>writeFileSync(path,rows?value.map(row=>JSON.stringify(row)+'\n').join(''):JSON.stringify(value),{mode:0o600});
  save(admission.planPath,closed.plan);save(admission.evidencePath,closed.events,true);save(admission.trafficPlanPath,traffic);save(admission.trafficEvidencePath,trafficEvents,true);
  const map=new Map(cohort.deployments.map(value=>['deployment/'+value.metadata.name,value]));
  map.set('service/control-api-gateway',service);map.set('configmap/internal-rpc-authority-publisher-target-registry',registry);
  map.set('deployment/internal-rpc-authority-publisher',publisher);map.set('configmap/kodex-installation-profile',cohort.observed.installation);
  map.set('namespace/kube-system',{metadata:{uid:target.clusterUID}});map.set('namespace/kodex-system',{metadata:{uid:target.namespaceUID}});
  for(const value of f.resources)map.set(value.kind.toLowerCase()+'/'+value.metadata.name,value);
  const state={mutations:[],lostACK:null,scopeClaimed:false,calls:0};
  const kube=(args,input)=>{
    state.calls++;
    if(args[0]==='kustomize')return selected.kube(args);
    if(args.includes('replicasets,pods,jobs'))return '';
    if(args.includes(producerResources))return state.scopeClaimed?producerMetadata([state.scopeClaimed==='serviceAccount'?
      {kind:'CronJob',metadata:{name:'foreign',namespace:'kodex-system',uid:uid(900)},spec:{jobTemplate:{spec:{template:{spec:{serviceAccountName:'artifact-retention'}}}}}}:
      {kind:'Pod',metadata:{name:'foreign',namespace:'kodex-system',uid:uid(900)},
        spec:{serviceAccountName:'foreign',volumes:[{name:'new',csi:{driver:'synthetic.example.invalid',
          nodePublishSecretRef:{name:'internal-rpc-authority-artifact-retention-issuer-postgresql'}}}]}}],producerResources):'';
    if(args.includes('endpointslices')||args.includes('pods'))return JSON.stringify({items:[]});
    if(args[0]==='create'||args[2]==='patch'){
      const body=JSON.parse(args[0]==='create'?input:readFileSync(args[args.indexOf('--patch-file')+1],'utf8'));let value;
      if(args[0]==='create'){
        value=structuredClone(body);value.metadata.uid=uid(500+state.mutations.length);value.metadata.generation=1;value.metadata.resourceVersion='1';
        if(value.kind==='Certificate'){
          value.status={conditions:[{type:'Ready',status:'True',observedGeneration:1}]};
          map.set('secret/'+value.spec.secretName,{tlsCertificate:structuredClone(value),tlsMetadata:{}});
        }
      }else{
        value=structuredClone(map.get((args[3]==='networkpolicies.networking.k8s.io'?'networkpolicy':args[3])+'/'+args[4]));
        for(const operation of body){
          if(operation.op==='test'){
            const actual=operation.path.startsWith('/metadata/')?value.metadata[operation.path.split('/')[2]]:value[operation.path.slice(1)];
            assert.deepEqual(actual,operation.value);
          }else if(operation.path==='/metadata/annotations')value.metadata.annotations=operation.value;
          else value[operation.path.slice(1)]=operation.value;
        }
      }
      map.set(value.kind.toLowerCase()+'/'+value.metadata.name,value);state.mutations.push(value.kind+'/'+value.metadata.name);
      if(state.lostACK===state.mutations.length)throw Error('synthetic lost ACK');return value.metadata.uid;
    }
    assert.equal(args[2],'get');const value=map.get((args[3]==='networkpolicies.networking.k8s.io'?'networkpolicy':args[3])+'/'+args[4]);
    if(args[3]==='secret'&&value?.tlsCertificate)return tlsRow(value.tlsCertificate,value.tlsMetadata);
    if(args[3]==='secret')return value?[value.metadata.uid,value.metadata.name,'kodex-system',value.type,'kodex','internal-rpc-authority-publisher',
      value.metadata.annotations['kodex.dev/secret-generation'],value.metadata.annotations[marker],String(Object.keys(value.data??{}).length),'0','mutable','live'].join('\t'):'';
    return value?JSON.stringify(value):'';
  };
  const apiMaintenance=readPersonalIntegrationsAPIAdmission({profile:'personal-integrations',closedPlanPath:admission.planPath,closedEvidencePath:admission.evidencePath,
    isolationPlanPath:admission.trafficPlanPath,isolationEvidencePath:admission.trafficEvidencePath},kube,{stopped:true}).admission;
  const handoff=planComponentMigrationHandoff(cohort.value,cohort.deployments,source);
  for(const part of handoff.components){const value=map.get('deployment/'+part.name);value.spec=structuredClone(part.stoppedSpec);
    value.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};}
  const migration={version:2,kind:'CONTROL_PLANE_MIGRATION',id:'synthetic-assets049',profile:'personal-integrations',migrationVersion:'20261114170000',source,
    context:target.context,kubeconfig:join(f.directory,'synthetic-kubeconfig'),portableTarget:cohort.target,apiMaintenance,
    postconditionSQLSHA256:'c'.repeat(64),componentHandoff:handoff};
  const inspection={status:'PASS',migrationVersion:'20261114170000',sourceRevision:source.revision,schemaPostcondition:'PASS',cpStage:'STOPPED',podsGone:true,
    cpReady:false,pairReady:false,jobUID:'synthetic-assets049-job',componentPair:handoff.components.map(part=>({name:part.name,stage:'stoppedSpec',joined:true,ready:false}))};
  save(admission.migrationPlanPath,migration);save(admission.migrationEvidencePath,[{type:'HEADER',kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(migration)},
    {type:'STOP_ACK'},{type:'READBACK',...inspection}],true);
  return {f,source,target,admission,kube,state,map,inspection,io:{inspect:()=>inspection}};
}

test('Actual source/API049/cohort factory проходит plan→14 effects→fresh status и resource-only staged sub-reader',async t=>{
  const g=factoryFixture(t);
  const plan=await planArtifactRetentionIssuerAssets({source:g.source,target:g.target,admission:g.admission,evidencePath:g.f.plan.evidencePath,intentID:g.f.plan.intentID},g.kube,g.io);
  assert.equal(plan.stoppedAdmission.cohort049.components.length,9);
  const j=privateJournal(plan.evidencePath,issuerAssetsJournalHeader(plan));
  try{
    const io=issuerAssetsIO(plan,g.kube,g.io);assert.equal((await executeArtifactRetentionIssuerAssets('apply',plan,j,io)).status,'PASS');
    assert.equal((await executeArtifactRetentionIssuerAssets('status',plan,j,io)).status,'PASS');assert.equal(g.state.mutations.length,14);
    const proof=readArtifactRetentionIssuerAssetResources({plan,events:j.events},g.kube);
    assert.equal(proof.purpose,'PRE_ROTATION');assert.equal(proof.status,undefined);assert.equal(proof.resources.length,14);
    const tlsKey='secret/'+g.f.inputs.certificate.spec.secretName,tls=g.map.get(tlsKey);g.map.delete(tlsKey);
    assert.throws(()=>readArtifactRetentionIssuerAssetResources({plan,events:j.events},g.kube),/TLS_CUSTODY_REJECTED/);
    g.map.set(tlsKey,tls);tls.tlsMetadata.uid=uid(999);
    assert.throws(()=>readArtifactRetentionIssuerAssetResources({plan,events:j.events},g.kube),/TLS_CUSTODY_REJECTED/);
    delete tls.tlsMetadata.uid;
    const art=g.map.get('deployment/artifact-retention');art.spec.template.spec.containers.push({name:'internal-rpc-authority-issuer'});
    assert.equal(readArtifactRetentionIssuerAssetResources({plan,events:j.events},g.kube).purpose,'PRE_ROTATION');
    await assert.rejects(executeArtifactRetentionIssuerAssets('status',plan,j,io),/WORKLOAD_CLAIMED/);
    art.spec.template.spec.containers.pop();
    g.state.scopeClaimed=true;await assert.rejects(executeArtifactRetentionIssuerAssets('status',plan,j,io),/CREDENTIAL_SCOPE_CLAIMED/);
    g.state.scopeClaimed='serviceAccount';await assert.rejects(executeArtifactRetentionIssuerAssets('status',plan,j,io),/CREDENTIAL_SCOPE_CLAIMED/);
    assert.equal(g.state.mutations.length,14);
  }finally{j.close();}
});

test('После изменения чужого Job требуется пара одинаковых полных снимков перед intent',async t=>{
  const g=factoryFixture(t),plan=await planArtifactRetentionIssuerAssets({source:g.source,target:g.target,admission:g.admission,
    evidencePath:g.f.plan.evidencePath,intentID:g.f.plan.intentID},g.kube,g.io);
  const snapshot=id=>producerMetadata([{kind:'Job',metadata:{name:`scheduled-${id}`,namespace:'kodex-system',uid:uid(900+id)}}],producerResources);
  let reads=0;
  const kube=(args,input)=>args.includes(producerResources)?snapshot([1,2,3,3][Math.min(reads++,3)]):g.kube(args,input);
  const journal=privateJournal(plan.evidencePath,issuerAssetsJournalHeader(plan));
  try{
    const result=await executeArtifactRetentionIssuerAssets('apply',plan,journal,issuerAssetsIO(plan,kube,g.io));
    assert.equal(result.status,'PASS');
    assert.equal(g.state.mutations.length,14);
    assert.ok(reads>4);
  }finally{journal.close();}
});

test('Нестабильный inventory и новый secret claim при повторном чтении закрывают effect',async t=>{
  const g=factoryFixture(t),plan=await planArtifactRetentionIssuerAssets({source:g.source,target:g.target,admission:g.admission,
    evidencePath:g.f.plan.evidencePath,intentID:g.f.plan.intentID},g.kube,g.io);
  const snapshot=id=>producerMetadata([{kind:'Job',metadata:{name:`scheduled-${id}`,namespace:'kodex-system',uid:uid(900+id)}}],producerResources);
  let reads=0,foreignClaim=false;
  const kube=(args,input)=>{
    if(!args.includes(producerResources))return g.kube(args,input);
    reads++;
    if(foreignClaim&&reads===3){
      g.state.scopeClaimed=true;
      return g.kube(args,input);
    }
    return snapshot(reads);
  };
  const journal=privateJournal(plan.evidencePath,issuerAssetsJournalHeader(plan));
  try{
    const apply=()=>executeArtifactRetentionIssuerAssets('apply',plan,journal,issuerAssetsIO(plan,kube,g.io));
    await assert.rejects(apply(),/ISSUER_ASSETS_DISCOVERY_CHANGED/);
    assert.equal(reads,4);
    foreignClaim=true;
    reads=0;
    await assert.rejects(apply(),/ISSUER_ASSETS_CREDENTIAL_SCOPE_CLAIMED/);
    assert.equal(reads,3);
    assert.deepEqual(journal.events.map(event=>event.type),['HEADER']);
    assert.equal(g.state.mutations.length,0);
  }finally{journal.close();}
});

test('Actual factory lost Role ACK: restart сверяет прежний UID; другой namespace закрыт до следующего effect',async t=>{
  const g=factoryFixture(t),plan=await planArtifactRetentionIssuerAssets({source:g.source,target:g.target,admission:g.admission,
    evidencePath:g.f.plan.evidencePath,intentID:g.f.plan.intentID},g.kube,g.io);
  g.state.lostACK=10;
  let j=privateJournal(plan.evidencePath,issuerAssetsJournalHeader(plan));
  try{await assert.rejects(executeArtifactRetentionIssuerAssets('apply',plan,j,issuerAssetsIO(plan,g.kube,g.io)),/ACK_UNKNOWN/);}finally{j.close();}
  assert.equal(g.state.mutations.length,10);
  j=privateJournal(plan.evidencePath,issuerAssetsJournalHeader(plan));
  try{
    assert.equal((await executeArtifactRetentionIssuerAssets('status',plan,j,issuerAssetsIO(plan,g.kube,g.io))).status,'NOT_STARTED');
    assert.equal(g.state.mutations.length,10);
    g.map.get('namespace/kodex-system').metadata.uid=uid(999);
    await assert.rejects(executeArtifactRetentionIssuerAssets('apply',plan,j,issuerAssetsIO(plan,g.kube,g.io)),/TARGET_REJECTED/);
    assert.equal(g.state.mutations.length,10);
  }finally{j.close();}
});
