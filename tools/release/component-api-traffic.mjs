#!/usr/bin/env node
import {isWorkflowFileProvenanceMigrationProfile,workflowFileProvenanceMigration,workflowFileCancelledCloseAnnotation,workflowFileUnboundRestoreIntent} from './workflow-file-provenance-migration-contract.mjs';
import {readPersonalIntegrationsPinnedAuthority} from './personal-integrations-executor-source.mjs';
import {isPersonalIntegrationsMigrationProfile,requirePersonalIntegrationsDeliveryPrerequisite} from './personal-integrations-migration-contract.mjs';
import {execFileSync} from 'node:child_process';
import {resolve} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {fingerprint,withPrivatePatchFile} from './scoped-release.mjs';
import {catalogPolicyTransition,authorityPolicyTransitionConfiguration,authorityPolicyReadbackContract} from './authority-catalog-transition.mjs';
import {telegramPolicyTransition} from './authority-telegram-transition.mjs';
import {resolveTelegramExecutorSource} from './telegram-executor-source.mjs';
import {resolvePersonalIntegrationsExecutorSource,migrationInspectTimeoutMs} from './personal-integrations-executor-source.mjs';
import {readPrivateJSON} from '../../infra/public-ingress/edge-plan.mjs';
import {createPrivatePrettyJSONSync} from './private-json-write.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {readComponentServingInventory,componentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {confirmedAPIMaintenance,requireClosedAPIMaintenance} from './component-api-maintenance.mjs';
import {readCatalogAuthorityBaseline,validateCatalogAuthorityBaseline,sameCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';
import {isRoutineMigrationAPIRestoreProfile,validateRoutineMigrationIsolation} from './component-migration-api-admission.mjs';
import {personalTerminalInspectionExecutor} from './backup-controller-maintenance-cli.mjs';
import {backupControllerMaintenanceHeader} from './backup-controller-maintenance.mjs';
import {readWorkflowFileIsolationWitness} from './workflow-file-provenance-isolation-witness.mjs';
import {readMigrationAPITerminal,readPersonalIntegrationsAPITerminal,validateMigrationAPITerminal,sameMigrationAPITerminal} from './component-migration-api-terminal.mjs';
import {planWorkflowFileUnboundAPI,readWorkflowFileUnboundAPI,validateWorkflowFileUnboundAPI,workflowFileUnboundAPISchema} from './workflow-file-provenance-unbound-api.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)),name='control-api-gateway',key='kodex.dev/component-traffic-isolation';
const requireValue=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const digest=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const adoptionRejected='WORKFLOW_FILE_PROVENANCE_ISOLATION_HANDOFF_REJECTED';
const matchesService=(service,spec,annotations)=>same(service.spec,spec)&&same(service.metadata.annotations??{},annotations);

// Service сохраняет прежний selector на всю изоляцию. Новый maintenanceID
// относится к остановке Deployment и отражается только в annotation Service.
export function componentAPIIsolationNonce(plan){
  const nonce=plan.adoptedIsolation?.maintenanceID??plan.maintenanceID;
  requireValue(uuid(nonce),'COMPONENT_API_TRAFFIC_PLAN_INVALID');
  return nonce;
}
const baselinePolicyRevision=profile=>isWorkflowFileProvenanceMigrationProfile(profile)?110:106;
const header=plan=>({schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID});
const apiHeader=plan=>({schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID});
const marker=plan=>JSON.stringify({maintenanceID:plan.maintenanceID,closePlanSHA256:plan.closePlanSHA256,...(plan.authorityTransition?{authorityTransition:plan.authorityTransition}:{})});
const identity=(service,plan)=>service?.apiVersion==='v1'&&service.kind==='Service'&&service.metadata?.name===name&&
  service.metadata.namespace==='kodex-system'&&service.metadata.uid===plan.serviceUID&&!service.metadata.deletionTimestamp;
// Исторический source остаётся exact pin. Только routine observer допускает
// отдельно выбранный ancestor с полностью тем же Git tree после squash merge.
// Старые rotation/status Job и их source/image/plan bytes не меняются.
export function verifyAuthorityObserverSource(original,current,predecessor,ancestor,equivalence){
  requireValue(/^[a-f0-9]{40}$/.test(original??'')&&/^[a-f0-9]{40}$/.test(current??'')&&
    original===(predecessor??current),'COMPONENT_API_TRAFFIC_AUTHORITY_SOURCE_CHANGED');
  if(predecessor!==undefined){
    requireValue(predecessor!==current,'COMPONENT_API_TRAFFIC_AUTHORITY_SUCCESSOR_INVALID');
    if(equivalence!==undefined){
      requireValue(/^[a-f0-9]{40}$/.test(equivalence.revision??'')&&equivalence.revision!==original&&typeof equivalence.tree==='function',
        'COMPONENT_API_TRAFFIC_AUTHORITY_EQUIVALENT_INVALID');
      try{ancestor(equivalence.revision,current);}catch{throw Error('COMPONENT_API_TRAFFIC_AUTHORITY_EQUIVALENT_UNRELATED');}
      let originalTree,equivalentTree;
      try{originalTree=equivalence.tree(original);equivalentTree=equivalence.tree(equivalence.revision);}catch{throw Error('COMPONENT_API_TRAFFIC_AUTHORITY_EQUIVALENT_UNREADABLE');}
      requireValue(/^[a-f0-9]{40}$/.test(originalTree??'')&&originalTree===equivalentTree,
        'COMPONENT_API_TRAFFIC_AUTHORITY_EQUIVALENT_TREE_CHANGED');
    }else ancestor(predecessor,current);
  }else requireValue(equivalence===undefined,'COMPONENT_API_TRAFFIC_AUTHORITY_EQUIVALENT_INVALID');
}
// Новый status Job может подтвердить тот же неизменный owner outcome перед effect.
// Различаются только provenance наблюдения и его время, не authority/target pins.
export function sameAuthorityOutcome(left,right){
  const outcome=value=>Object.fromEntries(Object.entries(value??{}).filter(([key])=>
    !['terminalSHA256','observedAt','freshPlanSHA256','podUID','jobUID','actualReadbackCount'].includes(key)));
  return same(outcome(left),outcome(right));
}
function validateTerminal(proof,plan,now=Date.now()){
  if(proof?.schema===workflowFileUnboundAPISchema)return validateWorkflowFileUnboundAPI(proof,plan,now);
  if(plan.migrationRestoreProfile)return validateMigrationAPITerminal(proof,plan,now);
  const digest=v=>/^[a-f0-9]{64}$/.test(v??''),uuid=v=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v??'');
  requireValue(proof&&typeof proof==='object'&&!Array.isArray(proof),'COMPONENT_API_TRAFFIC_AUTHORITY_TERMINAL_REQUIRED');
  const {terminalSHA256,...body}=proof,observed=Date.parse(proof.observedAt),readers=authorityPolicyReadbackContract(plan.authorityTransition??catalogPolicyTransition);
  requireValue(proof.authorityTargetSHA256===undefined||plan.authorityTransition===telegramPolicyTransition&&
    digest(proof.authorityTargetSHA256)&&proof.authorityTargetSHA256!==proof.targetSHA256,
    'COMPONENT_API_TRAFFIC_AUTHORITY_TERMINAL_REQUIRED');
  const configuration=authorityPolicyTransitionConfiguration(plan.authorityTransition??catalogPolicyTransition);
  requireValue(proof.policyRevision===configuration.desiredRevision&&uuid(proof.operationID)&&proof.targetSHA256===fingerprint(plan.target)&&
    proof.trafficPlanSHA256===plan.previousIsolationSHA256&&proof.serviceUID===plan.serviceUID&&
    Object.entries(readers).every(([key,value])=>proof[key]===value)&&Number.isSafeInteger(proof.actualReadbackCount)&&proof.actualReadbackCount>=readers.expectedReadbackCount&&proof.actualReadbackCount<=384&&
    Number.isSafeInteger(proof.registryRevision)&&proof.registryRevision>0&&Number.isSafeInteger(proof.snapshotRevision)&&proof.snapshotRevision>0&&
    ['policySHA256','registrySHA256','snapshotSHA256','planSHA256','freshPlanSHA256'].every(k=>digest(proof[k]))&&
    observed<=now+5000&&now-observed<=30000&&digest(terminalSHA256)&&terminalSHA256===fingerprint(body),
    'COMPONENT_API_TRAFFIC_AUTHORITY_TERMINAL_REQUIRED');
}
function validate(plan){
  requireValue(!(plan?.migrationRestore||plan?.migrationRestoreProfile)||plan.authorityTransition===undefined,'COMPONENT_API_TRAFFIC_MIGRATION_PROFILE_INVALID');
  requireValue(plan?.authorityTransition===undefined||plan.authorityTransition===telegramPolicyTransition,'COMPONENT_API_TRAFFIC_AUTHORITY_PROFILE_INVALID');
  requireValue(plan?.schema==='kodex.component-api-traffic-plan/v1'&&['isolate','restore'].includes(plan.action)&&
    /^[a-f0-9]{40}$/.test(plan.sourceSHA??'')&&uuid(plan.maintenanceID)&&uuid(plan.serviceUID)&&/^[a-f0-9]{64}$/.test(plan.closePlanSHA256??'')&&
    plan.target?.schema==='kodex.portable-runner-target/v1'&&plan.target.resources?.some(r=>r.apiVersion==='v1'&&r.kind==='Service'&&r.name===name&&r.uid===plan.serviceUID)&&
    /^[0-9]+$/.test(plan.beforeResourceVersion??'')&&plan.beforeSpec&&plan.afterSpec&&plan.beforeAnnotations&&plan.afterAnnotations,
    'COMPONENT_API_TRAFFIC_PLAN_INVALID');
  const isolate=plan.action==='isolate',adopted=plan.adoptedIsolation;
  const original=structuredClone(isolate?plan.beforeSpec:plan.afterSpec);
  const originalAnnotations={...(isolate?plan.beforeAnnotations:plan.afterAnnotations)};
  if(adopted!==undefined){
    requireValue(adopted&&Object.keys(adopted).sort().join(',')==='artifactJobUID,closePlanSHA256,maintenanceID,migrationPlanSHA256,openPlanSHA256,planSHA256'&&
      uuid(adopted.artifactJobUID)&&uuid(adopted.maintenanceID)&&adopted.maintenanceID!==plan.maintenanceID&&
      ['closePlanSHA256','migrationPlanSHA256','openPlanSHA256','planSHA256'].every(field=>digest(adopted[field]))&&
      adopted.closePlanSHA256!==plan.closePlanSHA256&&
      (isolate?isWorkflowFileProvenanceMigrationProfile(plan.migrationRestore?.profile)&&plan.migrationRestore.earlyRecovery:
        isWorkflowFileProvenanceMigrationProfile(plan.migrationRestoreProfile)&&plan.authority?.migrationVersion===workflowFileProvenanceMigration.after),
      adoptionRejected);
    if(isolate){
      requireValue(original.selector?.[key]===adopted.maintenanceID&&originalAnnotations[key]===marker(adopted),adoptionRejected);
      delete original.selector[key];delete originalAnnotations[key];
    }
  }
  requireValue(original.type==='ClusterIP'&&original.selector?.['app.kubernetes.io/name']===name&&Object.keys(original.selector).length>0&&
    !Object.hasOwn(original.selector,key)&&!original.externalIPs?.length&&!original.externalName,
    'COMPONENT_API_TRAFFIC_SERVICE_INVALID');
  const expectedSpec=structuredClone(original);expectedSpec.selector={...original.selector,[key]:componentAPIIsolationNonce(plan)};
  requireValue(!(adopted&&isolate)||same(plan.beforeSpec,plan.afterSpec),adoptionRejected);
  requireValue(!Object.hasOwn(originalAnnotations,key)&&same(expectedSpec,isolate?plan.afterSpec:plan.beforeSpec)&&
    same({...originalAnnotations,[key]:marker(plan)},isolate?plan.afterAnnotations:plan.beforeAnnotations)&&
    (isolate||/^[a-f0-9]{64}$/.test(plan.previousIsolationSHA256??'')), 'COMPONENT_API_TRAFFIC_PLAN_INVALID');
  // Старый план остаётся читаемым после TTL; fresh bound проверяется перед
  // restore intent. Исторический readback не открывает admission повторно.
  if(plan.migrationRestore)validateRoutineMigrationIsolation(plan);
  requireValue(plan.migrationRestoreProfile===undefined||!isolate&&isRoutineMigrationAPIRestoreProfile(plan.migrationRestoreProfile)&&
    /^[a-f0-9]{40}$/.test(plan.migrationSourceSHA??'')&&!plan.migrationRestore,'COMPONENT_API_TRAFFIC_MIGRATION_PROFILE_INVALID');
  requireValue(plan.authorityExecutorSHA===undefined||(!isolate||!!plan.migrationRestore)&&/^[a-f0-9]{40}$/.test(plan.authorityExecutorSHA)&&plan.authorityExecutorSHA!==plan.sourceSHA,'COMPONENT_API_TRAFFIC_AUTHORITY_SUCCESSOR_INVALID');
  requireValue(plan.authoritySourceEquivalentSHA===undefined||!!(plan.migrationRestore||plan.migrationRestoreProfile)&&
    /^[a-f0-9]{40}$/.test(plan.authoritySourceEquivalentSHA)&&!!plan.authorityExecutorSHA&&plan.authoritySourceEquivalentSHA!==plan.authorityExecutorSHA,
    'COMPONENT_API_TRAFFIC_AUTHORITY_EQUIVALENT_INVALID');
  if(!isolate)validateTerminal(plan.authority,plan,Date.parse(plan.authority?.observedAt));
  else requireValue(!Object.hasOwn(plan,'authority'),'COMPONENT_API_TRAFFIC_PLAN_INVALID');
}

// Проверяется отсутствие любых адресов, включая not-ready и terminating.
// Пустой discovery не доказывает завершение прежних TCP sessions: для них
// отдельно нужен положительный join API из исторического maintenance receipt.
export function requireIsolatedAPIService(service,slices,pods,plan){
  validate(plan);requireValue(plan.action==='isolate'&&identity(service,plan)&&same(service.spec,plan.afterSpec)&&
    same(service.metadata.annotations??{},plan.afterAnnotations),'COMPONENT_API_TRAFFIC_NOT_ISOLATED');
  requireValue(Array.isArray(slices?.items)&&!slices.metadata?.continue&&slices.items.every(slice=>
    slice.apiVersion==='discovery.k8s.io/v1'&&slice.kind==='EndpointSlice'&&slice.metadata?.namespace==='kodex-system'&&
    slice.metadata.labels?.['kubernetes.io/service-name']===name&&slice.metadata.ownerReferences?.some(o=>o.kind==='Service'&&o.uid===plan.serviceUID&&o.controller===true)&&
    (slice.endpoints===null||Array.isArray(slice.endpoints)&&slice.endpoints.length===0)),'COMPONENT_API_TRAFFIC_ENDPOINTS_REMAIN');
  requireValue(Array.isArray(pods?.items)&&!pods.metadata?.continue&&pods.items.every(p=>p.kind==='Pod'&&p.metadata?.namespace==='kodex-system'&&
    !Object.entries(plan.afterSpec.selector).every(([k,v])=>p.metadata.labels?.[k]===v)), 'COMPONENT_API_TRAFFIC_SELECTOR_MATCHES_POD');
}
function confirmedIsolation(plan,events){
  validate(plan);requireValue(plan.action==='isolate'&&Array.isArray(events)&&events[0]?.type==='HEADER'&&
    Object.entries(header(plan)).every(([k,v])=>events[0][k]===v),'COMPONENT_API_TRAFFIC_JOURNAL_INVALID');
  const intent=events.findLastIndex(e=>e.type==='INTENT'&&e.step==='apply');
  requireValue(intent>0&&events.some((e,i)=>i>intent&&e.type==='OBSERVED'&&e.state==='ISOLATED'&&e.serviceUID===plan.serviceUID&&
    e.maintenanceID===plan.maintenanceID),'COMPONENT_API_TRAFFIC_UNCONFIRMED');
}
export function requireClosedAPIAdmission(deployment,resources,closed,traffic){
  if(!traffic)return requireClosedAPIMaintenance(deployment,resources,closed);
  const proof=confirmedAPIMaintenance(closed),{plan,events,service,slices,pods}=traffic;
  requireValue(plan.closePlanSHA256===proof.planSHA256&&plan.maintenanceID===proof.maintenanceID&&
    same(plan.target,closed.plan.target)&&deployment?.metadata?.uid===proof.deploymentUID&&deployment.metadata.name===name&&
    deployment.metadata.namespace==='kodex-system'&&!deployment.metadata.deletionTimestamp,'COMPONENT_API_TRAFFIC_SCOPE_INVALID');
  confirmedIsolation(plan,events);requireIsolatedAPIService(service,slices,pods,plan);
  return {...proof,trafficPlanSHA256:fingerprint(plan),serviceUID:plan.serviceUID,...(plan.authorityTransition?{authorityTransition:plan.authorityTransition}:{})};
}
export function planComponentAPITraffic({service,deployment,resources,target,observed,sourceSHA,action,closed,previous,terminal,authorityTransition,authorityExecutorSHA,authoritySourceEquivalentSHA,migrationRestore,migrationRestoreProfile,migrationSourceSHA,handoffWitness}){
 if(action==='restore')requirePersonalIntegrationsDeliveryPrerequisite(migrationRestoreProfile,'restore-api-traffic');
  const profile=authorityTransition??previous?.plan?.authorityTransition??catalogPolicyTransition;
  authorityPolicyTransitionConfiguration(profile);
  requireValue(!(migrationRestore||migrationRestoreProfile)||profile===catalogPolicyTransition,
    'COMPONENT_API_TRAFFIC_MIGRATION_PROFILE_INVALID');
  requireValue(action!=='restore'||profile===(previous?.plan?.authorityTransition??catalogPolicyTransition),'COMPONENT_API_TRAFFIC_AUTHORITY_PROFILE_INVALID');
  const scope=portableRunnerTarget(target,observed),proof=confirmedAPIMaintenance(closed);
  requireValue(scope.allows(service)&&scope.allows(deployment)&&same(target,closed.plan.target)&&/^[a-f0-9]{40}$/.test(sourceSHA??'')&&
    ['isolate','restore'].includes(action),'COMPONENT_API_TRAFFIC_SCOPE_INVALID');
  let adoptedIsolation;
  if(action==='isolate'){
    requireValue(service.metadata.annotations?.[workflowFileCancelledCloseAnnotation]!==proof.maintenanceID,
      'COMPONENT_API_TRAFFIC_CLOSE_CANCELLED');
    requireClosedAPIMaintenance(deployment,resources,closed);
    if(handoffWitness!==undefined){
      const predecessor=previous?.plan,oldScope=predecessor&&portableRunnerTarget(predecessor.target,observed);
      requireValue(predecessor?.action==='isolate'&&!predecessor.migrationRestore&&!predecessor.adoptedIsolation&&!predecessor.authorityTransition&&
        oldScope.allows(service)&&oldScope.allows(deployment)&&closed.plan.sourceSHA===sourceSHA&&
        same(deployment.spec,closed.plan.afterSpec)&&same(deployment.metadata.annotations??{},closed.plan.afterAnnotations),adoptionRejected);
      confirmedIsolation(predecessor,previous.events);
      requireIsolatedAPIService(service,previous.slices,previous.pods,predecessor);
      adoptedIsolation={...handoffWitness,planSHA256:fingerprint(predecessor),maintenanceID:predecessor.maintenanceID,closePlanSHA256:predecessor.closePlanSHA256};
    }else requireValue(!previous,adoptionRejected);
  }
  else{
    requireValue(handoffWitness===undefined,adoptionRejected);
    adoptedIsolation=previous?.plan?.adoptedIsolation;
    requireClosedAPIAdmission(deployment,resources,closed,{...previous,service});
    if(migrationRestoreProfile)requireValue(terminal?.deploymentUID===deployment.metadata.uid&&terminal.apiSpecSHA256===fingerprint(deployment.spec),
      'COMPONENT_API_TRAFFIC_MIGRATION_API_SPEC_CHANGED');
    if(terminal?.schema===workflowFileUnboundAPISchema){
      const early=previous.plan.migrationRestore?.earlyRecovery;
      requireValue(early&&terminal.backup?.stopPlanSHA256===early.backupMaintenance.planSHA256&&same(terminal.controlPlane,early.controlPlane),
        'WORKFLOW_FILE_PROVENANCE_UNBOUND_API_TERMINAL_REQUIRED');
    }
    requireValue(componentServingReady(deployment,
      activeDeploymentPods(componentServingInventory(resources,name,deployment.metadata.uid),deployment.metadata.uid),deployment.spec?.replicas),
      'COMPONENT_API_TRAFFIC_READY_API_REQUIRED');
  }
  const plan={schema:'kodex.component-api-traffic-plan/v1',action,sourceSHA,target,maintenanceID:proof.maintenanceID,closePlanSHA256:proof.planSHA256,
    ...(profile===telegramPolicyTransition?{authorityTransition:profile}:{}),
    serviceUID:service.metadata.uid,beforeResourceVersion:service.metadata.resourceVersion,beforeSpec:structuredClone(service.spec),afterSpec:structuredClone(service.spec),
    beforeAnnotations:{...service.metadata.annotations},afterAnnotations:{...service.metadata.annotations},
    ...(adoptedIsolation?{adoptedIsolation}:{}),
    ...(authoritySourceEquivalentSHA?{authoritySourceEquivalentSHA}:{}),
    ...(action==='restore'?{previousIsolationSHA256:fingerprint(previous.plan),authority:terminal,
      ...(migrationRestoreProfile?{migrationRestoreProfile,migrationSourceSHA}:{}),...(authorityExecutorSHA?{authorityExecutorSHA}:{})}:
      migrationRestore?{migrationRestore,...(authorityExecutorSHA?{authorityExecutorSHA}:{})}:{})};
  const personalRestore=isPersonalIntegrationsMigrationProfile(migrationRestoreProfile)&&previous?.plan.action==='isolate'&&
    !previous.plan.migrationRestore&&!previous.plan.authorityTransition&&terminal?.sourceSHA===migrationSourceSHA;
  requireValue(action==='isolate'?!migrationRestoreProfile&&!migrationSourceSHA:
    (personalRestore||!previous.plan.migrationRestore&&!migrationRestoreProfile||previous.plan.migrationRestore?.profile===migrationRestoreProfile)&&!migrationRestore,
    'COMPONENT_API_TRAFFIC_MIGRATION_PROFILE_INVALID');
  if(action==='isolate'){
    plan.afterSpec.selector[key]=componentAPIIsolationNonce(plan);plan.afterAnnotations[key]=marker(plan);
  }else{delete plan.afterSpec.selector[key];delete plan.afterAnnotations[key];}
  validate(plan);if(action==='restore')validateTerminal(terminal,plan);return plan;
}
export function componentAPITrafficStep({phase,plan,journal,get,slices,pods,gate,patch}){
  if(phase==='apply'&&plan?.action==='restore')requirePersonalIntegrationsDeliveryPrerequisite(plan.migrationRestoreProfile,'restore-api-traffic');
  validate(plan);requireValue(['apply','status'].includes(phase),'COMPONENT_API_TRAFFIC_PHASE_INVALID');
  const service=get();requireValue(identity(service,plan),'COMPONENT_API_TRAFFIC_UID_CHANGED');
  const before=matchesService(service,plan.beforeSpec,plan.beforeAnnotations);
  const after=matchesService(service,plan.afterSpec,plan.afterAnnotations);
  requireValue(before||after,'COMPONENT_API_TRAFFIC_DRIFT');
  let state=before?'BEFORE':plan.action==='restore'?'RESTORED':'CONVERGING';
  if(after&&plan.action==='isolate'){
    try{requireIsolatedAPIService(service,slices(),pods(),plan);state='ISOLATED';}
    catch(error){if(!['COMPONENT_API_TRAFFIC_ENDPOINTS_REMAIN','COMPONENT_API_TRAFFIC_SELECTOR_MATCHES_POD'].includes(error.message))throw error;}
  }
  const observation={type:'OBSERVED',state,serviceUID:plan.serviceUID,maintenanceID:plan.maintenanceID,resourceVersion:service.metadata.resourceVersion};
  if(phase==='status'){journal.append(observation);return observation;}
  requireValue(plan.action!=='isolate'||!journal.events.some(event=>event.type===workflowFileUnboundRestoreIntent),
    'WORKFLOW_FILE_PROVENANCE_ISOLATION_CLOSED');
  const intent=journal.events.findLastIndex(e=>e.type==='INTENT'&&e.step==='apply');
  if(after){requireValue(intent>0,'COMPONENT_API_TRAFFIC_UNOWNED_STATE');journal.append(observation);return observation;}
  if(intent>=0)requireValue(journal.events.some((e,i)=>i>intent&&e.type==='OBSERVED'&&e.state==='BEFORE'&&e.resourceVersion===service.metadata.resourceVersion),
    'COMPONENT_API_TRAFFIC_OUTCOME_UNKNOWN');
  else requireValue(service.metadata.resourceVersion===plan.beforeResourceVersion,'COMPONENT_API_TRAFFIC_VERSION_CHANGED');
  const operations=[{op:'test',path:'/metadata/uid',value:plan.serviceUID},{op:'test',path:'/metadata/resourceVersion',value:service.metadata.resourceVersion},
    {op:'test',path:'/spec',value:plan.beforeSpec},{op:'add',path:'/metadata/annotations',value:plan.afterAnnotations},
    ...(plan.adoptedIsolation&&plan.action==='isolate'?[]:[{op:'replace',path:'/spec',value:plan.afterSpec}])];
  patch(operations,true);const terminal=gate();
  if(plan.action==='restore'){
    validateTerminal(terminal,plan);requireValue((plan.migrationRestoreProfile?sameMigrationAPITerminal:sameAuthorityOutcome)(terminal,plan.authority),'COMPONENT_API_TRAFFIC_AUTHORITY_CHANGED');
    journal.append({type:plan.migrationRestoreProfile?'MIGRATION_CONFIRMED':'AUTHORITY_CONFIRMED',plannedTerminalSHA256:plan.authority.terminalSHA256,authority:terminal});
  }else if(plan.migrationRestore){
    validateCatalogAuthorityBaseline(terminal,Date.now(),baselinePolicyRevision(plan.migrationRestore.profile));
    requireValue(sameCatalogAuthorityBaseline(plan.migrationRestore.authority,terminal),'COMPONENT_API_TRAFFIC_AUTHORITY_CHANGED');
    journal.append({type:'BASELINE_CONFIRMED',baseline:terminal});
  }
  journal.append({type:'INTENT',step:'apply'});
  try{patch(operations,false);}catch(error){journal.append({type:'UNKNOWN',step:'apply'});throw error;}
  return componentAPITrafficStep({phase:'status',plan,journal,get,slices,pods,gate,patch});
}

async function main(args){
  const phase=args.shift(),options={};
  while(args.length){const k=args.shift();requireValue(['--action','--target','--plan','--evidence','--closed-plan','--closed-evidence',
    '--isolation-plan','--isolation-evidence','--authority-plan','--authority-evidence','--authority-status-plan','--authority-status-evidence',
    '--approved-plan-sha','--kubeconfig','--context','--predecessor-executor-sha','--policy-transition','--authority-executor-sha','--authority-source-equivalent-sha','--migration-profile','--migration-source-sha','--migration-plan','--migration-evidence','--workflow-file-backup-stop-plan',
    '--workflow-file-isolation-evidence','--workflow-file-unbound','--adopt-personal-isolation','--api-open-plan','--api-open-evidence','--application-source','--application-revision','--telegram-port-repair-plan','--telegram-port-repair-executor-sha'].includes(k)&&!Object.hasOwn(options,k)&&args.length,
    'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');options[k]=args.shift();}
  requireValue(['plan','apply','status'].includes(phase)&&options['--plan']&&options['--closed-plan']&&options['--closed-evidence']&&
    options['--kubeconfig']?.startsWith('/')&&!/[\r\n\0]/.test(options['--kubeconfig'])&&/^[A-Za-z0-9_.:@/-]{1,160}$/.test(options['--context']??''),
    'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');
  requireValue(options['--predecessor-executor-sha']===undefined||phase==='status'&&/^[a-f0-9]{40}$/.test(options['--predecessor-executor-sha']),
    'COMPONENT_API_TRAFFIC_READBACK_SUCCESSOR_INVALID');
  requireValue(options['--authority-executor-sha']===undefined||phase==='plan'&&(options['--action']==='restore'||options['--action']==='isolate'&&isRoutineMigrationAPIRestoreProfile(options['--migration-profile']))&&/^[a-f0-9]{40}$/.test(options['--authority-executor-sha']),'COMPONENT_API_TRAFFIC_AUTHORITY_SUCCESSOR_INVALID');
  let authorityExecutorSHA=options['--authority-executor-sha'];
  let authoritySourceEquivalentSHA=options['--authority-source-equivalent-sha'];
  let migrationProfile=options['--migration-profile'],migrationSourceSHA=options['--migration-source-sha'],routineIsolation;
  requireValue(migrationProfile===undefined||phase==='plan'&&isRoutineMigrationAPIRestoreProfile(migrationProfile),
    'COMPONENT_API_TRAFFIC_MIGRATION_PROFILE_INVALID');
  requireValue(!isPersonalIntegrationsMigrationProfile(migrationProfile)||options['--action']==='restore'&&
    !authorityExecutorSHA&&!authoritySourceEquivalentSHA,'PERSONAL_INTEGRATIONS_API_RESTORE_PROFILE_REJECTED');
  requireValue(authoritySourceEquivalentSHA===undefined||phase==='plan'&&isRoutineMigrationAPIRestoreProfile(migrationProfile)&&
    /^[a-f0-9]{40}$/.test(authoritySourceEquivalentSHA)&&!!authorityExecutorSHA,
    'COMPONENT_API_TRAFFIC_AUTHORITY_EQUIVALENT_INVALID');
  requireValue(migrationSourceSHA===undefined||phase==='plan'&&options['--action']==='restore'&&migrationProfile&&/^[a-f0-9]{40}$/.test(migrationSourceSHA),
    'COMPONENT_API_TRAFFIC_MIGRATION_SOURCE_INVALID');
  const env=Object.fromEntries(['HOME','PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
  const run=(command,argv,input)=>{try{return execFileSync(command,argv,{cwd:root,env,input,encoding:'utf8',timeout:35000,maxBuffer:8<<20,stdio:[input===undefined?'ignore':'pipe','pipe','pipe']});}
    catch{throw Error('COMPONENT_API_TRAFFIC_OPERATION_FAILED_OR_UNKNOWN');}};
  const kube=(argv,input)=>run('kubectl',['--kubeconfig',options['--kubeconfig'],'--context',options['--context'],'--request-timeout=25s',...argv],input);
  const get=(kind,n)=>JSON.parse(kube(['-n','kodex-system','get',kind,n,'-o','json']));
  const retained=phase==='plan'?undefined:await readPrivateJSON(resolve(options['--plan']));
  if(retained)validate(retained);
  const earlyStopPath=options['--workflow-file-backup-stop-plan'],earlyEvidencePath=options['--workflow-file-isolation-evidence'];
  requireValue(earlyStopPath===undefined&&earlyEvidencePath===undefined||earlyStopPath?.startsWith('/')&&earlyEvidencePath?.startsWith('/')&&
    phase==='plan'&&options['--action']==='isolate'&&isWorkflowFileProvenanceMigrationProfile(migrationProfile),
    'WORKFLOW_FILE_PROVENANCE_EARLY_RECOVERY_SCOPE_CHANGED');
  requireValue(options['--workflow-file-unbound']===undefined||options['--workflow-file-unbound']==='true'&&phase==='plan'&&
    options['--action']==='restore'&&isWorkflowFileProvenanceMigrationProfile(migrationProfile),
    'WORKFLOW_FILE_PROVENANCE_EARLY_RECOVERY_SCOPE_CHANGED');
  const adoptionFlag=options['--adopt-personal-isolation'];
  requireValue(adoptionFlag===undefined||adoptionFlag==='true'&&phase==='plan'&&options['--action']==='isolate'&&
    isWorkflowFileProvenanceMigrationProfile(migrationProfile)&&earlyStopPath&&earlyEvidencePath,adoptionRejected);
  const adopting=adoptionFlag==='true'||retained?.action==='isolate'&&!!retained.adoptedIsolation;
  requireValue(!adopting||phase==='status'||['--isolation-plan','--isolation-evidence','--api-open-plan','--api-open-evidence']
    .every(key=>options[key]?.startsWith('/')&&!/[\r\n\0]/.test(options[key])),adoptionRejected);
  const unbound=options['--workflow-file-unbound']==='true'||retained?.authority?.schema===workflowFileUnboundAPISchema;
  requireValue(!unbound||!options['--migration-plan']&&!options['--migration-evidence'],'COMPONENT_API_TRAFFIC_MIGRATION_ARGUMENT_INVALID');
  const selected=options['--application-source']!==undefined||options['--application-revision']!==undefined;
  requireValue(!options['--telegram-port-repair-executor-sha']||selected&&options['--telegram-port-repair-plan']||options['--telegram-port-repair-executor-sha']===retained?.sourceCompatibility?.repairExecutor?.executorRevision,'TELEGRAM_EXECUTOR_SOURCE_REJECTED');
  requireValue(!options['--telegram-port-repair-plan']||selected||options['--telegram-port-repair-plan']===retained?.sourceCompatibility?.telegramPortRepair?.planPath,'TELEGRAM_EXECUTOR_SOURCE_REJECTED');
  const profile=options['--policy-transition']??retained?.authorityTransition;
  const personalExecutorProfile=isPersonalIntegrationsMigrationProfile(options['--migration-profile']??retained?.migrationRestoreProfile);
  requireValue(!personalExecutorProfile||!options['--telegram-port-repair-plan']&&!options['--telegram-port-repair-executor-sha'],'PERSONAL_INTEGRATIONS_EXECUTOR_SOURCE_REJECTED');
  requireValue(!selected||phase==='plan'&&options['--action']==='restore'&&(profile===telegramPolicyTransition||personalExecutorProfile),'TELEGRAM_EXECUTOR_SOURCE_REJECTED');
  requireValue(!retained?.sourceCompatibility||retained.action==='restore'&&(profile===telegramPolicyTransition||personalExecutorProfile),'TELEGRAM_EXECUTOR_SOURCE_REJECTED');
  const sourceCompatibility=selected||retained?.sourceCompatibility?(personalExecutorProfile?resolvePersonalIntegrationsExecutorSource:resolveTelegramExecutorSource)(root,
    retained?.sourceCompatibility?{saved:retained.sourceCompatibility}:{source:options['--application-source'],revision:options['--application-revision'],repairPlanPath:options['--telegram-port-repair-plan'],repairExecutorRevision:options['--telegram-port-repair-executor-sha']}):undefined;
  const sourceSHA=sourceCompatibility?.revision??run('git',['rev-parse','HEAD']).trim();requireValue(!run('git',['status','--porcelain','--untracked-files=normal']).trim(),
    'COMPONENT_API_TRAFFIC_CLEAN_SOURCE_REQUIRED');
  const readEvidence=(path,expected)=>{const j=privateJournal(path,expected,{readOnly:true});try{return structuredClone(j.events);}finally{j.close();}};
  const closePlan=await readPrivateJSON(resolve(options['--closed-plan']));
  const closed={plan:closePlan,events:readEvidence(options['--closed-evidence'],apiHeader(closePlan))};
  const service=()=>get('service',name),deployment=()=>get('deployment',name);
  const slices=()=>JSON.parse(kube(['-n','kodex-system','get','endpointslices','-l',`kubernetes.io/service-name=${name}`,'-o','json']));
  const pods=plan=>JSON.parse(kube(['-n','kodex-system','get','pods','-l',`${key}=${componentAPIIsolationNonce(plan)}`,'-o','json']));
  const observed={context:options['--context'],clusterUID:JSON.parse(kube(['get','namespace','kube-system','-o','json'])).metadata.uid,
    namespaceUID:JSON.parse(kube(['get','namespace','kodex-system','-o','json'])).metadata.uid,installation:get('configmap','kodex-installation-profile')};
  let previous;
  if(options['--isolation-plan']){
    requireValue(options['--isolation-evidence'],'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');
    const plan=await readPrivateJSON(resolve(options['--isolation-plan']));previous={plan,events:readEvidence(options['--isolation-evidence'],header(plan))};
  }else requireValue(!options['--isolation-evidence'],'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');
  let terminal,verifiedPersonalAPI,rotationPlan,handoffWitness;
  const readBaseline=expected=>{
    requireValue(options['--authority-plan']&&options['--authority-evidence']&&!options['--authority-status-plan']&&!options['--authority-status-evidence'],
      'COMPONENT_API_TRAFFIC_BASELINE_INPUT_REQUIRED');
    const plan=readAuthorityPrivateJSON(options['--authority-plan']);
    requireValue(plan.context===options['--context']&&plan.kubeconfig===options['--kubeconfig'],'COMPONENT_API_TRAFFIC_AUTHORITY_SOURCE_CHANGED');
    verifyAuthorityObserverSource(plan.revision,sourceSHA,authorityExecutorSHA,
      (old,current)=>run('git',['merge-base','--is-ancestor',old,current]),
      authoritySourceEquivalentSHA===undefined?undefined:{revision:authoritySourceEquivalentSHA,
        tree:revision=>run('git',['rev-parse',`${revision}^{tree}`]).trim()});
    rotationPlan=plan;
    return readCatalogAuthorityBaseline({plan,events:readAuthorityPrivateText(options['--authority-evidence']).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON),expected,
      policyRevision:baselinePolicyRevision(migrationProfile)},kube);
  };
  const readHandoff=async earlyRecovery=>{
    requireValue(previous&&earlyRecovery,adoptionRejected);
    confirmedIsolation(previous.plan,previous.events);
    requireIsolatedAPIService(service(),slices(),pods(previous.plan),previous.plan);
    const previousPlanPath=resolve(options['--isolation-plan']),previousEvidencePath=resolve(options['--isolation-evidence']);
    requireValue(earlyRecovery.isolationEvidencePath!==previousEvidencePath&&earlyRecovery.backupMaintenance.evidencePath!==previousEvidencePath,
      adoptionRejected);
    return readWorkflowFileIsolationWitness({rotation:rotationPlan,authority:terminal,previous,previousPlanPath,previousEvidencePath,closed,
      openPlanPath:resolve(options['--api-open-plan']),openEvidencePath:resolve(options['--api-open-evidence']),earlyRecovery,kube});
  };
  const readTerminal=async()=>{
    if(unbound){
      const prior=previous?.plan,early=prior?.migrationRestore?.earlyRecovery;
      requireValue(early&&!prior.adoptedIsolation&&isWorkflowFileProvenanceMigrationProfile(migrationProfile)&&migrationSourceSHA===sourceSHA&&
        prior.sourceSHA===sourceSHA&&early.isolationEvidencePath===resolve(options['--isolation-evidence'])&&
        options['--api-open-plan']&&options['--api-open-evidence'],'WORKFLOW_FILE_PROVENANCE_EARLY_RECOVERY_SCOPE_CHANGED');
      const open=readAuthorityPrivateJSON(options['--api-open-plan']);
      return readWorkflowFileUnboundAPI({kube,observed,closed,opened:{plan:open,events:readEvidence(options['--api-open-evidence'],apiHeader(open))},
        isolation:{...previous,service:service(),slices:slices(),pods:pods(previous.plan)},readAuthority:expected=>readBaseline(expected)});
    }
    if(migrationProfile){
      if(isPersonalIntegrationsMigrationProfile(migrationProfile)){
        const refs=['--migration-plan','--migration-evidence','--api-open-plan','--api-open-evidence','--authority-plan',
          '--authority-evidence','--authority-status-plan','--authority-status-evidence'];
        requireValue(refs.every(key=>options[key])&&previous?.plan.action==='isolate'&&
          !previous.plan.migrationRestore&&!previous.plan.authorityTransition&&migrationSourceSHA===sourceSHA,
          'PERSONAL_INTEGRATIONS_API_TERMINAL_INPUT_REQUIRED');
        const migration=readAuthorityPrivateJSON(options['--migration-plan']);
        requireValue(migration.profile===migrationProfile&&migration.source?.revision===migrationSourceSHA&&
          migration.context===options['--context']&&migration.kubeconfig===options['--kubeconfig']&&
          migration.apiMaintenance?.isolationPlanSHA256===fingerprint(previous.plan),
          'COMPONENT_API_TRAFFIC_MIGRATION_SOURCE_CHANGED');
        const openPlanPath=resolve(options['--api-open-plan']),openEvidencePath=resolve(options['--api-open-evidence']);
        const migrationEvidencePath=resolve(options['--migration-evidence']);
        const events=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
        let inspectionExecutor;
        return readPersonalIntegrationsAPITerminal({migration,migrationEvidencePath,openPlanPath,openEvidencePath,
          inspect:record=>{
            inspectionExecutor=personalTerminalInspectionExecutor(record.plan,record.events)??sourceCompatibility;
            const cli=inspectionExecutor?resolve(inspectionExecutor.executorSource,'tools/release/control-plane-migration.mjs'):
              new URL('./control-plane-migration.mjs',import.meta.url).pathname;
            const args=[cli,'inspect','--profile',migrationProfile,'--context',options['--context'],'--kubeconfig',options['--kubeconfig'],
              '--plan',resolve(options['--migration-plan']),'--evidence',migrationEvidencePath,'--evidence-read-only','true',
              '--api-open-plan',openPlanPath,'--api-open-evidence',openEvidencePath,
              ...(inspectionExecutor?['--personal-executor-sha',inspectionExecutor.executorRevision]:[])];
            try{return parseAuthorityPrivateJSON(execFileSync(process.execPath,args,{cwd:root,env,encoding:'utf8',
              timeout:migrationInspectTimeoutMs(migrationProfile),maxBuffer:4<<20,stdio:['ignore','pipe','pipe']}));}
            catch{throw Error('COMPONENT_API_TRAFFIC_MIGRATION_INSPECT_UNCONFIRMED');}
          },
          readAuthority:record=>readPersonalIntegrationsPinnedAuthority({plan:readAuthorityPrivateJSON(options['--authority-plan']),events:events(options['--authority-evidence']),
            freshPlan:readAuthorityPrivateJSON(options['--authority-status-plan']),freshEvents:events(options['--authority-status-evidence']),migration:record},
            kube,{purpose:'terminal',executor:inspectionExecutor}),
          onVerifiedAPI:snapshot=>{verifiedPersonalAPI=snapshot;}},kube);
      }
      requireValue(['--migration-plan','--migration-evidence','--api-open-plan','--api-open-evidence'].every(key=>options[key])&&
        previous?.plan.migrationRestore?.profile===migrationProfile&&/^[a-f0-9]{40}$/.test(migrationSourceSHA??''),
        'COMPONENT_API_TRAFFIC_MIGRATION_TERMINAL_INPUT_REQUIRED');
      const migration=readAuthorityPrivateJSON(options['--migration-plan']);
      requireValue(migration.profile===migrationProfile&&migration.source?.revision===migrationSourceSHA&&migration.context===options['--context'],
        'COMPONENT_API_TRAFFIC_MIGRATION_SOURCE_CHANGED');
      const args=[new URL('./control-plane-migration.mjs',import.meta.url).pathname,'inspect','--profile',migrationProfile,
        '--context',options['--context'],'--kubeconfig',options['--kubeconfig'],'--plan',resolve(options['--migration-plan']),
        '--evidence',resolve(options['--migration-evidence']),'--api-restoration-open-plan',resolve(options['--api-open-plan']),
        '--api-restoration-open-evidence',resolve(options['--api-open-evidence']),'--api-restoration-source-sha',migrationSourceSHA];
      return readMigrationAPITerminal({migration,migrationEvidencePath:resolve(options['--migration-evidence']),
        openPlanPath:resolve(options['--api-open-plan']),openEvidencePath:resolve(options['--api-open-evidence']),
        inspect:async()=>{try{return parseAuthorityPrivateJSON(execFileSync(process.execPath,args,{cwd:root,env,encoding:'utf8',timeout:120000,maxBuffer:4<<20,stdio:['ignore','pipe','pipe']}));}
          catch{throw Error('COMPONENT_API_TRAFFIC_MIGRATION_INSPECT_UNCONFIRMED');}},
        readAuthority:expected=>readBaseline(expected)},kube);
    }
    requireValue(['--authority-plan','--authority-evidence','--authority-status-plan','--authority-status-evidence'].every(k=>options[k]),
      'COMPONENT_API_TRAFFIC_AUTHORITY_TERMINAL_REQUIRED');
    const {readCatalogAuthorityTerminal,readTelegramAuthorityTerminal}=await import('./authority-rotation-transition.mjs');
    const profile=previous?.plan?.authorityTransition??catalogPolicyTransition;
    const readTerminal=profile===telegramPolicyTransition?readTelegramAuthorityTerminal:readCatalogAuthorityTerminal;
    requireValue(typeof readTerminal==='function','COMPONENT_API_TRAFFIC_AUTHORITY_TERMINAL_REQUIRED');
    const plan=readAuthorityPrivateJSON(options['--authority-plan']),freshPlan=readAuthorityPrivateJSON(options['--authority-status-plan']);
    requireValue(plan.policyTransition?.profile===profile&&plan.context===options['--context']&&plan.kubeconfig===options['--kubeconfig'],
      'COMPONENT_API_TRAFFIC_AUTHORITY_SOURCE_CHANGED');
    verifyAuthorityObserverSource(plan.revision,sourceSHA,authorityExecutorSHA,
      (old,current)=>run('git',['merge-base','--is-ancestor',old,current]));
    return readTerminal({plan,freshPlan,
      events:readAuthorityPrivateText(options['--authority-evidence']).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON),
      freshEvents:readAuthorityPrivateText(options['--authority-status-evidence']).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON),
      // Getter сам читает актуальный admission и сверяет с исходным proof.
      // Второй полный inventory здесь не добавляет отдельной границы authority.
      admissionProof:plan.maintenance.proof},kube);
  };
  const build=(target,action)=>{
    const d=deployment(),currentService=service();
    const personal=action==='restore'&&isPersonalIntegrationsMigrationProfile(migrationProfile);
    if(personal)requireValue(verifiedPersonalAPI&&same(d,verifiedPersonalAPI.deployment)&&same(currentService,verifiedPersonalAPI.isolation.service),
      'PERSONAL_INTEGRATIONS_API_TERMINAL_CHANGED');
    // Exact OPEN inventory уже подтверждён вместе со свежим owner proof.
    // CAS повторно читает Deployment, Service и его isolation endpoints.
    const resources=personal?verifiedPersonalAPI.resources:readComponentServingInventory(kube,name,d.metadata.uid);
    return {...planComponentAPITraffic({service:currentService,deployment:d,resources,
      target,observed,sourceSHA,action,closed,authorityTransition:profile,previous:previous?{...previous,slices:slices(),pods:pods(previous.plan)}:undefined,terminal,authorityExecutorSHA,authoritySourceEquivalentSHA,handoffWitness,
      ...(action==='isolate'?{migrationRestore:routineIsolation}:migrationProfile?{migrationRestoreProfile:migrationProfile,migrationSourceSHA}:{})}),...(sourceCompatibility?{sourceCompatibility}:{})};
  };
  if(phase==='plan'){
    requireValue(options['--target']&&!options['--evidence']&&!options['--approved-plan-sha'],'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');
    requireValue(migrationProfile&&options['--action']==='restore'||!['--migration-plan','--migration-evidence',
      ...(adopting?[]:['--api-open-plan','--api-open-evidence'])].some(key=>options[key]),
      'COMPONENT_API_TRAFFIC_MIGRATION_ARGUMENT_INVALID');
    const target=await readPrivateJSON(resolve(options['--target']));
    if(options['--action']==='isolate'&&migrationProfile){
      const authority=readBaseline();
      routineIsolation={profile:migrationProfile,closedPlanPath:resolve(options['--closed-plan']),closedEvidencePath:resolve(options['--closed-evidence']),authority};
      if(earlyStopPath)routineIsolation.earlyRecovery={...planWorkflowFileUnboundAPI(kube,target,observed,sourceSHA,resolve(earlyStopPath)),
        isolationEvidencePath:resolve(earlyEvidencePath)};
      if(adopting){terminal=authority;handoffWitness=await readHandoff(routineIsolation.earlyRecovery);}
    }
    if(options['--action']==='restore')terminal=await readTerminal();
    const plan=build(target,options['--action']);createPrivatePrettyJSONSync(resolve(options['--plan']),plan);
    console.log(JSON.stringify({status:'PLAN_ONLY',action:plan.action,planSHA256:fingerprint(plan)}));return;
  }
  requireValue(options['--evidence']&&!options['--action']&&!options['--target'],'COMPONENT_API_TRAFFIC_ARGUMENTS_INVALID');
  const plan=retained;validate(plan);authorityExecutorSHA=plan.authorityExecutorSHA;authoritySourceEquivalentSHA=plan.authoritySourceEquivalentSHA;
  requireValue(!options['--policy-transition']||options['--policy-transition']===(plan.authorityTransition??catalogPolicyTransition),'COMPONENT_API_TRAFFIC_AUTHORITY_PROFILE_INVALID');
  migrationProfile=plan.migrationRestore?.profile??plan.migrationRestoreProfile;migrationSourceSHA=plan.migrationSourceSHA;
  routineIsolation=plan.migrationRestore;
  requireValue(migrationProfile&&plan.action==='restore'&&phase==='apply'||!['--migration-plan','--migration-evidence',
    ...(adopting&&phase==='apply'?[]:['--api-open-plan','--api-open-evidence'])].some(key=>options[key]),
    'COMPONENT_API_TRAFFIC_MIGRATION_ARGUMENT_INVALID');
  // Исправленный successor может только наблюдать прежнюю изоляцию. Старые
  // plan/header/nonce не переписываются; apply и restore остаются exact-source.
  if(options['--predecessor-executor-sha']){
    requireValue(phase==='status'&&plan.action==='isolate'&&plan.sourceSHA===options['--predecessor-executor-sha']&&plan.sourceSHA!==sourceSHA,
      'COMPONENT_API_TRAFFIC_READBACK_SUCCESSOR_INVALID');
    run('git',['merge-base','--is-ancestor',plan.sourceSHA,sourceSHA]);
  }
  requireValue((plan.sourceSHA===sourceSHA||!!options['--predecessor-executor-sha'])&&portableRunnerTarget(plan.target,observed).allows(service())&&
    same(plan.target,closed.plan.target)&&plan.closePlanSHA256===fingerprint(closed.plan)&&
    (phase==='apply'?options['--approved-plan-sha']===fingerprint(plan):!options['--approved-plan-sha']),'COMPONENT_API_TRAFFIC_TARGET_CHANGED');
  const isolationEvidencePath=(unbound?previous?.plan.migrationRestore:plan.migrationRestore)?.earlyRecovery?.isolationEvidencePath;
  requireValue(!unbound||phase!=='apply'||!!isolationEvidencePath,'WORKFLOW_FILE_PROVENANCE_EARLY_RECOVERY_SCOPE_CHANGED');
  if(isolationEvidencePath)
    requireValue(isolationEvidencePath===resolve(unbound?options['--isolation-evidence']:options['--evidence'])&&
      (!unbound||isolationEvidencePath!==resolve(options['--evidence'])),'WORKFLOW_FILE_PROVENANCE_EARLY_RECOVERY_SCOPE_CHANGED');
  // Тот же lock удерживает mutating migration CLI; create/DDL не может
  // пересечь этап между положительным readback ART 050 и Service PATCH.
  const before=()=>matchesService(service(),plan.beforeSpec,plan.beforeAnnotations);
  let isolationJournal,journal,backupJournal;
  try{
    if(unbound&&phase==='apply'){
      isolationJournal=privateJournal(isolationEvidencePath,header(previous.plan));
      previous.events=isolationJournal.events;
      const fences=previous.events.filter(event=>event.type===workflowFileUnboundRestoreIntent);
      requireValue(fences.length<=1&&fences.every(event=>event.restorePlanSHA256===fingerprint(plan))&&
        (!same(service().spec,plan.afterSpec)||fences.length===1),'WORKFLOW_FILE_PROVENANCE_ISOLATION_CLOSED');
    }
    if(adopting&&phase==='apply'&&before()){
      const backup=routineIsolation.earlyRecovery.backupMaintenance;
      const stopped=readAuthorityPrivateJSON(backup.planPath);
      requireValue(fingerprint(stopped)===backup.planSHA256&&stopped.evidencePath===backup.evidencePath&&
        resolve(options['--evidence'])!==backup.evidencePath,adoptionRejected);
      backupJournal=privateJournal(backup.evidencePath,backupControllerMaintenanceHeader(stopped));
    }
    journal=privateJournal(options['--evidence'],header(plan));
    if(options['--predecessor-executor-sha'])journal.append({type:'READBACK_EXECUTOR',sourceSHA,predecessorExecutorSHA:plan.sourceSHA});
    if(phase==='apply'&&plan.action==='restore'&&before())terminal=await readTerminal();
    if(phase==='apply'&&plan.action==='isolate'&&routineIsolation&&before()){
      const early=routineIsolation.earlyRecovery;
      if(early)requireValue(same(planWorkflowFileUnboundAPI(kube,plan.target,observed,sourceSHA,early.backupMaintenance.planPath),
        {controlPlane:early.controlPlane,backupMaintenance:early.backupMaintenance}),'WORKFLOW_FILE_PROVENANCE_EARLY_RECOVERY_SCOPE_CHANGED');
      terminal=readBaseline(routineIsolation.authority);
      if(adopting){
        requireValue(backupJournal,adoptionRejected);
        handoffWitness=await readHandoff(early);
        terminal=readBaseline(routineIsolation.authority);
      }
    }
    const result=componentAPITrafficStep({phase,plan,journal,get:service,slices,pods:()=>pods(plan),gate:()=>{
      const fresh=build(plan.target,plan.action);
      if(plan.action==='restore'){
        requireValue((plan.migrationRestoreProfile?sameMigrationAPITerminal:sameAuthorityOutcome)(fresh.authority,plan.authority),'COMPONENT_API_TRAFFIC_AUTHORITY_CHANGED');
        fresh.authority=plan.authority;
      }
      requireValue(same(fresh,plan),'COMPONENT_API_TRAFFIC_GATE_CHANGED');
      if(isolationJournal&&!isolationJournal.events.some(event=>event.type===workflowFileUnboundRestoreIntent))
        isolationJournal.append({type:workflowFileUnboundRestoreIntent,restorePlanSHA256:fingerprint(plan)});
      return terminal;
    },patch:(operations,dryRun)=>withPrivatePatchFile(operations,path=>kube(['-n','kodex-system','patch','service',name,'--type=json','--patch-file',path,
      ...(dryRun?['--dry-run=server']:[]),'-o','name']))});console.log(JSON.stringify(result));
  }finally{journal?.close();isolationJournal?.close();backupJournal?.close();}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)main(process.argv.slice(2)).catch(error=>{
  console.error(JSON.stringify({status:'FAIL_OR_UNKNOWN',code:/^[A-Z_]+$/.test(error.message??'')?error.message:'COMPONENT_API_TRAFFIC_FAILED'}));process.exitCode=1;
});
