import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,rmSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {fingerprint} from './scoped-release.mjs';
import {validateStableReceipt} from './stable-image-publication.mjs';
import {qualifyComponentNodeImages} from './component-node-qualification.mjs';
import {readPersonalIntegrationsAuthorityImage} from './personal-integrations-authority-image.mjs';

// Только synthetic registry/node custody. Никаких actual Docker/CRI либо pull.
async function fixture(t){
 const dir=mkdtempSync(join(tmpdir(),'kodex-authority-image-'));t.after(()=>rmSync(dir,{recursive:true,force:true}));
 const uuid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sourceSHA='a'.repeat(40);
 const target={context:'k3d-synthetic',clusterUID:uuid(1),namespaceUID:uuid(2),nodes:[{name:'k3d-synthetic-server-0',uid:uuid(3)}]};
 const node={metadata:{...target.nodes[0]},status:{conditions:[{type:'Ready',status:'True'},{type:'DiskPressure',status:'False'}]}};
 const container={Id:'b'.repeat(64),Image:'sha256:'+'c'.repeat(64),Name:'/'+node.metadata.name,State:{Running:true},
  Config:{Labels:{'k3d.cluster':'synthetic','k3d.role':'server'},Cmd:[],Env:[],Entrypoint:['/bin/k3s']}};
 const receipt={schema:'kodex.stable-local-build/v1',component:'internal-rpc-authority',sourceSHA,contextSHA256:'b'.repeat(64),archiveSHA256:'c'.repeat(64),
  imageDigest:'sha256:'+'d'.repeat(64),publication:'NOT_RUN',admission:'NOT_RUN',nodePull:'NOT_RUN'};
 const plan={schema:'kodex.stable-image-publication/v2',artifactProfile:'built',context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,
  receipt,image:validateStableReceipt(receipt),archiveSHA256:receipt.archiveSHA256};
 const imageRef='registry.local.kodex/kodex/internal-rpc-authority@'+receipt.imageDigest;
 const events=[{schema:'kodex.stable-image-publication-attempt/v1',status:'INTENT',mode:'apply',planSHA256:fingerprint(plan),component:receipt.component,digest:receipt.imageDigest},
  {status:'PASS',phase:'REGISTRY_PUBLICATION_ONLY',nodePull:'NOT_RUN'}];
 const calls=[],faults={};
 const operation=async(program,args)=>{
  calls.push({program,args});
  if(program==='kubectl'){
   if(args.includes('kube-system'))return JSON.stringify({metadata:{uid:target.clusterUID}});
   if(args.includes('kodex-system'))return JSON.stringify({metadata:{uid:target.namespaceUID}});
   return JSON.stringify({items:[node]});
  }
  assert.equal(program,'docker');
  if(args[0]==='inspect')return JSON.stringify([container]);
  assert.equal(args[0],'exec');assert.equal(args[1],container.Id);assert.equal(args.at(-1),imageRef);
  assert.ok(args.includes('inspecti'));assert.equal(args.includes('pull'),false);
  return JSON.stringify({status:{id:receipt.imageDigest,repoDigests:faults.digest?[]:[imageRef]}});
 };
 const paths={publicationPlanPath:join(dir,'plan'),publicationEvidencePath:join(dir,'events'),nodeProofPath:join(dir,'nodes')};
 const write=()=>{
  writeFileSync(paths.publicationPlanPath,JSON.stringify(plan),{mode:0o600});
  writeFileSync(paths.publicationEvidencePath,events.map(row=>JSON.stringify(row)+'\n').join(''),{mode:0o600});
 };
 write();const proof=await qualifyComponentNodeImages({target,kubeconfig:'/synthetic/kubeconfig',images:[imageRef],mode:'readback'},operation);
 writeFileSync(paths.nodeProofPath,JSON.stringify(proof),{mode:0o600});calls.length=0;
 const input={sourceSHA,target,kubeconfig:'/synthetic/kubeconfig',...paths};
 return {input,events,plan,proof,node,container,calls,faults,write,imageRef,
  read:()=>readPersonalIntegrationsAuthorityImage(input,{operation})};
}

test('authority publication того же source049 требует fresh exact CRI без повторного pull',async t=>{
 const f=await fixture(t),proof=await f.read();assert.equal(proof.imageRef,f.imageRef);assert.equal(proof.sourceSHA,f.input.sourceSHA);
 assert.ok(f.calls.length>0);assert.equal(f.calls.some(call=>call.args.includes('pull')),false);
});

test('older source, missing publication ACK, digest mismatch и чужой node не становятся staged image',async t=>{
 for(const mutate of [f=>f.input.sourceSHA='e'.repeat(40),f=>{f.events.pop();f.write();},f=>f.faults.digest=true,
  f=>f.node.metadata.uid='10000000-0000-4000-8000-000000000999',f=>f.container.Config.Env=['K3S_PRIVATE_REGISTRY=/foreign']]){
  const f=await fixture(t);mutate(f);await assert.rejects(f.read);assert.equal(f.calls.some(call=>call.args.includes('pull')),false);
 }
});
