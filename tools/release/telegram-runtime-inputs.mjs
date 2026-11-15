import {createHash} from 'node:crypto';
import {fingerprint,withPrivatePatchFile} from './scoped-release.mjs';

const namespace='kodex-system',workload='interaction-gateway';
const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b),copy=structuredClone;
const ownerLabel='app.kubernetes.io/part-of';
const inputAnnotation='kodex.dev/telegram-runtime-inputs';
const retainedSchema='kodex.telegram-runtime-inputs/v1',currentSchema='kodex.telegram-runtime-inputs/v2';
const revisionKey='EGRESS_GATEWAY_TELEGRAM_POLICY_REVISION';
const knownSchema=schema=>schema===retainedSchema||schema===currentSchema;
// Runtime digest канонизирует policy; raw source pin отдельно сохраняет bytes.
const digest='91bf766d2cb635882309927bbbc6e4d45d6b91998b88f436efbcfd59fd5520f0';
const policyRawSHA256='64d68357a69d84307354efb2463e64e7f5c1baf882294b4ec0856b0a431ad88f';
const uid=value=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value??'');
const policyNames=Object.freeze(['egress-gateway-exact-runtime-paths','platform-postgresql-exact-clients',
 'control-plane-exact-runtime-paths','internal-rpc-authority-readback-attestor-exact-paths','internal-rpc-authority-restore-controller-exact-paths']);
const secretNames=Object.freeze(['issuer-readback-credential','issuer-readback-possession','issuer-restore-credential','issuer-restore-ack',
 'issuer-key','manifest-trust','proof-trust'].map(suffix=>`internal-rpc-authority-${workload}-${suffix}`));
const servicePort=name=>({name,port:8086,targetPort:name,protocol:'TCP'});
const peer={namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':namespace}},podSelector:{matchLabels:{
 'app.kubernetes.io/name':workload,'app.kubernetes.io/component':'interaction-adapter'}}};
const ingress={from:[peer],ports:[{protocol:'TCP',port:8086}]};
const cpPeer={podSelector:{matchLabels:{'app.kubernetes.io/name':workload}}};
const identity=value=>`${value.apiVersion}|${value.kind}|${value.metadata.name}`;
const one=(values,predicate)=>{const matches=values.filter(predicate);fail(matches.length===1,'TELEGRAM_RUNTIME_SOURCE_CLOSURE_INVALID');return matches[0];};
const named=(values,name)=>one(values??[],v=>v.name===name);
const asset=(resources,kind,name)=>one(resources,r=>r.kind===kind&&r.metadata?.name===name&&r.metadata.namespace===namespace);

// Из source выбираются только уже реализованные inputs. Новые destinations,
// env/volumes, Secret names, RBAC verbs либо произвольные kinds не принимаются.
export function telegramRuntimeInputProjection(resources){return runtimeInputProjection(resources,currentSchema);}

// Версию выбирает immutable install receipt, а не наличие env. Fresh producer
// всегда v2; v1 читает ровно прежнюю source projection и не допускает новый pin.
function runtimeInputProjection(resources,schema){
 fail(knownSchema(schema),'TELEGRAM_RUNTIME_INPUT_SCHEMA_INVALID');
 const cp=named(asset(resources,'Deployment','control-plane').spec.template.spec.containers,'control-plane');
 const egSpec=asset(resources,'Deployment','egress-gateway').spec.template.spec,eg=named(egSpec.containers,'egress-gateway');
 const cpSpec=asset(resources,'Deployment','control-plane').spec.template.spec;
 const cpEnv=['INTERACTION_PUBLIC_ORIGIN','CONTROL_PLANE_INTERACTION_GRANT_TRUST_FILE'].map(name=>copy(named(cp.env,name)));
 fail(same(cpEnv,[{name:'INTERACTION_PUBLIC_ORIGIN',valueFrom:{configMapKeyRef:{name:'kodex-platform-endpoints',key:'publicOrigin'}}},
  {name:'CONTROL_PLANE_INTERACTION_GRANT_TRUST_FILE',value:'/var/run/config/kodex/control-plane/interaction-grant-trust/interaction-gateway.platform-worker.public.jwk'}]),
  'TELEGRAM_RUNTIME_ORIGIN_OR_TRUST_INVALID');
 const cpMount=copy(named(cp.volumeMounts,'interaction-grant-trust')),cpVolume=copy(named(cpSpec.volumes,'interaction-grant-trust'));
 fail(same(cpMount,{name:'interaction-grant-trust',mountPath:'/var/run/config/kodex/control-plane/interaction-grant-trust',readOnly:true})&&
  same(cpVolume,{name:'interaction-grant-trust',secret:{secretName:'control-plane-interaction-grant-trust',defaultMode:292}}),
  'TELEGRAM_RUNTIME_ORIGIN_OR_TRUST_INVALID');
 const current=schema===currentSchema;
 fail(current||!eg.env?.some(e=>e.name===revisionKey),'TELEGRAM_RUNTIME_POLICY_INPUT_INVALID');
 const egEnv=['EGRESS_GATEWAY_TELEGRAM_CONNECT_LISTEN','EGRESS_GATEWAY_TELEGRAM_POLICY_FILE',...(current?[revisionKey]:[]),'EGRESS_GATEWAY_TELEGRAM_POLICY_DIGEST'].map(name=>copy(named(eg.env,name)));
 fail(same(egEnv,[{name:'EGRESS_GATEWAY_TELEGRAM_CONNECT_LISTEN',value:':8086'},
  {name:'EGRESS_GATEWAY_TELEGRAM_POLICY_FILE',value:'/var/run/config/kodex/egress-gateway-telegram/policy.json'},
  ...(current?[{name:revisionKey,value:'2026-09-05.1'}]:[]),
  {name:'EGRESS_GATEWAY_TELEGRAM_POLICY_DIGEST',value:digest}]),'TELEGRAM_RUNTIME_POLICY_INPUT_INVALID');
 const egMount=copy(named(eg.volumeMounts,'telegram-policy')),egVolume=copy(named(egSpec.volumes,'telegram-policy'));
 fail(same(egMount,{name:'telegram-policy',mountPath:'/var/run/config/kodex/egress-gateway-telegram',readOnly:true})&&
  /^egress-gateway-telegram-policy-[a-z0-9]{10}$/.test(egVolume.configMap?.name??'')&&
  same(egVolume,{name:'telegram-policy',configMap:{name:egVolume.configMap.name,defaultMode:292}}),
  'TELEGRAM_RUNTIME_POLICY_INPUT_INVALID');
 const policy=copy(asset(resources,'ConfigMap',egVolume.configMap.name));
 fail(policy.immutable===true&&Object.keys(policy.data??{}).join(',')==='policy.json'&&!policy.binaryData&&
  createHash('sha256').update(policy.data['policy.json']).digest('hex')===policyRawSHA256,'TELEGRAM_RUNTIME_POLICY_SOURCE_INVALID');
 const selected=eg.ports?.filter(value=>['tg-connect','telegram-connect'].includes(value.name));
 fail(selected?.length===1,'TELEGRAM_RUNTIME_PORT_INVALID');
 const selectedName=selected[0].name;
 fail(same(named(eg.ports,selectedName),{name:selectedName,containerPort:8086,protocol:'TCP'})&&
  same(named(asset(resources,'Service','egress-gateway').spec.ports,selectedName),servicePort(selectedName)),
  'TELEGRAM_RUNTIME_PORT_INVALID');
 const egPolicy=asset(resources,'NetworkPolicy',policyNames[0]);
 fail(egPolicy.spec.ingress.some(rule=>same(rule,ingress)),'TELEGRAM_RUNTIME_NETWORK_SOURCE_INVALID');
 const sourceRole=asset(resources,'Role','internal-rpc-authority-publisher');
 fail(sourceRole.rules.length===1&&same(sourceRole.rules[0].apiGroups,[''])&&same(sourceRole.rules[0].resources,['secrets'])&&
  same(sourceRole.rules[0].verbs,['get','update'])&&secretNames.every(name=>sourceRole.rules[0].resourceNames.includes(name)),
  'TELEGRAM_RUNTIME_PUBLISHER_SOURCE_INVALID');
 return {cp:{env:cpEnv,volumeMount:cpMount,volume:cpVolume},egress:{env:egEnv,volumeMount:egMount,volume:egVolume,
  port:copy(named(eg.ports,selectedName))},policy,networkSources:policyNames.map(name=>copy(asset(resources,'NetworkPolicy',name)))};
}

function append(values,value,key){
 fail(Array.isArray(values),'TELEGRAM_RUNTIME_PREDECESSOR_INVALID');
 const matches=values.filter(v=>key?v[key]===value[key]:same(v,value));
 fail(matches.length<=1&&(!matches.length||same(matches[0],value)),'TELEGRAM_RUNTIME_INPUT_CONFLICT');
 if(!matches.length)values.push(copy(value));
}
// Старые provider/mail/runtime projections и все иные поля spec остаются
// точными. Конфликтующий прежний input требует отдельного согласованного пути.
export function telegramDeploymentInputs(before,name,projection){
 fail(['control-plane','egress-gateway'].includes(name),'TELEGRAM_RUNTIME_DEPLOYMENT_INVALID');
 const result=copy(before),pod=result.template.spec,container=named(pod.containers,name),p=name==='control-plane'?projection.cp:projection.egress;
 container.env??=[];container.volumeMounts??=[];pod.volumes??=[];
 for(const value of p.env)append(container.env,value,'name');
 append(container.volumeMounts,p.volumeMount,'name');append(pod.volumes,p.volume,'name');
 if(name==='egress-gateway'){container.ports??=[];append(container.ports,p.port,'name');}
 return result;
}
function owned(resource,kind,name){
 fail(resource?.kind===kind&&resource.metadata?.name===name&&resource.metadata.namespace===namespace&&
  resource.metadata.labels?.[ownerLabel]==='kodex'&&uid(resource.metadata.uid)&&/^\d+$/.test(resource.metadata.resourceVersion??'')&&
  !resource.metadata.deletionTimestamp,'TELEGRAM_RUNTIME_INPUT_TARGET_CHANGED');return resource;
}
function networkSpec(before,name,projection){
 const result=copy(before),source=named(projection.networkSources.map(r=>({name:r.metadata.name,spec:r.spec})),name).spec;
 fail(same(result.podSelector,source.podSelector)&&same(result.policyTypes,source.policyTypes)&&Array.isArray(result.ingress),
  'TELEGRAM_RUNTIME_NETWORK_TARGET_CHANGED');
 if(name===policyNames[0])append(result.ingress,ingress);
 else{
  fail(same(result.ingress[0]?.ports,source.ingress[0]?.ports),'TELEGRAM_RUNTIME_NETWORK_TARGET_CHANGED');
  if(name==='control-plane-exact-runtime-paths')append(result.ingress[0].from,cpPeer);
  else{
   const expression=one(result.ingress[0]?.from?.[0]?.podSelector?.matchExpressions??[],e=>e.key==='app.kubernetes.io/name'&&e.operator==='In');
   fail(Array.isArray(expression.values)&&expression.values.every(v=>typeof v==='string')&&new Set(expression.values).size===expression.values.length,
    'TELEGRAM_RUNTIME_NETWORK_TARGET_CHANGED');
   if(!expression.values.includes(workload))expression.values.push(workload);
  }
 }
 return result;
}

// Plan содержит original snapshots и закрытую source projection, а не
// caller patch. UID/runtime values не выводятся из rendered placeholders.
export function planTelegramRuntimeInputs(input){return planRuntimeInputs(input,currentSchema);}

// Rebuild сохранённого receipt сохраняет его исходную версию и весь snapshot.
// Эта внутренняя ветвь не является producer старой конфигурации.
function planRuntimeInputs({resources,get,installation,sourceSHA,migrationID},schema){
 const projection=runtimeInputProjection(resources,schema);
 fail(/^[a-f0-9]{40}$/.test(sourceSHA??'')&&uid(migrationID)&&installation?.metadata?.name==='kodex-installation-profile'&&
  uid(installation.metadata.uid)&&installation.metadata.namespace===namespace&&installation.metadata.labels?.[ownerLabel]==='kodex',
  'TELEGRAM_RUNTIME_INPUT_SCOPE_INVALID');
 const endpoints=owned(get('ConfigMap','kodex-platform-endpoints'),'ConfigMap','kodex-platform-endpoints');
 let origin;try{origin=new URL(endpoints.data?.publicOrigin);}catch{throw Error('TELEGRAM_RUNTIME_PUBLIC_ORIGIN_INVALID');}
 fail(origin.protocol==='https:'&&!origin.username&&!origin.password&&origin.origin===endpoints.data.publicOrigin,
  'TELEGRAM_RUNTIME_PUBLIC_ORIGIN_INVALID');
 const patches=policyNames.map(name=>{const before=copy(owned(get('NetworkPolicy',name),'NetworkPolicy',name));
  return {before,field:'spec',after:networkSpec(before.spec,name,projection)};});
 const service=copy(owned(get('Service','egress-gateway'),'Service','egress-gateway'));
 const serviceSpec=copy(service.spec);fail(serviceSpec.type==='ClusterIP'&&!serviceSpec.externalName&&!serviceSpec.externalIPs?.length,
  'TELEGRAM_RUNTIME_SERVICE_TARGET_INVALID');append(serviceSpec.ports,servicePort(projection.egress.port.name),'name');patches.push({before:service,field:'spec',after:serviceSpec});
 const role=copy(owned(get('Role','internal-rpc-authority-publisher'),'Role','internal-rpc-authority-publisher')),rules=copy(role.rules);
 fail(rules.length===1&&same(rules[0].apiGroups,[''])&&same(rules[0].resources,['secrets'])&&same(rules[0].verbs,['get','update'])&&
  Array.isArray(rules[0].resourceNames)&&rules[0].resourceNames.every(name=>typeof name==='string'&&name!=='*'),
  'TELEGRAM_RUNTIME_PUBLISHER_TARGET_INVALID');
 for(const name of secretNames)if(!rules[0].resourceNames.includes(name))rules[0].resourceNames.push(name);
 patches.push({before:role,field:'rules',after:rules});
 const policy=copy(projection.policy);
 policy.metadata.ownerReferences=[{apiVersion:'v1',kind:'ConfigMap',name:installation.metadata.name,uid:installation.metadata.uid}];
 policy.metadata.annotations={...(policy.metadata.annotations??{}),[inputAnnotation]:JSON.stringify({migrationID,sourceSHA})};
 fail(!get('ConfigMap',policy.metadata.name),'TELEGRAM_RUNTIME_POLICY_ALREADY_EXISTS');
 return {schema,sourceSHA,migrationID,installationUID:installation.metadata.uid,
  installationSHA256:fingerprint(installation.data),endpoints:{uid:endpoints.metadata.uid,dataSHA256:fingerprint(endpoints.data)},projection,policy,patches};
}

// Fresh projection и immutable original snapshots восстанавливают весь план:
// сохранённые after/rules или replacement ConfigMap не становятся authority.
export function verifyTelegramRuntimeInputs(plan,resources,installation,endpoints){
 fail(knownSchema(plan?.schema)&&same(runtimeInputProjection(resources,plan.schema),plan.projection)&&
  installation.metadata.uid===plan.installationUID&&fingerprint(installation.data)===plan.installationSHA256&&
  owned(endpoints,'ConfigMap','kodex-platform-endpoints').metadata.uid===plan.endpoints.uid&&fingerprint(endpoints.data)===plan.endpoints.dataSHA256,
  'TELEGRAM_RUNTIME_INPUT_SOURCE_CHANGED');
 const snapshots=new Map(plan.patches.map(p=>[identity(p.before),p.before]));
 const get=(kind,name)=>kind==='ConfigMap'?(name==='kodex-platform-endpoints'?endpoints:null):
  [...snapshots.values()].find(r=>r.kind===kind&&r.metadata.name===name);
 const rebuilt=planRuntimeInputs({resources,get,installation,sourceSHA:plan.sourceSHA,migrationID:plan.migrationID},plan.schema);
 fail(same(rebuilt,plan),'TELEGRAM_RUNTIME_INPUT_PLAN_CHANGED');
}

// Journal общий с schema handoff. Каждый network/RBAC effect имеет собственный
// fsync INTENT; lost ACK rejoin читает exact UID/body и не повторяет PATCH/create.
export function applyTelegramRuntimeInputs({plan,journal,kube,gate,readOnly=false,requireStaged=true}){
 fail(knownSchema(plan?.schema),'TELEGRAM_RUNTIME_INPUT_SCHEMA_INVALID');
 fail(!journal.events.some(e=>e.type==='TELEGRAM_INPUT_CREATE_ACK_INVALID'),'TELEGRAM_RUNTIME_POLICY_ACK_INVALID');
 const get=(kind,name)=>{const raw=kube(['-n',namespace,'get',kind.toLowerCase(),name,'--ignore-not-found=true','-o','json']);return raw.trim()?JSON.parse(raw):null;};
 const policy=plan.policy,key=identity(policy),existing=get('ConfigMap',policy.metadata.name);
 const intent=journal.events.some(e=>e.type==='TELEGRAM_INPUT_CREATE_INTENT'&&e.resource===key);
 const projection=resource=>{
  const value=copy(resource);delete value.status;
  for(const field of ['uid','resourceVersion','creationTimestamp','generation','managedFields'])delete value.metadata[field];
  return value;
 };
 if(existing){
  fail(intent&&uid(existing.metadata.uid)&&!existing.metadata.deletionTimestamp&&same(projection(existing),projection(policy)),
   'TELEGRAM_RUNTIME_POLICY_REPLACED_OR_FOREIGN');
  const assigned=journal.events.find(e=>e.type==='TELEGRAM_INPUT_CREATED'&&e.resource===key),ack=journal.events.findLast(e=>e.type==='TELEGRAM_INPUT_CREATE_ACK'&&e.resource===key);
  fail((!assigned||assigned.uid===existing.metadata.uid)&&(!ack||ack.uid===existing.metadata.uid&&ack.resourceSHA256===fingerprint(projection(existing))),
   'TELEGRAM_RUNTIME_POLICY_REPLACED_OR_FOREIGN');
 }else{
  fail(!readOnly,'TELEGRAM_RUNTIME_INPUTS_NOT_STAGED');
  fail(!intent,'TELEGRAM_RUNTIME_POLICY_MISSING_AFTER_INTENT');gate();
  const dry=JSON.parse(kube(['create','--dry-run=server','-f','-','-o','json'],JSON.stringify(policy)));
  fail(same(projection(dry),projection(policy)),'TELEGRAM_RUNTIME_POLICY_ADMISSION_CHANGED');
  journal.append({type:'TELEGRAM_INPUT_CREATE_INTENT',resource:key});
  let acknowledged;
  try{acknowledged=kube(['create','-f','-','-o','json'],JSON.stringify(policy));}catch(error){journal.append({type:'UNKNOWN',step:'TELEGRAM_INPUT_CREATE'});throw error;}
  let acknowledgement;
  try{
   acknowledgement=JSON.parse(acknowledged);
   fail(uid(acknowledgement?.metadata?.uid)&&/^\d+$/.test(acknowledgement.metadata.resourceVersion??'')&&
    !acknowledgement.metadata.deletionTimestamp&&same(projection(acknowledgement),projection(policy)),'TELEGRAM_RUNTIME_POLICY_ACK_INVALID');
  }catch{journal.append({type:'TELEGRAM_INPUT_CREATE_ACK_INVALID',resource:key});throw Error('TELEGRAM_RUNTIME_POLICY_ACK_INVALID');}
  journal.append({type:'TELEGRAM_INPUT_CREATE_ACK',resource:key,uid:acknowledgement.metadata.uid,resourceSHA256:fingerprint(projection(acknowledgement))});
 }
 const created=get('ConfigMap',policy.metadata.name);
 fail(created&&uid(created.metadata.uid)&&!created.metadata.deletionTimestamp&&same(projection(created),projection(policy)),
  'TELEGRAM_RUNTIME_POLICY_UNCONFIRMED');
 const ack=journal.events.findLast(e=>e.type==='TELEGRAM_INPUT_CREATE_ACK'&&e.resource===key);
 fail(!ack||ack.uid===created.metadata.uid&&ack.resourceSHA256===fingerprint(projection(created)),'TELEGRAM_RUNTIME_POLICY_REPLACED_OR_FOREIGN');
 if(!readOnly||!requireStaged)journal.append({type:'TELEGRAM_INPUT_CREATED',resource:key,uid:created.metadata.uid});
 let pending=false;
 for(const part of plan.patches){
  const original=part.before,resource=identity(original),actual=owned(get(original.kind,original.metadata.name),original.kind,original.metadata.name);
  fail(actual.metadata.uid===original.metadata.uid,'TELEGRAM_RUNTIME_INPUT_UID_CHANGED');
  const before=same(actual[part.field],original[part.field]),after=same(actual[part.field],part.after);
  const last=journal.events.findLastIndex(e=>e.type==='TELEGRAM_INPUT_PATCH_INTENT'&&e.resource===resource);
  fail(before||after,'TELEGRAM_RUNTIME_INPUT_DRIFT');
  if(after){fail(before||last>=0,'TELEGRAM_RUNTIME_INPUT_UNOWNED_STATE');if(!readOnly)journal.append({type:'TELEGRAM_INPUT_PATCH_ACK',resource,uid:actual.metadata.uid});continue;}
  if(readOnly){
   fail(!requireStaged,'TELEGRAM_RUNTIME_INPUTS_NOT_STAGED');pending=true;
   journal.append({type:'TELEGRAM_INPUT_OBSERVED',resource,state:'BEFORE',uid:actual.metadata.uid,resourceVersion:actual.metadata.resourceVersion});continue;
  }
  // Повтор первоначального before после UNKNOWN требует отдельного inspect;
  // автоматического повторного patch после неполученного ACK здесь нет.
  fail(actual.metadata.resourceVersion===original.metadata.resourceVersion&&(last<0||journal.events.some((event,index)=>index>last&&
    event.type==='TELEGRAM_INPUT_OBSERVED'&&event.state==='BEFORE'&&event.resource===resource&&event.uid===actual.metadata.uid&&
    event.resourceVersion===actual.metadata.resourceVersion)),'TELEGRAM_RUNTIME_INPUT_OUTCOME_UNKNOWN');gate();
  const patch=[{op:'test',path:'/metadata/uid',value:original.metadata.uid},{op:'test',path:'/metadata/resourceVersion',value:actual.metadata.resourceVersion},
   {op:'test',path:`/${part.field}`,value:original[part.field]},{op:'replace',path:`/${part.field}`,value:part.after}];
  withPrivatePatchFile(patch,path=>{
   const args=['-n',namespace,'patch',original.kind.toLowerCase(),original.metadata.name,'--type=json',`--patch-file=${path}`];
   kube([...args,'--dry-run=server']);journal.append({type:'TELEGRAM_INPUT_PATCH_INTENT',resource});
   try{kube(args);}catch(error){journal.append({type:'UNKNOWN',step:'TELEGRAM_INPUT_PATCH',resource});throw error;}
  });
  const readback=owned(get(original.kind,original.metadata.name),original.kind,original.metadata.name);
  fail(readback.metadata.uid===original.metadata.uid&&same(readback[part.field],part.after),'TELEGRAM_RUNTIME_INPUT_PATCH_UNCONFIRMED');
  journal.append({type:'TELEGRAM_INPUT_PATCH_ACK',resource,uid:readback.metadata.uid});
 }
 return {state:pending?'INPUTS_PARTIAL':'INPUTS_STAGED',policyUID:created.metadata.uid};
}
