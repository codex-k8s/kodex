import assert from 'node:assert/strict';
import {readFileSync,mkdtempSync,rmSync,chmodSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
import {fingerprint} from '../scoped-release.mjs';
import {fixture,uid} from './authority-catalog-synthetic.mjs';
import {canonicalPostgresBackupSpec} from '../backup-postgres-boundary.mjs';
import {authorityOwnerDatabaseArgs} from '../migration-owner-database.mjs';
import {deriveCanonicalRegistryAdvance} from '../authority-rotation-transition.mjs';
import {authorityReadbackContract} from '../authority-readback-targets.mjs';
import {catalogAuthorityBaselineSQL,readCatalogAuthorityBaseline} from '../catalog-authority-baseline.mjs';

const historicalSources=new WeakMap();
export const catalog106SourceSHA='fc0aa41f670f82ed2a79c139cd2ad7fc7318c2ed';
export const catalog106PredecessorSHA='fbd27e47ea7fad9c779458f8a52aac9ff114e9a8';

// Исторические045/106 воспроизводятся на собственном опубликованном source,
// без подмены текущих046/107, fake git либо ослабления closed16 registry guard.
export function historicalCatalog106Source(t,revision=catalog106SourceSHA){
 let selected=historicalSources.get(t);if(!selected){selected=new Map();historicalSources.set(t,selected);}
 if(selected.has(revision))return selected.get(revision);
 assert.ok([catalog106SourceSHA,catalog106PredecessorSHA].includes(revision));
 const root=fileURLToPath(new URL('../../../',import.meta.url)).replace(/\/$/,''),directory=mkdtempSync(join(tmpdir(),'kodex-catalog106-source-'));
 chmodSync(directory,0o700);const path=join(directory,'source');
 const git=(...args)=>execFileSync('git',['-C',root,...args],{encoding:'utf8',timeout:30000,stdio:['ignore','pipe','pipe']});
 git('worktree','add','--detach',path,revision);
 t.after(()=>{git('worktree','remove','--force',path);rmSync(directory,{recursive:true,force:true});});
 const source={path,revision};selected.set(revision,source);return source;
}

// Только Kubernetes/SQL transport заменены оснасткой; исполняются настоящие
// pinned source/template, owner-shape, namespace, canonical registry и DB readers.
export function baselineFixture(t,{revision=catalog106SourceSHA}={}){
 const f=fixture(),source=historicalCatalog106Source(t,revision).path;
 const canonical=readFileSync(`${source}/deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml`,'utf8');
 const advance=deriveCanonicalRegistryAdvance(f.registry.data['key-delivery-targets.yaml'],canonical);
 f.registry.data['key-delivery-targets.yaml']=advance.raw;f.registry.data['authority-policy.json']=f.desired.raw;
 const intentID=uid(60),operationID=advance.identity.ownerOperationID;
 f.registry.metadata.annotations={'kodex.dev/rotation-intent':intentID,'kodex.dev/rotation-operation':operationID};
 f.publisher.metadata.generation=1;f.publisher.spec.replicas=1;
 f.publisher.spec.template.metadata.annotations={'kodex.dev/authority-rotation-operation':operationID};
 f.publisher.status={observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1};
 const transition=f.target.transition;
 const plan={action:'rotate',intentID,ownerOperationID:operationID,source,revision,context:f.profile.context,
  policyTransition:transition,registry:{uid:f.registry.metadata.uid,desiredData:structuredClone(f.registry.data),desiredDataSHA256:fingerprint(f.registry.data),
   sourceRevision:advance.identity.sourceRevision,sourceDigestSHA256:advance.identity.sourceDigestSHA256},
  publisher:{uid:f.publisher.metadata.uid,desiredSpecSHA256:fingerprint(f.publisher.spec)}};
 const now=Date.now(),row=(status,extra={})=>({status,intentID,ownerOperationID:operationID,at:new Date(now).toISOString(),...extra});
 const events=[row('INTENT',{action:'rotate',planSHA256:fingerprint(plan)}),row('APPLIED',{operation:'REGISTRY_CAS'}),
  row('APPLIED',{operation:'PUBLISHER_RESTART'}),row('SUCCEEDED',{rotationStatus:'RETIRED',jobUID:uid(61)})];
 const readers=authorityReadbackContract(advance.raw);
 const owner={observedAt:new Date(now).toISOString(),operationId:operationID,operationStatus:'RETIRED',status:'RETIRED',phase:'RETIRE',
  registryRevision:advance.identity.sourceRevision,registrySourceDigestSHA256:advance.identity.sourceDigestSHA256,
  baseRevision:1,sourceRevision:4,sourceDigestSHA256:'d'.repeat(64),intentId:uid(62),protocolVersion:2,
  expectedReadbackCount:readers.expectedReadbackCount,actualReadbackCount:readers.expectedReadbackCount,
  switchNotBefore:new Date(now-90000).toISOString(),previousNotAfter:new Date(now-5000).toISOString(),completedAt:new Date(now-1000).toISOString()};
 const postgresSpec=canonicalPostgresBackupSpec();
 const set={apiVersion:'apps/v1',kind:'StatefulSet',metadata:{name:'kodex-postgresql',namespace:'kodex-system',uid:uid(70)},spec:{template:{spec:postgresSpec}}};
 const pg={apiVersion:'v1',kind:'Pod',metadata:{name:'kodex-postgresql-0',namespace:'kodex-system',uid:uid(71),labels:{'app.kubernetes.io/name':'kodex-postgresql'},
  ownerReferences:[{apiVersion:'apps/v1',kind:'StatefulSet',name:'kodex-postgresql',uid:uid(70),controller:true}]},
  spec:{...structuredClone(postgresSpec),volumes:[...postgresSpec.volumes,{name:'data',persistentVolumeClaim:{claimName:'data-kodex-postgresql-0'}}]},
  status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:'postgresql',ready:true,state:{running:{}},imageID:postgresSpec.containers[0].image}]}};
 const rs={kind:'ReplicaSet',metadata:{name:'publisher-rs',namespace:'kodex-system',uid:uid(72),ownerReferences:[{kind:'Deployment',uid:f.publisher.metadata.uid,controller:true}]}};
 const publisherPod={kind:'Pod',metadata:{name:'publisher-0',namespace:'kodex-system',uid:uid(73),labels:{'app.kubernetes.io/name':'internal-rpc-authority-publisher'},ownerReferences:[{kind:'ReplicaSet',uid:rs.metadata.uid,controller:true}]},
  spec:structuredClone(f.publisher.spec.template.spec),status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:'publisher',ready:true,state:{running:{}},imageID:f.publisher.spec.template.spec.containers[0].image}]}};
 f.resources.set('pod/kodex-postgresql-0',pg);f.resources.set('statefulset/kodex-postgresql',set);f.resources.set('pod/publisher-0',publisherPod);
 const backend={systemIdentifier:'18446744073709551615',databaseOID:'20000',database:'internal_rpc_authority',port:'5432',serverAddress:null,serverPort:null};
 const calls=[],faults={};
 const kube=(args,sql)=>{
  calls.push({args,sql});
  if(args.includes('exec')){
   assert.deepEqual(args,authorityOwnerDatabaseArgs);
   if(String(sql).includes('pg_catalog.pg_control_system()'))return JSON.stringify(backend);
   assert.ok(Buffer.from(sql).equals(catalogAuthorityBaselineSQL()));
   faults.afterOwner?.();return JSON.stringify(owner);
  }
  const index=args.indexOf('get'),kind=args[index+1],name=args[index+2];
  if(kind==='replicasets,pods,jobs')return [rs,publisherPod].map(r=>[r.kind,r.metadata.name,r.metadata.uid,r.metadata.labels?.['app.kubernetes.io/name']??'',r.metadata.namespace,JSON.stringify(r.metadata.ownerReferences)].join('\t')).join('\n')+'\n';
  const value=f.resources.get(`${kind}/${name}`);assert.ok(value,`${kind}/${name}`);return JSON.stringify(value);
 };
 const read=expected=>readCatalogAuthorityBaseline({plan,events,expected,now:()=>now},kube,{inspectSource:()=>({revision:plan.revision})});
 return {...f,plan,events,now,owner,pg,set,publisherPod,backend,calls,faults,kube,read};
}
