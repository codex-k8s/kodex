import test from 'node:test';
import assert from 'node:assert/strict';
import { sentryObservabilityCatalog } from './sentry-observability-catalog.gen.mjs';
import { immutableMigrationProfile, validSentryObservabilityMigrationPostcondition, validSentryObservabilityMigrationPrecondition } from './control-plane-immutable-migration.mjs';

const activity=['activeRuntimeLeases','activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions','pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeIntegrationInvocations','activeIntegrationConnectionTests','activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks','oldClaimSessions'];
const proof=(backup=false,before=false)=>({profile:'SENTRY_OBSERVABILITY_'+(backup?'BACKUP_':'')+(before?'PREDECESSOR_':'')+'V1',database:'control_plane',readOnly:'on',gooseVersion:before?20261114120000:20261114130000,schemaCatalog:structuredClone(sentryObservabilityCatalog[backup?'backup':'normal'][before?'before':'after']),...(before?{sentryObservabilityAbsent:true,...Object.fromEntries(activity.map(key=>[key,0]))}:{})});

test('schema045 закрывает весь catalog, а backup имеет собственный профиль',()=>{
 for(const backup of [false,true]){
  const actual=proof(backup);assert.equal(validSentryObservabilityMigrationPostcondition(actual,backup),true);
  assert.equal(validSentryObservabilityMigrationPostcondition(actual,!backup),false);
  for(const key of Object.keys(actual.schemaCatalog)){
   const changed=structuredClone(actual);changed.schemaCatalog[key]=typeof changed.schemaCatalog[key]==='number'?changed.schemaCatalog[key]-1:'changed';assert.equal(validSentryObservabilityMigrationPostcondition(changed,backup),false,key);
  }
  const extra=structuredClone(actual);extra.schemaCatalog.extra=true;assert.equal(validSentryObservabilityMigrationPostcondition(extra,backup),false);
  for(const key of ['database','readOnly','gooseVersion','profile']){const changed=structuredClone(actual);changed[key]='unknown';assert.equal(validSentryObservabilityMigrationPostcondition(changed,backup),false,key);}
 }
});

test('schema045 требует exact044 и отсутствие незавершённых effect/claim/tasks',()=>{
 for(const backup of [false,true]){
  const actual=proof(backup,true);assert.equal(validSentryObservabilityMigrationPrecondition(actual,backup),true);
  for(const key of activity){const changed=structuredClone(actual);changed[key]=1;assert.equal(validSentryObservabilityMigrationPrecondition(changed,backup),false,key);}
  const changed=structuredClone(actual);changed.schemaCatalog.functionCount--;assert.equal(validSentryObservabilityMigrationPrecondition(changed,backup),false);
  assert.equal(immutableMigrationProfile('sentry-observability'+(backup?'-backup':'')).version,'20261114130000');
 }
});
