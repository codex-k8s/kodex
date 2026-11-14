import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,rmSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {spawnSync} from 'node:child_process';

test('native command bridge: exact kubeconfig, lineage, retained stop и неизвестный writer',()=>{
  const root=mkdtempSync(join(tmpdir(),'kodex-serving-stop-'));
  const uid='11111111-1111-4111-8111-111111111111';
  try {
    writeFileSync(join(root,'kubeconfig'),'synthetic',{mode:0o600});
    writeFileSync(join(root,'kubectl'),`#!/usr/bin/env node
const fs=require('node:fs'),p=require('node:path'),root=__dirname;
const a=process.argv.slice(2),x=JSON.parse(fs.readFileSync(p.join(root,'fixture.json')));
if(a[0]!=='--kubeconfig'||a[1]!==p.join(root,'kubeconfig')||a[2]!=='--context'||a[3]!=='synthetic')process.exit(30);
if(a.includes('deployment')){const f=p.join(root,'reads'),n=Number(fs.readFileSync(f,'utf8'));fs.writeFileSync(f,String(n+1));if(x.replacement&&n>0)x.deployment.metadata.uid='22222222-2222-4222-8222-222222222222';process.stdout.write(JSON.stringify(x.deployment));}
else if(a.includes('replicasets,pods,jobs'))process.stdout.write([x.rs,x.pod].map(r=>[r.kind,r.metadata.name,r.metadata.uid,r.metadata.labels?.['app.kubernetes.io/name']??'',r.metadata.namespace,JSON.stringify(r.metadata.ownerReferences)].join('\\t')).join('\\n')+'\\n');
else if(a.includes('pod'))process.stdout.write(JSON.stringify(x.pod));
else process.exit(31);
`,{mode:0o700});
    for(const mode of ['stopped','active','hidden','unknown','foreign','replicas','waiting','ephemeral','ready','wrong-image','mutated-command','extra-sidecar','spec-drift','replacement']) {
      const rs={kind:'ReplicaSet',metadata:{name:'runtime-controller-rs',uid:'rs',namespace:'kodex-system',ownerReferences:[{kind:'Deployment',name:'runtime-controller',uid,controller:true}]}};
      const pod={kind:'Pod',metadata:{name:'runtime-controller-pod',uid:'pod',namespace:'kodex-system',labels:{'app.kubernetes.io/name':mode==='hidden'?'':'runtime-controller'},ownerReferences:[{kind:'ReplicaSet',name:rs.metadata.name,uid:'rs',controller:true}]},spec:{containers:[{name:'app'}]},status:{phase:'Succeeded',containerStatuses:[{name:'app',state:{terminated:{exitCode:0}}}]}};
      if(['active','hidden'].includes(mode)){pod.status.phase='Running';pod.status.containerStatuses[0].state={running:{}};}
      if(mode==='unknown')pod.status.reason='NodeLost';
      if(mode==='waiting')pod.status.containerStatuses[0].state={waiting:{}};
      if(mode==='ephemeral')pod.spec.ephemeralContainers=[{name:'debug'}];
      const deployment={metadata:{uid:mode==='foreign'?'22222222-2222-4222-8222-222222222222':uid},spec:{replicas:mode==='replicas'?1:0}};
      const readyMode=['ready','wrong-image','mutated-command','extra-sidecar'].includes(mode);
      if(readyMode) {
        const image='images.example.invalid/controller@sha256:'+'a'.repeat(64);
        deployment.metadata.generation=1;deployment.spec.replicas=1;
        deployment.spec.template={spec:{containers:[{name:'app',image,command:['/controller']}]}};
        deployment.status={observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1};
        pod.spec=structuredClone(deployment.spec.template.spec);
        pod.status={phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:'app',ready:true,imageID:image,state:{running:{}}}]};
        if(mode==='wrong-image')pod.status.containerStatuses[0].imageID=image.replaceAll('a','b');
        if(mode==='mutated-command')pod.spec.containers[0].command=['/foreign'];
        if(mode==='extra-sidecar')pod.spec.containers.push({name:'unexpected',image});
      }
      writeFileSync(join(root,'fixture.json'),JSON.stringify({rs,pod,deployment,replacement:mode==='replacement'}));
      writeFileSync(join(root,'reads'),'0');
      const expected=structuredClone(deployment.spec);if(mode==='spec-drift')expected.replicas=5;
      const result=spawnSync(process.execPath,['tools/release/component-serving-readback.mjs',join(root,'kubeconfig'),'synthetic',uid,readyMode?'ready':'stopped'],{encoding:'utf8',input:JSON.stringify({spec:expected}),env:{PATH:root+':'+process.env.PATH},timeout:15000});
      if(['unknown','foreign','replicas','spec-drift','replacement'].includes(mode)){assert.equal(result.status,1,mode);assert.equal(result.stdout,'');}
      else {assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).confirmed,['stopped','ready'].includes(mode),mode);}
    }
  } finally {rmSync(root,{recursive:true,force:true});}
});
