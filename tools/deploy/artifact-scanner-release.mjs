#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { open, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { fingerprint } from "../release/stable-inventory.mjs";
import { inspectSource } from "../release/application-source.mjs";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";

const namespace = "kodex-system";
const annotation = "kodex.dev/artifact-scanner-configuration";
const ensure = (ok, code) => { if (!ok) throw Error(code); };
const equal = (a,b) => fingerprint(a) === fingerprint(b);

// Только CP-owned scanner configuration/tmp. Бинарь, source, identities, сеть,
// Secrets, PVC, replicas и стратегия приложения остаются прежними.
export function scannerReleasePlan(deployment, previous, data) {
  const meta = deployment?.metadata, spec = deployment?.spec;
  ensure(deployment?.kind === "Deployment" && meta?.name === "control-plane" && meta.namespace === namespace && meta.uid && meta.resourceVersion &&
    meta.labels?.["app.kubernetes.io/part-of"] === "kodex" &&
    spec?.template?.metadata?.labels?.["kodex.dev/security-profile"] === "trusted-cluster", "SCANNER_WORKLOAD_SCOPE_INVALID");
  ensure(spec.replicas === 2 && spec.strategy?.type === "RollingUpdate" && spec.strategy.rollingUpdate?.maxUnavailable === 0 &&
    spec.strategy.rollingUpdate.maxSurge === 1 && deployment.status?.readyReplicas === 2 && deployment.status.updatedReplicas === 2 &&
    deployment.status.replicas === 2 && deployment.status.observedGeneration >= meta.generation, "SCANNER_SAFE_ROLLOUT_REQUIRED");
  const volumes = spec.template.spec.volumes, config = volumes?.find(volume => volume.name === "skill-scanner-config");
  const temporary = volumes?.find(volume => volume.name === "skill-scanner-tmp");
  const scanner = spec.template.spec.containers?.find(container => container.name === "skill-scanner");
  ensure(config?.configMap?.name && previous?.kind === "ConfigMap" && previous.metadata?.namespace === namespace &&
    previous.metadata.name === config.configMap.name && previous.metadata.uid &&
    previous.metadata.labels?.["app.kubernetes.io/part-of"] === "kodex" &&
    scanner?.volumeMounts?.some(mount => mount.name === config.name && mount.mountPath === "/etc/kodex-skill-scanner" && mount.readOnly === true) &&
    equal(scanner.command,["/usr/sbin/clamd"]) && equal(scanner.args,["--config-file=/etc/kodex-skill-scanner/clamd.conf"]) &&
    temporary?.emptyDir && ["128Mi","2Gi"].includes(temporary.emptyDir.sizeLimit), "SCANNER_CONFIGURATION_BOUNDARY_INVALID");
  ensure(data && Object.keys(data).sort().join() === "clamd.conf,healthcheck.sh" &&
    Object.values(data).every(value => typeof value === "string" && value.length > 0 && value.length < 32768), "SCANNER_DATA_INVALID");
  const digest = fingerprint(data);
  const configMap = {apiVersion:"v1",kind:"ConfigMap",metadata:{name:`control-plane-antivirus-${digest.slice(0,16)}`,namespace,
    labels:{"app.kubernetes.io/part-of":"kodex","kodex.dev/artifact-antivirus-issue":"1814"}},immutable:true,data};
  const desired = structuredClone(spec);
  desired.template.spec.volumes.find(volume => volume.name === config.name).configMap.name = configMap.metadata.name;
  desired.template.spec.volumes.find(volume => volume.name === temporary.name).emptyDir.sizeLimit = "2Gi";
  desired.template.metadata.annotations = {...(desired.template.metadata.annotations ?? {}),[annotation]:digest};
  return {version:1,kind:"ARTIFACT_SCANNER_RELEASE",deploymentUID:meta.uid,resourceVersion:meta.resourceVersion,
    previous:{name:previous.metadata.name,uid:previous.metadata.uid,dataSHA256:fingerprint(previous.data)},
    beforeSpec:spec,afterSpec:desired,configMap};
}

export function scannerReleaseReadback(deployment, configMap, plan) {
  ensure(deployment?.metadata?.uid === plan.deploymentUID && equal(deployment.spec,plan.afterSpec), "SCANNER_DEPLOYMENT_DRIFT");
  ensure(configMap?.metadata?.name === plan.configMap.metadata.name && configMap.metadata.namespace === namespace &&
    configMap.immutable === true && equal(configMap.data,plan.configMap.data) &&
    configMap.metadata.labels?.["kodex.dev/artifact-antivirus-issue"] === "1814", "SCANNER_CONFIGMAP_DRIFT");
  const status = deployment.status;
  return {status:status?.observedGeneration >= deployment.metadata.generation && status.readyReplicas === 2 &&
    status.updatedReplicas === 2 && status.replicas === 2 ? "PASS" : "RUNNING",deploymentUID:plan.deploymentUID,
    configurationSHA256:fingerprint(configMap.data),configMapUID:configMap.metadata.uid};
}

async function main(args) {
  const phase=args.shift(), options={};
  while(args.length) {const key=args.shift();ensure(["--context","--plan","--approved-plan-sha"].includes(key) && args.length && !Object.hasOwn(options,key),"SCANNER_ARGUMENT_INVALID");options[key]=args.shift();}
  ensure(["plan","apply","readback"].includes(phase) && options["--plan"] && options["--context"] === "k3d-kodex" &&
    (phase === "plan" ? !options["--approved-plan-sha"] : /^[a-f0-9]{64}$/.test(options["--approved-plan-sha"] ?? "")),"SCANNER_LOCAL_CONTEXT_AND_PLAN_REQUIRED");
  const context=options["--context"];
  const kube=(args,input)=>{try{const argv=["--context",context,"--request-timeout=30s","-n",namespace,...args];
    return execFileSync(input === undefined ? "kubectl" : "/bin/sh",input === undefined ? argv : ["-c",'cat | kubectl "$@"',"kodex-scanner-pipe",...argv],
    {encoding:"utf8",input,timeout:35000,maxBuffer:4<<20,stdio:["pipe","pipe","pipe"]});}catch{throw Error("SCANNER_KUBERNETES_OPERATION_UNKNOWN");}};
  const get=(kind,name)=>JSON.parse(kube(["get",kind,name,"-o","json"]));
  const clusterUID=JSON.parse(kube(["get","namespace","kube-system","-o","json"])).metadata.uid;
  const sourceRoot=fileURLToPath(new URL("../../",import.meta.url)).replace(/\/$/,"");
  const source={path:sourceRoot,...inspectSource(sourceRoot)};
  const data=Object.fromEntries(await Promise.all(["clamd.conf","healthcheck.sh"].map(async name=>[name,
    await readFile(new URL(`../../deploy/k8s/base/control-plane/skill-scanner/${name}`,import.meta.url),"utf8")])));
  const deployment=get("deployment","control-plane");
  if(phase === "plan") {
    const old=get("configmap",deployment.spec.template.spec.volumes.find(volume=>volume.name === "skill-scanner-config").configMap.name);
    const plan={...scannerReleasePlan(deployment,old,data),source,clusterUID,context};
    const file=await open(options["--plan"],"wx",0o600);
    try{await file.writeFile(`${JSON.stringify(plan)}\n`);await file.sync();}finally{await file.close();}
    process.stdout.write(`${JSON.stringify({status:"PLANNED",planSHA256:fingerprint(plan),sourceRevision:source.revision})}\n`);return;
  }
  const plan=await readPrivateJSON(options["--plan"]);
  ensure(plan.kind === "ARTIFACT_SCANNER_RELEASE" && plan.version === 1 && plan.context === context && plan.clusterUID === clusterUID &&
    equal(plan.source,source) && equal(plan.configMap.data,data) && fingerprint(plan) === options["--approved-plan-sha"],"SCANNER_PLAN_CHANGED");
  if(phase === "apply" && !equal(deployment.spec,plan.afterSpec)) {
    const old=get("configmap",plan.previous.name);
    ensure(old.metadata.uid === plan.previous.uid && fingerprint(old.data) === plan.previous.dataSHA256 &&
      equal({...scannerReleasePlan(deployment,old,data),source,clusterUID,context},plan),"SCANNER_PREDECESSOR_CHANGED");
    const existingRaw=kube(["get","configmap",plan.configMap.metadata.name,"--ignore-not-found=true","-o","json"]);
    if(existingRaw.trim()) scannerReleaseReadback({...deployment,spec:plan.afterSpec},JSON.parse(existingRaw),plan);
    else kube(["create","-f","-"],JSON.stringify(plan.configMap));
    const patch=[{op:"test",path:"/metadata/uid",value:plan.deploymentUID},{op:"test",path:"/metadata/resourceVersion",value:plan.resourceVersion},
      {op:"test",path:"/spec",value:plan.beforeSpec},{op:"replace",path:"/spec",value:plan.afterSpec}];
    // После неизвестного ACK повтор читает прежний exact UID/spec; новый plan
    // не создаётся и второй rollout при совпавшем after не выполняется.
    kube(["patch","deployment","control-plane","--type=json","--patch-file=/dev/stdin"],JSON.stringify(patch));
  }
  const result=scannerReleaseReadback(get("deployment","control-plane"),get("configmap",plan.configMap.metadata.name),plan);
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if(process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url)
  main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Artifact scanner release failed: ${/^[A-Z_]+$/.test(error.message)?error.message:"SCANNER_RELEASE_FAILED"}\n`);process.exitCode=1;});
