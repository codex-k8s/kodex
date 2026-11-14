import {createHash} from 'node:crypto';
import {fingerprint} from '../scoped-release.mjs';
import {componentMigrationNames} from '../portable-component-migration.mjs';
const revision='a'.repeat(40);
const uuid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const roots={'control-plane':'services/internal/control-plane/','runtime-controller':'services/internal/runtime-controller/',
  'integration-gateway':'services/external/integration-gateway/','control-api-gateway':'services/external/control-api-gateway/',
  'staff-control-center':'services/staff/control-center/'};

// Пять настоящих форматов component-lock с синтетическими pins; ни кластер,
// ни registry, ни пользовательские credentials тест не открывает.
export function componentCohortFixture(migrationProfile='github-repository-scope') {
  const names=componentMigrationNames(migrationProfile);
  const baseHeads={'control-plane':20261112000100,'internal-rpc-authority':2,'email-bridge':3};
  const value={schema:'kodex.portable-component-migration/v2',profile:migrationProfile,context:'portable-fixture',
    clusterUID:uuid(1),namespaceUID:uuid(2),baseSourceSHA:'b'.repeat(40),baseReleaseLockSHA256:'c'.repeat(64),
    baseSchemaHeads:baseHeads,schemaHeadsBefore:{...baseHeads,'control-plane':20261113000100},
    schemaHeadsAfter:{...baseHeads,'control-plane':20261114120000},runtimeABI:5,runnerInput:'v10',
    registry:'registry.example.invalid',repositoryPrefix:'synthetic/upgrade',nodeUIDs:[uuid(3)],components:[]};
  for(const [index,component] of names.entries()) {
    const anchor=roots[component]+'internal/synthetic.go',changed=[anchor];
    const digest='sha256:'+String(index+1).repeat(64);
    const lock={schema:'kodex.component-image/v1',component,source_sha:revision,base_application_source_sha:value.baseSourceSHA,
      base_release_lock_sha256:value.baseReleaseLockSHA256,image_ref:`${value.registry}/${value.repositoryPrefix}/${component}@${digest}`,digest,
      production_file_sha256:'1'.repeat(64),schema_heads:value.schemaHeadsAfter,
      compatibility:{changed_files:changed,production_files:changed,shared_libs_contracts_unchanged:false,
        verified_cohort:{components:names,changed_files:changed,production_files:changed,anchor,
          contract_change:'GITHUB_REPOSITORY_SCOPE_UPGRADE',schema_change:true,runner_input_abi:'UNCHANGED'}},
      build:{dockerfile_sha256:'2'.repeat(64),metadata_sha256:'3'.repeat(64),private_source_sha:'4'.repeat(40),platform:'linux/amd64'},
      qualification:'PUBLISHED_NOT_NODE_QUALIFIED_NOT_DEPLOYED'};
    const part={component,expectedImageRef:`registry.example.invalid/base/${component}@sha256:${'f'.repeat(64)}`};
    replaceComponentLock(part,lock);
    part.nodeProof={schema:'kodex.component-node-proof/v1',component,sourceSHA:revision,componentLockSHA256:part.componentLockSHA256,
      imageRef:lock.image_ref,clusterUID:value.clusterUID,namespaceUID:value.namespaceUID,
      nodes:[{name:'node-0',uid:uuid(3),imageRef:lock.image_ref,repoDigestVerified:true}]};
    value.components.push(part);
  }
  const deployments=names.map((name,index)=>{
    const replicas=name==='control-api-gateway'?0:name==='control-plane'?3:2;
    return {apiVersion:'apps/v1',kind:'Deployment',metadata:{name,namespace:'kodex-system',uid:uuid(10+index),resourceVersion:'1',generation:1,
      labels:{'app.kubernetes.io/name':name,'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'}},
      spec:{replicas,template:{metadata:{annotations:{synthetic:'retained'}},spec:{containers:[{name,image:value.components[index].expectedImageRef,
        env:[{name:'SYNTHETIC',value:'unchanged'}]}]}}},status:{observedGeneration:1,replicas,updatedReplicas:replicas,readyReplicas:replicas,availableReplicas:replicas}};
  });
  const profile={version:1,identity:{mode:'bundled'},public_tls:{mode:'managed'}};
  const installation={apiVersion:'v1',kind:'ConfigMap',metadata:{name:'kodex-installation-profile',namespace:'kodex-system',uid:uuid(20),
    labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/managed-by':'kodex-install'}},data:{'profile.json':JSON.stringify(profile),
    'profile.sha256':fingerprint(profile),'source.sha':value.baseSourceSHA,'release-lock.sha256':value.baseReleaseLockSHA256,'schema-heads.json':JSON.stringify(baseHeads)}};
  const target={schema:'kodex.portable-runner-target/v1',context:value.context,clusterUID:value.clusterUID,namespaceUID:value.namespaceUID,
    installationUID:installation.metadata.uid,installationSHA256:fingerprint(installation.data),
    resources:deployments.map(d=>({apiVersion:d.apiVersion,kind:d.kind,name:d.metadata.name,uid:d.metadata.uid}))};
  const observed={sourceRevision:revision,context:value.context,clusterUID:value.clusterUID,namespaceUID:value.namespaceUID,installation,deployments,
    nodes:[{metadata:{uid:uuid(3),name:'node-0'},status:{conditions:[{type:'Ready',status:'True'}]}}]};
  return {value,deployments,target,observed};
}

export function replaceComponentLock(part,lock) {
  const raw=Buffer.from(JSON.stringify(lock,null,2)+'\n');
  part.componentLockRawBase64=raw.toString('base64');
  part.componentLockSHA256=createHash('sha256').update(raw).digest('hex');
  if(part.nodeProof)part.nodeProof.componentLockSHA256=part.componentLockSHA256;
}
