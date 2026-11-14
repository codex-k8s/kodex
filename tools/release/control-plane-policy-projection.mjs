#!/usr/bin/env node
import {readTelegramStagedPolicyMigration,telegramStagedPolicyProtocol} from './telegram-cp-policy.mjs';
import {readTelegramAuthorityDelivered} from './authority-rotation-transition.mjs';
import {resolveTelegramExecutorSource} from './telegram-executor-source.mjs';
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";
import { inspectSource } from "./application-source.mjs";
import { validateRotationPolicy } from "./authority-rotation-transition.mjs";
import { readAuthorityPolicyEnvironment, renderAuthorityPolicyEnvironment, verifyAuthorityPolicyEnvironmentReadback } from "./authority-policy-environment.mjs";
import { telegramPolicyTransition } from './authority-telegram-transition.mjs';
import { catalogPolicyTransition, validateCatalogPolicy, isPortableAuthorityTransition, validateAuthorityPolicy, readAuthorityPrivateJSON, readCatalogTarget, catalogKubeconfig, verifyCatalogTransition, readCatalogMaintenance, verifyCatalogAdmission, openCatalogProjectionJournal, validateCatalogProjectionEvents, readAuthorityPrivateText, parseAuthorityPrivateJSON, writeAuthorityPrivatePlan } from './authority-catalog-transition.mjs';

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
  if(plan.kind==='CP_CATALOG_POLICY_PROJECTION')return [
    {op:'test',path:'/metadata/uid',value:plan.deploymentUID},
    {op:'test',path:'/metadata/resourceVersion',value:plan.resourceVersion},
    {op:'add',path:'/spec/template/spec/volumes',value:plan.desired.volumes},
    {op:'add',path:`/spec/template/spec/containers/${index}/env`,value:plan.desired.env},
    {op:'add',path:`/spec/template/spec/containers/${index}/volumeMounts`,value:plan.desired.volumeMounts},
  ];
  return [{op:"test",path:"/metadata/uid",value:plan.deploymentUID},{op:"test",path:"/metadata/resourceVersion",value:plan.resourceVersion},
    {op:"add",path:"/spec/template/spec/volumes",value:[...(deployment.spec.template.spec.volumes??[]),{name:volumeName,configMap:{name:plan.projection.metadata.name,defaultMode:292}}]},
    {op:"add",path:`/spec/template/spec/containers/${index}/env`,value:[...(app.env??[]).filter(e=>e.name!==policyEnv),{name:policyEnv,value:`${mountPath}/policy.json`}]},
    {op:"add",path:`/spec/template/spec/containers/${index}/volumeMounts`,value:[...(app.volumeMounts??[]),{name:volumeName,mountPath,readOnly:true}]}];
}

// Закрытое обновление105→106 либо106→107 сохраняет прежнюю immutable CM.
// Registry уже обслуживает exact desired policy; меняется лишь pointer CP.
export function planCatalogPolicyProjection(deployment,registry,configMaps,{transition,target,existingProjection,telegramStage}) {
  requireValue(target?.allows(deployment)&&target.allows(registry),'CATALOG_AUTHORITY_TARGET_REJECTED');
  const environment=readAuthorityPolicyEnvironment(deployment,configMaps,target);
  const candidate=structuredClone(transition);candidate.environment=environment;
  verifyCatalogTransition(transition,candidate);
  const raw=registry.data?.['authority-policy.json'];
  const policy=validateAuthorityPolicy(raw,'desired',transition);
  const spec=deployment.spec,app=spec.template.spec.containers.find(c=>c.name==='control-plane');
  const staged=telegramStage!==undefined;
  if(staged)requireValue(transition.profile===telegramPolicyTransition&&telegramStage.proof?.protocol===telegramStagedPolicyProtocol&&
    spec.paused!==true&&spec.strategy?.type==='RollingUpdate'&&spec.strategy.rollingUpdate?.maxUnavailable===0&&
    Number.isSafeInteger(spec.strategy.rollingUpdate.maxSurge)&&spec.strategy.rollingUpdate.maxSurge>0&&telegramStage.proof.deploymentUID===deployment.metadata.uid&&telegramStage.proof.stagedSpecSHA256===fingerprint(spec)&&spec.replicas===0&&
    deployment.status?.observedGeneration>=deployment.metadata.generation&&!(deployment.status.replicas??0),'TELEGRAM_CP_STAGE_JOIN_REQUIRED');
  requireValue(staged||spec.paused!==true&&Number.isSafeInteger(spec.replicas)&&spec.replicas>0&&
    spec.strategy?.type==='RollingUpdate'&&spec.strategy.rollingUpdate.maxUnavailable===0&&
    Number.isSafeInteger(spec.strategy.rollingUpdate.maxSurge)&&spec.strategy.rollingUpdate.maxSurge>0&&
    deployment.status?.observedGeneration>=deployment.metadata.generation&&deployment.status.availableReplicas>=spec.replicas&&
    deployment.status.replicas<=spec.replicas+spec.strategy.rollingUpdate.maxSurge,'POLICY_PROJECTION_SAFE_ROLLOUT_REQUIRED');
  const fields=(app.env??[]).filter(e=>e.name===policyEnv);
  const volumes=(spec.template.spec.volumes??[]).filter(v=>v.name===volumeName);
  const mounts=(app.volumeMounts??[]).filter(v=>v.name===volumeName||v.mountPath===mountPath);
  requireValue(fields.length<=1,'POLICY_PROJECTION_PREDECESSOR_REJECTED');
  let predecessor;
  if(volumes.length||mounts.length) {
    requireValue(volumes.length===1&&mounts.length===1&&fields.length===1&&
      fingerprint(mounts[0])===fingerprint({name:volumeName,mountPath,readOnly:true})&&
      fingerprint(fields[0])===fingerprint({name:policyEnv,value:`${mountPath}/policy.json`})&&
      existingProjection?.apiVersion==='v1'&&existingProjection.kind==='ConfigMap'&&existingProjection.immutable===true&&
      existingProjection.metadata?.namespace===namespace&&existingProjection.metadata.uid&&existingProjection.metadata.resourceVersion&&
      !existingProjection.metadata.deletionTimestamp&&existingProjection.metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&
      fingerprint(volumes[0])===fingerprint({name:volumeName,configMap:{name:existingProjection.metadata.name,defaultMode:292}}),
      'POLICY_PROJECTION_PREDECESSOR_REJECTED');
    const previous=validateAuthorityPolicy(existingProjection.data?.['policy.json'],'predecessor',transition);
    requireValue(Object.keys(existingProjection.data).join(',')==='policy.json'&&
      existingProjection.metadata.name===`control-plane-policy-projection-${previous.sha256.slice(0,16)}`,
      'POLICY_PROJECTION_PREDECESSOR_REJECTED');
    predecessor={name:existingProjection.metadata.name,uid:existingProjection.metadata.uid,
      resourceVersion:existingProjection.metadata.resourceVersion,dataSHA256:fingerprint(existingProjection.data)};
  } else {
    requireValue(!existingProjection&&(!fields.length||fingerprint(fields[0])===fingerprint({name:policyEnv,value:oldPath})),
      'POLICY_PROJECTION_PREDECESSOR_REJECTED');
    const legacyVolumes=(spec.template.spec.volumes??[]).filter(v=>v.name==='authority-policy');
    const legacyMounts=(app.volumeMounts??[]).filter(v=>v.name==='authority-policy');
    requireValue(legacyVolumes.length===1&&legacyMounts.length===1&&
      fingerprint(legacyVolumes[0])===fingerprint({name:'authority-policy',configMap:{name:'internal-rpc-authority-publisher-target-registry',defaultMode:288,items:[{key:'authority-policy.json',path:'policy.json'}]}})&&
      fingerprint(legacyMounts[0])===fingerprint({name:'authority-policy',mountPath:'/var/run/config/kodex/control-plane/authority',readOnly:true}),
      'POLICY_PROJECTION_PREDECESSOR_REJECTED');
  }
  const projection={apiVersion:'v1',kind:'ConfigMap',metadata:{name:`control-plane-policy-projection-${policy.sha256.slice(0,16)}`,namespace,
    labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/policy-projection-issue':transition.profile===telegramPolicyTransition?'2143':'2139'}},immutable:true,data:{'policy.json':raw}};
  const desired={
    volumes:[...(spec.template.spec.volumes??[]).filter(v=>v.name!==volumeName),{name:volumeName,configMap:{name:projection.metadata.name,defaultMode:292}}],
    env:[...(app.env??[]).filter(e=>e.name!==policyEnv),{name:policyEnv,value:`${mountPath}/policy.json`}],
    volumeMounts:[...(app.volumeMounts??[]).filter(v=>v.name!==volumeName),{name:volumeName,mountPath,readOnly:true}],
  };
  const after=structuredClone(spec),index=spec.template.spec.containers.findIndex(c=>c.name==='control-plane');
  after.template.spec.volumes=desired.volumes;after.template.spec.containers[index].env=desired.env;
  after.template.spec.containers[index].volumeMounts=desired.volumeMounts;
  return {version:2,kind:'CP_CATALOG_POLICY_PROJECTION',environment,policyTransition:transition,...(staged?{telegramStage}:{}),
    deploymentUID:deployment.metadata.uid,resourceVersion:deployment.metadata.resourceVersion,
    beforeSpecSHA256:fingerprint(spec),afterSpecSHA256:fingerprint(after),desired,predecessor,
    registry:{uid:registry.metadata.uid,resourceVersion:registry.metadata.resourceVersion,dataSHA256:fingerprint(registry.data)},
    sourcePolicySHA256:policy.sourceSHA256,renderedPolicySHA256:policy.sha256,projection};
}

export function projectionMatches(actual,expected){
  return actual?.apiVersion==="v1"&&actual.kind==="ConfigMap"&&!actual.binaryData&&actual.metadata?.labels?.["app.kubernetes.io/part-of"]==="kodex"&&actual.metadata?.name===expected.metadata.name&&actual.metadata.namespace===namespace&&actual.immutable===true&&!actual.metadata.deletionTimestamp&&fingerprint(actual.data)===fingerprint(expected.data)&&actual.metadata.labels?.["kodex.dev/policy-projection-issue"]===expected.metadata.labels?.["kodex.dev/policy-projection-issue"];
}

export function withPrivatePatchFile(patch,apply){
  const directory=mkdtempSync(join(tmpdir(),"kodex-cp-policy-patch-"));
  const path=join(directory,"patch.json");
  try{
    writeFileSync(path,JSON.stringify(patch),{flag:"wx",mode:0o600});
    return apply(path);
  } finally {
    rmSync(directory,{recursive:true,force:true});
  }
}

export function main(argv=process.argv.slice(2),io={}){
  const [command,...args]=argv,options={};
  for(let i=0;i<args.length;i++){const key=args[i];requireValue(["--context","--kubeconfig","--portable-target","--policy-transition","--maintenance-plan","--maintenance-evidence","--traffic-plan","--traffic-evidence","--evidence","--output","--plan","--confirm","--k3s-sudo","--migration-plan","--migration-evidence","--authority-plan","--authority-evidence","--authority-status-plan","--authority-status-evidence","--application-source","--application-revision","--telegram-port-repair-plan","--telegram-port-repair-executor-sha"].includes(key)&&options[key]===undefined,"INVALID_ARGUMENTS");options[key]=key==="--k3s-sudo"?true:args[++i];}
  const saved=options['--plan']?readAuthorityPrivateJSON(options['--plan']):undefined;
  const profile=options['--policy-transition']??saved?.policyTransition?.profile;
  requireValue(profile===undefined||isPortableAuthorityTransition(profile),'CATALOG_POLICY_TRANSITION_REQUIRED');
  const catalog=isPortableAuthorityTransition(profile);
  const stageArguments=['--migration-plan','--migration-evidence','--authority-plan','--authority-evidence','--authority-status-plan','--authority-status-evidence'];
  requireValue(!stageArguments.some(key=>options[key])||profile===telegramPolicyTransition,'TELEGRAM_CP_STAGE_SCOPE_REJECTED');
  for(const group of [['--migration-plan','--migration-evidence'],['--authority-plan','--authority-evidence']])
    requireValue(!group.some(key=>options[key])||command==='plan'&&group.every(key=>options[key]),'TELEGRAM_CP_STAGE_INPUTS_REQUIRED');
  const statusArguments=['--authority-status-plan','--authority-status-evidence'];
  requireValue(!statusArguments.some(key=>options[key])||['apply','resume'].includes(command)&&saved?.telegramStage&&statusArguments.every(key=>options[key]),
    'TELEGRAM_CP_STAGE_INPUTS_REQUIRED');

  requireValue(["plan","apply","observe",...(catalog?['resume']:[])].includes(command)&&typeof options["--context"]==="string"&&/^[A-Za-z0-9_.:@/-]{1,160}$/.test(options["--context"])&&(catalog||!/prod/i.test(options["--context"])),"STAGING_CONTEXT_REQUIRED");
  const kubeconfig=catalog?catalogKubeconfig(options['--kubeconfig']):options['--kubeconfig'];
  const executorSource=resolve(fileURLToPath(new URL("../..",import.meta.url)));
  const selected=options['--application-source']!==undefined||options['--application-revision']!==undefined;
  requireValue(!options['--telegram-port-repair-executor-sha']||selected&&options['--telegram-port-repair-plan']||options['--telegram-port-repair-executor-sha']===saved?.sourceCompatibility?.repairExecutor?.executorRevision,'TELEGRAM_EXECUTOR_SOURCE_REJECTED');
  requireValue(!options['--telegram-port-repair-plan']||selected||options['--telegram-port-repair-plan']===saved?.sourceCompatibility?.telegramPortRepair?.planPath,'TELEGRAM_EXECUTOR_SOURCE_REJECTED');
  requireValue(!selected||command==='plan'&&profile===telegramPolicyTransition,'TELEGRAM_EXECUTOR_SOURCE_REJECTED');
  requireValue(!saved?.sourceCompatibility||profile===telegramPolicyTransition,'TELEGRAM_EXECUTOR_SOURCE_REJECTED');
  const sourceCompatibility=selected||saved?.sourceCompatibility?resolveTelegramExecutorSource(executorSource,
    saved?.sourceCompatibility?{saved:saved.sourceCompatibility}:{source:options['--application-source'],revision:options['--application-revision'],repairPlanPath:options['--telegram-port-repair-plan'],repairExecutorRevision:options['--telegram-port-repair-executor-sha']}):undefined;
  const source=sourceCompatibility?.source??executorSource,revision=sourceCompatibility?.revision??(io.inspectSource??inspectSource)(source).revision;
  const kube=(args,input)=>{try{return (io.execFileSync??execFileSync)(options["--k3s-sudo"]?"sudo":"kubectl",[...(options["--k3s-sudo"]?["-n","k3s","kubectl"]:[]),...(kubeconfig?['--kubeconfig',kubeconfig]:[]),"--context",options["--context"],"--request-timeout=30s",...args],{input,encoding:"utf8",stdio:["pipe","pipe","pipe"],timeout:35000,maxBuffer:8<<20});}catch{throw new Error("KUBERNETES_OPERATION_FAILED_READBACK_REQUIRED");}};
  const get=(kind,name)=>JSON.parse(kube(["-n",namespace,"get",kind,name,"-o","json"]));
  const ns=get("namespace",namespace);
  if(catalog)return catalogProjectionMain(command,options,{source,revision,kubeconfig,kube,get,ns,saved,sourceCompatibility});
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
  let assigned;
  if(existing.trim()) {
    assigned=JSON.parse(existing);requireValue(projectionMatches(assigned,plan.projection),"POLICY_PROJECTION_CONFIGMAP_CONFLICT");
  } else {
    assigned=JSON.parse(kube(["create","-f","-","-o","json"],JSON.stringify(plan.projection)));
    requireValue(projectionMatches(assigned,plan.projection)&&assigned.metadata.uid,"POLICY_PROJECTION_CREATE_ACK_INVALID");
  }
  const readback=get("configmap",plan.projection.metadata.name);
  requireValue(projectionMatches(readback,plan.projection)&&readback.metadata.uid===assigned.metadata.uid,"POLICY_PROJECTION_CONFIGMAP_READBACK_FAILED");
  withPrivatePatchFile(projectionPatch(deployment,plan),path=>kube(["-n",namespace,"patch","deployment","control-plane","--type=json","--patch-file",path,"-o","name"]));
  const current=get("deployment","control-plane");
  requireValue(current.metadata.uid===plan.deploymentUID&&fingerprint(current.spec)===plan.afterSpecSHA256,"POLICY_PROJECTION_OUTCOME_UNKNOWN");
  process.stdout.write(JSON.stringify({status:"APPLIED",target:"control-plane",policySHA256:plan.renderedPolicySHA256})+"\n");
}

async function catalogProjectionMain(command,options,{source,revision,kubeconfig,kube,get,ns,saved,sourceCompatibility}) {
  const profile=command==='plan'?readAuthorityPrivateJSON(options['--portable-target']):saved?.policyTransition?.targetProfile;
  const transitionProfile=options['--policy-transition']??saved?.policyTransition?.profile;
  const current=readCatalogTarget(profile,options['--context'],get,transitionProfile);
  const maintenancePaths=command==='plan'?{planPath:options['--maintenance-plan'],evidencePath:options['--maintenance-evidence'],
    trafficPlanPath:options['--traffic-plan'],trafficEvidencePath:options['--traffic-evidence']}:saved.maintenance;
  const maintenance=command==='observe'?saved.maintenance:await readCatalogMaintenance(maintenancePaths.planPath,maintenancePaths.evidencePath,kube,maintenancePaths);
  verifyCatalogAdmission(maintenance,saved?.policyTransition??current.transition);
  if(saved) {
    requireValue(saved.version===2&&saved.kind==='CP_CATALOG_POLICY_PROJECTION'&&saved.source===source&&saved.revision===revision&&
      saved.kubeconfig===kubeconfig&&saved.context===options['--context']&&saved.namespaceUID===ns.metadata.uid&&
      fingerprint(maintenance)===fingerprint(saved.maintenance),'POLICY_PROJECTION_PLAN_IDENTITY_REJECTED');
    verifyCatalogTransition(saved.policyTransition,current.transition);
  }
  const stagePaths=command==='plan'&&options['--migration-plan']?{planPath:options['--migration-plan'],evidencePath:options['--migration-evidence']}:saved?.telegramStage;
  requireValue(!options['--migration-evidence']||stagePaths?.planPath,'TELEGRAM_CP_STAGE_INPUTS_REQUIRED');
  const readStage=()=>readTelegramStagedPolicyMigration({planPath:stagePaths?.planPath,evidencePath:stagePaths?.evidencePath,...(sourceCompatibility?.telegramPortRepair?{repairPlanPath:sourceCompatibility.telegramPortRepair.planPath,...(sourceCompatibility.repairExecutor?{repairExecutorRevision:sourceCompatibility.repairExecutor.executorRevision}:{})}:{})},
    {source,revision,context:options['--context'],kubeconfig,transition:current.transition});
  let telegramStage;
  if(stagePaths){
    requireValue(transitionProfile===telegramPolicyTransition,'TELEGRAM_CP_STAGE_SCOPE_REJECTED');
    telegramStage=saved?saved.telegramStage:readStage();
    if(saved)requireValue(fingerprint(telegramStage)===fingerprint(saved.telegramStage),'TELEGRAM_CP_STAGE_CHANGED');
  }
  if(command==='plan'&&telegramStage)requireValue(options['--authority-plan']&&options['--authority-evidence'],'TELEGRAM_AUTHORITY_DELIVERY_REQUIRED');
  const telegramAuthority=command==='plan'&&telegramStage?{planPath:options['--authority-plan'],evidencePath:options['--authority-evidence'],
    planSHA256:fingerprint(readAuthorityPrivateJSON(options['--authority-plan']))}:saved?.telegramAuthority;
  const readDelivered=()=>{
    requireValue(telegramAuthority?.planPath&&telegramAuthority.evidencePath&&options['--authority-status-plan']&&options['--authority-status-evidence'],
      'TELEGRAM_AUTHORITY_DELIVERY_REQUIRED');
    const authority=readAuthorityPrivateJSON(telegramAuthority.planPath);
    requireValue(fingerprint(authority)===telegramAuthority.planSHA256&&authority.source===source&&authority.revision===revision&&authority.context===options['--context']&&authority.kubeconfig===kubeconfig&&
      authority.policyTransition?.profile===telegramPolicyTransition&&fingerprint(authority.introducedReceiver)===telegramStage.proof.introducedReceiverSHA256,
      'TELEGRAM_CP_PROJECTION_SCOPE_CHANGED');
    verifyCatalogTransition(authority.policyTransition,current.transition);
    const rows=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
    return readTelegramAuthorityDelivered({plan:authority,events:rows(telegramAuthority.evidencePath),
      freshPlan:readAuthorityPrivateJSON(options['--authority-status-plan']),freshEvents:rows(options['--authority-status-evidence']),admissionProof:maintenance.proof},kube);
  };
  const oldName=current.deployment.spec.template.spec.volumes?.find(v=>v.name===volumeName)?.configMap?.name;
  const existingProjection=oldName?get('configmap',oldName):undefined;
  if(command==='plan') {
    requireValue(options['--output']&&!options['--confirm'],'INVALID_PLAN_ARGUMENTS');
    const plan={...planCatalogPolicyProjection(current.deployment,current.registry,current.configMaps,{transition:current.transition,target:current.target,existingProjection,telegramStage}),
      ...(telegramAuthority?{telegramAuthority}:{}),...(sourceCompatibility?{sourceCompatibility}:{}),maintenance,source,revision,kubeconfig,context:options['--context'],namespaceUID:ns.metadata.uid};
    writeAuthorityPrivatePlan(options['--output'],plan);
    process.stdout.write(JSON.stringify({status:'PLANNED',revision,policySHA256:plan.renderedPolicySHA256,target:'control-plane'})+'\n');
    return;
  }
  const plan=saved;
  requireValue(current.registry.metadata.uid===plan.registry.uid&&current.registry.metadata.resourceVersion===plan.registry.resourceVersion&&
    fingerprint(current.registry.data)===plan.registry.dataSHA256,'POLICY_PROJECTION_REGISTRY_DRIFT');
  validateAuthorityPolicy(current.registry.data['authority-policy.json'],'desired',plan.policyTransition);
  const changed=current.deployment.metadata.uid===plan.deploymentUID&&fingerprint(current.deployment.spec)===plan.afterSpecSHA256;
  if(!changed) {
    const regenerated=planCatalogPolicyProjection(current.deployment,current.registry,current.configMaps,{transition:plan.policyTransition,target:current.target,existingProjection,telegramStage});
    const extra=new Set(['maintenance','source','revision','kubeconfig','context','namespaceUID','telegramAuthority','sourceCompatibility']);
    requireValue(fingerprint(regenerated)===fingerprint(Object.fromEntries(Object.entries(plan).filter(([key])=>!extra.has(key)))),'POLICY_PROJECTION_PLAN_DRIFT');
  }
  requireValue(options['--evidence'],'POLICY_PROJECTION_EVIDENCE_REQUIRED');
  if(command==='observe') {
    const events=readAuthorityPrivateText(options['--evidence']).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
    const acknowledged=validateCatalogProjectionEvents(events,plan),projection=get('configmap',plan.projection.metadata.name);
    requireValue(changed&&projectionMatches(projection,plan.projection)&&
      acknowledged.get('CP_POINTER_CAS')===current.deployment.metadata.uid&&acknowledged.get('CREATE_PROJECTION')===projection.metadata.uid,'POLICY_PROJECTION_READBACK_MISMATCH');
    process.stdout.write(JSON.stringify({status:'APPLIED',target:'control-plane',policySHA256:plan.renderedPolicySHA256})+'\n');return;
  }
  requireValue(command==='resume'||options['--confirm']===(transitionProfile===telegramPolicyTransition?'APPLY-PORTABLE-TELEGRAM-PROJECTION':'APPLY-PORTABLE-CATALOG-PROJECTION'),'CONFIRMATION_REQUIRED');
  requireValue(command!=='apply'||!changed,'POLICY_PROJECTION_EXISTING_OPERATION_REQUIRES_RESUME');
  const journal=openCatalogProjectionJournal(options['--evidence'],plan,command==='resume');
  try {
    journal.append({status:'INTENT',operation:command==='resume'?'RESUME':'APPLY'});
    const readDestination=()=>{
      const raw=kube(['-n',namespace,'get','configmap',plan.projection.metadata.name,'--ignore-not-found','-o','json']);
      return raw.trim()?JSON.parse(raw):undefined;
    };
    let destination=readDestination();
    const ack=journal.events.find(row=>row.status==='ACK'&&row.operation==='CREATE_PROJECTION');
    const apiAck=journal.events.findLast(row=>row.status==='ACK'&&row.operation==='CREATE_PROJECTION_ACK');
    requireValue((!ack||destination?.metadata.uid===ack.uid)&&(!apiAck||destination?.metadata.uid===apiAck.uid),'POLICY_PROJECTION_CONFIGMAP_REPLACED');
    if(!destination) {
      requireValue(!journal.events.some(row=>row.status==='INTENT'&&row.operation==='CREATE_PROJECTION'),'POLICY_PROJECTION_MISSING_AFTER_CREATE_INTENT');
      const fresh=readCatalogTarget(profile,options['--context'],get,transitionProfile);verifyCatalogTransition(plan.policyTransition,fresh.transition);
      requireValue(fingerprint(await readCatalogMaintenance(maintenance.planPath,maintenance.evidencePath,kube,maintenance))===fingerprint(maintenance),'POLICY_PROJECTION_MAINTENANCE_DRIFT');
      journal.append({status:'INTENT',operation:'CREATE_PROJECTION'});
      let result;
      try{result=kube(['create','-f','-','-o','json'],JSON.stringify(plan.projection));}catch{journal.append({status:'UNKNOWN',operation:'CREATE_PROJECTION'});}
      if(result!==undefined){
        let created;
        try{created=parseAuthorityPrivateJSON(result);}catch{}
        if(!projectionMatches(created,plan.projection)||!/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(created?.metadata?.uid??'')){
          journal.append({status:'UNKNOWN',operation:'CREATE_PROJECTION_ACK_REJECTED'});throw Error('POLICY_PROJECTION_CREATE_ACK_INVALID');
        }
        journal.append({status:'ACK',operation:'CREATE_PROJECTION_ACK',uid:created.metadata.uid});
      }
      destination=readDestination();
      const assigned=journal.events.findLast(row=>row.status==='ACK'&&row.operation==='CREATE_PROJECTION_ACK');
      requireValue(!assigned||destination?.metadata.uid===assigned.uid,'POLICY_PROJECTION_CONFIGMAP_REPLACED');
    }
    requireValue(projectionMatches(destination,plan.projection)&&destination.metadata.uid,'POLICY_PROJECTION_CONFIGMAP_READBACK_FAILED');
    journal.append({status:'ACK',operation:'CREATE_PROJECTION',uid:destination.metadata.uid});
    if(!changed) {
      let delivery;
      const fresh=readCatalogTarget(profile,options['--context'],get,transitionProfile);verifyCatalogTransition(plan.policyTransition,fresh.transition);
      requireValue(fingerprint(await readCatalogMaintenance(maintenance.planPath,maintenance.evidencePath,kube,maintenance))===fingerprint(maintenance),'POLICY_PROJECTION_MAINTENANCE_DRIFT');
      requireValue(fresh.registry.metadata.resourceVersion===plan.registry.resourceVersion&&fingerprint(fresh.registry.data)===plan.registry.dataSHA256,'POLICY_PROJECTION_REGISTRY_DRIFT');
      const projectionReadback=readDestination();
      requireValue(projectionReadback?.metadata.uid===destination.metadata.uid&&projectionMatches(projectionReadback,plan.projection),'POLICY_PROJECTION_CONFIGMAP_REPLACED');
      if(plan.predecessor) {
        const previous=get('configmap',plan.predecessor.name);
        requireValue(previous.metadata.uid===plan.predecessor.uid&&previous.metadata.resourceVersion===plan.predecessor.resourceVersion&&
          fingerprint(previous.data)===plan.predecessor.dataSHA256,'POLICY_PROJECTION_PREDECESSOR_REJECTED');
      }
      if(telegramStage){
        requireValue(fingerprint(readStage())===fingerprint(telegramStage),'TELEGRAM_CP_STAGE_CHANGED');
        delivery=await readDelivered();requireValue(delivery.state==='PENDING_AUTHORITY','TELEGRAM_AUTHORITY_DELIVERY_REQUIRED');
      }
      const patch=projectionPatch(fresh.deployment,plan);
      if(delivery){const now=Date.now(),observed=Date.parse(delivery.observedAt);
        requireValue(observed<=now+5000&&now-observed<=30000,'TELEGRAM_AUTHORITY_DELIVERY_REQUIRED');}
      journal.append({status:'INTENT',operation:'CP_POINTER_CAS'});
      try{withPrivatePatchFile(patch,path=>kube(['-n',namespace,'patch','deployment','control-plane','--type=json','--patch-file',path,'-o','name']));}
      catch{journal.append({status:'UNKNOWN',operation:'CP_POINTER_CAS'});}
    }
    const after=get('deployment','control-plane');
    requireValue(after.metadata.uid===plan.deploymentUID&&fingerprint(after.spec)===plan.afterSpecSHA256&&
      readDestination()?.metadata.uid===destination.metadata.uid,'POLICY_PROJECTION_OUTCOME_UNKNOWN');
    journal.append({status:'ACK',operation:'CP_POINTER_CAS',uid:after.metadata.uid});
    process.stdout.write(JSON.stringify({status:'APPLIED',target:'control-plane',policySHA256:plan.renderedPolicySHA256})+'\n');
  } finally {journal.close();}
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))Promise.resolve().then(main).catch(error=>{process.stderr.write((/^[A-Z0-9_]+$/.test(error.message)?error.message:'POLICY_PROJECTION_FAILED_READBACK_REQUIRED')+'\n');process.exitCode=1;});
