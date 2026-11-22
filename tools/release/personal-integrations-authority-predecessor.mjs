import {fingerprint} from './scoped-release.mjs';
import {inspectSource} from './application-source.mjs';
import {readCatalogTarget,validateAuthorityPolicy,verifyCatalogTransition} from './authority-catalog-transition.mjs';
import {deriveRegistryRotationIdentity,validateRetiredCatalogOwnerState} from './authority-rotation-transition.mjs';
import {validateTelegramRegistry,telegramAuthorityReadbackContract} from './authority-telegram-transition.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {capturePersonalIntegrationsOwnerScope,readPersonalIntegrationsCurrentOwner} from './personal-integrations-authority-owner.mjs';
import {componentServingReady,readComponentServingInventory} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
export const personalIntegrationsPredecessorSchema='kodex.personal-integrations-authority-predecessor107/v1';

// Никакой новый status Job не создаётся. Фиксированный owner SQL должен сам
// подтвердить RETIRED точной текущей107 операции и её обязательных readers.
// Snapshot PROMOTED либо policy ConfigMap отдельно не являются таким proof.
export function readPersonalIntegrationsAuthorityPredecessor({source,sourceSHA,context,kubeconfig,targetProfile,expected},kube,io={}){
 fail(/^[a-f0-9]{40}$/.test(sourceSHA??'')&&(io.inspectSource??inspectSource)(source).revision===sourceSHA,
  'PERSONAL_INTEGRATIONS_PREDECESSOR_SOURCE_REJECTED');
 const get=(kind,name)=>JSON.parse(kube(['-n','kodex-system','get',kind,name,'-o','json']));
 const actual=readCatalogTarget(targetProfile,context,get,personalIntegrationsAuthorityTransition),{registry,publisher}=actual;
 const policy=validateAuthorityPolicy(registry.data?.['authority-policy.json'],'predecessor',actual.transition);
 const registered=validateTelegramRegistry(registry.data?.['key-delivery-targets.yaml'],'desired');
 const identity=deriveRegistryRotationIdentity(registry.data['key-delivery-targets.yaml']);
 const readers=telegramAuthorityReadbackContract();
 fail(policy.revision===107&&registered.sourceRevision===identity.sourceRevision,
  'PERSONAL_INTEGRATIONS_PREDECESSOR_REGISTRY_REJECTED');
 fail(componentServingReady(publisher,activeDeploymentPods(readComponentServingInventory(kube,'internal-rpc-authority-publisher',publisher.metadata.uid),publisher.metadata.uid),publisher.spec.replicas),
  'PERSONAL_INTEGRATIONS_PREDECESSOR_PUBLISHER_UNJOINED');
 const plan={source,revision:sourceSHA,context,kubeconfig,policyTransition:actual.transition};
 const scope=expected?.ownerScope??capturePersonalIntegrationsOwnerScope(plan,kube,io);
 const {state}=readPersonalIntegrationsCurrentOwner(scope,plan,kube,io);
 validateRetiredCatalogOwnerState(state,{ownerOperationID:identity.ownerOperationID},identity,readers,(io.now??Date.now)());
 const latest=readCatalogTarget(targetProfile,context,get,personalIntegrationsAuthorityTransition);
 verifyCatalogTransition(actual.transition,latest.transition);
 fail((io.inspectSource??inspectSource)(source).revision===sourceSHA&&same(latest.registry,registry)&&same(latest.publisher,publisher),
  'PERSONAL_INTEGRATIONS_PREDECESSOR_TARGET_CHANGED');
 const body={schema:personalIntegrationsPredecessorSchema,sourceSHA,ownerScope:scope,policyRevision:107,policySHA256:policy.sha256,
  registryUID:registry.metadata.uid,registryDataSHA256:fingerprint(registry.data),registryRevision:identity.sourceRevision,registrySHA256:identity.sourceDigestSHA256,
  publisherUID:publisher.metadata.uid,publisherSpecSHA256:fingerprint(publisher.spec),targetSHA256:actual.transition.targetSHA256,
  operationID:state.operationId,snapshotRevision:state.sourceRevision,snapshotSHA256:state.sourceDigestSHA256,...readers,
  actualReadbackCount:state.actualReadbackCount,observedAt:state.observedAt};
 const result={...body,predecessorSHA256:fingerprint(body)};
 if(expected)fail(samePersonalIntegrationsAuthorityPredecessor(result,expected),'PERSONAL_INTEGRATIONS_PREDECESSOR_CHANGED');
 return Object.freeze(result);
}

// Fresh observation и optional acknowledgements не меняют owner coordinates.
// Эта проекция не используется вместо нового read при выполнении effect.
export function samePersonalIntegrationsAuthorityPredecessor(before,after){
 return same(personalIntegrationsAuthorityPredecessorPins(before),personalIntegrationsAuthorityPredecessorPins(after));
}

// Только stable coordinates для планов. Вызвавший код получает их после fresh
// owner factory; DTO не заменяет SQL observation и не является grant.
export function personalIntegrationsAuthorityPredecessorPins(value){
 fail(value?.schema===personalIntegrationsPredecessorSchema,'PERSONAL_INTEGRATIONS_PREDECESSOR_PROJECTION_REJECTED');
 return Object.fromEntries(Object.entries(value).filter(([key])=>!['predecessorSHA256','observedAt','actualReadbackCount'].includes(key)));
}
