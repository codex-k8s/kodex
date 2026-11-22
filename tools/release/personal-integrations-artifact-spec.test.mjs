import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {personalIntegrationsArtifactAPISpec,personalIntegrationsArtifactSpecMatches} from './personal-integrations-artifact-spec.mjs';
import {stageArtifactRetentionIssuerSpec} from './artifact-retention-issuer-inputs.mjs';
import {fingerprint} from './scoped-release.mjs';

// Только локальный canonical render, без Kubernetes API/данных Secret.
const render=execFileSync('kustomize',['build',new URL('../../deploy/k8s/profiles/web-only',import.meta.url).pathname],{encoding:'utf8',timeout:30000,maxBuffer:16<<20});
const deployment=JSON.parse(execFileSync('yq',['-o=json','select(.kind == "Deployment" and .metadata.name == "artifact-retention")','-'],{input:render,encoding:'utf8',timeout:10000,maxBuffer:1<<20}));
const authorityImage='registry.example.invalid/kodex/internal-rpc-authority@sha256:'+'b'.repeat(64);
const componentImage='registry.example.invalid/kodex/artifact-retention@sha256:'+'c'.repeat(64);
const beforeSpec={replicas:0,template:{metadata:{labels:{'app.kubernetes.io/name':'artifact-retention'}},spec:{serviceAccountName:'artifact-retention',automountServiceAccountToken:false,
 securityContext:{fsGroup:10001,fsGroupChangePolicy:'OnRootMismatch'},
 containers:[{name:'artifact-retention',image:componentImage,env:[],volumeMounts:[]}],volumes:[]}}};
const staged=stageArtifactRetentionIssuerSpec(beforeSpec,{deployment},componentImage,authorityImage);
const input={profile:'personal-integrations-backup',name:'artifact-retention',beforeSpec,expectedSpec:staged,authorityImage};

// Независимая явная фиксация API additions установленного render. Planned policy и
// fingerprint не меняются; original exact comparator обязан отказать.
function observedDefaults(spec){
 const actual=structuredClone(spec),pod=actual.template.spec;
 const issuer=pod.containers.find(c=>c.name==='internal-rpc-authority-issuer'),grant=pod.containers.find(c=>c.name==='platform-worker-grant-agent'),init=pod.initContainers[0];
 for(const c of [issuer,grant,init])Object.assign(c,{imagePullPolicy:'IfNotPresent',terminationMessagePath:'/dev/termination-log',terminationMessagePolicy:'File'});
 init.resources={};
 for(const c of [issuer,grant]){
  for(const e of c.env)if(e.valueFrom?.fieldRef)e.valueFrom.fieldRef.apiVersion='v1';
  for(const name of ['readinessProbe','startupProbe','livenessProbe'])if(c[name]){
   c[name].httpGet.scheme='HTTP';c[name].successThreshold=1;
   if(!Object.hasOwn(c[name],'failureThreshold'))c[name].failureThreshold=3;
  }
 }
 return actual;
}
function additions(a,b){
 let count=0;
 for(const [key,value] of Object.entries(b)){
  if(!Object.hasOwn(a,key))count++;
  else if(value&&typeof value==='object'&&a[key]&&typeof a[key]==='object')count+=additions(a[key],value);
  else assert.deepEqual(a[key],value);
 }
 return count;
}
test('049 ART: canonical render сохраняет defaults и exact explicit variant25 без изменения planned bytes',()=>{
 const hash=fingerprint(staged),actual=observedDefaults(staged);
 assert.equal(additions(staged,actual),26);
 const pinnedVariant=structuredClone(staged);pinnedVariant.template.spec.containers[1].env.find(e=>e.valueFrom?.fieldRef).valueFrom.fieldRef.apiVersion='v1';
 const pinnedActual=observedDefaults(pinnedVariant);assert.equal(additions(pinnedVariant,pinnedActual),25);
 assert.deepEqual(personalIntegrationsArtifactAPISpec({...input,expectedSpec:pinnedVariant}),pinnedActual);
 assert.notEqual(fingerprint(actual),hash); // Прежний exact-spec path отвергал принятый PATCH.
 assert.deepEqual(personalIntegrationsArtifactAPISpec(input),actual);
 for(const replicas of [0,2])assert.equal(personalIntegrationsArtifactSpecMatches({...input,expectedSpec:{...staged,replicas}},{...actual,replicas}),true);
 assert.equal(fingerprint(staged),hash);assert.equal(personalIntegrationsArtifactSpecMatches(input,staged),true);
});
test('049 ART: explicit поля сохраняются, неожиданные additions/values/main/UID scope не нормализуются',()=>{
 const explicit=structuredClone(staged),c=explicit.template.spec.containers[1];
 Object.assign(c,{imagePullPolicy:'Always',terminationMessagePath:'/custom/log'});
 c.readinessProbe.successThreshold=2;
 const expected=personalIntegrationsArtifactAPISpec({...input,expectedSpec:explicit});
 assert.equal(expected.template.spec.containers[1].imagePullPolicy,'Always');assert.equal(expected.template.spec.containers[1].readinessProbe.successThreshold,2);
 for(const mutate of [v=>v.template.spec.containers[1].imagePullPolicy='Always',v=>v.template.spec.containers[1].readinessProbe.httpGet.scheme='HTTPS',
  v=>v.template.spec.containers[1].startupProbe.failureThreshold=3,v=>v.template.spec.initContainers[0].resources={limits:{cpu:'1'}},
  v=>v.template.spec.containers[0].imagePullPolicy='IfNotPresent',v=>v.template.spec.containers[1].env.push({name:'FOREIGN',value:'x'}),
  v=>delete v.template.spec.containers[1].terminationMessagePath,v=>v.template.spec.hostNetwork=true]){
  const actual=observedDefaults(staged);mutate(actual);assert.equal(personalIntegrationsArtifactSpecMatches(input,actual),false);
 }
 for(const change of [{profile:'telegram-notifications'},{name:'control-plane'},{authorityImage:componentImage},
  {beforeSpec:{...beforeSpec,template:{...beforeSpec.template,spec:{...beforeSpec.template.spec,initContainers:[{name:'foreign'}]}}}}])
  assert.equal(personalIntegrationsArtifactSpecMatches({...input,...change},observedDefaults(staged)),false);
});

// Разбор canonical CLI принимает successor только на позднем handoff;
// ранние фазы и Telegram не получают новый executor argument.
test('049 ART: successor handoff CLI flag не расширяет early/legacy profiles',()=>{
 const cli=new URL('./control-plane-migration.mjs',import.meta.url).pathname;
 const run=(phase,profile='personal-integrations-backup',sha='a'.repeat(40))=>{
  try{execFileSync(process.execPath,[cli,phase,'--profile',profile,'--plan','/synthetic/absent','--personal-executor-sha',sha],{timeout:10000,stdio:['ignore','pipe','pipe']});assert.fail('unexpected success');}
  catch(error){return String(error.stderr);}
 };
 assert.match(run('handoff'),/STAGING_CONTEXT_REQUIRED/);
 for(const phase of ['plan','check','stop','apply','retry'])assert.match(run(phase),/PHASE_ARGUMENT_INVALID/);
 assert.match(run('handoff','telegram-notifications'),/PHASE_ARGUMENT_INVALID/);
 assert.match(run('handoff','personal-integrations','invalid'),/PHASE_ARGUMENT_INVALID/);
});
