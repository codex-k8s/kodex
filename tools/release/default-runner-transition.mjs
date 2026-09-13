#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { closeSync, fsyncSync, lstatSync, openSync, readFileSync, writeFileSync, writeSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";

const namespace = "kodex-system";
const annotation = "kodex.dev/default-runner-transition";
const stabilizedAnnotation = "kodex.dev/default-runner-transition-stabilized";
const referencePattern = /^pull\.kodex\.works\/kodex\/agent-runner@sha256:[a-f0-9]{64}$/;
const uuidPattern = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const requireValue = (condition, code) => { if (!condition) throw new Error(code); };

function application(deployment) {
  const metadata = deployment?.metadata, spec = deployment?.spec;
  requireValue(deployment?.apiVersion === "apps/v1" && deployment.kind === "Deployment" &&
    ["control-plane", "runtime-controller"].includes(metadata?.name) && metadata.namespace === namespace &&
    uuidPattern.test(metadata.uid ?? "") && typeof metadata.resourceVersion === "string" &&
    metadata.labels?.["app.kubernetes.io/part-of"] === "kodex" &&
    (metadata.labels?.["kodex.dev/local-profile"] === "hot-reload" || metadata.labels?.["kodex.dev/environment"] === "staging"),
  "STAGING_DEPLOYMENT_REQUIRED");
  requireValue(spec.strategy?.type === "RollingUpdate" && spec.strategy.rollingUpdate?.maxUnavailable === 0 &&
    Number.isSafeInteger(spec.replicas) && spec.replicas > 0 && deployment.status?.observedGeneration >= metadata.generation &&
    deployment.status.availableReplicas === spec.replicas && deployment.status.updatedReplicas === spec.replicas &&
    deployment.status.replicas === spec.replicas, "HEALTHY_ROLLOUT_REQUIRED");
  const containers = spec.template.spec.containers.filter((item) => item.name === metadata.name);
  requireValue(containers.length === 1, "APPLICATION_CONTAINER_REQUIRED");
  return containers[0];
}

function literal(container, name) {
  const entries = (container.env ?? []).filter((item) => item.name === name);
  requireValue(entries.length === 1 && typeof entries[0].value === "string" && !entries[0].valueFrom,
    "EXACT_LITERAL_CONFIG_REQUIRED");
  return entries[0];
}

export function planDefaultRunner(deployments, nextReference, operationID = randomUUID()) {
  requireValue(referencePattern.test(nextReference) && uuidPattern.test(operationID) && deployments.length === 2,
    "DEFAULT_RUNNER_INPUT_INVALID");
  const byName = new Map(deployments.map((item) => [item.metadata?.name, item]));
  requireValue(byName.size === 2, "DEFAULT_RUNNER_TARGETS_INVALID");
  const digest = nextReference.slice(nextReference.indexOf("@") + 1);
  const configs = [["runtime-controller", "RUNTIME_CONTROLLER_DEFAULT_ROLE_IMAGE_REFERENCE"], ["control-plane", "CONTROL_PLANE_DEFAULT_ROLE_IMAGE_REFERENCE"]];
  const operations = configs.map(([name, key]) => {
    const deployment = byName.get(name), container = application(deployment), current = literal(container, key).value;
    requireValue(referencePattern.test(current), "CURRENT_DEFAULT_RUNNER_INVALID");
    if (name === "control-plane") requireValue(literal(container, "CONTROL_PLANE_TRUSTED_ROLE_BASE_DIGEST").value === digest, "RUNNER_NOT_TRUSTED_BY_CONTROL_PLANE");
    const after = structuredClone(deployment.spec), nextContainer = after.template.spec.containers.find((item) => item.name === name);
    literal(nextContainer, key).value = nextReference;
    after.template.metadata.annotations = { ...(after.template.metadata.annotations ?? {}), [annotation]: operationID };
    let stabilizedAfter, stabilizedAfterSpecSHA256;
    if (name === "control-plane") {
      stabilizedAfter = structuredClone(after);
      stabilizedAfter.template.metadata.annotations[stabilizedAnnotation] = operationID;
      stabilizedAfterSpecSHA256 = fingerprint(stabilizedAfter);
    }
    return { name, key, uid: deployment.metadata.uid, resourceVersion: deployment.metadata.resourceVersion,
      beforeReference: current, afterReference: nextReference, beforeSpecSHA256: fingerprint(deployment.spec), afterSpecSHA256: fingerprint(after), changed: current !== nextReference,
      patch: [{op:"test",path:"/metadata/uid",value:deployment.metadata.uid},{op:"test",path:"/metadata/resourceVersion",value:deployment.metadata.resourceVersion},{op:"replace",path:"/spec",value:after}],
      ...(stabilizedAfter ? { stabilizedAfter, stabilizedAfterSpecSHA256 } : {}) };
  });
  requireValue(operations[0].beforeReference === operations[1].beforeReference && operations.some((item) => item.changed), "DEFAULT_RUNNER_PREDECESSOR_MISMATCH");
  return { version:1, kind:"DEFAULT_RUNNER_TRANSITION", id:operationID, nextReference, operations };
}

export function requirePublishedNodeReadback(policy, nextReference) {
  requireValue(policy?.apiVersion === "v1" && policy.kind === "ConfigMap" && policy.metadata?.namespace === namespace &&
    policy.immutable === true && policy.metadata.labels?.["app.kubernetes.io/part-of"] === "kodex" &&
    policy.metadata.labels?.["kodex.dev/owner-intent"] === "true" && policy.data?.nodeReadbackImage === nextReference,
  "RUNNER_NODE_READBACK_NOT_PUBLISHED");
}

export function requireRuntimeBinding(policy, binding, nextReference) {
  requireValue(policy?.data?.nodeReadbackImage === nextReference &&
    binding?.apiVersion === "admissionregistration.k8s.io/v1" && binding.kind === "ValidatingAdmissionPolicyBinding" &&
    binding.metadata?.name === "runtime-role-pod-exact-secret-projection" &&
    binding.spec?.policyName === binding.metadata.name && binding.spec?.paramRef?.namespace === namespace &&
    binding.spec?.paramRef?.name === policy.metadata?.name && binding.spec?.paramRef?.parameterNotFoundAction === "Deny" &&
    fingerprint(binding.spec?.validationActions) === fingerprint(["Deny"]), "RUNTIME_BINDING_NOT_TRANSITIONED");
}

function privateJSON(path) { const stat=lstatSync(path);requireValue(stat.isFile()&&stat.nlink===1&&(stat.mode&0o077)===0&&stat.size<8<<20,"PRIVATE_INPUT_REQUIRED");return JSON.parse(readFileSync(path,"utf8")); }

function main(args) {
  const command=args.shift(),options={};requireValue(["plan","apply","inspect"].includes(command),"INVALID_COMMAND");
  while(args.length){const key=args.shift();requireValue(/^--[a-z-]+$/.test(key??"")&&args.length&&!Object.hasOwn(options,key),"INVALID_ARGUMENTS");options[key]=args.shift();}
  const context=options["--context"];requireValue(/^[A-Za-z0-9_.:@/-]{1,160}$/.test(context??"")&&!/prod/i.test(context),"STAGING_CONTEXT_REQUIRED");
  const kube=(argv,input)=>execFileSync("kubectl",["--context",context,"--request-timeout=30s",...argv],{input,encoding:"utf8",timeout:335000,maxBuffer:8<<20,stdio:[input?"pipe":"ignore","pipe","pipe"]});
  const get=(name)=>JSON.parse(kube(["-n",namespace,"get","deployment",name,"-o","json"]));
  const admissionController=get("image-admission-controller"), admissionApp=admissionController.spec.template.spec.containers.filter((item)=>item.name==="image-admission-controller");
  const policyEntries=admissionApp[0]?.env?.filter((item)=>item.name==="IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP")??[];
  requireValue(admissionApp.length===1&&policyEntries.length===1&&typeof policyEntries[0].value==="string"&&!policyEntries[0].valueFrom,"ACTIVE_RUNNER_POLICY_REQUIRED");
  const activePolicy=JSON.parse(kube(["-n",namespace,"get","configmap",policyEntries[0].value,"-o","json"]));
  const runtimeBinding=JSON.parse(kube(["get","validatingadmissionpolicybinding","runtime-role-pod-exact-secret-projection","-o","json"]));
  const clusterUID=JSON.parse(kube(["get","namespace","kube-system","-o","json"])).metadata.uid, deployments=()=>[get("control-plane"),get("runtime-controller")];
  if(command==="plan"){
    requireValue(options["--runner-reference"]&&options["--output"]&&Object.keys(options).length===3,"PLAN_ARGUMENTS_INVALID");
    requirePublishedNodeReadback(activePolicy,options["--runner-reference"]);
    requireRuntimeBinding(activePolicy,runtimeBinding,options["--runner-reference"]);
    const plan={...planDefaultRunner(deployments(),options["--runner-reference"]),context,clusterUID};writeFileSync(options["--output"],`${JSON.stringify(plan)}\n`,{flag:"wx",mode:0o600});process.stdout.write(`${JSON.stringify({status:"PLANNED",id:plan.id,targets:plan.operations.length})}\n`);return;
  }
  requireValue(options["--plan"]&&options["--evidence"]&&(command!=="apply"||options["--confirm"]==="APPLY-STAGING-DEFAULT-RUNNER"),"OPERATION_ARGUMENTS_INVALID");
  const saved=privateJSON(options["--plan"]);requireValue(saved.context===context&&saved.clusterUID===clusterUID,"PLAN_SCOPE_CHANGED");
  requirePublishedNodeReadback(activePolicy,saved.nextReference);
  requireRuntimeBinding(activePolicy,runtimeBinding,saved.nextReference);
  if(command==="inspect"){
    requireValue(!options["--confirm"]&&Object.keys(options).length===3,"INSPECT_ARGUMENTS_INVALID");
    const states=deployments().map((deployment)=>{const expected=saved.operations.find((item)=>item.name===deployment.metadata.name),current=literal(application(deployment),expected.key).value;return{name:expected.name,status:current===expected.afterReference?"NEW":current===expected.beforeReference?"OLD":"DRIFT"};});
    writeFileSync(options["--evidence"],`${JSON.stringify({at:new Date().toISOString(),status:"INSPECTED",states})}\n`,{flag:"wx",mode:0o600});process.stdout.write(`${JSON.stringify({status:"INSPECTED",states})}\n`);return;
  }
  requireValue(Object.keys(options).length===4,"APPLY_ARGUMENTS_INVALID");const current=planDefaultRunner(deployments(),saved.nextReference,saved.id);requireValue(fingerprint(current.operations)===fingerprint(saved.operations),"PLAN_PRECONDITION_CHANGED");
  const fd=openSync(options["--evidence"],"wx",0o600),journal=(value)=>{writeSync(fd,`${JSON.stringify({at:new Date().toISOString(),id:saved.id,...value})}\n`);fsyncSync(fd);};
  try{for(const operation of saved.operations){journal({status:"INTENT",target:operation.name});kube(["-n",namespace,"patch","deployment",operation.name,"--type=json","-p",JSON.stringify(operation.patch)]);kube(["-n",namespace,"rollout","status",`deployment/${operation.name}`,"--timeout=300s"]);let after=get(operation.name);requireValue(after.metadata.uid===operation.uid&&fingerprint(after.spec)===operation.afterSpecSHA256,"TARGET_READBACK_MISMATCH");journal({status:"APPLIED",target:operation.name});if(operation.name==="control-plane"){journal({status:"STABILIZATION_INTENT",target:operation.name});const stabilizationPatch=[{op:"test",path:"/metadata/uid",value:operation.uid},{op:"test",path:"/metadata/resourceVersion",value:after.metadata.resourceVersion},{op:"replace",path:"/spec",value:operation.stabilizedAfter}];kube(["-n",namespace,"patch","deployment",operation.name,"--type=json","-p",JSON.stringify(stabilizationPatch)]);kube(["-n",namespace,"rollout","status",`deployment/${operation.name}`,"--timeout=300s"]);after=get(operation.name);requireValue(after.metadata.uid===operation.uid&&fingerprint(after.spec)===operation.stabilizedAfterSpecSHA256,"STABILIZATION_READBACK_MISMATCH");journal({status:"STABILIZED",target:operation.name});}}journal({status:"PASS",nextReference:saved.nextReference});process.stdout.write(`${JSON.stringify({status:"PASS",id:saved.id})}\n`);}catch{journal({status:"UNKNOWN",code:"DEFAULT_RUNNER_TRANSITION_FAILED"});throw new Error("DEFAULT_RUNNER_TRANSITION_FAILED");}finally{closeSync(fd);}
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){try{main(process.argv.slice(2));}catch(error){process.stderr.write(`${/^[A-Z_]+$/.test(error.message)?error.message:"DEFAULT_RUNNER_TRANSITION_FAILED"}\n`);process.exitCode=1;}}
