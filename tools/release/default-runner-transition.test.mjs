import test from "node:test";
import assert from "node:assert/strict";
import { planDefaultRunner as createDefaultRunnerPlan, requirePublishedNodeReadback, requireRuntimeBinding } from "./default-runner-transition.mjs";
function publicPolicy(host="pull.kodex.works",digest=`sha256:${"b".repeat(64)}`){return{apiVersion:"v1",kind:"ConfigMap",metadata:{name:"policy-next",namespace:"kodex-system",labels:{"app.kubernetes.io/part-of":"kodex","kodex.dev/owner-intent":"true"}},immutable:true,data:{pullRegistryHost:host,promotedPullRepository:`${host}/kodex/roles`,pullCredentialGeneration:"1",trustedRoleBaseDigest:digest,nodeReadbackImage:`${host}/kodex/agent-runner@${digest}`}};}
const planDefaultRunner=(values,reference,id,policy=publicPolicy())=>createDefaultRunnerPlan(values,reference,id,policy);

const oldRef=`pull.kodex.works/kodex/agent-runner@sha256:${"a".repeat(64)}`,nextRef=`pull.kodex.works/kodex/agent-runner@sha256:${"b".repeat(64)}`;
function deployment(name){const key=name==="control-plane"?"CONTROL_PLANE_DEFAULT_ROLE_IMAGE_REFERENCE":"RUNTIME_CONTROLLER_DEFAULT_ROLE_IMAGE_REFERENCE",env=[{name:key,value:oldRef}];if(name==="control-plane")env.push({name:"CONTROL_PLANE_TRUSTED_ROLE_BASE_DIGEST",value:`sha256:${"b".repeat(64)}`});return{apiVersion:"apps/v1",kind:"Deployment",metadata:{name,namespace:"kodex-system",uid:name==="control-plane"?"11111111-1111-4111-8111-111111111111":"22222222-2222-4222-8222-222222222222",resourceVersion:"1",generation:1,labels:{"app.kubernetes.io/part-of":"kodex","kodex.dev/local-profile":"hot-reload"}},spec:{replicas:2,strategy:{type:"RollingUpdate",rollingUpdate:{maxUnavailable:0,maxSurge:1}},template:{metadata:{annotations:{kept:"yes"}},spec:{containers:[{name,env},{name:"sidecar",env:[{name:"SECRET",valueFrom:{secretKeyRef:{name:"kept",key:"value"}}}]}]}}},status:{observedGeneration:1,availableReplicas:2,updatedReplicas:2,replicas:2}};}

test("план меняет только default runner двух приложений и сохраняет sidecar",()=>{const values=[deployment("control-plane"),deployment("runtime-controller")],before=structuredClone(values),plan=planDefaultRunner(values,nextRef,"33333333-3333-4333-8333-333333333333");assert.deepEqual(values,before);assert.deepEqual(plan.operations.map(x=>x.name),["runtime-controller","control-plane"]);for(const operation of plan.operations){const spec=operation.patch.at(-1).value;assert.equal(spec.template.spec.containers.find(x=>x.name===operation.name).env.find(x=>x.name===operation.key).value,nextRef);assert.deepEqual(spec.template.spec.containers.find(x=>x.name==="sidecar"),before.find(x=>x.metadata.name===operation.name).spec.template.spec.containers[1]);if(operation.name==="control-plane"){assert.equal(operation.stabilizedAfter.template.metadata.annotations["kodex.dev/default-runner-transition-stabilized"],plan.id);assert.notEqual(operation.stabilizedAfterSpecSHA256,operation.afterSpecSHA256);}else{assert.equal(operation.stabilizedAfter,undefined);}}});

test("план закрыто отклоняет чужой профиль, разные predecessor и недоверенный digest",()=>{for(const mutate of [v=>delete v[0].metadata.labels["kodex.dev/local-profile"],v=>v[0].status.availableReplicas=1,v=>v[1].spec.template.spec.containers[0].env[0].value=nextRef,v=>v[0].spec.template.spec.containers[0].env.find(x=>x.name==="CONTROL_PLANE_TRUSTED_ROLE_BASE_DIGEST").value=`sha256:${"c".repeat(64)}`]){const values=[deployment("control-plane"),deployment("runtime-controller")];mutate(values);assert.throws(()=>planDefaultRunner(values,nextRef,"33333333-3333-4333-8333-333333333333"));}});

test("переход требует опубликованный exact node readback runner",()=>{const policy=publicPolicy();assert.doesNotThrow(()=>requirePublishedNodeReadback(policy,nextRef));policy.data.nodeReadbackImage=oldRef;assert.throws(()=>requirePublishedNodeReadback(policy,nextRef),/NOT_PUBLISHED/);});

test("переход требует заранее переключённый закрытый runtime binding",()=>{const policy=publicPolicy(),binding={apiVersion:"admissionregistration.k8s.io/v1",kind:"ValidatingAdmissionPolicyBinding",metadata:{name:"runtime-role-pod-exact-secret-projection"},spec:{policyName:"runtime-role-pod-exact-secret-projection",validationActions:["Deny"],paramRef:{name:"policy-next",namespace:"kodex-system",parameterNotFoundAction:"Deny"}}};assert.doesNotThrow(()=>requireRuntimeBinding(policy,binding,nextRef));binding.spec.paramRef.name="policy-old";assert.throws(()=>requireRuntimeBinding(policy,binding,nextRef),/NOT_TRANSITIONED/);});

function trustedHotDeployment(name){
 const value=deployment(name);value.metadata.labels["kodex.dev/environment"]="staging";
 value.spec.template.metadata.labels={"kodex.dev/local-profile":"hot-reload","kodex.dev/security-profile":"trusted-cluster"};
 value.spec.replicas=1;value.spec.strategy={type:"Recreate"};
 value.status={observedGeneration:1,availableReplicas:1,updatedReplicas:1,replicas:1};return value;
}
test("точный trusted hot Recreate сохраняется в обоих CAS и CP stabilization без изменения стратегии",()=>{
 const values=["control-plane","runtime-controller"].map(trustedHotDeployment),before=structuredClone(values);
 const plan=planDefaultRunner(values,nextRef,"33333333-3333-4333-8333-333333333333");
 assert.deepEqual(values,before);
 for(const operation of plan.operations){
  const predecessor=before.find(value=>value.metadata.name===operation.name);
  assert.deepEqual(operation.patch.at(-1).value.strategy,predecessor.spec.strategy);
  assert.equal(operation.patch[0].value,predecessor.metadata.uid);assert.equal(operation.patch[1].value,predecessor.metadata.resourceVersion);
  if(operation.stabilizedAfter)assert.deepEqual(operation.stabilizedAfter.strategy,predecessor.spec.strategy);
 }
});
test("Recreate закрыто требует все точные trusted hot labels, одну здоровую реплику и отсутствие иных strategy keys",()=>{
 for(const mutate of [
  v=>delete v.metadata.labels["kodex.dev/local-profile"],
  v=>delete v.metadata.labels["kodex.dev/environment"],
  v=>v.metadata.labels["kodex.dev/environment"]="production",
  v=>delete v.spec.template.metadata.labels["kodex.dev/local-profile"],
  v=>v.spec.template.metadata.labels["kodex.dev/security-profile"]="protected",
  v=>{v.spec.replicas=2;v.status.availableReplicas=2;v.status.updatedReplicas=2;v.status.replicas=2;},
  v=>v.spec.strategy.rollingUpdate={},
  v=>v.spec.strategy.extra="unknown",
  v=>v.spec.strategy.type="UNKNOWN",
  v=>v.spec.strategy=undefined,
  v=>v.status.observedGeneration=0,
  v=>v.status.availableReplicas=0,
  v=>v.status.updatedReplicas=0,
  v=>v.status.replicas=2,
 ]){
  const values=["control-plane","runtime-controller"].map(trustedHotDeployment);mutate(values[0]);
  assert.throws(()=>planDefaultRunner(values,nextRef,"33333333-3333-4333-8333-333333333333"));
 }
});
test("обычный staging сохраняет RollingUpdate/maxUnavailable0 и не получает Recreate исключение",()=>{
 const values=["control-plane","runtime-controller"].map(name=>{
  const value=deployment(name);delete value.metadata.labels["kodex.dev/local-profile"];value.metadata.labels["kodex.dev/environment"]="staging";return value;
 });
 const plan=planDefaultRunner(values,nextRef,"33333333-3333-4333-8333-333333333333");
 for(const operation of plan.operations)assert.deepEqual(operation.patch.at(-1).value.strategy,values.find(value=>value.metadata.name===operation.name).spec.strategy);
 for(const strategy of [{type:"Recreate"},{type:"RollingUpdate",rollingUpdate:{maxUnavailable:1,maxSurge:1}}]){
  const changed=structuredClone(values);changed[0].spec.strategy=strategy;
  assert.throws(()=>planDefaultRunner(changed,nextRef,"33333333-3333-4333-8333-333333333333"),/HEALTHY_ROLLOUT_REQUIRED/);
 }
});
test("actual public policy repository связывает old/new defaults; unknown alias и missing policy закрыто отклоняются",()=>{
 const host="pull.kodex.127.0.0.2.nip.io",policy=publicPolicy(host),values=["control-plane","runtime-controller"].map(trustedHotDeployment);
 for(const value of values)value.spec.template.spec.containers[0].env[0].value=`${host}/kodex/agent-runner@sha256:${"a".repeat(64)}`;
 const plan=planDefaultRunner(values,policy.data.nodeReadbackImage,"33333333-3333-4333-8333-333333333333",policy);
 assert.ok(plan.operations.every(op=>op.afterReference===policy.data.nodeReadbackImage));
 const changed=structuredClone(values);changed[0].spec.template.spec.containers[0].env[0].value=oldRef;
 assert.throws(()=>planDefaultRunner(changed,policy.data.nodeReadbackImage,"33333333-3333-4333-8333-333333333333",policy),/CURRENT_DEFAULT_RUNNER_INVALID/);
 assert.throws(()=>planDefaultRunner(values,nextRef,"33333333-3333-4333-8333-333333333333",policy),/NOT_PUBLISHED/);
 assert.throws(()=>createDefaultRunnerPlan(values,policy.data.nodeReadbackImage,"33333333-3333-4333-8333-333333333333"),/NOT_PUBLISHED/);
});
