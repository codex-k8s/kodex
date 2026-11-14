import test from 'node:test';
import assert from 'node:assert/strict';
import {validKubernetesPortName,validateKubernetesPortNames} from './kubernetes-port-names.mjs';

test('IsValidPortName boundary совпадает с IANA validator, Service.name сохраняет DNS label semantics',()=>{
 for(const value of ['tg-connect','a','1a','a1','12345678901234a'])assert.equal(validKubernetesPortName(value),true,value);
 for(const value of ['',null,1,'123','-a','a-','a--b','A','a_b','telegram-connect','абв'])assert.equal(validKubernetesPortName(value),false,String(value));
 assert.doesNotThrow(()=>validateKubernetesPortNames([{kind:'Service',spec:{ports:[{name:'',targetPort:8086}]}},{kind:'Deployment',spec:{template:{spec:{containers:[{ports:[{name:'',containerPort:8086}]}]}}}}]));
 assert.doesNotThrow(()=>validateKubernetesPortNames([{kind:'Service',spec:{ports:[{name:'telegram-connect',targetPort:'tg-connect'}]}}]));
 for(const kind of ['Deployment','StatefulSet','DaemonSet','Job'])for(const section of ['containers','initContainers','ephemeralContainers']){
  const resource={kind,spec:{template:{spec:{[section]:[{ports:[{name:'telegram-connect'}]}]}}}};
  assert.throws(()=>validateKubernetesPortNames([resource]),/KUBERNETES_PORT_NAME_INVALID/);
 }
 assert.throws(()=>validateKubernetesPortNames([{kind:'Service',spec:{ports:[{targetPort:'telegram-connect'}]}}]),/KUBERNETES_PORT_NAME_INVALID/);
 assert.throws(()=>validateKubernetesPortNames([{kind:'Service',spec:{ports:[{name:'a'.repeat(64),targetPort:8086}]}}]));
});
