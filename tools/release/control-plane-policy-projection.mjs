#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";
import { inspectSource } from "./application-source.mjs";
import { validateRotationPolicy } from "./authority-rotation-transition.mjs";
import { readAuthorityPolicyEnvironment, renderAuthorityPolicyEnvironment, verifyAuthorityPolicyEnvironmentReadback } from "./authority-policy-environment.mjs";

const namespace="kodex-system";
const policyEnv="CONTROL_PLANE_AUTHORITY_POLICY_FILE";
const oldPath="/var/run/config/kodex/control-plane/authority/policy.json";
const mountPath="/var/run/config/kodex/control-plane/environment-policy";
const volumeName="control-plane-environment-policy";
function requireValue(ok,code){if(!ok)throw new Error(code);}

// Исправляется только input приложения CP. Publisher registry/snapshot/key
// history не записываются: исходный подписанный материал остаётся неизменным.
export function planPolicyProjection(deployment,registry,configMaps){
  const environment=readAuthorityPolicyEnvironment(deployment,configMaps);
  requireValue(registry?.kind==="ConfigMap"&&registry.metadata?.name==="internal-rpc-authority-publisher-target-registry"&&registry.metadata.namespace===namespace&&registry.metadata.uid&&registry.metadata.resourceVersion,"POLICY_REGISTRY_IDENTITY_REJECTED");
  const raw=registry.data?.["authority-policy.json"];
  validateRotationPolicy(raw);
  const rendered=renderAuthorityPolicyEnvironment(raw,environment);
  const spec=deployment.spec;
  requireValue(spec.paused!==true&&Number.isSafeInteger(spec.replicas)&&spec.replicas>0&&spec.strategy?.type==="RollingUpdate"&&spec.strategy.rollingUpdate?.maxUnavailable===0&&Number.isSafeInteger(spec.strategy.rollingUpdate.maxSurge)&&spec.strategy.rollingUpdate.maxSurge>0&&deployment.status?.observedGeneration>=deployment.metadata.generation&&deployment.status.availableReplicas>=spec.replicas&&deployment.status.replicas<=spec.replicas+spec.strategy.rollingUpdate.maxSurge,"POLICY_PROJECTION_SAFE_ROLLOUT_REQUIRED");
  const index=spec.template.spec.containers.findIndex(c=>c.name==="control-plane");
  const application=spec.template.spec.containers[index];
  const env=application.env??[];
  const policyFields=env.filter(e=>e.name===policyEnv);
  requireValue(policyFields.length<=1&&(!policyFields.length||(policyFields[0].value===oldPath&&!policyFields[0].valueFrom)),"POLICY_PROJECTION_PREDECESSOR_REJECTED");
  requireValue(!(spec.template.spec.volumes??[]).some(v=>v.name===volumeName)&&!(application.volumeMounts??[]).some(v=>v.name===volumeName||v.mountPath===mountPath),"POLICY_PROJECTION_ALREADY_CONFIGURED");
  const name=`control-plane-policy-projection-${rendered.renderedSHA256.slice(0,16)}`;
  const projection={apiVersion:"v1",kind:"ConfigMap",metadata:{name,namespace,labels:{"app.kubernetes.io/part-of":"kodex","kodex.dev/policy-projection-issue":"1476"}},immutable:true,data:{"policy.json":rendered.raw}};
  const desired=structuredClone(spec);
  desired.template.spec.volumes=[...(desired.template.spec.volumes??[]),{name:volumeName,configMap:{name,defaultMode:292}}];
  desired.template.spec.containers[index].env=[...env.filter(e=>e.name!==policyEnv),{name:policyEnv,value:`${mountPath}/policy.json`}];
  desired.template.spec.containers[index].volumeMounts=[...(application.volumeMounts??[]),{name:volumeName,mountPath,readOnly:true}];
  return {version:1,kind:"CP_ENVIRONMENT_POLICY_PROJECTION",environment,deploymentUID:deployment.metadata.uid,resourceVersion:deployment.metadata.resourceVersion,beforeSpecSHA256:fingerprint(spec),afterSpecSHA256:fingerprint(desired),registry:{uid:registry.metadata.uid,resourceVersion:registry.metadata.resourceVersion,dataSHA256:fingerprint(registry.data)},sourcePolicySHA256:rendered.sourceSHA256,renderedPolicySHA256:rendered.renderedSHA256,projection};
}

export function projectionPatch(deployment,plan){
  requireValue(deployment.metadata.uid===plan.deploymentUID&&deployment.metadata.resourceVersion===plan.resourceVersion&&fingerprint(deployment.spec)===plan.beforeSpecSHA256,"POLICY_PROJECTION_DEPLOYMENT_DRIFT");
  const index=deployment.spec.template.spec.containers.findIndex(c=>c.name==="control-plane");
  requireValue(index>=0,"POLICY_PROJECTION_APPLICATION_MISSING");
  const app=deployment.spec.template.spec.containers[index];
  return [{op:"test",path:"/metadata/uid",value:plan.deploymentUID},{op:"test",path:"/metadata/resourceVersion",value:plan.resourceVersion},
    {op:"add",path:"/spec/template/spec/volumes",value:[...(deployment.spec.template.spec.volumes??[]),{name:volumeName,configMap:{name:plan.projection.metadata.name,defaultMode:292}}]},
    {op:"add",path:`/spec/template/spec/containers/${index}/env`,value:[...(app.env??[]).filter(e=>e.name!==policyEnv),{name:policyEnv,value:`${mountPath}/policy.json`}]},
    {op:"add",path:`/spec/template/spec/containers/${index}/volumeMounts`,value:[...(app.volumeMounts??[]),{name:volumeName,mountPath,readOnly:true}]}];
}

export function projectionMatches(actual,expected){
  return actual?.kind==="ConfigMap"&&actual.metadata?.name===expected.metadata.name&&actual.metadata.namespace===namespace&&actual.immutable===true&&fingerprint(actual.data)===fingerprint(expected.data)&&actual.metadata.labels?.["kodex.dev/policy-projection-issue"]==="1476";
}

function main(){
  const [command,...args]=process.argv.slice(2),options={};
  for(let i=0;i<args.length;i++){const key=args[i];requireValue(["--context","--output","--plan","--confirm","--k3s-sudo"].includes(key)&&options[key]===undefined,"INVALID_ARGUMENTS");options[key]=key==="--k3s-sudo"?true:args[++i];}
  requireValue(["plan","apply","observe"].includes(command)&&typeof options["--context"]==="string"&&/^[A-Za-z0-9_.:@/-]{1,160}$/.test(options["--context"])&&!/prod/i.test(options["--context"]),"STAGING_CONTEXT_REQUIRED");
  const source=resolve(fileURLToPath(new URL("../..",import.meta.url))),revision=inspectSource(source).revision;
  const kube=(args,input)=>{try{return execFileSync(options["--k3s-sudo"]?"sudo":"kubectl",[...(options["--k3s-sudo"]?["-n","k3s","kubectl"]:[]),"--context",options["--context"],"--request-timeout=30s",...args],{input,encoding:"utf8",stdio:["pipe","pipe","pipe"],timeout:35000,maxBuffer:8<<20});}catch{throw new Error("KUBERNETES_OPERATION_FAILED_READBACK_REQUIRED");}};
  const get=(kind,name)=>JSON.parse(kube(["-n",namespace,"get",kind,name,"-o","json"]));
  const ns=get("namespace",namespace);
  requireValue(ns.metadata?.uid&&ns.metadata.labels?.["kodex.dev/environment"]==="staging","STAGING_NAMESPACE_REQUIRED");
  const deployment=get("deployment","control-plane");
  const registry=get("configmap","internal-rpc-authority-publisher-target-registry");
  const names=[...new Set((deployment.spec.template.spec.containers.find(c=>c.name==="control-plane")?.env??[]).filter(e=>["CONTROL_PLANE_OIDC_ISSUER","CONTROL_PLANE_OIDC_AUDIENCE"].includes(e.name)).map(e=>e.valueFrom?.configMapKeyRef?.name).filter(Boolean))];
  const configMaps=names.map(name=>get("configmap",name));
  if(command==="plan"){
    requireValue(options["--output"]&&!options["--plan"]&&!options["--confirm"],"INVALID_PLAN_ARGUMENTS");
    const plan={...planPolicyProjection(deployment,registry,configMaps),source,revision,context:options["--context"],namespaceUID:ns.metadata.uid};
    writeFileSync(options["--output"],JSON.stringify(plan),{mode:0o600,flag:"wx"});
    process.stdout.write(JSON.stringify({status:"PLANNED",revision,policySHA256:plan.renderedPolicySHA256,target:"control-plane"})+"\n");return;
  }
  requireValue(options["--plan"],"PLAN_REQUIRED");
  const plan=JSON.parse(readFileSync(options["--plan"],"utf8"));
  requireValue(plan.version===1&&plan.kind==="CP_ENVIRONMENT_POLICY_PROJECTION"&&plan.source===source&&plan.revision===revision&&plan.context===options["--context"]&&plan.namespaceUID===ns.metadata.uid,"POLICY_PROJECTION_PLAN_IDENTITY_REJECTED");
  requireValue(registry.metadata.uid===plan.registry.uid&&registry.metadata.resourceVersion===plan.registry.resourceVersion&&fingerprint(registry.data)===plan.registry.dataSHA256,"POLICY_PROJECTION_REGISTRY_DRIFT");
  if(command==="observe"){
    const cm=get("configmap",plan.projection.metadata.name);
    requireValue(projectionMatches(cm,plan.projection)&&deployment.metadata.uid===plan.deploymentUID&&fingerprint(deployment.spec)===plan.afterSpecSHA256,"POLICY_PROJECTION_READBACK_MISMATCH");
    const currentEnvironment=readAuthorityPolicyEnvironment(deployment,configMaps);
    // resourceVersion самого Deployment изменяется после нашего patch/status;
    // его spec уже проверен выше. ConfigMap dependencies должны остаться точными.
    currentEnvironment.evidence.resourceVersion=plan.environment.evidence.resourceVersion;
    requireValue(fingerprint(currentEnvironment)===fingerprint(plan.environment),"POLICY_PROJECTION_ENVIRONMENT_READBACK_DRIFT");
    process.stdout.write(JSON.stringify({status:"APPLIED",updated:deployment.status?.updatedReplicas??0,available:deployment.status?.availableReplicas??0,desired:deployment.spec.replicas})+"\n");return;
  }
  requireValue(options["--confirm"]==="APPLY-STAGING-CP-POLICY-PROJECTION","CONFIRMATION_REQUIRED");
  verifyAuthorityPolicyEnvironmentReadback(plan.environment,deployment,configMaps);
  const regenerated=planPolicyProjection(deployment,registry,configMaps);
  requireValue(fingerprint(regenerated)===fingerprint(Object.fromEntries(Object.entries(plan).filter(([k])=>!["source","revision","context","namespaceUID"].includes(k)))),"POLICY_PROJECTION_PLAN_DRIFT");
  const existing=kube(["-n",namespace,"get","configmap",plan.projection.metadata.name,"--ignore-not-found","-o","json"]);
  if(existing.trim()) requireValue(projectionMatches(JSON.parse(existing),plan.projection),"POLICY_PROJECTION_CONFIGMAP_CONFLICT");
  else kube(["create","-f","-"],JSON.stringify(plan.projection));
  requireValue(projectionMatches(get("configmap",plan.projection.metadata.name),plan.projection),"POLICY_PROJECTION_CONFIGMAP_READBACK_FAILED");
  kube(["-n",namespace,"patch","deployment","control-plane","--type=json","--patch-file=/dev/stdin","-o","name"],JSON.stringify(projectionPatch(deployment,plan)));
  const current=get("deployment","control-plane");
  requireValue(current.metadata.uid===plan.deploymentUID&&fingerprint(current.spec)===plan.afterSpecSHA256,"POLICY_PROJECTION_OUTCOME_UNKNOWN");
  process.stdout.write(JSON.stringify({status:"APPLIED",target:"control-plane",policySHA256:plan.renderedPolicySHA256})+"\n");
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){try{main();}catch(error){process.stderr.write((/^[A-Z0-9_]+$/.test(error.message)?error.message:"POLICY_PROJECTION_FAILED_READBACK_REQUIRED")+"\n");process.exitCode=1;}}
