import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {readArtifactRetentionIssuerInputs} from './artifact-retention-issuer-inputs.mjs';
import {artifactRetentionIssuerAssetSteps,issuerAssetsIO,issuerAssetsResourceScope,validateIssuerAssetsJournal} from './artifact-retention-issuer-assets.mjs';

const errorCustodySourceChanged='PERSONAL_INTEGRATIONS_CUSTODY_SOURCE_CHANGED';
const errorCustodyResourceChanged='PERSONAL_INTEGRATIONS_CUSTODY_RESOURCE_CHANGED';
const errorCustodySecretChanged='PERSONAL_INTEGRATIONS_CUSTODY_SECRET_CHANGED';
const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const namespace='kodex-system',marker='kodex.dev/artifact-retention-issuer-assets';
const apiVersions={Secret:'v1',Certificate:'cert-manager.io/v1',NetworkPolicy:'networking.k8s.io/v1',Role:'rbac.authorization.k8s.io/v1'};

// Отдельный late predicate сохраняет incarnation/manager/key allowlist и TLS
// custody. Generation — opaque CAS counter, не snapshot/credential revision.
// Ни generation>0, ни этот DTO не являются authority либо readiness receipt.
export function readPersonalIntegrationsInstalledCustody({plan,events,sourceSHA,target},kube,io={}){
 fail(plan.source?.revision===sourceSHA&&['context','clusterUID','namespaceUID','installationUID','installationSHA256'].every(k=>plan.target?.[k]===target[k]),
  'PERSONAL_INTEGRATIONS_CUSTODY_SCOPE_CHANGED');
 const {steps:states,tls}=validateIssuerAssetsJournal(events,plan);
 fail(states.length===issuerAssetsResourceScope.length&&states.every(state=>state.uid)&&tls,'PERSONAL_INTEGRATIONS_CUSTODY_RECEIPTS_REQUIRED');
 const inputs=(io.readInputs??readArtifactRetentionIssuerInputs)(plan.source,kube);
 fail(fingerprint(inputs)===plan.inputsSHA256&&same(artifactRetentionIssuerAssetSteps(inputs,plan.steps.slice(9).map(step=>step.scope.before),plan.intentID),plan.steps),
  errorCustodySourceChanged);
 const projections=(io.readProjections??(()=>parseAuthorityPrivateJSON(readFileSync(resolve(plan.source.path,'tools/install/secret-projections.json'),'utf8'))))();
 const transport=issuerAssetsIO(plan,kube),dynamic=[];
 for(const step of plan.steps){
  if(step.kind==='Certificate')continue;
  const actual=transport.get(step),meta=actual?.metadata;
  fail(actual?.apiVersion===apiVersions[step.kind]&&actual.kind===step.kind&&meta?.uid===states[step.id].uid&&meta.name===step.name&&
   meta.namespace===namespace&&!meta.deletionTimestamp&&meta.labels?.['app.kubernetes.io/part-of']==='kodex'&&
   (step.action==='observe'||meta.annotations?.[marker]===plan.intentID),errorCustodyResourceChanged);
  if(step.kind==='Secret'){
   const generation=meta.annotations?.['kodex.dev/secret-generation'];
   fail(actual.type==='Opaque'&&meta.labels?.['app.kubernetes.io/managed-by']==='internal-rpc-authority-publisher'&&
    actual.mutability==='mutable'&&actual.stringDataCount==='0'&&/^(0|[1-9][0-9]{0,19})$/.test(generation??'')&&
    BigInt(generation)<=18446744073709551615n,errorCustodySecretChanged);
   const template='{{.metadata.uid}}{{"\\t"}}{{with .metadata.annotations}}{{index . "kodex.dev/secret-generation"}}{{end}}{{"\\t"}}{{range $key,$value := .data}}{{$key}}{{","}}{{end}}{{"\\t"}}live';
   const raw=kube(['-n',namespace,'get','secret',step.name,'-o','go-template='+template]);
   const fields=raw.trim().split('\t');
   fail(fields.length===4&&fields[0]===meta.uid&&fields[1]===generation&&fields[3]==='live',errorCustodySecretChanged);
   const keys=fields[2].split(',').filter(Boolean),projection=projections.secrets?.filter(item=>item.name===step.name);
   fail(projection?.length===1&&projection[0].dynamic===true&&Array.isArray(projection[0].items),
    errorCustodySourceChanged);
   const allowed=new Set(['_generation',...projection[0].items.map(item=>item.key)]);
   fail(new Set(keys).size===keys.length&&keys.every(key=>allowed.has(key))&&String(keys.length)===actual.dataCount&&
    (generation==='0'?keys.length===0:keys.includes('_generation')),'PERSONAL_INTEGRATIONS_CUSTODY_SECRET_KEYS_CHANGED');
   dynamic.push({name:step.name,uid:meta.uid,generation,dataKeysSHA256:fingerprint([...keys].sort())});
  }else if(['patch','observe'].includes(step.action)){
   fail(same(actual[step.scope.field],step.scope.after)&&same(meta.annotations??{},step.action==='observe'?(step.scope.before.metadata.annotations??{}):{...step.scope.before.metadata.annotations,[marker]:plan.intentID}),
    'PERSONAL_INTEGRATIONS_CUSTODY_PATCH_CHANGED');
  }else fail(same(actual.spec,step.body.spec)&&Object.entries(step.body.metadata.labels).every(([key,value])=>meta.labels[key]===value),
   errorCustodyResourceChanged);
 }
 const certificate=transport.certificateReady(states[7].uid,tls);
 fail(fingerprint((io.readInputs??readArtifactRetentionIssuerInputs)(plan.source,kube))===plan.inputsSHA256,
  errorCustodySourceChanged);
 return {schema:'kodex.personal-integrations-installed-assets-custody/v1',purpose:'CUSTODY_ONLY',sourceSHA,planSHA256:fingerprint(plan),
  resources:plan.steps.map((step,index)=>({...issuerAssetsResourceScope[index],action:step.action,uid:states[step.id].uid,intentSHA256:step.bodySHA256})),certificate,dynamic};
}
