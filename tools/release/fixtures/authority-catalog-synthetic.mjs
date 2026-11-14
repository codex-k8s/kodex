import {readFileSync} from 'node:fs';
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
  const installation={apiVersion:'v1',kind:'ConfigMap',metadata:{...metadata('kodex-installation-profile',6),labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/managed-by':'kodex-install'}},data:{'profile.json':'synthetic-installation'}};
  const oidc={apiVersion:'v1',kind:'ConfigMap',metadata:metadata('oidc-public',7),data:{issuer:'https://sso.example.invalid/realms/kodex'}};
  const resources=new Map([
    ['namespace/kube-system',{metadata:{uid:uid(1)}}],['namespace/kodex-system',{metadata:{uid:uid(2)}}],
    ['deployment/control-plane',deployment],['deployment/internal-rpc-authority-publisher',publisher],
    ['configmap/internal-rpc-authority-publisher-target-registry',registry],['configmap/kodex-installation-profile',installation],['configmap/oidc-public',oidc],
  ]);
  const profile={schema:'kodex.portable-runner-target/v1',context:'synthetic-production',clusterUID:uid(1),namespaceUID:uid(2),installationUID:uid(6),installationSHA256:fingerprint(installation.data),
    resources:[deployment,publisher,registry].map(resource=>({apiVersion:resource.apiVersion,kind:resource.kind,name:resource.metadata.name,uid:resource.metadata.uid}))};
  const get=(kind,name)=>structuredClone(resources.get(`${kind}/${name}`));
  const target=readCatalogTarget(profile,profile.context,get);
  const previous=renderAuthorityPolicyEnvironment(catalogPolicySource('predecessor').raw,target.transition.environment);
  const desired=renderAuthorityPolicyEnvironment(catalogPolicySource('desired').raw,target.transition.environment);
  registry.data={'authority-policy.json':previous.raw,'key-delivery-targets.yaml':readFileSync(new URL('../../../deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml',import.meta.url),'utf8').replace('source_revision: 7','source_revision: 1'),'unrelated':'unchanged'};
  return {deployment,registry,publisher,installation,oidc,profile,get,resources,target,previous,desired};
}

