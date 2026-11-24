import {readTelegramIntroductionInventory} from './telegram-introduction-inventory.mjs';
import {validateTelegramIntroducedReceiverProof} from './telegram-introduction-contract.mjs';
import {resolve} from 'node:path';
import {validatedStablePublication} from './stable-image-publication.mjs';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {telegramResourceCreateHeader,telegramResourceCreateStep,telegramResourceCreateRequests} from './telegram-resource-create.mjs';

const namespace='kodex-system';
const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);

// Read-only bridge связывает введённый API UID с настоящим create intent и
// текущими десятью dormant objects. Сохранённый boolean не является admission.
export async function readTelegramIntroducedReceiver({planPath,evidencePath,publicationPath,sourceSHA,context,target,publisherImage},kube,renderSource){
 const plan=readAuthorityPrivateJSON(planPath);
 fail(plan.planPath===resolve(planPath)&&plan.evidencePath===resolve(evidencePath)&&plan.target?.sourceSHA===sourceSHA&&
  plan.target.context===context&&['clusterUID','namespaceUID','installationUID','installationSHA256'].every(key=>plan.target[key]===target[key])&&
  plan.target.authorityImage===publisherImage,'TELEGRAM_INTRODUCTION_SOURCE_OR_TARGET_CHANGED');
 const publication=readAuthorityPrivateJSON(publicationPath);
 const interactionImage=validatedStablePublication(sourceSHA,publication,'interaction-gateway',plan.target.pullRegistry);
 fail(plan.interactionImage===interactionImage&&publication.publicationPlan.context===context&&
  publication.publicationPlan.clusterUID===target.clusterUID&&publication.publicationPlan.namespaceUID===target.namespaceUID&&
  typeof renderSource==='function'&&same(plan.objects?.map(part=>part.request),
   telegramResourceCreateRequests(await renderSource(),plan.target,plan.createID,interactionImage)),
  'TELEGRAM_INTRODUCTION_SOURCE_OR_PUBLICATION_CHANGED');
 const events=readAuthorityPrivateText(evidencePath).trimEnd().split('\n').map(parseAuthorityPrivateJSON);
 fail(events[0]?.type==='HEADER'&&Object.entries(telegramResourceCreateHeader(plan)).every(([key,value])=>events[0][key]===value),
  'TELEGRAM_INTRODUCTION_JOURNAL_CHANGED');
 const get=(kind,name)=>JSON.parse(kube(['-n',namespace,'get',kind,name,'-o','json']));
 const observe=()=>{
  const ns=JSON.parse(kube(['get','namespace',namespace,'-o','json']));
  return {context,clusterUID:JSON.parse(kube(['get','namespace','kube-system','-o','json'])).metadata.uid,
   namespaceUID:ns.metadata.uid,namespace:ns,installation:get('configmap','kodex-installation-profile'),publisher:get('deployment','internal-rpc-authority-publisher')};
 };
 const result=await telegramResourceCreateStep({phase:'status',plan,journal:{events,append:()=>{}},observe,
  inventory:()=>readTelegramIntroductionInventory(kube),
  get:request=>{const raw=kube(['-n',namespace,'get',request.kind.toLowerCase(),request.metadata.name,'--ignore-not-found','-o','json']);return raw.trim()?JSON.parse(raw):null;},
  dryRun:()=>{throw Error('TELEGRAM_INTRODUCTION_READ_ONLY_REQUIRED');},create:()=>{throw Error('TELEGRAM_INTRODUCTION_READ_ONLY_REQUIRED');}});
 fail(result.state==='CREATED_DORMANT'&&result.qualification==='NOT_ACTIVATED_NOT_INSTALLED','TELEGRAM_INTRODUCTION_DORMANT_REQUIRED');
 const request=plan.objects.find(part=>part.request.kind==='Deployment'),receipt=result.resources.find(row=>row.resource==='apps/v1|Deployment|interaction-gateway');
 fail(request&&receipt&&request.expected.spec?.replicas===0&&plan.intendedReplicas===3,'TELEGRAM_INTRODUCTION_DORMANT_REQUIRED');
 const proof={schema:'kodex.telegram-introduced-receiver/v1',state:'PENDING_AUTHORITY',sourceSHA,
  planPath:resolve(planPath),evidencePath:resolve(evidencePath),publicationPath:resolve(publicationPath),publicationSHA256:fingerprint(publication),createPlanSHA256:fingerprint(plan),createID:plan.createID,
  targetSHA256:fingerprint(plan.target),deploymentUID:receipt.uid,specSHA256:fingerprint(request.expected.spec),
  imageRef:plan.interactionImage,intendedReplicas:plan.intendedReplicas,resources:result.resources,
  ...(plan.target.monitoringResources===undefined?{}:{monitoringResources:plan.target.monitoringResources})};
 validateTelegramIntroducedReceiverProof(proof,sourceSHA);return Object.freeze(proof);
}

// После capture сравнивается весь новый proof: UID/spec/image и прочие objects
// не могут смениться во время подготовки либо до первого registry CAS.
export function verifyTelegramIntroducedReceiver(expected,actual){
 validateTelegramIntroducedReceiverProof(expected,expected?.sourceSHA);
 validateTelegramIntroducedReceiverProof(actual,expected.sourceSHA);
 fail(same(expected,actual),
  'TELEGRAM_INTRODUCTION_CHANGED');
}
