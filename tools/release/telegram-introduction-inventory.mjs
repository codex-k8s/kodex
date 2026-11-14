// Полный встроенный producer registry: pending template тоже несёт workload
// identity, хотя ещё не создал Pod. CRD этим закрытым профилем не добавляются.
export const telegramIntroductionResources='deployments,statefulsets,daemonsets,replicasets,replicationcontrollers,jobs,cronjobs,pods';
const kinds=new Set(['Deployment','StatefulSet','DaemonSet','ReplicaSet','ReplicationController','Job','CronJob','Pod']);
// Имена identity credentials, без shared provider/telemetry Secrets и без их
// значений. Чужой producer с прямой ссылкой тоже может получить identity.
export const telegramIdentitySecretNames=Object.freeze(['interaction-gateway-platform-worker-grant-signer',
 ...['manifest-trust','proof-trust','issuer-key','workload-tls','issuer-postgresql','issuer-readback-credential',
 'issuer-readback-possession','issuer-restore-credential','issuer-restore-ack'].map(suffix=>'internal-rpc-authority-interaction-gateway-'+suffix)]);

const paths=['.kind','.metadata.name','.metadata.uid','.metadata.namespace','.metadata.generation',
 '.metadata.labels.app\\.kubernetes\\.io/name','.metadata.ownerReferences','.spec.serviceAccountName',
 '.spec.template.spec.serviceAccountName','.spec.jobTemplate.spec.template.spec.serviceAccountName',
 '.spec.template.metadata.labels.app\\.kubernetes\\.io/name','.spec.jobTemplate.spec.template.metadata.labels.app\\.kubernetes\\.io/name',
 '.spec.replicas','.status.replicas','.status.observedGeneration',
 '.spec..secretName','.spec..secretKeyRef.name','.spec..secretRef.name','.spec..secret.name','.spec..imagePullSecrets[*].name','.spec..nodePublishSecretRef.name'];
export const telegramIntroductionMetadataQuery='jsonpath={range .items[*]}'+paths.map(path=>`{${path}}`).join('{"\\t"}')+'{"\\n"}{end}';
const fail=ok=>{if(!ok)throw Error('TELEGRAM_INTRODUCTION_INVENTORY_INVALID');};
const number=value=>{if(value==='')return undefined;fail(/^(0|[1-9][0-9]*)$/.test(value)&&Number.isSafeInteger(Number(value)));return Number(value);};

// Только identity/template metadata, без env/commands/Secret payload. Пустой
// последний столбец сохраняется; общий trim разрушил бы этот wire contract.
export function parseTelegramIntroductionInventory(raw){
 fail(typeof raw==='string'&&Buffer.byteLength(raw)<=4<<20&&!raw.includes('\r'));
 const items=[],seen=new Set();
 for(const line of raw.split('\n').filter(value=>value!=='')){
  const fields=line.split('\t');fail(fields.length===paths.length);
  const [kind,name,uid,namespace,generation,app,owners,podSA,templateSA,cronSA,templateApp,cronApp,replicas,statusReplicas,observed,...secretColumns]=fields;
  fail(kinds.has(kind)&&/^[a-z0-9][a-z0-9.-]{0,252}$/.test(name)&&/^[A-Za-z0-9-]{1,128}$/.test(uid)&&namespace==='kodex-system'&&
   [app,podSA,templateSA,cronSA,templateApp,cronApp].every(value=>/^[A-Za-z0-9_.-]{0,253}$/.test(value))&&!seen.has(kind+'/'+name));
  let references;try{references=owners?JSON.parse(owners):[];}catch{throw Error('TELEGRAM_INTRODUCTION_INVENTORY_INVALID');}
  fail(Array.isArray(references)&&references.length<=16);seen.add(kind+'/'+name);
  const secretRefs=secretColumns.flatMap(value=>value===''?[]:value.split(' '));
  fail(secretRefs.every(value=>/^[a-z0-9][a-z0-9.-]{0,252}$/.test(value)));
  items.push({kind,secretRefs,metadata:{name,uid,namespace,generation:number(generation),labels:{'app.kubernetes.io/name':app},ownerReferences:references},
   spec:{serviceAccountName:podSA,replicas:number(replicas),template:{metadata:{labels:{'app.kubernetes.io/name':templateApp}},spec:{serviceAccountName:templateSA}},
    jobTemplate:{spec:{template:{metadata:{labels:{'app.kubernetes.io/name':cronApp}},spec:{serviceAccountName:cronSA}}}}},
   status:{replicas:number(statusReplicas),observedGeneration:number(observed)}});
 }
 fail(items.length<=10000);return {items};
}
// Source/full-object adapters используют тот же набор имён ссылок, не literal
// env values. Обычный namespace reader получает их только через JSONPath выше.
export function telegramProducerSecretReferences(spec){
 const names=[];
 const visit=node=>{
  if(!node||typeof node!=='object')return;
  for(const [key,value] of Object.entries(node)){
   if(key==='secretName'&&typeof value==='string')names.push(value);
   if(['secretKeyRef','secretRef','secret','nodePublishSecretRef'].includes(key)&&typeof value?.name==='string')names.push(value.name);
   if(key==='imagePullSecrets'&&Array.isArray(value))for(const item of value)if(typeof item?.name==='string')names.push(item.name);
   visit(value);
  }
 };visit(spec);return names;
}
export function readTelegramIntroductionInventory(kube){
 return parseTelegramIntroductionInventory(kube(['-n','kodex-system','get',telegramIntroductionResources,'-o',telegramIntroductionMetadataQuery]));
}
