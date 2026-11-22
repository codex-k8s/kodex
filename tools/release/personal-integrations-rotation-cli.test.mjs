import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {personalRotationFixture} from './fixtures/personal-integrations-rotation-synthetic.mjs';
import {main} from './authority-rotation-transition.mjs';
import {verifyPersonalIntegrationsAuthorityAPI} from './personal-integrations-authority-api.mjs';
import {verifyPersonalIntegrationsAuthorityPolicy,personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {telegramRegistrySource,telegramPolicyTransition} from './authority-telegram-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';
import {fingerprint} from './scoped-release.mjs';

// Публичный parser/planner/CAS/journal path, настоящие source и Job validators.
// Только тяжелые independently-tested fixed admission readers заменены ports;
// никаких настоящих Kubernetes/SQL запросов, Secret payload либо paid effects.
function fixture(t){
 const f=personalRotationFixture(),directory=mkdtempSync(join(tmpdir(),'kodex-personal-rotation-cli-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const targetPath=join(directory,'target'),kubeconfig=join(directory,'kubeconfig'),output=join(directory,'plan'),evidence=join(directory,'events'),refsPath=join(directory,'refs');
 const refs=f.migration.personalIntegrationsRelease;
 writeFileSync(targetPath,JSON.stringify(f.profile),{mode:0o600});writeFileSync(kubeconfig,'synthetic-unused',{mode:0o600});
 const {authorityTarget,...configuration}=refs;
 writeFileSync(refsPath,JSON.stringify({...configuration,authorityTargetPath:targetPath}),{mode:0o600});
 f.registry.data['key-delivery-targets.yaml']=telegramRegistrySource('desired').raw;
 f.registry.data['authority-policy.json']=renderAuthorityPolicyEnvironment(verifyPersonalIntegrationsAuthorityPolicy().predecessor.raw,f.plan.policyTransition.environment).raw;
 delete f.registry.metadata.annotations;f.publisher.spec.template.metadata.annotations={};
 f.resources.set('namespace/kodex-system',{kind:'Namespace',metadata:{uid:f.profile.namespaceUID}});
 const decode=path=>execFileSync('yq',['ea','-o=json','-I=0','.',path],{encoding:'utf8',timeout:10000,maxBuffer:1<<20}).trim().split('\n').map(JSON.parse);
 for(const path of ['serviceaccount.yaml','networkpolicy.yaml'])for(const r of decode(f.plan.source+'/deploy/k8s/base/internal-rpc-authority-data/'+path))f.resources.set(r.kind.toLowerCase()+'/'+r.metadata.name,r);
 const faults={},jobs=new Map(),effects=[],admissions=[],losses=new Set(['registry','publisher','job']);
 const run=(command,args,input)=>{
  assert.equal(command,'kubectl');const prefix=['--kubeconfig',kubeconfig,'--context',f.profile.context,'--request-timeout=30s'];assert.deepEqual(args.slice(0,prefix.length),prefix);
  const raw=args.slice(prefix.length),argv=[];for(let i=0;i<raw.length;i++){if(raw[i]==='-n'){assert.equal(raw[++i],'kodex-system');}else argv.push(raw[i]);}
  if(argv[0]==='auth')return 'yes';
  if(argv[0]==='get'){
   const [,kind,name]=argv;
   if(kind==='job')return jobs.has(name)?JSON.stringify(jobs.get(name)):'';
   if(kind==='secret'||name==='internal-rpc-authority-postgresql-ca')return kind+'/'+name;
   const value=f.get(kind,name);assert.ok(value,kind+'/'+name);return JSON.stringify(value);
  }
  if(argv[0]==='create'&&argv.includes('--dry-run=server'))return input;
  if(argv[0]==='replace'){
   const desired=JSON.parse(input),key=desired.kind.toLowerCase()+'/'+desired.metadata.name,current=f.resources.get(key);
   assert.equal(desired.metadata.uid,current.metadata.uid);assert.equal(desired.metadata.resourceVersion,current.metadata.resourceVersion);
   Object.assign(current,desired);current.metadata.resourceVersion=String(Number(current.metadata.resourceVersion)+1);effects.push(key);
   if(losses.delete(desired.kind==='ConfigMap'?'registry':'publisher'))throw Error('SYNTHETIC_ACK_LOST');return JSON.stringify(current);
  }
  if(argv[0]==='create'){
   const desired=JSON.parse(input);assert.equal(desired.kind,'Job');assert.ok(!jobs.has(desired.metadata.name));
   desired.metadata.uid='00000000-0000-4000-8000-000000000950';desired.status={active:1};jobs.set(desired.metadata.name,desired);effects.push('job/'+desired.metadata.name);
   if(losses.delete('job')){if(faults.dropCreated)jobs.delete(desired.metadata.name);throw Error('SYNTHETIC_ACK_LOST');}return JSON.stringify(desired);
  }
  assert.fail('Unexpected synthetic operation');
 };
 const io={run,inspectSource:()=>({revision:f.plan.revision}),capturePersonalOwnerScope:plan=>({...f.plan.ownerScope,kubeconfig:plan.kubeconfig}),
  readPersonalStagedAdmission(input){assert.deepEqual(input.authorityTarget,f.profile);assert.equal(input.admission.migrationPlanPath,'/synthetic/migration-plan');return structuredClone(f.plan.maintenance);},
  readPersonalRotationAdmission(plan){verifyPersonalIntegrationsAuthorityAPI(plan.maintenance,plan.policyTransition);admissions.push(fingerprint(f.registry.data)===plan.registry.currentDataSHA256?'PRE_ROTATION':'POST_CAS');}};
 const args=['plan','--context',f.profile.context,'--kubeconfig',kubeconfig,'--portable-target',targetPath,'--policy-transition',personalIntegrationsAuthorityTransition,
  '--source',f.plan.source,'--revision',f.plan.revision,'--action','rotate','--output',output,'--personal-prerequisites',refsPath,
  '--maintenance-plan','/synthetic/close','--maintenance-evidence','/synthetic/close-events','--traffic-plan','/synthetic/isolate','--traffic-evidence','/synthetic/isolate-events',
  '--migration-plan','/synthetic/migration-plan','--migration-evidence','/synthetic/migration-events'];
 const flags=['--context',f.profile.context,'--kubeconfig',kubeconfig,'--plan',output,'--evidence',evidence];
 return {...f,io,args,flags,output,evidence,effects,admissions,jobs,faults};
}

test('personal public CLI persists source/refs then reuses exact CAS/publisher/Job after all lost ACKs',async t=>{
 const f=fixture(t);await main(f.args,f.io);const plan=JSON.parse(readFileSync(f.output,'utf8'));
 assert.equal(plan.policyTransition.profile,personalIntegrationsAuthorityTransition);assert.equal(plan.registry.policyRevision,110);
 assert.equal(plan.registry.previousPolicyRevision,107);assert.ok(plan.registry.predecessorRaw);assert.equal(plan.publisher.image,f.publisher.spec.template.spec.containers[0].image);
 assert.deepEqual(plan.personalIntegrationsRelease,f.migration.personalIntegrationsRelease);assert.ok(plan.ownerScope);
 await main(['apply',...f.flags,'--confirm','APPLY-PERSONAL-INTEGRATIONS-AUTHORITY'],f.io);
 await main(['resume',...f.flags],f.io);
 assert.equal(f.effects.length,3);assert.deepEqual(f.admissions,['PRE_ROTATION','POST_CAS','POST_CAS']);
 const rows=readFileSync(f.evidence,'utf8').trim().split('\n').map(JSON.parse);
 assert.equal(rows.filter(row=>row.status==='UNKNOWN').length,3);assert.equal(new Set(rows.filter(row=>row.jobUID).map(row=>row.jobUID)).size,1);
});

test('personal profile refuses abort and missing typed prerequisites; Telegram cannot consume new flags',async t=>{
 const f=fixture(t),abort=[...f.args];abort[abort.indexOf('--action')+1]='abort';await assert.rejects(main(abort,f.io),/ABORT_UNQUALIFIED/);assert.equal(f.effects.length,0);
 const missing=f.args.filter((v,i,a)=>v!=='--personal-prerequisites'&&a[i-1]!=='--personal-prerequisites');await assert.rejects(main(missing,f.io));
 const old=[...f.args];old[old.indexOf('--policy-transition')+1]=telegramPolicyTransition;await assert.rejects(main(old,f.io),/PERSONAL_INTEGRATIONS_ROTATION_PROFILE_REQUIRED/);
 assert.equal(f.effects.length,0);
});


test('UNKNOWN CREATE+absence и known UID replacement не дают повторный CREATE на resume',async t=>{
 for(const kind of ['unknown-absent','known-absent','known-replaced']){
  const f=fixture(t);await main(f.args,f.io);
  if(kind==='unknown-absent')f.faults.dropCreated=true;
  const apply=()=>main(['apply',...f.flags,'--confirm','APPLY-PERSONAL-INTEGRATIONS-AUTHORITY'],f.io);
  if(kind==='unknown-absent')await assert.rejects(apply);else await apply();
  if(kind==='known-replaced')[...f.jobs.values()][0].metadata.uid='00000000-0000-4000-8000-000000000999';else f.jobs.clear();
  const before=f.effects.length;
  await assert.rejects(main(['resume',...f.flags],f.io),kind==='known-replaced'?/ROTATION_JOB_REPLACED/:/CREATE_UNRESOLVED/);
  assert.equal(f.effects.length,before);assert.equal(f.effects.filter(name=>name.startsWith('job/')).length,1);
 }
});

test('foreign journal header и concurrent personal custody lock закрывают resume до effects',async t=>{
 for(const kind of ['journal','lock']){
  const f=fixture(t);await main(f.args,f.io);await main(['apply',...f.flags,'--confirm','APPLY-PERSONAL-INTEGRATIONS-AUTHORITY'],f.io);
  if(kind==='journal'){
   const rows=readFileSync(f.evidence,'utf8').trim().split('\n').map(JSON.parse);rows[0].planSHA256='f'.repeat(64);
   writeFileSync(f.evidence,rows.map(row=>JSON.stringify(row)+'\n').join(''));
  }else writeFileSync(f.evidence+'.custody.lock','synthetic-other-writer\n',{mode:0o600,flag:'wx'});
  const before=f.effects.length;await assert.rejects(main(['resume',...f.flags],f.io));assert.equal(f.effects.length,before);
 }
});
