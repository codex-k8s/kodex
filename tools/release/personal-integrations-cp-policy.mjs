import {createHash} from 'node:crypto';
import {readCatalogTarget} from './authority-catalog-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';
import {personalIntegrationsAuthorityTransition,validatePersonalIntegrationsAuthorityPolicy,verifyPersonalIntegrationsAuthorityPolicy}
 from './authority-personal-integrations-source.mjs';
import {fingerprint} from './scoped-release.mjs';

const namespace='kodex-system',volumeName='control-plane-environment-policy';
const mountPath='/var/run/config/kodex/control-plane/environment-policy';
const variable='CONTROL_PLANE_AUTHORITY_POLICY_FILE';
const intentType='PERSONAL_INTEGRATIONS_CP_POLICY_CREATE_INTENT';
const ackType='PERSONAL_INTEGRATIONS_CP_POLICY_CREATE_ACK';
const rejectedType='PERSONAL_INTEGRATIONS_CP_POLICY_CREATE_ACK_REJECTED';
const verifiedType='PERSONAL_INTEGRATIONS_CP_POLICY_VERIFIED';
const issueLabel='kodex.dev/policy-projection-issue';
const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const sha=value=>/^[a-f0-9]{64}$/.test(value??'');
const uid=value=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value??'');
const read=(kube,kind,name)=>JSON.parse(kube(['-n',namespace,'get',kind,name,'-o','json']));
const volume=name=>({name:volumeName,configMap:{name,defaultMode:292}});
const projectionName=digest=>`control-plane-policy-projection-${digest.slice(0,16)}`;

// Перечитывает выбранный CP и immutable policy107. Для STAGED исходный pointer
// берётся из прежнего exact spec того же Deployment, поскольку live CP указывает на110.
export function readPersonalIntegrationsCPPolicyInput(sourceSHA,authorityTarget,kube,beforeSpec){
 const current=readCatalogTarget(authorityTarget,authorityTarget.context,(kind,name)=>read(kube,kind,name),personalIntegrationsAuthorityTransition);
 const {transition,deployment,registry}=current;
 const previous=validatePersonalIntegrationsAuthorityPolicy(registry.data?.['authority-policy.json'],'predecessor',transition.environment);
 const name=projectionName(previous.sha256),mounted=read(kube,'configmap',name);
 fail(mounted?.apiVersion==='v1'&&mounted.kind==='ConfigMap'&&mounted.immutable===true&&!mounted.binaryData&&
  mounted.metadata?.namespace===namespace&&mounted.metadata.name===name&&uid(mounted.metadata.uid)&&!mounted.metadata.deletionTimestamp&&
  mounted.metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&same(mounted.data,{'policy.json':registry.data['authority-policy.json']}),
  'PERSONAL_INTEGRATIONS_CP_POLICY_PREDECESSOR_CHANGED');
 const canonical=verifyPersonalIntegrationsAuthorityPolicy().desired;
 const next=renderAuthorityPolicyEnvironment(canonical.raw,transition.environment);
 const input={schema:'kodex.personal-integrations-cp-policy/v1',sourceSHA,targetSHA256:transition.targetSHA256,
  deploymentUID:deployment.metadata.uid,predecessor:{name,uid:mounted.metadata.uid,policySHA256:previous.sha256},policySHA256:next.renderedSHA256,
  projection:{apiVersion:'v1',kind:'ConfigMap',metadata:{name:projectionName(next.renderedSHA256),namespace,
   labels:{'app.kubernetes.io/part-of':'kodex',[issueLabel]:'2145'}},immutable:true,data:{'policy.json':next.raw}}};
 // Проверяет указатель и volume shape на той же derivation, что использует handoff.
 stagePersonalIntegrationsCPPolicy({...structuredClone(beforeSpec??deployment.spec),replicas:0},input);
 return input;
}

// Сохраняет env и read-only mount. Меняется единственная immutable CM ссылка.
export function stagePersonalIntegrationsCPPolicy(stopped,input){
 const value=input?.projection?.data?.['policy.json'];
 fail(input?.schema==='kodex.personal-integrations-cp-policy/v1'&&sha(input.policySHA256)&&
  typeof value==='string'&&Buffer.byteLength(value)<1<<20&&createHash('sha256').update(value).digest('hex')===input.policySHA256&&
  input.projection?.metadata?.name===projectionName(input.policySHA256)&&
  sha(input.predecessor?.policySHA256)&&input.predecessor.name===projectionName(input.predecessor.policySHA256)&&
  input.predecessor.name!==input.projection.metadata.name&&stopped.replicas===0,'PERSONAL_INTEGRATIONS_CP_POLICY_INPUT_REJECTED');
 const next=structuredClone(stopped),pod=next.template.spec;
 const apps=pod.containers.filter(container=>container.name==='control-plane'),app=apps[0];
 const policyFields=(app?.env??[]).filter(field=>field.name===variable);
 const volumes=(pod.volumes??[]).filter(value=>value.name===volumeName);
 const mounts=(app?.volumeMounts??[]).filter(value=>value.name===volumeName||value.mountPath===mountPath);
 fail(apps.length===1&&policyFields.length===1&&same(policyFields[0],{name:variable,value:`${mountPath}/policy.json`})&&
  volumes.length===1&&same(volumes[0],volume(input.predecessor.name))&&
  mounts.length===1&&same(mounts[0],{name:volumeName,mountPath,readOnly:true}),
  'PERSONAL_INTEGRATIONS_CP_POLICY_POINTER_CHANGED');
 volumes[0].configMap.name=input.projection.metadata.name;
 return next;
}

const ownerKey='kodex.dev/personal-integrations-plan-sha256';
function projection(plan){
 const input=plan.personalIntegrationsInputs?.cpPolicy;
 const expected=input?.projection;
 const cp=plan.componentHandoff?.components.find(part=>part.name==='control-plane');
 fail(input?.schema==='kodex.personal-integrations-cp-policy/v1'&&input.sourceSHA===plan.source.revision&&input.deploymentUID===cp?.uid&&
  input.targetSHA256===plan.personalIntegrationsPrerequisites?.owner107?.targetSHA256&&expected?.metadata?.name&&
  cp.stagedSpec?.template?.spec?.volumes?.filter(value=>same(value,volume(expected.metadata.name))).length===1,
  'PERSONAL_INTEGRATIONS_CP_POLICY_SCOPE_CHANGED');
 return {...expected,metadata:{...expected.metadata,annotations:{[ownerKey]:fingerprint(plan)}}};
}

function verify(actual,expected){
 fail(actual?.apiVersion==='v1'&&actual.kind==='ConfigMap'&&!actual.binaryData&&actual.immutable===true&&
  actual.metadata?.name===expected.metadata.name&&actual.metadata.namespace===namespace&&uid(actual.metadata.uid)&&
  !actual.metadata.deletionTimestamp&&same(actual.metadata.labels,expected.metadata.labels)&&
  same(actual.metadata.annotations,expected.metadata.annotations)&&same(actual.data,expected.data),
  'PERSONAL_INTEGRATIONS_CP_POLICY_PROJECTION_CHANGED');
 return {name:actual.metadata.name,uid:actual.metadata.uid,projectionSHA256:fingerprint(expected)};
}

function custody(expected,events){
 const relevant=events.filter(row=>[intentType,ackType,rejectedType,verifiedType].includes(row.type));
 const hash=fingerprint(expected),order=relevant.map(row=>row.type).join(',');
 fail(['',intentType,`${intentType},${ackType}`,`${intentType},${verifiedType}`,`${intentType},${ackType},${verifiedType}`].includes(order)&&
  relevant.every(row=>row.projectionSHA256===hash&&row.name===expected.metadata.name&&(row.type===intentType||uid(row.uid)))&&
  (relevant.length!==3||relevant[1].uid===relevant[2].uid),
  'PERSONAL_INTEGRATIONS_CP_POLICY_JOURNAL_CHANGED');
 return {intent:relevant[0],ack:relevant.find(row=>row.type===ackType),verified:relevant.find(row=>row.type===verifiedType)};
}

// Единственный create предшествует pointer CAS. После неизвестного ACK
// оператор перечитывает ожидаемый объект и больше не отправляет create.
export function ensurePersonalIntegrationsPolicyProjection(plan,journal,kube,gate){
 const expected=projection(plan),rows=custody(expected,journal.events),name=expected.metadata.name,projectionSHA256=fingerprint(expected);
 let ack=rows.ack;
 if(!rows.intent){
  const found=kube(['-n',namespace,'get','configmap',name,'--ignore-not-found=true','-o','json']);
  fail(found.trim()==='','PERSONAL_INTEGRATIONS_CP_POLICY_PROJECTION_UNOWNED');
  const create=['-n',namespace,'create','-f','-','-o','json'];
  kube([...create,'--dry-run=server'],JSON.stringify(expected));
  gate();journal.append({type:intentType,name,projectionSHA256});
  const response=kube(create,JSON.stringify(expected));
  try{ack=verify(JSON.parse(response),expected);}
  catch(error){journal.append({type:rejectedType,name,projectionSHA256});throw error;}
  journal.append({type:ackType,...ack});
 }
 const observed=verify(read(kube,'configmap',name),expected);
 fail((!ack||ack.uid===observed.uid)&&(!rows.verified||rows.verified.uid===observed.uid),'PERSONAL_INTEGRATIONS_CP_POLICY_UID_CHANGED');
 if(!rows.verified)journal.append({type:verifiedType,...observed});
 return {...observed,policySHA256:plan.personalIntegrationsInputs.cpPolicy.policySHA256};
}

// Поздние readers принимают только тот же UID, который получил handoff.
export function readPersonalIntegrationsPolicyProjection(plan,events,kube){
 const expected=projection(plan),rows=custody(expected,events);
 fail(rows.verified,'PERSONAL_INTEGRATIONS_CP_POLICY_PROJECTION_UNCONFIRMED');
 const observed=verify(read(kube,'configmap',expected.metadata.name),expected);
 fail(observed.uid===rows.verified.uid,'PERSONAL_INTEGRATIONS_CP_POLICY_UID_CHANGED');
 return {...observed,policySHA256:plan.personalIntegrationsInputs.cpPolicy.policySHA256};
}
