import test from 'node:test';
import assert from 'node:assert/strict';
import {telegramEgressRevisionRepairSpec} from './telegram-egress-revision-repair.mjs';

const image=digest=>'registry.local.kodex/kodex/egress-gateway@sha256:'+digest.repeat(64);
function fixture(){
 return {replicas:2,selector:{matchLabels:{app:'egress-gateway'}},template:{metadata:{labels:{app:'egress-gateway'}},spec:{
  serviceAccountName:'egress-gateway',volumes:[{name:'base-policy',configMap:{name:'synthetic-base'}}],
  containers:[{name:'egress-gateway',image:image('a'),env:[
   {name:'EGRESS_GATEWAY_EXPECTED_POLICY_REVISION',value:'release-synthetic'},
   {name:'EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST',value:'b'.repeat(64)},
   {name:'EGRESS_GATEWAY_TELEGRAM_POLICY_FILE',value:'/var/run/config/kodex/egress-gateway-telegram/policy.json'},
   {name:'EGRESS_GATEWAY_TELEGRAM_POLICY_DIGEST',value:'91bf766d2cb635882309927bbbc6e4d45d6b91998b88f436efbcfd59fd5520f0'}
  ],securityContext:{readOnlyRootFilesystem:true},readinessProbe:{httpGet:{path:'/readyz',port:'metrics'}}}]}}};
}

test('egress repair сохраняет base policy, probes, replicas, volumes и добавляет только отдельный pin/image',()=>{
 const before=fixture(),saved=structuredClone(before),after=telegramEgressRevisionRepairSpec(before,image('c'));
 const expected=structuredClone(before);expected.template.spec.containers[0].image=image('c');
 expected.template.spec.containers[0].env.push({name:'EGRESS_GATEWAY_TELEGRAM_POLICY_REVISION',value:'2026-09-05.1'});
 assert.deepEqual(after,expected);assert.deepEqual(before,saved);
});

test('egress repair отклоняет mutable/foreign/zero image и отсутствие реальной замены',()=>{
 for(const candidate of [image('a'),'registry.local.kodex/kodex/egress-gateway:latest',image('0'),image('c').replace('egress-gateway','control-plane')])
  assert.throws(()=>telegramEgressRevisionRepairSpec(fixture(),candidate),/REPAIR_REJECTED/);
});

test('egress repair отклоняет уже назначенный pin, duplicate env, valueFrom и чужой Telegram digest',()=>{
 for(const mutate of [
  e=>e.push({name:'EGRESS_GATEWAY_TELEGRAM_POLICY_REVISION',value:'2026-09-05.1'}),
  e=>e.push({...e[0]}),e=>{delete e[2].value;e[2].valueFrom={configMapKeyRef:{name:'foreign',key:'value'}};},
  e=>e[3].value='d'.repeat(64),e=>e.splice(0,1)
 ]){const before=fixture();mutate(before.template.spec.containers[0].env);assert.throws(()=>telegramEgressRevisionRepairSpec(before,image('c')),/REPAIR_REJECTED/);}
});

test('egress repair не принимает другой workload, дополнительные containers или остановленный predecessor',()=>{
 for(const mutate of [f=>f.replicas=0,f=>f.replicas=1.5,f=>f.template.spec.containers[0].name='foreign',
  f=>f.template.spec.containers.push(structuredClone(f.template.spec.containers[0]))]){
  const before=fixture();mutate(before);assert.throws(()=>telegramEgressRevisionRepairSpec(before,image('c')),/REPAIR_REJECTED/);
 }
});
