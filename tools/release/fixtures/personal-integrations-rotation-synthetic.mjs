import assert from 'node:assert/strict';
import {fileURLToPath} from 'node:url';
import {fixture as authorityFixture,uid,image} from './authority-catalog-synthetic.mjs';
import {fingerprint} from '../scoped-release.mjs';
import {catalogAuthorityBaselineSQLSHA256} from '../catalog-authority-baseline.mjs';
import {authorityPolicyReadbackContract,readCatalogTarget} from '../authority-catalog-transition.mjs';
import {personalIntegrationsAuthorityTransition,planPersonalIntegrationsAuthorityRegistry,verifyPersonalIntegrationsAuthorityPolicy} from '../authority-personal-integrations-source.mjs';
import {telegramRegistrySource,telegramAuthorityReadbackContract} from '../authority-telegram-transition.mjs';
import {renderAuthorityPolicyEnvironment} from '../authority-policy-environment.mjs';
import {deriveRegistryRotationIdentity,createCanonicalRotationTemplate,createRotationJob} from '../authority-rotation-transition.mjs';
import {personalIntegrationsRotationProtocol} from '../personal-integrations-rotation-journal.mjs';
import {issuerAssetsResourcesSchema,issuerAssetsResourceScope} from '../artifact-retention-issuer-assets.mjs';

// Синтетические Kubernetes/owner ports. Настоящие source policy, registry,
// Job/Pod cardinality и typed guards исполняются; live SQL/crypto NOT RUN.
export function personalRotationFixture(){
 const f=authorityFixture(),profile=personalIntegrationsAuthorityTransition;
 const source=fileURLToPath(new URL('../../../',import.meta.url)).replace(/\/$/,''),revision='a'.repeat(40),now=Date.now();
 const transition=readCatalogTarget(f.profile,f.profile.context,f.get,profile).transition;
 const predecessorRaw=telegramRegistrySource('desired').raw,advance=planPersonalIntegrationsAuthorityRegistry(predecessorRaw),identity=deriveRegistryRotationIdentity(advance.raw);
 const policy=renderAuthorityPolicyEnvironment(verifyPersonalIntegrationsAuthorityPolicy().desired.raw,transition.environment);
 f.registry.data={'authority-policy.json':policy.raw,'key-delivery-targets.yaml':advance.raw,unrelated:'unchanged'};
 const plan={personalRotationProtocol:personalIntegrationsRotationProtocol,version:3,intentID:uid(100),action:'rotate',context:f.profile.context,kubeconfig:'/synthetic/kubeconfig',source,revision,
  policyTransition:transition,cliImage:image,cliCommand:['/usr/local/bin/internal-rpc-authority-cli'],ownerOperationID:identity.ownerOperationID};
 f.registry.metadata.annotations={'kodex.dev/rotation-intent':plan.intentID,'kodex.dev/rotation-operation':plan.ownerOperationID};
 f.publisher.spec.template.metadata.annotations={'kodex.dev/authority-rotation-intent':plan.intentID,'kodex.dev/authority-rotation-operation':plan.ownerOperationID};
 plan.registry={uid:f.registry.metadata.uid,desiredData:structuredClone(f.registry.data),desiredDataSHA256:fingerprint(f.registry.data),predecessorRaw,
  sourceRevision:identity.sourceRevision,sourceDigestSHA256:identity.sourceDigestSHA256,policySHA256:policy.renderedSHA256};
 plan.publisher={uid:f.publisher.metadata.uid,image,desiredSpecSHA256:fingerprint(f.publisher.spec)};
 const target=Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(key=>[key,f.profile[key]]));
 const apiTarget={...f.profile,resources:f.profile.resources.filter(r=>r.name==='control-api-gateway')};
 const apiAdmission={schema:'kodex.personal-integrations-api-admission/v1',profile:'personal-integrations-backup',sourceSHA:revision,
  apiTargetSHA256:fingerprint(apiTarget),deploymentUID:f.api.metadata.uid,serviceUID:f.service.metadata.uid,
  closedPlanSHA256:'b'.repeat(64),isolationPlanSHA256:'c'.repeat(64)};
 const controlPlanePolicy={name:'synthetic-policy110',uid:uid(201),projectionSHA256:'d'.repeat(64),policySHA256:policy.renderedSHA256};
 const refs={schema:'kodex.personal-integrations-release-inputs/v1',authorityTarget:f.profile,
  material:{planPath:'/synthetic/material',evidencePath:'/synthetic/material-events',sourceSHA:'b'.repeat(40)},
  issuerMigration:{planPath:'/synthetic/role',evidencePath:'/synthetic/role-events',sourceSHA:'c'.repeat(40)},
  assets:{planPath:'/synthetic/assets',evidencePath:'/synthetic/asset-events'},
  authorityImage:{publicationPlanPath:'/synthetic/publication',publicationEvidencePath:'/synthetic/publication-events',nodeProofPath:'/synthetic/nodes'}};
 const migrationJobName='control-plane-migrate-20261114170000';
 const migration={version:2,kind:'CONTROL_PLANE_MIGRATION',source:{path:source,revision},profile:apiAdmission.profile,migrationVersion:'20261114170000',job:{metadata:{name:migrationJobName}},
  context:plan.context,kubeconfig:plan.kubeconfig,portableTarget:f.profile,apiMaintenance:apiAdmission,personalIntegrationsRelease:refs,
  componentHandoff:{publicationSHA256:'e'.repeat(64),components:[{name:'control-plane',uid:f.deployment.metadata.uid,stagedSpec:{replicas:0}}]}};
 const resources={schema:issuerAssetsResourcesSchema,purpose:'PRE_ROTATION',sourceSHA:revision,planSHA256:'f'.repeat(64),
  resources:issuerAssetsResourceScope.map((scope,i)=>({...scope,uid:uid(300+i),action:i<9?'create':'patch',bodySHA256:String(i%9).repeat(64)})),
  certificate:{uid:uid(307),ready:true,tlsSecretUID:uid(400),tlsMetadataSHA256:'1'.repeat(64)}};
 const scope={schema:'kodex.personal-integrations-authority-owner-scope/v1',profile,context:plan.context,kubeconfig:plan.kubeconfig,sourceRevision:revision,
  targetSHA256:transition.targetSHA256,executorSource:source,executorRevision:revision,sqlSHA256:catalogAuthorityBaselineSQLSHA256,
  postgres:{pod:'kodex-postgresql-0',uid:uid(500),ownerUID:uid(501)},
  postgresBackend:{systemIdentifier:'18446744073709551615',databaseOID:'20000',database:'internal_rpc_authority',port:'5432',serverAddress:null,serverPort:null}};
 plan.ownerScope=scope;
 plan.maintenance={schema:'kodex.personal-integrations-staged-admission/v1',sourceSHA:revision,target,apiTarget,apiAdmission:structuredClone(apiAdmission),apiAdmissionSHA256:fingerprint(apiAdmission),
  cohort049:{phase:'STAGED',sourceSHA:revision,migrationPlanSHA256:fingerprint(migration),migrationTargetSHA256:'2'.repeat(64),jobUID:uid(600)},
  owner107:{schema:'kodex.personal-integrations-authority-predecessor107/v1',policyRevision:107,sourceSHA:revision,targetSHA256:transition.targetSHA256,
   ownerScope:scope,...telegramAuthorityReadbackContract(),snapshotRevision:7,snapshotSHA256:'3'.repeat(64),operationID:uid(601)},
  issuerMigrationReady:{schema:'kodex.artifact-retention-issuer-migration-ready/v1',migrationVersion:20261115000200},
  materialReady:{schema:'kodex.artifact-retention-issuer-ready/v1'},assetsResources:structuredClone(resources),controlPlanePolicy:structuredClone(controlPlanePolicy),
  artifactRetentionStage:{uid:uid(602),stagedSpecSHA256:'4'.repeat(64)}};
 const owner={observedAt:new Date(now).toISOString(),operationId:plan.ownerOperationID,operationStatus:'DISTRIBUTING',status:'DISTRIBUTING',phase:'DISTRIBUTE',
  registryRevision:identity.sourceRevision,registrySourceDigestSHA256:identity.sourceDigestSHA256,baseRevision:7,baseDigestSHA256:'3'.repeat(64),
  sourceRevision:8,sourceDigestSHA256:'5'.repeat(64),intentId:uid(603),protocolVersion:2,expectedReadbackCount:authorityPolicyReadbackContract(profile).expectedReadbackCount,
  actualReadbackCount:0,deliveryStartedAt:new Date(now-10000).toISOString(),deliveredAt:new Date(now-5000).toISOString()};
 const freshPlan={personalRotationProtocol:personalIntegrationsRotationProtocol,version:3,intentID:uid(604),action:'status',context:plan.context,kubeconfig:plan.kubeconfig,source,revision,policyTransition:transition,cliImage:image,ownerScope:structuredClone(scope)};
 const expected=createRotationJob(createCanonicalRotationTemplate(source,image,true),freshPlan),job={...structuredClone(expected),status:{succeeded:1}};
 job.metadata.uid=uid(605);
 const pod={apiVersion:'v1',kind:'Pod',metadata:{name:'status-pod',namespace:'kodex-system',uid:uid(606),ownerReferences:[{kind:'Job',name:job.metadata.name,uid:job.metadata.uid,controller:true}]},
  spec:structuredClone(job.spec.template.spec),status:{phase:'Succeeded'}};
 for(const field of ['containers','initContainers','ephemeralContainers'])pod.status[field==='containers'?'containerStatuses':field==='initContainers'?'initContainerStatuses':'ephemeralContainerStatuses']=
  (pod.spec[field]??[]).map(c=>({name:c.name,restartCount:0,state:{terminated:{exitCode:0}},imageID:c.image}));
 const at=new Date(now).toISOString(),row=(p,status,extra={})=>({at,intentID:p.intentID,ownerOperationID:p.ownerOperationID,status,...extra});
 const events=[row(plan,'INTENT',{action:'rotate',planSHA256:fingerprint(plan)}),row(plan,'APPLIED',{operation:'REGISTRY_CAS'}),row(plan,'APPLIED',{operation:'PUBLISHER_RESTART'}),row(plan,'INTENT',{operation:'CREATE'}),row(plan,'RUNNING',{jobUID:uid(607)})];
 const freshEvents=[row(freshPlan,'INTENT',{action:'status',planSHA256:fingerprint(freshPlan)}),row(freshPlan,'INTENT',{operation:'CREATE'}),row(freshPlan,'SUCCEEDED',{jobUID:job.metadata.uid,rotationStatus:owner.status})];
 const migrationEvents=[{type:'HEADER',version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(migration)},
  {type:'CREATE_INTENT',jobName:migrationJobName},{type:'CREATE_ACK',jobUID:uid(600)},
  {type:'READBACK',jobName:migrationJobName,jobUID:uid(600),status:'PASS',schemaPostcondition:'PASS',sourceRevision:revision,migrationVersion:migration.migrationVersion},
  {type:'PAIR_HANDOFF_ACK',publicationSHA256:migration.componentHandoff.publicationSHA256}];
 const recorded=structuredClone(owner),calls=[];
 const kube=args=>{
  calls.push(args);assert.ok(!args.some(arg=>['create','patch','replace','delete','apply'].includes(arg)));
  if(args.includes('logs'))return JSON.stringify(recorded)+'\n';
  const i=args.indexOf('get'),kind=args[i+1],name=args[i+2];
  if(kind==='job')return JSON.stringify(job);
  if(kind==='pods')return JSON.stringify({items:[pod]});
  const value=f.resources.get(kind+'/'+name);assert.ok(value,kind+'/'+name);return JSON.stringify(value);
 };
 const custody={schema:'kodex.personal-integrations-installed-assets-custody/v1',purpose:'CUSTODY_ONLY',sourceSHA:revision,planSHA256:resources.planSHA256,
  resources:resources.resources.map(({bodySHA256,...r})=>({...r,intentSHA256:bodySHA256})),certificate:structuredClone(resources.certificate)};
 const currentAPI={admission:structuredClone(apiAdmission),isolation:{plan:{target:f.profile}}};
 const io={inspectSource:()=>({revision}),readOwner:()=>({state:structuredClone(owner),scope}),
  readMaterial:()=>structuredClone(plan.maintenance.materialReady),readPolicy:()=>structuredClone(controlPlanePolicy),readAPI:()=>structuredClone(currentAPI),readCustody:()=>structuredClone(custody),
  readPrivateJSON:path=>{assert.equal(path,refs.assets.planPath);return {};},readEvents:path=>{assert.equal(path,refs.assets.evidencePath);return [];},now:()=>now};
 const input={plan,events,freshPlan,freshEvents,migration:{plan:migration,events:migrationEvents}};
 const seal=()=>{events[0].planSHA256=fingerprint(plan);freshEvents[0].planSHA256=fingerprint(freshPlan);job.metadata.annotations['kodex.dev/rotation-plan-sha256']=fingerprint(freshPlan);};
 return {...f,plan,migration,events,freshPlan,freshEvents,input,owner,recorded,kube,io,now,assetResources:resources,custody,controlPlanePolicy,job,pod,currentAPI,seal,calls};
}
