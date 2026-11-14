import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync,chmodSync} from 'node:fs';
import {dirname} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {planTelegramPortRepair,telegramPortRepairEffective,telegramPortRepairConfiguration,readTelegramPortRepair,telegramPortRepairManifestPaths} from './telegram-port-repair.mjs';
import {resolveTelegramExecutorSource} from './telegram-executor-source.mjs';

function fixture(t){
 const directory=mkdtempSync('/tmp/kodex-port-repair-');chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const source=directory+'/application',executor=directory+'/executor';mkdirSync(source,{mode:0o755});
 const git=(...args)=>execFileSync('git',['-C',source,...args],{encoding:'utf8',timeout:15000,stdio:['ignore','pipe','pipe']}).trim();
 const write=(path,value)=>{mkdirSync(dirname(path),{recursive:true});writeFileSync(path,value,{mode:0o644});};
 git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');
 telegramPortRepairManifestPaths.forEach((path,index)=>write(source+'/'+path,('name: telegram-connect\n').repeat(index+1)));
 git('add','.');git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Исходный config');const revision=git('rev-parse','HEAD');
 git('worktree','add','-q','--detach',executor,revision);
 telegramPortRepairManifestPaths.forEach((path,index)=>write(source+'/'+path,('name: tg-connect\n').repeat(index+1)));
 git('add','.');git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Исправленные port aliases');
 // application — detached original; executor — новый canonical config.
 const application={path:executor,revision};
 const spec=()=>({replicas:0,template:{spec:{containers:[{name:'egress-gateway',image:'synthetic@sha256:'+'a'.repeat(64),ports:[{name:'telegram-connect',containerPort:8086,protocol:'TCP'}]}]}}});
 const components=['control-plane','egress-gateway','control-api-gateway','integration-gateway','interaction-gateway'].map(name=>({name,uid:name,stagedSpec:spec(),resumedSpec:spec()}));
 const migration={version:2,profile:'telegram-notifications',migrationVersion:'20261114140000',id:'synthetic-migration',source:application,
  portableTarget:{installationUID:'synthetic-installation'},postgres:{database:'control_plane'},postconditionSQLSHA256:'c'.repeat(64),
  componentHandoff:{components,publicationSHA256:'d'.repeat(64)},telegramInputs:{sourceSHA:revision,
   projection:{egress:{port:{name:'telegram-connect',containerPort:8086,protocol:'TCP'}}},
   patches:[{before:{kind:'Service',metadata:{name:'egress-gateway',uid:'service-uid'},spec:{ports:[]}},
    after:{ports:[{name:'telegram-connect',port:8086,targetPort:'telegram-connect',protocol:'TCP'}]}}]}};
 const inspection={status:'PASS',schemaPostcondition:'PASS',cpStage:'STOPPED',podsGone:true,cpReady:false,jobUID:'synthetic-job',
  componentPair:components.map(c=>({name:c.name,stage:'stoppedSpec',joined:true,ready:false}))};
 const rows=[{type:'HEADER',kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(migration)},{type:'STOP_ACK'},
  {type:'TERMINAL_READBACK',jobUID:inspection.jobUID},{type:'INSPECT',stage:'STOPPED'},
  ...components.map(c=>({type:'PAIR_INSPECT',name:c.name,stage:'stoppedSpec'})),
  {type:'READBACK',status:'PASS',schemaPostcondition:'PASS',jobUID:inspection.jobUID}];
 const evidence=()=>rows.map(row=>JSON.stringify(row)).join('\n')+'\n';
 const input={migration,migrationPlanPath:directory+'/migration.json',migrationEvidencePath:directory+'/migration.jsonl',
  evidence:evidence(),inspection,configuration:telegramPortRepairConfiguration(source,application),observedAt:'2026-11-15T00:00:00Z'};
 const store=()=>{
  writeFileSync(input.migrationPlanPath,JSON.stringify(migration)+'\n',{mode:0o600});
  writeFileSync(input.migrationEvidencePath,evidence(),{mode:0o600});
  const plan=planTelegramPortRepair({...input,evidence:evidence()}),path=directory+'/repair.json';writeFileSync(path,JSON.stringify(plan)+'\n',{mode:0o600});return {path,plan};
 };
 return {source,application,migration,inspection,rows,input,store,evidence,git,write};
}

test('repair даёт ровно три aliases и отдельные hashes, original migration immutable',t=>{
 const f=fixture(t),original=structuredClone(f.migration),effective=telegramPortRepairEffective(f.migration);
 assert.deepEqual(f.migration,original);assert.equal(effective.inputs.projection.egress.port.name,'tg-connect');
 assert.equal(effective.inputs.patches[0].after.ports[0].targetPort,'tg-connect');
 assert.notEqual(fingerprint(effective.handoff),fingerprint(f.migration.componentHandoff));
 const {path,plan}=f.store(),loaded=readTelegramPortRepair(path,{executorSource:f.source,migration:f.migration,phase:'inspect'});
 assert.equal(loaded.sha256,fingerprint(plan));
 for(const phase of ['plan','check','apply','stop','retry'])assert.throws(()=>readTelegramPortRepair(path,{executorSource:f.source,phase}),/PHASE_REJECTED/);
 f.rows.push({type:'TELEGRAM_PORT_REPAIR_BOUND',repairSHA256:loaded.sha256,jobUID:plan.jobUID});
 writeFileSync(f.input.migrationEvidencePath,f.evidence(),{mode:0o600});assert.doesNotThrow(()=>readTelegramPortRepair(path,{executorSource:f.source}));
 writeFileSync(f.input.migrationEvidencePath,' '+f.evidence(),{mode:0o600});assert.throws(()=>readTelegramPortRepair(path,{executorSource:f.source}),/EVIDENCE_CHANGED/);
});

test('running/replaced Job, partial/cohort ready и handoff/service intent закрывают repair creation',t=>{
 for(const mutate of [f=>f.inspection.status='RUNNING',f=>f.inspection.jobUID='replacement',f=>f.inspection.cpReady=true,
  f=>f.inspection.componentPair[1].joined=false,f=>f.inspection.componentPair[2].stage='stagedSpec',f=>f.inspection.componentPair.pop(),
  f=>f.rows.push({type:'PAIR_HANDOFF_INTENT'}),f=>f.rows.push({type:'RESUME_INTENT'}),
  f=>f.rows.push({type:'TELEGRAM_INPUT_PATCH_INTENT',resource:'v1|Service|egress-gateway'}),
  f=>f.rows.push({type:'PAIR_INSPECT',name:'egress-gateway',stage:'stagedSpec'})]){
  const f=fixture(t);mutate(f);assert.throws(()=>planTelegramPortRepair({...f.input,evidence:f.evidence()}));
 }
});

test('unknown plan fields, content drift и новый executor не усыновляют старый repair',t=>{
 const f=fixture(t),{path,plan}=f.store();
 writeFileSync(path,JSON.stringify({...plan,extra:true})+'\n',{mode:0o600});assert.throws(()=>readTelegramPortRepair(path,{executorSource:f.source}),/PLAN_CHANGED/);
 writeFileSync(path,JSON.stringify(plan)+'\n',{mode:0o600});
 f.write(f.source+'/tools/release/changed.mjs','new executor');f.git('add','.');f.git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Другой executor');
 assert.throws(()=>readTelegramPortRepair(path,{executorSource:f.source}),/SOURCE_REJECTED/);
 const g=fixture(t);g.write(g.source+'/'+telegramPortRepairManifestPaths[0],'name: tg-connect\nport: 443\n');g.git('add','.');g.git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Лишняя delta');
 assert.throws(()=>telegramPortRepairConfiguration(g.source,g.application),/CONFIG_REJECTED/);
});

test('deploy delta требует exact repair source proof, saved pin не переносится на иной план',t=>{
 const f=fixture(t),{path}=f.store();
 assert.throws(()=>resolveTelegramExecutorSource(f.source,{source:f.application.path,revision:f.application.revision}),/SOURCE_REJECTED/);
 const proof=resolveTelegramExecutorSource(f.source,{source:f.application.path,revision:f.application.revision,repairPlanPath:path});
 assert.equal(proof.telegramPortRepair.planPath,path);assert.deepEqual(resolveTelegramExecutorSource(f.source,{saved:proof}),proof);
 assert.throws(()=>resolveTelegramExecutorSource(f.source,{saved:{...proof,telegramPortRepair:{...proof.telegramPortRepair,planSHA256:'f'.repeat(64)}}}),/SOURCE_REJECTED/);
 const g=fixture(t);g.write(g.source+'/deploy/foreign.yaml','foreign config');g.git('add','.');g.git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Лишний deploy');
 assert.throws(()=>telegramPortRepairConfiguration(g.source,g.application),/CONFIG_REJECTED/);
});


test('APPLICATION A / CONFIG B / EXECUTOR C сохраняют original repair и требуют explicit tools-only ancestor',t=>{
 const f=fixture(t),{path,plan}=f.store(),original=readFileSync(path),prefix=readFileSync(f.input.migrationEvidencePath),next=f.source+'-late';
 f.git('worktree','add','-q','--detach',next,'HEAD');
 const git=(...args)=>execFileSync('git',['-C',next,...args],{encoding:'utf8',timeout:15000,stdio:['ignore','pipe','pipe']}).trim();
 f.write(next+'/tools/release/late-owner.mjs','qualified tools-only successor');git('add','.');git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Позднее owner чтение');
 const revision=git('rev-parse','HEAD');
 assert.throws(()=>readTelegramPortRepair(path,{executorSource:next}),/SOURCE_REJECTED/);
 assert.throws(()=>readTelegramPortRepair(path,{executorSource:next,executorRevision:'f'.repeat(40)}),/SOURCE_REJECTED/);
 const loaded=readTelegramPortRepair(path,{executorSource:next,executorRevision:revision});
 assert.deepEqual(loaded.repair.configuration,plan.configuration);assert.equal(loaded.executor.configurationRevision,plan.configuration.revision);assert.equal(loaded.executor.executorRevision,revision);
 const proof=resolveTelegramExecutorSource(next,{source:f.application.path,revision:f.application.revision,repairPlanPath:path,repairExecutorRevision:revision});
 assert.deepEqual(resolveTelegramExecutorSource(next,{saved:proof}),proof);
 assert.throws(()=>resolveTelegramExecutorSource(next,{saved:{...proof,repairExecutor:{...proof.repairExecutor,configurationRevision:'f'.repeat(40)}}}),/SOURCE_REJECTED/);
 assert.deepEqual(readFileSync(path),original);assert.deepEqual(readFileSync(f.input.migrationEvidencePath),prefix);
 f.write(next+'/tools/release/dirty.mjs','dirty');assert.throws(()=>readTelegramPortRepair(path,{executorSource:next,executorRevision:revision}));
});

for(const changed of ['deploy/foreign.yaml','services/example/main.go','contracts/foreign.proto','tools/release/foreign.sql','tools/release/authority-telegram-transition.mjs','tools/release/fixtures/authority-policy-foreign.json'])
 test(`late repair executor закрывает protected delta ${changed}`,t=>{
  const f=fixture(t),{path}=f.store(),next=f.source+'-late';f.git('worktree','add','-q','--detach',next,'HEAD');
  const git=(...args)=>execFileSync('git',['-C',next,...args],{encoding:'utf8',timeout:15000,stdio:['ignore','pipe','pipe']}).trim();
  f.write(next+'/'+changed,'foreign');git('add','.');git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Запрещённая delta');
  assert.throws(()=>readTelegramPortRepair(path,{executorSource:next,executorRevision:git('rev-parse','HEAD')}),/SOURCE_REJECTED/);
 });

test('same-tree orphan не заменяет настоящую ancestry CONFIG→EXECUTOR',t=>{
 const f=fixture(t),{path}=f.store(),tree=f.git('rev-parse','HEAD^{tree}'),orphan=f.git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit-tree',tree,'-m','Синтетический orphan'),next=f.source+'-orphan';
 f.git('worktree','add','-q','--detach',next,orphan);
 assert.throws(()=>readTelegramPortRepair(path,{executorSource:next,executorRevision:orphan}),/SOURCE_REJECTED/);
});
