#!/usr/bin/env node
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {createPrivatePrettyJSONSync} from './private-json-write.mjs';
import {readTelegramEgressRevisionCandidate} from './telegram-egress-revision-repair.mjs';
import {readComponentNodeTarget,qualifyComponentNodeImages} from './component-node-qualification.mjs';

const fail=ok=>{if(!ok)throw Error('TELEGRAM_EGRESS_CANDIDATE_REJECTED');};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const path=value=>typeof value==='string'&&value===resolve(value)&&!/[\r\n\0]/.test(value);

// Source descriptor предварительно содержит nodeProof:null и сам не допускается
// migrator. Plan связывает точные target/контейнеры; qualify выдаёт исполнимый pin.
export async function telegramEgressCandidatePlan({candidatePath,migrationPath,kubeconfig,context}){
 fail(path(candidatePath)&&path(migrationPath)&&path(kubeconfig));
 const migration=readAuthorityPrivateJSON(migrationPath),candidate=readAuthorityPrivateJSON(candidatePath);
 fail(migration.profile==='telegram-notifications-backup'||migration.profile==='telegram-notifications');
 fail(migration.migrationVersion==='20261114140000'&&migration.context===context&&migration.kubeconfig===kubeconfig&&candidate.nodeProof===null);
 const source=await readTelegramEgressRevisionCandidate(candidatePath,migration,{allowUnqualified:true});
 const target={context,clusterUID:migration.clusterUID,namespaceUID:migration.namespaceUID,
  nodes:migration.imageEvidence.components[0].nodeProof.nodes.map(n=>({name:n.name,uid:n.uid})).sort((a,b)=>a.name.localeCompare(b.name))};
 const binding=await readComponentNodeTarget(target,kubeconfig);
 return {schema:'kodex.telegram-egress-node-plan/v1',candidatePath,migrationPath,kubeconfig,context,
  migrationSHA256:fingerprint(migration),candidateSHA256:fingerprint(candidate),source,target,binding};
}

async function main(args){
 const phase=args.shift(),options={};
 while(args.length){const key=args.shift();fail(['--candidate','--migration-plan','--kubeconfig','--context','--plan','--output','--approved-plan-sha'].includes(key)&&
  !Object.hasOwn(options,key)&&args.length);options[key]=args.shift();}
 fail(['plan','qualify','readback'].includes(phase)&&path(options['--output']));
 if(phase==='plan'){
  fail(Object.keys(options).length===5&&options['--candidate']&&options['--migration-plan']&&options['--kubeconfig']&&options['--context']);
  const plan=await telegramEgressCandidatePlan({candidatePath:options['--candidate'],migrationPath:options['--migration-plan'],kubeconfig:options['--kubeconfig'],context:options['--context']});
  createPrivatePrettyJSONSync(options['--output'],plan);
  console.log(JSON.stringify({status:'PLAN_ONLY',planSHA256:fingerprint(plan),imageRef:plan.source.imageRef}));return;
 }
 fail(Object.keys(options).length===(phase==='qualify'?5:4)&&path(options['--plan'])&&path(options['--kubeconfig'])&&options['--context']&&
  (phase==='qualify'?typeof options['--approved-plan-sha']==='string':!options['--approved-plan-sha']));
 const plan=readAuthorityPrivateJSON(options['--plan']);
 fail(plan.schema==='kodex.telegram-egress-node-plan/v1'&&plan.kubeconfig===options['--kubeconfig']&&plan.context===options['--context']&&
  (phase!=='qualify'||fingerprint(plan)===options['--approved-plan-sha']));
 const actual=await telegramEgressCandidatePlan(plan);fail(same(plan,actual));
 const result=await qualifyComponentNodeImages({target:plan.target,kubeconfig:plan.kubeconfig,images:[plan.source.imageRef],mode:phase,expectedBinding:plan.binding});
 const candidate=readAuthorityPrivateJSON(plan.candidatePath);fail(fingerprint(candidate)===plan.candidateSHA256);
 candidate.nodeProof={schema:'kodex.telegram-egress-node-proof/v1',sourceSHA:plan.source.source.revision,imageRef:plan.source.imageRef,
  clusterUID:plan.target.clusterUID,namespaceUID:plan.target.namespaceUID,bindingSHA256:fingerprint(result.binding),nodes:result.images[0].nodes};
 createPrivatePrettyJSONSync(options['--output'],candidate);
 console.log(JSON.stringify({status:'NODE_QUALIFIED_NOT_DEPLOYED',candidateSHA256:fingerprint(candidate),imageRef:plan.source.imageRef}));
}

if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)main(process.argv.slice(2)).catch(error=>{
 console.error(JSON.stringify({status:'FAIL',code:/^[A-Z_]+$/.test(error.message)?error.message:'TELEGRAM_EGRESS_CANDIDATE_FAILED'}));process.exitCode=1;
});
