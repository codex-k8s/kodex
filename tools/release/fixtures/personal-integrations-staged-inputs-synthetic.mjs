import {createHash} from 'node:crypto';
import {componentCohortFixture} from './component-cohort-synthetic.mjs';
import {planComponentMigrationHandoff} from '../portable-component-migration.mjs';
import {legacyPersonalStagedInputsSchema} from '../artifact-retention-issuer-inputs.mjs';
const source={revision:'a'.repeat(40)},authorityImage='registry.example.invalid/kodex/internal-rpc-authority@sha256:'+'b'.repeat(64);
const mount=name=>({name,mountPath:'/synthetic/'+name,readOnly:true});

// Общая synthetic derivation двух template delta; не actual authority/DDL proof.
export function personalIntegrationsStagedInputsFixture(profile='personal-integrations',schema=legacyPersonalStagedInputsSchema){
 const f=componentCohortFixture(profile);
 const cpDeployment=f.deployments.find(d=>d.metadata.name==='control-plane'),cp=cpDeployment.spec.template.spec;
 cp.serviceAccountName='control-plane';cp.containers[0].volumeMounts=[{name:'application-grants',mountPath:'/var/run/config/kodex/control-plane/application-grants',readOnly:true}];
 cp.volumes=[{name:'application-grants',secret:{secretName:'control-plane-application-grants',defaultMode:292}}];
 const previousSHA='a'.repeat(64),raw='{"policy_revision":110}\n',policySHA256=createHash('sha256').update(raw).digest('hex');
 const previousName='control-plane-policy-projection-'+previousSHA.slice(0,16);
 const policyMount='/var/run/config/kodex/control-plane/environment-policy',policyVolume='control-plane-environment-policy';
 cp.containers[0].env.push({name:'CONTROL_PLANE_AUTHORITY_POLICY_FILE',value:policyMount+'/policy.json'});
 cp.containers[0].volumeMounts.push({name:policyVolume,mountPath:policyMount,readOnly:true});
 cp.volumes.push({name:policyVolume,configMap:{name:previousName,defaultMode:292}});
 const art=f.deployments.find(d=>d.metadata.name==='artifact-retention').spec.template;
 art.metadata.labels={'app.kubernetes.io/name':'artifact-retention'};
 Object.assign(art.spec,{serviceAccountName:'artifact-retention',automountServiceAccountToken:false,
  securityContext:{fsGroup:10001,fsGroupChangePolicy:'OnRootMismatch'},volumes:[{name:'synthetic-existing',emptyDir:{}}]});
 art.spec.containers[0].volumeMounts=[mount('synthetic-existing')];
 const added=['internal-rpc-authority-sockets','workload-tls','control-plane-ca','application-grant'];
 const deployment={spec:{template:{metadata:{labels:{'kodex.dev/internal-rpc-authority-abi':'2','kodex.dev/internal-rpc-authority-issuer':'enabled'}},spec:{
  securityContext:{fsGroup:29000,fsGroupChangePolicy:'OnRootMismatch'},
  containers:[{name:'artifact-retention',env:[{name:'INTERNAL_RPC_AUTHORITY_ISSUER_SOCKET',value:'/run/kodex/issuer.sock'},{name:'INTERNAL_RPC_AUTHORITY_LOCAL_ROLE',value:'issuer'}],volumeMounts:added.map(mount)},
   {name:'internal-rpc-authority-issuer',env:[],volumeMounts:[mount(added[0])]},
   {name:'platform-worker-grant-agent',env:[],volumeMounts:[mount('application-grant')]}],
  initContainers:[{name:'internal-rpc-authority-socket-init',securityContext:{runAsUser:29000,runAsGroup:29000},
   volumeMounts:[mount(added[0])]}],volumes:added.map(name=>({name,emptyDir:{}}))}}}};
 const inputs={schema,sourceSHA:source.revision,issuerInputs:{sourceSHA:source.revision,deployment},
  cpPolicy:{schema:'kodex.personal-integrations-cp-policy/v1',sourceSHA:source.revision,deploymentUID:cpDeployment.metadata.uid,
   targetSHA256:'1'.repeat(64),predecessor:{name:previousName,uid:cpDeployment.metadata.uid,policySHA256:previousSHA},policySHA256,
   projection:{apiVersion:'v1',kind:'ConfigMap',metadata:{name:'control-plane-policy-projection-'+policySHA256.slice(0,16),
    namespace:'kodex-system',labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/policy-projection-issue':'2145'}},immutable:true,data:{'policy.json':raw}}},
  authority:{schema:'kodex.personal-integrations-authority-image/v1',sourceSHA:source.revision,imageRef:authorityImage,publicationSHA256:'c'.repeat(64),nodeProofSHA256:'d'.repeat(64)}};
 return {...f,inputs,read:()=>planComponentMigrationHandoff(f.value,f.deployments,source,undefined,inputs)};
}
