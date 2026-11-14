import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,rmSync,chmodSync} from 'node:fs';
import {join} from 'node:path';
import {fixture as authorityFixture} from './fixtures/authority-catalog-synthetic.mjs';
import {fingerprint} from './scoped-release.mjs';
import {telegramPolicyTransition,telegramPolicySource} from './authority-telegram-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';
import {readCatalogTarget} from './authority-catalog-transition.mjs';
import {planCatalogPolicyProjection} from './control-plane-policy-projection.mjs';
import {telegramStagedPolicyProof,readTelegramStagedPolicyMigration,readTelegramCPProjection,telegramProjectedCPSpec,telegramStagedPolicyProtocol} from './telegram-cp-policy.mjs';

function fixture(t){
 const f=authorityFixture(),directory=mkdtempSync('/tmp/kodex-telegram-cp-stage-');chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));
 f.deployment.spec.replicas=0;f.deployment.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
 const current=readCatalogTarget(f.profile,f.profile.context,f.get,telegramPolicyTransition),transition=current.transition;
 f.registry.data['authority-policy.json']=renderAuthorityPolicyEnvironment(telegramPolicySource('desired').raw,transition.environment).raw;
 const cp={name:'control-plane',uid:f.deployment.metadata.uid,stagedSpec:structuredClone(f.deployment.spec),resumedSpec:{...structuredClone(f.deployment.spec),replicas:3}};
 const migration={version:2,kind:'CONTROL_PLANE_MIGRATION',id:'migration-id',profile:'telegram-notifications',migrationVersion:'20261114140000',source:{path:directory,revision:'a'.repeat(40)},
  imageEvidence:{introducedReceiver:{synthetic:'server-assigned'}},context:f.profile.context,kubeconfig:join(directory,'kubeconfig'),portableTarget:f.profile,postconditionSQLSHA256:'b'.repeat(64),
  componentHandoff:{introducedReadiness:telegramStagedPolicyProtocol,publicationSHA256:'c'.repeat(64),components:[cp,...['control-api-gateway','staff-control-center','egress-gateway','interaction-gateway'].map(name=>({name}))]}};
 const rows=[{type:'HEADER',kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(migration)},{type:'STOP_ACK'},{type:'TERMINAL_READBACK',jobUID:'00000000-0000-4000-8000-000000000050'},{type:'PAIR_HANDOFF_ACK',publicationSHA256:migration.componentHandoff.publicationSHA256}];
 const inspection={status:'PASS',schemaPostcondition:'PASS',cpStage:'STAGED',podsGone:true,cpReady:false,pairReady:false,jobUID:'00000000-0000-4000-8000-000000000050',
  componentPair:migration.componentHandoff.components.map(part=>({name:part.name,stage:'stagedSpec',joined:true,ready:false}))};
 const scope={source:directory,revision:migration.source.revision,context:migration.context,kubeconfig:migration.kubeconfig,transition};
 const paths={planPath:join(directory,'migration.json'),evidencePath:join(directory,'migration.jsonl')};
 const write=(path,value)=>writeFileSync(path,JSON.stringify(value)+'\n',{mode:0o600});
 write(paths.planPath,migration);writeFileSync(paths.evidencePath,rows.map(row=>JSON.stringify(row)+'\n').join(''),{mode:0o600});
 const stage={...paths,proof:telegramStagedPolicyProof(migration,rows,inspection,scope)};
 return {...f,directory,current,transition,cp,migration,rows,inspection,scope,paths,write,stage};
}

test('CP0 admission требует полный owner inspect/join; STAGED не выдаёт READY и106guard сохранён',t=>{
 const f=fixture(t),proof=readTelegramStagedPolicyMigration(f.paths,f.scope,{inspect:()=>f.inspection});assert.deepEqual(proof,f.stage);
 assert.throws(()=>planCatalogPolicyProjection(f.deployment,f.registry,[f.oidc],{transition:f.transition,target:f.current.target}),/SAFE_ROLLOUT_REQUIRED/);
 const plan=planCatalogPolicyProjection(f.deployment,f.registry,[f.oidc],{transition:f.transition,target:f.current.target,telegramStage:f.stage});
 assert.equal(telegramProjectedCPSpec(f.cp.stagedSpec,plan).replicas,0);
 for(const mutate of [value=>value.status='RUNNING',value=>value.schemaPostcondition='FAIL',value=>value.cpReady=true,value=>value.podsGone=false,
  value=>value.componentPair[1].joined=false,value=>value.componentPair.pop(),value=>value.componentPair[0].stage='resumedSpec']){
  const changed=structuredClone(f.inspection);mutate(changed);assert.throws(()=>readTelegramStagedPolicyMigration(f.paths,f.scope,{inspect:()=>changed}),/STAGE_JOIN_REQUIRED/);
 }
 assert.throws(()=>telegramStagedPolicyProof(f.migration,[...f.rows,{type:'RESUME_INTENT'}],f.inspection,f.scope),/STAGE_JOURNAL_REJECTED/);
 assert.throws(()=>telegramStagedPolicyProof(f.migration,f.rows,f.inspection,{...f.scope,kubeconfig:'/foreign'}),/STAGE_SCOPE_REJECTED/);
 assert.throws(()=>planCatalogPolicyProjection(f.deployment,f.registry,[f.oidc],{transition:{...f.transition,profile:'MODEL_DISCOVERY_105_106'},target:f.current.target,telegramStage:f.stage}));
});

test('CP projected spec принимается только own ACK UID/journal, source107 и exact pointer delta',t=>{
 const f=fixture(t),plan={...planCatalogPolicyProjection(f.deployment,f.registry,[f.oidc],{transition:f.transition,target:f.current.target,telegramStage:f.stage}),
  source:f.scope.source,revision:f.scope.revision,context:f.scope.context,kubeconfig:f.scope.kubeconfig};
 const path=join(f.directory,'projection.json'),evidence=join(f.directory,'projection.jsonl'),uid='00000000-0000-4000-8000-000000000100';
 const cm={...structuredClone(plan.projection),metadata:{...plan.projection.metadata,uid,resourceVersion:'1'}};
 const rows=[{status:'INTENT',operation:'APPLY'},{status:'INTENT',operation:'CREATE_PROJECTION'},{status:'ACK',operation:'CREATE_PROJECTION_ACK',uid},
  {status:'ACK',operation:'CREATE_PROJECTION',uid},{status:'INTENT',operation:'CP_POINTER_CAS'},{status:'ACK',operation:'CP_POINTER_CAS',uid:f.cp.uid}];
 const persist=()=>{f.write(path,plan);writeFileSync(evidence,rows.map(row=>JSON.stringify({...row,planSHA256:fingerprint(plan),at:new Date().toISOString()})+'\n').join(''),{mode:0o600});};persist();
 const kube=args=>JSON.stringify(args.includes(cm.metadata.name)?cm:f.registry);
 const read=()=>readTelegramCPProjection({migration:f.migration,planPath:path,evidencePath:evidence},kube);
 assert.equal(read().stagedSpec.replicas,0);assert.equal(read().resumedSpec.replicas,3);
 cm.metadata.uid='00000000-0000-4000-8000-000000000101';assert.throws(read,/READBACK_CHANGED/);cm.metadata.uid=uid;
 rows.pop();persist();assert.throws(read,/READBACK_CHANGED/);
 rows.push({status:'ACK',operation:'CP_POINTER_CAS',uid:f.cp.uid});
 plan.desired.env.push({name:'FOREIGN',value:'injected'});plan.afterSpecSHA256=fingerprint(telegramProjectedCPSpec(f.cp.stagedSpec,plan));persist();
 assert.throws(read,/SPEC_REJECTED/);
});
