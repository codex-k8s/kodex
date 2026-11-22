import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import {performance} from 'node:perf_hooks';
import {fingerprint,requireInstanceGrants} from './scoped-release.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {validatedStablePublication} from './stable-image-publication.mjs';
import {componentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {requireClosedAPIAdmission} from './component-api-traffic.mjs';
import {requireClosedAPIMaintenance} from './component-api-maintenance.mjs';
import {validateCatalogAuthorityBaseline,sameCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';
import {validWorkflowFileProvenanceComposedPostcondition} from './workflow-file-provenance-migration-contract.mjs';
import {validateMigrationDatabaseIdentity} from './migration-owner-database.mjs';

export const pairedTokenReleaseProfile='PERSONAL_TOKEN_ALLOWANCE_SAME_SCHEMA_051';
export const pairedTokenReleaseNames=Object.freeze(['control-plane','integration-gateway']);
export const pairedTokenReleaseHeads=Object.freeze({'control-plane':20261121000200,'internal-rpc-authority':20261115000200});
const schema='kodex.paired-token-release-manifest/v1';
const ensure=(value,code)=>{if(!value)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const sha=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value)&&!/^0+$/.test(value);
const revision=value=>typeof value==='string'&&/^[a-f0-9]{40}$/.test(value)&&!/^0+$/.test(value);
const uid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&same(Object.keys(value).sort(),[...keys].sort());
const image=(value,name)=>typeof value==='string'&&new RegExp(`^registry\\.local\\.kodex/kodex/${name}@sha256:[a-f0-9]{64}$`).test(value)&&!value.endsWith('0'.repeat(64));
const ownerProofs=new WeakMap();

// Это отдельный image-only план, а не alias существующего forward profile.
// Он не содержит migration command, Secret, SQL, patch или разрешения apply.
export function validatePairedTokenReleaseManifest(value){
 ensure(exact(value,['schema','profile','sourceSHA','schemaHeadsBefore','schemaHeadsAfter','target','components','apiSpecSHA256'])&&
  value.schema===schema&&value.profile===pairedTokenReleaseProfile&&revision(value.sourceSHA)&&
  sha(value.apiSpecSHA256)&&
  same(value.schemaHeadsBefore,pairedTokenReleaseHeads)&&same(value.schemaHeadsAfter,pairedTokenReleaseHeads),
  'PAIRED_TOKEN_RELEASE_MANIFEST_REJECTED');
 const required=[['apps/v1','Deployment','control-plane'],['apps/v1','Deployment','integration-gateway'],
  ['apps/v1','Deployment','control-api-gateway'],['v1','Service','control-api-gateway']];
 ensure(exact(value.target,['schema','context','clusterUID','namespaceUID','installationUID','installationSHA256','resources'])&&
  value.target.schema==='kodex.portable-runner-target/v1'&&/^[A-Za-z0-9_.:@/-]{1,160}$/.test(value.target.context??'')&&
  ['clusterUID','namespaceUID','installationUID'].every(k=>uid(value.target[k]))&&sha(value.target.installationSHA256)&&
  value.target.resources?.length===required.length&&value.target.resources.every(r=>exact(r,['apiVersion','kind','name','uid']))&&required.every(([apiVersion,kind,name])=>
  value.target.resources.filter(r=>r.apiVersion===apiVersion&&r.kind===kind&&r.name===name&&uid(r.uid)).length===1),
  'PAIRED_TOKEN_RELEASE_TARGET_REJECTED');
 ensure(Array.isArray(value.components)&&same(value.components.map(c=>c?.name),pairedTokenReleaseNames),
  'PAIRED_TOKEN_RELEASE_COMPONENTS_REJECTED');
 for(const part of value.components){
  ensure(exact(part,['name','uid','beforeImage','beforeSourceSHA','beforeSpecSHA256','publication'])&&uid(part.uid)&&
   revision(part.beforeSourceSHA)&&sha(part.beforeSpecSHA256)&&image(part.beforeImage,part.name)&&
   value.target.resources.some(r=>r.apiVersion==='apps/v1'&&r.kind==='Deployment'&&r.name===part.name&&r.uid===part.uid),
   'PAIRED_TOKEN_RELEASE_COMPONENT_REJECTED');
  const published=validatedStablePublication(value.sourceSHA,part.publication,part.name),plan=part.publication.publicationPlan;
  ensure(image(published,part.name)&&published!==part.beforeImage&&plan.context===value.target.context&&
   plan.clusterUID===value.target.clusterUID&&plan.namespaceUID===value.target.namespaceUID&&plan.source===value.sourceSHA&&
   plan.receipt.dockerfile===(part.name==='control-plane'?'services/internal/control-plane/':'services/external/integration-gateway/')+'Dockerfile',
   'PAIRED_TOKEN_RELEASE_PUBLICATION_REJECTED');
 }
 // Один Git context/source; OCI archive каждого компонента закономерно свой.
 const receipts=value.components.map(c=>c.publication.publicationPlan.receipt);
 ensure(receipts.every(r=>r.contextSHA256===receipts[0].contextSHA256),
  'PAIRED_TOKEN_RELEASE_BUILD_SOURCE_CHANGED');
 return value;
}

// Reader остаётся внутренним adapter port. Сериализованный JSON не становится
// owner proof: план принимает только результат именно этого свежего чтения.
// Пока нет связанного CLI/lifecycle receiver, apply отсутствует.
export function readPairedTokenReleaseOwner(manifest,{readControlPlane,readAuthorityHead,readAuthority,now=Date.now}){
 validatePairedTokenReleaseManifest(manifest);
 const monotonicBegan=performance.now(),began=now();
 const controlPlane=readControlPlane();
 ensure(validWorkflowFileProvenanceComposedPostcondition(controlPlane.proof)&&
  uid(controlPlane.postgres?.uid)&&uid(controlPlane.postgres?.ownerUID)&&controlPlane.postgres.pod==='kodex-postgresql-0',
  'PAIRED_TOKEN_RELEASE_SCHEMA_REJECTED');
 validateMigrationDatabaseIdentity(controlPlane.backend);
 const head=readAuthorityHead();
 ensure(head?.database==='internal_rpc_authority'&&head.readOnly==='on'&&head.gooseVersion===pairedTokenReleaseHeads['internal-rpc-authority'],
  'PAIRED_TOKEN_RELEASE_SCHEMA_REJECTED');
 const authority=readAuthority();
 const finished=now();
 ensure(Number.isFinite(began)&&Number.isFinite(finished)&&finished>=began&&finished-began<=30000,
  'PAIRED_TOKEN_RELEASE_OWNER_EXPIRED');
 ensure(performance.now()-monotonicBegan<=30000,'PAIRED_TOKEN_RELEASE_OWNER_EXPIRED');
 validateCatalogAuthorityBaseline(authority,finished,110);
 ensure(same(controlPlane.postgres,authority.postgres)&&
  controlPlane.backend.systemIdentifier===authority.postgresBackend.systemIdentifier&&
  controlPlane.backend.databaseOID!==authority.postgresBackend.databaseOID&&
  same(head.postgres,authority.postgres)&&same(head.backend,authority.postgresBackend)&&
  same(authority.scope,Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(k=>[k,manifest.target[k]]))),
  'PAIRED_TOKEN_RELEASE_OWNER_SCOPE_CHANGED');
 // Hash read-only SQL producer из immutable source фиксируется отдельно от
 // historical installation profile. Последний не выдаётся за observed051.
 const sql=readFileSync(new URL('./workflow-file-provenance-backup-migration.sql',import.meta.url));
 const body={manifestSHA256:fingerprint(manifest),schemaHeads:pairedTokenReleaseHeads,
  controlPlane,authority,authorityHead:head,controlPlaneSQLSHA256:createHash('sha256').update(sql).digest('hex'),began};
 const proof=Object.freeze(structuredClone(body));
 ownerProofs.set(proof,{hash:fingerprint(proof),until:monotonicBegan+30000});
 return proof;
}

function requireOwner(manifest,owner,now){
 const sealed=ownerProofs.get(owner);
 ensure(sealed?.hash===fingerprint(owner)&&performance.now()<sealed.until&&owner.manifestSHA256===fingerprint(manifest)&&
  Number.isFinite(now)&&now>=owner.began&&now-owner.began<=30000,'PAIRED_TOKEN_RELEASE_OWNER_PROOF_REQUIRED');
 validateCatalogAuthorityBaseline(owner.authority,now,110);
}

// Только проверка свежего private proof для stage receiver. Возврат не создаёт
// effect authority и не заменяет scope/source/API/positive-join guards stage.
export function verifyPairedTokenReleaseOwner(manifest,owner,now=Date.now()){
 validatePairedTokenReleaseManifest(manifest);requireOwner(manifest,owner,now);return owner;
}

// Граница исходного чтения для transport. Повторная проверка её не продлевает.
export function pairedTokenReleaseOwnerDeadline(manifest,owner){
 verifyPairedTokenReleaseOwner(manifest,owner);
 return ownerProofs.get(owner).until;
}

// План закрепляет реальные full specs; image+provenance — единственная будущая
// смена template. Масштабирование/positive join и обратная поставка пока не
// материализованы, поэтому ни patch, ни apply authority не возвращаются.
export function planPairedTokenRelease(manifest,{observed,owner,api,source,nodeReadback,now=Date.now()}){
 validatePairedTokenReleaseManifest(manifest);requireOwner(manifest,owner,now);
 ensure(source?.revision===manifest.sourceSHA,'PAIRED_TOKEN_RELEASE_SOURCE_CHANGED');
 const scope=portableRunnerTarget(manifest.target,observed);
 ensure(Array.isArray(observed.deployments)&&same(observed.deployments.map(d=>d.metadata?.name).sort(),
  [...pairedTokenReleaseNames,'control-api-gateway'].sort()),'PAIRED_TOKEN_RELEASE_DEPLOYMENTS_REJECTED');
 const gateway=observed.deployments.find(d=>d.metadata.name==='control-api-gateway');
 const admissionTarget=api.closed?.plan.target;
 ensure(observed.deployments.every(d=>scope.allows(d))&&scope.allows(api.service)&&fingerprint(gateway.spec)===manifest.apiSpecSHA256&&
  same(api.traffic?.plan.target,admissionTarget)&&['context','clusterUID','namespaceUID','installationUID','installationSHA256'].every(k=>
   admissionTarget?.[k]===manifest.target[k]),
  'PAIRED_TOKEN_RELEASE_API_SCOPE_CHANGED');
 requireClosedAPIMaintenance(gateway,api.resources,api.closed);
 const admission=requireClosedAPIAdmission(gateway,api.resources,api.closed,{...api.traffic,service:api.service,slices:api.slices,pods:api.pods});
 const parts=manifest.components.map(part=>{
  const actual=observed.deployments.find(d=>d.metadata.name===part.name),pod=actual.spec?.template?.spec;
  ensure(actual.metadata.uid===part.uid&&fingerprint(actual.spec)===part.beforeSpecSHA256&&
   actual.spec.template.metadata?.annotations?.['kodex.dev/component-source-sha']===part.beforeSourceSHA&&
   Number.isSafeInteger(actual.spec.replicas)&&actual.spec.replicas>=1&&actual.spec.replicas<=8&&
   actual.spec.paused!==true&&!pod?.hostNetwork&&!pod?.hostPID&&!pod?.hostIPC&&
   !pod?.volumes?.some(v=>v.hostPath)&&![...(pod?.containers??[]),...(pod?.initContainers??[])].some(c=>c.securityContext?.privileged),
   'PAIRED_TOKEN_RELEASE_SPEC_CHANGED');
  const apps=pod.containers?.filter(c=>c.name===part.name);
  ensure(apps?.length===1&&apps[0].image===part.beforeImage&&!apps[0].command&&!apps[0].args,
   'PAIRED_TOKEN_RELEASE_APPLICATION_CHANGED');
  requireInstanceGrants(actual.spec.template);
  const current=activeDeploymentPods(componentServingInventory(observed.serving[part.name],part.name,part.uid),part.uid);
  ensure(componentServingReady(actual,current,actual.spec.replicas),'PAIRED_TOKEN_RELEASE_SERVING_NOT_READY');
  const imageRef=validatedStablePublication(manifest.sourceSHA,part.publication,part.name);
  ensure(Array.isArray(nodeReadback?.images)&&nodeReadback.images.length===2&&Array.isArray(nodeReadback.binding)&&
   nodeReadback.binding.length>0&&nodeReadback.binding.length<=8&&
   new Set(nodeReadback.binding.map(n=>n.uid)).size===nodeReadback.binding.length&&nodeReadback.binding.every(n=>uid(n.uid))&&
   Array.isArray(observed.nodes)&&observed.nodes.length===nodeReadback.binding.length&&
   observed.nodes.every(n=>nodeReadback.binding.filter(b=>b.uid===n.metadata?.uid&&b.name===n.metadata.name).length===1&&
    !n.metadata.deletionTimestamp&&n.status?.conditions?.some(c=>c.type==='Ready'&&c.status==='True')&&
    n.status.conditions.some(c=>c.type==='DiskPressure'&&c.status==='False')),
   'PAIRED_TOKEN_RELEASE_NODE_PROOF_REQUIRED');
  const selected=nodeReadback.images.filter(i=>i.imageRef===imageRef);
  ensure(selected.length===1&&Array.isArray(selected[0].nodes)&&selected[0].nodes.length===nodeReadback.binding.length&&
   nodeReadback.binding.every(n=>selected[0].nodes.filter(v=>v.name===n.name&&v.uid===n.uid&&v.imageRef===imageRef&&v.repoDigestVerified===true).length===1),
   'PAIRED_TOKEN_RELEASE_NODE_PROOF_REQUIRED');
  const afterSpec=structuredClone(actual.spec);
  afterSpec.template.spec.containers.find(c=>c.name===part.name).image=imageRef;
  afterSpec.template.metadata.annotations['kodex.dev/component-source-sha']=manifest.sourceSHA;
  return {name:part.name,uid:part.uid,beforeResourceVersion:actual.metadata.resourceVersion,
   beforeSpec:structuredClone(actual.spec),afterSpec,beforeImage:part.beforeImage,imageRef,
   servingPodUIDs:current.map(p=>p.metadata.uid).sort(),publicationSHA256:fingerprint(part.publication)};
 });
 return {schema:'kodex.paired-token-release-plan/v1',profile:pairedTokenReleaseProfile,sourceSHA:manifest.sourceSHA,
  manifestSHA256:fingerprint(manifest),target:structuredClone(manifest.target),schemaHeads:pairedTokenReleaseHeads,
  ownerSHA256:fingerprint(owner),authority:structuredClone(owner.authority),apiAdmission:admission,
  nodeReadback:structuredClone(nodeReadback),components:parts,
  state:'PLAN_ONLY',releaseReadiness:'BLOCKED_LIFECYCLE_NOT_IMPLEMENTED'};
}

// Повторный capture не переводит план в APPLIED и не принимает замену owner
// scope. Точные временные/фактические proofs заново нужны каждому будущему effect.
export function verifyPairedTokenReleasePlan(plan,manifest,current){
 const fresh=planPairedTokenRelease(manifest,current);
 const immutable=value=>Object.fromEntries(Object.entries(value??{}).filter(([k])=>!['ownerSHA256','authority'].includes(k)));
 ensure(same(immutable(plan),immutable(fresh))&&
  sameCatalogAuthorityBaseline(plan.authority,fresh.authority),
  'PAIRED_TOKEN_RELEASE_PLAN_CHANGED');
 return fresh;
}
