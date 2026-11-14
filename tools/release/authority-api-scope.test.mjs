import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,rmSync,chmodSync} from 'node:fs';
import {requireAuthorityAPIScope} from './authority-api-scope.mjs';
import {verifyCatalogAdmission,catalogPolicyTransition} from './authority-catalog-transition.mjs';
import {telegramPolicyTransition} from './authority-telegram-transition.mjs';
import {fingerprint} from './scoped-release.mjs';

const uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function fixture(){
 const authority={schema:'kodex.portable-runner-target/v1',context:'k3d-synthetic',clusterUID:uid(1),namespaceUID:uid(2),
  installationUID:uid(3),installationSHA256:'a'.repeat(64),resources:[
   ['apps/v1','Deployment','control-api-gateway'],['v1','Service','control-api-gateway'],['apps/v1','Deployment','control-plane'],
   ['apps/v1','Deployment','internal-rpc-authority-publisher'],['v1','ConfigMap','internal-rpc-authority-publisher-target-registry']]
   .map(([apiVersion,kind,name],index)=>({apiVersion,kind,name,uid:uid(10+index)}))};
 return {authority,api:{...structuredClone(authority),resources:structuredClone(authority.resources.slice(0,2))}};
}

test('107 допускает exact API2 и полный authority5, сохраняя разные hashes',()=>{
 const f=fixture();assert.notEqual(fingerprint(f.api),fingerprint(f.authority));
 assert.doesNotThrow(()=>requireAuthorityAPIScope(f.api,f.authority));
 assert.doesNotThrow(()=>requireAuthorityAPIScope(f.authority,f.authority));
 f.api.resources.reverse();assert.doesNotThrow(()=>requireAuthorityAPIScope(f.api,f.authority));
 for(const mutate of [x=>x.api.clusterUID=uid(90),x=>x.api.namespaceUID=uid(90),x=>x.api.installationUID=uid(90),
  x=>x.api.context='k3d-foreign',x=>x.api.installationSHA256='b'.repeat(64),x=>x.api.schema='unknown',
  x=>x.api.resources[0].uid=uid(90),x=>x.api.resources[1].name='foreign',x=>x.api.resources.pop(),
  x=>x.api.resources.push(x.authority.resources[2]),x=>x.api.resources[1]=x.api.resources[0],
  x=>x.authority.resources.pop(),x=>x.authority.resources.push(x.authority.resources[0]),
  x=>x.authority.resources[2].name='foreign',x=>x.api.extra=true]){
  const broken=fixture();mutate(broken);assert.throws(()=>requireAuthorityAPIScope(broken.api,broken.authority),/SCOPE_MISMATCH/);
 }
});

test('107 admission перечитывает exact frozen close bytes;106 и переписанные pins отказаны',t=>{
 const directory=mkdtempSync('/tmp/kodex-api-scope-');chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const f=fixture(),closed={target:f.api,synthetic:'closed'},path=directory+'/closed.json';
 writeFileSync(path,JSON.stringify(closed)+'\n',{mode:0o600});
 const maintenance={planPath:path,trafficPlanPath:directory+'/traffic.json',trafficEvidencePath:directory+'/traffic.jsonl',
  proof:{authorityTransition:telegramPolicyTransition,planSHA256:fingerprint(closed),targetSHA256:fingerprint(f.api),
   deploymentUID:f.api.resources[0].uid,serviceUID:f.api.resources[1].uid,trafficPlanSHA256:'c'.repeat(64)}};
 const transition={profile:telegramPolicyTransition,targetProfile:f.authority,targetSHA256:fingerprint(f.authority)};
 assert.doesNotThrow(()=>verifyCatalogAdmission(maintenance,transition));
 assert.throws(()=>verifyCatalogAdmission(maintenance,{...transition,profile:catalogPolicyTransition}),/ADMISSION_PROOF_REQUIRED/);
 writeFileSync(path,JSON.stringify({...closed,synthetic:'rewritten'})+'\n');
 assert.throws(()=>verifyCatalogAdmission(maintenance,transition),/ADMISSION_PROOF_REQUIRED/);
});
