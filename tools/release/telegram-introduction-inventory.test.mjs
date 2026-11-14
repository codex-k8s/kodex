import test from 'node:test';
import assert from 'node:assert/strict';
import producerMetadata from './fixtures/telegram-producer-metadata.cjs';
import {telegramIdentitySecretNames,telegramIntroductionResources,telegramIntroductionMetadataQuery,parseTelegramIntroductionInventory,readTelegramIntroductionInventory} from './telegram-introduction-inventory.mjs';
import {requireTelegramDormantInventory} from './telegram-resource-create.mjs';

const uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const object=(kind,name)=>({kind,metadata:{name,uid:uid(2),namespace:'kodex-system',generation:1},spec:{replicas:0},status:{replicas:0,observedGeneration:1}});

test('все встроенные pending producers со скрытым SA/template отклоняются без Pod и без label selector',()=>{
 const expected='deployments,statefulsets,daemonsets,replicasets,replicationcontrollers,jobs,cronjobs,pods';
 assert.equal(telegramIntroductionResources,expected);
 for(const kind of ['Deployment','StatefulSet','DaemonSet','ReplicaSet','ReplicationController','Job','CronJob','Pod'])for(const mode of ['serviceAccount','templateLabel']){
  const foreign=object(kind,'foreign-producer');
  if(kind==='Pod'){
   if(mode==='serviceAccount')foreign.spec.serviceAccountName='interaction-gateway';
   else foreign.metadata.labels={'app.kubernetes.io/name':'interaction-gateway'};
  }else{
   const template={metadata:{labels:{}},spec:{}};
   if(mode==='serviceAccount')template.spec.serviceAccountName='interaction-gateway';
   else template.metadata.labels['app.kubernetes.io/name']='interaction-gateway';
   if(kind==='CronJob')foreign.spec.jobTemplate={spec:{template}};else foreign.spec.template=template;
  }
  const calls=[],kube=args=>{calls.push(args);return producerMetadata([foreign],args[args.indexOf('get')+1]);};
  const inventory=readTelegramIntroductionInventory(kube);assert.equal(inventory.items.length,1);
  assert.deepEqual(calls,[['-n','kodex-system','get',expected,'-o',telegramIntroductionMetadataQuery]]);
  assert.equal(calls[0].includes('-l'),false);
  assert.throws(()=>requireTelegramDormantInventory(inventory,uid(1)),/TELEGRAM_CREATE_ORPHAN_WORKLOAD_PRESENT/);
 }
});

test('assigned Deployment0 и собственный RS0 требуют exact UID, generation/observed и отсутствие Pod',()=>{
 const deployment=object('Deployment','interaction-gateway');deployment.metadata.uid=uid(1);
 const set=object('ReplicaSet','interaction-gateway-rs');set.metadata.ownerReferences=[{kind:'Deployment',controller:true,name:'interaction-gateway',uid:uid(1)}];
 const read=items=>parseTelegramIntroductionInventory(producerMetadata(items,telegramIntroductionResources));
 assert.doesNotThrow(()=>requireTelegramDormantInventory(read([deployment,set]),uid(1)));
 for(const mutate of [x=>x[0].metadata.uid=uid(99),x=>x[0].spec.replicas=1,x=>x[1].status.replicas=1,
  x=>x[1].metadata.ownerReferences[0].uid=uid(99),x=>x[1].status.observedGeneration=0,x=>x[0].metadata.generation=0,
  x=>x.push({...object('Pod','interaction-gateway-unexpected'),spec:{serviceAccountName:'interaction-gateway'}})]){
  const items=structuredClone([deployment,set]);mutate(items);assert.throws(()=>requireTelegramDormantInventory(read(items),uid(1)),/ORPHAN_WORKLOAD_PRESENT/);
 }
 assert.throws(()=>requireTelegramDormantInventory(read([deployment,set])),/ORPHAN_WORKLOAD_PRESENT/);
});

test('metadata wire сохраняет пустые TSV columns; неизвестные/дублированные/повреждённые строки закрыты',()=>{
 const pod={kind:'Pod',metadata:{name:'unrelated',uid:uid(2),namespace:'kodex-system'},spec:{}};
 const raw=producerMetadata([pod],telegramIntroductionResources);
 assert.equal(raw.split('\n')[0].split('\t').length,20);assert.equal(parseTelegramIntroductionInventory(raw).items.length,1);
 assert.throws(()=>parseTelegramIntroductionInventory(raw.trim()),/INVENTORY_INVALID/);
 for(const invalid of [raw+raw,raw.replace('kodex-system','foreign'),raw.replace('Pod','ForeignKind'),raw+'\r',raw.replace('[]','{bad')])
  assert.throws(()=>parseTelegramIntroductionInventory(invalid),/INVENTORY_INVALID/);
 assert.deepEqual(parseTelegramIntroductionInventory(''),{items:[]});
});

test('чужие pending templates с прямыми Secret refs отклоняются при другом SA и labels',()=>{
 for(const kind of ['Deployment','StatefulSet','DaemonSet','ReplicaSet','ReplicationController','Job','CronJob','Pod'])for(const secretName of telegramIdentitySecretNames){
  const object={kind,metadata:{name:'foreign',uid:uid(90),namespace:'kodex-system',generation:1},spec:{}};
  const pod={serviceAccountName:'foreign-account',containers:[{name:'unrelated',env:[{name:'IGNORED_LITERAL',value:'THIS_VALUE_MUST_NOT_ENTER_METADATA'},
   {name:'CREDENTIAL',valueFrom:{secretKeyRef:{name:secretName,key:'key'}}}]}]};
  if(kind==='Pod')object.spec=pod;else if(kind==='CronJob')object.spec={jobTemplate:{spec:{template:{spec:pod}}}};else object.spec={template:{spec:pod}};
  const raw=producerMetadata([object],telegramIntroductionResources);assert.doesNotMatch(raw,/THIS_VALUE_MUST_NOT_ENTER_METADATA|IGNORED_LITERAL/);
  assert.throws(()=>requireTelegramDormantInventory(parseTelegramIntroductionInventory(raw),uid(1)),/ORPHAN_WORKLOAD_PRESENT/);
 }
 for(const ref of [{volumes:[{name:'x',secret:{secretName:telegramIdentitySecretNames[0]}}]},
  {volumes:[{name:'x',projected:{sources:[{secret:{name:telegramIdentitySecretNames[0]}}]}}]},
  {initContainers:[{name:'init',envFrom:[{secretRef:{name:telegramIdentitySecretNames[0]}}]}]},
  {ephemeralContainers:[{name:'debug',envFrom:[{secretRef:{name:telegramIdentitySecretNames[0]}}]}]},
  {imagePullSecrets:[{name:telegramIdentitySecretNames[0]}]}]){
  const pod={kind:'Pod',metadata:{name:'foreign',namespace:'kodex-system',uid:uid(90)},spec:ref};
  assert.throws(()=>requireTelegramDormantInventory(parseTelegramIntroductionInventory(producerMetadata([pod],telegramIntroductionResources)),uid(1)),/ORPHAN_WORKLOAD_PRESENT/);
 }
});
