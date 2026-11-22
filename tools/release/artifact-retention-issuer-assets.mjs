#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {existsSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint,withPrivatePatchFile} from './scoped-release.mjs';
import {readArtifactRetentionIssuerInputs,artifactRetentionIssuerPatchFields} from './artifact-retention-issuer-inputs.mjs';
import {readCatalogTarget,catalogKubeconfig,
  readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON,
  syncAuthorityJournalDirectory,writeAuthorityPrivatePlan} from './authority-catalog-transition.mjs';
import {personalIntegrationsAuthorityTransition,validatePersonalIntegrationsAuthorityPolicy,
  planPersonalIntegrationsAuthorityRegistry} from './authority-personal-integrations-source.mjs';
import {readComponentServingInventory} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {readPersonalIntegrationsStoppedAdmission,verifyPersonalIntegrationsStoppedAdmission} from './personal-integrations-stopped-admission.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {telegramIntroductionResources as producerResources,telegramIntroductionMetadataQuery as producerMetadataQuery,
  parseTelegramIntroductionInventory as parseProducerInventory,telegramProducerSecretReferences as producerSecretReferences} from './telegram-introduction-inventory.mjs';

const namespace='kodex-system',workload='artifact-retention';
const schema='kodex.artifact-retention-issuer-assets/v2';
export const issuerAssetsResourcesSchema='kodex.artifact-retention-issuer-assets-resources/v2';
const marker='kodex.dev/artifact-retention-issuer-assets';
const owner='app.kubernetes.io/part-of',manager='app.kubernetes.io/managed-by';
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const sha=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const ensure=(ok,code)=>{if(!ok)throw Error('ISSUER_ASSETS_'+code);};
const kinds={Secret:'secret',Certificate:'certificate',NetworkPolicy:'networkpolicy',Role:'role'};
const apiVersions={Secret:'v1',Certificate:'cert-manager.io/v1',NetworkPolicy:'networking.k8s.io/v1',Role:'rbac.authorization.k8s.io/v1'};
const secretNames=['issuer-key','issuer-readback-credential','issuer-readback-possession','issuer-restore-ack',
  'issuer-restore-credential','manifest-trust','proof-trust'].map(suffix=>'internal-rpc-authority-artifact-retention-'+suffix);
const runtimePolicyName='artifact-retention-exact-runtime-paths';
const runtimePolicyResource='networkpolicies.networking.k8s.io';
export const issuerAssetsResourceScope=Object.freeze([
  ...secretNames.map(name=>({kind:'Secret',name})),
  {kind:'Certificate',name:'internal-rpc-authority-artifact-retention-workload'},
  {kind:'NetworkPolicy',name:'artifact-retention-internal-rpc-authority-exact-paths'},
  {kind:'Role',name:'internal-rpc-authority-publisher'},
  ...['control-plane-exact-runtime-paths','internal-rpc-authority-readback-attestor-exact-paths',
    'internal-rpc-authority-restore-controller-exact-paths',runtimePolicyName].map(name=>({kind:'NetworkPolicy',name})),
].map(item=>Object.freeze({apiVersion:apiVersions[item.kind],namespace,...item})));
const stepResource=step=>step.name===runtimePolicyName&&step.kind==='NetworkPolicy'?runtimePolicyResource:kinds[step.kind];

function resourceBody(resource) {
  const {apiVersion,kind,metadata,spec,rules}=resource;
  ensure(Object.hasOwn(kinds,kind)&&apiVersion===apiVersions[kind]&&metadata?.namespace===namespace&&metadata.labels?.[owner]==='kodex','SOURCE_REJECTED');
  return {apiVersion,kind,metadata:{name:metadata.name,namespace,labels:structuredClone(metadata.labels),annotations:{...metadata.annotations}},
    ...(spec?{spec:structuredClone(spec)}:{}),...(rules?{rules:structuredClone(rules)}:{})};
}

// Только официальные пустые generation0 проекции; material/rotation сюда не входят.
export function artifactRetentionIssuerAssetRequests(inputs,intentID) {
  ensure(uuid(intentID)&&same(inputs?.issuerSecretNames,secretNames),'SOURCE_REJECTED');
  const secrets=inputs.issuerSecretNames.map(name=>({apiVersion:'v1',kind:'Secret',metadata:{name,namespace,
    labels:{[owner]:'kodex',[manager]:'internal-rpc-authority-publisher'},
    annotations:{'kodex.dev/secret-generation':'0',[marker]:intentID}},type:'Opaque',data:{}}));
  return [...secrets,...[inputs.certificate,inputs.issuerNetworkPolicy].map(resource=>{
    const body=resourceBody(resource);body.metadata.annotations={...body.metadata.annotations,[marker]:intentID};return body;
  })];
}

function secretRefs(pod) {
  return [...new Set(producerSecretReferences(pod))].sort();
}

// Старый UID сам по себе не разрешает новый issuer scope. Positive join нужен
// до первой записи и при каждом readback; template update делает другой оператор.
export function requireArtifactRetentionAssetsUnclaimed(deployment,inventory,inputs,pins) {
  const meta=deployment?.metadata,pod=deployment?.spec?.template?.spec;
  ensure(deployment?.apiVersion==='apps/v1'&&deployment.kind==='Deployment'&&meta?.name===workload&&meta.namespace===namespace&&
    meta.labels?.[owner]==='kodex'&&uuid(meta.uid)&&!meta.deletionTimestamp&&deployment.spec.replicas===0&&
    Number.isSafeInteger(meta.generation)&&deployment.status?.observedGeneration>=meta.generation&&
    ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(field=>(deployment.status[field]??0)===0)&&
    pod?.serviceAccountName===workload&&pod.automountServiceAccountToken===false&&pod.containers?.length===1&&
    pod.containers[0].name===workload&&!pod.initContainers?.length&&!pod.ephemeralContainers?.length&&
    !deployment.spec.template.metadata?.labels?.['kodex.dev/internal-rpc-authority-issuer']&&
    !pod.containers[0].env?.some(env=>/^INTERNAL_RPC_AUTHORITY_(ISSUER_SOCKET|LOCAL_ROLE)$/.test(env.name))&&
    activeDeploymentPods(inventory,meta.uid).length===0,'WORKLOAD_CLAIMED');
  const refs=secretRefs(pod),required=[...inputs.issuerSecretNames,inputs.certificate.spec.secretName,
    'internal-rpc-authority-artifact-retention-issuer-postgresql','artifact-retention-platform-worker-grant-signer'];
  ensure(!refs.some(name=>required.includes(name)),'WORKLOAD_CLAIMED');
  const introduced=[...new Set([...required,...secretRefs(inputs.deployment.spec.template.spec).filter(name=>!refs.includes(name)&&
    (name.startsWith('internal-rpc-authority-artifact-retention-')||name.startsWith('artifact-retention-')))])].sort();
  const result={uid:meta.uid,specSHA256:fingerprint(deployment.spec),introducedSecrets:introduced};
  if(pins)ensure(same(pins,result),'WORKLOAD_CHANGED');
  return result;
}

// Общий metadata producer покрывает pending RC/CronJob/CSI refs без payload.
// Его исторический Telegram eligibility здесь не вызывается и не расширяется.
function assertUnclaimedReferences(kube,pins,joined) {
  const raw=kube(['-n',namespace,'get',producerResources,'--chunk-size=0','-o',
    producerMetadataQuery.replace('jsonpath=','jsonpath={.metadata.continue}{"\\n"}')]);
  ensure(Buffer.byteLength(raw)<=1<<20,'DISCOVERY_REJECTED');
  const {items}=parseProducerInventory(raw);ensure(items.length<=4096,'DISCOVERY_REJECTED');
  const joinedUIDs=new Set(joined.items.map(item=>item.metadata.uid));
  for(const item of items){
    ensure(!item.secretRefs.some(ref=>pins.introducedSecrets.includes(ref)),'CREDENTIAL_SCOPE_CLAIMED');
    const claims=[item.metadata.labels?.['app.kubernetes.io/name'],item.spec.serviceAccountName,
      item.spec.template.metadata.labels?.['app.kubernetes.io/name'],item.spec.template.spec.serviceAccountName,
      item.spec.jobTemplate.spec.template.metadata.labels?.['app.kubernetes.io/name'],item.spec.jobTemplate.spec.template.spec.serviceAccountName].includes(workload);
    if(!claims)continue;
    const assigned=item.kind==='Deployment'&&item.metadata.name===workload&&item.metadata.uid===pins.uid;
    const oldPod=item.kind==='Pod'&&joinedUIDs.has(item.metadata.uid);
    const refs=item.metadata.ownerReferences,oldSet=item.kind==='ReplicaSet'&&joinedUIDs.has(item.metadata.uid)&&refs.length===1&&
      refs[0].kind==='Deployment'&&refs[0].controller===true&&refs[0].uid===pins.uid&&item.spec.replicas===0&&
      (item.status.replicas??0)===0&&Number.isSafeInteger(item.metadata.generation)&&item.metadata.generation>=1&&
      item.status.observedGeneration>=item.metadata.generation;
    ensure(assigned||oldPod||oldSet,'CREDENTIAL_SCOPE_CLAIMED');
  }
  return fingerprint(items.sort((a,b)=>(a.kind+'/'+a.metadata.name).localeCompare(b.kind+'/'+b.metadata.name)));
}

function get(kube,kind,name) {
  const raw=kube(['-n',namespace,'get',kind,name,'--ignore-not-found=true','-o','json']);
  return raw.trim()?parseAuthorityPrivateJSON(raw):null;
}

// Возвращаются только UID/labels/counts. Ни data, ни stringData не покидают API.
function getSecret(kube,name) {
  const template='{{if .metadata.uid}}{{.metadata.uid}}{{"\\t"}}{{.metadata.name}}{{"\\t"}}{{.metadata.namespace}}{{"\\t"}}'+
    '{{.type}}{{"\\t"}}{{index .metadata.labels "'+owner+'"}}{{"\\t"}}{{index .metadata.labels "'+manager+'"}}{{"\\t"}}'+
    '{{index .metadata.annotations "kodex.dev/secret-generation"}}{{"\\t"}}{{index .metadata.annotations "'+marker+'"}}{{"\\t"}}'+
    '{{if .data}}{{len .data}}{{else}}0{{end}}{{"\\t"}}{{if .stringData}}{{len .stringData}}{{else}}0{{end}}{{"\\t"}}'+
    '{{if .immutable}}immutable{{else}}mutable{{end}}{{"\\t"}}{{if .metadata.deletionTimestamp}}deleting{{else}}live{{end}}{{end}}';
  const raw=kube(['-n',namespace,'get','secret',name,'--ignore-not-found=true','-o','go-template='+template]);
  if(!raw.trim())return null;
  const fields=raw.trim().split('\t');ensure(fields.length===12,'SECRET_METADATA_REJECTED');
  const [uid,actual,ns,type,owned,managed,generation,intent,dataCount,stringDataCount,mutability,state]=fields;
  ensure(uuid(uid)&&actual===name&&ns===namespace&&state==='live','SECRET_METADATA_REJECTED');
  return {apiVersion:'v1',kind:'Secret',metadata:{name:actual,namespace:ns,uid,labels:{[owner]:owned,[manager]:managed},
    annotations:{'kodex.dev/secret-generation':generation,[marker]:intent}},type,dataCount,stringDataCount,mutability};
}

// Controller ownership refs необязательны у штатного cert-manager. Вместо
// Secret payload проверяются тип, наличие ключей и exact certificate/issuer/SAN
// annotations; присутствующий ownerRef обязан ссылаться на этот Certificate UID.
function readTLSCustody(kube,certificate,expectedUID) {
  const annotations=['certificate-name','issuer-group','issuer-kind','issuer-name','alt-names','uri-sans'];
  const annotationFields=annotations.map(name=>name==='issuer-group'
    ?'{{range $name,$value := .metadata.annotations}}{{if eq $name "cert-manager.io/issuer-group"}}={{$value}}{{end}}{{end}}'
    :'{{with .metadata.annotations}}{{with index . "cert-manager.io/'+name+'"}}{{.}}{{end}}{{end}}').join('{{"\\t"}}');
  const template='{{if .metadata.uid}}{{.metadata.uid}}{{"\\t"}}{{.metadata.name}}{{"\\t"}}{{.metadata.namespace}}{{"\\t"}}{{.type}}{{"\\t"}}'+
    annotationFields+'{{"\\t"}}{{range $name,$value := .data}}{{$name}}{{","}}{{end}}{{"\\t"}}'+
    '{{if .data}}{{if index .data "tls.crt"}}1{{else}}0{{end}}{{else}}0{{end}}{{"\\t"}}'+
    '{{if .data}}{{if index .data "tls.key"}}1{{else}}0{{end}}{{else}}0{{end}}{{"\\t"}}'+
    '{{range .metadata.ownerReferences}}{{.apiVersion}}{{"|"}}{{.kind}}{{"|"}}{{.name}}{{"|"}}{{.uid}}{{"|"}}{{.controller}}{{";"}}{{end}}{{"\\t"}}'+
    '{{if .metadata.deletionTimestamp}}deleting{{else}}live{{end}}{{end}}';
  const raw=kube(['-n',namespace,'get','secret',certificate.spec.secretName,'--ignore-not-found=true','-o','go-template='+template]);
  const fields=raw.trim().split('\t');ensure(fields.length===15,'TLS_CUSTODY_REJECTED');
  const [uid,name,ns,type,certName,issuerGroupField,kind,issuer,dns,uris,keys,crtPresent,keyPresent,owners,state]=fields;
  // cert-manager записывает исходное значение IssuerRef.Group, в том числе
  // пустую строку для штатной группы. Пустое значение допустимо только при
  // наличии аннотации. Digest сохраняет фактическую строку без wire-префикса.
  const issuerGroup=issuerGroupField.startsWith('=')?issuerGroupField.slice(1):undefined;
  const csv=(actual,expected)=>{
    const values=actual?actual.split(','):[];
    return new Set(values).size===values.length&&same([...values].sort(),[...expected??[]].sort());
  };
  const keyNames=keys.split(',').filter(Boolean),refs=owners.split(';').filter(Boolean);
  ensure(uuid(uid)&&(!expectedUID||uid===expectedUID)&&name===certificate.spec.secretName&&ns===namespace&&type==='kubernetes.io/tls'&&state==='live'&&
    certName===certificate.metadata.name&&issuerGroup!==undefined&&
    (issuerGroup===''?'cert-manager.io':issuerGroup)===(certificate.spec.issuerRef.group??'cert-manager.io')&&
    kind===(certificate.spec.issuerRef.kind??'Issuer')&&issuer===certificate.spec.issuerRef.name&&csv(dns,certificate.spec.dnsNames)&&csv(uris,certificate.spec.uris)&&
    keyNames.includes('tls.crt')&&keyNames.includes('tls.key')&&new Set(keyNames).size===keyNames.length&&
    keyNames.every(key=>['tls.crt','tls.key','ca.crt'].includes(key))&&crtPresent==='1'&&keyPresent==='1'&&
    (refs.length===0||refs.length===1&&refs[0]===['cert-manager.io/v1','Certificate',certificate.metadata.name,certificate.metadata.uid,'true'].join('|')),
    'TLS_CUSTODY_REJECTED');
  const custodyFields=[uid,name,ns,type,certName,issuerGroup,kind,issuer,dns,uris,keys,crtPresent,keyPresent,owners,state];
  return {tlsSecretUID:uid,tlsMetadataSHA256:fingerprint(custodyFields)};
}

function patchRequest(scope,intentID) {
  const before=scope.before;
  return [{op:'test',path:'/metadata/uid',value:before.metadata.uid},
    {op:'test',path:'/metadata/resourceVersion',value:before.metadata.resourceVersion},
    {op:'test',path:'/'+scope.field,value:before[scope.field]},
    {op:'add',path:'/metadata/annotations',value:{...before.metadata.annotations,[marker]:intentID}},
    {op:'replace',path:'/'+scope.field,value:scope.after}];
}

// Расширяется только прежний exact ART selector; прочие egress/ingress правила
// сохраняются побайтно в spec. Отсутствие canonical CP правила не заменяется wildcard.
export function artifactRetentionRuntimeEgressScope(inputs,resources){
  const source=inputs.runtimeNetworkPolicy,matches=resources.filter(item=>item?.kind==='NetworkPolicy'&&item.metadata?.name===runtimePolicyName);
  const selector={matchLabels:{'app.kubernetes.io/name':'artifact-retention','app.kubernetes.io/component':'retention-job'}};
  const desired={to:[{podSelector:{matchLabels:{'app.kubernetes.io/name':'control-plane'}}}],ports:[{protocol:'TCP',port:8443}]};
  ensure(source?.apiVersion===apiVersions.NetworkPolicy&&source.kind==='NetworkPolicy'&&source.metadata?.name===runtimePolicyName&&
    source.metadata.namespace===namespace&&source.metadata.labels?.[owner]==='kodex'&&same(source.spec?.podSelector,selector)&&
    same(source.spec.policyTypes,['Ingress','Egress'])&&source.spec.egress?.filter(rule=>same(rule,desired)).length===1&&matches.length===1,'EGRESS_SOURCE_REJECTED');
  const before=matches[0];
  ensure(before.apiVersion===apiVersions.NetworkPolicy&&before.metadata.namespace===namespace&&before.metadata.labels?.[owner]==='kodex'&&
    uuid(before.metadata.uid)&&/^[1-9][0-9]*$/.test(before.metadata.resourceVersion??'')&&!before.metadata.deletionTimestamp&&
    same(before.spec?.podSelector,selector)&&same(before.spec.policyTypes,source.spec.policyTypes)&&
    (before.spec.egress===undefined||Array.isArray(before.spec.egress)),'EGRESS_OWNER_REJECTED');
  const after=structuredClone(before.spec);after.egress??=[];
  const count=after.egress.filter(rule=>same(rule,desired)).length;ensure(count<=1,'EGRESS_OWNER_REJECTED');
  if(!count)after.egress.push(structuredClone(desired));
  return {before,field:'spec',after};
}

function scopes(inputs,resources){
  return [...artifactRetentionIssuerPatchFields(inputs,resources),artifactRetentionRuntimeEgressScope(inputs,resources)];
}

function scopeStep(scope,intentID){
  const before=scope.before,observe=same(before[scope.field],scope.after);
  ensure(observe||!before.metadata.annotations?.[marker]||before.metadata.annotations[marker]===intentID,'FOREIGN_MANAGEMENT');
  const target={apiVersion:before.apiVersion,kind:before.kind,namespace:before.metadata.namespace,name:before.metadata.name,uid:before.metadata.uid};
  return {action:observe?'observe':'patch',kind:before.kind,name:before.metadata.name,target,scope,
    body:observe?{...target,field:scope.field,value:scope.after}:patchRequest(scope,intentID)};
}

export function artifactRetentionIssuerAssetSteps(inputs,resources,intentID) {
  ensure(resources.every(resource=>Object.hasOwn(kinds,resource?.kind??'')&&resource.apiVersion===apiVersions[resource.kind]),'SOURCE_REJECTED');
  return [...artifactRetentionIssuerAssetRequests(inputs,intentID).map(body=>({action:'create',kind:body.kind,name:body.metadata.name,body})),
    ...scopes(inputs,resources).map(scope=>scopeStep(scope,intentID))]
    .map((step,id)=>({...step,id,bodySHA256:fingerprint(step.body)}));
}

function validatePlan(plan) {
  ensure(plan?.schema===schema&&uuid(plan.intentID)&&typeof plan.evidencePath==='string'&&plan.evidencePath===resolve(plan.evidencePath)&&
    /^[a-f0-9]{40}$/.test(plan.source?.revision??'')&&plan.source.path===resolve(plan.source.path)&&
    plan.target?.context&&sha(plan.apiAdmissionSHA256)&&sha(plan.inputsSHA256)&&uuid(plan.workload?.uid)&&
    Array.isArray(plan.steps)&&plan.steps.length===issuerAssetsResourceScope.length,'PLAN_REJECTED');
  ensure(plan.admission&&Object.keys(plan.admission).sort().join(',')==='evidencePath,migrationEvidencePath,migrationPlanPath,planPath,trafficEvidencePath,trafficPlanPath'&&
    Object.values(plan.admission).every(path=>typeof path==='string'&&path===resolve(path)),'PLAN_REJECTED');
  ensure(plan.stoppedAdmission?.schema==='kodex.personal-integrations-stopped-admission/v1'&&
    plan.stoppedAdmission.sourceSHA===plan.source.revision&&plan.stoppedAdmission.apiAdmissionSHA256===plan.apiAdmissionSHA256,'PLAN_REJECTED');
  ensure(same(plan.steps.map(step=>step.id),issuerAssetsResourceScope.map((_,index)=>index))&&
    plan.steps.every((step,index)=>same({apiVersion:apiVersions[step.kind],namespace,kind:step.kind,name:step.name},issuerAssetsResourceScope[index])),'PLAN_REJECTED');
}

async function boundary(parameters,kube,inputs,io={}) {
  const current=readCatalogTarget(parameters.target,parameters.target.context,(kind,name)=>get(kube,kind,name),personalIntegrationsAuthorityTransition);
  const target=Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(name=>[name,parameters.target[name]]));
  const stopped=readPersonalIntegrationsStoppedAdmission({sourceSHA:parameters.source.revision,target,admission:parameters.admission},kube,io);
  validatePersonalIntegrationsAuthorityPolicy(current.registry.data['authority-policy.json'],'predecessor',current.transition.environment);
  planPersonalIntegrationsAuthorityRegistry(current.registry.data['key-delivery-targets.yaml']);
  const apiAdmissionSHA256=stopped.apiAdmissionSHA256;
  ensure(stopped.schema==='kodex.personal-integrations-stopped-admission/v1'&&stopped.sourceSHA===parameters.source.revision&&
    same(stopped.target,target)&&sha(apiAdmissionSHA256),'ADMISSION_REJECTED');
  if(parameters.stoppedAdmission)verifyPersonalIntegrationsStoppedAdmission(stopped,parameters.stoppedAdmission);
  if(parameters.apiAdmissionSHA256)ensure(parameters.apiAdmissionSHA256===apiAdmissionSHA256,'ADMISSION_CHANGED');
  const deployment=get(kube,'deployment',workload);
  ensure(current.target.allows(deployment),'TARGET_REJECTED');
  const joined=readComponentServingInventory(kube,workload,deployment.metadata.uid);
  const workloadPins=requireArtifactRetentionAssetsUnclaimed(deployment,joined,inputs,parameters.workload);
  // Плановые Jobs и TTL могут изменить первый снимок. Каждый из максимум
  // четырёх снимков заново проходит полную проверку ссылок и identity.
  const discoveryDeadline=performance.now()+20000;
  let previous=assertUnclaimedReferences(kube,workloadPins,joined);
  let stable=false;
  for(let attempt=1;attempt<4&&!stable&&performance.now()<discoveryDeadline;attempt++){
    const current=assertUnclaimedReferences(kube,workloadPins,joined);
    stable=current===previous;
    previous=current;
  }
  ensure(stable&&performance.now()<discoveryDeadline,'DISCOVERY_CHANGED');
  return {apiAdmissionSHA256,stoppedAdmission:stopped,workload:workloadPins};
}

export async function planArtifactRetentionIssuerAssets(parameters,kube,io={}) {
  const {source,target,admission,evidencePath,intentID=randomUUID()}=parameters;
  ensure(uuid(intentID)&&typeof evidencePath==='string'&&evidencePath===resolve(evidencePath)&&!existsSync(evidencePath),'PLAN_REJECTED');
  ensure(admission&&Object.keys(admission).sort().join(',')==='evidencePath,migrationEvidencePath,migrationPlanPath,planPath,trafficEvidencePath,trafficPlanPath'&&
    Object.values(admission).every(path=>typeof path==='string'&&path===resolve(path)),'PLAN_REJECTED');
  syncAuthorityJournalDirectory(evidencePath);
  const inputs=readArtifactRetentionIssuerInputs(source,kube);
  const observed=await boundary({source,target,admission},kube,inputs,io);
  const creates=artifactRetentionIssuerAssetRequests(inputs,intentID);
  for(const resource of creates)ensure(!(resource.kind==='Secret'?getSecret(kube,resource.metadata.name):get(kube,kinds[resource.kind],resource.metadata.name)),'RESOURCE_EXISTS');
  ensure(!getSecret(kube,inputs.certificate.spec.secretName),'TLS_SECRET_EXISTS');
  const resources=[get(kube,'role','internal-rpc-authority-publisher'),...inputs.ingressPolicies.map(policy=>get(kube,'networkpolicy',policy.metadata.name)),
    get(kube,runtimePolicyResource,runtimePolicyName)];
  const steps=artifactRetentionIssuerAssetSteps(inputs,resources,intentID);
  const plan={schema,intentID,evidencePath,source,target,admission,...observed,inputsSHA256:fingerprint(inputs),steps};
  validatePlan(plan);return plan;
}

export const issuerAssetsJournalHeader=plan=>({schema,planSHA256:fingerprint(plan),intentID:plan.intentID});

// Единственный intent на шаг. Неоднозначный ACK не становится разрешением retry.
export function validateIssuerAssetsJournal(events,plan) {
  validatePlan(plan);ensure(Array.isArray(events)&&events.length>=1&&events.length<=58,'JOURNAL_REJECTED');
  const exact=(row,value)=>Number.isFinite(Date.parse(row?.at))&&same(Object.fromEntries(Object.entries(row).filter(([key])=>key!=='at')),value);
  ensure(exact(events[0],{type:'HEADER',...issuerAssetsJournalHeader(plan)}),'JOURNAL_REJECTED');
  const states=plan.steps.map(()=>({}));let position=1;
  for(const step of plan.steps){
    if(position===events.length)break;
    if(step.action==='observe'){
      const row=events[position++];ensure(uuid(row.uid)&&row.uid===step.target.uid&&
        exact(row,{type:'NOOP_VERIFIED',id:step.id,uid:row.uid,bodySHA256:step.bodySHA256}),'JOURNAL_REJECTED');
      states[step.id]={intent:true,uid:row.uid,noop:true};continue;
    }
    ensure(exact(events[position++],{type:'INTENT',id:step.id,bodySHA256:step.bodySHA256}),'JOURNAL_REJECTED');states[step.id].intent=true;
    if(position<events.length&&events[position].type==='ACK'){
      const row=events[position++];ensure(uuid(row.uid)&&exact(row,{type:'ACK',id:step.id,uid:row.uid}),'JOURNAL_REJECTED');states[step.id].ack=row.uid;
    }
    if(position<events.length){const row=events[position++];ensure(uuid(row.uid)&&(!states[step.id].ack||states[step.id].ack===row.uid)&&
      exact(row,{type:'VERIFIED',id:step.id,uid:row.uid}),'JOURNAL_REJECTED');states[step.id].uid=row.uid;}else break;
  }
  let tls;
  if(position<events.length){
    const row=events[position++];ensure(states.every(state=>state.uid)&&uuid(row.tlsSecretUID)&&sha(row.tlsMetadataSHA256)&&
      exact(row,{type:'TLS_VERIFIED',certificateUID:states[7].uid,tlsSecretUID:row.tlsSecretUID,tlsMetadataSHA256:row.tlsMetadataSHA256}),'JOURNAL_REJECTED');
    tls={tlsSecretUID:row.tlsSecretUID,tlsMetadataSHA256:row.tlsMetadataSHA256};
  }
  ensure(position===events.length,'JOURNAL_REJECTED');return {steps:states,tls};
}

function verifyStep(step,actual,intentID,uid) {
  ensure(actual&&actual.apiVersion===apiVersions[step.kind]&&uuid(actual.metadata?.uid)&&(!uid||uid===actual.metadata.uid)&&actual.kind===step.kind&&actual.metadata.name===step.name&&
    actual.metadata.namespace===namespace&&!actual.metadata.deletionTimestamp&&actual.metadata.labels?.[owner]==='kodex','RESOURCE_CHANGED');
  if(step.action==='observe'){
    ensure(actual.metadata.uid===step.target.uid&&same(actual[step.scope.field],step.scope.after)&&
      same(actual.metadata.annotations??{},step.scope.before.metadata.annotations??{}),'NOOP_CHANGED');return actual.metadata.uid;
  }
  ensure(actual.metadata.annotations?.[marker]===intentID,'RESOURCE_CHANGED');
  if(step.kind==='Secret')ensure(actual.type==='Opaque'&&actual.metadata.labels[manager]==='internal-rpc-authority-publisher'&&
    actual.metadata.annotations['kodex.dev/secret-generation']==='0'&&actual.dataCount==='0'&&actual.stringDataCount==='0'&&actual.mutability==='mutable','SECRET_NOT_EMPTY');
  else if(step.action==='patch')ensure(actual.metadata.uid===step.scope.before.metadata.uid&&same(actual[step.scope.field],step.scope.after)&&
    same(actual.metadata.annotations,{...step.scope.before.metadata.annotations,[marker]:intentID}),'PATCH_CHANGED');
  else {
    const spec=structuredClone(actual.spec);
    // Закрытые defaults Certificate v1 не меняют закреплённые SAN/issuer/key policy.
    // Любая неизвестная admission mutation остаётся mismatch, а не новым desired.
    if(step.kind==='Certificate'){
      if(step.body.spec.issuerRef.group===undefined&&spec?.issuerRef?.group==='cert-manager.io')delete spec.issuerRef.group;
      if(step.body.spec.revisionHistoryLimit===undefined&&spec.revisionHistoryLimit===1)delete spec.revisionHistoryLimit;
      if(step.body.spec.privateKey&&step.body.spec.privateKey.encoding===undefined&&spec.privateKey?.encoding==='PKCS1')delete spec.privateKey.encoding;
    }
    ensure(same(spec,step.body.spec)&&Object.entries(step.body.metadata.labels).every(([name,value])=>actual.metadata.labels[name]===value),'CREATE_CHANGED');
  }
  return actual.metadata.uid;
}

function checkedInputs(plan,kube) {
  const inputs=readArtifactRetentionIssuerInputs(plan.source,kube);ensure(fingerprint(inputs)===plan.inputsSHA256,'SOURCE_CHANGED');
  ensure(same(artifactRetentionIssuerAssetRequests(inputs,plan.intentID),plan.steps.slice(0,9).map(step=>step.body)),'PLAN_REJECTED');
  const expected=scopes(inputs,plan.steps.slice(9).map(step=>step.scope.before));
  ensure(expected.length===5&&expected.every((scope,index)=>{
    const wanted=scopeStep(scope,plan.intentID),actual=plan.steps[9+index];
    return same({...wanted,id:actual.id,bodySHA256:fingerprint(wanted.body)},actual);
  }),'PLAN_REJECTED');
  for(const step of plan.steps)ensure(fingerprint(step.body)===step.bodySHA256,'PLAN_REJECTED');
  return inputs;
}

// io.inspect допускается только локальной synthetic-оснасткой общего reader.
// CLI и exported Ready не принимают caller-supplied inspection/boolean.
export function issuerAssetsIO(plan,kube,io={}) {
  validatePlan(plan);
  return {
    async gate(){
      await boundary(plan,kube,checkedInputs(plan,kube),io);
    },
    get:step=>step.kind==='Secret'?getSecret(kube,step.name):get(kube,stepResource(step),step.name),
    beforeMutation(step){if(step.kind==='Certificate')ensure(!getSecret(kube,step.body.spec.secretName),'TLS_SECRET_EXISTS');},
    mutate(step){
      ensure(['create','patch'].includes(step.action),'NOOP_MUTATION_REJECTED');
      let raw;try{
        raw=step.action==='create'?kube(['create','-f','-','-o','jsonpath={.metadata.uid}'],JSON.stringify(step.body)):
          withPrivatePatchFile(step.body,path=>kube(['-n',namespace,'patch',stepResource(step),step.name,'--type=json','--patch-file',path,'-o','jsonpath={.metadata.uid}']));
      }catch{throw Error('ISSUER_ASSETS_ACK_UNKNOWN');}
      ensure(uuid(raw.trim()),'ACK_UNKNOWN');return raw.trim();
    },
    certificateReady(expectedUID,tls){
      const step=plan.steps[7],certificate=get(kube,'certificate',step.name);
      const uid=verifyStep(step,certificate,plan.intentID,expectedUID);
      const conditions=certificate.status?.conditions?.filter(condition=>condition.type==='Ready');
      ensure(Number.isSafeInteger(certificate.metadata.generation)&&certificate.metadata.generation>=1&&conditions?.length===1&&
        conditions[0].status==='True'&&conditions[0].observedGeneration===certificate.metadata.generation,'CERTIFICATE_NOT_READY');
      const custody=readTLSCustody(kube,certificate,tls?.tlsSecretUID);
      if(tls)ensure(same(custody,tls),'TLS_CUSTODY_REJECTED');
      return {uid,ready:true,...custody};
    },
  };
}

export async function executeArtifactRetentionIssuerAssets(phase,plan,journal,io) {
  ensure(['apply','status'].includes(phase),'PHASE_REJECTED');
  let {steps:states,tls}=validateIssuerAssetsJournal(journal.events,plan);await io.gate();
  for(const step of plan.steps){
    let state=states[step.id],actual=io.get(step);
    if(step.action==='observe'){
      const uid=verifyStep(step,actual,plan.intentID,state.uid);
      if(!state.uid){await io.gate();verifyStep(step,io.get(step),plan.intentID,uid);
        journal.append({type:'NOOP_VERIFIED',id:step.id,uid,bodySHA256:step.bodySHA256});}
      ({steps:states,tls}=validateIssuerAssetsJournal(journal.events,plan));continue;
    }
    if(!state.intent){
      if(phase==='status')return {status:'NOT_STARTED',step:step.id};
      if(step.action==='create')ensure(!actual,'RESOURCE_EXISTS');
      else ensure(actual?.metadata?.uid===step.scope.before.metadata.uid&&actual.metadata.resourceVersion===step.scope.before.metadata.resourceVersion&&
        same(actual[step.scope.field],step.scope.before[step.scope.field]),'PREIMAGE_CHANGED');
      await io.gate();io.beforeMutation(step);journal.append({type:'INTENT',id:step.id,bodySHA256:step.bodySHA256});
      const acknowledged=io.mutate(step);journal.append({type:'ACK',id:step.id,uid:acknowledged});actual=io.get(step);
      verifyStep(step,actual,plan.intentID,acknowledged);
    }
    const uid=verifyStep(step,actual,plan.intentID,state.uid??state.ack);
    if(!state.uid)journal.append({type:'VERIFIED',id:step.id,uid});
    ({steps:states,tls}=validateIssuerAssetsJournal(journal.events,plan));
  }
  await io.gate();let certificate=io.certificateReady(states[7].uid,tls);
  if(!tls){journal.append({type:'TLS_VERIFIED',certificateUID:certificate.uid,tlsSecretUID:certificate.tlsSecretUID,tlsMetadataSHA256:certificate.tlsMetadataSHA256});
    ({tls}=validateIssuerAssetsJournal(journal.events,plan));}
  for(const step of plan.steps)verifyStep(step,io.get(step),plan.intentID,states[step.id].uid);
  await io.gate();
  certificate=io.certificateReady(states[7].uid,tls);
  return {status:'PASS',schema:'kodex.artifact-retention-issuer-assets-ready/v2',planSHA256:fingerprint(plan),sourceSHA:plan.source.revision,
    target:plan.target,apiAdmissionSHA256:plan.apiAdmissionSHA256,artifactRetentionUID:plan.workload.uid,
    artifactRetentionSpecSHA256:plan.workload.specSHA256,
    resources:plan.steps.map(step=>({apiVersion:apiVersions[step.kind],namespace,kind:step.kind,name:step.name,uid:states[step.id].uid,action:step.action,bodySHA256:step.bodySHA256})),certificate};
}

// Только production reader: готовый boolean либо caller receipt не заменяют API.
export async function readArtifactRetentionIssuerAssetsReady({plan,events},kube) {
  const journal={events:structuredClone(events),append(){throw Error('ISSUER_ASSETS_READ_ONLY');}};
  const {steps:states,tls}=validateIssuerAssetsJournal(events,plan);ensure(states.every(state=>state.uid)&&tls,'INCOMPLETE');
  return executeArtifactRetentionIssuerAssets('status',plan,journal,issuerAssetsIO(plan,kube));
}

// Только PRE_ROTATION resource predicate для общего STAGED admission consumer.
// Сам результат не выдаёт authority/Ready и не заменяет свежий cohort049 proof.
// После publisher delivery generation0 уже не является подходящим предикатом.
export function readArtifactRetentionIssuerAssetResources({plan,events},kube) {
  const {steps:states,tls}=validateIssuerAssetsJournal(events,plan);ensure(states.every(state=>state.uid)&&tls,'INCOMPLETE');
  checkedInputs(plan,kube);const io=issuerAssetsIO(plan,kube);
  for(const step of plan.steps)verifyStep(step,io.get(step),plan.intentID,states[step.id].uid);
  checkedInputs(plan,kube);const certificate=io.certificateReady(states[7].uid,tls);
  return {schema:issuerAssetsResourcesSchema,purpose:'PRE_ROTATION',planSHA256:fingerprint(plan),sourceSHA:plan.source.revision,
    resources:plan.steps.map(step=>({apiVersion:apiVersions[step.kind],namespace,kind:step.kind,name:step.name,uid:states[step.id].uid,action:step.action,bodySHA256:step.bodySHA256})),certificate};
}

export async function mainIssuerAssets(args) {
  const phase=args.shift(),options={};
  const flags={plan:['--target','--source','--source-sha','--maintenance-plan','--maintenance-evidence','--traffic-plan','--traffic-evidence','--migration-plan','--migration-evidence','--output','--evidence'],
    apply:['--plan','--evidence','--approved-plan-sha'],status:['--plan','--evidence']};
  ensure(Object.hasOwn(flags,phase??''),'ARGUMENT_REJECTED');
  while(args.length){const flag=args.shift();ensure([...flags[phase],'--context','--kubeconfig'].includes(flag)&&!Object.hasOwn(options,flag)&&args[0]&&!args[0].startsWith('--'),'ARGUMENT_REJECTED');options[flag]=args.shift();}
  ensure(options['--context']&&options['--kubeconfig']&&options['--evidence'],'ARGUMENT_REJECTED');
  const config=catalogKubeconfig(options['--kubeconfig']);
  const kube=(command,input)=>{try{return execFileSync('kubectl',['--context',options['--context'],'--kubeconfig',config,'--request-timeout=20s',...command],
    {input,encoding:'utf8',timeout:30000,maxBuffer:16<<20,stdio:['pipe','pipe','pipe'],env:{PATH:process.env.PATH,HOME:process.env.HOME,LANG:'C.UTF-8'}});}catch{throw Error('ISSUER_ASSETS_TARGET_OPERATION_UNCONFIRMED');}};
  if(phase==='plan'){
    ensure(flags.plan.every(flag=>options[flag]),'ARGUMENT_REJECTED');
    const target=readAuthorityPrivateJSON(options['--target']);ensure(target.context===options['--context'],'TARGET_REJECTED');
    const plan=await planArtifactRetentionIssuerAssets({target,source:{path:resolve(options['--source']),revision:options['--source-sha']},evidencePath:resolve(options['--evidence']),
      admission:{planPath:options['--maintenance-plan'],evidencePath:options['--maintenance-evidence'],trafficPlanPath:options['--traffic-plan'],trafficEvidencePath:options['--traffic-evidence'],
        migrationPlanPath:options['--migration-plan'],migrationEvidencePath:options['--migration-evidence']}},kube);
    writeAuthorityPrivatePlan(options['--output'],plan);return {status:'PLANNED',planSHA256:fingerprint(plan)};
  }
  const plan=readAuthorityPrivateJSON(options['--plan']);validatePlan(plan);
  ensure(plan.target.context===options['--context']&&plan.evidencePath===resolve(options['--evidence']),'TARGET_REJECTED');
  if(phase==='apply')ensure(options['--approved-plan-sha']===fingerprint(plan),'PLAN_NOT_APPROVED');
  if(existsSync(plan.evidencePath))readAuthorityPrivateText(plan.evidencePath).split('\n').filter(Boolean).forEach(parseAuthorityPrivateJSON);
  else ensure(phase==='apply','INCOMPLETE');
  const journal=privateJournal(plan.evidencePath,issuerAssetsJournalHeader(plan));
  try{return await executeArtifactRetentionIssuerAssets(phase,plan,journal,issuerAssetsIO(plan,kube));}finally{journal.close();}
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))mainIssuerAssets(process.argv.slice(2)).then(result=>process.stdout.write(JSON.stringify(result)+'\n')).catch(error=>{
  process.stderr.write((/^ISSUER_ASSETS_[A-Z_]+$/.test(error.message)?error.message:'ISSUER_ASSETS_REJECTED')+'\n');process.exitCode=1;
});
