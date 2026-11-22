import {fingerprint} from '../scoped-release.mjs';
import {validateStableReceipt} from '../stable-image-publication.mjs';
import {catalogAuthorityBaselineSchema,catalogAuthorityBaselineSQLSHA256} from '../catalog-authority-baseline.mjs';
import {planComponentAPIMaintenance} from '../component-api-maintenance.mjs';
import {planComponentAPITraffic} from '../component-api-traffic.mjs';
import {componentCohortFixture} from './component-cohort-synthetic.mjs';
import {pairedTokenReleaseProfile,pairedTokenReleaseHeads,pairedTokenReleaseNames,readPairedTokenReleaseOwner} from '../paired-token-release-plan.mjs';

export const pairedTokenReleaseFixtureUID=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
export const pairedTokenReleaseFixtureSource='a'.repeat(40),pairedTokenReleaseFixtureDigest='d'.repeat(64);
const uid=pairedTokenReleaseFixtureUID,source=pairedTokenReleaseFixtureSource,digest=pairedTokenReleaseFixtureDigest;

// Только локальные данные: существующие portable/API/publication validators
// исполняются напрямую; reader ports ниже — явно synthetic, не installed proof.
export function pairedTokenReleaseFixture({broaderAdmission=false}={}){
 const f=componentCohortFixture('github-repository-scope');
 const observed=f.observed;observed.context='k3d-fixture';
 observed.nodes[0].status.conditions.push({type:'DiskPressure',status:'False'});
 observed.deployments=observed.deployments.filter(d=>[...pairedTokenReleaseNames,'control-api-gateway'].includes(d.metadata.name));
 observed.serving={};
 const labels={'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'};
 const service={apiVersion:'v1',kind:'Service',metadata:{name:'control-api-gateway',namespace:'kodex-system',uid:uid(90),resourceVersion:'1',labels},
  spec:{type:'ClusterIP',selector:{'app.kubernetes.io/name':'control-api-gateway'},ports:[{name:'https',port:8443,targetPort:'https'}]}};
 const target={...f.target,context:observed.context,resources:[...observed.deployments,service].map(d=>
  ({apiVersion:d.apiVersion,kind:d.kind,name:d.metadata.name,uid:d.metadata.uid}))};
 const components=pairedTokenReleaseNames.map((name,index)=>{
  const d=observed.deployments.find(d=>d.metadata.name===name);
  d.spec.replicas=1;d.status={observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1};
  const beforeImage=`registry.local.kodex/kodex/${name}@sha256:${'f'.repeat(64)}`;
  d.spec.template.spec.containers[0].image=beforeImage;
  d.spec.template.metadata.annotations['kodex.dev/component-source-sha']='b'.repeat(40);
  // Sidecar и native init должны целиком сохраниться в proposed spec.
  d.spec.template.spec.containers.push({name:'issuer',image:`registry.local.kodex/kodex/internal-rpc-authority@sha256:${'7'.repeat(64)}`,
   env:[{name:'SYNTHETIC',value:'retained'}]});
  const rs={kind:'ReplicaSet',metadata:{uid:uid(60+index),ownerReferences:[{kind:'Deployment',uid:d.metadata.uid,controller:true}]}};
  const p={kind:'Pod',metadata:{uid:uid(70+index),namespace:'kodex-system',labels:{'app.kubernetes.io/name':name},
   ownerReferences:[{kind:'ReplicaSet',uid:rs.metadata.uid,controller:true}]},spec:structuredClone(d.spec.template.spec),
   status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:d.spec.template.spec.containers.map(c=>
    ({name:c.name,ready:true,state:{running:{}},imageID:c.image}))}};
  observed.serving[name]={items:[rs,p]};
  const receipt={schema:'kodex.stable-local-build/v1',component:name,sourceSHA:source,contextSHA256:digest,archiveSHA256:digest,
   imageDigest:'sha256:'+String(index+1).repeat(64),dockerfile:(name==='control-plane'?'services/internal/control-plane/':'services/external/integration-gateway/')+'Dockerfile',
   target:'',admission:'NOT_RUN',publication:'NOT_RUN',nodePull:'NOT_RUN'};
  const plan={schema:'kodex.stable-image-publication/v2',artifactProfile:'built',context:observed.context,clusterUID:target.clusterUID,
   namespaceUID:target.namespaceUID,source,receipt,image:validateStableReceipt(receipt),archiveSHA256:receipt.archiveSHA256};
  const publication={publicationPlan:plan,publicationEvidence:[{schema:'kodex.stable-image-publication-attempt/v1',status:'INTENT',mode:'apply',
   planSHA256:fingerprint(plan),component:name,digest:plan.image.digest},{status:'PASS',phase:'REGISTRY_PUBLICATION_ONLY',nodePull:'NOT_RUN'}]};
  return {name,uid:d.metadata.uid,beforeImage,beforeSourceSHA:'b'.repeat(40),beforeSpecSHA256:fingerprint(d.spec),publication};
 });
 const manifest={schema:'kodex.paired-token-release-manifest/v1',profile:pairedTokenReleaseProfile,sourceSHA:source,
  schemaHeadsBefore:{...pairedTokenReleaseHeads},schemaHeadsAfter:{...pairedTokenReleaseHeads},target,components};
 const gateway=observed.deployments.find(d=>d.metadata.name==='control-api-gateway');
 gateway.spec.template.spec.containers[0].ports=[{name:'https',containerPort:8443,protocol:'TCP'}];
 gateway.spec.replicas=1;gateway.status={observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1};
 const originalTarget=structuredClone(target);
 if(broaderAdmission)originalTarget.resources.push({apiVersion:'apps/v1',kind:'Deployment',name:'runtime-controller',uid:uid(94)});
 const close=planComponentAPIMaintenance(gateway,originalTarget,observed,source,'close');
 const closed={plan:close,events:[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(close),maintenanceID:close.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'CLOSED',deploymentUID:gateway.metadata.uid,maintenanceID:close.maintenanceID}]};
 gateway.spec=structuredClone(close.afterSpec);gateway.metadata.annotations=structuredClone(close.afterAnnotations);
 manifest.apiSpecSHA256=fingerprint(gateway.spec);
 gateway.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
 const resources={items:[]},trafficPlan=planComponentAPITraffic({service,deployment:gateway,resources,target:originalTarget,observed,sourceSHA:source,action:'isolate',closed});
 const traffic={plan:trafficPlan,events:[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(trafficPlan),maintenanceID:close.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'ISOLATED',serviceUID:service.metadata.uid,maintenanceID:close.maintenanceID}]};
 service.spec=structuredClone(trafficPlan.afterSpec);service.metadata.annotations=structuredClone(trafficPlan.afterAnnotations);
 const api={closed,traffic,service,resources,slices:{items:[]},pods:{items:[]}};
 const now=Date.now(),postgres={pod:'kodex-postgresql-0',uid:uid(80),ownerUID:uid(81)};
 const backend={database:'internal_rpc_authority',databaseOID:'20000',systemIdentifier:'12345678901234567890',port:'5432',serverAddress:null,serverPort:null};
 const authority={schema:catalogAuthorityBaselineSchema,policyRevision:110,operationID:uid(82),registryUID:uid(83),publisherUID:uid(84),
  policySHA256:digest,registryDataSHA256:digest,registrySHA256:digest,publisherSpecSHA256:digest,snapshotSHA256:digest,rotationPlanSHA256:digest,
  rotationEvidenceSHA256:digest,targetSHA256:digest,requiredReadbackTargetsSHA256:digest,registryRevision:5,snapshotRevision:6,
  expectedReadbackCount:8,registeredReadbackCount:8,actualReadbackCount:8,sqlSHA256:catalogAuthorityBaselineSQLSHA256,
  scope:Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(k=>[k,target[k]])),
  postgres,postgresBackend:backend,observedAt:new Date(now).toISOString()};
 authority.baselineSHA256=fingerprint(authority);
 const controlPlane={postgres,backend:{...backend,database:'control_plane',databaseOID:'20001'},proof:{profile:'WORKFLOW_FILE_PROVENANCE_BACKUP_051',
  database:'control_plane',readOnly:'on',gooseVersion:20261121000200,serverVersionNum:180003,backupQualification:'CANONICAL_BACKUP',
  publicSequenceSafe:true,historyMatches:true,schemaMatches:true,artifactPredecessorApplied:true,
  schemaCatalog:{catalogDigest:'31d4205868c2e96b508f8c1ce3572a55477407580486053a23c16044227d80ad'}}};
 const head={database:'internal_rpc_authority',readOnly:'on',gooseVersion:20261115000200,postgres,backend};
 const ports={readControlPlane:()=>controlPlane,readAuthorityHead:()=>head,readAuthority:()=>authority,now:()=>now};
 const nodeReadback={binding:[{name:'node-0',uid:uid(3)}],images:components.map(c=>{
  const imageRef=`registry.local.kodex/kodex/${c.name}@${c.publication.publicationPlan.image.digest}`;
  return {imageRef,nodes:[{name:'node-0',uid:uid(3),imageRef,repoDigestVerified:true}]};})};
 const current=()=>({observed,owner:readPairedTokenReleaseOwner(manifest,ports),api,source:{revision:source},nodeReadback,now});
 return {manifest,observed,api,authority,controlPlane,head,ports,nodeReadback,current,now};
}
