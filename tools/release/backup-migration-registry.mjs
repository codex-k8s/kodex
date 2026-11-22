import {workflowFileProvenanceMigration as workflowFile} from './workflow-file-provenance-migration-contract.mjs';
import {personalIntegrationsCatalog} from './personal-integrations-migration-contract.mjs';
import {telegramNotificationsCatalog} from './telegram-notifications-catalog.gen.mjs';
import {githubRepositoryScopeCatalog} from './github-repository-scope-catalog.gen.mjs';
import {sentryObservabilityCatalog} from './sentry-observability-catalog.gen.mjs';

const profiles=Object.freeze({
 [workflowFile.profile]:Object.freeze({profile:workflowFile.profile,before:workflowFile.before,after:workflowFile.after,
  receipt:workflowFile.receipt,files:Object.freeze({before:workflowFile.proofFile,after:workflowFile.proofFile})}),
 'personal-integrations-backup':Object.freeze({profile:'personal-integrations-backup',before:20261114140000,after:20261114170000,
  receipt:'Personal integrations backup migration completed at version 20261114170000',proofPrefix:'PERSONAL_INTEGRATIONS_BACKUP_',catalog:personalIntegrationsCatalog,
  files:Object.freeze({before:'personal-integrations-backup-migration-precondition.sql',after:'personal-integrations-backup-migration-postcondition.sql'})}),
 'telegram-notifications-backup':Object.freeze({profile:'telegram-notifications-backup',before:20261114130000,after:20261114140000,
  receipt:'Telegram notifications backup migration completed at version 20261114140000',proofPrefix:'TELEGRAM_NOTIFICATIONS_BACKUP_',catalog:telegramNotificationsCatalog,
  files:Object.freeze({before:'telegram-notifications-backup-migration-precondition.sql',after:'telegram-notifications-backup-migration-postcondition.sql'})}),
 'github-repository-scope-backup':Object.freeze({profile:'github-repository-scope-backup',before:20261113000100,after:20261114120000,
  receipt:'GitHub repository scope backup migration completed at version 20261114120000',
  proofPrefix:'GITHUB_REPOSITORY_SCOPE_BACKUP_',catalog:githubRepositoryScopeCatalog,
  files:Object.freeze({before:'github-repository-scope-backup-migration-precondition.sql',after:'github-repository-scope-backup-migration-postcondition.sql'})}),
 'sentry-observability-backup':Object.freeze({profile:'sentry-observability-backup',before:20261114120000,after:20261114130000,
  receipt:'Sentry observability backup migration completed at version 20261114130000',
  proofPrefix:'SENTRY_OBSERVABILITY_BACKUP_',catalog:sentryObservabilityCatalog,
  files:Object.freeze({before:'sentry-observability-backup-migration-precondition.sql',after:'sentry-observability-backup-migration-postcondition.sql'})}),
});

// Только historical044 plan без нового selector получает прежний044 scope.
// Неизвестный/пустой selector не превращается в допуск к следующей schema.
export function backupMigrationProfile(profile='github-repository-scope-backup'){
 const configuration=Object.hasOwn(profiles,profile)?profiles[profile]:undefined;
 if(!configuration)throw Error('BACKUP_MAINTENANCE_PROFILE_UNREGISTERED');
 return configuration;
}

export function requiresBackupMaintenance(profile){return Object.hasOwn(profiles,profile);}
