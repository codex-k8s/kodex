import test from "node:test";
import assert from "node:assert/strict";
import { scannerReleasePlan, scannerReleaseReadback } from "./artifact-scanner-release.mjs";

const data={"clamd.conf":"owned new config","healthcheck.sh":"owned health check"};
function fixture() {
 const config={apiVersion:"v1",kind:"ConfigMap",metadata:{name:"old-scanner",namespace:"kodex-system",uid:"config-uid",labels:{"app.kubernetes.io/part-of":"kodex"}},data:{"clamd.conf":"old"}};
 const deployment={kind:"Deployment",metadata:{name:"control-plane",namespace:"kodex-system",uid:"cp-uid",resourceVersion:"7",generation:2,labels:{"app.kubernetes.io/part-of":"kodex"}},
  status:{readyReplicas:2,updatedReplicas:2,replicas:2,observedGeneration:2},spec:{replicas:2,strategy:{type:"RollingUpdate",rollingUpdate:{maxUnavailable:0,maxSurge:1}},
   template:{metadata:{labels:{"kodex.dev/security-profile":"trusted-cluster"},annotations:{"preserve":"source"}},spec:{serviceAccountName:"control-plane",containers:[
    {name:"control-plane",image:"application",env:[{name:"SECRET_FILE",value:"reference-only"}]},
    {name:"skill-scanner",image:"scanner",command:["/usr/sbin/clamd"],args:["--config-file=/etc/kodex-skill-scanner/clamd.conf"],
     volumeMounts:[{name:"skill-scanner-config",mountPath:"/etc/kodex-skill-scanner",readOnly:true}]}],volumes:[
     {name:"skill-scanner-config",configMap:{name:config.metadata.name}}, {name:"skill-scanner-tmp",emptyDir:{sizeLimit:"128Mi"}},
     {name:"persistent",persistentVolumeClaim:{claimName:"owned-pvc"}}]}}}};
 return {config,deployment};
}

test("scanner release changes only exact scanner config and bounded tmp",()=>{
 const {deployment,config}=fixture(),before=structuredClone(deployment),plan=scannerReleasePlan(deployment,config,data);
 assert.deepEqual(deployment,before);
 const restored=structuredClone(plan.afterSpec);
 restored.template.spec.volumes[0].configMap.name=config.metadata.name;
 restored.template.spec.volumes[1].emptyDir.sizeLimit="128Mi";
 delete restored.template.metadata.annotations["kodex.dev/artifact-scanner-configuration"];
 assert.deepEqual(restored,deployment.spec);
 const actual={...deployment,spec:plan.afterSpec};
 const map={...plan.configMap,metadata:{...plan.configMap.metadata,uid:"new-config"}};
 assert.equal(scannerReleaseReadback(actual,map,plan).status,"PASS");
 actual.status={...actual.status,readyReplicas:1};assert.equal(scannerReleaseReadback(actual,map,plan).status,"RUNNING");
 map.data={...map.data,"clamd.conf":"foreign"};assert.throws(()=>scannerReleaseReadback(actual,map,plan),/CONFIGMAP_DRIFT/);
});

test("scanner release rejects foreign identities and unsafe rollout",()=>{
 for(const mutate of [
  d=>d.metadata.name="other",d=>d.metadata.namespace="other",d=>delete d.metadata.uid,
  d=>d.spec.replicas=1,d=>d.status.readyReplicas=1,d=>d.status.replicas=3,
  d=>d.spec.strategy.rollingUpdate.maxUnavailable=1,d=>d.spec.template.metadata.labels["kodex.dev/security-profile"]="other",
  d=>d.spec.template.spec.volumes[1].emptyDir.sizeLimit="32Gi",d=>d.spec.template.spec.containers[1].command=["foreign"],
  d=>d.spec.template.spec.containers[1].volumeMounts[0].readOnly=false,
 ]) {
  const {deployment,config}=fixture();mutate(deployment);
  assert.throws(()=>scannerReleasePlan(deployment,config,data));
 }
 const {deployment,config}=fixture();config.metadata.uid="";assert.throws(()=>scannerReleasePlan(deployment,config,data));
});

test("scanner readback rejects a replaced workload or changed application",()=>{
 const {deployment,config}=fixture(),plan=scannerReleasePlan(deployment,config,data);
 const actual={...deployment,spec:structuredClone(plan.afterSpec)};
 actual.spec.template.spec.containers[0].image="foreign";
 assert.throws(()=>scannerReleaseReadback(actual,plan.configMap,plan),/DEPLOYMENT_DRIFT/);
 actual.spec=plan.afterSpec;actual.metadata={...actual.metadata,uid:"replacement"};
 assert.throws(()=>scannerReleaseReadback(actual,plan.configMap,plan),/DEPLOYMENT_DRIFT/);
});
