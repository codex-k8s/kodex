import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdtempSync,readFileSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {migrationAPIRestorationExecutor} from './component-migration-api-admission.mjs';

test('exact executor boundary: real unrelated same-tree commit, missing explicit pin and ancestor SQL drift fail for their precise reasons',t=>{
 const root=resolve(fileURLToPath(new URL('../../',import.meta.url)));
 const git=(...args)=>execFileSync('git',['-C',root,...args],{encoding:'utf8',timeout:30000,stdio:['ignore','pipe','pipe']}).trim();
 const current=git('rev-parse','HEAD'),tree=git('rev-parse','HEAD^{tree}'),ancestor=git('rev-parse','HEAD^');
 // Настоящий orphan Git object с тем же публичным tree не получает ref и не
 // публикуется. Поэтому SQL equality не маскирует отсутствие ancestry.
 const orphan=git('-c','commit.gpgSign=false','commit-tree',tree,'-m','Синтетический независимый source для отказной проверки2140');
 assert.match(orphan,/^[a-f0-9]{40}$/);
 const directory=mkdtempSync(join(tmpdir(),'kodex-api-executor-')),owned=[];
 t.after(()=>{for(const path of owned.reverse())git('worktree','remove','--force',path);rmSync(directory,{recursive:true,force:true});});
 const checkout=(name,revision)=>{const path=join(directory,name);git('worktree','add','--detach',path,revision);owned.push(path);return path;};
 const sqlName='tools/release/sentry-observability-backup-migration-postcondition.sql';
 const digest=path=>createHash('sha256').update(readFileSync(join(path,sqlName))).digest('hex');
 const expected=digest(root),record=(path,revision)=>({profile:'sentry-observability-backup',source:{path,revision},postconditionSQLSHA256:expected});
 assert.equal(migrationAPIRestorationExecutor(record(root,current),current).executorSHA,current);
 assert.throws(()=>migrationAPIRestorationExecutor(record(root,current),undefined),error=>error.message==='COMPONENT_MIGRATION_API_SOURCE_REQUIRED');
 const unrelated=checkout('unrelated',orphan);assert.equal(digest(unrelated),expected);
 assert.throws(()=>migrationAPIRestorationExecutor(record(unrelated,orphan),orphan),error=>error.message==='COMPONENT_MIGRATION_API_EXECUTOR_UNRELATED');
 const previous=checkout('ancestor',ancestor);assert.equal(digest(previous),expected);
 assert.equal(migrationAPIRestorationExecutor(record(previous,ancestor),ancestor).executorSHA,current);
 writeFileSync(join(previous,sqlName),readFileSync(join(previous,sqlName),'utf8')+'\n-- synthetic SQL drift\n');
 assert.throws(()=>migrationAPIRestorationExecutor(record(previous,ancestor),ancestor),error=>error.message==='COMPONENT_MIGRATION_API_SQL_SOURCE_CHANGED');
});
