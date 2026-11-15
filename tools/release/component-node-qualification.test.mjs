import test from 'node:test';
import assert from 'node:assert/strict';
import {readComponentNodeTarget,qualifyComponentNodeImages} from './component-node-qualification.mjs';

const uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const image='registry.local.kodex/kodex/egress-gateway@sha256:'+'a'.repeat(64);
function fixture(){
 const target={context:'k3d-synthetic',clusterUID:uid(1),namespaceUID:uid(2),nodes:[{name:'k3d-synthetic-server-0',uid:uid(3)}]};
 const node={metadata:{name:target.nodes[0].name,uid:uid(3)},status:{conditions:[{type:'Ready',status:'True'},{type:'DiskPressure',status:'False'}]}};
 const container={Id:'b'.repeat(64),Image:'sha256:'+'c'.repeat(64),Name:'/'+node.metadata.name,State:{Running:true},
  Config:{Labels:{'k3d.cluster':'synthetic','k3d.role':'server'},Cmd:[],Env:[],Entrypoint:['/bin/k3s']}};
 const fault={},calls=[];
 const operation=async(program,args)=>{
  calls.push({program,args});
  if(program==='kubectl'){
   assert.deepEqual(args.slice(0,4),['--kubeconfig','/synthetic/kubeconfig','--context','k3d-synthetic']);
   if(args.includes('kube-system'))return JSON.stringify({metadata:{uid:fault.cluster?uid(90):target.clusterUID}});
   if(args.includes('kodex-system'))return JSON.stringify({metadata:{uid:target.namespaceUID}});
   return JSON.stringify({items:[node]});
  }
  assert.equal(program,'docker');
  if(args[0]==='inspect')return JSON.stringify([container]);
  assert.equal(args[1],container.Id);assert.equal(args.at(-1),image);
  if(args.includes('pull')){if(fault.lost)throw Error('LOST_ACK');return 'pulled';}
  if(fault.drift)node.metadata.uid=uid(99);
  return JSON.stringify({status:{id:'sha256:'+'d'.repeat(64),repoDigests:fault.digest?[]:[image]}});
 };
 return {target,node,container,fault,calls,operation,input:{target,kubeconfig:'/synthetic/kubeconfig',images:[image],mode:'qualify'}};
}

test('node qualify и readback используют exact target/CRI digest; readback не выполняет pull',async()=>{
 const f=fixture(),binding=await readComponentNodeTarget(f.target,f.input.kubeconfig,f.operation);
 const result=await qualifyComponentNodeImages({...f.input,expectedBinding:binding},f.operation);
 assert.deepEqual(result.images,[{imageRef:image,nodes:[{...f.target.nodes[0],imageRef:image,repoDigestVerified:true}]}]);
 const before=f.calls.filter(c=>c.args.includes('pull')).length;
 await qualifyComponentNodeImages({...f.input,mode:'readback'},f.operation);
 assert.equal(f.calls.filter(c=>c.args.includes('pull')).length,before);
});

test('foreign cluster, node replacement, DiskPressure и Docker runtime override отвергаются до pull',async()=>{
 for(const mutate of [f=>f.fault.cluster=true,f=>f.node.metadata.uid=uid(9),f=>f.node.status.conditions[1].status='True',
  f=>f.container.Config.Env=['K3S_PRIVATE_REGISTRY=/foreign'],f=>f.container.Config.Labels['k3d.cluster']='foreign']){
  const f=fixture();mutate(f);await assert.rejects(()=>qualifyComponentNodeImages(f.input,f.operation));
  assert.equal(f.calls.some(c=>c.args.includes('pull')),false);
 }
});

test('потерянный pull ACK, digest mismatch и drift после inspect не становятся доказательством допуска',async()=>{
 for(const key of ['lost','digest','drift']){const f=fixture();f.fault[key]=true;
  await assert.rejects(()=>qualifyComponentNodeImages(f.input,f.operation));assert.equal(f.calls.filter(c=>c.args.includes('pull')).length,1);
 }
});

test('чужой binding, mutable image и неизвестный mode отвергаются',async()=>{
 for(const change of [{expectedBinding:[]},{images:['example.invalid/image:latest']},{mode:'apply-anything'},{images:[image,image]}]){
  const f=fixture();await assert.rejects(()=>qualifyComponentNodeImages({...f.input,...change},f.operation));
  assert.equal(f.calls.some(c=>c.args.includes('pull')),false);
 }
});
