#!/usr/bin/env node
import {execFile} from 'node:child_process';
import {createHash} from 'node:crypto';
import {constants} from 'node:fs';
import {open} from 'node:fs/promises';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {resolve} from 'node:path';
import {promisify} from 'node:util';
import {fingerprint} from './stable-inventory.mjs';
import {validatedStablePublication,sourceArchiveHash} from './stable-image-publication.mjs';
import {componentMigrationConfiguration,componentMigrationPublication} from './portable-component-migration.mjs';
import {validateNodeContainer} from './stable-node-registry-model.mjs';
import {createPrivatePrettyJSONSync} from './private-json-write.mjs';

const execute=promisify(execFile),root=fileURLToPath(new URL('../../',import.meta.url));
const hash=raw=>createHash('sha256').update(raw).digest('hex');
const requireValue=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&same(Object.keys(value).sort(),[...keys].sort());
const env=()=>Object.fromEntries(['HOME','PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
async function run(program,args,timeout=30000,cwd=root){
  try{return (await execute(program,args,{cwd,env:env(),timeout,maxBuffer:32<<20})).stdout;}
  catch{throw Error('COMPONENT_PUBLICATION_OPERATION_FAILED_OR_UNKNOWN');}
}
// Private bytes читаются один раз; только canonical JSON/JSONL, без duplicate keys.
async function privateDocument(path,lines=false){
  const file=await open(resolve(path),constants.O_RDONLY|constants.O_NOFOLLOW);
  try{
    const before=await file.stat();
    requireValue(before.isFile()&&before.uid===process.getuid()&&before.nlink===1&&(before.mode&0o777)===0o600&&
      before.size>0&&before.size<=1<<20,'COMPONENT_PUBLICATION_PRIVATE_FILE_REQUIRED');
    const raw=await file.readFile(),after=await file.stat();
    requireValue(before.size===raw.length&&before.ino===after.ino&&before.size===after.size&&before.mtimeMs===after.mtimeMs&&
      before.ctimeMs===after.ctimeMs,'COMPONENT_PUBLICATION_INPUT_CHANGED');
    const text=raw.toString('utf8'),value=lines?text.trimEnd().split('\n').map(JSON.parse):JSON.parse(text);
    const canonical=lines?value.map(v=>JSON.stringify(v)+'\n').join(''):JSON.stringify(value)+'\n';
    requireValue(raw.equals(Buffer.from(canonical))||(!lines&&raw.equals(Buffer.from(JSON.stringify(value,null,2)+'\n'))),
      'COMPONENT_PUBLICATION_NONCANONICAL_JSON');
    return {raw,value};
  }finally{await file.close();}
}

export function validateComponentPublicationConfiguration(config){
  requireValue(exact(config,['schema','profile','sourceSHA','context','clusterUID','namespaceUID','baseSourceSHA','baseReleaseLockSHA256',
    'baseReleaseLock','sourceCheckout','baseSchemaHeads','schemaHeadsBefore','schemaHeadsAfter','nodes','components'])&&
    config.schema==='kodex.component-publication-config/v1'&&/^[a-f0-9]{40}$/.test(config.sourceSHA??'')&&
    config.sourceSHA!=='0'.repeat(40)&&/^[a-f0-9]{40}$/.test(config.baseSourceSHA??'')&&
    /^[a-f0-9]{64}$/.test(config.baseReleaseLockSHA256??'')&&/^k3d-[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(config.context??''),
    'COMPONENT_PUBLICATION_CONFIGURATION_INVALID');
  const profile=componentMigrationConfiguration(config.profile),uid=v=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v??'');
  requireValue(uid(config.clusterUID)&&uid(config.namespaceUID)&&Array.isArray(config.nodes)&&config.nodes.length>0&&config.nodes.length<=8&&
    config.nodes.every(n=>exact(n,['name','uid'])&&uid(n.uid)&&/^[a-z0-9][a-z0-9.-]{0,252}$/.test(n.name??''))&&
    new Set(config.nodes.map(n=>n.name)).size===config.nodes.length&&new Set(config.nodes.map(n=>n.uid)).size===config.nodes.length,
    'COMPONENT_PUBLICATION_TARGET_INVALID');
  const heads=['control-plane','internal-rpc-authority','email-bridge'];
  requireValue([config.baseSchemaHeads,config.schemaHeadsBefore,config.schemaHeadsAfter].every(h=>exact(h,heads)&&
    heads.every(k=>Number.isSafeInteger(h[k])&&h[k]>0))&&config.schemaHeadsBefore['control-plane']===profile.before&&
    config.schemaHeadsAfter['control-plane']===profile.after&&[20261112000100,20261113000100].includes(config.baseSchemaHeads['control-plane'])&&
    (profile.schema.endsWith('/v2')||same(config.baseSchemaHeads,config.schemaHeadsBefore))&&
    heads.filter(k=>k!=='control-plane').every(k=>config.baseSchemaHeads[k]===config.schemaHeadsBefore[k]&&
      config.schemaHeadsBefore[k]===config.schemaHeadsAfter[k]),'COMPONENT_PUBLICATION_SCHEMA_INVALID');
  const privatePath=v=>typeof v==='string'&&v.startsWith('/')&&!/[\r\n\0]/.test(v);
  requireValue(privatePath(config.sourceCheckout)&&privatePath(config.baseReleaseLock)&&Array.isArray(config.components)&&same(config.components.map(c=>c.name),profile.components)&&
    config.components.every(c=>exact(c,['name','expectedImageRef','anchor','buildReceipt','publicationPlan','publicationEvidence'])&&
      /^registry\.local\.kodex\/kodex\/[a-z-]+@sha256:[a-f0-9]{64}$/.test(c.expectedImageRef??'')&&
      c.expectedImageRef.startsWith(`registry.local.kodex/kodex/${c.name}@`)&&
      typeof c.anchor==='string'&&c.anchor.startsWith(profile.sources[c.name])&&/^[A-Za-z0-9_./-]+$/.test(c.anchor)&&
      !c.anchor.split('/').includes('..')&&[c.buildReceipt,c.publicationPlan,c.publicationEvidence].every(privatePath)),
    'COMPONENT_PUBLICATION_COMPONENTS_INVALID');
  return profile;
}

// Locks выводятся из actual publisher receipts и Git diff; совместимость cohort
// по-прежнему требует технического review, а не угадывается по успешной сборке.
export function createComponentImageLock(config,component,evidence,sourceEvidence){
  const profile=validateComponentPublicationConfiguration(config);
  requireValue(config.components.some(c=>same(c,component)),'COMPONENT_PUBLICATION_COMPONENTS_INVALID');
  const plan=evidence.publicationPlan,imageRef=validatedStablePublication(config.sourceSHA,evidence,component.name);
  requireValue(plan.context===config.context&&plan.clusterUID===config.clusterUID&&plan.namespaceUID===config.namespaceUID&&
    plan.source===config.sourceSHA&&same(plan.receipt,sourceEvidence.receipt)&&
    plan.receipt.contextSHA256===sourceEvidence.contextSHA256&&imageRef!==component.expectedImageRef,
    'COMPONENT_PUBLICATION_SOURCE_MISMATCH');
  const changed=sourceEvidence.changedFiles;
  requireValue(Array.isArray(changed)&&changed.length>0&&changed.length<=4096&&same(changed,[...new Set(changed)].sort())&&
    changed.includes(component.anchor)&&changed.every(p=>typeof p==='string'&&/^[A-Za-z0-9_./-]{1,512}$/.test(p)&&!p.split('/').includes('..')&&
      ['contracts/','libs/go/',profile.sources[component.name]].some(prefix=>p.startsWith(prefix)))&&
    ['productionSHA256','dockerfileSHA256','receiptSHA256'].every(k=>/^[a-f0-9]{64}$/.test(sourceEvidence[k]??'')),
    'COMPONENT_PUBLICATION_SOURCE_INVALID');
  return {schema:'kodex.component-image/v1',component:component.name,source_sha:config.sourceSHA,
    base_application_source_sha:config.baseSourceSHA,base_release_lock_sha256:config.baseReleaseLockSHA256,
    image_ref:imageRef,digest:plan.image.digest,production_file_sha256:sourceEvidence.productionSHA256,schema_heads:config.schemaHeadsAfter,
    compatibility:{changed_files:changed,production_files:changed,shared_libs_contracts_unchanged:false,
      verified_cohort:{components:profile.components,changed_files:changed,production_files:changed,anchor:component.anchor,
        contract_change:profile.contract,schema_change:true,runner_input_abi:'UNCHANGED'}},
    build:{dockerfile_sha256:sourceEvidence.dockerfileSHA256,metadata_sha256:sourceEvidence.receiptSHA256,
      private_source_sha:config.sourceSHA,platform:'linux/amd64'},qualification:'PUBLISHED_NOT_NODE_QUALIFIED_NOT_DEPLOYED'};
}

async function prepare(config){
  const profile=validateComponentPublicationConfiguration(config);
  const sourceGit=args=>run('git',args,30000,config.sourceCheckout);
  requireValue((await sourceGit(['rev-parse','HEAD'])).trim()===config.sourceSHA&&
    (await sourceGit(['status','--porcelain','--untracked-files=normal'])).trim()==='','COMPONENT_PUBLICATION_CLEAN_SOURCE_REQUIRED');
  await sourceGit(['merge-base','--is-ancestor',config.baseSourceSHA,config.sourceSHA]);
  const base=await privateDocument(config.baseReleaseLock);
  requireValue(hash(base.raw)===config.baseReleaseLockSHA256&&base.value.source_sha===config.baseSourceSHA,
    'COMPONENT_PUBLICATION_BASE_LOCK_MISMATCH');
  const contextSHA256=await sourceArchiveHash(config.sourceSHA,config.sourceCheckout),components=[];
  for(const component of config.components){
    const plan=await privateDocument(component.publicationPlan),journal=await privateDocument(component.publicationEvidence,true),
      receipt=await privateDocument(component.buildReceipt);
    const paths=['contracts/','libs/go/',profile.sources[component.name]];
    const changedFiles=(await sourceGit(['diff','--no-ext-diff','--no-textconv','--no-renames','--name-only','-z',config.baseSourceSHA,config.sourceSHA,'--',...paths]))
      .split('\0').filter(Boolean).sort();
    await sourceGit(['cat-file','-e',`${config.sourceSHA}:${component.anchor}`]);
    requireValue(receipt.value.dockerfile===`${profile.sources[component.name]}Dockerfile`,'COMPONENT_PUBLICATION_DOCKERFILE_INVALID');
    const dockerfileSHA256=hash(await sourceGit(['show',`${config.sourceSHA}:${receipt.value.dockerfile}`]));
    // Хеш полного binary diff учитывает удаления и изменения mode. Это
    // консервативный набор всех changed files, включая tests, без пропусков.
    const productionSHA256=hash(await sourceGit(['diff','--no-ext-diff','--no-textconv','--binary','--no-renames',
      config.baseSourceSHA,config.sourceSHA,'--',...paths]));
    const lock=createComponentImageLock(config,component,{publicationPlan:plan.value,publicationEvidence:journal.value},
      {receipt:receipt.value,receiptSHA256:hash(receipt.raw),contextSHA256,changedFiles,dockerfileSHA256,productionSHA256});
    // Полный manifest сохраняется; компактная запись не расходует лимит на отступы.
    const raw=Buffer.from(JSON.stringify(lock)+'\n');
    requireValue(raw.length<=65536,'COMPONENT_PUBLICATION_LOCK_TOO_LARGE');
    components.push({component:component.name,expectedImageRef:component.expectedImageRef,componentLockRawBase64:raw.toString('base64'),
      componentLockSHA256:hash(raw)});
  }
  const publication={schema:profile.schema,profile:config.profile,context:config.context,clusterUID:config.clusterUID,namespaceUID:config.namespaceUID,
    baseSourceSHA:config.baseSourceSHA,baseReleaseLockSHA256:config.baseReleaseLockSHA256,
    ...(profile.schema.endsWith('/v2')?{baseSchemaHeads:config.baseSchemaHeads}:{}),schemaHeadsBefore:config.schemaHeadsBefore,
    schemaHeadsAfter:config.schemaHeadsAfter,runtimeABI:5,runnerInput:'v10',registry:'registry.local.kodex',repositoryPrefix:'kodex',
    nodeUIDs:config.nodes.map(n=>n.uid),components};
  return {schema:'kodex.component-publication-prepared/v1',sourceSHA:config.sourceSHA,configuration:config,publication};
}

async function main(args){
  const mode=args.shift(),options={};
  while(args.length){const key=args.shift();requireValue(['--configuration','--prepared','--context','--kubeconfig','--output','--approved-plan-sha'].includes(key)&&
    !Object.hasOwn(options,key)&&args.length,'COMPONENT_PUBLICATION_ARGUMENTS_INVALID');options[key]=args.shift();}
  requireValue(['prepare','qualify','readback'].includes(mode)&&options['--output'],'COMPONENT_PUBLICATION_ARGUMENTS_INVALID');
  if(mode==='prepare'){
    requireValue(options['--configuration']&&Object.keys(options).length===2,'COMPONENT_PUBLICATION_ARGUMENTS_INVALID');
    const prepared=await prepare((await privateDocument(options['--configuration'])).value);
    createPrivatePrettyJSONSync(resolve(options['--output']),prepared);
    console.log(JSON.stringify({status:'PASS',phase:'PREPARED_NOT_NODE_QUALIFIED',planSHA256:fingerprint(prepared)}));return;
  }
  requireValue(options['--prepared']&&options['--context']&&options['--kubeconfig']?.startsWith('/')&&!options['--configuration']&&
    !/[\r\n\0]/.test(options['--kubeconfig']),'COMPONENT_PUBLICATION_ARGUMENTS_INVALID');
  const prepared=(await privateDocument(options['--prepared'])).value;
  requireValue(exact(prepared,['schema','sourceSHA','configuration','publication'])&&prepared.schema==='kodex.component-publication-prepared/v1'&&
    prepared.configuration.context===options['--context']&&prepared.sourceSHA===prepared.configuration.sourceSHA,
    'COMPONENT_PUBLICATION_PREPARED_INVALID');
  requireValue(mode==='qualify'?options['--approved-plan-sha']===fingerprint(prepared):!options['--approved-plan-sha'],
    'COMPONENT_PUBLICATION_EXACT_PLAN_REQUIRED');
  // Повторная локальная проверка bytes/receipts/source не подменяется флагом в JSON.
  requireValue(same(await prepare(prepared.configuration),prepared),'COMPONENT_PUBLICATION_PREPARED_DRIFT');
  const config=prepared.configuration,context=config.context;
  const kube=async argv=>JSON.parse(await run('kubectl',['--kubeconfig',options['--kubeconfig'],'--context',context,'--request-timeout=15s',...argv,'-o','json']));
  const snapshot=async()=>{
    const system=await kube(['get','namespace','kube-system']),ns=await kube(['get','namespace','kodex-system']),nodes=await kube(['get','nodes']);
    requireValue(system.metadata?.uid===config.clusterUID&&ns.metadata?.uid===config.namespaceUID&&Array.isArray(nodes.items)&&
      nodes.items.length===config.nodes.length&&nodes.items.every(n=>config.nodes.some(c=>c.name===n.metadata?.name&&c.uid===n.metadata?.uid))&&
      nodes.items.every(n=>!n.metadata.deletionTimestamp&&n.status?.conditions?.some(c=>c.type==='DiskPressure'&&c.status==='False')),'COMPONENT_PUBLICATION_TARGET_DRIFT');
    const bound=[];
    for(const node of nodes.items){const inspect=JSON.parse(await run('docker',['inspect','--type','container',node.metadata.name]));
      requireValue(inspect.length===1,'COMPONENT_PUBLICATION_NODE_INVALID');
      bound.push(validateNodeContainer(node,inspect[0],context.slice(4)));}
    return bound.sort((a,b)=>a.name.localeCompare(b.name));
  };
  const before=await snapshot(),publication=structuredClone(prepared.publication);
  for(const part of publication.components){
    const lock=JSON.parse(Buffer.from(part.componentLockRawBase64,'base64')),nodes=[];
    for(const node of before){
      requireValue(same(await snapshot(),before),'COMPONENT_PUBLICATION_TARGET_DRIFT');
      const cri=['exec',node.containerID,'crictl','--runtime-endpoint','unix:///run/k3s/containerd/containerd.sock',
        '--image-endpoint','unix:///run/k3s/containerd/containerd.sock'];
      if(mode==='qualify')await run('docker',[...cri,'pull',lock.image_ref],180000);
      const image=JSON.parse(await run('docker',[...cri,'inspecti','-q','-o','json',lock.image_ref]));
      requireValue(/^sha256:[a-f0-9]{64}$/.test(image.status?.id??'')&&image.status?.repoDigests?.includes(lock.image_ref),
        'COMPONENT_PUBLICATION_NODE_DIGEST_MISMATCH');
      nodes.push({name:node.name,uid:node.uid,imageRef:lock.image_ref,repoDigestVerified:true});
    }
    part.nodeProof={schema:'kodex.component-node-proof/v1',component:part.component,sourceSHA:config.sourceSHA,
      componentLockSHA256:part.componentLockSHA256,imageRef:lock.image_ref,clusterUID:config.clusterUID,namespaceUID:config.namespaceUID,nodes};
  }
  requireValue(same(await snapshot(),before),'COMPONENT_PUBLICATION_TARGET_DRIFT');
  componentMigrationPublication(publication,{revision:config.sourceSHA},config.profile);
  createPrivatePrettyJSONSync(resolve(options['--output']),publication);
  console.log(JSON.stringify({status:'PASS',phase:'NODE_QUALIFIED_NOT_DEPLOYED',publicationSHA256:fingerprint(publication)}));
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)main(process.argv.slice(2)).catch(error=>{
  console.error(JSON.stringify({status:'FAIL_OR_UNKNOWN',code:/^[A-Z_]+$/.test(error.message??'')?error.message:'COMPONENT_PUBLICATION_FAILED'}));process.exitCode=1;
});
