import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {inspectSource,sourceGitArguments} from './application-source.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {qualifyComponentNodeImages} from './component-node-qualification.mjs';
import {sourceArchiveHash,validatedStablePublication} from './stable-image-publication.mjs';
import {fingerprint} from './scoped-release.mjs';
import {isProvenStoppedPod} from './stable-archive-transition.mjs';
import {componentPodMatchesTemplate} from './portable-component-migration.mjs';
import {workflowFileProvenanceMigration} from './workflow-file-provenance-migration-contract.mjs';
import {artifactGrantCatalogPostconditionSQL,artifactGrantCatalogReceipt,artifactGrantCatalogRequired,artifactGrantSchemaVersion,artifactGrantSchemaCommand,
 artifactGrantSchemaJobName,artifactGrantSchemaReceiptType,verifyArtifactGrantCatalogProof} from './artifact-retention-grant-catalog.mjs';

const prefix='PERSONAL_ARTIFACT_GRANT_CATALOG',namespace='kodex-system';
const intentType=prefix+'_CREATE_INTENT',ackType=prefix+'_CREATE_ACK',marker='kodex.dev/artifact-grant-schema-release';
const verify=(ok,code=prefix+'_REJECTED')=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const digest=body=>createHash('sha256').update(body).digest('hex');
const uuid=value=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value??'');
const migrationFile=artifactGrantSchemaVersion+'_control_plane_artifact_retention_grant.sql';
const cliRoot='services/internal/control-plane/cmd/cli/';
const repairResumeAck='PERSONAL_ARTIFACT_GRANT_REPAIR_RESUME_ACK';

// Переиспользуем зафиксированный образ 049 с той же миграционной командой 050.
export function readStagedArtifactGrantCatalogCandidate(migration,imageRef){
 const {path,revision}=migration.source;
 const part=migration.imageEvidence.components.find(part=>part.component==='control-plane');
 verify(artifactGrantCatalogRequired(migration)&&inspectSource(path).revision===revision&&
  imageRef===migration.job.spec.template.spec.containers[0].image&&part?.nodeProof?.imageRef===imageRef&&
  part.nodeProof.sourceSHA===revision);
 const migrationSQLSHA256=digest(readFileSync(resolve(path,cliRoot,'migrations',migrationFile)));
 verify(migrationSQLSHA256===workflowFileProvenanceMigration.artifactSHA256);
 return {imageRef,sourceSHA:revision,migrationSQLSHA256,
  publicationSHA256:fingerprint(migration.imageEvidence),nodeProofSHA256:fingerprint(part.nodeProof)};
}

export async function readArtifactGrantCatalogCandidate(path,migration){
 const input=readAuthorityPrivateJSON(path);
 verify(input.schema==='kodex.artifact-grant-catalog-release-inputs/v1'&&
  Object.keys(input).sort().join(',')==='nodeProofPath,publicationEvidencePath,publicationPlanPath,schema,source,sourceSHA'&&
  typeof input.source==='string'&&resolve(input.source)===input.source&&/^[a-f0-9]{40}$/.test(input.sourceSHA)&&
  input.sourceSHA!==migration.source.revision&&inspectSource(input.source).revision===input.sourceSHA);
 const git=(...args)=>execFileSync('git',sourceGitArguments(input.source,...args),
  {timeout:15000,maxBuffer:1<<20,stdio:['ignore','pipe','pipe']});
 git('merge-base','--is-ancestor',migration.source.revision,input.sourceSHA);
 const changed=git('diff','--no-ext-diff','--no-renames','--name-only','-z',migration.source.revision,input.sourceSHA)
  .toString().split('\0').filter(Boolean);
 const allowed=[
  'Makefile','docs/guides/distributed-security.md','docs/operations/personal-email-2168.md','scripts/tests/control-plane-postgres-test.sh',
  ...['artifact_retention_grant.go','artifact_retention_grant_sql/postcondition.sql','artifact_retention_grant_sql/read_role.sql','artifact_retention_grant_test.go',
    'migrations/'+migrationFile,'postgres.go','postgres_test.go','workspace_backup.go'].map(file=>cliRoot+file),
 ];
 verify(changed.includes(cliRoot+'migrations/'+migrationFile)&&changed.every(file=>allowed.includes(file)));
 const publicationPlan=readAuthorityPrivateJSON(input.publicationPlanPath);
 const publicationEvidence=readAuthorityPrivateText(input.publicationEvidencePath).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
 const imageRef=validatedStablePublication(input.sourceSHA,{publicationPlan,publicationEvidence},'control-plane');
 verify(publicationPlan.source===input.sourceSHA&&publicationPlan.context===migration.context&&
  publicationPlan.clusterUID===migration.clusterUID&&publicationPlan.namespaceUID===migration.namespaceUID&&
  publicationPlan.receipt.dockerfile==='services/internal/control-plane/Dockerfile'&&
  publicationPlan.receipt.contextSHA256===await sourceArchiveHash(input.sourceSHA,input.source));
 const nodes=readAuthorityPrivateJSON(input.nodeProofPath);
 verify(Array.isArray(nodes.binding)&&same(nodes.binding.map(node=>node.uid).sort(),[...migration.imageEvidence.nodeUIDs].sort()));
 const target={context:migration.context,clusterUID:migration.clusterUID,namespaceUID:migration.namespaceUID,
  nodes:nodes.binding.map(({name,uid})=>({name,uid}))};
 const ready=await qualifyComponentNodeImages({target,kubeconfig:migration.kubeconfig,images:[imageRef],mode:'readback',expectedBinding:nodes.binding});
 verify(same(ready,nodes)&&inspectSource(input.source).revision===input.sourceSHA);
 return {imageRef,sourceSHA:input.sourceSHA,migrationSQLSHA256:digest(readFileSync(resolve(input.source,cliRoot,'migrations',migrationFile))),
  publicationSHA256:fingerprint({publicationPlan,publicationEvidence}),nodeProofSHA256:fingerprint(ready)};
}

function buildJob(migration,candidate,planSHA256){
 const predecessor=migration.job,job=structuredClone(predecessor),pod=job.spec.template.spec;
 verify(predecessor.kind==='Job'&&predecessor.apiVersion==='batch/v1'&&pod.serviceAccountName==='control-plane-migrator'&&
  pod.automountServiceAccountToken===false&&pod.restartPolicy==='Never'&&pod.containers.length===1&&pod.containers[0].name==='migrate'&&
  predecessor.spec.backoffLimit===0&&String(job.metadata.labels['app.kubernetes.io/component'])==='migration'&&
  /^registry\.local\.kodex\/kodex\/control-plane@sha256:[a-f0-9]{64}$/.test(candidate.imageRef));
 job.metadata.name=artifactGrantSchemaJobName;
 job.metadata.annotations={[marker]:planSHA256};
 job.spec.template.metadata.annotations={...job.spec.template.metadata.annotations,'kodex.dev/source-revision':candidate.sourceSHA};
 pod.containers[0].image=candidate.imageRef;
 pod.containers[0].args=[artifactGrantSchemaCommand];
 return job;
}

export function readArtifactGrantCatalogPostcondition(migration,readOwnerSQL,expectedSQLSHA256){
 const profile=migration.profile.endsWith('-backup')?'-backup':'';
 const raw=readFileSync(resolve(migration.source.path,'tools/release/personal-integrations'+profile+'-migration-postcondition.sql'));
 const sql=artifactGrantCatalogPostconditionSQL(raw,migration.postconditionSQLSHA256),sqlSHA256=digest(sql);
 verify(!expectedSQLSHA256||sqlSHA256===expectedSQLSHA256);
 const proof=readOwnerSQL(sql);
 return {...verifyArtifactGrantCatalogProof(proof,profile==='-backup'),sqlSHA256};
}

export async function releaseArtifactGrantCatalog({migration,journal,kube,normalizePodSpec,readJobPods,readOwnerSQL,readAPI,
 readAuthority,checkMigration,loadCandidate,bindExecutor,readStaged,authorizeStaged,wait,clock,approvedSHA256,successReceipt}){
 const events=journal.events;
 const staged=artifactGrantCatalogRequired(migration);
 verify(migration.profile==='personal-integrations-backup'&&
  !events.some(row=>['PERSONAL_INTEGRATIONS_TERMINAL_ACCEPTED','BACKUP_RESUME_INTENT'].includes(row.type)));
 const binding=events.find(row=>row.type==='PERSONAL_INTEGRATIONS_READERS_BOUND');
 if(staged){
  verify(typeof readStaged==='function'&&typeof authorizeStaged==='function'&&!binding&&
   !events.some(row=>['RESUME_INTENT','PAIR_RESUME_INTENT','PERSONAL_EXECUTOR_BOUND'].includes(row.type)||
    /^PERSONAL_ARTIFACT_(?:SOCKET|GRANT)_REPAIR_/.test(row.type)));
  readStaged();
 }else verify(events.some(row=>row.type===repairResumeAck)&&binding&&readAPI().openPlanSHA256===binding.openPlanSHA256);
 const candidate=await loadCandidate(),migrationPlanSHA256=fingerprint(migration);
 const admission=staged?{admission:'STAGED_COHORT'}:{operationID:binding.operationID};
 if(staged)verify(candidate.sourceSHA===migration.source.revision&&candidate.imageRef===migration.job.spec.template.spec.containers[0].image);
 const planSHA256=fingerprint({migrationPlanSHA256,candidate,...admission,jobName:artifactGrantSchemaJobName});
 const job=buildJob(migration,candidate,planSHA256),jobSpecSHA256=fingerprint(normalizePodSpec(job.spec.template.spec));
 const intents=events.filter(row=>row.type===intentType),acks=events.filter(row=>row.type===ackType);
 verify(intents.length<=1&&acks.length<=1&&(!intents.length||intents[0].planSHA256===planSHA256&&intents[0].jobSpecSHA256===jobSpecSHA256)&&
  (!acks.length||intents.length===1&&events.indexOf(intents[0])<events.indexOf(acks[0])&&acks[0].planSHA256===planSHA256&&
   acks[0].jobSpecSHA256===jobSpecSHA256&&uuid(acks[0].jobUID)));
 let observedUID=acks[0]?.jobUID;
 const validate=value=>{
  verify(value.kind==='Job'&&value.metadata.name===artifactGrantSchemaJobName&&value.metadata.namespace===namespace&&
   uuid(value.metadata.uid)&&(!observedUID||value.metadata.uid===observedUID)&&value.metadata.annotations?.[marker]===planSHA256&&
   !value.metadata.deletionTimestamp&&value.spec.backoffLimit===job.spec.backoffLimit&&value.spec.activeDeadlineSeconds===job.spec.activeDeadlineSeconds&&
   fingerprint(normalizePodSpec(value.spec.template.spec))===jobSpecSHA256);
  return value;
 };
 const read=()=>{
  const raw=kube(['-n',namespace,'get','job',artifactGrantSchemaJobName,'--ignore-not-found=true','-o','json']);
  return raw.trim()?validate(JSON.parse(raw)):undefined;
 };
 const remember=value=>{
  if(!observedUID){
   observedUID=value.metadata.uid;
   journal.append({type:ackType,planSHA256,jobSpecSHA256,jobUID:observedUID});
  }
  return value;
 };
 if(!intents.length){
  verify(!read());
  checkMigration();
  const simulated=JSON.parse(kube(['create','--dry-run=server','-f','-','-o','json'],JSON.stringify(job)));
  verify(simulated.metadata?.name===job.metadata.name&&simulated.metadata.annotations?.[marker]===planSHA256&&
   fingerprint(normalizePodSpec(simulated.spec.template.spec))===jobSpecSHA256);
 }
 if(approvedSHA256===undefined){
  verify(!intents.length);
  return {status:'PLAN_ONLY',planSHA256,migrationPlanSHA256,jobName:artifactGrantSchemaJobName,imageRef:candidate.imageRef};
 }
 verify(approvedSHA256===planSHA256);
 if(!intents.length){
  if(staged){
   await authorizeStaged();
   verify(!read());
  }else{
   const proof=await readAuthority();
   verify(proof.state==='RETIRED'&&proof.policyRevision===110&&proof.operationID===binding.operationID&&proof.bindingSHA256===binding.bindingSHA256&&
    readAPI().openPlanSHA256===binding.openPlanSHA256&&!read());
   const observed=Date.parse(proof.observedAt);
   verify(observed<=Date.now()+5000&&Date.now()-observed<=30000);
   bindExecutor();
  }
  journal.append({type:intentType,planSHA256,migrationPlanSHA256,jobSpecSHA256,...(staged?admission:{}),...candidate});
  let response;
  try{response=kube(['create','-f','-','-o','json'],JSON.stringify(job));}catch{throw Error(prefix+'_CREATE_UNKNOWN');}
  remember(validate(JSON.parse(response)));
 }
 const observe=()=>{const value=read();return value?remember(value):undefined;};
 const terminal=await wait(observe,value=>value?.status?.conditions?.some(row=>['Complete','Failed'].includes(row.type)&&row.status==='True'),
  prefix+'_JOB_NOT_TERMINAL',clock);
 verify(terminal.status?.succeeded===1&&!terminal.status.active&&!terminal.status.terminating&&
  terminal.status.conditions.filter(row=>row.type==='Complete'&&row.status==='True').length===1&&
  !terminal.status.conditions.some(row=>row.type==='Failed'&&row.status==='True'));
 const pods=readJobPods(terminal.metadata.uid);
 const imageDigest=candidate.imageRef.split('@')[1];
 verify(pods.length===1&&pods.every(pod=>isProvenStoppedPod(pod)&&componentPodMatchesTemplate(terminal,pod)&&pod.status.phase==='Succeeded'&&
  (pod.status.initContainerStatuses??[]).every(entry=>(entry.restartCount??0)===0&&entry.state.terminated.exitCode===0)&&
  pod.status.containerStatuses.length===1&&pod.status.containerStatuses.some(entry=>entry.name==='migrate'&&
   (entry.restartCount??0)===0&&entry.state.terminated.exitCode===0&&typeof entry.imageID==='string'&&
   (entry.imageID===imageDigest||entry.imageID==='containerd://'+imageDigest||entry.imageID.endsWith('@'+imageDigest)))));
 const logs=kube(['-n',namespace,'logs','job/'+artifactGrantSchemaJobName,'-c','migrate','--limit-bytes=262144']);
 verify(logs.trim().split('\n').includes(successReceipt));
 const post=readArtifactGrantCatalogPostcondition(migration,readOwnerSQL);
 const receipt={type:artifactGrantSchemaReceiptType,planSHA256,migrationPlanSHA256,jobName:artifactGrantSchemaJobName,
  jobUID:terminal.metadata.uid,jobSpecSHA256,...post};
 const prior=artifactGrantCatalogReceipt(migration,events);
 if(prior){const {at,...body}=prior;verify(same(receipt,body));}
 else journal.append(receipt);
 return {status:'ARTIFACT_GRANT_CATALOG_APPLIED',...receipt};
}
