import test from 'node:test';
import assert from 'node:assert/strict';
import {telegramCreateSourceFixture} from './fixtures/telegram-introduction-synthetic.mjs';
import {projectTelegramRuntimeObservability,telegramPublisherRuntimeObservability,requireTelegramRuntimeObservability} from './telegram-runtime-observability.mjs';

const deployment=()=>telegramCreateSourceFixture().find(r=>r.kind==='Deployment');
const publisher=()=>({apiVersion:'apps/v1',kind:'Deployment',metadata:{name:'internal-rpc-authority-publisher',namespace:'kodex-system'},
 spec:{template:{spec:{containers:[{name:'publisher',image:'registry.local.kodex/kodex/internal-rpc-authority@sha256:'+'a'.repeat(64),env:[{name:'OTEL_SDK_DISABLED',value:'true'}]}]}}}});

test('canonical Python добавляет disabled mode только трём long-running, сохраняя one-shot init и весь остальной spec',()=>{
 const before=deployment();
 before.spec.template.spec.initContainers[0].env=[{name:'ONE_SHOT',value:'unchanged'}];
 for(const container of before.spec.template.spec.containers){container.image='synthetic-image';container.env=[{name:'UNCHANGED',value:'same'}];}
 const expected=structuredClone(before);
 for(const container of expected.spec.template.spec.containers)container.env.push({name:'OTEL_SDK_DISABLED',value:'true'});
 assert.deepEqual(projectTelegramRuntimeObservability(before,'DISABLED'),expected);
 assert.deepEqual(projectTelegramRuntimeObservability(before,'ENABLED'),before);
 assert.deepEqual(projectTelegramRuntimeObservability(expected,'DISABLED'),expected);
 assert.equal(before.spec.template.spec.containers[0].env.length,1);
});

test('explicit publisher profile проверяется повторно, restart annotations не меняют runtime proof',()=>{
 const value=publisher(),profile=telegramPublisherRuntimeObservability(value,'DISABLED');
 assert.deepEqual(requireTelegramRuntimeObservability(profile,value),profile);
 value.spec.template.metadata={annotations:{'kubectl.kubernetes.io/restartedAt':'2026-11-15T00:00:00Z'}};
 assert.deepEqual(requireTelegramRuntimeObservability(profile,value),profile);
 for(const mutate of [v=>v.spec.template.spec.containers[0].image+='foreign',v=>v.spec.template.spec.containers[0].env=[],
  v=>v.spec.template.spec.containers[0].env.push({name:'OTEL_SDK_DISABLED',value:'true'}),
  v=>v.spec.template.spec.containers[0].env[0]={name:'OTEL_SDK_DISABLED',valueFrom:{secretKeyRef:{name:'foreign',key:'mode'}}}]){
  const changed=publisher();mutate(changed);assert.throws(()=>requireTelegramRuntimeObservability(profile,changed),/RUNTIME_OBSERVABILITY_REJECTED/);
 }
 assert.throws(()=>telegramPublisherRuntimeObservability(publisher(),'ENABLED'),/REJECTED/);
 assert.throws(()=>requireTelegramRuntimeObservability({...profile,extra:true},publisher()),/REJECTED/);
});

test('unknown mode, foreign Deployment и лишние long-running containers закрыты до renderer',()=>{
 for(const mode of [undefined,'disabled','UNKNOWN'])assert.throws(()=>projectTelegramRuntimeObservability(deployment(),mode),/REJECTED/);
 for(const mutate of [v=>v.metadata.name='foreign',v=>v.spec.template.spec.containers.push({name:'foreign'}),
  v=>v.spec.template.spec.initContainers.push({name:'foreign',restartPolicy:'Always'}),
  v=>v.spec.template.spec.containers[0].env=[{name:'OTEL_SDK_DISABLED',value:'true'},{name:'OTEL_SDK_DISABLED',value:'false'}]]){
  const value=deployment();mutate(value);assert.throws(()=>projectTelegramRuntimeObservability(value,'DISABLED'),/REJECTED/);
 }
});
