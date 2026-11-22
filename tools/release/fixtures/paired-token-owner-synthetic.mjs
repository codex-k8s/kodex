import {fingerprint} from '../scoped-release.mjs';
import {baselineFixture} from './catalog-authority-baseline-synthetic.mjs';
import {pairedTokenReleaseFixture} from './paired-token-release-synthetic.mjs';
import {renderAuthorityPolicyEnvironment} from '../authority-policy-environment.mjs';
import {telegramRegistrySource} from '../authority-telegram-transition.mjs';
import {verifyPersonalIntegrationsAuthorityPolicy,planPersonalIntegrationsAuthorityRegistry,personalIntegrationsAuthorityTransition,personalIntegrationsAuthorityReadbackContract} from '../authority-personal-integrations-source.mjs';
import {deriveRegistryRotationIdentity} from '../authority-rotation-transition.mjs';
import {pairedTokenOwnerQueries} from '../paired-token-owner-database.mjs';
import {migrationOwnerDatabaseArgs,authorityOwnerDatabaseArgs} from '../migration-owner-database.mjs';
import {readCatalogTarget} from '../authority-catalog-transition.mjs';
import assert from 'node:assert/strict';

// Transport synthetic: исполняются настоящие 110 policy/registry/journal,
// canonical publisher inventory и sealed owner factory. Installed proof не создаётся.
export function pairedTokenOwnerFixture(t){
 // Только новый public source checkout получает runtime-readable modes;
 // приватный parent fixture остаётся0700, маска вызывающего восстанавливается.
 const previousMask=process.umask(0o022);let f;
 try{f=baselineFixture(t);}finally{process.umask(previousMask);}
 const pair=pairedTokenReleaseFixture(),previous=telegramRegistrySource('desired').raw;
 for(const key of ['context','clusterUID','namespaceUID','installationUID','installationSHA256'])f.profile[key]=pair.manifest.target[key];
 f.resources.get('namespace/kube-system').metadata.uid=f.profile.clusterUID;
 f.resources.get('namespace/kodex-system').metadata.uid=f.profile.namespaceUID;
 f.installation.metadata.uid=f.profile.installationUID;f.installation.data=structuredClone(pair.observed.installation.data);
 f.target=readCatalogTarget(f.profile,f.profile.context,(kind,name)=>structuredClone(f.resources.get(`${kind}/${name}`)),personalIntegrationsAuthorityTransition);
 f.plan.context=f.profile.context;f.plan.policyTransition=f.target.transition;
 const next=planPersonalIntegrationsAuthorityRegistry(previous),identity=deriveRegistryRotationIdentity(next.raw);
 f.registry.data['key-delivery-targets.yaml']=next.raw;
 f.registry.data['authority-policy.json']=renderAuthorityPolicyEnvironment(verifyPersonalIntegrationsAuthorityPolicy().desired.raw,f.target.transition.environment).raw;
 f.registry.metadata.annotations['kodex.dev/rotation-operation']=identity.ownerOperationID;
 f.publisher.spec.template.metadata.annotations['kodex.dev/authority-rotation-operation']=identity.ownerOperationID;
 f.publisherPod.spec=structuredClone(f.publisher.spec.template.spec);
 Object.assign(f.plan,{ownerOperationID:identity.ownerOperationID,personalRotationProtocol:'PERSONAL_INTEGRATIONS_ROTATION_V1'});
 f.plan.policyTransition.profile=personalIntegrationsAuthorityTransition;
 Object.assign(f.plan.registry,{desiredData:structuredClone(f.registry.data),desiredDataSHA256:fingerprint(f.registry.data),
  sourceRevision:identity.sourceRevision,sourceDigestSHA256:identity.sourceDigestSHA256,predecessorRaw:previous});
 f.plan.publisher.desiredSpecSHA256=fingerprint(f.publisher.spec);
 f.events.splice(3,0,{...f.events[1],operation:'CREATE',status:'INTENT'});
 for(const row of f.events){row.ownerOperationID=identity.ownerOperationID;if(row.planSHA256)row.planSHA256=fingerprint(f.plan);}
 Object.assign(f.owner,{operationId:identity.ownerOperationID,registryRevision:identity.sourceRevision,
  registrySourceDigestSHA256:identity.sourceDigestSHA256,expectedReadbackCount:personalIntegrationsAuthorityReadbackContract().expectedReadbackCount,
  actualReadbackCount:personalIntegrationsAuthorityReadbackContract().expectedReadbackCount});
 const postgres={pod:'kodex-postgresql-0',uid:f.pg.metadata.uid,ownerUID:f.set.metadata.uid};
 const controlPlane={...pair.controlPlane,postgres,backend:{...f.backend,database:'control_plane',databaseOID:'20001'}};
 const queries=pairedTokenOwnerQueries(),calls=[],faults={};
 const kube=(args,input)=>{
  calls.push({args,input});
  if(args.includes('exec')){
   if(String(input)===queries.epoch.sql){
    assert.deepEqual(args,migrationOwnerDatabaseArgs);
    const epoch={profile:'PAIRED_TOKEN_DATABASE_EPOCH',readOnly:'on',isolation:'repeatable read',database:'control_plane',
     systemIdentifier:f.backend.systemIdentifier,port:'5432',serverAddress:null,serverPort:null,
     databases:{control_plane:controlPlane.backend.databaseOID,internal_rpc_authority:f.backend.databaseOID}};
    faults.epoch?.(epoch);return JSON.stringify(epoch);
   }
   const authority=args.includes('--dbname=internal_rpc_authority');
   assert.deepEqual(args,authority?authorityOwnerDatabaseArgs:migrationOwnerDatabaseArgs);
   assert.equal(String(input),authority?queries.authority.sql:queries.controlPlane.sql);
   const value={profile:authority?'PAIRED_TOKEN_AUTHORITY110':'PAIRED_TOKEN_CP051',readOnly:'on',isolation:'repeatable read',
    backend:authority?f.backend:controlPlane.backend,...(authority?{head:{database:'internal_rpc_authority',readOnly:'on',gooseVersion:20261115000200},state:f.owner}:{proof:controlPlane.proof})};
   faults.response?.(value,authority);const raw=JSON.stringify(value);if(authority)faults.afterAuthority?.();return raw;
  }
  return f.kube(args,input);
 };
 return {...f,manifest:pair.manifest,controlPlane,postgres,calls,faults,kube};
}
