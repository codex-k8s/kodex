import {telegramResourceCreateKeys} from '../telegram-introduction-contract.mjs';
const uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;

// Только metadata fixture для pure producer/consumer tests. Она не доказывает
// создание ресурсов и не используется live CLI вместо fresh private readback.
export function telegramIntroductionFixture({sourceSHA,imageRef,deploymentUID=uid(109),specSHA256='6'.repeat(64)}){
 return {schema:'kodex.telegram-introduced-receiver/v1',state:'PENDING_AUTHORITY',sourceSHA,
  planPath:'/synthetic/create-plan.json',evidencePath:'/synthetic/create.jsonl',publicationPath:'/synthetic/publication.json',
  publicationSHA256:'1'.repeat(64),createPlanSHA256:'2'.repeat(64),createID:uid(120),targetSHA256:'3'.repeat(64),deploymentUID,specSHA256,
  imageRef,intendedReplicas:3,resources:telegramResourceCreateKeys.map((resource,index)=>({resource,state:'CREATED_DORMANT',
   uid:resource.startsWith('apps/v1|Deployment|')?deploymentUID:uid(100+index),resourceSHA256:String(index).repeat(64),
   ...(resource==='v1|Service|interaction-gateway'?{assigned:{clusterIP:'10.43.1.2',clusterIPs:['10.43.1.2'],ipFamilies:['IPv4'],ipFamilyPolicy:'SingleStack'}}:{})}))};
}

// Закрытый минимальный Kustomize transport fixture; production CLI получает
// настоящий source render самостоятельно, эти objects не поставляются в кластер.
export function telegramCreateSourceFixture(){
 const metadata=name=>({name,namespace:'kodex-system',labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'production','kodex.dev/profile':'web-only'}});
 const resources=telegramResourceCreateKeys.map(identity=>{const [apiVersion,kind,name]=identity.split('|');return {apiVersion,kind,metadata:metadata(name),...(kind==='ConfigMap'?{data:{INTERACTION_GATEWAY_ALLOWED_HOSTS:''}}:kind==='ServiceAccount'?{}:{spec:{}})};});
 resources.find(r=>r.kind==='Deployment').spec={replicas:3,selector:{matchLabels:{'app.kubernetes.io/name':'interaction-gateway'}},template:{metadata:{labels:{'app.kubernetes.io/name':'interaction-gateway'}},spec:{serviceAccountName:'interaction-gateway',containers:[{name:'interaction-gateway'},{name:'internal-rpc-authority-issuer'},{name:'platform-worker-grant-agent'}],initContainers:[{name:'internal-rpc-authority-socket-init'}]}}};
 resources.find(r=>r.kind==='Service').spec={type:'ClusterIP',selector:{'app.kubernetes.io/name':'interaction-gateway'},ports:[{name:'metrics',port:9090,targetPort:'metrics',protocol:'TCP'}]};return resources;
}
