import {readFileSync,writeFileSync} from 'node:fs';
import {join} from 'node:path';
import {planComponentAPIMaintenance} from '../component-api-maintenance.mjs';
import {planComponentAPITraffic,requireClosedAPIAdmission} from '../component-api-traffic.mjs';
import {catalogPolicySource,readCatalogTarget} from '../authority-catalog-transition.mjs';
import {renderAuthorityPolicyEnvironment} from '../authority-policy-environment.mjs';
import {fingerprint} from '../scoped-release.mjs';

export const uid = n => `00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
export const image = 'registry.example.invalid/kodex/internal-rpc-authority@sha256:'+'a'.repeat(64);
const labels={'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'};
export const metadata=(name,n)=>({name,namespace:'kodex-system',uid:uid(n),resourceVersion:'1',labels:{...labels}});

export function fixture(){
  const deployment={apiVersion:'apps/v1',kind:'Deployment',metadata:{...metadata('control-plane',3),generation:1},spec:{replicas:2,strategy:{type:'RollingUpdate',rollingUpdate:{maxUnavailable:0,maxSurge:1}},template:{spec:{
    containers:[{name:'control-plane',image:'unchanged-application',env:[{name:'CONTROL_PLANE_OIDC_ISSUER',valueFrom:{configMapKeyRef:{name:'oidc-public',key:'issuer'}}},{name:'CONTROL_PLANE_OIDC_AUDIENCE',value:'kodex-control-api'}],
      volumeMounts:[{name:'authority-policy',mountPath:'/var/run/config/kodex/control-plane/authority',readOnly:true}]},{name:'verifier',image:'unchanged-sidecar'}],
    volumes:[{name:'authority-policy',configMap:{name:'internal-rpc-authority-publisher-target-registry',defaultMode:288,items:[{key:'authority-policy.json',path:'policy.json'}]}}],
  }}},status:{observedGeneration:1,availableReplicas:2,replicas:2}};
  const registry={apiVersion:'v1',kind:'ConfigMap',metadata:metadata('internal-rpc-authority-publisher-target-registry',4),data:{}};
  const publisher={apiVersion:'apps/v1',kind:'Deployment',metadata:metadata('internal-rpc-authority-publisher',5),spec:{template:{metadata:{},spec:{containers:[{name:'publisher',image,command:['/usr/local/bin/internal-rpc-authority-publisher']}]}}}};
  const api={apiVersion:'apps/v1',kind:'Deployment',metadata:{...metadata('control-api-gateway',11),generation:1},
    spec:{replicas:3,selector:{matchLabels:{'app.kubernetes.io/name':'control-api-gateway'}},template:{spec:{containers:[{name:'control-api-gateway',image:'registry.example.invalid/kodex/control-api-gateway@sha256:'+'b'.repeat(64)}]}}},
    status:{observedGeneration:1,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3}};
  const service={apiVersion:'v1',kind:'Service',metadata:metadata('control-api-gateway',12),
    spec:{type:'ClusterIP',clusterIP:'192.0.2.10',selector:{'app.kubernetes.io/name':'control-api-gateway'},ports:[{name:'https',port:8443,targetPort:'https'},{name:'metrics',port:9090,targetPort:'metrics'}]}};
  const installation={apiVersion:'v1',kind:'ConfigMap',metadata:{...metadata('kodex-installation-profile',6),labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/managed-by':'kodex-install'}},data:{'profile.json':'synthetic-installation'}};
  const oidc={apiVersion:'v1',kind:'ConfigMap',metadata:metadata('oidc-public',7),data:{issuer:'https://sso.example.invalid/realms/kodex'}};
  const resources=new Map([
    ['namespace/kube-system',{metadata:{uid:uid(1)}}],['namespace/kodex-system',{metadata:{uid:uid(2)}}],
    ['deployment/control-plane',deployment],['deployment/internal-rpc-authority-publisher',publisher],
    ['configmap/internal-rpc-authority-publisher-target-registry',registry],['configmap/kodex-installation-profile',installation],['configmap/oidc-public',oidc],
    ['deployment/control-api-gateway',api],['service/control-api-gateway',service],
  ]);
  const profile={schema:'kodex.portable-runner-target/v1',context:'synthetic-production',clusterUID:uid(1),namespaceUID:uid(2),installationUID:uid(6),installationSHA256:fingerprint(installation.data),
    resources:[deployment,publisher,registry,api,service].map(resource=>({apiVersion:resource.apiVersion,kind:resource.kind,name:resource.metadata.name,uid:resource.metadata.uid}))};
  const get=(kind,name)=>structuredClone(resources.get(`${kind}/${name}`));
  const target=readCatalogTarget(profile,profile.context,get);
  const previous=renderAuthorityPolicyEnvironment(catalogPolicySource('predecessor').raw,target.transition.environment);
  const desired=renderAuthorityPolicyEnvironment(catalogPolicySource('desired').raw,target.transition.environment);
  registry.data={'authority-policy.json':previous.raw,'key-delivery-targets.yaml':readFileSync(new URL('../../../deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml',import.meta.url),'utf8').replace('source_revision: 7','source_revision: 1'),'unrelated':'unchanged'};
  return {deployment,registry,publisher,api,service,installation,oidc,profile,get,resources,target,previous,desired};
}


export function prepareAdmission(f){
  const observed={context:f.profile.context,clusterUID:f.profile.clusterUID,namespaceUID:f.profile.namespaceUID,installation:f.installation,resources:{items:[]}};
  const closePlan=planComponentAPIMaintenance(f.api,f.profile,observed,f.revision,'close');
  const closed={plan:closePlan,events:[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(closePlan),maintenanceID:closePlan.maintenanceID},
    {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'CLOSED',deploymentUID:f.api.metadata.uid,maintenanceID:closePlan.maintenanceID}]};
  f.api.spec=structuredClone(closePlan.afterSpec);f.api.metadata.annotations=structuredClone(closePlan.afterAnnotations);
  f.api.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
  const trafficPlan=planComponentAPITraffic({service:f.service,deployment:f.api,resources:{items:[]},target:f.profile,observed,sourceSHA:f.revision,action:'isolate',closed});
  const trafficEvents=[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(trafficPlan),maintenanceID:trafficPlan.maintenanceID},
    {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'ISOLATED',serviceUID:f.service.metadata.uid,maintenanceID:closePlan.maintenanceID}];
  f.service.spec=structuredClone(trafficPlan.afterSpec);f.service.metadata.annotations=structuredClone(trafficPlan.afterAnnotations);
  const paths={planPath:join(f.directory,'closed-plan.json'),evidencePath:join(f.directory,'closed.jsonl'),trafficPlanPath:join(f.directory,'traffic-plan.json'),trafficEvidencePath:join(f.directory,'traffic.jsonl')};
  for(const [path,value]of [[paths.planPath,closePlan],[paths.trafficPlanPath,trafficPlan]])writeFileSync(path,JSON.stringify(value),{mode:0o600});
  for(const [path,value]of [[paths.evidencePath,closed.events],[paths.trafficEvidencePath,trafficEvents]])writeFileSync(path,value.map(row=>JSON.stringify(row)+'\n').join(''),{mode:0o600});
  const proof=requireClosedAPIAdmission(f.api,{items:[]},closed,{plan:trafficPlan,events:trafficEvents,service:f.service,slices:f.state.slices,pods:f.state.isolationPods});
  const flags=['--maintenance-plan',paths.planPath,'--maintenance-evidence',paths.evidencePath,'--traffic-plan',paths.trafficPlanPath,'--traffic-evidence',paths.trafficEvidencePath];
  return {paths,flags,proof,closed,trafficPlan,trafficEvents};
}

