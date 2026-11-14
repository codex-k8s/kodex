import test from 'node:test';
import assert from 'node:assert/strict';
import { telegramNotificationsCatalog } from './telegram-notifications-catalog.gen.mjs';
import { immutableMigrationProfile, validTelegramNotificationsMigrationPostcondition, validTelegramNotificationsMigrationPrecondition } from './control-plane-immutable-migration.mjs';

const activity=['activeRuntimeLeases','activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions','pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeIntegrationInvocations','activeIntegrationConnectionTests','activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks','activeInteractionDeliveries','oldClaimSessions','activeBackupSessions'];
const proof=(backup=false,before=false)=>({profile:'TELEGRAM_NOTIFICATIONS_'+(backup?'BACKUP_':'')+(before?'PREDECESSOR_':'')+'V1',database:'control_plane',readOnly:'on',gooseVersion:before?20261114130000:20261114140000,backupQualification:backup?'CANONICAL_BACKUP':'NONE',publicSequenceSafe:true,schemaCatalog:structuredClone(telegramNotificationsCatalog[backup?'backup':'normal'][before?'before':'after']),...(before?{workspaceOwnerIdle:true,telegramNotificationsAbsent:true,...Object.fromEntries(activity.map(key=>[key,0]))}:{})});

test('schema046 закрывает весь catalog, а backup имеет собственный профиль',()=>{
 for(const backup of [false,true]){
  const actual=proof(backup);assert.equal(validTelegramNotificationsMigrationPostcondition(actual,backup),true);
  assert.equal(validTelegramNotificationsMigrationPostcondition(actual,!backup),false);
  for(const key of Object.keys(actual.schemaCatalog)){
   const changed=structuredClone(actual);changed.schemaCatalog[key]=typeof changed.schemaCatalog[key]==='number'?changed.schemaCatalog[key]-1:'changed';assert.equal(validTelegramNotificationsMigrationPostcondition(changed,backup),false,key);
  }
  const extra=structuredClone(actual);extra.schemaCatalog.extra=true;assert.equal(validTelegramNotificationsMigrationPostcondition(extra,backup),false);
  for(const key of ['database','readOnly','gooseVersion','profile','backupQualification','publicSequenceSafe']){const changed=structuredClone(actual);changed[key]='unknown';assert.equal(validTelegramNotificationsMigrationPostcondition(changed,backup),false,key);}
 }
});

test('schema046 требует exact045 и отсутствие незавершённых effect/claim/tasks',()=>{
 for(const backup of [false,true]){
  const actual=proof(backup,true);assert.equal(validTelegramNotificationsMigrationPrecondition(actual,backup),true);
  for(const key of activity){const changed=structuredClone(actual);changed[key]=1;assert.equal(validTelegramNotificationsMigrationPrecondition(changed,backup),false,key);}
  const changed=structuredClone(actual);changed.schemaCatalog.functionCount--;assert.equal(validTelegramNotificationsMigrationPrecondition(changed,backup),false);
  assert.equal(validTelegramNotificationsMigrationPrecondition({...actual,workspaceOwnerIdle:false},backup),false);
  assert.equal(immutableMigrationProfile('telegram-notifications'+(backup?'-backup':'')).version,'20261114140000');
  assert.equal(immutableMigrationProfile('telegram-notifications'+(backup?'-backup':'')).executionCommand,backup?'up-telegram-notifications-backup-maintenance':'up-telegram-notifications');
 }
});

test('table-only metadata не объявляется canonical и требует собственного exact catalog',()=>{
 for(const before of [false,true]){
  const actual=proof(true,before),validate=before?validTelegramNotificationsMigrationPrecondition:validTelegramNotificationsMigrationPostcondition;
  actual.backupQualification='TABLE_ONLY_BACKUP';assert.equal(validate(actual,true),false);
  actual.schemaCatalog=structuredClone(telegramNotificationsCatalog['table-only-backup'][before?'before':'after']);
  assert.equal(validate(actual,true),true);
  actual.backupQualification='CANONICAL_BACKUP';assert.equal(validate(actual,true),false);
 }
});
