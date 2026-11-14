import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,mkdirSync,copyFileSync,readFileSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,dirname} from 'node:path';
import {spawnSync} from 'node:child_process';
import { githubRepositoryScopeCatalog } from './github-repository-scope-catalog.gen.mjs';
import { immutableMigrationProfile, validGitHubRepositoryScopeMigrationPostcondition, validGitHubRepositoryScopeMigrationPrecondition } from './control-plane-immutable-migration.mjs';

const activity=['activeRuntimeLeases','activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions','pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeIntegrationInvocations','activeIntegrationConnectionTests','activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks','oldClaimSessions','activeBackupSessions'];
const proof=(backup=false,before=false)=>({profile:'GITHUB_REPOSITORY_SCOPE_'+(backup?'BACKUP_':'')+(before?'PREDECESSOR_':'')+'V1',database:'control_plane',readOnly:'on',publicSequenceSafe:true,backupQualification:backup?'CANONICAL_BACKUP':'NONE',gooseVersion:before?20261113000100:20261114120000,schemaCatalog:structuredClone(githubRepositoryScopeCatalog[backup?'backup':'normal'][before?'before':'after']),...(before?{githubRepositoryScopeAbsent:true,...Object.fromEntries(activity.map(key=>[key,0]))}:{})});

test('schema044 закрывает весь catalog, а backup имеет собственный профиль',()=>{
 for(const backup of [false,true]){
  const actual=proof(backup);assert.equal(validGitHubRepositoryScopeMigrationPostcondition(actual,backup),true);
  assert.equal(validGitHubRepositoryScopeMigrationPostcondition(actual,!backup),false);
  for(const key of Object.keys(actual.schemaCatalog)){
   const changed=structuredClone(actual);changed.schemaCatalog[key]=typeof changed.schemaCatalog[key]==='number'?changed.schemaCatalog[key]-1:'changed';assert.equal(validGitHubRepositoryScopeMigrationPostcondition(changed,backup),false,key);
  }
  const extra=structuredClone(actual);extra.schemaCatalog.extra=true;assert.equal(validGitHubRepositoryScopeMigrationPostcondition(extra,backup),false);
  for(const key of ['database','readOnly','gooseVersion','profile']){const changed=structuredClone(actual);changed[key]='unknown';assert.equal(validGitHubRepositoryScopeMigrationPostcondition(changed,backup),false,key);}
 }
});

test('schema044 требует exact043 и отсутствие незавершённых effect/claim/tasks',()=>{
 for(const backup of [false,true]){
  const actual=proof(backup,true);assert.equal(validGitHubRepositoryScopeMigrationPrecondition(actual,backup),true);
  for(const key of activity){const changed=structuredClone(actual);changed[key]=1;assert.equal(validGitHubRepositoryScopeMigrationPrecondition(changed,backup),false,key);}
  const changed=structuredClone(actual);changed.schemaCatalog.functionCount--;assert.equal(validGitHubRepositoryScopeMigrationPrecondition(changed,backup),false);
  assert.equal(immutableMigrationProfile('github-repository-scope'+(backup?'-backup':'')).version,'20261114120000');
 }
});

test('schema044: TABLE_ONLY_BACKUP и CANONICAL_BACKUP — exact ACL qualifications, без unknown fallback',()=>{
 for(const before of [false,true]){
  const actual=proof(true,before),validate=before?validGitHubRepositoryScopeMigrationPrecondition:validGitHubRepositoryScopeMigrationPostcondition;
  actual.backupQualification='TABLE_ONLY_BACKUP';
  assert.equal(validate(actual,true),false);
  actual.schemaCatalog=structuredClone(githubRepositoryScopeCatalog['table-only-backup'][before?'before':'after']);
  assert.equal(validate(actual,true),true);
  actual.backupQualification='UNKNOWN';assert.equal(validate(actual,true),false);
 }
});

test('schema044 generator связывает proofs с exact SQL initializer; изменение source закрыто отвергается',t=>{
 const directory=mkdtempSync(join(tmpdir(),'kodex-044-proof-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const files=['tools/release/generate-github-repository-scope-proof.py',
 'tools/release/github-repository-scope-schema-manifest.json','tools/release/github-repository-scope-catalog.sql','tools/release/github-repository-scope-public-sequence.sql',
 'tools/release/github-repository-scope-restore-catalog.sql','tools/release/workspace-unbound-stop-migration-precondition.sql',
 'tools/release/github-repository-scope-catalog.gen.mjs',
 ...['github-repository-scope','github-repository-scope-backup'].flatMap(p=>['precondition','postcondition'].map(k=>'tools/release/'+p+'-migration-'+k+'.sql')),
 ...['restore_20261113000100.sql','restore_20261114120000.sql','normalize_20261113000100.sql'].map(n=>'services/internal/control-plane/cmd/cli/workspace_backup_sql/'+n),
 'deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh'];
 for(const file of files){const out=join(directory,file);mkdirSync(dirname(out),{recursive:true});copyFileSync(new URL('../../'+file,import.meta.url),out);}
 const run=()=>spawnSync('python3',[join(directory,files[0]),'--check'],{encoding:'utf8',timeout:10000});
 assert.equal(run().status,0);
 const initializer=join(directory,files.at(-1)),before=readFileSync(initializer,'utf8');
 writeFileSync(initializer,before.replace('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public, control_plane','GRANT UPDATE ON ALL SEQUENCES IN SCHEMA public, control_plane'));
 const rejected=run();assert.notEqual(rejected.status,0);assert.match(rejected.stderr,/initializer source changed/);
 for(const file of files.filter(f=>f.endsWith('.sql')))assert.deepEqual(readFileSync(join(directory,file)),readFileSync(new URL('../../'+file,import.meta.url)));
});
