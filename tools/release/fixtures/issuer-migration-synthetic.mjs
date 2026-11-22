import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,rmSync,readFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from '../scoped-release.mjs';
import {canonicalPostgresBackupSpec} from '../backup-postgres-boundary.mjs';
import {authorityOwnerDatabaseArgs} from '../migration-owner-database.mjs';
import {privateJournal} from '../../dev/role-image-acceptance.mjs';
import {renderIssuerMigrationJob,issuerMigrationJournalHeader} from '../artifact-retention-issuer-migration.mjs';

export const root=resolve(fileURLToPath(new URL('../../../',import.meta.url)));
export const uid=n=>'10000000-0000-4000-8000-'+String(n).padStart(12,'0');
export const before=()=>({head:20261115000100,roleExists:false,roleValid:false,activeRotationCount:0,migratorSessions:0,observedAt:new Date().toISOString()});
export const after=()=>({...before(),head:20261115000200,roleExists:true,roleValid:true});
export const backend={systemIdentifier:'18446744073709551615',databaseOID:'16384',database:'internal_rpc_authority',port:'5432',serverAddress:null,serverPort:null};

export function publication() {
 const receipt={schema:'kodex.stable-local-build/v1',component:'internal-rpc-authority',sourceSHA:'a'.repeat(40),contextSHA256:'b'.repeat(64),archiveSHA256:'c'.repeat(64),imageDigest:'sha256:'+'d'.repeat(64),publication:'NOT_RUN',admission:'NOT_RUN',nodePull:'NOT_RUN',dockerfile:'services/internal/internal-rpc-authority/Dockerfile'};
 const image={component:receipt.component,sourceSHA:receipt.sourceSHA,digest:receipt.imageDigest,tag:'stable-'+receipt.sourceSHA+'-'+receipt.imageDigest.slice(-12)};
 const plan={schema:'kodex.stable-image-publication/v2',artifactProfile:'built',receipt,image,archiveSHA256:receipt.archiveSHA256,
  context:'fixture',clusterUID:uid(2),namespaceUID:uid(3)};
 return {publicationPlan:plan,publicationEvidence:[{schema:'kodex.stable-image-publication-attempt/v1',status:'INTENT',mode:'apply',planSHA256:fingerprint(plan),component:image.component,digest:image.digest},{status:'PASS',phase:'REGISTRY_PUBLICATION_ONLY',nodePull:'NOT_RUN'}]};
}

export function fixture(t) {
 const directory=mkdtempSync(join(tmpdir(),'issuer-migration-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const plan={schema:'kodex.artifact-retention-issuer-migration/v1',intentID:uid(1),evidencePath:join(directory,'evidence.jsonl'),source:{path:root,revision:'a'.repeat(40)},publication:publication(),
  image:'registry.local.kodex/kodex/internal-rpc-authority@sha256:'+'d'.repeat(64),sqlSHA256:'',target:{context:'fixture',clusterUID:uid(2),namespaceUID:uid(3)},
  postgres:{pod:'kodex-postgresql-0',uid:uid(4),ownerUID:uid(5)},backend:structuredClone(backend),before:before()};
 plan.jobSpecSHA256=fingerprint(renderIssuerMigrationJob(plan).spec);
 const expected=renderIssuerMigrationJob(plan);
 const actual={...structuredClone(expected),metadata:{...expected.metadata,uid:uid(6)},status:{succeeded:1,conditions:[{type:'Complete',status:'True'}]}};
 const spec=structuredClone(expected.spec.template.spec);
 const stateFor=container=>({name:container.name,restartCount:0,imageID:'containerd://'+container.image.split('@')[1],state:{terminated:{exitCode:0}}});
 const pod={apiVersion:'v1',kind:'Pod',metadata:{name:'synthetic',namespace:'kodex-system',uid:uid(7),ownerReferences:[{kind:'Job',name:actual.metadata.name,uid:uid(6),controller:true}]},spec,
  status:{phase:'Succeeded',containerStatuses:spec.containers.map(stateFor),initContainerStatuses:(spec.initContainers??[]).map(stateFor)}};
 const state={job:null,pod,creates:0,reads:0,preflights:0,loseACK:false,absentAfterUnknown:false};
 const io={expectedJob:()=>expected,preflight(){state.preflights++;},getJob:()=>state.job,getPods:()=>({items:[state.pod]}),readState(){state.reads++;return state.job?after():before();},
  createJob(){state.creates++;if(!state.absentAfterUnknown)state.job=structuredClone(actual);if(state.loseACK)throw Error('ISSUER_MIGRATION_TARGET_OPERATION_UNCONFIRMED');return state.job;}};
 const path=join(directory,'evidence.jsonl');
 const journal=()=>privateJournal(path,issuerMigrationJournalHeader(plan));
 return {plan,expected,actual,pod,state,io,path,journal};
}

export function ownerFixture() {
 const raw=['deploy/k8s/base/platform-state/postgresql.yaml','deploy/k8s/base/platform-state/ca-bundles.yaml','deploy/k8s/base/internal-rpc-authority-data/serviceaccount.yaml','deploy/k8s/base/internal-rpc-authority-data/networkpolicy.yaml'].map(path=>readFileSync(join(root,path),'utf8')).join('\n---\n');
 const docs=JSON.parse(execFileSync('python3',['-I','-c','import sys,json,yaml; print(json.dumps(list(yaml.safe_load_all(sys.stdin))))'],{input:raw,encoding:'utf8'}));
 const resources=new Map(docs.filter(Boolean).map(item=>[item.kind+'/'+item.metadata.name,item]));
 const spec=canonicalPostgresBackupSpec();
 const podSpec=structuredClone(spec);podSpec.volumes.push({name:'data',persistentVolumeClaim:{claimName:'data-kodex-postgresql-0'}});
 const stateful={apiVersion:'apps/v1',kind:'StatefulSet',metadata:{name:'kodex-postgresql',namespace:'kodex-system',uid:uid(5)},spec:{template:{spec}}};
 const pod={apiVersion:'v1',kind:'Pod',metadata:{name:'kodex-postgresql-0',namespace:'kodex-system',uid:uid(4),labels:{'app.kubernetes.io/name':'kodex-postgresql'},ownerReferences:[{apiVersion:'apps/v1',kind:'StatefulSet',name:'kodex-postgresql',uid:uid(5),controller:true}]},spec:podSpec,
  status:{phase:'Running',podIPs:[{ip:'10.42.0.3'}],conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:'postgresql',ready:true,state:{running:{}},imageID:'containerd://'+spec.containers[0].image.split('@')[1]}]}};
 const service=structuredClone(resources.get('Service/internal-rpc-authority-postgresql-rw'));
 service.metadata={...service.metadata,namespace:'kodex-system',uid:uid(8)};service.spec.type='ClusterIP';
 const endpoint={apiVersion:'discovery.k8s.io/v1',kind:'EndpointSlice',metadata:{name:'authority-fixture',namespace:'kodex-system',uid:uid(9),labels:{'kubernetes.io/service-name':'internal-rpc-authority-postgresql-rw','endpointslice.kubernetes.io/managed-by':'endpointslice-controller.k8s.io'},ownerReferences:[{kind:'Service',name:service.metadata.name,uid:uid(8),controller:true}]},addressType:'IPv4',ports:[{name:'postgresql',protocol:'TCP',port:5432}],endpoints:[{addresses:['10.42.0.3'],conditions:{ready:true},targetRef:{kind:'Pod',namespace:'kodex-system',name:pod.metadata.name,uid:uid(4)}}]};
 const caName='internal-rpc-authority-postgresql-ca';
 const bundle=structuredClone(resources.get('Bundle/'+caName));
 Object.assign(bundle.metadata,{uid:uid(12),resourceVersion:'10'});
 bundle.status={conditions:[{type:'Synced',status:'True'}]};
 const ca={kind:'ConfigMap',metadata:{name:caName,namespace:'kodex-system',uid:uid(13),resourceVersion:'1',
  labels:{'trust.cert-manager.io/bundle':caName},ownerReferences:[{apiVersion:bundle.apiVersion,kind:'Bundle',name:caName,uid:uid(12),controller:true}]},
  data:{'ca.pem':'-----BEGIN CERTIFICATE-----\nsynthetic\n-----END CERTIFICATE-----'}};
 const state={endpoint,pod,ca,bundle,backend:{...backend},creates:0,queries:[],job:null};
 const kube=(args,input)=>{
  if(args.includes('exec')) {
   assert.deepEqual(args,authorityOwnerDatabaseArgs);state.queries.push(input);
   return JSON.stringify(input.includes('pg_control_system()')?state.backend:state.job?after():before());
  }
  if(args.includes('create')) {
   state.creates++;state.job=JSON.parse(input);state.job.metadata.uid=uid(6);state.job.status={succeeded:1,conditions:[{type:'Complete',status:'True'}]};
   const childSpec=structuredClone(state.job.spec.template.spec);
   const status=container=>({name:container.name,imageID:'containerd://'+container.image.split('@')[1],restartCount:0,state:{terminated:{exitCode:0}}});
   state.jobPod={kind:'Pod',metadata:{name:'synthetic-migrator',uid:uid(7),namespace:'kodex-system',ownerReferences:[{kind:'Job',name:state.job.metadata.name,uid:uid(6),controller:true}]},spec:childSpec,
    status:{phase:'Succeeded',containerStatuses:childSpec.containers.map(status),initContainerStatuses:(childSpec.initContainers??[]).map(status)}};
   if(state.loseACK)throw Error('ISSUER_MIGRATION_TARGET_OPERATION_UNCONFIRMED');
   return JSON.stringify(state.job);
  }
  if(args.includes('jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.metadata.labels.app\\.kubernetes\\.io/part-of}'))return uid(10)+'|1|kodex';
  if(args.includes('bundles.trust.cert-manager.io'))return JSON.stringify(state.bundle);
  if(args.includes('configmap')&&args.includes(caName))return JSON.stringify(state.ca);
  if(args.includes('endpointslices'))return JSON.stringify({items:[state.endpoint]});
  if(args.includes('namespace'))return JSON.stringify({metadata:{uid:args.includes('kube-system')?uid(2):uid(3),labels:{'app.kubernetes.io/part-of':'kodex'}}});
  if(args.includes('kodex-installation-profile'))return JSON.stringify({metadata:{namespace:'kodex-system',uid:uid(11)},data:{profile:'synthetic'}});
  if(args.includes('statefulset'))return JSON.stringify(stateful);
  if(args.includes('pod')&&args.includes('kodex-postgresql-0'))return JSON.stringify(state.pod);
  if(args.includes('service'))return JSON.stringify(service);
  if(args.includes('serviceaccount'))return JSON.stringify(resources.get('ServiceAccount/internal-rpc-authority-migrator'));
  if(args.includes('networkpolicy'))return JSON.stringify(resources.get('NetworkPolicy/'+args[args.indexOf('networkpolicy')+1]));
  if(args.includes('jobs')||args.includes('pods')&&!args.includes('-l')){
   assert(args.includes('--chunk-size=0'));
   const items=args.includes('jobs')?(state.job?[state.job]:[]):[state.pod,...(state.jobPod?[state.jobPod]:[])];
   return '\n'+items.map(item=>{
    const spec=item.spec.template?.spec??item.spec;
    return [item.metadata.name,item.metadata.uid,spec.serviceAccountName??'',spec.serviceAccount??'',
      (spec.volumes??[]).map(volume=>volume.secret?.secretName).filter(Boolean).join(' '),'','','','',''].join('\t')+'\n';
   }).join('');
  }
  if(args.includes('pods'))return JSON.stringify({items:state.jobPod?[state.jobPod]:[]});
  if(args.includes('pod')&&args.includes('synthetic-migrator'))return JSON.stringify(state.jobPod);
  if(args.includes('job'))return state.job?JSON.stringify(state.job):'';
  throw Error('unexpected fixture command '+JSON.stringify(args));
 };
 return {state,kube};
}
