import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {readFileSync,writeFileSync} from 'node:fs';
import {join} from 'node:path';
import {deriveRegistryRotationIdentity} from '../authority-rotation-transition.mjs';
import {renderAuthorityPolicyEnvironment} from '../authority-policy-environment.mjs';
import {telegramAuthorityReadbackContract,telegramPolicySource,telegramRegistrySource} from '../authority-telegram-transition.mjs';
import {qualifyComponentNodeImages} from '../component-node-qualification.mjs';
import {fingerprint} from '../scoped-release.mjs';
import {validateStableReceipt} from '../stable-image-publication.mjs';
import {validateNodeContainer} from '../stable-node-registry-model.mjs';
import {artifactCatalogPrerequisitesFixture} from './artifact-catalog-prerequisites-synthetic.mjs';
import {personalStagedInputsSchema,readArtifactRetentionIssuerInputs} from '../artifact-retention-issuer-inputs.mjs';
import {readPersonalIntegrationsAuthorityImage} from '../personal-integrations-authority-image.mjs';
import {readPersonalIntegrationsAuthorityPredecessor,personalIntegrationsAuthorityPredecessorPins} from '../personal-integrations-authority-predecessor.mjs';
import {readPersonalIntegrationsCPPolicyInput} from '../personal-integrations-cp-policy.mjs';
import {verifyPersonalIntegrationsMaterialSource} from '../personal-integrations-material-source.mjs';
import {bindPersonalIntegrationsDeploymentPlan,personalIntegrationsReleaseReferences} from '../personal-integrations-deployment-inputs.mjs';
import {planComponentMigrationHandoff} from '../portable-component-migration.mjs';

// Изолирует чтение Docker и CRI: ни одна команда fixture не попадает в Docker.
function authorityDockerReadback(container,imageRef,args) {
 if(args[0]==='inspect'){
  assert.deepEqual(args,['inspect','--type','container',container.Name.slice(1)]);
  return JSON.stringify([container]);
 }
 assert.deepEqual(args,['exec',container.Id,'crictl','--runtime-endpoint','unix:///run/k3s/containerd/containerd.sock',
  '--image-endpoint','unix:///run/k3s/containerd/containerd.sock','inspecti','-q','-o','json',imageRef]);
 return JSON.stringify({status:{id:imageRef.slice(imageRef.indexOf('@')+1),repoDigests:[imageRef]}});
}

export async function prepareArtifactCatalogAuthorityImage({directory,source,cohort,save,journal},kubeconfig) {
 const target=cohort.target,node=cohort.observed.nodes[0];
 const container={Id:'a'.repeat(64),Image:'sha256:'+'b'.repeat(64),Name:'/'+node.metadata.name,State:{Running:true},
  Config:{Labels:{'k3d.cluster':target.context.slice(4),'k3d.role':'server'},Cmd:[],Env:[],Entrypoint:['/bin/k3s']}};
 validateNodeContainer(node,container,target.context.slice(4));
 const component='internal-rpc-authority',sourceSHA=source.revision,imageDigest='sha256:'+'d'.repeat(64);
 const receipt={schema:'kodex.stable-local-build/v1',component,sourceSHA,contextSHA256:'b'.repeat(64),archiveSHA256:'c'.repeat(64),
  imageDigest,publication:'NOT_RUN',admission:'NOT_RUN',nodePull:'NOT_RUN',dockerfile:'services/internal/internal-rpc-authority/Dockerfile'};
 const plan={schema:'kodex.stable-image-publication/v2',artifactProfile:'built',
  context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,
  receipt,image:validateStableReceipt(receipt),archiveSHA256:receipt.archiveSHA256};
 const events=[{schema:'kodex.stable-image-publication-attempt/v1',status:'INTENT',mode:'apply',
  planSHA256:fingerprint(plan),component,digest:imageDigest},{status:'PASS',phase:'REGISTRY_PUBLICATION_ONLY',nodePull:'NOT_RUN'}];
 const imageRef='registry.local.kodex/kodex/'+component+'@'+imageDigest;
 const operation=(program,args)=>{
  if(program==='docker')return authorityDockerReadback(container,imageRef,args);
  assert.equal(program,'kubectl');
  if(args.includes('kube-system'))return JSON.stringify({metadata:{uid:target.clusterUID}});
  if(args.includes('kodex-system'))return JSON.stringify({metadata:{uid:target.namespaceUID}});
  assert.ok(args.includes('nodes'));
  return JSON.stringify({items:[node]});
 };
 const proof=await qualifyComponentNodeImages({target:{...target,nodes:[node.metadata]},kubeconfig,images:[imageRef],mode:'readback'},operation);
 save('authority-publication.json',plan);journal('authority-publication.jsonl',events);save('authority-node-proof.json',proof);
 const fake='#!'+process.execPath+'\nconst assert=require("node:assert/strict");\nprocess.stdout.write(('+
  authorityDockerReadback.toString()+')('+JSON.stringify(container)+','+JSON.stringify(imageRef)+',process.argv.slice(2)));\n';
 writeFileSync(join(directory,'bin','docker'),fake,{mode:0o700});
 const reference={publicationPlanPath:join(directory,'authority-publication.json'),publicationEvidencePath:join(directory,'authority-publication.jsonl'),
  nodeProofPath:join(directory,'authority-node-proof.json')};
 return {reference,plan,events,operation};
}

export function prepareArtifactCatalogAuthority107({apps,baseline,directory,source}) {
 const {environment}=baseline.plan.policyTransition;
 baseline.registry.data['key-delivery-targets.yaml']=telegramRegistrySource('desired').raw;
 const rendered=renderAuthorityPolicyEnvironment(telegramPolicySource('desired').raw,environment);
 baseline.registry.data['authority-policy.json']=rendered.raw;
 const identity=deriveRegistryRotationIdentity(baseline.registry.data['key-delivery-targets.yaml']);
 const readers=telegramAuthorityReadbackContract();
 Object.assign(baseline.owner,{operationId:identity.ownerOperationID,registryRevision:identity.sourceRevision,
  registrySourceDigestSHA256:identity.sourceDigestSHA256,
  expectedReadbackCount:readers.expectedReadbackCount,actualReadbackCount:readers.expectedReadbackCount});
 const name='control-plane-policy-projection-'+rendered.renderedSHA256.slice(0,16);
 const cp=apps['control-plane'],projection=cp.spec.template.spec.volumes.find(volume=>volume.name==='control-plane-environment-policy');
 assert.ok(projection);
 projection.configMap.name=name;
 const before={apiVersion:'v1',kind:'ConfigMap',immutable:true,
  metadata:{name,namespace:'kodex-system',uid:'00000000-0000-4000-8000-000000000470',labels:{'app.kubernetes.io/part-of':'kodex'}},
  data:{'policy.json':rendered.raw}};
 baseline.resources.set('configmap/'+name,before);
 const artifactIssuerRenderPath=join(directory,'checked-issuer-render.yaml');
 writeFileSync(artifactIssuerRenderPath,execFileSync('kustomize',['build',join(source.path,'deploy/k8s/profiles/web-only')],{maxBuffer:16<<20}),{mode:0o600});
 return {configMaps:{[name]:before},artifactIssuerRenderPath,
  kube:(args,input)=>{
   if(args[0]==='kustomize')return readFileSync(artifactIssuerRenderPath,'utf8');
   if(args.includes('exec')&&String(input).includes('authority_rotation_status()'))baseline.owner.observedAt=new Date().toISOString();
   return baseline.kube(args,input);
  }};
}

export async function prepareArtifactCatalogPrerequisites(t,context) {
 const {apps,baseline,cohort,directory,kubeconfig,migration,source}=context;
 const before=prepareArtifactCatalogAuthority107(context);
 const image=await prepareArtifactCatalogAuthorityImage(context,kubeconfig);
 const target=cohort.target,authorityTarget=baseline.profile,sourceSHA=source.revision;
 const prerequisites=await artifactCatalogPrerequisitesFixture(t,{source,target,installation:cohort.observed.installation,
  postgres:migration.postgres,backend:baseline.backend,
  publication:{publicationPlan:image.plan,publicationEvidence:image.events}});
 const owner107=readPersonalIntegrationsAuthorityPredecessor({source:source.path,sourceSHA,context:target.context,kubeconfig,
  targetProfile:authorityTarget},before.kube);
 const {material,issuerMigration}=prerequisites;
 const releaseReferences=personalIntegrationsReleaseReferences({authorityTarget,material,issuerMigration,authorityImage:image.reference,
  assets:{planPath:join(directory,'asset-plan.json'),evidencePath:join(directory,'asset-evidence.jsonl')}});
 const controlPlane=apps['control-plane'];
 const inputs={schema:personalStagedInputsSchema,sourceSHA,issuerInputs:readArtifactRetentionIssuerInputs(source,before.kube),
  authority:await readPersonalIntegrationsAuthorityImage({...image.reference,sourceSHA,target,kubeconfig},{operation:image.operation}),
  cpPolicy:readPersonalIntegrationsCPPolicyInput(sourceSHA,authorityTarget,before.kube,controlPlane.spec)};
 bindPersonalIntegrationsDeploymentPlan(migration,{releaseReferences,inputs,prerequisites:{
  schema:'kodex.personal-integrations-deployment-prerequisites/v1',sourceSHA,owner107:personalIntegrationsAuthorityPredecessorPins(owner107),
  materialSource:verifyPersonalIntegrationsMaterialSource({source:source.path,sourceSHA,materialSourceSHA:material.sourceSHA}),
  materialReady:prerequisites.materialReady,issuerMigrationReady:prerequisites.issuerReady}});
 migration.componentHandoff=planComponentMigrationHandoff(cohort.value,Object.values(apps),source,undefined,inputs);
 migration.anchor.specSHA256=fingerprint(controlPlane.spec);
 migration.anchor.stoppedSpecSHA256=fingerprint({...controlPlane.spec,replicas:0});
 return {artifactPrerequisites:prerequisites.state,artifactIssuerRenderPath:before.artifactIssuerRenderPath,configMaps:before.configMaps};
}
