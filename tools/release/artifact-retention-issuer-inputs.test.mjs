import assert from 'node:assert/strict';
import {test} from 'node:test';
import {artifactRetentionIssuerPatchFields, legacyPersonalStagedInputsSchema, stageArtifactRetentionIssuerSpec, stagePersonalIntegrationsControlPlaneSpec} from './artifact-retention-issuer-inputs.mjs';

const componentImage = 'registry.example.invalid/kodex/artifact-retention@sha256:' + 'a'.repeat(64);
const authorityImage = 'registry.example.invalid/kodex/internal-rpc-authority@sha256:' + 'b'.repeat(64);
const mount = name => ({name, mountPath: '/fixture/' + name, readOnly: true});

test('Добавляет authority к точному stopped pod spec, сохраняя S3 CA и политику observability', () => {
  const before = {replicas: 0, template: {metadata:{labels: {'app.kubernetes.io/name':'artifact-retention'}}, spec:{
    serviceAccountName: 'artifact-retention', automountServiceAccountToken: false,
    securityContext:{runAsNonRoot:true,fsGroup:10001,fsGroupChangePolicy:'OnRootMismatch'},
    containers: [{name:'artifact-retention',image:componentImage,env:[{name:'OTEL_SDK_DISABLED',value:'true'}],
      volumeMounts:[mount('object-storage-ca')]}],
    volumes: [{name:'object-storage-ca',configMap:{name:'fixture-local-s3'}}],
  }}};
  const added = ['internal-rpc-authority-sockets','workload-tls','control-plane-ca','application-grant'];
  const deployment = {spec:{template:{metadata:{labels:{'kodex.dev/internal-rpc-authority-abi':'2',
    'kodex.dev/internal-rpc-authority-issuer':'enabled'}},spec:{
    securityContext:{fsGroup:29000,fsGroupChangePolicy:'OnRootMismatch'},
    containers:[{name:'artifact-retention',env:[
      {name:'INTERNAL_RPC_AUTHORITY_ISSUER_SOCKET',value:'/run/kodex/issuer.sock'},
      {name:'INTERNAL_RPC_AUTHORITY_LOCAL_ROLE',value:'issuer'}],volumeMounts:added.map(mount)},
      {name:'internal-rpc-authority-issuer',env:[{name:'FIXTURE',value:'issuer'}],volumeMounts:[mount(added[0])]},
      {name:'platform-worker-grant-agent',env:[],volumeMounts:[mount('application-grant')]}],
    initContainers:[{name:'internal-rpc-authority-socket-init',securityContext:{runAsUser:29000,runAsGroup:29000},
      volumeMounts:[mount(added[0])]}],
    volumes:added.map(name=>({name,emptyDir:{}})),
  }}}};
  const source = {deployment};
  const saved = structuredClone(before);
  const staged = stageArtifactRetentionIssuerSpec(before, source, componentImage, authorityImage);
  assert.deepEqual(before,saved);
  assert.equal(staged.replicas,0);
  assert.deepEqual(staged.template.spec.volumes[0],before.template.spec.volumes[0]);
  assert.deepEqual(staged.template.spec.securityContext,{...before.template.spec.securityContext,fsGroup:29000});
  const historical=stageArtifactRetentionIssuerSpec(before,source,componentImage,authorityImage,legacyPersonalStagedInputsSchema);
  assert.deepEqual(historical.template.spec.securityContext,before.template.spec.securityContext);
  assert.deepEqual(staged.template.spec.containers.map(c=>c.image),[componentImage,authorityImage,authorityImage]);
  assert.equal(staged.template.spec.initContainers[0].image,authorityImage);
  assert(staged.template.spec.containers.every(c=>c.env.some(e=>e.name==='OTEL_SDK_DISABLED'&&e.value==='true')));
  assert.throws(()=>stageArtifactRetentionIssuerSpec({...before,replicas:2},source,componentImage,authorityImage),/STOPPED_SPEC_REJECTED/);
  assert.throws(()=>stageArtifactRetentionIssuerSpec(before,source,componentImage,componentImage),/STOPPED_SPEC_REJECTED/);
  assert.throws(()=>stageArtifactRetentionIssuerSpec(staged,source,componentImage,authorityImage),/STOPPED_SPEC_REJECTED/);
  const badSource=structuredClone(source);
  badSource.deployment.spec.template.spec.initContainers[0].securityContext.runAsGroup=12345;
  assert.throws(()=>stageArtifactRetentionIssuerSpec(before,badSource,componentImage,authorityImage),/SOCKET_GROUP_REJECTED/);
});

test('Дельта ограничена семью проекциями и тремя существующими ingress правилами', () => {
  const authorityPrefix = 'internal-rpc-authority-artifact-retention-';
  const issuerSecretNames = ['issuer-key','issuer-readback-credential','issuer-readback-possession','issuer-restore-ack',
    'issuer-restore-credential','manifest-trust','proof-trust'].map(suffix=>authorityPrefix+suffix);
  const names=['control-plane-exact-runtime-paths','internal-rpc-authority-readback-attestor-exact-paths',
    'internal-rpc-authority-restore-controller-exact-paths'];
  const resource=(kind,name,fields)=>({kind,metadata:{name,namespace:'kodex-system',resourceVersion:'42',
    uid:'a1a2a3a4-0000-4000-8000-a1a2a3a4a5a6',labels:{'app.kubernetes.io/part-of':'kodex'}},...fields});
  const policy=(name,from)=>resource('NetworkPolicy',name,{spec:{podSelector:{matchLabels:{fixture:name}},policyTypes:['Ingress','Egress'],
    ingress:[{from,ports:[{protocol:'TCP',port:8443}]}],egress:[{to:[{podSelector:{matchLabels:{fixture:'egress'}}}]}]}});
  const set=values=>({podSelector:{matchExpressions:[{operator:'In',key:'app.kubernetes.io/name',values}]}});
  const currentPeer={podSelector:{matchLabels:{'app.kubernetes.io/name':'session-archive'}}};
  const newPeer={podSelector:{matchLabels:{'app.kubernetes.io/name':'artifact-retention','app.kubernetes.io/component':'retention-job'}}};
  const role=resource('Role','internal-rpc-authority-publisher',{rules:[{apiGroups:[''],resources:['secrets'],
    verbs:['get','update'],resourceNames:['fixture-predecessor']}]});
  const ingressPolicies=names.map((name,index)=>policy(name,index?[set(['session-archive','artifact-retention'])]:[currentPeer,newPeer]));
  const before=names.map((name,index)=>policy(name,index?[set(['session-archive'])]:[currentPeer]));
  const fields=artifactRetentionIssuerPatchFields({issuerSecretNames,ingressPolicies},[role,...before]);
  assert.deepEqual(fields[0].after[0].resourceNames,['fixture-predecessor',...issuerSecretNames]);
  assert.deepEqual(fields[1].after.ingress[0].from,[currentPeer,newPeer]);
  assert.deepEqual(fields[2].after.ingress[0].from,[set(['session-archive','artifact-retention'])]);
  assert(fields.slice(1).every(({before,after})=>JSON.stringify(before.spec.egress)===JSON.stringify(after.egress)));
  assert.throws(()=>artifactRetentionIssuerPatchFields({issuerSecretNames:[...issuerSecretNames,'*'],ingressPolicies},[role,...before]),/PROJECTION_CHANGED/);
  role.rules[0].verbs.push('create');
  assert.throws(()=>artifactRetentionIssuerPatchFields({issuerSecretNames,ingressPolicies},[role,...before]),/PUBLISHER_ROLE_REJECTED/);
});

test('Остановленный control-plane получает два точных входа, сохраняя старые настройки', () => {
  const spec={replicas:0,template:{spec:{serviceAccountName:'control-plane',
    containers:[{name:'control-plane',env:[{name:'CONTROL_PLANE_PROVIDER_FIXTURE',value:'synthetic'}],
      volumeMounts:[{name:'application-grants',mountPath:'/var/run/config/kodex/control-plane/application-grants',readOnly:true}]}],
    volumes:[{name:'application-grants',secret:{secretName:'control-plane-application-grants',defaultMode:292}}]}}};
  const saved=structuredClone(spec);
  const after=stagePersonalIntegrationsControlPlaneSpec(spec);
  const pod=after.template.spec,app=pod.containers[0];
  assert.deepEqual(spec,saved);
  assert.deepEqual(app.env.map(entry=>entry.name),['CONTROL_PLANE_PROVIDER_FIXTURE','CONTROL_PLANE_ARTIFACT_RETENTION_GRANT_TRUST_FILE',
    'CONTROL_PLANE_KUBERNETES_CAPTURE_SPOOL_ROOT']);
  assert.deepEqual(pod.volumes.at(-1),{name:'kubernetes-capture-spool',emptyDir:{sizeLimit:'512Mi'}});
  assert.deepEqual(stagePersonalIntegrationsControlPlaneSpec(after),after);
  assert.throws(()=>stagePersonalIntegrationsControlPlaneSpec({...spec,replicas:1}),/STOPPED_SPEC_REJECTED/);
  pod.volumes.at(-1).emptyDir.sizeLimit='4Gi';
  assert.throws(()=>stagePersonalIntegrationsControlPlaneSpec(after),/INPUT_CONFLICT/);
});
