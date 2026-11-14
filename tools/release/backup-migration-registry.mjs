import {githubRepositoryScopeCatalog} from './github-repository-scope-catalog.gen.mjs';
import {sentryObservabilityCatalog} from './sentry-observability-catalog.gen.mjs';

const profiles=Object.freeze({
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
