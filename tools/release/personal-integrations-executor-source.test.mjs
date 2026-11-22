import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,rmSync} from 'node:fs';
import {dirname,join} from 'node:path';
import {tmpdir} from 'node:os';
import {resolvePersonalIntegrationsExecutorSource,personalIntegrationsExecutorPhasePaths,
 readPersonalIntegrationsMigrationExecutor,bindPersonalIntegrationsMigrationExecutor,migrationInspectTimeoutMs} from './personal-integrations-executor-source.mjs';
import {backupMigrationInspectArguments} from './backup-controller-maintenance-cli.mjs';

function fixture(t){
 const directory=mkdtempSync(join(tmpdir(),'kodex-personal-executor-'));t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const executor=join(directory,'executor'),source=join(directory,'application');mkdirSync(executor);
 const git=(...args)=>execFileSync('git',['-C',executor,...args],{encoding:'utf8',timeout:15000,stdio:['ignore','pipe','pipe']}).trim();
 const write=(path,value)=>{mkdirSync(dirname(join(executor,path)),{recursive:true});writeFileSync(join(executor,path),value);};
 const commit=()=>{git('add','.');git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Синтетический personal executor');return git('rev-parse','HEAD');};
 git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');
 for(const path of personalIntegrationsExecutorPhasePaths)write(path,'immutable synthetic bytes');
 write('tools/release/control-plane-immutable-migration.mjs','original executor');const revision=commit();git('worktree','add','-q','--detach',source,revision);
 write('tools/release/control-plane-immutable-migration.mjs','qualified executor');const executorRevision=commit();
 return {source,executor,revision,executorRevision,write,commit};
}

test('personal split хранит оба exact source, immutable phase hashes и повторяемый readback',t=>{
 const f=fixture(t),proof=resolvePersonalIntegrationsExecutorSource(f.executor,{source:f.source,revision:f.revision});
 assert.equal(proof.revision,f.revision);assert.equal(proof.executorRevision,f.executorRevision);assert.notEqual(proof.phaseSourceSHA256,proof.changedPathsSHA256);
 assert.deepEqual(resolvePersonalIntegrationsExecutorSource(f.executor,{saved:proof}),proof);
 const plan={source:{path:f.source,revision:f.revision}};
 assert.throws(()=>readPersonalIntegrationsMigrationExecutor(plan,undefined,f.executor),/SOURCE_REJECTED/);
 assert.deepEqual(readPersonalIntegrationsMigrationExecutor(plan,f.executorRevision,f.executor),proof);
 assert.throws(()=>resolvePersonalIntegrationsExecutorSource(f.executor,{saved:{...proof,executorRevision:'f'.repeat(40)}}),/SOURCE_REJECTED/);
 assert.throws(()=>resolvePersonalIntegrationsExecutorSource(f.source,{source:f.executor,revision:f.executorRevision}),/SOURCE_REJECTED/);
 const journal={events:[],append(row){this.events.push(row);}};
 bindPersonalIntegrationsMigrationExecutor(journal,proof,{readOnly:true});assert.equal(journal.events.length,0);
 bindPersonalIntegrationsMigrationExecutor(journal,proof);assert.equal(journal.events.length,1);
 bindPersonalIntegrationsMigrationExecutor(journal,proof,{readOnly:true});assert.equal(journal.events.length,1);
 assert.throws(()=>bindPersonalIntegrationsMigrationExecutor(journal,{...proof,executorRevision:'f'.repeat(40)}),/SOURCE_REJECTED/);
 assert.throws(()=>bindPersonalIntegrationsMigrationExecutor(journal,undefined),/SOURCE_REJECTED/);
});

for(const path of ['services/example/main.go','libs/go/foreign.go','contracts/foreign.yaml','deploy/foreign.yaml',
 'tools/release/personal-integrations-schema-manifest.json','tools/release/personal-integrations-backup-migration-postcondition.sql',
 'tools/release/authority-rotation-transition.mjs','tools/release/new-unreviewed-operator.mjs'])test(`personal split закрывает непроверенный путь ${path}`,t=>{
 const f=fixture(t);f.write(path,'changed');f.commit();
 assert.throws(()=>resolvePersonalIntegrationsExecutorSource(f.executor,{source:f.source,revision:f.revision}),/SOURCE_REJECTED/);
});

test('dirty source/executor и непроверенный successor не переиспользуют saved proof',t=>{
 const f=fixture(t),proof=resolvePersonalIntegrationsExecutorSource(f.executor,{source:f.source,revision:f.revision});
 f.write('tools/release/control-plane-immutable-migration.mjs','dirty');assert.throws(()=>resolvePersonalIntegrationsExecutorSource(f.executor,{saved:proof}));
 f.commit();assert.throws(()=>resolvePersonalIntegrationsExecutorSource(f.executor,{saved:proof}));
 writeFileSync(join(f.source,personalIntegrationsExecutorPhasePaths[0]),'dirty application');
 assert.throws(()=>resolvePersonalIntegrationsExecutorSource(f.executor,{source:f.source,revision:f.revision}));
});

test('late child inspect получает exact executor, read-only journal и конечный personal240s без расширения legacy',()=>{
 const revision='a'.repeat(40),args=backupMigrationInspectArguments({profile:'personal-integrations-backup',context:'fixture',kubeconfig:'/synthetic/config',
  migrationPlanPath:'/synthetic/plan',migrationEvidencePath:'/synthetic/events',executorSHA:revision,
  references:{'--api-open-plan':'/synthetic/open','--api-open-evidence':'/synthetic/open-events'}});
 assert.equal(args[args.indexOf('--personal-executor-sha')+1],revision);assert.equal(args[args.indexOf('--evidence-read-only')+1],'true');
 assert.equal(migrationInspectTimeoutMs('personal-integrations'),240000);assert.equal(migrationInspectTimeoutMs('personal-integrations-backup'),240000);
 assert.equal(migrationInspectTimeoutMs('telegram-notifications-backup'),300000);assert.equal(migrationInspectTimeoutMs('sentry-observability-backup'),120000);
 assert.throws(()=>backupMigrationInspectArguments({profile:'sentry-observability-backup',executorSHA:revision,references:{}}),/INPUT_INVALID/);
});
