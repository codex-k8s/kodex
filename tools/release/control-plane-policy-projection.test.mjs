import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { planPolicyProjection,projectionPatch,projectionMatches } from "./control-plane-policy-projection.mjs";
import { fingerprint } from "./scoped-release.mjs";

function fixture(){
  const deployment={apiVersion:"apps/v1",kind:"Deployment",metadata:{name:"control-plane",namespace:"kodex-system",uid:"00000000-0000-4000-8000-000000000001",resourceVersion:"12",generation:4,labels:{"app.kubernetes.io/part-of":"kodex","kodex.dev/environment":"staging"}},spec:{replicas:2,strategy:{type:"RollingUpdate",rollingUpdate:{maxUnavailable:0,maxSurge:1}},template:{spec:{volumes:[{name:"unchanged",secret:{secretName:"reference-only"}}],containers:[{name:"control-plane",image:"unchanged",env:[{name:"CONTROL_PLANE_OIDC_ISSUER",valueFrom:{configMapKeyRef:{name:"cp-config",key:"issuer"}}},{name:"UNRELATED",valueFrom:{secretKeyRef:{name:"reference-only",key:"key"}}}],volumeMounts:[{name:"unchanged",mountPath:"/unrelated",readOnly:true}]},{name:"verifier",image:"unchanged",env:[{name:"UNCHANGED",value:"sentinel"}]}]}}},status:{observedGeneration:4,availableReplicas:2,updatedReplicas:1,replicas:3}};
  const registry={kind:"ConfigMap",metadata:{name:"internal-rpc-authority-publisher-target-registry",namespace:"kodex-system",uid:"registry-uid",resourceVersion:"8"},data:{"authority-policy.json":readFileSync(new URL("../../deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json",import.meta.url),"utf8"),"key-delivery-targets.yaml":"unchanged-registry"}};
  const configMaps=[{kind:"ConfigMap",metadata:{name:"cp-config",namespace:"kodex-system",uid:"config-uid",resourceVersion:"5"},data:{issuer:"https://sso.fixture.test/realms/kodex"}}];
  return {deployment,registry,configMaps};
}
test("projection preserves source policy and sidecars during incomplete rollout",()=>{
  const f=fixture(),before=structuredClone(f);
  const plan=planPolicyProjection(f.deployment,f.registry,f.configMaps);
  assert.deepEqual(f,before);
  assert.equal(plan.projection.immutable,true);
  assert.equal(projectionMatches(plan.projection,plan.projection),true);
  const patch=projectionPatch(f.deployment,plan);
  assert.deepEqual(patch.slice(0,2).map(p=>p.path),["/metadata/uid","/metadata/resourceVersion"]);
  assert.deepEqual(patch.slice(2).map(p=>p.path),["/spec/template/spec/volumes","/spec/template/spec/containers/0/env","/spec/template/spec/containers/0/volumeMounts"]);
  const changed=structuredClone(f.deployment);
  for(const item of patch.slice(2)){const parts=item.path.slice(1).split('/');let parent=changed;for(const key of parts.slice(0,-1))parent=parent[key];parent[parts.at(-1)]=item.value;}
  assert.equal(fingerprint(changed.spec),plan.afterSpecSHA256);
  assert.deepEqual(changed.spec.template.spec.containers[1],before.deployment.spec.template.spec.containers[1]);
  assert.deepEqual(changed.spec.template.spec.containers[0].env[1],before.deployment.spec.template.spec.containers[0].env[1]);
  const original=JSON.parse(f.registry.data["authority-policy.json"]),projected=JSON.parse(plan.projection.data["policy.json"]);
  assert.deepEqual(projected.policy.operation_bindings,original.policy.operation_bindings);
  assert.equal(projected.policy_revision,original.policy_revision);
});
test("unsafe rollout, foreign profile and source drift reject before mutation",()=>{
  for(const mutate of [f=>{f.deployment.status.availableReplicas=1;},f=>{f.deployment.spec.strategy.rollingUpdate.maxUnavailable=1;},f=>{f.deployment.metadata.labels['kodex.dev/environment']='production';},f=>{f.registry.data['authority-policy.json']='{}';},f=>{f.deployment.spec.template.spec.containers[0].env.push({name:'CONTROL_PLANE_AUTHORITY_POLICY_FILE',value:'/foreign'});}]){
    const f=fixture();mutate(f);assert.throws(()=>planPolicyProjection(f.deployment,f.registry,f.configMaps));
  }
  const f=fixture(),plan=planPolicyProjection(f.deployment,f.registry,f.configMaps);f.deployment.metadata.resourceVersion='13';assert.throws(()=>projectionPatch(f.deployment,plan),/DRIFT/);
});
test("existing config map content cannot be silently replaced",()=>{
  const f=fixture(),plan=planPolicyProjection(f.deployment,f.registry,f.configMaps);
  const foreign=structuredClone(plan.projection);foreign.data['policy.json']='changed';
  assert.equal(projectionMatches(foreign,plan.projection),false);
});
