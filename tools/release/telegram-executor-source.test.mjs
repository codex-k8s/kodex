import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,rmSync,chmodSync} from 'node:fs';
import {dirname} from 'node:path';
import {resolveTelegramExecutorSource} from './telegram-executor-source.mjs';

function fixture(t){
 const directory=mkdtempSync('/tmp/kodex-executor-source-');chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const source=directory+'/source',executor=directory+'/executor';mkdirSync(source,{mode:0o755});
 const git=(...args)=>execFileSync('git',['-C',source,...args],{encoding:'utf8',timeout:15000,stdio:['ignore','pipe','pipe']}).trim();
 const write=(path,value)=>{mkdirSync(dirname(source+'/'+path),{recursive:true,mode:0o755});writeFileSync(source+'/'+path,value,{mode:0o644});};
 const commit=()=>{git('add','.');git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Синтетическая revision');return git('rev-parse','HEAD');};
 git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');write('services/example/main.go','synthetic application');
 write('tools/release/executor.mjs','synthetic executor');const revision=commit();git('worktree','add','-q','--detach',executor,revision);
 write('tools/release/executor.mjs','qualified successor');commit();
 return {source:executor,executor:source,revision,write,commit,git};
}

test('frozen application и новый executor имеют отдельные exact pins и повторный readback',t=>{
 const f=fixture(t),proof=resolveTelegramExecutorSource(f.executor,{source:f.source,revision:f.revision});
 assert.notEqual(proof.executorRevision,proof.revision);assert.equal(proof.revision,f.revision);
 assert.deepEqual(resolveTelegramExecutorSource(f.executor,{saved:proof}),proof);
 assert.throws(()=>resolveTelegramExecutorSource(f.executor,{saved:{...proof,executorRevision:'f'.repeat(40)}}));
 assert.throws(()=>resolveTelegramExecutorSource(f.executor,{source:f.source,revision:'f'.repeat(40)}));
 f.write('tools/release/new-test.mjs','new revision');f.commit();
 assert.throws(()=>resolveTelegramExecutorSource(f.executor,{saved:proof}));
});

for(const path of ['services/example/main.go','libs/new.go','contracts/new.yaml','deploy/new.yaml','go.work','tools/install/new.py',
 'tools/release/telegram-notifications-migration-postcondition.sql','tools/release/fixtures/authority-policy-revision107.json',
 'tools/release/authority-telegram-transition.mjs','tools/release/telegram-notifications-catalog.gen.mjs'])
 test(`application/policy/schema drift ${path} запрещает successor`,t=>{
  const f=fixture(t);f.write(path,'changed');f.commit();
  assert.throws(()=>resolveTelegramExecutorSource(f.executor,{source:f.source,revision:f.revision}),/SOURCE_REJECTED/);
 });

test('dirty application/executor и обратный ancestor закрыто отказаны',t=>{
 const f=fixture(t);
 assert.throws(()=>resolveTelegramExecutorSource(f.source,{source:f.executor,revision:f.git('rev-parse','HEAD')}),/SOURCE_REJECTED/);
 f.write('tools/release/dirty.mjs','uncommitted');
 assert.throws(()=>resolveTelegramExecutorSource(f.executor,{source:f.source,revision:f.revision}));
 f.commit();writeFileSync(f.source+'/services/example/main.go','dirty');
 assert.throws(()=>resolveTelegramExecutorSource(f.executor,{source:f.source,revision:f.revision}));
});
