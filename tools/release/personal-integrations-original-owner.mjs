import {resolve,join} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {inspectSource} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';
import {resolvePersonalIntegrationsExecutorSource} from './personal-integrations-executor-source.mjs';
import {personalIntegrationsAuthorityPredecessorPins} from './personal-integrations-authority-predecessor.mjs';
import {isPersonalIntegrationsMigrationProfile} from './personal-integrations-migration-contract.mjs';

const diagnostic='PERSONAL_INTEGRATIONS_ORIGINAL_OWNER_SOURCE_REJECTED';
const fail=ok=>{if(!ok)throw Error(diagnostic);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const root=fileURLToPath(new URL('../../',import.meta.url)).replace(/\/$/,'');
const readerPath='tools/release/personal-integrations-authority-predecessor.mjs';

// Прежний immutable prerequisite перечитывается прежним fixed R/O module.
// Его executor scope не переписывается. Новый rotate/status scope принадлежит
// отдельному current executor и сохраняет собственную строгую проверку.
export async function readPersonalIntegrationsOriginalOwner107({migration,expected,...input},kube){
 const pinned=migration?.personalIntegrationsPrerequisites?.owner107,scope=pinned?.ownerScope,application=migration?.source;
 fail(isPersonalIntegrationsMigrationProfile(migration?.profile)&&pinned?.schema==='kodex.personal-integrations-authority-predecessor107/v1'&&
  pinned.sourceSHA===application?.revision&&scope?.sourceRevision===application.revision&&scope.executorRevision===application.revision&&
  input.source===application.path&&input.sourceSHA===application.revision&&input.context===migration.context&&input.kubeconfig===migration.kubeconfig&&
  typeof scope.executorSource==='string'&&scope.executorSource===resolve(scope.executorSource));
 fail(inspectSource(application.path).revision===application.revision&&inspectSource(scope.executorSource).revision===scope.executorRevision);
 const executorRevision=inspectSource(root).revision;
 if(executorRevision!==application.revision)resolvePersonalIntegrationsExecutorSource(root,{source:application.path,revision:application.revision});
 if(expected)fail(same(personalIntegrationsAuthorityPredecessorPins(expected),pinned));
 const original=await import(pathToFileURL(join(scope.executorSource,readerPath)).href);
 fail(typeof original.readPersonalIntegrationsAuthorityPredecessor==='function');
 const fresh=original.readPersonalIntegrationsAuthorityPredecessor({...input,expected:pinned},kube);
 fail(same(personalIntegrationsAuthorityPredecessorPins(fresh),pinned)&&
  inspectSource(application.path).revision===application.revision&&inspectSource(scope.executorSource).revision===scope.executorRevision&&
  inspectSource(root).revision===executorRevision);
 return fresh;
}
