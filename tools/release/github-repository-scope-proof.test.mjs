import test from 'node:test';
import assert from 'node:assert/strict';
import { githubRepositoryScopeCatalog } from './github-repository-scope-catalog.gen.mjs';
import { immutableMigrationProfile, validGitHubRepositoryScopeMigrationPostcondition, validGitHubRepositoryScopeMigrationPrecondition } from './control-plane-immutable-migration.mjs';

const activity=['activeRuntimeLeases','activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions','pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeIntegrationInvocations','activeIntegrationConnectionTests','activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks','oldClaimSessions'];
const proof=(backup=false,before=false)=>({profile:'GITHUB_REPOSITORY_SCOPE_'+(backup?'BACKUP_':'')+(before?'PREDECESSOR_':'')+'V1',database:'control_plane',readOnly:'on',gooseVersion:before?20261113000100:20261114120000,schemaCatalog:structuredClone(githubRepositoryScopeCatalog[backup?'backup':'normal'][before?'before':'after']),...(before?{githubRepositoryScopeAbsent:true,...Object.fromEntries(activity.map(key=>[key,0]))}:{})});

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
